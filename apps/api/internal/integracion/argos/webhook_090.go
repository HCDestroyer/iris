package argos

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	conectorbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/conector/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Contrato 0.9.0, bloque E (§12) y cobro de la instalación (A36): webhooks de Argos hacia Iris y
// las lecturas de los equipos. Argos solo los manda a un conector con contrato ≥ 0.9.0; Iris los
// convierte en eventos internos (outbox) que consumen campo y la cola de cambios de Wi-Fi. Nunca
// llevan la clave Wi-Fi, el SSID ni credenciales: lo que no está en la lista blanca se descarta.

// Webhooks de la 0.9.0.
const (
	EventoInstalacionAprovisionada = "instalacion.aprovisionada"
	EventoInstalacionFallida       = "instalacion.fallida"
	EventoWifiResuelto             = "equipo.wifi_resuelto"
	EventoBienvenidaCambiada       = "servicio.bienvenida_cambiada"
	EventoFacturaEmitida           = "factura.emitida"
	EventoFacturaAnulada           = "factura.anulada"
	EventoPagoRegistrado           = "pago.registrado"
	EventoPagoAnulado              = "pago.anulado"
)

// Eventos internos (outbox) de la 0.9.0.
const (
	// EventoInstalacionResuelta: instalacion.aprovisionada o .fallida (campo actualiza la activación).
	EventoInstalacionResuelta = "argos.instalacion_resuelta"
	// EventoWifiResueltoInterno: equipo.wifi_resuelto (la cola de cambios de Wi-Fi lo guarda).
	EventoWifiResueltoInterno = "argos.wifi_resuelto"
	// EventoBienvenidaCambiadaInterno: servicio.bienvenida_cambiada (campo actualiza la bienvenida).
	EventoBienvenidaCambiadaInterno = "argos.bienvenida_cambiada"
	// EventoFacturaCambiada: factura.* y pago.* de una orden de Iris (campo sigue el cobro).
	EventoFacturaCambiada = "argos.factura_cambiada"
)

// ContratoEquipos es el primer contrato con el bloque E (§12) y la bienvenida de §12.5.
const ContratoEquipos = "0.9.0"

// SoportaEquipos: Argos informó un contrato ≥ 0.9.0.
func SoportaEquipos(contrato *string) bool { return conector.VersionAlMenos(contrato, ContratoEquipos) }

// CapacidadAprovisionamiento es lo que Argos puede aprovisionar en una empresa (§4.3).
type CapacidadAprovisionamiento struct {
	EmpresaID int64 `json:"empresa_id"`
	FTTH      bool  `json:"ftth"`
	Wireless  bool  `json:"wireless"`
	TR069     bool  `json:"tr069"`
}

// capacidadesEstado es `capacidades` de GET /integraciones/iris/estado (0.9.0 §4.3; equipos desde 0.14.3).
type capacidadesEstado struct {
	Aprovisionamiento []CapacidadAprovisionamiento `json:"aprovisionamiento"`
	Equipos           []CapacidadEquipos           `json:"equipos"`
}

// CapacidadEquipos: las funciones del bloque E por empresa (0.14.3 §4.3).
type CapacidadEquipos struct {
	EmpresaID    int64 `json:"empresa_id"`
	EstadoEquipo bool  `json:"estado_equipo"`
	Wifi         bool  `json:"wifi"`
	Bienvenida   bool  `json:"bienvenida"`
	CobroEquipo  bool  `json:"cobro_equipo"`
}

// Funciones de capacidades.equipos (0.14.3).
const (
	FuncionEstadoEquipo = "estado_equipo"
	FuncionWifi         = "wifi"
	FuncionBienvenida   = "bienvenida"
	FuncionCobroEquipo  = "cobro_equipo"
)

// CodigoCapacidadNoDisponible: Argos (0.14.3) no tiene la función en true para la empresa.
const CodigoCapacidadNoDisponible = "capacidad_no_disponible"

// Tecnologías de la activación (§12.1).
const (
	TecnologiaFTTH     = "ftth"
	TecnologiaWireless = "wireless"
	TecnologiaTR069    = "tr069"
)

