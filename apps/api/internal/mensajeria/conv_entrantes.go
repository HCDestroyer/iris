package mensajeria

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Mensajes del cliente (webhook de Meta, ya guardado en mensajeria.entrantes y procesado en el job
// mensajeria.procesar_entrante): cada mensaje del número del canal entra a la conversación de
// (empresa, número, contacto), deduplicado por su wamid. Los medios se descargan en otro job.

var numeroWA = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)

// respuestaAvisoVentana: un mensaje del cliente hasta 72 h después de un aviso de ese número se
// marca como respuesta al aviso.
const respuestaAvisoVentana = 72 * time.Hour

func soloDigitos(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// contenidoEntrante traduce un mensaje de Meta al mensaje de la conversación.
func contenidoEntrante(msg metaMensaje) nuevoMensaje {
	n := nuevoMensaje{direccion: DirEntrante, tipo: "texto"}
	texto := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" {
			s = recortar(s, MaxTextoWhatsApp)
			n.texto = &s
		}
	}
	medio := func(tipo string, md *metaMedio) {
		n.tipo = tipo
		if md.ID != "" {
			id := md.ID
			n.medio = &id
		}
		texto(md.Caption)
	}
	switch {
	case msg.Type == "text":
		texto(msg.Text.Body)
	case msg.Image != nil:
		medio("imagen", msg.Image)
	case msg.Sticker != nil:
		medio("imagen", msg.Sticker)
	case msg.Document != nil:
		medio("documento", msg.Document)
		if msg.Document.Caption == "" && msg.Document.Filename != "" {
			texto(msg.Document.Filename)
		}
	case msg.Audio != nil:
		medio("audio", msg.Audio)
	case msg.Video != nil:
		medio("video", msg.Video)
	case msg.Location != nil:
		n.tipo = "ubicacion"
		lat, lon := msg.Location.Latitude, msg.Location.Longitude
		if lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180 {
			n.lat, n.lon = &lat, &lon
		}
		texto(strings.TrimSpace(msg.Location.Name + " " + msg.Location.Address))
	case msg.Button != nil:
		texto(msg.Button.Text)
	case msg.Interactive != nil && msg.Interactive.ButtonReply != nil:
		texto(msg.Interactive.ButtonReply.Title)
	case msg.Interactive != nil && msg.Interactive.ListReply != nil:
		texto(msg.Interactive.ListReply.Title)
	case len(msg.Contacts) > 0:
		n.tipo = "contacto"
		texto(msg.Contacts[0].Name.FormattedName)
	default:
		texto("[" + recortar(msg.Type, 30) + "]")
	}
	if n.texto == nil && n.medio == nil && n.lat == nil {
		vacio := "[" + recortar(msg.Type, 30) + "]"
		n.texto = &vacio
	}
	return n
}

