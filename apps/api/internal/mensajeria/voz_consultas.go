package mensajeria

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Lecturas de llamadas, la nota posterior, «Recibir llamadas» y la lista «Por devolver».

// aLlamadas arma las llamadas como las ve este usuario (con sus tramos, su conversación y `puede`).
func (m *Modulo) aLlamadas(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv, filas []mensajeriabd.MensajeriaLlamada) ([]Llamada, error) {
	if len(filas) == 0 {
		return []Llamada{}, nil
	}
	ids := make([]uuid.UUID, 0, len(filas))
	var convs, usuarios []int64
	for _, f := range filas {
		ids = append(ids, f.ID)
		if f.ConversacionID != nil {
			convs = append(convs, *f.ConversacionID)
		}
		for _, u := range []*int64{f.UsuarioID, f.ReclamadaPor} {
			if u != nil {
				usuarios = append(usuarios, *u)
			}
		}
	}
	nombres := map[int64]string{}
	if len(usuarios) > 0 {
		ns, err := q.NombresUsuarios(ctx, usuarios)
		if err != nil {
			return nil, err
		}
		for _, n := range ns {
			nombres[n.ID] = n.Nombre
		}
	}
	resumenes := map[int64]ConversacionCliente{}
	if len(convs) > 0 {
		cs, err := m.resumirConvs(ctx, q, a.usuario, convs)
		if err != nil {
			return nil, err
		}
		for _, c := range cs {
			resumenes[c.ID] = c
		}
	}
	tramos := map[uuid.UUID][]TramoLlamada{}
	ts, err := q.TramosDeLlamadas(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, t := range ts {
		tr := TramoLlamada{Tipo: t.Tipo, Inicio: t.Inicio, Fin: t.Fin, DuracionSeg: t.DuracionSeg, MotivoFin: t.MotivoFin}
		if t.UsuarioID != nil {
			tr.Usuario = &UsuarioRef{ID: *t.UsuarioID, Nombre: t.UsuarioNombre}
		}
		tramos[t.LlamadaID] = append(tramos[t.LlamadaID], tr)
	}
	grabaciones := map[uuid.UUID]string{}
	gs, err := q.GrabacionesDeLlamadas(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, g := range gs {
		grabaciones[g.LlamadaID] = g.Estado
	}
	permisos := contexto.PermisosDe(ctx)
	puede := func(p nucleo.Permiso) bool { return permisos.Tiene(string(p)) }
	out := make([]Llamada, 0, len(filas))
	for _, f := range filas {
		l := Llamada{ID: f.ID, EmpresaID: f.EmpresaID, Origen: f.Origen, Direccion: f.Direccion, ConversacionID: f.ConversacionID, MensajeID: uuidDe(f.MensajeID),
			Numero: f.Numero, Titulo: f.Numero, Contacto: ContactoDeLlamada{Tipo: "desconocido"}, Estado: f.Estado, AtendidaPor: f.AtendidaPor,
			Tramos: tramos[f.ID], SonoEn: f.SonoEn, ContestadaEn: f.ContestadaEn, TerminadaEn: f.TerminadaEn, DuracionSeg: f.DuracionSeg,
			MotivoFin: f.MotivoFin, Nota: f.Nota, Resultado: f.Resultado, DevolucionID: f.DevolucionID}
		if l.Tramos == nil {
			l.Tramos = []TramoLlamada{}
		}
		if f.Estado == LlamadaSonando || f.Estado == LlamadaConectando {
			l.SuenaHasta = &f.SuenaHasta
		}
		if f.ConversacionID != nil {
			if c, ok := resumenes[*f.ConversacionID]; ok {
				l.Titulo, l.Cola = c.Titulo, c.Cola
				l.Canal = &CanalDeLlamada{ID: c.Canal.ID, Nombre: c.Canal.Nombre, Numero: c.Canal.Numero}
				if c.Contacto.ClienteID != nil {
					l.Contacto = ContactoDeLlamada{Tipo: "cliente", ClienteID: c.Contacto.ClienteID, ServicioID: c.Contacto.ServicioID}
					if c.Contacto.Verificacion != nil {
						l.Contacto.Verificacion = *c.Contacto.Verificacion
					}
					if !llamadaFinal(f.Estado) {
						s, err := senalesDe(ctx, q, a.empresa, *c.Contacto.ClienteID, c.Contacto.ServicioID, puede(nucleo.PermClientesVer), puede(nucleo.PermTicketsVer))
						if err != nil {
							return nil, err
						}
						l.Senales = s
					}
				}
			}
		}
		quien := f.UsuarioID
		if quien == nil {
			quien = f.ReclamadaPor
		}
		if quien != nil {
			l.Usuario = &UsuarioRef{ID: *quien, Nombre: nombres[*quien]}
		}
		if e, ok := grabaciones[f.ID]; ok {
			l.Grabacion = &EstadoGrabacion{Estado: e}
		}
		mia := f.UsuarioID != nil && *f.UsuarioID == a.usuario
		l.Puede = PuedeLlamada{
			Contestar: f.Estado == LlamadaSonando && puede(nucleo.PermLlamadasContestar),
			Rechazar:  f.Estado == LlamadaSonando && puede(nucleo.PermLlamadasRechazar),
			Colgar: (f.Estado == LlamadaEnCurso || (f.Estado == LlamadaMarcando && f.Direccion == "saliente")) && (mia || a.supervisa) &&
				puede(nucleo.PermLlamadasColgar),
			Anotar: (f.Estado == LlamadaEnCurso || f.Estado == LlamadaTerminada) && (mia || a.supervisa) && puede(nucleo.PermLlamadasAnotar),
		}
		out = append(out, l)
	}
	return out, nil
}

// ticketsAbiertosVisibles: los tickets abiertos del cliente que quien mira puede ver con tickets.ver
// (su alcance por zona o cuadrilla, o los que tiene asignados). Sin el permiso, solo los suyos.
func ticketsAbiertosVisibles(ctx context.Context, q *mensajeriabd.Queries, cliente, empresa *int64) ([]mensajeriabd.TicketsAbiertosDelClienteRow, error) {
	global, zonas, cuadrillas := contexto.PermisosDe(ctx).Alcances(string(nucleo.PermTicketsVer))
	p := mensajeriabd.TicketsAbiertosDelClienteParams{ClienteID: cliente, Empresa: empresa, AlcanceGlobal: global, Zonas: zonas, Cuadrillas: cuadrillas}
	if p.Zonas == nil {
		p.Zonas = []int64{}
	}
	if p.Cuadrillas == nil {
		p.Cuadrillas = []int64{}
	}
	if actor := contexto.ActorDe(ctx); actor.EsUsuario() {
		p.ActorID = &actor.ID
	}
	return q.TicketsAbiertosDelCliente(ctx, p)
}

// senalesDe lee las señales del cliente que llama de lo que Iris ya tiene (instantánea de Argos y
// tickets). El saldo vencido no está aquí: se pide a Argos en vivo, fuera de la transacción (VerLlamada).
func senalesDe(ctx context.Context, q *mensajeriabd.Queries, empresa, cliente int64, servicio *int64, verCliente, verTickets bool) (*SenalesLlamada, error) {
	s := &SenalesLlamada{}
	if verCliente {
		f, err := q.ServicioPrincipal(ctx, mensajeriabd.ServicioPrincipalParams{ClienteID: cliente, Empresa: empresa, ServicioID: servicio})
		switch {
		case err == nil:
			s.Servicio = &ServicioDeLlamada{ID: f.ID, Codigo: f.Codigo, Plan: f.PlanNombre, Estado: f.Estado, Zona: f.ZonaNombre}
			if f.PrecioMensual != "" && f.Moneda != "" {
				precio, moneda := f.PrecioMensual, f.Moneda
				s.Servicio.PrecioMensual, s.Servicio.Moneda = &precio, &moneda
			}
		case !errors.Is(err, pgx.ErrNoRows):
			return nil, err
		}
	}
	if verTickets {
		ts, err := ticketsAbiertosVisibles(ctx, q, &cliente, &empresa)
		if err != nil {
			return nil, err
		}
		if len(ts) > 0 {
			s.TicketAbierto = &TicketDeLlamada{ID: ts[0].ID, Numero: ts[0].Numero, Asunto: ts[0].Asunto}
		}
	}
	return s, nil
}

// VerLlamada es GET /llamadas/{id}: la llamada y, si está viva, es de un cliente y quien la mira
// puede ver el estado de cuenta, la señal «tiene saldo vencido» leída de Argos en vivo (después de la
// transacción, con plazo corto). Solo un booleano: nunca montos. Si Argos no responde, no se sabe y
// el campo no viaja.
func (m *Modulo) VerLlamada(ctx context.Context, in EntradaLlamada) (Llamada, error) {
	l, err := m.Voz.Ver.Ejecutar(ctx, in)
	if err != nil || l.Senales == nil || l.Contacto.ClienteID == nil || l.ConversacionID == nil || m.cfg.Argos == nil ||
		!contexto.PermisosDe(ctx).Tiene(string(nucleo.PermClientesEstadoCuenta)) {
		return l, err
	}
	a, err := alcanceVoz(ctx, nucleo.PermLlamadasVer)
	if err != nil {
		return l, nil //nolint:nilerr // sin alcance no hay señal; la llamada ya se leyó
	}
	l.Senales.SaldoVencido = m.saldoVencido(ctx, a, *l.ConversacionID, *l.Contacto.ClienteID)
	return l, nil
}

// saldoLlamada es una entrada de la caché de «tiene saldo vencido».
type saldoLlamada struct {
	vencido *bool // nil: no se sabe
	hasta   time.Time
	// listo: abierto mientras alguien le pregunta a Argos; los demás esperan esa respuesta.
	listo chan struct{}
}

// saldoVencido pregunta a Argos si el cliente tiene saldo vencido: un solo intento (sin los
// reintentos del conector) dentro de PlazoSaldoLlamada, una sola consulta a la vez por cliente, y la
// respuesta se guarda 60 s (un fallo, 10 s): el panel puede releer la llamada sin martillar a un
// Argos degradado. nil = no se sabe.
func (m *Modulo) saldoVencido(ctx context.Context, a alcanceConv, conv, cliente int64) *bool {
	clave := [2]int64{a.empresa, cliente}
	m.saldosMu.Lock()
	if m.saldos == nil {
		m.saldos = map[[2]int64]*saldoLlamada{}
	}
	ahora := m.ahora()
	if e := m.saldos[clave]; e != nil {
		if e.listo != nil {
			listo := e.listo
			m.saldosMu.Unlock()
			select {
			case <-listo:
			case <-ctx.Done():
				return nil
			}
			m.saldosMu.Lock()
			defer m.saldosMu.Unlock()
			if e := m.saldos[clave]; e != nil && e.listo == nil {
				return e.vencido
			}
			return nil
		}
		if ahora.Before(e.hasta) {
			m.saldosMu.Unlock()
			return e.vencido
		}
	}
	// Las vencidas se barren aquí: la caché no crece sin límite.
	if len(m.saldos) > 500 {
		for k, e := range m.saldos {
			if e.listo == nil && !ahora.Before(e.hasta) {
				delete(m.saldos, k)
			}
		}
	}
	enVuelo := &saldoLlamada{listo: make(chan struct{})}
	m.saldos[clave] = enVuelo
	m.saldosMu.Unlock()

	var vencido *bool
	vale := VigenciaSaldoFallo
	cctx, cancelar := context.WithTimeout(conector.UnIntento(ctx), PlazoSaldoLlamada)
	e, err := m.cfg.Argos.EstadoCuentaCliente(cctx, atencionDe(a, conv), cliente)
	cancelar()
	if err == nil {
		vale = VigenciaSaldo
		if c := cuentaDe(cliente, e); c != nil {
			v := !c.alDia
			vencido = &v
		}
	}
	m.saldosMu.Lock()
	m.saldos[clave] = &saldoLlamada{vencido: vencido, hasta: m.ahora().Add(vale)}
	m.saldosMu.Unlock()
	close(enVuelo.listo)
	return vencido
}

func (m *Modulo) aLlamada(ctx context.Context, q *mensajeriabd.Queries, a alcanceConv, f mensajeriabd.MensajeriaLlamada) (Llamada, error) {
	ls, err := m.aLlamadas(ctx, q, a, []mensajeriabd.MensajeriaLlamada{f})
	if err != nil {
		return Llamada{}, err
	}
	return ls[0], nil
}

func nuevoVerLlamada(m *Modulo) *auditoria.Caso[EntradaLlamada, Llamada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaLlamada, Llamada]{
		Accion: "llamadas.ver", Permiso: nucleo.PermLlamadasVer, AuditarExito: sinAuditar[Llamada],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaLlamada) (Llamada, error) {
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasVer)
			if err != nil {
				return Llamada{}, err
			}
			q := mensajeriabd.New(t)
			l, err := llamadaVisible(ctx, q, a, in.ID, false)
			if err != nil {
				return Llamada{}, err
			}
			return m.aLlamada(ctx, q, a, l)
		},
	})
}

