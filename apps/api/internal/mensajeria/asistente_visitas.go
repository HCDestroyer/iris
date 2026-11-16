package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/agenda"
	"github.com/HCDestroyer/iris/apps/api/internal/campo"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// El asistente de IA agenda una visita técnica (contratos/conversaciones.md §17.10). Dos
// herramientas: ofrecer_visita (ventanas que caben en la agenda de la zona, sin ver la agenda) y
// agendar_visita (crea la orden y la programa en una ventana ofrecida). La lógica de agenda vive en
// agenda (HuecosOfrecibles, ElegirTecnicoEnVentana) y la de órdenes en campo (CrearOrden,
// ProgramarOrden): aquí solo se aplican las reglas del asistente.

// Herramientas de visitas.
const (
	HerrOfrecerVisita = "ofrecer_visita"
	HerrAgendarVisita = "agendar_visita"
)

// VigenciaOfertaVisita: lo que vale una ventana ofrecida.
const VigenciaOfertaVisita = 30 * time.Minute

// BloqueVisita es un bloque de atención (hora de Guatemala).
type BloqueVisita struct {
	Inicio string `json:"inicio" pattern:"^([01][0-9]|2[0-3]):[0-5][0-9]$" doc:"HH:MM"`
	Fin    string `json:"fin" pattern:"^([01][0-9]|2[0-4]):[0-5][0-9]$" doc:"HH:MM, después del inicio"`
}

// VisitasAsistente son los ajustes de la empresa para que el asistente agende visitas (§17.10).
type VisitasAsistente struct {
	TiposPermitidos       []string       `json:"tiposPermitidos" minItems:"1" maxItems:"2" uniqueItems:"true" doc:"Tipos de orden que puede agendar: visita y/o mantenimiento. El primero es el que usa"`
	MaxAbiertasPorCliente int32          `json:"maxAbiertasPorCliente" minimum:"1" maximum:"5" doc:"Visitas vivas por cliente (1 por defecto)"`
	ExigeTicket           bool           `json:"exigeTicket" doc:"true (por defecto): la visita nace de un ticket; si la conversación no tiene uno, el asistente lo crea antes"`
	PermitirConMora       bool           `json:"permitirConMora" doc:"false (por defecto): con el servicio cortado por falta de pago no agenda y pasa a una persona"`
	Bloques               []BloqueVisita `json:"bloques" minItems:"1" maxItems:"6" doc:"Ventanas de llegada que se ofrecen cada día (08:00–12:00 y 13:00–17:00 por defecto)"`
	AnticipacionHoras     int32          `json:"anticipacionHoras" minimum:"0" maximum:"168" doc:"Horas mínimas entre ahora y el inicio de la ventana (12 por defecto)"`
	DiasAdelante          int32          `json:"diasAdelante" minimum:"1" maximum:"30" doc:"Hasta cuántos días adelante ofrece (7 por defecto); el límite diasAdelanteMax del perfil lo puede acortar"`
	OfertasPorDia         int32          `json:"ofertasPorDia" minimum:"1" maximum:"10" doc:"Veces que ofrece ventanas por conversación y día (3 por defecto)"`
	VisitasPorDia         int32          `json:"visitasPorDia" minimum:"1" maximum:"5" doc:"Visitas que agenda por conversación y día (1 por defecto)"`
	VisitasPorDiaEmpresa  int32          `json:"visitasPorDiaEmpresa" minimum:"1" maximum:"500" doc:"Visitas que agenda en toda la empresa por día (20 por defecto)"`
}

// TiposVisitaAsistente: los tipos de orden que el asistente puede agendar.
var TiposVisitaAsistente = []string{"visita", "mantenimiento"}

func visitasPorDefecto() VisitasAsistente {
	return VisitasAsistente{TiposPermitidos: []string{"visita"}, MaxAbiertasPorCliente: 1, ExigeTicket: true,
		Bloques: []BloqueVisita{{"08:00", "12:00"}, {"13:00", "17:00"}}, AnticipacionHoras: 12, DiasAdelante: 7,
		OfertasPorDia: 3, VisitasPorDia: 1, VisitasPorDiaEmpresa: 20}
}

// visitasDe lee los ajustes guardados (nil o inválido = los de fábrica).
func visitasDe(crudo []byte) VisitasAsistente {
	v := visitasPorDefecto()
	if len(crudo) == 0 {
		return v
	}
	var g VisitasAsistente
	if json.Unmarshal(crudo, &g) != nil || validarVisitas(&g) != nil {
		return v
	}
	return g
}