// recibirMensaje guarda un mensaje del cliente en su conversación (la crea si no existe) y aplica
// los cambios de estado: reabre, abre la espera del SLA y marca la ventana de 24 h.
func (m *Modulo) recibirMensaje(ctx context.Context, t *auditoria.Tx, canal mensajeriabd.MensajeriaCanal, msg metaMensaje, perfil string, ahora time.Time) error {
	numero := "+" + strings.TrimPrefix(msg.From, "+")
	if msg.ID == "" || !numeroWA.MatchString(numero) {
		return nil
	}
	q := mensajeriabd.New(t)
	// Un número compartido: la empresa la elige la regla de §15.3; si no, la dueña.
	d, err := m.rutearEntrante(ctx, q, canal, numero)
	if err != nil {
		return err
	}
	empresa := d.empresa
	t.FijarEmpresa(empresa)
	cuando := instanteMeta(msg.Timestamp, ahora)
	c, nueva, err := m.conversacionDe(ctx, t, canal, d, numero, perfil, ahora, "cliente")
	if err != nil {
		return err
	}
	if d.compartido && d.motivo != RutaConversacionAbierta {
		if err := t.Emitir("conversacion.empresa_asignada", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "canal_id": c.CanalID,
			"motivo": d.motivo, "anterior_empresa_id": nil}); err != nil {
			return err
		}
	}
	if empresa != derefEmpresa(canal.EmpresaID) && msg.Type == "text" && palabrasBaja[palabraDe(msg.Text.Body)] {
		// La baja también vale en la empresa que atiende la conversación por el número prestado.
		porNumero := cuando
		if ahora.After(porNumero) {
			porNumero = ahora
		}
		if _, err := q.InsertarConsentimientoNumero(ctx, mensajeriabd.InsertarConsentimientoNumeroParams{EmpresaID: c.EmpresaID, Telefono: numero,
			Estado: "baja", Origen: "cliente", OcurridoEn: porNumero}); err != nil {
			return err
		}
	}
	// El mismo mensaje dos veces (Meta reintenta, o el mismo mensaje en otro webhook): nada.
	wamid := msg.ID
	if _, err := q.MensajeEntrantePorProveedor(ctx, mensajeriabd.MensajeEntrantePorProveedorParams{ConversacionID: c.ID, ProveedorID: &wamid}); err == nil {
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	n := contenidoEntrante(msg)
	n.proveedor = &wamid
	n.creadoEn = cuando
	antes := c
	if perfil != "" && (c.PerfilNombre == nil || *c.PerfilNombre != perfil) {
		p := recortar(perfil, 120)
		c.PerfilNombre = &p
	}
	reabierta := false
	switch c.Estado {
	case EstadoConvResuelta:
		reabierta = true
		c.Estado, c.ReabiertaEn, c.ResueltaEn = EstadoConvAbierta, &ahora, nil
		if err := m.conservarAsignacion(ctx, t, &c); err != nil {
			return err
		}
	case EstadoConvEsperando:
		c.Estado = EstadoConvAbierta
	}
	if c.UltimoEntranteEn == nil || cuando.After(*c.UltimoEntranteEn) {
		c.UltimoEntranteEn = &cuando
	}
	if c.EsperaDesde == nil {
		c.EsperaDesde = &cuando
		c.SlaAvisado = false
		if c.SlaVenceEn, err = m.slaDe(ctx, q, c); err != nil {
			return err
		}
	}
	f, err := m.agregarMensaje(ctx, t, &c, n)
	if err != nil {
		return err
	}
	if nueva || reabierta || msg.Context != nil {
		if err := m.marcarRespuestaAviso(ctx, t, &c, canal.ID, numero, msg, ahora); err != nil {
			return err
		}
	}
	if reabierta {
		if err := m.sistema(ctx, t, &c, EventoSistema{Tipo: "reabierta"}); err != nil {
			return err
		}
		if err := t.Emitir("conversacion.reabierta", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "origen": "cliente",
			"asignado_a": c.AsignadoA}); err != nil {
			return err
		}
	}
	cambioEstado := antes.Estado != c.Estado || antes.AsignadoA != c.AsignadoA
	if c, err = guardarConv(ctx, q, c, cambioEstado || nueva); err != nil {
		return err
	}
	if err := m.emitirEventoConv(ctx, t, c, EventoConvActualizada, nil, nil); err != nil {
		return err
	}
	if n.medio != nil {
		if err := m.encolar(ctx, t.Tx, ArgsDescargarMedio{MensajeID: f.ID}); err != nil {
			return err
		}
	}
	// Horario, bot y fuera de horario son de la dueña del número: no se aplican a las
	// conversaciones de otra empresa (§15.3).
	if m.alRecibir != nil && empresa == derefEmpresa(canal.EmpresaID) {
		if err := m.alRecibir(ctx, t, &c, canal, f); err != nil {
			return err
		}
	}
	return t.Emitir("conversacion.mensaje_recibido", map[string]any{"conversacion_id": c.ID, "mensaje_id": f.ID, "empresa_id": c.EmpresaID,
		"canal_id": c.CanalID, "tipo": f.Tipo, "cliente_id": c.ClienteID, "asignado_a": c.AsignadoA, "cola_id": c.ColaID})
}