// EntradaActiva es GET /llamadas/activa.
type EntradaActiva struct {
	Pestana string `json:"pestana,omitempty"`
}

func nuevoLlamadaActiva(m *Modulo) *auditoria.Caso[EntradaActiva, *Llamada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaActiva, *Llamada]{
		Accion: "llamadas.ver_activa", Permiso: nucleo.PermLlamadasVer, AuditarExito: sinAuditar[*Llamada],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaActiva) (*Llamada, error) {
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasVer)
			if err != nil {
				return nil, err
			}
			q := mensajeriabd.New(t)
			f, err := q.LlamadaActivaDeUsuario(ctx, mensajeriabd.LlamadaActivaDeUsuarioParams{Empresa: a.empresa, UsuarioID: &a.usuario})
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			l, err := m.aLlamada(ctx, q, a, f)
			// Una saliente es de la pestaña que la hizo: el panel sabe así si esta tiene el audio o si
			// es una llamada huérfana (la pestaña que llamó ya no está) y solo puede colgarla.
			if f.Direccion == "saliente" {
				propia := in.Pestana != "" && f.Pestana != nil && *f.Pestana == in.Pestana
				l.DeEstaPestana = &propia
			}
			return &l, err
		},
	})
}

// EntradaListarLlamadas es GET /llamadas.
type EntradaListarLlamadas struct {
	Estado         string `query:"estado" enum:"sonando,conectando,en_curso,terminada,perdida,rechazada,fuera_de_horario,fallida" required:"false"`
	Direccion      string `query:"direccion" enum:"entrante,saliente" required:"false"`
	ConversacionID int64  `query:"conversacionId" minimum:"1" required:"false"`
	ColaID         int64  `query:"colaId" minimum:"1" required:"false"`
	Desde          string `query:"desde" format:"date-time" required:"false"`
	Hasta          string `query:"hasta" format:"date-time" required:"false"`
	Cursor         string `query:"cursor" required:"false"`
	Limit          int    `query:"limit" minimum:"1" maximum:"100" default:"30"`
}