// CapacidadSoloContrato: para Aprovisionamiento, solo el contrato ≥ 0.9.0 (el equipo y el Wi-Fi, que
// no dependen de la marca de aprovisionamiento).
const CapacidadSoloContrato = "contrato"

// Aprovisionamiento dice si Iris puede ofrecer «Activar servicio» en la empresa con esa tecnología
// (contrato ≥ 0.9.0 y la marca de capacidades.aprovisionamiento en true). Sin conector activo,
// sin contrato 0.9.0 o sin la marca: false (Iris activa al cerrar, §4.3). tecnologia vacía: alguna.
func (m *Modulo) Aprovisionamiento(ctx context.Context, q conector.DBTX, empresa *int64, tecnologia string) (bool, error) {
	var contrato *string
	var crudo []byte
	err := q.QueryRow(ctx, `SELECT contrato_remoto, capacidades_aprovisionamiento FROM integracion.conectores
		WHERE sistema = $1 AND estado = 'activo' ORDER BY id DESC LIMIT 1`, Sistema).Scan(&contrato, &crudo)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if tecnologia == CapacidadSoloContrato {
		return SoportaEquipos(contrato), nil
	}
	if f, ok := strings.CutPrefix(tecnologia, "funcion:"); ok {
		return m.funcionEquipos(ctx, q, contrato, empresa, f)
	}
	if !SoportaEquipos(contrato) || len(crudo) == 0 || empresa == nil {
		return false, nil
	}
	var caps []CapacidadAprovisionamiento
	if json.Unmarshal(crudo, &caps) != nil {
		return false, nil
	}
	for _, c := range caps {
		if c.EmpresaID != *empresa {
			continue
		}
		switch tecnologia {
		case TecnologiaFTTH:
			return c.FTTH, nil
		case TecnologiaWireless:
			return c.Wireless, nil
		case TecnologiaTR069:
			return c.TR069, nil
		default:
			return c.FTTH || c.Wireless || c.TR069, nil
		}
	}
	return false, nil
}

// funcionEquipos: la marca de capacidades.equipos (0.14.3) de la empresa. Sin el campo (Argos <
// 0.14.3), como en la 0.14.2: según el contrato informado. Una empresa sin fila cuenta como false.
func (m *Modulo) funcionEquipos(ctx context.Context, q conector.DBTX, contrato *string, empresa *int64, funcion string) (bool, error) {
	if !SoportaEquipos(contrato) {
		return false, nil
	}
	var crudo []byte
	if err := q.QueryRow(ctx, `SELECT capacidades_equipos FROM integracion.conectores WHERE sistema = $1 AND estado = 'activo'
		ORDER BY id DESC LIMIT 1`, Sistema).Scan(&crudo); err != nil {
		return false, err
	}
	if len(crudo) == 0 || string(crudo) == "null" {
		return true, nil
	}
	var caps []CapacidadEquipos
	if json.Unmarshal(crudo, &caps) != nil || empresa == nil {
		return false, nil
	}
	for _, c := range caps {
		if c.EmpresaID == *empresa {
			switch funcion {
			case FuncionEstadoEquipo:
				return c.EstadoEquipo, nil
			case FuncionWifi:
				return c.Wifi, nil
			case FuncionBienvenida:
				return c.Bienvenida, nil
			case FuncionCobroEquipo:
				return c.CobroEquipo, nil
			}
		}
	}
	return false, nil
}

// Funcion arma el argumento de Aprovisionamiento para una función de capacidades.equipos.
func Funcion(f string) string { return "funcion:" + f }

// guardarCapacidades guarda capacidades.aprovisionamiento del conector (null si Argos no la mandó).
func guardarCapacidades(ctx context.Context, q conector.DBTX, conectorID int64, caps *capacidadesEstado) error {
	var crudo []byte
	if caps != nil && caps.Aprovisionamiento != nil {
		var err error
		if crudo, err = json.Marshal(caps.Aprovisionamiento); err != nil {
			return err
		}
	}
	var equipos []byte
	if caps != nil && caps.Equipos != nil {
		var err error
		if equipos, err = json.Marshal(caps.Equipos); err != nil {
			return err
		}
	}
	_, err := q.Exec(ctx, `UPDATE integracion.conectores SET capacidades_aprovisionamiento = $2, capacidades_equipos = $3 WHERE id = $1`,
		conectorID, crudo, equipos)
	return err
}