// conversacionDe lee (bloqueada) o crea la conversación de (empresa, canal, número).
func (m *Modulo) conversacionDe(ctx context.Context, t *auditoria.Tx, canal mensajeriabd.MensajeriaCanal, d destinoEntrante, numero, perfil string,
	ahora time.Time, origen string) (mensajeriabd.MensajeriaConversacion, bool, error) {
	q := mensajeriabd.New(t)
	empresa := d.empresa
	c, err := q.ConversacionPorContacto(ctx, mensajeriabd.ConversacionPorContactoParams{Empresa: empresa, CanalID: canal.ID, Numero: numero})
	if err == nil {
		return c, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return c, false, err
	}
	var cola *int64
	if d.cola != nil {
		if co, err := q.ColaPorID(ctx, mensajeriabd.ColaPorIDParams{ID: *d.cola, Empresa: empresa}); err == nil && co.Activa {
			cola = d.cola
		}
	}
	empresaConv := canal.EmpresaID
	if empresa != derefEmpresa(canal.EmpresaID) {
		empresaConv = &empresa
	}
	// Una sola coincidencia exacta del número en la instantánea de Argos de la empresa: se asocia sola.
	var cliente *int64
	var asociacion *string
	digitos := soloDigitos(numero)
	cs, err := q.ClientesPorTelefonoInstantanea(ctx, mensajeriabd.ClientesPorTelefonoInstantaneaParams{Digitos: digitos, Empresa: empresa})
	if err != nil {
		return c, false, err
	}
	titulares := 0
	for _, x := range cs {
		if x.PorTelefono || x.PorMovil {
			titulares++
			if titulares == 1 {
				id := x.ID
				cliente = &id
			}
		}
	}
	if titulares == 1 {
		asociacion = nombrePtr("automatica")
	} else {
		cliente = nil
	}
	var perfilNombre *string
	if perfil != "" {
		perfilNombre = nombrePtr(recortar(perfil, 120))
	}
	c, err = q.InsertarConversacionCliente(ctx, mensajeriabd.InsertarConversacionClienteParams{EmpresaID: empresaConv, CanalID: canal.ID,
		Numero: numero, PerfilNombre: perfilNombre, ColaID: cola, ClienteID: cliente, Asociacion: asociacion, ClienteVerificado: cliente != nil,
		UltimaActividad: ahora})
	if errors.Is(err, pgx.ErrNoRows) {
		c, err = q.ConversacionPorContacto(ctx, mensajeriabd.ConversacionPorContactoParams{Empresa: empresa, CanalID: canal.ID, Numero: numero})
		return c, false, err
	}
	if err != nil {
		return c, false, err
	}
	t.FijarEntidad("conversacion", c.ID)
	if err := m.sistema(ctx, t, &c, EventoSistema{Tipo: "conversacion_creada", ColaNombre: m.nombreCola(ctx, q, empresa, c.ColaID)}); err != nil {
		return c, false, err
	}
	return c, true, t.Emitir("conversacion.creada", map[string]any{"conversacion_id": c.ID, "empresa_id": c.EmpresaID, "canal_id": c.CanalID,
		"cola_id": c.ColaID, "cliente_id": c.ClienteID, "asociacion": c.Asociacion, "origen": origen})
}

func (m *Modulo) nombreCola(ctx context.Context, q *mensajeriabd.Queries, empresa int64, id *int64) *string {
	if id == nil {
		return nil
	}
	co, err := q.ColaPorID(ctx, mensajeriabd.ColaPorIDParams{ID: *id, Empresa: empresa})
	if err != nil {
		return nil
	}
	return &co.Nombre
}

// conservarAsignacion: al reabrirse, sigue con su agente si todavía la atiende (activo, de la
// empresa, con conversaciones.ver y en la cola o supervisando); si no, queda sin asignar.
func (m *Modulo) conservarAsignacion(ctx context.Context, t *auditoria.Tx, c *mensajeriabd.MensajeriaConversacion) error {
	if c.AsignadoA == nil {
		return nil
	}
	_, sup, err := usuarioAtiende(ctx, t, empresaDeConv(*c), *c.AsignadoA)
	var p *nucleo.Problema
	if errors.As(err, &p) {
		c.AsignadoA, c.AsignadoEn = nil, nil
		return nil
	}
	if err != nil {
		return err
	}
	if sup || c.ColaID == nil {
		return nil
	}
	miembro, err := atiendeCola(ctx, mensajeriabd.New(t), *c.ColaID, *c.AsignadoA)
	if err != nil {
		return err
	}
	if !miembro {
		c.AsignadoA, c.AsignadoEn = nil, nil
	}
	return nil
}