func nuevoListarLlamadas(m *Modulo) *auditoria.Caso[EntradaListarLlamadas, nucleo.Pagina[Llamada]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarLlamadas, nucleo.Pagina[Llamada]]{
		Accion: "llamadas.listar", Permiso: nucleo.PermLlamadasVer, AuditarExito: sinAuditar[nucleo.Pagina[Llamada]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarLlamadas) (nucleo.Pagina[Llamada], error) {
			vacia := nucleo.Pagina[Llamada]{}
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasVer)
			if err != nil {
				return vacia, err
			}
			p := mensajeriabd.ListarLlamadasParams{Empresa: a.empresa, Usuario: a.usuario, Supervisa: a.supervisa}
			if in.Limit <= 0 {
				in.Limit = 30
			}
			p.Limite = int32(in.Limit + 1) //nolint:gosec // limit ≤ 100
			if in.Estado != "" {
				p.Estado = &in.Estado
			}
			if in.Direccion != "" {
				p.Direccion = &in.Direccion
			}
			p.ConversacionID, p.ColaID = ptrSiPositivo(in.ConversacionID), ptrSiPositivo(in.ColaID)
			for campo, dst := range map[string]**time.Time{"desde": &p.Desde, "hasta": &p.Hasta} {
				crudo := in.Desde
				if campo == "hasta" {
					crudo = in.Hasta
				}
				if crudo == "" {
					continue
				}
				v, err := time.Parse(time.RFC3339, crudo)
				if err != nil {
					return vacia, campoConv(campo, "Fecha y hora en RFC 3339", "date-time")
				}
				*dst = &v
			}
			var ultimo string
			if con, err := nucleo.DecodificarCursor(in.Cursor, &ultimo); err != nil {
				return vacia, err
			} else if con {
				id, err := uuid.Parse(ultimo)
				if err != nil {
					return vacia, campoConv("cursor", "Cursor inválido", "cursor")
				}
				p.AntesDe = pgUUID(&id)
			}
			q := mensajeriabd.New(t)
			filas, err := q.ListarLlamadas(ctx, p)
			if err != nil {
				return vacia, err
			}
			var siguiente *string
			if len(filas) > in.Limit {
				filas = filas[:in.Limit]
				c := nucleo.CodificarCursor(filas[len(filas)-1].ID.String())
				siguiente = &c
			}
			ls, err := m.aLlamadas(ctx, q, a, filas)
			if err != nil {
				return vacia, err
			}
			return nucleo.Pagina[Llamada]{Data: ls, NextCursor: siguiente}, nil
		},
	})
}

