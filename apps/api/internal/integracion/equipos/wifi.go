// Package equipos: los cambios de Wi-Fi del equipo del cliente pedidos a Argos (contrato
// argosisp.md 0.9.0 §12.4, pedidos B12 y B14) desde la orden, el ticket o la conversación (enlace de
// un solo uso del asistente). Argos es el único que habla con SmartOLT y el ACS: Iris nunca guarda
// sus credenciales. La clave Wi-Fi viaja cifrada en el depósito de tránsito de la cola de pendientes
// y se borra al 2xx de Argos (o a su rechazo); aquí solo queda si se pidió (con_clave), nunca la clave
// ni el SSID. El resultado llega por el webhook equipo.wifi_resuelto (argos.wifi_resuelto).
package equipos

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	equiposbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/equipos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Orígenes del cambio (origen_tipo de §12.4).
const (
	OrigenOrden        = "iris_orden"
	OrigenTicket       = "iris_ticket"
	OrigenConversacion = "iris_conversacion"
)

// Métodos de verificación del titular (§12.4).
const (
	MetodoPresencial        = "presencial"
	MetodoLlamadaVerificada = "llamada_verificada"
	MetodoCodigoTelefono    = "codigo_telefono"
	MetodoCodigoCorreo      = "codigo_correo"
	MetodoDatosTitular      = "datos_titular"
)

// Estados de un cambio.
const (
	EstadoPendiente = "pendiente"
	EstadoEnProceso = "en_proceso"
	EstadoAplicado  = "aplicado"
	EstadoFallido   = "fallido"
)

// Eventos (catálogo de CLAUDE.md; nunca el SSID ni la clave).
const (
	EventoCambioPedido   = "wifi.cambio_pedido"
	EventoCambioResuelto = "wifi.cambio_resuelto"
)

// VigenciaVerificacion: la verificación §7.5 vale 10 min para un solo cambio (§12.4).
const VigenciaVerificacion = 10 * time.Minute

func verificacionRequerida() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusUnprocessableEntity, "verificacion_requerida", "Verificación requerida",
		"Confirma al titular con el desafío de identidad (vale 10 minutos y para un solo cambio)",
		nucleo.ErrorCampo{Field: "verificacionId", Message: "Falta una verificación vigente y sin usar del titular", Rule: "verificacion"})
}

// MaxCambiosDia: Argos acepta 3 cambios por servicio al día (§12.4); Iris no encola un cuarto.
const MaxCambiosDia = 3

// ActorAsistente es X-Iris-Actor del asistente de IA (§7.1).
const ActorAsistente = "asistente_ia"

// Pedido es un cambio de Wi-Fi a encolar en la transacción del caso de uso que lo origina.
type Pedido struct {
	EmpresaID  *int64
	ServicioID int64
	ClienteID  int64
	OrigenTipo string
	OrigenID   int64
	// Actor: usuario:<id> o asistente_ia (X-Iris-Actor).
	Actor     string
	UsuarioID *int64
	// Verificación del titular (§12.4): método y cuándo.
	Metodo       string
	VerificadoEn time.Time
	// VerificacionID: la verificación §7.5 que respalda el cambio (de un solo uso); con ella, VerificadoEn
	// es la de la fila. Obligatoria salvo el técnico en sitio (presencial).
	VerificacionID *int64
	// EnlaceID: el enlace del asistente que usa la verificación (no cuenta como otro uso).
	EnlaceID *uuid.UUID
	// RevalidarServicio: comprobar en la instantánea que el servicio sigue siendo del cliente (enlace).
	RevalidarServicio bool
	SSID              *string
	// Clave: solo en memoria; va al depósito cifrado.
	Clave *string
	Ahora time.Time
	// Zona del día para el límite (America/Guatemala).
	Zona *time.Location
}