var reHoraBloque = regexp.MustCompile(`^([01][0-9]|2[0-4]):[0-5][0-9]$`)

// validarVisitas revisa los ajustes con sus topes máximos (los valida el servidor, no solo el esquema).
func validarVisitas(v *VisitasAsistente) error {
	if len(v.TiposPermitidos) == 0 || len(v.TiposPermitidos) > len(TiposVisitaAsistente) {
		return campoConv("visitas.tiposPermitidos", "Uno o dos tipos: visita, mantenimiento", "rango")
	}
	for i, t := range v.TiposPermitidos {
		if !slices.Contains(TiposVisitaAsistente, t) || slices.Index(v.TiposPermitidos, t) != i {
			return campoConv(fmt.Sprintf("visitas.tiposPermitidos[%d]", i), "visita o mantenimiento, sin repetir", "enum")
		}
	}
	rangos := []struct {
		campo    string
		v        int32
		min, max int32
	}{{"maxAbiertasPorCliente", v.MaxAbiertasPorCliente, 1, 5}, {"anticipacionHoras", v.AnticipacionHoras, 0, 168}, {"diasAdelante", v.DiasAdelante, 1, 30},
		{"ofertasPorDia", v.OfertasPorDia, 1, 10}, {"visitasPorDia", v.VisitasPorDia, 1, 5}, {"visitasPorDiaEmpresa", v.VisitasPorDiaEmpresa, 1, 500}}
	for _, r := range rangos {
		if r.v < r.min || r.v > r.max {
			return campoConv("visitas."+r.campo, fmt.Sprintf("Entre %d y %d", r.min, r.max), "rango")
		}
	}
	if len(v.Bloques) == 0 || len(v.Bloques) > 6 {
		return campoConv("visitas.bloques", "Entre 1 y 6 bloques", "rango")
	}
	for i, b := range v.Bloques {
		if !reHoraBloque.MatchString(b.Inicio) || !reHoraBloque.MatchString(b.Fin) || b.Fin <= b.Inicio || b.Fin > "24:00" {
			return campoConv(fmt.Sprintf("visitas.bloques[%d]", i), "Horas HH:MM, con el fin después del inicio", "hora")
		}
		if i > 0 && b.Inicio < v.Bloques[i-1].Fin {
			return campoConv(fmt.Sprintf("visitas.bloques[%d]", i), "Los bloques van en orden y sin cruzarse", "orden")
		}
	}
	return nil
}

// ctxVisitas: el contexto del asistente con los permisos de campo que necesita para agendar. Solo lo
// arman las herramientas de visita, y solo con la acción agendar_visita encendida en el perfil
// vigente: fuera de ahí el actor asistente_ia no tiene ordenes.crear ni ordenes.programar.
func ctxVisitas(ctx context.Context, empresa int64) context.Context {
	mapa := map[string][]contexto.Alcance{}
	for _, p := range append(nucleo.PermisosAsistenteIA(), nucleo.PermisosAsistenteVisitas()...) {
		mapa[string(p)] = []contexto.Alcance{{}}
	}
	return contexto.ComoAsistenteIA(ctx, empresa, contexto.NuevosPermisos(mapa))
}

// visitaPreparada: lo que las dos herramientas validan antes de tocar la agenda.
type visitaPreparada struct {
	cfg      VisitasAsistente
	tipo     string
	servicio int64
	zona     int64
	// pendiente: la orden que el asistente ya creó para este cliente y quedó sin programar.
	pendiente *campo.OrdenAbierta
}

// diasLargos y mesesLargos: la fecha como se le dice al cliente (hora de Guatemala).
var (
	diasLargos  = []string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"}
	mesesLargos = []string{"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"}
)

// textoVentana: «jueves 6 de noviembre, entre las 08:00 y las 12:00».
func textoVentana(ini, fin time.Time, loc *time.Location) string {
	i, f := ini.In(loc), fin.In(loc)
	return fmt.Sprintf("%s %d de %s, entre las %s y las %s", diasLargos[i.Weekday()], i.Day(), mesesLargos[i.Month()-1], i.Format("15:04"), f.Format("15:04"))
}