// EntradaAnotarLlamada es POST /llamadas/{id}/anotar.
type EntradaAnotarLlamada struct {
	ID        uuid.UUID `json:"id"`
	Nota      string    `json:"-"`
	Resultado string    `json:"resultado,omitempty"`
}

func nuevoAnotarLlamada(m *Modulo) *auditoria.Caso[EntradaAnotarLlamada, Llamada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaAnotarLlamada, Llamada]{
		Accion: "llamadas.anotar", Permiso: nucleo.PermLlamadasAnotar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaAnotarLlamada) (Llamada, error) {
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasAnotar)
			if err != nil {
				return Llamada{}, err
			}
			t.FijarEmpresa(a.empresa)
			t.FijarEntidad("llamada", in.ID)
			q := mensajeriabd.New(t)
			l, err := llamadaVisible(ctx, q, a, in.ID, true)
			if err != nil {
				return Llamada{}, err
			}
			// Quien la atendió, o un supervisor.
			if (l.UsuarioID == nil || *l.UsuarioID != a.usuario) && !a.supervisa {
				return Llamada{}, llamadaNoEncontrada()
			}
			if l.Estado != LlamadaEnCurso && l.Estado != LlamadaTerminada {
				return Llamada{}, nucleo.Conflicto("llamada_sin_contestar", "Solo se anota una llamada que se contestó")
			}
			nota := strings.TrimSpace(in.Nota)
			if nota == "" || utf8.RuneCountInString(nota) > 2000 || strings.ContainsFunc(nota, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) {
				return Llamada{}, campoConv("nota", "Entre 1 y 2000 caracteres", "longitud")
			}
			switch in.Resultado {
			case "":
			case "resuelto", "ticket_creado", "devolver", "equivocado", "otro":
				l.Resultado = &in.Resultado
			default:
				return Llamada{}, campoConv("resultado", "resuelto, ticket_creado, devolver, equivocado u otro", "enum")
			}
			l.Nota = &nota
			if l, err = guardarLlamada(ctx, q, l); err != nil {
				return Llamada{}, err
			}
			// La nota también va al hilo, como nota interna.
			c, err := q.ConversacionClienteParaActualizar(ctx, *l.ConversacionID)
			if err != nil {
				return Llamada{}, err
			}
			if _, err := m.agregarMensaje(ctx, t, &c, nuevoMensaje{direccion: DirNota, tipo: "texto", texto: &nota, autor: &a.usuario}); err != nil {
				return Llamada{}, err
			}
			if _, err := guardarConv(ctx, q, c, false); err != nil {
				return Llamada{}, err
			}
			if err := m.emitirEventoLlamada(ctx, t, l, EventoLlamadaActualizada, nil); err != nil {
				return Llamada{}, err
			}
			if err := t.Emitir("llamada.anotada", map[string]any{"llamada_id": l.ID, "conversacion_id": l.ConversacionID, "empresa_id": l.EmpresaID,
				"usuario_id": a.usuario, "resultado": l.Resultado}); err != nil {
				return Llamada{}, err
			}
			return m.aLlamada(ctx, q, a, l)
		},
	})
}