// marcarRespuestaAviso agrega «Respondió al aviso …» si el mensaje responde (context) a un aviso de
// ese número o si hubo uno en las últimas 72 h (al crearse o reabrirse la conversación).
func (m *Modulo) marcarRespuestaAviso(ctx context.Context, t *auditoria.Tx, c *mensajeriabd.MensajeriaConversacion, canal int64, numero string, msg metaMensaje, ahora time.Time) error {
	q := mensajeriabd.New(t)
	var aviso mensajeriabd.MensajeriaAviso
	var err error
	if msg.Context != nil && msg.Context.ID != "" {
		ctxID := msg.Context.ID
		aviso, err = q.AvisoDeProveedorCanal(ctx, mensajeriabd.AvisoDeProveedorCanalParams{CanalID: &canal, ProveedorID: &ctxID})
	} else {
		aviso, err = q.UltimoAvisoA(ctx, mensajeriabd.UltimoAvisoAParams{CanalID: &canal, Destino: &numero, Desde: ahora.Add(-respuestaAvisoVentana)})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if aviso.EmpresaID != empresaDeConv(*c) {
		// En un número compartido, el aviso de otra empresa no se muestra aquí.
		return nil
	}
	id := aviso.ID.String()
	return m.sistema(ctx, t, c, EventoSistema{Tipo: "respondio_aviso", Plantilla: &aviso.Plantilla, EntidadID: &id})
}

// Estados de entrega de los mensajes salientes ------------------------------------------------------

var ordenEntrega = map[string]int{EntregaEnCola: 0, EntregaEnviado: 1, EntregaEntregado: 2, EntregaLeido: 3}

// estadoMensajeConv aplica un estado de Meta a un mensaje saliente de una conversación (nunca
// retrocede). ok = false si el wamid no es de un mensaje de conversación (puede ser de un aviso).
func (m *Modulo) estadoMensajeConv(ctx context.Context, t *auditoria.Tx, q *mensajeriabd.Queries, canal int64, s metaEstado, ahora time.Time) (bool, error) {
	if s.ID == "" {
		return false, nil
	}
	id := s.ID
	f, err := q.MensajeSalientePorProveedor(ctx, mensajeriabd.MensajeSalientePorProveedorParams{CanalID: canal, ProveedorID: &id})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if f.EmpresaID != nil {
		t.FijarEmpresa(*f.EmpresaID)
	}
	actual := ""
	if f.EstadoEntrega != nil {
		actual = *f.EstadoEntrega
	}
	cuando := instanteMeta(s.Timestamp, ahora)
	p := mensajeriabd.MarcarEnvioMensajeParams{ID: f.ID, MotivoFalla: f.MotivoFalla, DetalleFalla: f.DetalleFalla}
	switch s.Status {
	case "sent":
		p.EstadoEntrega = EntregaEnviado
	case "delivered":
		p.EstadoEntrega, p.EntregadoEn = EntregaEntregado, &cuando
	case "read":
		p.EstadoEntrega, p.LeidoEn = EntregaLeido, &cuando
		if f.EntregadoEn == nil {
			p.EntregadoEn = &cuando
		}
	case "failed":
		if actual == EntregaFallido || ordenEntrega[actual] >= ordenEntrega[EntregaEntregado] {
			return true, nil
		}
		motivo, detalle := motivoFallaMeta(s)
		p.EstadoEntrega, p.MotivoFalla, p.DetalleFalla = EntregaFallido, &motivo, &detalle
	default:
		return true, nil
	}
	if s.Status != "failed" && (actual == EntregaFallido || ordenEntrega[p.EstadoEntrega] <= ordenEntrega[actual]) {
		return true, nil
	}
	f, err = q.MarcarEnvioMensaje(ctx, p)
	if err != nil {
		return true, err
	}
	c, err := q.ConversacionClientePorID(ctx, f.ConversacionID)
	if err != nil {
		return true, err
	}
	if err := m.emitirEventoConv(ctx, t, c, EventoConvMensajeActualizad, &f.ID, nil); err != nil {
		return true, err
	}
	if p.EstadoEntrega == EntregaFallido {
		return true, t.Emitir("conversacion.mensaje_fallido", map[string]any{"conversacion_id": c.ID, "mensaje_id": f.ID, "empresa_id": c.EmpresaID,
			"motivo": f.MotivoFalla, "autor_id": f.AutorID})
	}
	return true, nil
}

// Medios del cliente --------------------------------------------------------------------------------

// ArgsDescargarMedio descarga de Meta el archivo de un mensaje del cliente.
type ArgsDescargarMedio struct {
	MensajeID uuid.UUID `json:"mensaje_id"`
}

// Kind implementa river.JobArgs.
func (ArgsDescargarMedio) Kind() string { return "mensajeria.descargar_medio" }

// InsertOpts: cola de mensajería, uno por mensaje, 5 intentos.
func (ArgsDescargarMedio) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Cola, MaxAttempts: 5, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type trabajadorMedio struct {
	river.WorkerDefaults[ArgsDescargarMedio]
	m  *Modulo
	bd nucleo.BD
}

func (w *trabajadorMedio) Work(ctx context.Context, job *river.Job[ArgsDescargarMedio]) error {
	return w.m.descargarMedio(ctxSistema(ctx, w.bd), job.Args.MensajeID, job.Attempt >= job.MaxAttempts)
}

type medioPorBajar struct {
	MensajeID uuid.UUID `json:"mensaje_id"`
	nada      bool
	medio     string
	token     string
	tipo      string
	conv      mensajeriabd.MensajeriaConversacion
}

type medioBajado struct {
	MensajeID uuid.UUID `json:"mensaje_id"`
	Mime      string    `json:"mime,omitempty"`
	Bytes     int       `json:"bytes,omitempty"`
	Error     string    `json:"error,omitempty"`
	datos     []byte
}

// MarshalJSON: sin los datos.
func (b medioBajado) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"mensaje_id": b.MensajeID, "mime": b.Mime, "bytes": b.Bytes, "error": b.Error})
}