// prepararVisita aplica las reglas comunes: cliente asociado y verificado (número, o fuerte si la
// empresa lo exige), servicio identificado y suyo, servicio activo (o en mora si la empresa lo
// permite), zona, y el tope de visitas abiertas. Devuelve un resultado de herramienta si no procede.
func (m *Modulo) prepararVisita(ctx context.Context, t *turnoIA, servicioPedido int64) (visitaPreparada, *resHerramienta) {
	no := func(r resHerramienta) (visitaPreparada, *resHerramienta) { return visitaPreparada{}, &r }
	if m.cfg.Campo == nil || m.cfg.Campo.Agenda() == nil {
		return no(rechazo("agendar visitas no está disponible. Pasa a una persona."))
	}
	if t.clienteID == 0 || !t.verificado {
		return no(rechazo("la conversación no tiene un cliente identificado. Pasa a una persona."))
	}
	if t.visitaFuerte && !t.fuerte {
		return no(rechazo("la identidad del cliente no está verificada: agendar una visita exige verificarla (verificar_identidad) o una persona."))
	}
	q := mensajeriabd.New(nucleo.BDDe(ctx))
	svs, err := q.ServiciosParaVisita(ctx, mensajeriabd.ServiciosParaVisitaParams{ClienteID: t.clienteID, Empresa: t.empresa})
	if err != nil {
		return no(resHerramienta{fallo: true})
	}
	servicio := servicioPedido
	if servicio == 0 && t.servicioID != nil {
		servicio = *t.servicioID
	}
	if servicio == 0 && len(svs) == 1 {
		servicio = svs[0].ID
	}
	if servicio == 0 {
		if len(svs) == 0 {
			return no(resHerramienta{paso: PasoSinRespuesta, resumen: "Pide una visita técnica, pero no tiene servicios en esta empresa."})
		}
		return no(rechazo("el cliente tiene varios servicios: pregúntale para cuál es la visita y vuelve a llamar con su servicio_id (de estado_del_servicio)."))
	}
	i := slices.IndexFunc(svs, func(s mensajeriabd.ServiciosParaVisitaRow) bool { return s.ID == servicio })
	if i < 0 {
		return no(rechazo("ese servicio no es del cliente de esta conversación."))
	}
	cfg := t.visitas
	switch estado := strings.ToLower(strings.TrimSpace(svs[i].Estado)); {
	case estado == "activo":
	case estado == "cortado" && cfg.PermitirConMora:
	case estado == "cortado":
		return no(resHerramienta{paso: PasoSinRespuesta, resumen: "Pide una visita técnica con el servicio cortado por falta de pago (la empresa no agenda visitas con mora)."})
	default:
		return no(resHerramienta{paso: PasoSinRespuesta, resumen: "Pide una visita técnica para un servicio que no está activo."})
	}
	if svs[i].ZonaID == nil || *svs[i].ZonaID <= 0 {
		return no(resHerramienta{paso: PasoSinRespuesta, resumen: "Pide una visita técnica, pero su servicio no tiene zona: la agenda no sabe quién lo atiende."})
	}
	p := visitaPreparada{cfg: cfg, tipo: cfg.TiposPermitidos[0], servicio: servicio, zona: *svs[i].ZonaID}
	abiertas, err := campo.OrdenesAbiertasDeCliente(ctx, nucleo.BDDe(ctx), empresaPtrDe(t.empresa), t.clienteID, TiposVisitaAsistente)
	if err != nil {
		return no(resHerramienta{fallo: true})
	}
	vivas := int32(0)
	for _, o := range abiertas {
		// La que el propio asistente creó y quedó sin programar no cuenta: es la que se va a programar.
		if o.Origen == campo.OrigenAsistente && o.Estado == campo.EstadoPendiente && o.ServicioID != nil && *o.ServicioID == servicio && p.pendiente == nil {
			p.pendiente = &o
			continue
		}
		vivas++
	}
	if vivas >= cfg.MaxAbiertasPorCliente {
		return no(resHerramienta{paso: PasoSinRespuesta, resumen: fmt.Sprintf("Pide una visita técnica y ya tiene %d abierta(s): %s. Moverla o agregar otra lo decide una persona.", vivas, abiertas[0].Numero)})
	}
	return p, nil
}