// CambioWifi es lo que se muestra de un cambio (sin el SSID ni la clave).
type CambioWifi struct {
	ID            uuid.UUID `json:"id"`
	Estado        string    `json:"estado" enum:"pendiente,en_proceso,aplicado,fallido"`
	OrigenTipo    string    `json:"origenTipo" enum:"iris_orden,iris_ticket,iris_conversacion"`
	Metodo        string    `json:"metodo"`
	ConSSID       bool      `json:"conSsid"`
	ConClave      bool      `json:"conClave"`
	Causa         *string   `json:"causa" nullable:"true" doc:"Con fallido: equipo_sin_gestion, equipo_sin_respuesta, rechazado_por_equipo, otro, o el código con que Argos rechazó el pedido"`
	Detalle       *string   `json:"detalle" nullable:"true"`
	CreadoEn      time.Time `json:"creadoEn"`
	ActualizadoEn time.Time `json:"actualizadoEn"`
}

func aCambio(f equiposbd.IntegracionCambiosWifi) CambioWifi {
	return CambioWifi{ID: f.ID, Estado: f.Estado, OrigenTipo: f.OrigenTipo, Metodo: f.Metodo, ConSSID: f.ConSsid, ConClave: f.ConClave,
		Causa: f.Causa, Detalle: f.Detalle, CreadoEn: f.CreatedAt, ActualizadoEn: f.UpdatedAt}
}

// entidadDe: la entidad del pendiente y del depósito según el origen.
func entidadDe(origen string) (entidad, corto string) {
	switch origen {
	case OrigenTicket:
		return pendientes.EntidadTransitoTicket, "ticket"
	case OrigenConversacion:
		return pendientes.EntidadTransitoConversacion, "conversacion"
	}
	return pendientes.EntidadTransitoOrden, "orden"
}