// errMedioPermanente: Meta no lo va a dar (no existe, demasiado grande): no se reintenta.
type errMedioPermanente struct{ motivo string }

func (e errMedioPermanente) Error() string { return "medio: " + e.motivo }

func nuevoPrepararMedio(m *Modulo) *auditoria.Caso[uuid.UUID, medioPorBajar] {
	return auditoria.Nuevo(auditoria.Caso[uuid.UUID, medioPorBajar]{
		Accion: "mensajeria.preparar_medio", Acceso: auditoria.Autenticado, AuditarExito: sinAuditar[medioPorBajar],
		Logica: func(ctx context.Context, t *auditoria.Tx, id uuid.UUID) (medioPorBajar, error) {
			q := mensajeriabd.New(t)
			f, err := q.MensajeConversacionPorID(ctx, id)
			if errors.Is(err, pgx.ErrNoRows) {
				return medioPorBajar{nada: true}, nil
			}
			if err != nil {
				return medioPorBajar{}, err
			}
			if f.MedioID == nil || uuidDe(f.AdjuntoID) != nil {
				return medioPorBajar{nada: true}, nil
			}
			c, err := q.ConversacionClientePorID(ctx, f.ConversacionID)
			if err != nil {
				return medioPorBajar{}, err
			}
			canal, err := q.CanalPorID(ctx, c.CanalID)
			if err != nil {
				return medioPorBajar{}, err
			}
			cr, err := m.credencialesDe(canal)
			if err != nil {
				return medioPorBajar{}, err
			}
			return medioPorBajar{MensajeID: id, medio: *f.MedioID, token: cr.Token, tipo: f.Tipo, conv: c}, nil
		},
	})
}