// Recibir llamadas (§3.4) ---------------------------------------------------------------------------

// RecibirLlamadas dice si este navegador recibe el timbre.
type RecibirLlamadas struct {
	Recibe bool `json:"recibe" doc:"Este navegador tiene «Recibir llamadas» encendido"`
	// Vivo: hay un flujo de eventos abierto con latido reciente (sin él no suena ninguna llamada).
	Vivo bool `json:"vivo"`
	// Motivo: por qué lo apagó el servidor. Ausente si está encendido, si lo apagó la persona o si
	// nunca se encendió en este navegador.
	Motivo *string `json:"motivo,omitempty" enum:"tope,inactivo" doc:"Por qué el servidor apagó «Recibir llamadas» en este navegador: tope (pasaste de 10 navegadores) o inactivo (30 días sin abrirlo)"`
	// NumeroConLlamadas: la empresa tiene algún número con las llamadas encendidas en Iris (si no, no
	// tiene sentido ofrecer encender «Recibir llamadas»).
	NumeroConLlamadas bool `json:"numeroConLlamadas" doc:"La empresa activa tiene algún número con las llamadas de WhatsApp encendidas en Iris"`
}

// EntradaRecibir es POST /llamadas/recibir/encender y /apagar.
type EntradaRecibir struct {
	Navegador string `json:"navegador" minLength:"8" maxLength:"64" pattern:"^[A-Za-z0-9_-]+$" doc:"Id que el panel genera y guarda en este navegador"`
	recibe    bool
}