// ofrecerVentanas busca las ventanas, las guarda como ofertas de la conversación y arma el texto
// para el modelo. Sin ventanas (o zona sin cobertura), pasa a una persona.
func (m *Modulo) ofrecerVentanas(ctx context.Context, t *turnoIA, p visitaPreparada, motivo string) resHerramienta {
	loc := m.cfg.Zona
	ahora := m.ahora()
	dias := min(p.cfg.DiasAdelante, t.limites.DiasAdelanteMax)
	excluir := int64(0)
	if p.pendiente != nil {
		excluir = p.pendiente.ID
	}
	hs, err := m.cfg.Campo.Agenda().HuecosOfrecibles(ctx, nucleo.BDDe(ctx), agenda.EntradaHuecos{EmpresaID: t.empresa, ZonaID: p.zona, Tipo: p.tipo,
		Desde: ahora, Hasta: ahora.In(loc).AddDate(0, 0, int(dias)), NoAntesDe: ahora.Add(time.Duration(p.cfg.AnticipacionHoras) * time.Hour),
		Bloques: bloquesDeAgenda(p.cfg.Bloques), Max: 3, ExcluirOrden: excluir})
	if err != nil {
		return resHerramienta{fallo: true}
	}
	if hs.SinCobertura {
		return resHerramienta{paso: PasoSinRespuesta, resumen: "Pide una visita técnica, pero ningún técnico de la agenda atiende la zona de su servicio."}
	}
	if len(hs.Data) == 0 {
		return resHerramienta{paso: PasoSinRespuesta, resumen: fmt.Sprintf("Pide una visita técnica y no hay espacio en la agenda en los próximos %d días.", dias)}
	}
	q := mensajeriabd.New(nucleo.BDDe(ctx))
	if err := q.BorrarOfertasVisita(ctx, t.conv); err != nil {
		return resHerramienta{fallo: true}
	}
	var b strings.Builder
	b.WriteString("Ventanas disponibles para la visita (hora de Guatemala). Ofréceselas al cliente tal cual y pregúntale cuál prefiere:\n")
	for i, h := range hs.Data {
		clave := "V" + strconv.Itoa(i+1)
		if err := q.InsertarOfertaVisita(ctx, mensajeriabd.InsertarOfertaVisitaParams{EmpresaID: empresaPtrDe(t.empresa), ConversacionID: t.conv, ClienteID: t.clienteID,
			ServicioID: p.servicio, ZonaID: p.zona, Tipo: p.tipo, Clave: clave, Inicio: h.Inicio, Fin: h.Fin, Motivo: motivo, OfrecidaEn: ahora,
			VenceEn: ahora.Add(VigenciaOfertaVisita)}); err != nil {
			return resHerramienta{fallo: true}
		}
		b.WriteString(clave + ": " + textoVentana(h.Inicio, h.Fin, loc) + "\n")
	}
	b.WriteString("Cuando el cliente elija, llama a agendar_visita con esa opción. No menciones técnicos ni la dirección: la visita es en la dirección registrada de su servicio.")
	return resHerramienta{contenido: b.String()}
}

func bloquesDeAgenda(bs []BloqueVisita) []agenda.BloqueHora {
	out := make([]agenda.BloqueHora, 0, len(bs))
	for _, b := range bs {
		out = append(out, agenda.BloqueHora{Inicio: b.Inicio, Fin: b.Fin})
	}
	return out
}

// herrOfrecerVisita propone ventanas para una visita técnica.
func (m *Modulo) herrOfrecerVisita(ctx context.Context, t *turnoIA, l LlamadaIA, h accionHecha) (resHerramienta, accionHecha) {
	h.Accion = HerrOfrecerVisita
	var a struct {
		ServicioID int64  `json:"servicio_id"`
		Motivo     string `json:"motivo"`
	}
	if args(l.Argumentos, &a) != nil || a.ServicioID < 0 {
		return rechazo("argumentos no válidos (servicio_id y motivo)."), h
	}
	motivo := strings.TrimSpace(reEspacios.ReplaceAllString(RedactarParaIA(a.Motivo), " "))
	if len([]rune(motivo)) < 3 || len([]rune(motivo)) > 200 {
		return rechazo("el motivo debe tener entre 3 y 200 caracteres."), h
	}
	p, no := m.prepararVisita(ctx, t, a.ServicioID)
	if no != nil {
		return *no, h
	}
	q := mensajeriabd.New(nucleo.BDDe(ctx))
	n, err := q.AccionesAsistenteRecientes(ctx, mensajeriabd.AccionesAsistenteRecientesParams{ConversacionID: t.conv, Accion: HerrOfrecerVisita, Desde: m.ahora().Add(-24 * time.Hour)})
	if err != nil {
		return resHerramienta{fallo: true}, h
	}
	if n >= int64(p.cfg.OfertasPorDia) {
		return resHerramienta{paso: PasoSinRespuesta, resumen: "Pidió ventanas para una visita varias veces hoy sin agendar."}, h
	}
	return m.ofrecerVentanas(ctx, t, p, motivo), h
}