func nuevoGuardarMedio(m *Modulo) *auditoria.Caso[medioBajado, struct{}] {
	return auditoria.Nuevo(auditoria.Caso[medioBajado, struct{}]{
		Accion: "mensajeria.guardar_medio", Acceso: auditoria.Autenticado, AuditarExito: func(struct{}) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in medioBajado) (struct{}, error) {
			q := mensajeriabd.New(t)
			f, err := q.MensajeConversacionParaActualizar(ctx, in.MensajeID)
			if err != nil {
				return struct{}{}, err
			}
			if f.MedioID == nil || uuidDe(f.AdjuntoID) != nil {
				return struct{}{}, nil
			}
			c, err := q.ConversacionClientePorID(ctx, f.ConversacionID)
			if err != nil {
				return struct{}{}, err
			}
			t.FijarEmpresa(empresaDeConv(c))
			t.FijarEntidad("mensaje_conversacion", f.ID)
			if in.Error != "" {
				// No se pudo: el mensaje queda sin archivo y lo dice.
				motivo := "medio_no_disponible"
				if _, err := q.MarcarEnvioMensajeEntrante(ctx, mensajeriabd.MarcarEnvioMensajeEntranteParams{ID: f.ID, MotivoFalla: &motivo,
					DetalleFalla: nombrePtr(recortar(in.Error, 300))}); err != nil {
					return struct{}{}, err
				}
				t.RegistrarCambios("mensaje_conversacion", f.ID, map[string]any{"medio": "pendiente"}, map[string]any{"medio": "no_disponible"})
				return struct{}{}, m.emitirEventoConv(ctx, t, c, EventoConvMensajeActualizad, &f.ID, nil)
			}
			id, err := uuid.NewV7()
			if err != nil {
				return struct{}{}, err
			}
			tipo := "documento"
			if strings.HasPrefix(in.Mime, "image/") {
				tipo = "foto"
			}
			if _, err := adjuntos.GuardarCompleto(ctx, t, m.cfg.Almacen, id, adjuntos.EntradaCompleto{EmpresaID: c.EmpresaID,
				Entidad: EntidadAdjuntosConversacion, EntidadID: strconv.FormatInt(c.ID, 10), Tipo: tipo, Mime: in.Mime, Datos: in.datos}); err != nil {
				return struct{}{}, err
			}
			if _, err := q.FijarAdjuntoMensaje(ctx, mensajeriabd.FijarAdjuntoMensajeParams{ID: f.ID, AdjuntoID: pgUUID(&id)}); err != nil {
				return struct{}{}, err
			}
			return struct{}{}, m.emitirEventoConv(ctx, t, c, EventoConvMensajeActualizad, &f.ID, nil)
		},
	})
}

// descargarMedio baja el archivo de Meta (fuera de transacción) y lo guarda en el módulo de
// adjuntos. ultimo: es el último intento (si falla, el mensaje queda sin archivo).
func (m *Modulo) descargarMedio(ctx context.Context, id uuid.UUID, ultimo bool) error {
	guardar := m.guardarMedio
	p, err := m.prepararMedio.Ejecutar(ctx, id)
	if err != nil || p.nada {
		return err
	}
	if m.cfg.Almacen == nil {
		slog.WarnContext(ctx, "mensajeria: sin almacenamiento de adjuntos; el medio no se descarga", "mensaje_id", id)
		_, err := guardar.Ejecutar(ctx, medioBajado{MensajeID: id, Error: "sin almacenamiento de adjuntos"})
		return err
	}
	datos, mime, err := m.bajarDeMeta(ctx, p.medio, p.token)
	var perm errMedioPermanente
	switch {
	case errors.As(err, &perm):
		_, err := guardar.Ejecutar(ctx, medioBajado{MensajeID: id, Error: perm.motivo})
		return err
	case err != nil && ultimo:
		_, err2 := guardar.Ejecutar(ctx, medioBajado{MensajeID: id, Error: err.Error()})
		return err2
	case err != nil:
		return err
	}
	_, err = guardar.Ejecutar(ctx, medioBajado{MensajeID: id, Mime: mime, Bytes: len(datos), datos: datos})
	return err
}

