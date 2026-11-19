package mensajeria

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Herramientas del asistente autónomo (contratos/conversaciones.md §17.8). La lista es cerrada y se
// arma en el servidor con las acciones encendidas del turno; cada herramienta valida sus argumentos
// (sin propiedades de más: el modelo nunca nombra cliente, empresa ni conversación) y corre un caso
// de uso de la bandeja con el actor asistente_ia. Ninguna recibe ni devuelve un monto de deuda.

// Nombres de las herramientas.
const (
	HerrBuscarManual    = "buscar_manual"
	HerrEstadoServicio  = "estado_del_servicio"
	HerrConsultarSaldo  = "consultar_saldo"
	HerrCrearTicket     = "crear_ticket"
	HerrSimularPromesa  = "simular_promesa"
	HerrPedirPromesa    = "pedir_promesa"
	HerrCuentasBoleta   = "cuentas_para_boleta"
	HerrRegistrarBoleta = "registrar_boleta"
	HerrPedirReconexion = "pedir_reconexion"
	HerrPasarAPersona   = "pasar_a_persona"
	HerrVerificarIdent  = "verificar_identidad"
)

// herramientasDeLectura: las que no cambian nada (las únicas, con pasar_a_persona, del respaldo en JSON).
var herramientasDeLectura = map[string]bool{HerrBuscarManual: true, HerrEstadoServicio: true, HerrConsultarSaldo: true, HerrSimularPromesa: true,
	HerrCuentasBoleta: true, HerrPasarAPersona: true}

// ArgosIdentidad es el desafío de identidad de Argos (contrato con Argos §7.5): Argos compara el dato
// y solo dice si coincide. Opcional: sin él no hay herramienta verificar_identidad.
type ArgosIdentidad interface {
	VerificarIdentidad(ctx context.Context, a argos.AtencionIris, clienteID int64, metodo, valor string) (bool, error)
}

// accionDeHerramienta: la acción del perfil (§17.1) que enciende cada herramienta.
var accionDeHerramienta = map[string]string{
	HerrBuscarManual: "responder_dudas", HerrEstadoServicio: "avisar_fallas", HerrConsultarSaldo: "dar_saldo", HerrCrearTicket: "crear_ticket",
	HerrSimularPromesa: "promesa_pago", HerrPedirPromesa: "promesa_pago", HerrCuentasBoleta: "recibir_boleta", HerrRegistrarBoleta: "recibir_boleta",
	HerrPedirReconexion: "reconexion_temporal", HerrPasarAPersona: "pasar_a_persona", HerrVerificarIdent: HerrVerificarIdent,
	HerrOfrecerVisita: "agendar_visita", HerrAgendarVisita: "agendar_visita",
}

// ArgosReconexion es la reconexión temporal de Argos (contrato con Argos §7.3): una promesa ligada
// a un comprobante pendiente. Es opcional: si el cliente de Argos del proceso no la implementa, la
// herramienta pedir_reconexion no existe para el modelo.
type ArgosReconexion interface {
	PedirReconexionTemporal(ctx context.Context, a argos.AtencionIris, comprobanteID int64, servicioID *int64, motivo, clave string) (promesaID int64, hasta time.Time, err error)
}