// EntradaVerRecibir es GET /llamadas/recibir.
type EntradaVerRecibir struct {
	Cabecera  string `header:"X-Iris-Navegador" required:"false" maxLength:"64" pattern:"^[A-Za-z0-9_-]{8,64}$" doc:"El id de este navegador (preferido: no queda en los registros de acceso)"`
	Navegador string `query:"navegador" required:"false" maxLength:"64" pattern:"^[A-Za-z0-9_-]{8,64}$" doc:"El id de este navegador, si no va en la cabecera"`
}

func nuevoRecibirLlamadas(m *Modulo) *auditoria.Caso[EntradaRecibir, RecibirLlamadas] {
	return auditoria.Nuevo(auditoria.Caso[EntradaRecibir, RecibirLlamadas]{
		Accion: "llamadas.recibir", Permiso: nucleo.PermLlamadasContestar,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaRecibir) (RecibirLlamadas, error) {
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasContestar)
			if err != nil {
				return RecibirLlamadas{}, err
			}
			t.FijarEmpresa(a.empresa)
			t.Anotar(map[string]any{"recibe": in.recibe})
			// Tope de navegadores por usuario: se apagan (con su motivo) los que llevan 30 días sin verse y
			// los más viejos; de los apagados solo se guardan los más recientes.
			if err := mensajeriabd.New(t).ApagarReceptoresDeMas(ctx, mensajeriabd.ApagarReceptoresDeMasParams{Empresa: a.empresa, UsuarioID: a.usuario,
				Navegador: in.Navegador, Viejo: m.ahora().Add(-30 * 24 * time.Hour), Conservar: MaxNavegadoresRecibir - 1}); err != nil {
				return RecibirLlamadas{}, err
			}
			if err := mensajeriabd.New(t).BorrarReceptoresSobrantes(ctx, mensajeriabd.BorrarReceptoresSobrantesParams{Empresa: a.empresa, UsuarioID: a.usuario,
				Navegador: in.Navegador, Conservar: 2 * MaxNavegadoresRecibir}); err != nil {
				return RecibirLlamadas{}, err
			}
			f, err := mensajeriabd.New(t).FijarReceptor(ctx, mensajeriabd.FijarReceptorParams{EmpresaID: a.empresaPtr(), UsuarioID: a.usuario,
				Navegador: in.Navegador, Recibe: in.recibe, Ahora: m.ahora()})
			if err != nil {
				return RecibirLlamadas{}, err
			}
			return RecibirLlamadas{Recibe: f.Recibe, Vivo: f.Recibe}, nil
		},
	})
}

func nuevoVerRecibir(m *Modulo) *auditoria.Caso[EntradaVerRecibir, RecibirLlamadas] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerRecibir, RecibirLlamadas]{
		Accion: "llamadas.ver_recibir", Permiso: nucleo.PermLlamadasContestar, AuditarExito: sinAuditar[RecibirLlamadas],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerRecibir) (RecibirLlamadas, error) {
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasContestar)
			if err != nil {
				return RecibirLlamadas{}, err
			}
			navegador := in.Cabecera
			if navegador == "" {
				navegador = in.Navegador
			}
			if navegador == "" {
				return RecibirLlamadas{}, reglaVoz("navegador_requerido", "Falta el id de este navegador (cabecera X-Iris-Navegador)")
			}
			conLlamadas, err := mensajeriabd.New(t).EmpresaConLlamadas(ctx, a.empresa)
			if err != nil {
				return RecibirLlamadas{}, err
			}
			f, err := mensajeriabd.New(t).ReceptorDe(ctx, mensajeriabd.ReceptorDeParams{Empresa: a.empresa, UsuarioID: a.usuario, Navegador: navegador})
			if errors.Is(err, pgx.ErrNoRows) {
				return RecibirLlamadas{NumeroConLlamadas: conLlamadas}, nil
			}
			if err != nil {
				return RecibirLlamadas{}, err
			}
			r := RecibirLlamadas{Recibe: f.Recibe, Vivo: f.Recibe && f.VistoEn.After(m.ahora().Add(-FlujoVivo)), NumeroConLlamadas: conLlamadas}
			if !f.Recibe {
				r.Motivo = f.ApagadoMotivo
			}
			return r, nil
		},
	})
}

// Devoluciones («Por devolver») -----------------------------------------------------------------------

// EntradaListarDevoluciones es GET /llamadas/devoluciones.
type EntradaListarDevoluciones struct {
	Estado string `query:"estado" enum:"pendiente,hecha,descartada,vencida" default:"pendiente"`
	Cursor string `query:"cursor" required:"false"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"30"`
}