// bajarDeMeta: GET /{media_id} (url y mime) y luego la url, las dos con el token del canal.
func (m *Modulo) bajarDeMeta(ctx context.Context, medio, token string) ([]byte, string, error) {
	pedir := func(url string) (*http.Response, error) {
		ctx, cancelar := context.WithTimeout(ctx, 60*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			cancelar()
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("User-Agent", "Iris/"+m.cfg.Version)
		resp, err := m.cfg.HTTPMeta.Do(req)
		if err != nil {
			cancelar()
			return nil, errors.New("meta: sin respuesta")
		}
		resp.Body = struct {
			io.Reader
			io.Closer
		}{resp.Body, cerrarYCancelar{resp.Body, cancelar}}
		return resp, nil
	}
	resp, err := pedir(m.cfg.MetaURL + "/" + medio)
	if err != nil {
		return nil, "", err
	}
	var info struct {
		URL      string `json:"url"`
		MimeType string `json:"mime_type"`
		FileSize int64  `json:"file_size"`
	}
	cuerpo, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest:
		return nil, "", errMedioPermanente{motivo: fmt.Sprintf("meta %d: el medio ya no está disponible", resp.StatusCode)}
	case resp.StatusCode/100 != 2:
		return nil, "", fmt.Errorf("meta %d al leer el medio", resp.StatusCode)
	}
	if err := json.Unmarshal(cuerpo, &info); err != nil || info.URL == "" {
		return nil, "", errMedioPermanente{motivo: "meta: respuesta sin url"}
	}
	if info.FileSize > m.cfg.MaxMedioEntrante {
		return nil, "", errMedioPermanente{motivo: fmt.Sprintf("medio de %d bytes (máximo %d)", info.FileSize, m.cfg.MaxMedioEntrante)}
	}
	resp, err = pedir(info.URL)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return nil, "", fmt.Errorf("meta %d al descargar el medio", resp.StatusCode)
	}
	datos, err := io.ReadAll(io.LimitReader(resp.Body, m.cfg.MaxMedioEntrante+1))
	if err != nil {
		return nil, "", errors.New("meta: descarga cortada")
	}
	if int64(len(datos)) > m.cfg.MaxMedioEntrante {
		return nil, "", errMedioPermanente{motivo: fmt.Sprintf("medio de más de %d bytes", m.cfg.MaxMedioEntrante)}
	}
	if len(datos) == 0 {
		return nil, "", errMedioPermanente{motivo: "medio vacío"}
	}
	return datos, mimeDeBytes(datos, info.MimeType), nil
}

// mimeDeBytes: el tipo que dicen los bytes (imágenes, PDF…). Lo que Meta declara solo se usa para
// audio y video que los bytes no identifican; nunca para hacer pasar algo por imagen o PDF.
func mimeDeBytes(datos []byte, declarado string) string {
	detectado := strings.ToLower(strings.TrimSpace(strings.Split(http.DetectContentType(datos), ";")[0]))
	decl := strings.ToLower(strings.TrimSpace(strings.Split(declarado, ";")[0]))
	if detectado == "application/octet-stream" && (strings.HasPrefix(decl, "audio/") || strings.HasPrefix(decl, "video/")) {
		return decl
	}
	return detectado
}

type cerrarYCancelar struct {
	c        io.Closer
	cancelar context.CancelFunc
}

func (c cerrarYCancelar) Close() error {
	defer c.cancelar()
	return c.c.Close()
}

// Adjuntos de la conversación (POST /api/v1/adjuntos con entidad conversacion) ------------------------

func registrarAdjuntosConversacion() {
	adjuntos.RegistrarEntidadAdjuntable(EntidadAdjuntosConversacion, func(ctx context.Context, db adjuntos.DBTX, id string, uso adjuntos.Uso) (int64, error) {
		conv, err := strconv.ParseInt(id, 10, 64)
		if err != nil || conv <= 0 {
			return 0, conversacionClienteNoEncontrada()
		}
		p := contexto.PermisosDe(ctx)
		if !p.Tiene(string(nucleo.PermConversacionesVer)) || (uso == adjuntos.UsoSubir && !p.Tiene(string(nucleo.PermConversacionesResponder))) {
			return 0, conversacionClienteNoEncontrada()
		}
		a, err := alcanceConvDe(ctx)
		if err != nil {
			return 0, conversacionClienteNoEncontrada()
		}
		c, err := convVisible(ctx, mensajeriabd.New(db), a, conv, false)
		if err != nil {
			return 0, err
		}
		return empresaDeConv(c), nil
	})
}