// herrAgendarVisita crea la orden de visita y la programa en la ventana elegida.
func (m *Modulo) herrAgendarVisita(ctx context.Context, t *turnoIA, l LlamadaIA, h accionHecha) (resHerramienta, accionHecha) {
	var a struct {
		Opcion string `json:"opcion"`
	}
	if args(l.Argumentos, &a) != nil {
		return rechazo("argumentos no válidos (opcion: V1, V2 o V3)."), h
	}
	if t.hechas[HerrAgendarVisita] {
		return rechazo("ya agendaste una visita en este turno."), h
	}
	q := mensajeriabd.New(nucleo.BDDe(ctx))
	ahora := m.ahora()
	of, err := q.OfertaVisita(ctx, mensajeriabd.OfertaVisitaParams{ConversacionID: t.conv, Clave: strings.ToUpper(strings.TrimSpace(a.Opcion)), Ahora: ahora})
	if errors.Is(err, pgx.ErrNoRows) {
		return rechazo("esa opción no es una ventana ofrecida y vigente en esta conversación. Usa ofrecer_visita y deja que el cliente elija."), h
	}
	if err != nil {
		return resHerramienta{fallo: true}, h
	}
	// Las reglas otra vez (el cliente, el servicio o los ajustes pudieron cambiar desde la oferta).
	p, no := m.prepararVisita(ctx, t, of.ServicioID)
	if no != nil {
		return *no, h
	}
	if of.ClienteID != t.clienteID || derefEmpresa(of.EmpresaID) != t.empresa || p.zona != of.ZonaID || !slices.Contains(p.cfg.TiposPermitidos, of.Tipo) {
		return rechazo("esa ventana ya no vale para este cliente. Usa ofrecer_visita otra vez."), h
	}
	desde := ahora.Add(-24 * time.Hour)
	if n, err := q.AccionesAsistenteRecientes(ctx, mensajeriabd.AccionesAsistenteRecientesParams{ConversacionID: t.conv, Accion: HerrAgendarVisita, Desde: desde}); err != nil {
		return resHerramienta{fallo: true}, h
	} else if n >= int64(p.cfg.VisitasPorDia) {
		return resHerramienta{paso: PasoSinRespuesta, resumen: "Ya agendó una visita hoy en esta conversación y pide otra."}, h
	}
	if n, err := q.AccionesAsistenteEmpresa(ctx, mensajeriabd.AccionesAsistenteEmpresaParams{Empresa: t.empresa, Accion: HerrAgendarVisita, Desde: desde}); err != nil {
		return resHerramienta{fallo: true}, h
	} else if n >= int64(p.cfg.VisitasPorDiaEmpresa) {
		return resHerramienta{paso: PasoTope, resumen: "Se llegó al tope de visitas que el asistente agenda por día en la empresa."}, h
	}
	ctxV := ctxVisitas(ctx, t.empresa)
	// La orden: la que ya creó y quedó sin programar, o una nueva (con su ticket si la empresa lo exige).
	ordenID, numero, version := int64(0), "", int32(0)
	if p.pendiente != nil {
		ordenID, numero, version = p.pendiente.ID, p.pendiente.Numero, p.pendiente.Version
	} else {
		e := EntradaOrdenConv{ID: t.conv, Tipo: of.Tipo, ServicioID: &of.ServicioID, Clave: "iris:asistente:" + t.id.String() + ":visita",
			Descripcion: "Visita agendada por el asistente de IA desde WhatsApp. Motivo: " + recortar(of.Motivo, 200) +
				". Si aparece sin programar, la ventana que eligió el cliente ya no tenía espacio: prográmala desde despacho."}
		if p.cfg.ExigeTicket {
			ticket, res := m.ticketParaVisita(ctx, t, of)
			if res != nil {
				return *res, h
			}
			e.TicketID = &ticket
		}
		v, err := m.Conv.CrearOrden.Ejecutar(ctxV, e)
		if err != nil {
			return deError(err), h
		}
		ordenID, _ = strconv.ParseInt(v.Vinculo.ID, 10, 64)
		numero, version = v.Vinculo.Referencia, 1
	}
	h.Entidad, h.EntidadID = "orden", strconv.FormatInt(ordenID, 10)
	// El técnico lo elige la agenda; el asistente (y el cliente) nunca lo ven.
	entrada := agenda.EntradaHuecos{EmpresaID: t.empresa, ZonaID: of.ZonaID, Tipo: of.Tipo, ExcluirOrden: ordenID}
	elegido, err := m.cfg.Campo.Agenda().ElegirTecnicoEnVentana(ctx, nucleo.BDDe(ctx), entrada, agenda.Hueco{Inicio: of.Inicio, Fin: of.Fin})
	if err == nil {
		_, err = m.cfg.Campo.ProgramarOrden.Ejecutar(ctxV, campo.EntradaProgramarOrden{ID: ordenID, Version: version, TecnicoID: &elegido.UsuarioID,
			Inicio: elegido.Inicio, Fin: &elegido.Fin, VentanaInicio: &of.Inicio, VentanaFin: &of.Fin})
	}
	if err != nil {
		if _, negocio := esDeNegocio(err); !negocio {
			// Ni «no encontrada» es aquí «una persona presente»: la orden ya existe; es una falla.
			slog.Warn("asistente: no se pudo programar la visita", "tipo_error", fmt.Sprintf("%T", err))
			return resHerramienta{fallo: true}, h
		}
		// La ventana ya no tiene espacio: la orden queda creada (sin programar, a la vista de
		// despacho) y se ofrecen ventanas nuevas; el próximo agendar_visita programa esa misma orden.
		p.pendiente = &campo.OrdenAbierta{ID: ordenID, Numero: numero}
		res := m.ofrecerVentanas(ctx, t, p, of.Motivo)
		if res.paso != "" {
			res.resumen += " La orden " + numero + " quedó creada SIN PROGRAMAR: prográmala desde despacho."
			return res, h
		}
		res.contenido = "No se hizo: esa ventana ya no tiene espacio. " + res.contenido
		res.rechazada = true
		return res, h
	}
	if err := q.BorrarOfertasVisita(ctx, t.conv); err != nil {
		return resHerramienta{fallo: true}, h
	}
	h.actividadTipo, h.actividadTexto = "visita_agendada", "Agendó la visita "+numero
	t.permitir(ResRegistrar, ResConfirmar)
	return resHerramienta{contenido: "Visita agendada: orden " + numero + ", " + textoVentana(of.Inicio, of.Fin, m.cfg.Zona) +
		" (hora de Guatemala). Confírmaselo al cliente con el día, la ventana y el número de orden. No menciones técnicos ni la dirección."}, h
}