func nuevoListarDevoluciones(m *Modulo) *auditoria.Caso[EntradaListarDevoluciones, nucleo.Pagina[DevolucionLlamada]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListarDevoluciones, nucleo.Pagina[DevolucionLlamada]]{
		Accion: "llamadas.listar_devoluciones", Permiso: nucleo.PermLlamadasVer, AuditarExito: sinAuditar[nucleo.Pagina[DevolucionLlamada]],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListarDevoluciones) (nucleo.Pagina[DevolucionLlamada], error) {
			vacia := nucleo.Pagina[DevolucionLlamada]{}
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasVer)
			if err != nil {
				return vacia, err
			}
			if in.Estado == "" {
				in.Estado = "pendiente"
			}
			if in.Limit <= 0 {
				in.Limit = 30
			}
			p := mensajeriabd.ListarDevolucionesParams{Empresa: a.empresa, Estado: in.Estado, Usuario: a.usuario, Supervisa: a.supervisa,
				Limite: int32(in.Limit + 1)} //nolint:gosec // limit ≤ 100
			var ultimo int64
			if con, err := nucleo.DecodificarCursor(in.Cursor, &ultimo); err != nil {
				return vacia, err
			} else if con {
				p.AntesDe = &ultimo
			}
			filas, err := mensajeriabd.New(t).ListarDevoluciones(ctx, p)
			if err != nil {
				return vacia, err
			}
			var siguiente *string
			if len(filas) > in.Limit {
				filas = filas[:in.Limit]
				c := nucleo.CodificarCursor(filas[len(filas)-1].ID)
				siguiente = &c
			}
			out := make([]DevolucionLlamada, 0, len(filas))
			for _, f := range filas {
				out = append(out, aDevolucion(f))
			}
			if err := m.conTitulos(ctx, mensajeriabd.New(t), a.usuario, out); err != nil {
				return vacia, err
			}
			return nucleo.Pagina[DevolucionLlamada]{Data: out, NextCursor: siguiente}, nil
		},
	})
}

// EntradaDescartarDevolucion es POST /llamadas/devoluciones/{id}/descartar.
type EntradaDescartarDevolucion struct {
	ID     int64  `json:"id"`
	Motivo string `json:"motivo"`
}

func nuevoDescartarDevolucion(m *Modulo) *auditoria.Caso[EntradaDescartarDevolucion, DevolucionLlamada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaDescartarDevolucion, DevolucionLlamada]{
		Accion: "llamadas.descartar_devolucion", Permiso: nucleo.PermLlamadasDescartarDevolucion,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaDescartarDevolucion) (DevolucionLlamada, error) {
			a, err := alcanceVoz(ctx, nucleo.PermLlamadasDescartarDevolucion)
			if err != nil {
				return DevolucionLlamada{}, err
			}
			t.FijarEmpresa(a.empresa)
			t.FijarEntidad("llamada_devolucion", in.ID)
			q := mensajeriabd.New(t)
			d, err := q.DevolucionParaActualizar(ctx, mensajeriabd.DevolucionParaActualizarParams{ID: in.ID, Empresa: a.empresa})
			if errors.Is(err, pgx.ErrNoRows) {
				return DevolucionLlamada{}, llamadaNoEncontrada()
			}
			if err != nil {
				return DevolucionLlamada{}, err
			}
			if _, err := convVisible(ctx, q, a, d.ConversacionID, false); err != nil {
				return DevolucionLlamada{}, llamadaNoEncontrada()
			}
			motivo := strings.TrimSpace(in.Motivo)
			if motivo == "" || utf8.RuneCountInString(motivo) > 300 {
				return DevolucionLlamada{}, campoConv("motivo", "Entre 1 y 300 caracteres", "longitud")
			}
			if d.Estado != "pendiente" {
				return DevolucionLlamada{}, nucleo.Conflicto("devolucion_cerrada", "Esa devolución ya se cerró")
			}
			f, err := q.CerrarDevolucion(ctx, mensajeriabd.CerrarDevolucionParams{ID: d.ID, Estado: "descartada", AtendidaPor: &a.usuario, Motivo: &motivo})
			if err != nil {
				return DevolucionLlamada{}, err
			}
			if l, err := q.LlamadaPorID(ctx, f.LlamadaID); err == nil {
				if err := m.emitirEventoLlamada(ctx, t, l, EventoDevolucion, nil); err != nil {
					return DevolucionLlamada{}, err
				}
			}
			ds := []DevolucionLlamada{aDevolucion(f)}
			if err := m.conTitulos(ctx, q, a.usuario, ds); err != nil {
				return DevolucionLlamada{}, err
			}
			return ds[0], t.Emitir("llamada.devolucion_cerrada", map[string]any{"devolucion_id": f.ID, "conversacion_id": f.ConversacionID,
				"empresa_id": f.EmpresaID, "llamada_id": f.LlamadaID, "estado": f.Estado, "usuario_id": a.usuario, "llamada_devuelta_id": nil})
		},
	})
}

