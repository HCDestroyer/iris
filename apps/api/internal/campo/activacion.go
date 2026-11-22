package campo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Activación desde la orden (contrato argosisp.md 0.9.0 §12.2, pedido B12; reglas del lote en
// contratos/sync.md). El técnico (también sin señal: orden.activar_servicio) o un agente desde el
// panel (también con la orden cerrada) piden a Argos que aprovisione y active el servicio:
//
//	pedir → integracion.pendientes (iris:aprovisionar:<orden>:<intento>) → 202 en_proceso
//	      → webhook instalacion.aprovisionada / .fallida (o GET /aprovisionamientos/:id a los 10 min)
//
// Lo que nunca debe pasar es activar dos veces o duplicar el comodato del equipo:
//   - la orden se toma con FOR UPDATE (la misma fila que toma el cierre) antes de crear el intento;
//   - un solo intento vivo (pendiente, en_proceso o listo) por orden y por servicio (índices únicos);
//   - cada intento tiene su clave, y un reintento de la misma operación del teléfono es «duplicada»;
//   - si el cierre ya mandó activar-instalacion (modo «al cerrar»), no se ofrece activar (409);
//   - el cierre no manda activar-instalacion ni el comodato del equipo aprovisionado si hay un
//     intento vivo o el modo no es «al cerrar» (cierre_orden.go en pendientes);
//   - los webhooks se deduplican por X-Webhook-Id y un intento ya resuelto no cambia.
//
// La clave Wi-Fi no se guarda: va cifrada al depósito de tránsito (pendientes) y se borra al 2xx de
// Argos o a su rechazo; no va en la activación, los eventos, la auditoría (***) ni los logs.

// OpActivarServicio es la operación del lote (contratos/sync.md §2).
const OpActivarServicio = "orden.activar_servicio"

// Modos de la activación por empresa o plantilla (§12.2).
const (
	ModoAlCerrar        = "al_cerrar"
	ModoExigirLista     = "exigir_lista"
	ModoPendienteAgente = "pendiente_agente"
)

// Estados de un intento.
const (
	ActivacionPendiente = "pendiente"
	ActivacionEnProceso = "en_proceso"
	ActivacionLista     = "listo"
	ActivacionFallida   = "fallido"
	// ActivacionNinguna: la orden no tiene intentos (solo en orden.cerrada y en el estado).
	ActivacionNinguna = "ninguna"
	// ActivacionFuera: el servicio se activó fuera de la orden (0.14.3; solo en orden.cerrada).
	ActivacionFuera = "activado_fuera"
)

// Eventos de la activación (catálogo de CLAUDE.md; nunca la clave Wi-Fi ni el SSID).
const (
	EventoActivacionPedida   = "orden.activacion_pedida"
	EventoActivacionCambiada = "orden.activacion_cambiada"
)

// EsperaWebhook: si Argos no avisa en 10 min, Iris consulta el GET (§12.2).
const EsperaWebhook = 10 * time.Minute

// ConfigEquipos conecta la activación y el equipo con Argos (lo arma cmd con argos.Modulo).
type ConfigEquipos struct {
	// Capacidad: Argos ≥ 0.9.0 con capacidades.aprovisionamiento para la empresa y la tecnología
	// (vacía: alguna). nil = nunca (Iris activa al cerrar, §4.3).
	Capacidad func(ctx context.Context, q pgxQuerier, empresa *int64, tecnologia string) (bool, error)
	// LeerAprovisionamiento: GET /aprovisionamientos/:id (fuera de transacción).
	LeerAprovisionamiento func(ctx context.Context, id int64) (argos.Aprovisionamiento, error)
	// LeerEquipo: GET /servicios/:id/equipo (fuera de transacción).
	LeerEquipo func(ctx context.Context, servicio int64, actor string, asistente bool) (argos.EstadoEquipo, error)
}

// pgxQuerier: lo que la capacidad necesita de la transacción.
type pgxQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// EquiposDeArgos adapta argos.Modulo para ConfigEquipos.
func EquiposDeArgos(a *argos.Modulo) ConfigEquipos {
	if a == nil {
		return ConfigEquipos{}
	}
	return ConfigEquipos{
		Capacidad: func(ctx context.Context, q pgxQuerier, empresa *int64, tecnologia string) (bool, error) {
			return a.Aprovisionamiento(ctx, q, empresa, tecnologia)
		},
		LeerAprovisionamiento: a.LeerAprovisionamiento,
		LeerEquipo:            a.LeerEquipo,
	}
}

// --- Entradas y salidas ------------------------------------------------------------------------

// EquipoActivacion es el equipo que Argos aprovisiona (§12.2).
type EquipoActivacion struct {
	Tipo     string  `json:"tipo" enum:"ont,cpe_wireless,router_tr069" doc:"ont (exige serie), cpe_wireless (exige mac) o router_tr069 (mac o serie)"`
	Serie    *string `json:"serie,omitempty" minLength:"1" maxLength:"64"`
	MAC      *string `json:"mac,omitempty" minLength:"12" maxLength:"17" doc:"AA:BB:CC:DD:EE:FF"`
	UnidadID *int64  `json:"unidadId,omitempty" minimum:"1" doc:"Unidad del inventario de Argos, si se conoce (debe estar en la bodega del técnico)"`
	Modelo   *string `json:"modelo,omitempty" minLength:"1" maxLength:"80"`
}

// PlantaActivacion: dónde se conectó la ONT (A9; solo con ont).
type PlantaActivacion struct {
	OLT    *string `json:"olt,omitempty" minLength:"1" maxLength:"60"`
	Slot   *int32  `json:"slot,omitempty" minimum:"0" maximum:"999"`
	PON    *int32  `json:"pon,omitempty" minimum:"0" maximum:"999"`
	Caja   *string `json:"caja,omitempty" minLength:"1" maxLength:"60"`
	Puerto *int32  `json:"puerto,omitempty" minimum:"0" maximum:"999"`
}

// WirelessActivacion: el AP y el sector (solo con cpe_wireless).
type WirelessActivacion struct {
	AP     *string `json:"ap,omitempty" minLength:"1" maxLength:"60"`
	Sector *string `json:"sector,omitempty" minLength:"1" maxLength:"60"`
}

// WifiActivacion: el Wi-Fi inicial que escribe el técnico. La clave nunca se guarda en Iris.
type WifiActivacion struct {
	SSID  string  `json:"ssid" minLength:"1" maxLength:"32" doc:"1 a 32 caracteres, sin caracteres de control"`
	Clave *string `json:"clave,omitempty" minLength:"8" maxLength:"63" auditoria:"sensible" doc:"8 a 63 caracteres imprimibles (WPA2). Nunca se devuelve ni queda en eventos, logs, auditoría ni en la cola: viaja cifrada hasta Argos y se borra"`
}

// EntradaActivarServicio es la entrada de ordenes.activar_servicio (y los datos de orden.activar_servicio).
type EntradaActivarServicio struct {
	ID       int64               `json:"-"`
	Equipo   EquipoActivacion    `json:"equipo"`
	Planta   *PlantaActivacion   `json:"planta,omitempty" doc:"Solo con ont"`
	Wireless *WirelessActivacion `json:"wireless,omitempty" doc:"Solo con cpe_wireless"`
	Wifi     *WifiActivacion     `json:"wifi,omitempty" doc:"Opcional. Con un equipo que Argos no gestiona (wireless sin TR-069), Argos responde wifi_no_disponible"`
	Sync     *OpSync             `json:"-"`
	// activacionID: el id del intento (el de la operación del lote o uno v7 del panel).
	activacionID uuid.UUID
}

func (e EntradaActivarServicio) ordenID() int64 { return e.ID }

// EquipoActivado es el equipo de un intento (sin el Wi-Fi).
type EquipoActivado struct {
	Tipo     string  `json:"tipo" enum:"ont,cpe_wireless,router_tr069"`
	Serie    *string `json:"serie" nullable:"true"`
	MAC      *string `json:"mac" nullable:"true"`
	UnidadID *int64  `json:"unidadId" nullable:"true"`
	Modelo   *string `json:"modelo" nullable:"true"`
}

// Activacion es un intento de activación.
type Activacion struct {
	ID                  uuid.UUID                     `json:"id"`
	Intento             int32                         `json:"intento"`
	Estado              string                        `json:"estado" enum:"pendiente,en_proceso,listo,fallido" doc:"pendiente: aún no llega a Argos (Argos caído o esperando el paso del titular); en_proceso: Argos lo está haciendo; listo: el servicio quedó activo; fallido: ver causa"`
	Origen              string                        `json:"origen" enum:"campo,panel"`
	PedidoPor           *int64                        `json:"pedidoPor" nullable:"true"`
	Equipo              EquipoActivado                `json:"equipo"`
	ConWifi             bool                          `json:"conWifi" doc:"Se pidió el Wi-Fi inicial (sin el nombre ni la clave)"`
	AprovisionamientoID *int64                        `json:"aprovisionamientoId" nullable:"true"`
	Causa               *string                       `json:"causa" nullable:"true" doc:"Con fallido: equipo_no_encontrado, equipo_en_uso, sin_puerto, perfil_sin_mapear, sin_ip_disponible, olt_sin_respuesta, acs_sin_respuesta, servicio_no_pendiente, otro, o el código de Argos que rechazó el pedido (unidad_en_otra_bodega, wifi_no_disponible…)"`
	Reintentable        *bool                         `json:"reintentable" nullable:"true"`
	Detalle             *string                       `json:"detalle" nullable:"true" doc:"Texto de Argos para la persona, sin secretos"`
	Pasos               []argos.PasoAprovisionamiento `json:"pasos" doc:"ip, equipo, wifi y activacion con su estado (si Argos los informó)"`
	CreadaEn            time.Time                     `json:"creadaEn"`
	ActualizadaEn       time.Time                     `json:"actualizadaEn"`
}

// EstadoActivacion es GET /ordenes/{id}/activacion.
type EstadoActivacion struct {
	OrdenID             int64        `json:"ordenId"`
	Modo                string       `json:"modo" enum:"al_cerrar,exigir_lista,pendiente_agente" doc:"Efectivo: al_cerrar si Argos no puede aprovisionar en la empresa (§4.3)"`
	Disponible          bool         `json:"disponible" doc:"Se puede pedir «Activar servicio» ahora (con el actual)"`
	NoDisponible        *string      `json:"noDisponible" nullable:"true" doc:"Por qué no: sin_capacidad, modo_al_cerrar, activacion_al_cerrar, activacion_en_curso, activacion_lista, no_es_instalacion, sin_servicio, orden_terminada"`
	Estado              string       `json:"estado" enum:"ninguna,pendiente,en_proceso,listo,fallido" doc:"El del último intento"`
	Actual              *Activacion  `json:"actual,omitempty" doc:"El último intento"`
	Intentos            []Activacion `json:"intentos" doc:"Todos, del último al primero"`
	SinActivacionMotivo *string      `json:"sinActivacionMotivo" nullable:"true" doc:"La orden se cerró sin la activación lista (supervisor, con motivo)"`
	ExigeBienvenida     bool         `json:"exigeBienvenida" doc:"La empresa o la plantilla exigen la bienvenida enviada para cerrar (solo con exigir_lista)"`
}

func aActivacion(f campobd.CampoActivacione) Activacion {
	a := Activacion{ID: f.ID, Intento: f.Intento, Estado: f.Estado, Origen: f.Origen, PedidoPor: f.PedidoPor,
		Equipo:  EquipoActivado{Tipo: f.EquipoTipo, Serie: f.EquipoSerie, MAC: f.EquipoMac, UnidadID: f.EquipoUnidadID, Modelo: f.EquipoModelo},
		ConWifi: f.WifiSsid != nil, AprovisionamientoID: f.AprovisionamientoID, Causa: f.Causa, Reintentable: f.Reintentable,
		Detalle: f.Detalle, Pasos: []argos.PasoAprovisionamiento{}, CreadaEn: f.CreatedAt, ActualizadaEn: f.UpdatedAt}
	if len(f.Pasos) > 0 {
		_ = json.Unmarshal(f.Pasos, &a.Pasos)
	}
	return a
}

// --- Validación -------------------------------------------------------------------------------

var (
	reSerie   = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,64}$`)
	reTextoEq = regexp.MustCompile(`^[^\x00-\x1f\x7f]+$`)
)

// ValidarSSID y ValidarClaveWifi: SSID de 1 a 32 caracteres y clave de 8 a 63 caracteres ASCII
// imprimibles, sin caracteres de control (§12.2 y §12.4, WPA2). El error nunca repite la clave.
func ValidarSSID(campo, s string) error {
	if n := utf8.RuneCountInString(s); n < 1 || n > 32 || !reTextoEq.MatchString(s) || len(s) > 32 {
		return campoInvalido(campo, "El nombre del Wi-Fi lleva de 1 a 32 caracteres, sin caracteres de control", "wifi")
	}
	return nil
}

// ValidarClaveWifi ver ValidarSSID.
func ValidarClaveWifi(campo, c string) error {
	if len(c) < 8 || len(c) > 63 {
		return campoInvalido(campo, "La clave del Wi-Fi lleva de 8 a 63 caracteres", "wifi")
	}
	for i := 0; i < len(c); i++ {
		if c[i] < 0x20 || c[i] > 0x7e {
			return campoInvalido(campo, "La clave del Wi-Fi solo lleva letras, números y signos (sin acentos ni espacios de control)", "wifi")
		}
	}
	return nil
}

func textoEquipo(campo string, v *string, max int) (*string, error) {
	if v == nil {
		return nil, nil
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(s) > max || !reTextoEq.MatchString(s) {
		return nil, campoInvalido(campo, "Texto inválido", "formato")
	}
	return &s, nil
}

// equipoValido aplica §12.2: tipo y su identificador, planta solo con ont y wireless solo con
// cpe_wireless. Devuelve la tecnología (ftth, wireless o tr069) para la capacidad.
func equipoValido(in *EntradaActivarServicio) (string, error) {
	e := &in.Equipo
	var err error
	if e.Serie, err = textoEquipo("equipo.serie", e.Serie, 64); err != nil {
		return "", err
	}
	if e.Serie != nil && !reSerie.MatchString(*e.Serie) {
		return "", campoInvalido("equipo.serie", "La serie lleva letras, números, punto, guion o barra", "formato")
	}
	if e.MAC != nil {
		m, ok := normalizarMAC(*e.MAC)
		if !ok {
			return "", campoInvalido("equipo.mac", "La MAC va como AA:BB:CC:DD:EE:FF", "formato")
		}
		e.MAC = &m
	}
	if e.Modelo, err = textoEquipo("equipo.modelo", e.Modelo, 80); err != nil {
		return "", err
	}
	tecnologia := ""
	switch e.Tipo {
	case "ont":
		if e.Serie == nil {
			return "", campoInvalido("equipo.serie", "La ONT se identifica por su serie", "equipo_invalido")
		}
		tecnologia = argos.TecnologiaFTTH
	case "cpe_wireless":
		if e.MAC == nil {
			return "", campoInvalido("equipo.mac", "El CPE o radio se identifica por su MAC", "equipo_invalido")
		}
		tecnologia = argos.TecnologiaWireless
	case "router_tr069":
		if e.MAC == nil && e.Serie == nil {
			return "", campoInvalido("equipo.mac", "El router TR-069 lleva su MAC o su serie", "equipo_invalido")
		}
		tecnologia = argos.TecnologiaTR069
	default:
		return "", campoInvalido("equipo.tipo", "Tipo de equipo desconocido", "enum")
	}
	if in.Planta != nil && e.Tipo != "ont" {
		return "", campoInvalido("planta", "La planta (OLT, PON, caja) solo va con una ONT", "equipo_invalido")
	}
	if in.Wireless != nil && e.Tipo != "cpe_wireless" {
		return "", campoInvalido("wireless", "El AP y el sector solo van con un CPE o radio", "equipo_invalido")
	}
	if in.Planta != nil {
		if in.Planta.OLT, err = textoEquipo("planta.olt", in.Planta.OLT, 60); err != nil {
			return "", err
		}
		if in.Planta.Caja, err = textoEquipo("planta.caja", in.Planta.Caja, 60); err != nil {
			return "", err
		}
	}
	if in.Wireless != nil {
		if in.Wireless.AP, err = textoEquipo("wireless.ap", in.Wireless.AP, 60); err != nil {
			return "", err
		}
		if in.Wireless.Sector, err = textoEquipo("wireless.sector", in.Wireless.Sector, 60); err != nil {
			return "", err
		}
	}
	if in.Wifi != nil {
		if err := ValidarSSID("wifi.ssid", in.Wifi.SSID); err != nil {
			return "", err
		}
		if in.Wifi.Clave != nil {
			if err := ValidarClaveWifi("wifi.clave", *in.Wifi.Clave); err != nil {
				return "", err
			}
		}
	}
	return tecnologia, nil
}

// --- Reglas de la orden -----------------------------------------------------------------------

// ajustesActivacion: el modo y la bienvenida de la orden (plantilla sobre empresa) y si Argos puede
// aprovisionar en la empresa (con alguna tecnología). Sin la capacidad, el modo efectivo es al_cerrar.
func (m *Modulo) ajustesActivacion(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, f campobd.CampoOrden, tecnologia string) (modo string, exigeBienvenida, capacidad bool, err error) {
	a, err := q.AjustesActivacion(ctx, campobd.AjustesActivacionParams{EmpresaID: f.EmpresaID, PlantillaID: f.PlantillaID})
	if err != nil {
		return "", false, false, err
	}
	if m.cfg.Equipos.Capacidad != nil && f.EmpresaID != nil {
		if capacidad, err = m.cfg.Equipos.Capacidad(ctx, t, f.EmpresaID, tecnologia); err != nil {
			return "", false, false, err
		}
	}
	modo = a.Modo
	if !capacidad {
		modo = ModoAlCerrar
	}
	return modo, a.ExigeBienvenida && modo == ModoExigirLista, capacidad, nil
}

// noDisponible explica por qué no se ofrece «Activar servicio» (vacío: sí se ofrece).
func (m *Modulo) noDisponible(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, f campobd.CampoOrden, tipo string,
	modo string, capacidad bool, viva *campobd.CampoActivacione) (string, error) {
	switch {
	case tipo != tipoInstalacion:
		return "no_es_instalacion", nil
	case f.ServicioID == nil || f.ClienteID == nil:
		return "sin_servicio", nil
	case f.Estado == EstadoCancelada || f.Estado == EstadoFallida:
		return "orden_terminada", nil
	case !capacidad:
		return "sin_capacidad", nil
	case modo == ModoAlCerrar:
		return "modo_al_cerrar", nil
	case viva != nil && viva.Estado == ActivacionLista:
		return "activacion_lista", nil
	case viva != nil:
		return "activacion_en_curso", nil
	}
	if f.Estado == EstadoCerrada {
		// Si el cierre ya mandó activar-instalacion (el modo era «al cerrar»), no se activa otra vez.
		if _, err := pendientes.PorClave(ctx, t, "iris:instalacion:"+strconv.FormatInt(f.ID, 10)); err == nil {
			return "activacion_al_cerrar", nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
	}
	return "", nil
}

func problemaNoDisponible(motivo string) *nucleo.Problema {
	switch motivo {
	case "activacion_lista":
		return nucleo.Conflicto("activacion_lista", "El servicio ya quedó activo con un intento anterior")
	case "activacion_en_curso":
		return nucleo.Conflicto("activacion_en_curso", "Ya hay una activación en curso para esta orden o este servicio: espera su resultado")
	case "activacion_al_cerrar":
		return nucleo.Conflicto("activacion_al_cerrar", "La activación ya salió al cerrar la orden (modo «al cerrar»): revísala en los pendientes hacia Argos")
	case "no_es_instalacion":
		return nucleo.ReglaNegocio("orden_no_es_instalacion", "Solo se activa el servicio desde una orden de instalación")
	case "sin_servicio":
		return nucleo.ReglaNegocio("orden_sin_servicio", "La orden no tiene el cliente y el servicio de Argos")
	case "orden_terminada":
		return transicionInvalida("activar el servicio de", "cancelada o fallida")
	case "modo_al_cerrar":
		return nucleo.ReglaNegocio("activacion_no_disponible", "La empresa activa el servicio al cerrar la orden")
	}
	return nucleo.ReglaNegocio("activacion_no_disponible", "Argos no puede aprovisionar este tipo de equipo en la empresa: el servicio se activa al cerrar")
}

// esTecnico: el actor trabaja la orden como técnico (no ve todas las órdenes).
func esTecnico(ctx context.Context) bool { return !puedeVerTodas(ctx) }

// accesoEquipo: el técnico solo en sus órdenes en curso (asignadas a él y no cerradas, §12.2 y
// §12.4); un agente, en las de su alcance aun cerradas.
func accesoEquipo(ctx context.Context, f campobd.CampoOrden, permiso nucleo.Permiso) error {
	if !esTecnico(ctx) {
		return nil
	}
	if !propia(ctx, f) {
		return fueraDeAlcance("El técnico solo lo hace en sus órdenes")
	}
	if f.Estado == EstadoCerrada || f.Estado == EstadoFallida || f.Estado == EstadoCancelada {
		return nucleo.NuevoProblema(http.StatusForbidden, "orden_no_en_curso", "Orden terminada",
			"Con la orden terminada, solo un agente puede hacerlo desde el panel ("+string(permiso)+")")
	}
	return nil
}

// --- Casos de uso ------------------------------------------------------------------------------

// CasosActivacion: activar, ver y resolver los intentos.
type CasosActivacion struct {
	Activar   *auditoria.Caso[EntradaActivarServicio, Orden]
	Ver       *auditoria.Caso[EntradaVerOrden, EstadoActivacion]
	Resolver  *auditoria.Caso[EntradaResolverActivacion, ResultadoResolver]
	Consultar func(ctx context.Context) (int, error)
}

func (m *Modulo) nuevosCasosActivacion() CasosActivacion {
	c := CasosActivacion{Activar: m.nuevoActivarServicio(), Ver: m.nuevoVerActivacion(), Resolver: m.nuevoResolverActivacion()}
	c.Consultar = m.consultarActivaciones
	return c
}

func (m *Modulo) nuevoActivarServicio() *auditoria.Caso[EntradaActivarServicio, Orden] {
	return accionOrden("ordenes.activar_servicio", nucleo.PermOrdenesActivarServicio, func(e EntradaActivarServicio) *OpSync { return e.Sync },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaActivarServicio) (campobd.CampoOrden, string, error) {
			f, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesActivarServicio, nil, in.Sync)
			if err != nil {
				return f, "", err
			}
			if err := accesoEquipo(ctx, f, nucleo.PermOrdenesActivarServicio); err != nil {
				return f, "", err
			}
			if err := noPausada(f, "activar el servicio"); err != nil {
				return f, "", err
			}
			a := contexto.ActorDe(ctx)
			if !a.EsUsuario() {
				return f, "", nucleo.Prohibido("La activación la pide una persona (X-Iris-Actor)")
			}
			tecnologia, err := equipoValido(&in)
			if err != nil {
				return f, "", err
			}
			tipo, err := tipoDe(ctx, q, f)
			if err != nil {
				return f, "", err
			}
			modo, _, capacidad, err := m.ajustesActivacion(ctx, t, q, f, tecnologia)
			if err != nil {
				return f, "", err
			}
			viva, err := q.ActivacionVivaDeOrden(ctx, f.ID)
			var pv *campobd.CampoActivacione
			switch {
			case err == nil:
				pv = &viva
			case !errors.Is(err, pgx.ErrNoRows):
				return f, "", err
			}
			if pv == nil && f.ServicioID != nil {
				// Otra orden del mismo servicio con un intento vivo (el índice único también lo impide).
				if otra, err := q.ActivacionVivaDeServicio(ctx, *f.ServicioID); err == nil {
					pv = &otra
				} else if !errors.Is(err, pgx.ErrNoRows) {
					return f, "", err
				}
			}
			motivo, err := m.noDisponible(ctx, t, q, f, tipo.Codigo, modo, capacidad, pv)
			if err != nil {
				return f, "", err
			}
			if motivo != "" {
				return f, "", problemaNoDisponible(motivo)
			}
			// 0.14.3 §4.3: el Wi-Fi inicial exige la marca wifi de la empresa.
			if in.Wifi != nil && m.cfg.Equipos.Capacidad != nil {
				if ok, err := m.cfg.Equipos.Capacidad(ctx, t, f.EmpresaID, argos.Funcion(argos.FuncionWifi)); err != nil {
					return f, "", err
				} else if !ok {
					return f, "", nucleo.ReglaNegocio(argos.CodigoCapacidadNoDisponible, "El Wi-Fi inicial no está disponible para esta empresa: activa sin él")
				}
			}
			if f.Estado != EstadoCerrada && esTecnico(ctx) {
				if err := exigirEstado(f, "activar el servicio de", EstadoEnSitio); err != nil {
					return f, "", err
				}
			}
			cuando := m.cuandoEn(in.Sync, f)
			if _, err := q.AsegurarOrdenActivacion(ctx, campobd.AsegurarOrdenActivacionParams{OrdenID: f.ID, EmpresaID: f.EmpresaID}); err != nil {
				return f, "", err
			}
			intento, err := q.SumarIntentoActivacion(ctx, f.ID)
			if err != nil {
				return f, "", err
			}
			clave := pendientes.ClaveAprovisionar(f.ID, intento)
			cuerpo := map[string]any{
				"equipo": map[string]any{"tipo": in.Equipo.Tipo, "serie": in.Equipo.Serie, "mac": in.Equipo.MAC,
					"unidad_id": in.Equipo.UnidadID, "modelo": in.Equipo.Modelo},
				"wifi": nil, "origen_tipo": pendientes.OrigenTipo, "origen_id": strconv.FormatInt(f.ID, 10),
			}
			if in.Planta != nil {
				cuerpo["planta"] = map[string]any{"olt": in.Planta.OLT, "slot": in.Planta.Slot, "pon": in.Planta.PON,
					"caja": in.Planta.Caja, "puerto": in.Planta.Puerto}
			}
			if in.Wireless != nil {
				cuerpo["wireless"] = map[string]any{"ap": in.Wireless.AP, "sector": in.Wireless.Sector}
			}
			var transito *pendientes.Transito
			var ssid *string
			if in.Wifi != nil {
				ssid = &in.Wifi.SSID
				cuerpo["wifi"] = map[string]any{"ssid": in.Wifi.SSID}
				if in.Wifi.Clave != nil {
					// La clave va solo en el depósito cifrado; se agrega al cuerpo al enviarlo (wifi.clave).
					transito = &pendientes.Transito{Campos: map[string]any{"wifi.clave": *in.Wifi.Clave}}
				}
			}
			// El técnico de la orden (§12.2): la unidad debe estar en su bodega. Con la orden cerrada,
			// Iris manda ese técnico.
			if t, err := tecnicoArgos(ctx, q, f.TecnicoID); err != nil {
				return f, "", err
			} else if t != nil {
				cuerpo["tecnico"] = t
			}
			// La activación espera a los pasos vivos del titular (contrato 0.10.0 §3.4: titular → datos
			// → plan → activar).
			deps := []int64{}
			pasos, err := pendientes.PasosDeOrden(ctx, t, f.ID)
			if err != nil {
				return f, "", err
			}
			for _, p := range pasos {
				if p.Estado != pendientes.EstadoResueltoManual {
					deps = append(deps, p.ID)
				}
			}
			bytesCuerpo, err := json.Marshal(cuerpo)
			if err != nil {
				return f, "", err
			}
			reg, creado, err := pendientes.AccionConTransito(ctx, t, m.cfg.Titular.Cifrador, pendientes.Accion{
				EmpresaID: f.EmpresaID, Operacion: pendientes.OpAprovisionar, IdempotencyKey: clave, Entidad: "orden",
				EntidadID: strconv.FormatInt(f.ID, 10), Ruta: "/servicios/" + strconv.FormatInt(*f.ServicioID, 10) + "/aprovisionar",
				Cuerpo: bytesCuerpo, DependeDe: deps, Desde: cuando, Actor: pendientes.ActorUsuario(a.ID),
			}, transito, m.cfg.Titular.Encolar)
			if err != nil {
				return f, "", err
			}
			if !creado {
				return f, "", errors.New("campo: la clave de aprovisionamiento ya existía")
			}
			if len(deps) > 0 {
				if err := pendientes.BloquearSiDependenciaFallida(ctx, t, deps); err != nil {
					return f, "", err
				}
			}
			id := in.activacionID
			if in.Sync != nil {
				id = in.Sync.ID
			}
			if id == uuid.Nil {
				if id, err = uuid.NewV7(); err != nil {
					return f, "", err
				}
			}
			origen := "panel"
			if in.Sync != nil {
				origen = "campo"
			}
			var planta, wireless []byte
			if in.Planta != nil {
				planta, _ = json.Marshal(cuerpo["planta"])
			}
			if in.Wireless != nil {
				wireless, _ = json.Marshal(cuerpo["wireless"])
			}
			act, err := q.CrearActivacion(ctx, campobd.CrearActivacionParams{
				ID: id, OrdenID: f.ID, EmpresaID: f.EmpresaID, ServicioID: *f.ServicioID, Intento: intento, IdempotencyKey: clave,
				Origen: origen, PedidoPor: &a.ID, EquipoTipo: in.Equipo.Tipo, EquipoSerie: in.Equipo.Serie, EquipoMac: in.Equipo.MAC,
				EquipoUnidadID: in.Equipo.UnidadID, EquipoModelo: in.Equipo.Modelo, Planta: planta, Wireless: wireless,
				WifiSsid: ssid, ConClaveWifi: transito != nil, PendienteID: &reg.ID,
			})
			if esViolacionUnica(err) {
				// Otro pedido (el teléfono y el panel a la vez, u otra orden del servicio) ganó la carrera.
				return f, "", problemaNoDisponible("activacion_en_curso")
			}
			if err != nil {
				return f, "", err
			}
			t.RegistrarCambios("activacion", act.ID, nil, map[string]any{"intento": act.Intento, "estado": act.Estado,
				"equipo_tipo": act.EquipoTipo, "con_wifi": act.WifiSsid != nil, "pendiente_id": reg.ID})
			despues, err := guardar(ctx, t, q, f, f)
			if err != nil {
				return despues, "", err
			}
			if err := historial(ctx, t, q, f.ID, "activacion", nil, ptr(ActivacionPendiente), nil,
				map[string]any{"intento": intento, "equipo_tipo": in.Equipo.Tipo, "origen": origen}, cuando); err != nil {
				return despues, "", err
			}
			return despues, "", t.Emitir(EventoActivacionPedida, map[string]any{
				"orden_id": f.ID, "activacion_id": act.ID, "intento": intento, "servicio_id": *f.ServicioID,
				"equipo_tipo": in.Equipo.Tipo, "con_wifi": in.Wifi != nil, "origen": origen, "pendiente_id": reg.ID,
			})
		})
}

// esViolacionUnica: un índice único rechazó la fila (23505).
func esViolacionUnica(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

func (m *Modulo) nuevoVerActivacion() *auditoria.Caso[EntradaVerOrden, EstadoActivacion] {
	return auditoria.Nuevo(auditoria.Caso[EntradaVerOrden, EstadoActivacion]{
		Accion: "ordenes.ver_activacion", Acceso: auditoria.Autenticado, AuditarExito: soloLecturas[EstadoActivacion],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaVerOrden) (EstadoActivacion, error) {
			q := campobd.New(t)
			t.FijarEntidad("orden", in.ID)
			f, err := q.OrdenPorID(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return EstadoActivacion{}, noEncontrada()
			}
			if err != nil {
				return EstadoActivacion{}, err
			}
			if err := verificarAcceso(ctx, nucleo.PermOrdenesVer, f); err != nil {
				return EstadoActivacion{}, err
			}
			t.FijarEmpresa(deref(f.EmpresaID))
			return m.estadoActivacion(ctx, t, q, f)
		},
	})
}

// estadoActivacion arma el estado de la activación de la orden.
func (m *Modulo) estadoActivacion(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, f campobd.CampoOrden) (EstadoActivacion, error) {
	r := EstadoActivacion{OrdenID: f.ID, Estado: ActivacionNinguna, Intentos: []Activacion{}}
	tipo, err := tipoDe(ctx, q, f)
	if err != nil {
		return r, err
	}
	modo, exige, capacidad, err := m.ajustesActivacion(ctx, t, q, f, "")
	if err != nil {
		return r, err
	}
	r.Modo, r.ExigeBienvenida = modo, exige
	filas, err := q.ActivacionesDeOrden(ctx, f.ID)
	if err != nil {
		return r, err
	}
	var viva *campobd.CampoActivacione
	for i, a := range filas {
		r.Intentos = append(r.Intentos, aActivacion(a))
		if a.Estado != ActivacionFallida && viva == nil {
			viva = &filas[i]
		}
	}
	if len(r.Intentos) > 0 {
		r.Actual, r.Estado = &r.Intentos[0], r.Intentos[0].Estado
	}
	motivo, err := m.noDisponible(ctx, t, q, f, tipo.Codigo, modo, capacidad, viva)
	if err != nil {
		return r, err
	}
	if motivo == "" && contexto.PermisosDe(ctx).Tiene(string(nucleo.PermOrdenesActivarServicio)) && accesoEquipo(ctx, f, nucleo.PermOrdenesActivarServicio) == nil {
		r.Disponible = true
	} else if motivo != "" {
		r.NoDisponible = &motivo
	}
	if oa, err := q.OrdenActivacion(ctx, f.ID); err == nil {
		r.SinActivacionMotivo = oa.SinActivacionMotivo
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return r, err
	}
	return r, nil
}

// --- Resolver un intento (cola, webhook y consulta) --------------------------------------------

// Fuentes del resultado de un intento.
const (
	FuentePendiente = "pendiente"
	FuenteWebhook   = "webhook"
	FuenteConsulta  = "consulta"
)

// EntradaResolverActivacion: lo que se supo de un intento (actor sistema).
type EntradaResolverActivacion struct {
	Fuente string `json:"fuente"`
	// Uno de los dos identifica el intento (la clave manda).
	IdempotencyKey      *string `json:"idempotency_key,omitempty"`
	AprovisionamientoID *int64  `json:"aprovisionamiento_id,omitempty"`
	// Estado al que pasa: en_proceso, listo o fallido; vacío = solo anota el aprovisionamiento_id.
	Estado         string                        `json:"estado"`
	Causa          *string                       `json:"causa,omitempty"`
	Reintentable   *bool                         `json:"reintentable,omitempty"`
	Detalle        *string                       `json:"detalle,omitempty"`
	Pasos          []argos.PasoAprovisionamiento `json:"pasos,omitempty"`
	ServicioEstado *string                       `json:"servicio_estado,omitempty"`
	EntranteID     *int64                        `json:"entrante_id,omitempty"`
	// Del webhook: deben coincidir con los del intento (si no, se ignora).
	ServicioID *int64 `json:"servicio_id,omitempty"`
	EmpresaID  *int64 `json:"empresa_id,omitempty"`
	// Revivir: un pendiente que había fallido y se reintentó a mano quedó hecho (vuelve a en_proceso
	// si no hay otro intento vivo).
	Revivir bool `json:"revivir,omitempty"`
}

// ResultadoResolver dice qué pasó.
type ResultadoResolver struct {
	ActivacionID *uuid.UUID `json:"activacion_id,omitempty"`
	OrdenID      int64      `json:"orden_id,omitempty"`
	De           string     `json:"de,omitempty"`
	A            string     `json:"a,omitempty"`
	Resultado    string     `json:"resultado"` // aplicado | sin_cambios | desconocido
}

// transicionValida: lo final no cambia (un webhook repetido o tardío no pisa nada).
func transicionValida(de, a string, revivir bool) bool {
	switch de {
	case ActivacionPendiente:
		return a == ActivacionEnProceso || a == ActivacionLista || a == ActivacionFallida
	case ActivacionEnProceso:
		return a == ActivacionLista || a == ActivacionFallida
	case ActivacionFallida:
		return revivir && (a == ActivacionEnProceso || a == ActivacionLista)
	}
	return false
}

func (m *Modulo) nuevoResolverActivacion() *auditoria.Caso[EntradaResolverActivacion, ResultadoResolver] {
	return auditoria.Nuevo(auditoria.Caso[EntradaResolverActivacion, ResultadoResolver]{
		Accion: "ordenes.resolver_activacion", Acceso: auditoria.Autenticado,
		AuditarExito: func(r ResultadoResolver) bool { return r.Resultado == "aplicado" },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaResolverActivacion) (ResultadoResolver, error) {
			q := campobd.New(t)
			r := ResultadoResolver{Resultado: "desconocido"}
			// Primero se ubica el intento (sin candado) para tomar la orden: el orden de los candados es
			// orden → intento, el mismo que el pedido y el cierre.
			var previo campobd.CampoActivacione
			var err error
			switch {
			case in.IdempotencyKey != nil:
				previo, err = q.ActivacionPorClave(ctx, *in.IdempotencyKey)
			case in.AprovisionamientoID != nil:
				previo, err = q.ActivacionPorAprovisionamiento(ctx, *in.AprovisionamientoID)
			default:
				return r, nil
			}
			if errors.Is(err, pgx.ErrNoRows) && in.IdempotencyKey != nil && in.AprovisionamientoID != nil {
				previo, err = q.ActivacionPorAprovisionamiento(ctx, *in.AprovisionamientoID)
			}
			if errors.Is(err, pgx.ErrNoRows) {
				t.Anotar(map[string]any{"desconocido": true, "aprovisionamiento_id": in.AprovisionamientoID})
				return r, nil
			}
			if err != nil {
				return r, err
			}
			t.FijarEntidad("orden", previo.OrdenID)
			t.FijarEmpresa(deref(previo.EmpresaID))
			f, err := q.OrdenPorIDParaActualizar(ctx, previo.OrdenID)
			if err != nil {
				return r, err
			}
			antes, err := q.ActivacionPorIDParaActualizar(ctx, previo.ID)
			if err != nil {
				return r, err
			}
			r.ActivacionID, r.OrdenID, r.De, r.Resultado = &antes.ID, antes.OrdenID, antes.Estado, "sin_cambios"
			if (in.ServicioID != nil && *in.ServicioID != antes.ServicioID) || (in.EmpresaID != nil && !mismoID(in.EmpresaID, antes.EmpresaID)) {
				t.Anotar(map[string]any{"webhook_de_otro_servicio": true})
				return r, nil
			}
			if in.AprovisionamientoID != nil && antes.AprovisionamientoID != nil && *antes.AprovisionamientoID != *in.AprovisionamientoID {
				// Otro aprovisionamiento con la misma clave: no se mezcla (se revisa a mano).
				t.Anotar(map[string]any{"aprovisionamiento_distinto": *in.AprovisionamientoID})
				return r, nil
			}
			p := campobd.GuardarActivacionParams{ID: antes.ID, Estado: antes.Estado, AprovisionamientoID: antes.AprovisionamientoID,
				Causa: antes.Causa, Reintentable: antes.Reintentable, Detalle: antes.Detalle, Pasos: antes.Pasos,
				ServicioEstado: antes.ServicioEstado, ConsultadoEn: antes.ConsultadoEn, ResueltoEn: antes.ResueltoEn, EntranteID: antes.EntranteID}
			cambia := false
			if p.AprovisionamientoID == nil && in.AprovisionamientoID != nil {
				p.AprovisionamientoID, cambia = in.AprovisionamientoID, true
			}
			ahora := m.ahora()
			if in.Fuente == FuenteConsulta {
				p.ConsultadoEn, cambia = &ahora, true
			}
			if len(in.Pasos) > 0 {
				p.Pasos, _ = json.Marshal(in.Pasos)
				cambia = true
			}
			if in.Estado != "" && in.Estado != antes.Estado && transicionValida(antes.Estado, in.Estado, in.Revivir) {
				p.Estado, cambia = in.Estado, true
				switch in.Estado {
				case ActivacionLista:
					p.Causa, p.Reintentable, p.Detalle, p.ServicioEstado, p.ResueltoEn = nil, nil, nil, in.ServicioEstado, &ahora
				case ActivacionFallida:
					p.Causa, p.Reintentable, p.Detalle, p.ResueltoEn = in.Causa, in.Reintentable, recortar(textoLimpio(in.Detalle), 300), &ahora
				case ActivacionEnProceso:
					p.Causa, p.Reintentable, p.Detalle, p.ResueltoEn = nil, nil, nil, nil
				}
				if in.EntranteID != nil {
					p.EntranteID = in.EntranteID
				}
			}
			if !cambia {
				return r, nil
			}
			despues, err := q.GuardarActivacion(ctx, p)
			if esViolacionUnica(err) {
				// Revivir chocó con otro intento vivo: queda fallido (no hay dos vivos).
				t.Anotar(map[string]any{"revivir_rechazado": true})
				return r, nil
			}
			if err != nil {
				return r, err
			}
			r.A, r.Resultado = despues.Estado, "aplicado"
			t.RegistrarCambios("activacion", despues.ID, map[string]any{"estado": antes.Estado, "aprovisionamiento_id": antes.AprovisionamientoID},
				map[string]any{"estado": despues.Estado, "aprovisionamiento_id": despues.AprovisionamientoID, "causa": despues.Causa})
			if despues.Estado == antes.Estado {
				return r, nil
			}
			if _, err := guardar(ctx, t, q, f, f); err != nil {
				return r, err
			}
			if err := historial(ctx, t, q, f.ID, "activacion", &antes.Estado, &despues.Estado, despues.Causa,
				map[string]any{"intento": despues.Intento, "fuente": in.Fuente}, ahora); err != nil {
				return r, err
			}
			return r, t.Emitir(EventoActivacionCambiada, map[string]any{
				"orden_id": f.ID, "activacion_id": despues.ID, "intento": despues.Intento, "servicio_id": despues.ServicioID,
				"de": antes.Estado, "a": despues.Estado, "causa": despues.Causa, "aprovisionamiento_id": despues.AprovisionamientoID,
				"fuente": in.Fuente,
			})
		},
	})
}

// consultarActivaciones: las en_proceso sin respuesta de Argos desde hace 10 min se consultan con
// GET /aprovisionamientos/:id (fuera de transacción) y se resuelven con lo que diga Argos.
func (m *Modulo) consultarActivaciones(ctx context.Context) (int, error) {
	if m.cfg.Equipos.LeerAprovisionamiento == nil {
		return 0, nil
	}
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return 0, nucleo.ErrSinBD
	}
	filas, err := campobd.New(bd).ActivacionesPorConsultar(ctx, campobd.ActivacionesPorConsultarParams{
		Antes: ptr(m.ahora().Add(-EsperaWebhook)), Limite: 50})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, f := range filas {
		a, err := m.cfg.Equipos.LeerAprovisionamiento(ctx, *f.AprovisionamientoID)
		in := EntradaResolverActivacion{Fuente: FuenteConsulta, IdempotencyKey: &f.IdempotencyKey, AprovisionamientoID: f.AprovisionamientoID}
		if err == nil {
			in.Pasos = a.Pasos
			if a.Estado != ActivacionEnProceso {
				in.Estado, in.Causa, in.Reintentable, in.Detalle, in.ServicioEstado = a.Estado, a.Causa, a.Reintentable, a.Detalle, a.ServicioEstado
			}
		}
		// Sin respuesta de Argos, igual se anota la consulta (la siguiente, en otros 10 min).
		if _, err := m.Activacion.Resolver.Ejecutar(ctx, in); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// EventoPendienteDeActivacion: lo que la activación usa de integracion.pendiente_*.
type eventoPendienteAct struct {
	Operacion      string `json:"operacion"`
	Entidad        string `json:"entidad"`
	IdempotencyKey string `json:"idempotency_key"`
}

// resolverDesdePendiente traduce el resultado del pendiente de aprovisionar a la activación.
func (m *Modulo) resolverDesdePendiente(ctx context.Context, evento string, payload []byte) error {
	var e eventoPendienteAct
	if err := json.Unmarshal(payload, &e); err != nil || e.Operacion != pendientes.OpAprovisionar || e.Entidad != "orden" {
		return nil
	}
	bd := nucleo.BDDe(ctx)
	reg, err := pendientes.PorClave(ctx, bd, e.IdempotencyKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	in := EntradaResolverActivacion{Fuente: FuentePendiente, IdempotencyKey: &e.IdempotencyKey}
	switch evento {
	case pendientes.EventoHecho:
		if id, ok := pendientes.CampoDeRespuesta(reg.Respuesta, "aprovisionamiento_id"); ok {
			var n json.Number
			if json.Unmarshal(id, &n) == nil {
				if v, err := n.Int64(); err == nil && v > 0 {
					in.AprovisionamientoID = &v
				}
			}
		}
		in.Estado, in.Revivir = ActivacionEnProceso, true
		// Reintento con la misma clave de uno ya terminado: Argos responde 200 con el estado final.
		if est, ok := pendientes.CampoDeRespuesta(reg.Respuesta, "estado"); ok {
			var s string
			if json.Unmarshal(est, &s) == nil && (s == ActivacionLista || s == ActivacionFallida) {
				in.Estado = s
				if s == ActivacionFallida {
					in.Causa = campoTexto(reg.Respuesta, "causa")
					if in.Causa == nil {
						in.Causa = ptr("otro")
					}
				}
			}
		}
	case pendientes.EventoFallido:
		in.Estado, in.Causa, in.Detalle = ActivacionFallida, reg.CodigoError, reg.UltimoError
		if in.Causa == nil {
			in.Causa = ptr("rechazado_por_argos")
		}
	case pendientes.EventoResueltoManual:
		in.Estado, in.Causa = ActivacionFallida, ptr("resuelto_manual")
	default:
		// Agotado: el intento sigue vivo (pendiente) hasta que se reintente o se resuelva a mano en
		// la cola: nunca dos aprovisionamientos a la vez.
		return nil
	}
	_, err = m.Activacion.Resolver.Ejecutar(ctx, in)
	return err
}

func campoTexto(respuesta []byte, campo string) *string {
	v, ok := pendientes.CampoDeRespuesta(respuesta, campo)
	if !ok {
		return nil
	}
	var s string
	if json.Unmarshal(v, &s) != nil || s == "" {
		return nil
	}
	return &s
}

// eventoInstalacion es el payload de argos.instalacion_resuelta.
type eventoInstalacion struct {
	AprovisionamientoID *int64  `json:"aprovisionamiento_id"`
	IdempotencyKey      *string `json:"idempotency_key"`
	Estado              string  `json:"estado"`
	Causa               *string `json:"causa"`
	Reintentable        *bool   `json:"reintentable"`
	Detalle             *string `json:"detalle"`
	ServicioEstado      *string `json:"servicio_estado"`
	EntranteID          *int64  `json:"entrante_id"`
	ServicioID          *int64  `json:"servicio_id"`
	EmpresaID           *int64  `json:"empresa_id"`
}

// resolverDesdeWebhook aplica instalacion.aprovisionada / instalacion.fallida.
func (m *Modulo) resolverDesdeWebhook(ctx context.Context, payload []byte) error {
	var e eventoInstalacion
	if err := json.Unmarshal(payload, &e); err != nil || (e.Estado != ActivacionLista && e.Estado != ActivacionFallida) {
		return nil
	}
	_, err := m.Activacion.Resolver.Ejecutar(ctx, EntradaResolverActivacion{Fuente: FuenteWebhook, IdempotencyKey: e.IdempotencyKey,
		AprovisionamientoID: e.AprovisionamientoID, Estado: e.Estado, Causa: e.Causa, Reintentable: e.Reintentable, Detalle: e.Detalle,
		ServicioEstado: e.ServicioEstado, EntranteID: e.EntranteID, ServicioID: e.ServicioID, EmpresaID: e.EmpresaID})
	return err
}

// textoLimpio: el texto sin espacios a los lados (nil si queda vacío).
func textoLimpio(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}

// --- Cierre ------------------------------------------------------------------------------------

// activacionCierre es lo que el cierre manda a la cola sobre la activación (orden.cerrada.activacion
// y equipos[].aprovisionado, §12.2).
type activacionCierre struct {
	instalacion bool
	modo        string
	estado      string
	viva        *campobd.CampoActivacione
	sinMotivo   *string
	// activadoFuera: Argos ya tiene el servicio activo (0.14.3); ningún equipo lleva comodato al cerrar.
	activadoFuera bool
}

// alPayload agrega activacion a orden.cerrada y marca el equipo aprovisionado (el cierre no manda
// su comodato: Argos ya lo asignó, o lo asigna al aprovisionar).
func (a activacionCierre) alPayload(p map[string]any) {
	if !a.instalacion {
		p["activacion"] = nil
		return
	}
	var aprov *int64
	if a.viva != nil {
		aprov = a.viva.AprovisionamientoID
	}
	p["activacion"] = map[string]any{"modo": a.modo, "estado": a.estado, "aprovisionamiento_id": aprov,
		"cerrada_sin_activacion_motivo": a.sinMotivo}
	equipos, _ := p["equipos"].([]map[string]any)
	for _, e := range equipos {
		e["aprovisionado"] = e["accion"] == AccionInstalar && (a.activadoFuera || (a.viva != nil && mismoEquipo(*a.viva, e)))
	}
}

// mismoEquipo: el equipo de la orden es el del intento (unidad, serie o MAC).
func mismoEquipo(a campobd.CampoActivacione, e map[string]any) bool {
	if u, ok := e["unidad_id"].(*int64); ok && u != nil && a.EquipoUnidadID != nil && *u == *a.EquipoUnidadID {
		return true
	}
	if s, ok := e["serie"].(*string); ok && s != nil && a.EquipoSerie != nil && strings.EqualFold(strings.TrimSpace(*s), *a.EquipoSerie) {
		return true
	}
	if mac, ok := e["mac"].(*string); ok && mac != nil && a.EquipoMac != nil {
		if n, ok := normalizarMAC(*mac); ok && n == *a.EquipoMac {
			return true
		}
	}
	return false
}

// activacionAlCerrar aplica las reglas del modo al cerrar (la orden ya tomada con FOR UPDATE: un
// intento no puede nacer ni resolverse en medio):
//   - exigir_lista: sin un intento listo, el cierre exitoso pide un supervisor con motivo
//     (422 activacion_requerida); con cierre_exige_bienvenida, además la bienvenida enviada o no
//     enviada porque el cliente no acepta avisos (422 bienvenida_requerida);
//   - pendiente_agente: cierra sin ella; un agente la termina desde el panel.
func (m *Modulo) activacionAlCerrar(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, f campobd.CampoOrden, tipo, resultado string,
	motivo *string) (activacionCierre, error) {
	r := activacionCierre{instalacion: tipo == tipoInstalacion, estado: ActivacionNinguna}
	if !r.instalacion {
		return r, nil
	}
	modo, exigeBienvenida, _, err := m.ajustesActivacion(ctx, t, q, f, "")
	if err != nil {
		return r, err
	}
	r.modo = modo
	filas, err := q.ActivacionesDeOrden(ctx, f.ID)
	if err != nil {
		return r, err
	}
	if len(filas) > 0 {
		r.estado = filas[0].Estado
	}
	for i := range filas {
		if filas[i].Estado != ActivacionFallida {
			r.viva = &filas[i]
			r.estado = filas[i].Estado
			break
		}
	}
	if resultado != "exitosa" || modo != ModoExigirLista {
		return r, nil
	}
	// 0.14.3: el servicio ya está activo en Argos sin un intento listo de la orden («activado fuera de la
	// orden»): se cierra sin supervisor y sin comodato doble (ni activar-instalacion).
	if (r.viva == nil || r.viva.Estado != ActivacionLista) && f.ServicioID != nil {
		fuera, err := q.ServicioActivoEnArgos(ctx, *f.ServicioID)
		if err != nil {
			return r, err
		}
		if fuera {
			r.estado, r.activadoFuera = ActivacionFuera, true
			return r, nil
		}
	}
	falta := ""
	switch {
	case r.viva == nil || r.viva.Estado != ActivacionLista:
		falta = "activacion"
	case exigeBienvenida:
		oa, err := q.OrdenActivacion(ctx, f.ID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return r, err
		}
		if !bienvenidaCumplida(oa.BienvenidaCanales) {
			falta = "bienvenida"
		}
	}
	if falta == "" {
		return r, nil
	}
	m2 := textoLimpio(motivo)
	if m2 == nil {
		if falta == "bienvenida" {
			return r, nucleo.ReglaNegocio("bienvenida_requerida", "La empresa exige la bienvenida enviada para cerrar la instalación (o un supervisor con motivo)")
		}
		return r, nucleo.ReglaNegocio("activacion_requerida", "La empresa exige la activación lista para cerrar la instalación como exitosa (o un supervisor con motivo)")
	}
	if !cubre(ctx, nucleo.PermOrdenesCerrarSinActivacion, f) {
		return r, fueraDeAlcance("Cerrar sin la activación lista lo hace un supervisor (ordenes.cerrar_sin_activacion)")
	}
	if utf8.RuneCountInString(*m2) < 10 {
		return r, campoInvalido("sinActivacionMotivo", "Escribe el motivo (10 a 500 caracteres)", "minLength")
	}
	if _, err := q.AsegurarOrdenActivacion(ctx, campobd.AsegurarOrdenActivacionParams{OrdenID: f.ID, EmpresaID: f.EmpresaID}); err != nil {
		return r, err
	}
	a := contexto.ActorDe(ctx)
	if err := q.FijarSinActivacion(ctx, campobd.FijarSinActivacionParams{OrdenID: f.ID, Motivo: m2, Por: &a.ID}); err != nil {
		return r, err
	}
	t.RegistrarCambios("orden_activacion", f.ID, nil, map[string]any{"sin_activacion_motivo": *m2, "falta": falta})
	r.sinMotivo = m2
	return r, nil
}

// bienvenidaCumplida: la bienvenida salió por algún canal o no salió porque el cliente no acepta
// avisos (sin_permiso, baja), §12.5.
func bienvenidaCumplida(crudo []byte) bool {
	var canales []struct {
		Estado      string  `json:"estado"`
		Motivo      *string `json:"motivo"`
		Relacionada bool    `json:"relacionadaPorServicio"`
	}
	if len(crudo) == 0 || json.Unmarshal(crudo, &canales) != nil {
		return false
	}
	for _, c := range canales {
		if c.Relacionada {
			continue // relacionada por servicio (0.14.3): no cuenta para el cierre
		}
		if c.Estado == "enviada" {
			return true
		}
		if c.Motivo != nil && (*c.Motivo == "sin_permiso" || *c.Motivo == "baja") {
			return true
		}
	}
	return false
}