func pTexto(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

func pEntero(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

func pOpcion(desc string, valores ...string) map[string]any {
	return map[string]any{"type": "string", "enum": valores, "description": desc}
}

// herramientasDe arma la lista cerrada del turno, en orden estable: solo las acciones encendidas
// (perfil vigente − apagadas de la cola) y solo lo que el servidor puede cumplir (Argos conectado,
// cliente asociado). pasar_a_persona va siempre.
func (m *Modulo) herramientasDe(t *turnoIA) []HerramientaIA {
	var hs []HerramientaIA
	si := func(accion string, h HerramientaIA) {
		if t.acciones[accion] {
			hs = append(hs, h)
		}
	}
	conArgos := m.cfg.Argos != nil && t.clienteID != 0
	si("responder_dudas", HerramientaIA{Nombre: HerrBuscarManual, Descripcion: "Busca en los manuales de la empresa. Úsala cuando <datos> no alcance para responder una duda.",
		Parametros: map[string]any{"consulta": pTexto("Palabras clave de lo que pregunta el cliente")}})
	if t.clienteID != 0 {
		si("avisar_fallas", HerramientaIA{Nombre: HerrEstadoServicio, Descripcion: "Devuelve los servicios del cliente y si hay una falla de red abierta que lo afecte."})
	}
	if conArgos {
		// El desafío de identidad: solo si alguna acción que lo exige está encendida.
		if _, ok := m.cfg.Argos.(ArgosIdentidad); ok && !t.fuerte && (t.acciones["dar_saldo"] || t.acciones["promesa_pago"] || t.acciones["reconexion_temporal"] ||
			(t.acciones["agendar_visita"] && t.visitaFuerte)) {
			t.acciones[HerrVerificarIdent] = true
			hs = append(hs, HerramientaIA{Nombre: HerrVerificarIdent,
				Descripcion: "Verifica la identidad del cliente con su DPI o NIT completo. Primero pídeselo; cuando lo haya escrito en su último mensaje, llama a esta herramienta (sin argumentos: el sistema toma el dato del mensaje; tú lo ves como ***). Nunca repitas el dato."})
		}
		si("dar_saldo", HerramientaIA{Nombre: HerrConsultarSaldo,
			Descripcion: "Consulta el estado de cuenta del cliente. Exige la identidad verificada. No devuelve el monto: escribe {{saldo}} donde va y el sistema lo reemplaza."})
	}
	si("crear_ticket", HerramientaIA{Nombre: HerrCrearTicket, Descripcion: "Crea un ticket de soporte para el cliente de esta conversación. Dile el número de ticket que devuelve.",
		Parametros: map[string]any{"asunto": pTexto("Asunto corto del problema (3 a 120 caracteres)"), "descripcion": pTexto("Qué reporta el cliente, sin contraseñas ni documentos"),
			"prioridad": pOpcion("Prioridad del ticket", "baja", "media", "alta", "critica")}})
	if conArgos {
		si("promesa_pago", HerramientaIA{Nombre: HerrSimularPromesa, Descripcion: "Pregunta al sistema de facturación si el cliente puede tener una promesa de pago para ese día. No crea nada.",
			Parametros: map[string]any{"servicio_id": pEntero("Id de un servicio del cliente (de <datos> o de estado_del_servicio)"), "fecha": pTexto("Día en que promete pagar, AAAA-MM-DD")}})
		si("promesa_pago", HerramientaIA{Nombre: HerrPedirPromesa, Descripcion: "Pide la promesa de pago al sistema de facturación, que decide. Úsala solo si el cliente confirmó el día.",
			Parametros: map[string]any{"servicio_id": pEntero("Id de un servicio del cliente"), "fecha": pTexto("Día en que promete pagar, AAAA-MM-DD")}})
		si("recibir_boleta", HerramientaIA{Nombre: HerrCuentasBoleta, Descripcion: "Lista las cuentas bancarias de la empresa y los bancos de origen, para registrar una boleta."})
		si("recibir_boleta", HerramientaIA{Nombre: HerrRegistrarBoleta,
			Descripcion: "Registra la boleta de pago que mandó el cliente (su última foto o PDF) para que caja la revise. No aprueba el pago. Pide al cliente los datos que falten.",
			Parametros: map[string]any{"metodo": pOpcion("Cómo pagó", "deposito", "transferencia", "ach", "remesa"),
				"cuenta_bancaria_id": pEntero("Id de la cuenta de la empresa (de cuentas_para_boleta)"),
				"banco_origen_id":    pEntero("Id del banco de origen (de cuentas_para_boleta); 0 si es depósito o remesa"),
				"referencia":         pTexto("Número de boleta o de transacción"), "monto": pTexto("Monto que dice el cliente, con 2 decimales (ej. 125.50)"),
				"fecha_deposito": pTexto("Fecha del depósito, AAAA-MM-DD")}})
		if _, ok := m.cfg.Argos.(ArgosReconexion); ok {
			si("reconexion_temporal", HerramientaIA{Nombre: HerrPedirReconexion,
				Descripcion: "Pide al sistema de facturación una reconexión temporal mientras caja revisa la boleta ya registrada. El sistema decide."})
		}
	}
	if t.clienteID != 0 && m.cfg.Campo != nil {
		si("agendar_visita", HerramientaIA{Nombre: HerrOfrecerVisita,
			Descripcion: "Busca ventanas libres para una visita técnica en el servicio del cliente y te las devuelve (V1, V2, V3) para que se las ofrezcas. No agenda nada.",
			Parametros: map[string]any{"servicio_id": pEntero("Id del servicio del cliente (de estado_del_servicio); 0 = el de la conversación"),
				"motivo": pTexto("Para qué es la visita, en pocas palabras, sin datos personales")}})
		si("agendar_visita", HerramientaIA{Nombre: HerrAgendarVisita,
			Descripcion: "Agenda la visita técnica en la ventana que eligió el cliente, de las que devolvió ofrecer_visita. Devuelve el número de orden.",
			Parametros:  map[string]any{"opcion": pOpcion("La ventana que eligió el cliente", "V1", "V2", "V3")}})
	}
	hs = append(hs, HerramientaIA{Nombre: HerrPasarAPersona, Descripcion: "Pasa la conversación a una persona del equipo. Termina tu turno.",
		Parametros: map[string]any{"motivo": pOpcion("Por qué", PasoClientePidio, PasoEnojo, PasoSinRespuesta),
			"resumen": pTexto("Resumen breve para el agente: qué pidió el cliente y qué falta")}})
	return hs
}

// resHerramienta es el resultado de una llamada.
type resHerramienta struct {
	// contenido: lo que se le devuelve al modelo.
	contenido string
	// rechazada: el servidor no la ejecutó (argumentos, límites, verificación, acción apagada).
	rechazada bool
	// fallo: se intentó y falló (el turno pasa a una persona).
	fallo bool
	// persona: una persona tomó la conversación o escribió (el turno termina sin enviar).
	persona bool
	// paso: el modelo pidió pasar a una persona (motivo) con su resumen.
	paso, resumen string
	// sinAnotar: un reintento de algo que ya quedó anotado (no se anota ni se cuenta otra vez).
	sinAnotar bool
}

func rechazo(detalle string) resHerramienta {
	return resHerramienta{contenido: "No se hizo: " + detalle, rechazada: true}
}

// args decodifica los argumentos sin aceptar propiedades de más.
func args(crudo json.RawMessage, destino any) error {
	if len(bytes.TrimSpace(crudo)) == 0 {
		crudo = json.RawMessage("{}")
	}
	d := json.NewDecoder(bytes.NewReader(crudo))
	d.DisallowUnknownFields()
	return d.Decode(destino)
}

func esNoEncontrado(err error) bool {
	var p *nucleo.Problema
	return errors.As(err, &p) && p.Status == http.StatusNotFound
}

func esDeNegocio(err error) (string, bool) {
	var p *nucleo.Problema
	if errors.As(err, &p) && (p.Status == http.StatusUnprocessableEntity || p.Status == http.StatusConflict || p.Status == http.StatusBadRequest) {
		return p.Detail, true
	}
	return "", false
}

var reFechaISO = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// ejecutarHerramienta valida y ejecuta una llamada del modelo. Lo que no está en la lista del turno
// (una herramienta que no existe, o una acción apagada que un mensaje del cliente «pidió» encender)
// se rechaza sin ejecutar nada.
func (m *Modulo) ejecutarHerramienta(ctx context.Context, t *turnoIA, l LlamadaIA) resHerramienta {
	accion, existe := accionDeHerramienta[l.Nombre]
	ofrecida := slices.ContainsFunc(t.pedido.Herramientas, func(h HerramientaIA) bool { return h.Nombre == l.Nombre })
	if !existe || !ofrecida || !t.acciones[accion] {
		m.registrarAccion(ctx, t, accionHecha{Accion: "herramienta_no_permitida", Resultado: "rechazada"})
		return rechazo("esa herramienta no existe o no está permitida en esta conversación.")
	}
	// La conversación sigue siendo del asistente (nadie la tomó ni escribió, misma empresa).
	l0, err := m.Asistente.leerConv.Ejecutar(ctx, EntradaConversacion{ID: t.conv})
	if err != nil {
		if esNoEncontrado(err) {
			return resHerramienta{persona: true}
		}
		return resHerramienta{fallo: true}
	}
	c := l0.conv
	// El cliente de la conversación no puede haber cambiado desde que se armó el turno.
	var clienteAhora int64
	if c.ClienteID != nil {
		clienteAhora = *c.ClienteID
	}
	nivel := verificacionDe(c, m.ahora(), t.vigencia)
	if clienteAhora != t.clienteID || (nivel != "") != t.verificado {
		return resHerramienta{fallo: true}
	}
	// El nivel se recalcula en cada herramienta: si la verificación fuerte venció a mitad del turno,
	// las acciones de dinero que siguen ya no la tienen.
	t.fuerte = nivel == VerificacionFuerte
	var res resHerramienta
	hecha := accionHecha{Accion: accion}
	switch l.Nombre {
	case HerrPasarAPersona:
		var a struct {
			Motivo  string `json:"motivo"`
			Resumen string `json:"resumen"`
		}
		if args(l.Argumentos, &a) != nil || !slices.Contains([]string{PasoClientePidio, PasoEnojo, PasoSinRespuesta}, a.Motivo) {
			a.Motivo = PasoSinRespuesta
		}
		// La acción y el evento los deja terminarTurno (asistente.paso_a_persona).
		return resHerramienta{paso: a.Motivo, resumen: a.Resumen}
	case HerrBuscarManual:
		res, hecha = m.herrBuscarManual(ctx, t, l, hecha)
	case HerrEstadoServicio:
		res, hecha = m.herrEstadoServicio(ctx, t, l, hecha)
	case HerrConsultarSaldo:
		res, hecha = m.herrConsultarSaldo(ctx, t, l, hecha)
	case HerrCrearTicket:
		res, hecha = m.herrCrearTicket(ctx, t, l, hecha)
	case HerrSimularPromesa, HerrPedirPromesa:
		res, hecha = m.herrPromesa(ctx, t, l, hecha)
	case HerrCuentasBoleta:
		res, hecha = m.herrCuentasBoleta(ctx, t, l, hecha)
	case HerrRegistrarBoleta:
		res, hecha = m.herrRegistrarBoleta(ctx, t, l, hecha)
	case HerrPedirReconexion:
		res, hecha = m.herrReconexion(ctx, t, l, hecha)
	case HerrVerificarIdent:
		res, hecha = m.herrVerificarIdentidad(ctx, t, l, hecha)
	case HerrOfrecerVisita:
		res, hecha = m.herrOfrecerVisita(ctx, t, l, hecha)
	case HerrAgendarVisita:
		res, hecha = m.herrAgendarVisita(ctx, t, l, hecha)
	}
	if res.paso != "" {
		m.registrarAccion(ctx, t, accionHecha{Accion: hecha.Accion, Resultado: "fallo"})
		return res
	}
	if res.persona {
		return res
	}
	if res.sinAnotar {
		t.hechas[hecha.Accion] = true
		return res
	}
	hecha.Resultado = "ok"
	switch {
	case res.fallo:
		hecha.Resultado = "fallo"
	case res.rechazada:
		hecha.Resultado = "rechazada"
	}
	if !m.registrarAccion(ctx, t, hecha) && hecha.Accion == HerrVerificarIdent && res.rechazada {
		// Los rechazos de verificar_identidad se cuentan con este registro (a la segunda pasa a una
		// persona): si no quedó anotado, el turno se corta aquí en vez de repetirse sin registro.
		return resHerramienta{paso: PasoHerramientaFallo, resumen: "No se pudo registrar el intento de verificación de identidad."}
	}
	return res
}

// deError traduce el error de un caso de uso: una persona presente, una regla de negocio (el modelo
// la puede explicar) o una falla.
func deError(err error) resHerramienta {
	if esNoEncontrado(err) {
		return resHerramienta{persona: true}
	}
	if d, ok := esDeNegocio(err); ok {
		return rechazo(d)
	}
	// Solo el tipo del problema (los detalles pueden nombrar datos del cliente).
	var p *nucleo.Problema
	if errors.As(err, &p) {
		slog.Warn("asistente: una herramienta falló", "tipo", p.Type, "status", p.Status)
	} else {
		// Nunca el error crudo (puede traer datos del cliente o de la consulta): solo su tipo.
		slog.Warn("asistente: una herramienta falló", "tipo_error", fmt.Sprintf("%T", err))
	}
	return resHerramienta{fallo: true}
}

func (m *Modulo) herrBuscarManual(ctx context.Context, t *turnoIA, l LlamadaIA, h accionHecha) (resHerramienta, accionHecha) {
	var a struct {
		Consulta string `json:"consulta"`
	}
	if args(l.Argumentos, &a) != nil {
		return rechazo("argumentos no válidos."), h
	}
	consulta := consultaManuales(recortar(a.Consulta, 200))
	if consulta == "" {
		return rechazo("la consulta está vacía."), h
	}
	ms, err := mensajeriabd.New(nucleo.BDDe(ctx)).ManualesAsistente(ctx, mensajeriabd.ManualesAsistenteParams{Empresa: t.empresa, Consulta: consulta})
	if err != nil {
		return resHerramienta{fallo: true}, h
	}
	if len(ms) == 0 {
		return resHerramienta{contenido: "No hay manuales sobre eso."}, h
	}
	var b strings.Builder
	for _, mn := range ms {
		titulo := limpiarDato(mn.Titulo, 120)
		cuerpo := neutralizar(recortarIA(mn.Resumen+"\n"+mn.Cuerpo, 1500))
		if mn.Visibilidad == "cliente" {
			linea := "(manual, se puede compartir con el cliente) «" + titulo + "»: " + cuerpo
			t.enlaces = append(t.enlaces, reURL.FindAllString(linea, -1)...)
			b.WriteString(linea + "\n")
		} else {
			t.internos = append(t.internos, mn.Resumen+"\n"+mn.Cuerpo)
			// El cuerpo de un manual «Del equipo» no viaja al proveedor cuando el asistente atiende
			// solo (§17.9): solo se le dice que existe, para que pase la conversación a una persona.
			b.WriteString("(hay un manual interno del equipo sobre esto, «" + titulo + "»: no puedes leerlo ni mencionarlo; si hace falta, pasa a una persona)\n")
		}
	}
	h.actividadTipo, h.actividadTexto = "respondio_manual", "Respondió con el manual"
	return resHerramienta{contenido: b.String()}, h
}

func (m *Modulo) herrEstadoServicio(ctx context.Context, t *turnoIA, l LlamadaIA, h accionHecha) (resHerramienta, accionHecha) {
	var a struct{}
	if args(l.Argumentos, &a) != nil {
		return rechazo("esta herramienta no lleva argumentos."), h
	}
	q := mensajeriabd.New(nucleo.BDDe(ctx))
	svs, err := q.ServiciosClienteAsistente(ctx, mensajeriabd.ServiciosClienteAsistenteParams{ClienteID: t.clienteID, Empresa: t.empresa, ServicioID: t.servicioID})
	if err != nil {
		return resHerramienta{fallo: true}, h
	}
	fallas, err := q.FallasHorusCliente(ctx, mensajeriabd.FallasHorusClienteParams{ClienteID: t.clienteID, Empresa: t.empresa})
	if err != nil {
		return resHerramienta{fallo: true}, h
	}
	var b strings.Builder
	b.WriteString(describirServiciosIA(svs, t.numeroVerificado))
	if len(svs) == 0 {
		b.WriteString("El cliente no tiene servicios en esta empresa.\n")
	}
	zona := ""
	for _, f := range fallas {
		zona = limpiarDato(f.ZonaNombre, 60)
		// Nunca el nombre del router (contrato argosisp.md 0.8.0 §2.4): solo la zona.
		b.WriteString(fmt.Sprintf("Falla de red abierta: un equipo de red de %s está caído y afecta al cliente. No hay hora estimada de solución.\n", zonaFalla(zona)))
	}
	if len(fallas) == 0 {
		b.WriteString("No hay fallas de red abiertas que afecten al cliente.\n")
	} else {
		if zona == "" {
			zona = "su zona"
		}
		h.actividadTipo, h.actividadTexto = "respondio_falla", "Respondió: falla en "+zona
	}
	// El modelo puede decir «tu servicio está activo» solo si el servicio de la conversación vino
	// activo; sin servicio fijado (o si no está entre los leídos), si alguno lo está.
	if slices.ContainsFunc(svs, func(s mensajeriabd.ServiciosClienteAsistenteRow) bool {
		activo := strings.EqualFold(strings.TrimSpace(s.Estado), "activo")
		if t.servicioID != nil && slices.ContainsFunc(svs, func(o mensajeriabd.ServiciosClienteAsistenteRow) bool { return o.ID == *t.servicioID }) {
			return activo && s.ID == *t.servicioID
		}
		return activo
	}) {
		t.permitir(ResActivo)
	}
	return resHerramienta{contenido: b.String()}, h
}

// herrConsultarSaldo lee el estado de cuenta en vivo, solo de un cliente verificado. El monto no va
// al modelo, ni a logs, auditoría o eventos: queda en memoria para la marca {{saldo}} del mensaje.
func (m *Modulo) herrConsultarSaldo(ctx context.Context, t *turnoIA, l LlamadaIA, h accionHecha) (resHerramienta, accionHecha) {
	var a struct{}
	if args(l.Argumentos, &a) != nil {
		return rechazo("esta herramienta no lleva argumentos."), h
	}
	// El saldo exige la verificación fuerte (una persona la confirmó o pasó el desafío de Argos), salvo
	// que la empresa la haya bajado a «el número coincide» en el perfil.
	if !t.verificado || t.clienteID == 0 || (t.saldoFuerte && !t.fuerte) {
		return rechazo("la identidad del cliente no está verificada. No hables de su saldo ni de deudas; verifica su identidad (si tienes verificar_identidad) o pásalo con una persona."), h
	}
	cctx, cancelar := context.WithTimeout(ctx, PlazoCuentaIA)
	defer cancelar()
	e, err := m.cfg.Argos.EstadoCuentaCliente(cctx, argos.AtencionIris{Actor: ActorAsistenteIA, Conversacion: "iris:conversacion:" + strconv.FormatInt(t.conv, 10),
		EmpresaID: t.empresa}, t.clienteID)
	if err != nil {
		slog.InfoContext(ctx, "asistente: no se pudo leer el estado de cuenta", "conversacion_id", t.conv, "caido", conector.EsCaido(err))
		return resHerramienta{fallo: true}, h
	}
	k := cuentaDe(t.clienteID, e)
	if k == nil {
		return resHerramienta{fallo: true}, h
	}
	h.actividadTipo, h.actividadTexto = "saldo_informado", "Informó el saldo"
	if k.alDia {
		return resHerramienta{contenido: "El cliente está al día: no tiene facturas vencidas."}, h
	}
	monto := e.MontoVencido
	if !positivo(monto) {
		monto = e.Saldo
	}
	if positivo(monto) {
		v := strings.TrimSpace(*monto)
		t.saldo = &v
	}
	out := "El cliente tiene saldo vencido"
	if k.vencidas != nil && *k.vencidas > 0 {
		out += fmt.Sprintf(" (%d facturas vencidas)", *k.vencidas)
	}
	if t.saldo == nil {
		return resHerramienta{contenido: out + ". El monto no está disponible: no escribas ninguna cifra."}, h
	}
	return resHerramienta{contenido: out + ". Para decirle el monto escribe exactamente {{saldo}} (el sistema pone la cifra); no escribas ninguna cifra tú."}, h
}

var ordenPrioridad = []string{"baja", "media", "alta", "critica"}

func (m *Modulo) herrCrearTicket(ctx context.Context, t *turnoIA, l LlamadaIA, h accionHecha) (resHerramienta, accionHecha) {
	var a struct {
		Asunto      string `json:"asunto"`
		Descripcion string `json:"descripcion"`
		Prioridad   string `json:"prioridad"`
	}
	if args(l.Argumentos, &a) != nil {
		return rechazo("argumentos no válidos."), h
	}
	if t.hechas["crear_ticket"] {
		return rechazo("ya creaste un ticket en este turno."), h
	}
	if n, err := mensajeriabd.New(nucleo.BDDe(ctx)).TicketsAsistenteRecientes(ctx, mensajeriabd.TicketsAsistenteRecientesParams{ConversacionID: t.conv,
		Desde: m.ahora().Add(-24 * time.Hour)}); err != nil {
		return resHerramienta{fallo: true}, h
	} else if n >= TicketsPorDiaConv || (t.clienteID == 0 && n >= TicketsPorDiaSinCliente) {
		return rechazo("ya se crearon varios tickets hoy en esta conversación. Pasa a una persona si hace falta otro."), h
	}
	asunto := strings.TrimSpace(reEspacios.ReplaceAllString(RedactarParaIA(a.Asunto), " "))
	if len([]rune(asunto)) < 3 || len([]rune(asunto)) > 120 {
		return rechazo("el asunto debe tener entre 3 y 120 caracteres."), h
	}
	pr := slices.Index(ordenPrioridad, a.Prioridad)
	if pr < 0 {
		return rechazo("la prioridad debe ser baja, media, alta o critica."), h
	}
	if pr > slices.Index(ordenPrioridad, t.limites.PrioridadMaxima) {
		return rechazo("la prioridad máxima que puedes poner es " + t.limites.PrioridadMaxima + ". Usa esa o una menor, o pasa a una persona."), h
	}
	// Idempotente por turno: un reintento devuelve el mismo ticket.
	v, err := m.Conv.CrearTicket.Ejecutar(ctx, EntradaTicketConv{ID: t.conv, Asunto: asunto, Descripcion: recortar(RedactarParaIA(a.Descripcion), 2000),
		Prioridad: a.Prioridad, Clave: "iris:asistente:" + t.id.String() + ":ticket"})
	if err != nil {
		return deError(err), h
	}
	h.Entidad, h.EntidadID = "ticket", v.Vinculo.ID
	h.actividadTipo, h.actividadTexto = "ticket_creado", "Creó el ticket "+v.Vinculo.Referencia
	t.permitir(ResRegistrar)
	return resHerramienta{contenido: "Ticket creado: " + v.Vinculo.Referencia + ". Dale ese número al cliente."}, h
}

// servicioDelCliente: el servicio es del cliente de la conversación y de su empresa.
func (m *Modulo) servicioDelCliente(ctx context.Context, t *turnoIA, servicio int64) bool {
	if servicio <= 0 || t.clienteID == 0 {
		return false
	}
	ok, err := mensajeriabd.New(nucleo.BDDe(ctx)).ServicioDeClienteEmpresa(ctx, mensajeriabd.ServicioDeClienteEmpresaParams{ServicioID: servicio,
		ClienteID: t.clienteID, Empresa: t.empresa})
	return err == nil && ok
}

// herrPromesa: simular y pedir. Pedir siempre simula primero en el servidor; Argos decide (canal ia).
func (m *Modulo) herrPromesa(ctx context.Context, t *turnoIA, l LlamadaIA, h accionHecha) (resHerramienta, accionHecha) {
	var a struct {
		ServicioID int64  `json:"servicio_id"`
		Fecha      string `json:"fecha"`
	}
	if args(l.Argumentos, &a) != nil || !reFechaISO.MatchString(a.Fecha) {
		return rechazo("argumentos no válidos (servicio_id y fecha AAAA-MM-DD)."), h
	}
	if !t.fuerte {
		return rechazo("la identidad del cliente no está verificada: una promesa de pago exige verificarla (verificar_identidad) o una persona."), h
	}
	if !m.servicioDelCliente(ctx, t, a.ServicioID) {
		return rechazo("ese servicio no es del cliente de esta conversación."), h
	}
	dia, err := time.ParseInLocation("2006-01-02", a.Fecha, m.cfg.Zona)
	hoy := m.ahora().In(m.cfg.Zona)
	if err != nil || !dia.After(time.Date(hoy.Year(), hoy.Month(), hoy.Day(), 0, 0, 0, 0, m.cfg.Zona)) {
		return rechazo("la fecha debe ser un día futuro."), h
	}
	sim, err := m.Conv.Promesas.Simular(ctx, EntradaSimularPromesa{ID: t.conv, ServicioID: a.ServicioID, VenceEl: &a.Fecha})
	if err != nil {
		return deErrorPromesa(err), h
	}
	// Del simulador solo sale el resultado y el código de la regla (nunca su texto libre ni un monto).
	motivo := ""
	if sim.Regla != nil {
		motivo = " " + textoReglaPromesa(*sim.Regla)
	}
	if l.Nombre == HerrSimularPromesa {
		h.Accion = "simular_promesa"
		// Del simulador solo sale el resultado y el motivo: nunca el monto de la deuda.
		resultado := sim.Resultado
		if !slices.Contains([]string{"aprobada", "rechazada", "pendiente_aprobacion"}, resultado) {
			resultado = "pendiente_aprobacion"
		}
		return resHerramienta{contenido: "Resultado de la simulación: " + resultado + "." + motivo +
			" Si el cliente confirma el día, usa pedir_promesa."}, h
	}
	if sim.Resultado == "rechazada" {
		return rechazo("el sistema de facturación no acepta esa promesa." + motivo + " No la prometas."), h
	}
	if t.hechas["promesa_pago"] {
		return rechazo("ya pediste una promesa en este turno."), h
	}
	// Misma clave en cada intento (sale del turno): si la respuesta de Argos se pierde o responde
	// 409 idempotencia_en_curso, se repite y Argos devuelve la promesa que ya guardó (B4).
	pedido := EntradaPromesa{ID: t.conv, ServicioID: a.ServicioID, VenceEl: a.Fecha, Clave: "iris:asistente:" + t.id.String() + ":promesa"}
	p, err := m.Conv.Promesas.Crear(ctx, pedido)
	for intento := 0; err != nil && intento < 2 && esTransitorioArgos(err) && ctx.Err() == nil; intento++ {
		p, err = m.Conv.Promesas.Crear(ctx, pedido)
	}
	if err != nil {
		return deErrorPromesa(err), h
	}
	h.Entidad, h.EntidadID = "promesa", p.ID.String()
	h.actividadTipo, h.actividadTexto = "promesa_pedida", "Pidió una promesa de pago"
	switch p.Estado {
	case "activa":
		t.permitir(ResRegistrar, ResAceptar, ResAprobar, ResConfirmar)
		return resHerramienta{contenido: "Promesa aceptada para el " + a.Fecha + ". Confírmaselo al cliente sin decir montos."}, h
	case "rechazada":
		return resHerramienta{contenido: "El sistema de facturación rechazó la promesa. Díselo al cliente y pasa a una persona si pide más."}, h
	}
	return resHerramienta{contenido: "La promesa quedó en revisión: un supervisor la aprueba. Dile al cliente que le avisarán."}, h
}

// deErrorPromesa: como deError, pero el código crudo de una regla desconocida de Argos nunca llega
// al modelo (texto fijo), y promesa_ya_enviada es una falla (puede haber una promesa en Argos sin
// registro en Iris: lo revisa una persona), no un motivo para probar otra fecha.
func deErrorPromesa(err error) resHerramienta {
	var p *nucleo.Problema
	if errors.As(err, &p) {
		switch p.Codigo() {
		case CodigoPromesaNoPermitida:
			return rechazo(textoPromesaNoPermitidaFijo)
		case CodigoPromesaYaEnviada:
			slog.Warn("asistente: una herramienta falló", "tipo", p.Type, "status", p.Status)
			return resHerramienta{fallo: true}
		}
	}
	return deError(err)
}

func (m *Modulo) atencionTurno(t *turnoIA) argos.AtencionIris {
	return atencionDe(alcanceConv{asistente: true, empresa: t.empresa}, t.conv)
}

func (m *Modulo) herrCuentasBoleta(ctx context.Context, t *turnoIA, l LlamadaIA, h accionHecha) (resHerramienta, accionHecha) {
	h.Accion = "cuentas_para_boleta"
	var a struct{}
	if args(l.Argumentos, &a) != nil {
		return rechazo("esta herramienta no lleva argumentos."), h
	}
	cs, err := m.cfg.Argos.CuentasBancarias(ctx, m.atencionTurno(t))
	if err != nil {
		return resHerramienta{fallo: true}, h
	}
	bs, err := m.cfg.Argos.Bancos(ctx, m.atencionTurno(t))
	if err != nil {
		return resHerramienta{fallo: true}, h
	}
	var b strings.Builder
	b.WriteString("Cuentas de la empresa que reciben depósitos:\n")
	for _, k := range cs {
		// Solo las de la empresa de la conversación y en quetzales.
		if !k.RecibeDe(t.empresa) || (k.Moneda != "" && k.Moneda != "GTQ") {
			continue
		}
		b.WriteString(fmt.Sprintf("- cuenta id %d: %s\n", k.ID, limpiarDato(k.Nombre, 80)))
	}
	b.WriteString("Bancos de origen (para transferencia o ach):\n")
	for i, k := range bs {
		if i == 40 {
			break
		}
		b.WriteString(fmt.Sprintf("- banco id %d: %s\n", k.ID, limpiarDato(k.Nombre, 60)))
	}
	return resHerramienta{contenido: b.String()}, h
}

var (
	reMontoBoleta = regexp.MustCompile(`^[0-9]{1,10}\.[0-9]{2}$`)
)

// herrRegistrarBoleta registra el comprobante con la última foto o PDF del cliente. Nunca aprueba ni
// rechaza: eso es de caja (y el rol del asistente no tiene ese permiso).
func (m *Modulo) herrRegistrarBoleta(ctx context.Context, t *turnoIA, l LlamadaIA, h accionHecha) (resHerramienta, accionHecha) {
	var a struct {
		Metodo           string `json:"metodo"`
		CuentaBancariaID int64  `json:"cuenta_bancaria_id"`
		BancoOrigenID    int64  `json:"banco_origen_id"`
		Referencia       string `json:"referencia"`
		Monto            string `json:"monto"`
		FechaDeposito    string `json:"fecha_deposito"`
	}
	if args(l.Argumentos, &a) != nil {
		return rechazo("argumentos no válidos."), h
	}
	if !t.verificado {
		return rechazo("el cliente no está verificado: la boleta la registra una persona."), h
	}
	if t.hechas["recibir_boleta"] {
		return rechazo("ya registraste una boleta en este turno."), h
	}
	ref := strings.TrimSpace(a.Referencia)
	if !slices.Contains([]string{"deposito", "transferencia", "ach", "remesa"}, a.Metodo) || a.CuentaBancariaID <= 0 || ref == "" || len([]rune(ref)) > 60 ||
		!reMontoBoleta.MatchString(a.Monto) || !reFechaISO.MatchString(a.FechaDeposito) {
		return rechazo("faltan datos o no son válidos: método, cuenta, referencia, monto con 2 decimales y fecha AAAA-MM-DD. Pídeselos al cliente."), h
	}
	q := mensajeriabd.New(nucleo.BDDe(ctx))
	// La cuenta debe ser de la empresa de la conversación (Argos también lo valida).
	cs, err := m.cfg.Argos.CuentasBancarias(ctx, m.atencionTurno(t))
	if err != nil {
		return resHerramienta{fallo: true}, h
	}
	if !slices.ContainsFunc(cs, func(k argos.CuentaBancariaArgos) bool {
		return k.ID == a.CuentaBancariaID && k.RecibeDe(t.empresa)
	}) {
		return rechazo("esa cuenta no es de la empresa. Usa una de cuentas_para_boleta."), h
	}
	// Tope por conversación y día, y la foto debe ser de este periodo y posterior a la última boleta
	// (cada adjunto vale para una sola boleta).
	if n, err := q.BoletasRecientesConv(ctx, mensajeriabd.BoletasRecientesConvParams{ConversacionID: t.conv, Desde: m.ahora().Add(-24 * time.Hour)}); err != nil {
		return resHerramienta{fallo: true}, h
	} else if n >= BoletasPorDiaConv {
		return rechazo("ya se registraron varias boletas hoy en esta conversación. Pasa a una persona."), h
	}
	adj, err := q.UltimoAdjuntoEntrante(ctx, mensajeriabd.UltimoAdjuntoEntranteParams{ConversacionID: t.conv, Desde: t.desde})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && uuidDe(adj.AdjuntoID) == nil) {
		return rechazo("el cliente todavía no mandó una foto o un PDF nuevo de la boleta. Pídesela."), h
	}
	if err != nil {
		return resHerramienta{fallo: true}, h
	}
	// La misma boleta (misma referencia) no se registra dos veces en la conversación.
	if previa, err := q.BoletaPorReferenciaConv(ctx, mensajeriabd.BoletaPorReferenciaConvParams{ConversacionID: t.conv, Referencia: ref, Empresa: t.empresa}); err == nil {
		h.Entidad, h.EntidadID = "boleta", previa.ID.String()
		return resHerramienta{contenido: "Esa boleta ya estaba registrada: caja la revisa."}, h
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return resHerramienta{fallo: true}, h
	}
	in := EntradaBoleta{ID: t.conv, AdjuntoID: *uuidDe(adj.AdjuntoID), Metodo: a.Metodo, CuentaBancariaID: a.CuentaBancariaID, Referencia: ref,
		Monto: a.Monto, FechaDeposito: a.FechaDeposito, Nota: nombrePtr("Recibida por el asistente de IA; datos dictados por el cliente.")}
	if a.BancoOrigenID > 0 {
		in.BancoOrigenID = &a.BancoOrigenID
	}
	b, err := m.Conv.Boleta.Ejecutar(ctx, in)
	if err != nil {
		return deError(err), h
	}
	h.Entidad, h.EntidadID = "boleta", b.ID.String()
	h.actividadTipo, h.actividadTexto = "boleta_enviada", "Boleta enviada a Argos"
	t.permitir(ResRegistrar, ResRecibir)
	return resHerramienta{contenido: "Boleta registrada: caja la revisa y confirma el pago. No digas que el pago ya está aplicado."}, h
}

// herrReconexion pide la reconexión temporal ligada a la boleta ya registrada (§7.3) con el caso de
// uso PedirReconexion (permiso propio, auditoría, evento y registro local). Argos decide.
func (m *Modulo) herrReconexion(ctx context.Context, t *turnoIA, l LlamadaIA, h accionHecha) (resHerramienta, accionHecha) {
	var a struct{}
	if args(l.Argumentos, &a) != nil {
		return rechazo("esta herramienta no lleva argumentos."), h
	}
	if t.hechas["reconexion_temporal"] {
		return rechazo("ya pediste una reconexión en este turno."), h
	}
	r, err := m.Conv.Reconexion.Ejecutar(ctx, EntradaReconexion{ID: t.conv})
	if err != nil {
		return deError(err), h
	}
	h.Entidad, h.EntidadID = "reconexion", r.ID.String()
	if r.Repetida && r.NoVigente {
		return rechazo("la reconexión temporal de esta boleta ya no está vigente: no la anuncies ni des fechas. Si el cliente insiste, pasa a una persona."), h
	}
	if r.Estado == "aceptada" && r.Hasta == nil {
		// Argos la aceptó pero sin una fecha creíble: no se dice nada que no se sepa; sigue una persona.
		return resHerramienta{paso: PasoHerramientaFallo, resumen: "Argos aceptó la reconexión temporal pero no dio una fecha válida: confirma en Argos hasta cuándo vale antes de decírselo al cliente."}, h
	}
	if r.Estado != "aceptada" {
		return rechazo("el sistema de facturación no permite la reconexión temporal para este servicio. No la prometas; pasa a una persona si el cliente insiste."), h
	}
	h.actividadTipo, h.actividadTexto = "reconexion_pedida", "Pidió una reconexión temporal"
	t.permitir(ResReconectar, ResActivo, ResAceptar, ResConfirmar)
	// La fecha y la hora las pone el servidor (§7.3): el modelo nunca ve ni escribe `hasta`.
	t.fraseReconexion = FraseReconexion(*r.Hasta, m.cfg.Zona)
	return resHerramienta{contenido: "Reconexión temporal aceptada mientras caja revisa la boleta. Escribe {{reconexion}} donde va: el sistema pone " +
		"hasta cuándo vale y que el comprobante queda pendiente de revisión. No escribas tú fechas, horas, «medianoche» ni cuándo se corta el servicio. " +
		"Si el cliente pregunta qué pasa después: si el pago se confirma antes, no pasa nada; si no, el servicio puede volver a suspenderse después de esa hora."}, h
}

// herrVerificarIdentidad hace el desafío de identidad de Argos (§7.5) con el documento que el cliente
// escribió. El dato no pasa por el modelo (lo ve como ***), no se guarda (el mensaje queda con ***) y
// no va a logs, auditoría ni eventos. Máximo 3 intentos por cliente (y empresa) y día, sumando todas
// sus conversaciones desde números verificados como suyos; desde un número asociado a mano los
// intentos van aparte (intentosDisponibles). Al agotarlos o si Argos no puede verificar, pasa a una persona.
func (m *Modulo) herrVerificarIdentidad(ctx context.Context, t *turnoIA, l LlamadaIA, h accionHecha) (resHerramienta, accionHecha) {
	var a struct{}
	if args(l.Argumentos, &a) != nil {
		return rechazo("esta herramienta no lleva argumentos: el sistema toma el dato del mensaje del cliente."), h
	}
	ver, ok := m.cfg.Argos.(ArgosIdentidad)
	if !ok || t.clienteID == 0 {
		return rechazo("la verificación de identidad no está disponible. Pasa a una persona."), h
	}
	if t.fuerte {
		return resHerramienta{contenido: "La identidad del cliente ya está verificada."}, h
	}
	q := mensajeriabd.New(nucleo.BDDe(ctx))
	desde := m.ahora().Add(-24 * time.Hour)
	agotados := resHerramienta{paso: PasoHerramientaFallo, resumen: "No pasó la verificación de identidad (intentos agotados)."}
	quedan, err := m.intentosDisponibles(ctx, q, t, desde)
	if err != nil {
		return resHerramienta{fallo: true}, h
	}
	if quedan <= 0 {
		return agotados, h
	}
	// El dato sale del último mensaje del cliente, tal como lo escribió (el modelo solo vio ***).
	ms, err := q.UltimosEntrantesTexto(ctx, mensajeriabd.UltimosEntrantesTextoParams{ConversacionID: t.conv, Desde: t.desde})
	if err != nil {
		return resHerramienta{fallo: true}, h
	}
	valor, invalido := "", false
	for _, f := range ms {
		if f.Texto == nil {
			continue
		}
		// Un DPI válido, o lo que el cliente nombró como su documento; nunca el número del chat.
		var inv bool
		if valor, inv = documentoCandidato(*f.Texto, t.telefono); valor != "" {
			break
		}
		invalido = invalido || inv
	}
	if valor == "" && invalido {
		// Nombró un DPI que no puede ser un DPI (dígito verificador): no se manda a Argos ni gasta un
		// intento; se le pide otra vez y, si vuelve a pasar, sigue una persona.
		previos, err := q.RechazosVerificacionConv(ctx, mensajeriabd.RechazosVerificacionConvParams{ConversacionID: t.conv, Desde: m.ahora().Add(-24 * time.Hour)})
		if err != nil {
			return resHerramienta{fallo: true}, h
		}
		if previos >= 1 {
			return resHerramienta{paso: PasoHerramientaFallo, resumen: "El cliente escribió dos veces un DPI que no es válido."}, h
		}
		return rechazo("el DPI que escribió el cliente no es válido (le falta o le sobra un dígito). Pídele que lo revise y lo escriba completo."), h
	}
	if valor == "" {
		// Sin un candidato claro no se gasta un intento: se le pide el dato otra vez.
		return rechazo("no encuentro un DPI o NIT completo en lo que escribió el cliente. Pídele que lo escriba completo, solo el número."), h
	}
	// El intento se reserva antes de llamar a Argos, con el conteo del cliente bajo candado: dos
	// conversaciones del mismo cliente a la vez no suman más de VerificacionesPorDiaCliente.
	reserva, quedan, err := m.reservarVerificacion(ctx, t, desde)
	if err != nil {
		return resHerramienta{fallo: true}, h
	}
	if reserva == 0 {
		return agotados, h
	}
	// Argos: 429 demasiados_intentos, 422 metodo_no_disponible o 503 → pasa a una persona, sin reintentar.
	coincide, errArgos := ver.VerificarIdentidad(ctx, m.atencionTurno(t), t.clienteID, "documento", valor)
	reg := verificacionHecha{ConversacionID: t.conv, ClienteID: t.clienteID, Resultado: "no_coincide", Detalle: IdentidadNoCoincide, desde: t.desde,
		reserva: reserva}
	switch {
	case errArgos != nil:
		reg.Resultado, reg.Detalle = "error", IdentidadNoDisponible
		var er *conector.ErrorRemoto
		if errors.As(errArgos, &er) && (er.Status == http.StatusUnprocessableEntity || er.Status == http.StatusTooManyRequests) {
			reg.Resultado = "no_disponible"
			if er.Status == http.StatusTooManyRequests {
				reg.Detalle = IdentidadDemasiadosIntentos
			}
		}
	case coincide:
		reg.Resultado, reg.Detalle = "coincide", IdentidadCoincide
	}
	vale, err := m.Asistente.registrarVerificacion.Ejecutar(context.WithoutCancel(ctx), reg)
	if err != nil {
		if esNoEncontrado(err) {
			return resHerramienta{persona: true}, h
		}
		return resHerramienta{fallo: true}, h
	}
	if reg.Resultado == "coincide" && !vale {
		// La conversación cambió de cliente mientras Argos respondía: la verificación no cuenta.
		return resHerramienta{persona: true}, h
	}
	switch reg.Resultado {
	case "coincide":
		t.fuerte = true
		h.actividadTipo, h.actividadTexto = "identidad_verificada", "Verificó la identidad del cliente"
		return resHerramienta{contenido: "Identidad verificada. Ya puedes usar las herramientas que la exigen."}, h
	case "no_coincide":
		if quedan--; quedan > 0 {
			return rechazo(fmt.Sprintf("el dato no coincide. Pídele que lo revise y lo escriba completo (quedan %d intentos).", quedan)), h
		}
		return resHerramienta{paso: PasoHerramientaFallo, resumen: "No pasó la verificación de identidad (el dato no coincidió)."}, h
	}
	// Argos no pudo verificar (cliente sin documento, demasiados intentos o caído): sigue una persona.
	return resHerramienta{paso: PasoHerramientaFallo, resumen: "No se pudo verificar la identidad del cliente en Argos."}, h
}

// intentosDisponibles: cuántos intentos del desafío le quedan a esta conversación.
//   - Número verificado como del cliente: 3 por cliente y día entre todas sus conversaciones
//     verificadas. Los intentos hechos desde números sin verificar no los gastan.
//   - Número asociado a mano (sin verificar): 3 por conversación y día, 3 por cliente y día entre
//     todos sus números sin verificar y 2 en media hora. Quien logra que asocien su número al cliente
//     de otro no le quema los intentos al titular, y en media hora Argos nunca recibe más de 3 + 2 = 5
//     intentos de un cliente (su límite antes del 429).
func (m *Modulo) intentosDisponibles(ctx context.Context, q *mensajeriabd.Queries, t *turnoIA, desde time.Time) (int64, error) {
	n, err := q.VerificacionesRecientes(ctx, mensajeriabd.VerificacionesRecientesParams{Empresa: t.empresa, ClienteID: t.clienteID, Desde: desde,
		ConversacionID: t.conv, Reciente: m.ahora().Add(-30 * time.Minute)})
	if err != nil {
		return 0, err
	}
	if t.numeroVerificado {
		return VerificacionesPorDiaCliente - n.Verificados, nil
	}
	return min(VerificacionesSinVerificarPorDiaConv-n.DeLaConversacion, VerificacionesSinVerificarPorDiaCliente-n.SinVerificar,
		VerificacionesSinVerificarPorMediaHora-n.SinVerificarRecientes), nil
}

// reservarVerificacion cuenta los intentos bajo el candado del cliente y, si a la conversación le
// queda alguno, reserva uno. Devuelve el id de la reserva (0 = agotados) y cuántos quedaban antes.
func (m *Modulo) reservarVerificacion(ctx context.Context, t *turnoIA, desde time.Time) (int64, int64, error) {
	var reserva, quedan int64
	err := nucleo.EnTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		q := mensajeriabd.New(tx)
		if err := q.BloquearVerificacionesCliente(ctx, mensajeriabd.BloquearVerificacionesClienteParams{Empresa: t.empresa, ClienteID: t.clienteID}); err != nil {
			return err
		}
		var err error
		if quedan, err = m.intentosDisponibles(ctx, q, t, desde); err != nil || quedan <= 0 {
			return err
		}
		reserva, err = q.ReservarVerificacion(ctx, mensajeriabd.ReservarVerificacionParams{EmpresaID: empresaPtrDe(t.empresa), ConversacionID: t.conv,
			ClienteID: t.clienteID, CreadaEn: m.ahora(), NumeroVerificado: t.numeroVerificado})
		return err
	})
	return reserva, quedan, err
}