// ticketParaVisita: el ticket del que nace la visita. El último que se creó desde esta conversación
// en este periodo; si no hay, el asistente crea uno (con la clave del turno: un reintento da el mismo).
func (m *Modulo) ticketParaVisita(ctx context.Context, t *turnoIA, of mensajeriabd.MensajeriaAsistenteOfertasVisitum) (int64, *resHerramienta) {
	q := mensajeriabd.New(nucleo.BDDe(ctx))
	id, err := q.UltimoTicketDeConv(ctx, t.conv)
	if err == nil {
		if n, errN := strconv.ParseInt(id, 10, 64); errN == nil && n > 0 {
			return n, nil
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return 0, &resHerramienta{fallo: true}
	}
	v, err := m.Conv.CrearTicket.Ejecutar(ctx, EntradaTicketConv{ID: t.conv, Asunto: recortar("Visita técnica: "+of.Motivo, 120),
		Descripcion: "El cliente pidió una visita técnica por WhatsApp. Motivo: " + recortar(of.Motivo, 200), Prioridad: "media",
		ServicioID: &of.ServicioID, Clave: "iris:asistente:" + t.id.String() + ":ticket-visita"})
	if err != nil {
		r := deError(err)
		return 0, &r
	}
	n, _ := strconv.ParseInt(v.Vinculo.ID, 10, 64)
	return n, nil
}