func nuevoVencerDevoluciones(m *Modulo) *auditoria.Caso[struct{}, int] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, int]{
		Accion: "llamadas.vencer_devoluciones", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(n int) bool { return n > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (int, error) {
			if err := soloSistemaVoz(ctx); err != nil {
				return 0, err
			}
			q := mensajeriabd.New(t)
			ds, err := q.DevolucionesVencidas(ctx, m.ahora())
			if err != nil {
				return 0, err
			}
			for _, d := range ds {
				f, err := q.CerrarDevolucion(ctx, mensajeriabd.CerrarDevolucionParams{ID: d.ID, Estado: "vencida"})
				if err != nil {
					return 0, err
				}
				if err := t.EmitirEnEmpresa("llamada.devolucion_cerrada", f.EmpresaID, map[string]any{"devolucion_id": f.ID, "conversacion_id": f.ConversacionID,
					"empresa_id": f.EmpresaID, "llamada_id": f.LlamadaID, "estado": f.Estado, "usuario_id": nil, "llamada_devuelta_id": nil}); err != nil {
					return 0, err
				}
			}
			t.Anotar(map[string]any{"vencidas": len(ds)})
			return len(ds), nil
		},
	})
}

// llamadasDeMensajes: el objeto llamada de los mensajes de tipo llamada.
func llamadasDeMensajes(ctx context.Context, q *mensajeriabd.Queries, filas []mensajeriabd.MensajeriaConversacionMensaje) (map[uuid.UUID]LlamadaEnMensaje, error) {
	var ids []uuid.UUID
	for _, f := range filas {
		if id := uuidDe(f.LlamadaID); id != nil {
			ids = append(ids, *id)
		}
	}
	out := map[uuid.UUID]LlamadaEnMensaje{}
	if len(ids) == 0 {
		return out, nil
	}
	ls, err := q.LlamadasPorIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	var usuarios []int64
	for _, l := range ls {
		if l.UsuarioID != nil {
			usuarios = append(usuarios, *l.UsuarioID)
		}
	}
	nombres := map[int64]string{}
	if len(usuarios) > 0 {
		ns, err := q.NombresUsuarios(ctx, usuarios)
		if err != nil {
			return nil, err
		}
		for _, n := range ns {
			nombres[n.ID] = n.Nombre
		}
	}
	grabaciones := map[uuid.UUID]string{}
	gs, err := q.GrabacionesDeLlamadas(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, g := range gs {
		grabaciones[g.LlamadaID] = g.Estado
	}
	for _, l := range ls {
		x := LlamadaEnMensaje{ID: l.ID, Direccion: l.Direccion, Estado: l.Estado, AtendidaPor: l.AtendidaPor, DuracionSeg: l.DuracionSeg, DevolucionID: l.DevolucionID,
			MotivoFin: l.MotivoFin, TieneNota: l.Nota != nil}
		if l.UsuarioID != nil {
			x.Usuario = &UsuarioRef{ID: *l.UsuarioID, Nombre: nombres[*l.UsuarioID]}
		}
		if e, ok := grabaciones[l.ID]; ok {
			x.Grabacion = &EstadoGrabacion{Estado: e}
		}
		if l.RafagaHasta != nil && l.CanalID != nil {
			// Pocas (solo las rechazadas por ráfaga): una lectura por llamada.
			r := RafagaDeLlamada{Hasta: *l.RafagaHasta, Intentos: l.RafagaIntentos}
			canal, err := q.CanalPorID(ctx, *l.CanalID)
			if err != nil {
				return nil, err
			}
			r.PuedeLevantar = derefEmpresa(canal.EmpresaID) == derefEmpresa(l.EmpresaID)
			if en, err := q.RafagaLevantadaEn(ctx, mensajeriabd.RafagaLevantadaEnParams{CanalID: *l.CanalID, Numero: l.Numero, Empresa: derefEmpresa(l.EmpresaID)}); err == nil {
				r.Levantada = en.After(l.SonoEn)
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
			x.Rafaga = &r
		}
		out[l.ID] = x
	}
	return out, nil
}