// reglasPromesa: el código de regla del simulador de Argos, en un texto fijo. El código crudo nunca
// viaja al modelo (un código desconocido cae en el texto genérico).
var reglasPromesa = map[string]string{
	"no_habilitada":        "La empresa no tiene habilitadas las promesas de pago para este servicio.",
	"cupo_agotado":         "El cliente ya usó las promesas que permite la política.",
	"promesa_abierta":      "El servicio ya tiene una promesa activa o pendiente.",
	"promesa_incumplida":   "El cliente tiene una promesa anterior sin cumplir.",
	"fecha_fuera_de_rango": "La fecha está más lejos de lo que permite la política: prueba con un día más cercano.",
	"dias_maximos":         "La fecha está más lejos de lo que permite la política: prueba con un día más cercano.",
	"monto_maximo":         "La deuda supera lo que la política permite prometer.",
	"servicio_no_cortado":  "El servicio no está cortado: no hace falta una promesa.",
	"suspension_manual":    "El servicio lo suspendió un operador: solo una persona puede revisarlo.",
	"requiere_aprobacion":  "La promesa necesita la aprobación de un supervisor.",
}

func textoReglaPromesa(codigo string) string {
	if t, ok := reglasPromesa[strings.ToLower(strings.TrimSpace(codigo))]; ok {
		return t
	}
	return "No cumple la política de promesas de la empresa."
}

// esTransitorioArgos: no se sabe si Argos aplicó el pedido (caído, sin respuesta, o 409
// idempotencia_en_curso): repetir con la misma clave es seguro.
func esTransitorioArgos(err error) bool {
	var p *nucleo.Problema
	if !errors.As(err, &p) {
		return true
	}
	// Un rechazo de Argos por la integración (cabeceras, Idempotency-Key, empresa) no cambia al repetir.
	return (p.Status >= http.StatusInternalServerError && p.Codigo() != CodigoPromesaIntegracion) || p.Codigo() == "idempotencia_en_curso"
}