// --- Webhooks ----------------------------------------------------------------------------------

// evento090 es un webhook de la 0.9.0 ya normalizado: el evento interno y su payload (lista blanca).
type evento090 struct {
	evento  string
	empresa *int64
	payload map[string]any
}

var (
	reCodigo  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,59}$`)
	reClaveId = regexp.MustCompile(`^iris:[a-z_]+:[A-Za-z0-9:%._-]{1,200}$`)
)

// codigo: un código de Argos de la lista cerrada de forma (minúsculas y _); otro texto, nil.
func codigo(s *string) *string {
	if s == nil || !reCodigo.MatchString(*s) {
		return nil
	}
	v := *s
	return &v
}

// detalleSeguro: texto para la persona, recortado y sin saltos de control (Argos ya lo manda sin secretos).
func detalleSeguro(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, strings.TrimSpace(*s))
	if r := []rune(v); len(r) > 300 {
		v = string(r[:300])
	}
	if v == "" {
		return nil
	}
	return &v
}

// instalacionResuelta es el payload de instalacion.aprovisionada / instalacion.fallida (§2.3).
type instalacionResuelta struct {
	AprovisionamientoID *conector.Numero `json:"aprovisionamiento_id"`
	ServicioID          *conector.Numero `json:"servicio_id"`
	EmpresaID           *conector.Numero `json:"empresa_id"`
	ClienteID           *conector.Numero `json:"cliente_id"`
	OrigenTipo          *string          `json:"origen_tipo"`
	OrigenID            *conector.Texto  `json:"origen_id"`
	IdempotencyKey      *string          `json:"idempotency_key"`
	ServicioEstado      *string          `json:"servicio_estado"`
	Causa               *string          `json:"causa"`
	Reintentable        *bool            `json:"reintentable"`
	Detalle             *string          `json:"detalle"`
	ActivadoEn          *time.Time       `json:"activado_en"`
	Equipo              *struct {
		Tipo     *string          `json:"tipo"`
		Serie    *string          `json:"serie"`
		MAC      *string          `json:"mac"`
		UnidadID *conector.Numero `json:"unidad_id"`
	} `json:"equipo"`
}

func clave(s *string) *string {
	if s == nil || !reClaveId.MatchString(*s) {
		return nil
	}
	v := *s
	return &v
}

func texto(t *conector.Texto) *string {
	if t == nil || strings.TrimSpace(string(*t)) == "" {
		return nil
	}
	v := strings.TrimSpace(string(*t))
	if len(v) > 100 {
		return nil
	}
	return &v
}

// de090 lee un webhook de la 0.9.0. omitido dice por qué se ignora (payload inválido o empresa fuera
// del alcance del conector).
func de090(e conector.Entrante, con conectorbd.IntegracionConectore) (*evento090, string) {
	switch e.Evento {
	case EventoInstalacionAprovisionada, EventoInstalacionFallida:
		var p instalacionResuelta
		if err := conector.Decodificar(e.Payload, &p); err != nil {
			return nil, "payload_invalido"
		}
		aprov, servicio, empresa := p.AprovisionamientoID.Entero(), p.ServicioID.Entero(), p.EmpresaID.Entero()
		if aprov == nil || servicio == nil || empresa == nil {
			return nil, "payload_invalido"
		}
		if !empresaEnAlcance(con, *empresa) {
			return nil, "empresa_fuera_de_alcance"
		}
		estado := "listo"
		if e.Evento == EventoInstalacionFallida {
			estado = "fallido"
		}
		d := map[string]any{"aprovisionamiento_id": *aprov, "servicio_id": *servicio, "empresa_id": *empresa,
			"cliente_id": p.ClienteID.Entero(), "origen_tipo": codigo(p.OrigenTipo), "origen_id": texto(p.OrigenID),
			"idempotency_key": clave(p.IdempotencyKey), "estado": estado, "entrante_id": e.ID}
		if estado == "listo" {
			d["servicio_estado"] = codigo(p.ServicioEstado)
			if p.Equipo != nil {
				d["equipo"] = map[string]any{"tipo": codigo(p.Equipo.Tipo), "unidad_id": p.Equipo.UnidadID.Entero()}
			}
		} else {
			causa := codigo(p.Causa)
			if causa == nil {
				otro := "otro"
				causa = &otro
			}
			d["causa"], d["reintentable"], d["detalle"] = causa, p.Reintentable, detalleSeguro(p.Detalle)
		}
		return &evento090{evento: EventoInstalacionResuelta, empresa: empresa, payload: d}, ""
	case EventoWifiResuelto:
		var p struct {
			SolicitudID    *conector.Numero `json:"solicitud_id"`
			ServicioID     *conector.Numero `json:"servicio_id"`
			EmpresaID      *conector.Numero `json:"empresa_id"`
			OrigenTipo     *string          `json:"origen_tipo"`
			OrigenID       *conector.Texto  `json:"origen_id"`
			IdempotencyKey *string          `json:"idempotency_key"`
			Estado         *string          `json:"estado"`
			Causa          *string          `json:"causa"`
		}
		if err := conector.Decodificar(e.Payload, &p); err != nil {
			return nil, "payload_invalido"
		}
		sol, servicio, empresa := p.SolicitudID.Entero(), p.ServicioID.Entero(), p.EmpresaID.Entero()
		if sol == nil || servicio == nil || empresa == nil || p.Estado == nil || (*p.Estado != "aplicado" && *p.Estado != "fallido") {
			return nil, "payload_invalido"
		}
		if !empresaEnAlcance(con, *empresa) {
			return nil, "empresa_fuera_de_alcance"
		}
		return &evento090{evento: EventoWifiResueltoInterno, empresa: empresa, payload: map[string]any{
			"solicitud_id": *sol, "servicio_id": *servicio, "empresa_id": *empresa, "origen_tipo": codigo(p.OrigenTipo),
			"origen_id": texto(p.OrigenID), "idempotency_key": clave(p.IdempotencyKey), "estado": *p.Estado,
			"causa": codigo(p.Causa), "entrante_id": e.ID,
		}}, ""
	case EventoBienvenidaCambiada:
		var p struct {
			ServicioID *conector.Numero `json:"servicio_id"`
			EmpresaID  *conector.Numero `json:"empresa_id"`
			ClienteID  *conector.Numero `json:"cliente_id"`
			OrigenTipo *string          `json:"origen_tipo"`
			OrigenID   *conector.Texto  `json:"origen_id"`
			Reenvio    bool             `json:"reenvio"`
			Canales    []struct {
				Canal   *string         `json:"canal"`
				Estado  *string         `json:"estado"`
				Motivo  *string         `json:"motivo"`
				AvisoID *conector.Texto `json:"aviso_id"`
			} `json:"canales"`
		}
		if err := conector.Decodificar(e.Payload, &p); err != nil {
			return nil, "payload_invalido"
		}
		servicio, empresa := p.ServicioID.Entero(), p.EmpresaID.Entero()
		if servicio == nil || empresa == nil {
			return nil, "payload_invalido"
		}
		if !empresaEnAlcance(con, *empresa) {
			return nil, "empresa_fuera_de_alcance"
		}
		canales := []map[string]any{}
		for _, c := range p.Canales {
			if c.Canal == nil || c.Estado == nil || !canalBienvenida[*c.Canal] || !estadoBienvenida[*c.Estado] {
				continue
			}
			canales = append(canales, map[string]any{"canal": *c.Canal, "estado": *c.Estado, "motivo": codigo(c.Motivo), "aviso_id": texto(c.AvisoID)})
		}
		return &evento090{evento: EventoBienvenidaCambiadaInterno, empresa: empresa, payload: map[string]any{
			"servicio_id": *servicio, "empresa_id": *empresa, "cliente_id": p.ClienteID.Entero(), "origen_tipo": codigo(p.OrigenTipo),
			"origen_id": texto(p.OrigenID), "reenvio": p.Reenvio, "canales": canales, "entrante_id": e.ID,
		}}, ""
	case EventoFacturaEmitida, EventoFacturaAnulada, EventoPagoRegistrado, EventoPagoAnulado:
		var p struct {
			FacturaID  *conector.Numero `json:"factura_id"`
			PagoID     *conector.Numero `json:"pago_id"`
			Numero     *conector.Texto  `json:"numero"`
			EmpresaID  *conector.Numero `json:"empresa_id"`
			OrigenTipo *string          `json:"origen_tipo"`
			OrigenID   *conector.Texto  `json:"origen_id"`
			Estado     *string          `json:"estado"`
			Total      *conector.Texto  `json:"total"`
			Saldo      *conector.Texto  `json:"saldo"`
			Moneda     *string          `json:"moneda"`
		}
		if err := conector.Decodificar(e.Payload, &p); err != nil {
			return nil, "payload_invalido"
		}
		factura, empresa := p.FacturaID.Entero(), p.EmpresaID.Entero()
		if factura == nil || empresa == nil || p.OrigenTipo == nil || *p.OrigenTipo != "iris_orden" || texto(p.OrigenID) == nil {
			return nil, "payload_invalido"
		}
		if !empresaEnAlcance(con, *empresa) {
			return nil, "empresa_fuera_de_alcance"
		}
		return &evento090{evento: EventoFacturaCambiada, empresa: empresa, payload: map[string]any{
			"webhook": e.Evento, "factura_id": *factura, "pago_id": p.PagoID.Entero(), "numero": texto(p.Numero), "empresa_id": *empresa,
			"origen_id": texto(p.OrigenID), "estado": codigo(p.Estado), "total": decimal(p.Total), "saldo": decimal(p.Saldo),
			"moneda": moneda(p.Moneda), "entrante_id": e.ID,
		}}, ""
	}
	return nil, "evento_desconocido"
}

var (
	canalBienvenida  = map[string]bool{"whatsapp": true, "correo": true, "telegram": true}
	estadoBienvenida = map[string]bool{"pendiente": true, "enviada": true, "fallida": true, "omitida": true}
	reDecimal        = regexp.MustCompile(`^-?[0-9]{1,12}(\.[0-9]{1,2})?$`)
	reMonedaISO      = regexp.MustCompile(`^[A-Z]{3}$`)
)

func decimal(t *conector.Texto) *string {
	if t == nil || !reDecimal.MatchString(strings.TrimSpace(string(*t))) {
		return nil
	}
	v := strings.TrimSpace(string(*t))
	return &v
}

func moneda(s *string) *string {
	if s == nil || !reMonedaISO.MatchString(*s) {
		return nil
	}
	v := *s
	return &v
}

// emitir090 deja el evento interno del webhook.
func emitir090(t *auditoria.Tx, ev evento090) error {
	if ev.empresa != nil && *ev.empresa != 0 {
		t.FijarEmpresa(*ev.empresa)
	}
	return t.Emitir(ev.evento, ev.payload)
}

// --- Lecturas de los equipos (§12.2 y §12.3) ---------------------------------------------------

// PasoAprovisionamiento es un paso del aprovisionamiento en Argos.
type PasoAprovisionamiento struct {
	Paso   string     `json:"paso"`
	Estado string     `json:"estado"`
	En     *time.Time `json:"en,omitempty"`
}

// Aprovisionamiento es GET /aprovisionamientos/:id (§12.2), con lo que Iris guarda.
type Aprovisionamiento struct {
	ID             int64                   `json:"aprovisionamiento_id"`
	ServicioID     int64                   `json:"servicio_id"`
	Estado         string                  `json:"estado"`
	Pasos          []PasoAprovisionamiento `json:"pasos"`
	Causa          *string                 `json:"causa"`
	Reintentable   *bool                   `json:"reintentable"`
	Detalle        *string                 `json:"detalle"`
	ServicioEstado *string                 `json:"servicio_estado"`
}

// LeerAprovisionamiento lee el estado de un aprovisionamiento en Argos (si el webhook no llegó en 10
// min). Fuera de transacción. Sin conector activo, 409 conector_apagado.
func (m *Modulo) LeerAprovisionamiento(ctx context.Context, id int64) (Aprovisionamiento, error) {
	var a Aprovisionamiento
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return a, nucleo.ErrSinBD
	}
	cli, err := m.ClienteActivo(ctx, bd)
	if err != nil {
		return a, err
	}
	var crudo struct {
		ID             *conector.Numero        `json:"aprovisionamiento_id"`
		ServicioID     *conector.Numero        `json:"servicio_id"`
		Estado         string                  `json:"estado"`
		Pasos          []PasoAprovisionamiento `json:"pasos"`
		Causa          *string                 `json:"causa"`
		Reintentable   *bool                   `json:"reintentable"`
		Detalle        *string                 `json:"detalle"`
		ServicioEstado *string                 `json:"servicio_estado"`
	}
	if err := cli.Pedir(ctx, http.MethodGet, "/aprovisionamientos/"+strconv.FormatInt(id, 10), nil, nil, true,
		&conector.Recurso{Destino: &crudo}); err != nil {
		return a, err
	}
	if crudo.ID.Entero() == nil || *crudo.ID.Entero() != id {
		return a, errors.New("argos: el aprovisionamiento leído no es el pedido")
	}
	switch crudo.Estado {
	case "en_proceso", "listo", "fallido":
	default:
		return a, errors.New("argos: estado de aprovisionamiento desconocido")
	}
	a = Aprovisionamiento{ID: id, Estado: crudo.Estado, Causa: codigo(crudo.Causa), Reintentable: crudo.Reintentable,
		Detalle: detalleSeguro(crudo.Detalle), ServicioEstado: codigo(crudo.ServicioEstado)}
	if s := crudo.ServicioID.Entero(); s != nil {
		a.ServicioID = *s
	}
	for _, p := range crudo.Pasos {
		if pasoValido[p.Paso] && estadoPasoValido[p.Estado] {
			a.Pasos = append(a.Pasos, p)
		}
	}
	if a.Estado == "fallido" && a.Causa == nil {
		otro := "otro"
		a.Causa = &otro
	}
	return a, nil
}

var (
	pasoValido       = map[string]bool{"ip": true, "equipo": true, "wifi": true, "activacion": true}
	estadoPasoValido = map[string]bool{"pendiente": true, "hecho": true, "fallido": true, "no_aplica": true}
)

// EstadoEquipo es GET /servicios/:id/equipo (§12.3). Nunca la clave Wi-Fi ni credenciales.
type EstadoEquipo struct {
	ServicioID     int64              `json:"servicioId"`
	Tecnologia     string             `json:"tecnologia" enum:"ftth,wireless"`
	Tipo           *string            `json:"tipo,omitempty" enum:"ont,cpe_wireless,router_tr069" doc:"Nunca con el asistente de IA"`
	Serie          *string            `json:"serie,omitempty" doc:"Nunca con el asistente de IA"`
	MAC            *string            `json:"mac,omitempty" doc:"Nunca con el asistente de IA"`
	Modelo         *string            `json:"modelo,omitempty" doc:"Nunca con el asistente de IA"`
	Firmware       *string            `json:"firmware,omitempty" doc:"Nunca con el asistente de IA"`
	EnLinea        bool               `json:"enLinea"`
	Senal          map[string]float64 `json:"senal" nullable:"true" doc:"ONT: rx_dbm y tx_dbm; CPE gestionado: rssi_dbm y snr_db; o null"`
	UltimoContacto *time.Time         `json:"ultimoContacto,omitempty"`
	Fuente         *string            `json:"fuente,omitempty" enum:"smartolt,acs"`
	LeidoEn        *time.Time         `json:"leidoEn,omitempty"`
}

var senalPermitida = map[string]bool{"rx_dbm": true, "tx_dbm": true, "rssi_dbm": true, "snr_db": true}

// LeerEquipo lee el estado del equipo del servicio en Argos (§12.3). asistente = true manda
// X-Iris-Actor: asistente_ia y, aunque Argos lo mandara, Iris quita serie, MAC, modelo y firmware.
// Errores de Argos: 409 equipo_sin_gestion y 503 equipo_sin_respuesta (Iris no reintenta solo).
func (m *Modulo) LeerEquipo(ctx context.Context, servicioID int64, actor string, asistente bool) (EstadoEquipo, error) {
	var r EstadoEquipo
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return r, nucleo.ErrSinBD
	}
	cli, err := m.ClienteActivo(ctx, bd)
	if err != nil {
		return r, err
	}
	c := cli.Cliente
	if actor != "" {
		c = c.ConCabecera(CabeceraActorIris, actor)
	}
	var crudo struct {
		ServicioID     *conector.Numero   `json:"servicio_id"`
		Tecnologia     string             `json:"tecnologia"`
		Tipo           *string            `json:"tipo"`
		Serie          *conector.Texto    `json:"serie"`
		MAC            *conector.Texto    `json:"mac"`
		Modelo         *conector.Texto    `json:"modelo"`
		Firmware       *conector.Texto    `json:"firmware"`
		EnLinea        bool               `json:"en_linea"`
		Senal          map[string]float64 `json:"senal"`
		UltimoContacto *time.Time         `json:"ultimo_contacto"`
		Fuente         *string            `json:"fuente"`
		LeidoEn        *time.Time         `json:"leido_en"`
	}
	if err := c.Pedir(ctx, http.MethodGet, "/servicios/"+strconv.FormatInt(servicioID, 10)+"/equipo", nil, nil, true,
		&conector.Recurso{Destino: &crudo}); err != nil {
		return r, err
	}
	if crudo.ServicioID.Entero() == nil || *crudo.ServicioID.Entero() != servicioID {
		return r, errors.New("argos: el equipo leído no es del servicio pedido")
	}
	r = EstadoEquipo{ServicioID: servicioID, Tecnologia: crudo.Tecnologia, EnLinea: crudo.EnLinea, UltimoContacto: crudo.UltimoContacto,
		LeidoEn: crudo.LeidoEn}
	if r.Tecnologia != TecnologiaFTTH && r.Tecnologia != TecnologiaWireless {
		r.Tecnologia = ""
	}
	if len(crudo.Senal) > 0 {
		r.Senal = map[string]float64{}
		for k, v := range crudo.Senal {
			if senalPermitida[k] {
				r.Senal[k] = v
			}
		}
	}
	if asistente {
		return r, nil // §12.3: con el asistente, sin serie, MAC, modelo, firmware ni datos de red
	}
	r.Tipo, r.Serie, r.MAC, r.Modelo, r.Firmware = codigo(crudo.Tipo), texto(crudo.Serie), texto(crudo.MAC), texto(crudo.Modelo), texto(crudo.Firmware)
	r.Fuente = codigo(crudo.Fuente)
	return r, nil
}

// CabeceraActorIris: quién actúa en Iris (§7.1).
const CabeceraActorIris = "X-Iris-Actor"

// --- Confirmar una cuenta recién cambiada (§12.5) -----------------------------------------------

// EntradaConfirmarCuenta: la cuenta de la instantánea (§7.8) que un admin de Iris confirma.
type EntradaConfirmarCuenta struct {
	ID   int64 `path:"id" minimum:"1"`
	Body struct {
		// CambiadaEn: lo que leyó quien revisó la cuenta; si Argos la cambió después, 409.
		CambiadaEn *time.Time `json:"cambiadaEn" nullable:"true" doc:"cambiadaEn que se revisó (null si no tenía); si la cuenta cambió después, 409 cuenta_cambiada"`
	}
}

// CuentaConfirmada es la respuesta (sin el número ni el titular).
type CuentaConfirmada struct {
	ID           int64      `json:"id"`
	EmpresaID    int64      `json:"empresaId"`
	CambiadaEn   *time.Time `json:"cambiadaEn" nullable:"true"`
	ConfirmadaEn *time.Time `json:"confirmadaEn" nullable:"true"`
}

// EventoCuentaConfirmada: un admin confirmó una cuenta recién cambiada (entra ya en la bienvenida).
const EventoCuentaConfirmada = "argos.cuenta_bancaria_confirmada"

func (m *Modulo) nuevoConfirmarCuenta() *auditoria.Caso[EntradaConfirmarCuenta, CuentaConfirmada] {
	return auditoria.Nuevo(auditoria.Caso[EntradaConfirmarCuenta, CuentaConfirmada]{
		Accion: "integraciones.confirmar_cuenta_bancaria", Permiso: nucleo.PermIntegracionesConfirmarCuentaBancaria,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaConfirmarCuenta) (CuentaConfirmada, error) {
			q := argosbd.New(t)
			t.FijarEntidad("cuenta_bancaria", in.ID)
			antes, err := q.CuentaBancariaParaActualizar(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return CuentaConfirmada{}, nucleo.NoEncontrado("La cuenta no está en la instantánea de Argos")
			}
			if err != nil {
				return CuentaConfirmada{}, err
			}
			t.FijarEmpresa(antes.EmpresaID)
			// Solo una cuenta de una empresa que el actor puede ver (la misma respuesta que si no existe).
			if !empresaAccesible(ctx, antes.EmpresaID) {
				return CuentaConfirmada{}, nucleo.NoEncontrado("La cuenta no está en la instantánea de Argos")
			}
			if !mismaHoraPtr(antes.CambiadaEn, in.Body.CambiadaEn) {
				return CuentaConfirmada{}, nucleo.Conflicto("cuenta_cambiada", "Argos cambió la cuenta después de revisarla: revísala otra vez")
			}
			a := contexto.ActorDe(ctx)
			if !a.EsUsuario() {
				return CuentaConfirmada{}, nucleo.Prohibido("La confirma una persona")
			}
			f, err := q.ConfirmarCuentaBancaria(ctx, argosbd.ConfirmarCuentaBancariaParams{ID: in.ID, UsuarioID: &a.ID})
			if err != nil {
				return CuentaConfirmada{}, err
			}
			t.RegistrarCambios("cuenta_bancaria", f.ID, map[string]any{"confirmada_en": antes.ConfirmadaEn}, map[string]any{"confirmada_en": f.ConfirmadaEn})
			return CuentaConfirmada{ID: f.ID, EmpresaID: f.EmpresaID, CambiadaEn: f.CambiadaEn, ConfirmadaEn: f.ConfirmadaEn},
				t.Emitir(EventoCuentaConfirmada, map[string]any{"cuenta_bancaria_id": f.ID, "empresa_id": f.EmpresaID, "usuario_id": a.ID})
		},
	})
}

func (m *Modulo) registrarConfirmarCuenta(api huma.API) {
	caso := m.nuevoConfirmarCuenta()
	op := conector.Operacion(conector.EtiquetaIntegraciones, "confirmarCuentaBancaria", http.MethodPost,
		"/api/v1/integraciones/argosisp/cuentas-bancarias/{id}/confirmar", "Confirmar una cuenta recién cambiada",
		nucleo.PermIntegracionesConfirmarCuentaBancaria, http.StatusOK, http.StatusNotFound)
	op.Description += " Una cuenta creada o editada en Argos no entra en la bienvenida (§12.5) durante 24 h, o hasta que un admin de Iris la confirma aquí. " +
		"Otro cambio en Argos (número, titular, banco, moneda o empresa) quita la confirmación. Emite `argos.cuenta_bancaria_confirmada`."
	huma.Register(api, op, func(ctx context.Context, in *EntradaConfirmarCuenta) (*conector.Cuerpo[CuentaConfirmada], error) {
		return conector.Responder(caso.Ejecutar(ctx, *in))
	})
}

// empresaAccesible: la empresa está entre las del actor (o el actor no tiene límite de empresa).
func empresaAccesible(ctx context.Context, empresa int64) bool {
	e := contexto.EmpresasDe(ctx)
	return e.Todas || slices.Contains(e.Disponibles, empresa) || contexto.EmpresaDe(ctx) == empresa && empresa != 0
}

func mismaHoraPtr(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Truncate(time.Microsecond).Equal(b.Truncate(time.Microsecond))
}

// ConfirmarCuentaBancaria corre el caso de uso de confirmar una cuenta (también para las pruebas).
func (m *Modulo) ConfirmarCuentaBancaria(ctx context.Context, in EntradaConfirmarCuenta) (CuentaConfirmada, error) {
	return m.nuevoConfirmarCuenta().Ejecutar(ctx, in)
}