// Encolar valida el cambio (SSID y clave, límite del día) y lo encola hacia Argos con su
// verificación. La clave va al depósito cifrado; nunca a la fila, al payload ni a la auditoría.
func Encolar(ctx context.Context, t *auditoria.Tx, cif *nucleo.Cifrador, p Pedido, aviso pendientes.Encolador) (CambioWifi, error) {
	if p.SSID == nil && p.Clave == nil {
		return CambioWifi{}, nucleo.Validacion(nucleo.ErrorCampo{Field: "ssid", Message: "Escribe el nombre o la clave nueva del Wi-Fi", Rule: "required"})
	}
	if p.SSID != nil {
		if err := ValidarSSID("ssid", *p.SSID); err != nil {
			return CambioWifi{}, err
		}
	}
	if p.Clave != nil {
		if err := ValidarClave("clave", *p.Clave); err != nil {
			return CambioWifi{}, err
		}
	}
	q := equiposbd.New(t)
	if p.Metodo != MetodoPresencial {
		if p.VerificacionID == nil {
			return CambioWifi{}, verificacionRequerida()
		}
		v, err := q.VerificacionParaPanel(ctx, equiposbd.VerificacionParaPanelParams{ID: *p.VerificacionID, ClienteID: p.ClienteID, EnlaceID: uuidPG(p.EnlaceID),
			Desde: p.Ahora.Add(-VigenciaVerificacion)})
		if errors.Is(err, pgx.ErrNoRows) {
			return CambioWifi{}, verificacionRequerida()
		}
		if err != nil {
			return CambioWifi{}, err
		}
		p.VerificadoEn = v.CreadaEn
	}
	if p.RevalidarServicio {
		ok, err := q.ServicioEsDelCliente(ctx, equiposbd.ServicioEsDelClienteParams{ServicioID: p.ServicioID, ClienteID: p.ClienteID})
		if err != nil {
			return CambioWifi{}, err
		}
		if !ok {
			return CambioWifi{}, nucleo.ReglaNegocio("servicio_de_otro_cliente", "El servicio ya no es de este cliente")
		}
	}
	zona := p.Zona
	if zona == nil {
		zona = time.UTC
	}
	l := p.Ahora.In(zona)
	desde := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, zona).UTC()
	// Candado por servicio: dos pedidos a la vez no pasan los dos el límite.
	if _, err := t.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('wifi:' || $1::bigint))`, p.ServicioID); err != nil {
		return CambioWifi{}, err
	}
	n, err := q.CambiosWifiDelDia(ctx, equiposbd.CambiosWifiDelDiaParams{ServicioID: p.ServicioID, Desde: desde})
	if err != nil {
		return CambioWifi{}, err
	}
	if n >= MaxCambiosDia {
		manana := desde.Add(24 * time.Hour)
		return CambioWifi{}, nucleo.NuevoProblema(http.StatusTooManyRequests, "demasiados_cambios_wifi", "Demasiados cambios de Wi-Fi",
			"El Wi-Fi de este servicio ya se cambió 3 veces hoy").ConCabecera("Retry-After", strconv.FormatInt(int64(manana.Sub(p.Ahora).Seconds())+1, 10))
	}
	id, err := uuid.NewV7()
	if err != nil {
		return CambioWifi{}, err
	}
	entidad, corto := entidadDe(p.OrigenTipo)
	clave := pendientes.ClaveWifi(corto, p.OrigenID, id.String())
	cuerpo := map[string]any{"origen_tipo": p.OrigenTipo, "origen_id": strconv.FormatInt(p.OrigenID, 10),
		"verificacion": map[string]any{"metodo": p.Metodo, "verificado_en": p.VerificadoEn.UTC().Format(time.RFC3339), "cliente_id": p.ClienteID}}
	if p.SSID != nil {
		cuerpo["ssid"] = *p.SSID
	}
	var transito *pendientes.Transito
	if p.Clave != nil {
		transito = &pendientes.Transito{Campos: map[string]any{"clave": *p.Clave}}
	}
	crudo, err := json.Marshal(cuerpo)
	if err != nil {
		return CambioWifi{}, err
	}
	reg, _, err := pendientes.AccionConTransito(ctx, t, cif, pendientes.Accion{
		EmpresaID: p.EmpresaID, Operacion: pendientes.OpCambiarWifi, IdempotencyKey: clave, Entidad: entidad,
		EntidadID: strconv.FormatInt(p.OrigenID, 10), Ruta: "/servicios/" + strconv.FormatInt(p.ServicioID, 10) + "/wifi",
		Cuerpo: crudo, Desde: p.Ahora, Actor: p.Actor,
	}, transito, aviso)
	if err != nil {
		return CambioWifi{}, err
	}
	f, err := q.CrearCambioWifi(ctx, equiposbd.CrearCambioWifiParams{
		ID: id, EmpresaID: p.EmpresaID, ServicioID: p.ServicioID, ClienteID: p.ClienteID, OrigenTipo: p.OrigenTipo,
		OrigenID: strconv.FormatInt(p.OrigenID, 10), Actor: p.Actor, UsuarioID: p.UsuarioID, Metodo: p.Metodo,
		VerificadoEn: p.VerificadoEn.UTC(), ConSsid: p.SSID != nil, ConClave: p.Clave != nil, IdempotencyKey: clave, PendienteID: &reg.ID,
		VerificacionID: p.VerificacionID,
	})
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return CambioWifi{}, verificacionRequerida() // la verificación ya se usó (otro pedido a la vez)
	}
	if err != nil {
		return CambioWifi{}, err
	}
	t.RegistrarCambios("cambio_wifi", f.ID, nil, map[string]any{"servicio_id": f.ServicioID, "origen_tipo": f.OrigenTipo,
		"metodo": f.Metodo, "con_ssid": f.ConSsid, "con_clave": f.ConClave, "pendiente_id": reg.ID})
	return aCambio(f), t.Emitir(EventoCambioPedido, map[string]any{
		"cambio_id": f.ID, "empresa_id": f.EmpresaID, "servicio_id": f.ServicioID, "origen_tipo": f.OrigenTipo, "origen_id": f.OrigenID,
		"actor": f.Actor, "metodo": f.Metodo, "con_ssid": f.ConSsid, "con_clave": f.ConClave,
	})
}

// DeOrigen: los últimos cambios pedidos desde una orden, un ticket o una conversación.
func DeOrigen(ctx context.Context, q equiposbd.DBTX, origen string, id int64) ([]CambioWifi, error) {
	filas, err := equiposbd.New(q).CambiosWifiDeOrigen(ctx, equiposbd.CambiosWifiDeOrigenParams{OrigenTipo: origen, OrigenID: strconv.FormatInt(id, 10)})
	if err != nil {
		return nil, err
	}
	r := make([]CambioWifi, 0, len(filas))
	for _, f := range filas {
		r = append(r, aCambio(f))
	}
	return r, nil
}

// ValidarSSID: 1 a 32 caracteres, sin caracteres de control (§12.4).
func ValidarSSID(campo, s string) error {
	if len([]rune(s)) < 1 || len(s) > 32 {
		return nucleo.Validacion(nucleo.ErrorCampo{Field: campo, Message: "El nombre del Wi-Fi lleva de 1 a 32 caracteres", Rule: "wifi"})
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return nucleo.Validacion(nucleo.ErrorCampo{Field: campo, Message: "El nombre del Wi-Fi no lleva caracteres de control", Rule: "wifi"})
		}
	}
	return nil
}

// ValidarClave: 8 a 63 caracteres ASCII imprimibles (WPA2). El error nunca repite la clave.
func ValidarClave(campo, c string) error {
	if len(c) < 8 || len(c) > 63 {
		return nucleo.Validacion(nucleo.ErrorCampo{Field: campo, Message: "La clave del Wi-Fi lleva de 8 a 63 caracteres", Rule: "wifi"})
	}
	for i := 0; i < len(c); i++ {
		if c[i] < 0x20 || c[i] > 0x7e {
			return nucleo.Validacion(nucleo.ErrorCampo{Field: campo, Message: "La clave del Wi-Fi solo lleva letras, números y signos", Rule: "wifi"})
		}
	}
	return nil
}

// --- Resultado ---------------------------------------------------------------------------------

// Modulo resuelve los cambios con la cola y los webhooks.
type Modulo struct {
	resolver *auditoria.Caso[entradaResolver, *CambioWifi]
}

// Nuevo arma el módulo.
func Nuevo() *Modulo {
	m := &Modulo{}
	m.resolver = auditoria.Nuevo(auditoria.Caso[entradaResolver, *CambioWifi]{
		Accion: "integraciones.resolver_cambio_wifi", Acceso: auditoria.Autenticado,
		AuditarExito: func(r *CambioWifi) bool { return r != nil },
		Logica:       resolver,
	})
	return m
}

type entradaResolver struct {
	IdempotencyKey *string `json:"idempotency_key,omitempty"`
	SolicitudID    *int64  `json:"solicitud_id,omitempty"`
	Estado         string  `json:"estado"`
	Causa          *string `json:"causa,omitempty"`
	Detalle        *string `json:"detalle,omitempty"`
	EntranteID     *int64  `json:"entrante_id,omitempty"`
	ServicioID     *int64  `json:"servicio_id,omitempty"`
	EmpresaID      *int64  `json:"empresa_id,omitempty"`
}

func valida(de, a string) bool {
	switch de {
	case EstadoPendiente:
		return a == EstadoEnProceso || a == EstadoAplicado || a == EstadoFallido
	case EstadoEnProceso:
		return a == EstadoAplicado || a == EstadoFallido
	}
	return false
}

func resolver(ctx context.Context, t *auditoria.Tx, in entradaResolver) (*CambioWifi, error) {
	q := equiposbd.New(t)
	var f equiposbd.IntegracionCambiosWifi
	var err error
	switch {
	case in.IdempotencyKey != nil:
		f, err = q.CambioWifiPorClave(ctx, *in.IdempotencyKey)
		if errors.Is(err, pgx.ErrNoRows) && in.SolicitudID != nil {
			f, err = q.CambioWifiPorSolicitud(ctx, *in.SolicitudID)
		}
	case in.SolicitudID != nil:
		f, err = q.CambioWifiPorSolicitud(ctx, *in.SolicitudID)
	default:
		return nil, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.FijarEntidad("cambio_wifi", f.ID)
	if (in.ServicioID != nil && *in.ServicioID != f.ServicioID) || (in.EmpresaID != nil && !equalPtr(in.EmpresaID, f.EmpresaID)) {
		t.Anotar(map[string]any{"webhook_de_otro_servicio": true})
		return nil, nil
	}
	if f.EmpresaID != nil {
		t.FijarEmpresa(*f.EmpresaID)
	}
	sol := f.SolicitudID
	if sol == nil {
		sol = in.SolicitudID
	}
	cambia := !equalPtr(sol, f.SolicitudID)
	estado, causa, detalle, entrante := f.Estado, f.Causa, f.Detalle, f.EntranteID
	if in.Estado != f.Estado && valida(f.Estado, in.Estado) {
		estado, cambia = in.Estado, true
		if in.Estado == EstadoFallido {
			causa, detalle = in.Causa, in.Detalle
		}
		if in.EntranteID != nil {
			entrante = in.EntranteID
		}
	}
	if !cambia {
		return nil, nil
	}
	d, err := q.GuardarCambioWifi(ctx, equiposbd.GuardarCambioWifiParams{ID: f.ID, Estado: estado, SolicitudID: sol, Causa: causa,
		Detalle: detalle, EntranteID: entrante})
	if err != nil {
		return nil, err
	}
	t.RegistrarCambios("cambio_wifi", d.ID, map[string]any{"estado": f.Estado}, map[string]any{"estado": d.Estado, "causa": d.Causa})
	c := aCambio(d)
	if d.Estado == f.Estado {
		return &c, nil
	}
	return &c, t.Emitir(EventoCambioResuelto, map[string]any{
		"cambio_id": d.ID, "empresa_id": d.EmpresaID, "servicio_id": d.ServicioID, "origen_tipo": d.OrigenTipo, "origen_id": d.OrigenID,
		"de": f.Estado, "a": d.Estado, "causa": d.Causa,
	})
}

func equalPtr(a, b *int64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

// Suscribir conecta el resultado de la cola (202 de Argos o su rechazo) y el webhook
// equipo.wifi_resuelto (cmd/worker). Idempotente: un cambio resuelto no cambia.
func (m *Modulo) Suscribir(bus *integracion.Bus, bd nucleo.BD, log *slog.Logger) {
	pendiente := func(ctx context.Context, e integracion.Evento) error {
		var p struct {
			Operacion      string `json:"operacion"`
			IdempotencyKey string `json:"idempotency_key"`
		}
		if json.Unmarshal(e.Payload, &p) != nil || p.Operacion != pendientes.OpCambiarWifi {
			return nil
		}
		ctx = nucleo.ConBD(ctx, bd)
		reg, err := pendientes.PorClave(ctx, bd, p.IdempotencyKey)
		if err != nil {
			return nil
		}
		in := entradaResolver{IdempotencyKey: &p.IdempotencyKey}
		switch e.Evento {
		case pendientes.EventoHecho:
			in.Estado = EstadoEnProceso
			if v, ok := pendientes.CampoDeRespuesta(reg.Respuesta, "solicitud_id"); ok {
				var n json.Number
				if json.Unmarshal(v, &n) == nil {
					if s, err := n.Int64(); err == nil && s > 0 {
						in.SolicitudID = &s
					}
				}
			}
		case pendientes.EventoFallido, pendientes.EventoResueltoManual:
			in.Estado, in.Causa, in.Detalle = EstadoFallido, reg.CodigoError, reg.UltimoError
		default:
			return nil
		}
		_, err = m.resolver.Ejecutar(ctx, in)
		return err
	}
	for _, ev := range []string{pendientes.EventoHecho, pendientes.EventoFallido, pendientes.EventoResueltoManual} {
		bus.Suscribir("equipos.wifi_pendiente", ev, pendiente)
	}
	bus.Suscribir("equipos.wifi_webhook", argos.EventoWifiResueltoInterno, func(ctx context.Context, e integracion.Evento) error {
		var p struct {
			SolicitudID    *int64  `json:"solicitud_id"`
			IdempotencyKey *string `json:"idempotency_key"`
			Estado         string  `json:"estado"`
			Causa          *string `json:"causa"`
			EntranteID     *int64  `json:"entrante_id"`
			ServicioID     *int64  `json:"servicio_id"`
			EmpresaID      *int64  `json:"empresa_id"`
		}
		if json.Unmarshal(e.Payload, &p) != nil {
			return nil
		}
		_, err := m.resolver.Ejecutar(nucleo.ConBD(ctx, bd), entradaResolver{IdempotencyKey: p.IdempotencyKey, SolicitudID: p.SolicitudID,
			Estado: p.Estado, Causa: p.Causa, EntranteID: p.EntranteID, ServicioID: p.ServicioID, EmpresaID: p.EmpresaID})
		if err != nil {
			log.WarnContext(ctx, "equipos: no se pudo guardar el resultado del cambio de Wi-Fi", "evento", e.ID, "err", err)
		}
		return err
	})
}

func uuidPG(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: *id, Valid: true}
}
