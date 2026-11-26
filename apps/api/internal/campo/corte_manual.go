package campo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Corte manual de respaldo (contrato argosisp.md 0.15.0 §13.2, A54 / B34).
//
// argos.corte_manual (el núcleo lo emite tras validar servicio.corte_manual_*) → campo guarda la
// solicitud en campo.cortes_manuales (clave: solicitud_id; dedupe por version, B3) y:
//   - solicitado: abre una orden nueva corte_catv o reconexion_catv (idempotente por solicitud_id), con
//     la causa visible; la reconexión siempre con prioridad alta (B5). Con un descarte (B9: empresa
//     fuera del alcance o servicio desconocido) la guarda sin abrir nada.
//   - cancelado (B4): con la orden sin empezar, la cancela y avisa cancelada; con el técnico ya en camino
//     o en sitio, la marca (cancelacion_pedida) y decide el cierre: «Hecho» → resuelta (Argos pide la
//     reconexión), si no → cancelada; con el corte ya resuelto, vuelve a informar resuelta; antes que el
//     solicitado, la guarda (cancelada_antes) y el solicitado de una versión menor o igual ya no abre nada.
//
// Iris devuelve corte_manual.estado_cambiado (outbox → webhook de §6.6) con su propio contador de
// version: en_curso al asignar, resuelta al cerrar con «Hecho», no_realizada con motivo_codigo
// (sin_acceso, equipo_no_encontrado, otro) y cancelada. Los terminales no retroceden.
//
// Qué no hace (B6, B7): la orden no va al portal de Argos ni genera ticket.* (no tiene ticket), no avisa
// al cliente (no hay encuesta ni aviso propio) y su cierre no llama a activar-instalacion ni a retiro (la
// cola de pendientes solo actúa por esos tipos). En un servicio mixto solo se toca el cable (B8).

// Tipos de orden del corte manual.
const (
	TipoCorteCATV      = "corte_catv"
	TipoReconexionCATV = "reconexion_catv"
)

// EventoCorteEstado es el evento que va a Argos (§6.6).
const EventoCorteEstado = "corte_manual.estado_cambiado"

// Estados de la solicitud (campo.cortes_manuales).
const (
	CorteAbierta        = "abierta"
	CorteEnCurso        = "en_curso"
	CorteResuelta       = "resuelta"
	CorteNoRealizada    = "no_realizada"
	CorteCancelada      = "cancelada"
	CorteDescartada     = "descartada"
	CorteCanceladaAntes = "cancelada_antes"
)

// Opciones del ítem «resultado» del checklist de cierre (migración campo_corte_manual).
var resultadosCorte = map[string]string{
	"Hecho": "", "Sin acceso": "sin_acceso", "Equipo no encontrado": "equipo_no_encontrado", "Otro motivo": "otro",
}

var textosCausa = map[string]string{
	"sin_onu":                "el servicio no tiene ONU registrada en SmartOLT",
	"instancia_no_operativa": "la instancia de SmartOLT no está operativa",
	"smartolt_fallo":         "SmartOLT agotó sus reintentos",
	"catv_no_soportado":      "la ONU no admite el corte de CATV por SmartOLT",
}

// TextoCausa: la causa de Argos para mostrar.
func TextoCausa(causa string) string {
	if t, ok := textosCausa[causa]; ok {
		return t
	}
	return "otra causa informada por Argos"
}

func terminalCorte(estado string) bool {
	return estado == CorteResuelta || estado == CorteNoRealizada || estado == CorteCancelada
}

// EntradaCorteArgos es el payload de argos.corte_manual (contratos/eventos.md).
type EntradaCorteArgos struct {
	Evento       string     `json:"evento"`
	SolicitudID  int64      `json:"solicitud_id"`
	Version      int32      `json:"version"`
	SolicitadaEn *time.Time `json:"solicitada_en"`
	ServicioID   int64      `json:"servicio_id"`
	EmpresaID    int64      `json:"empresa_id"`
	ClienteID    *int64     `json:"cliente_id"`
	Componente   string     `json:"componente"`
	Accion       string     `json:"accion"`
	Causa        string     `json:"causa"`
	ConectorID   int64      `json:"conector_id"`
	EntranteID   *int64     `json:"entrante_id"`
	Descarte     *string    `json:"descarte"`
}

// ResultadoCorte: cómo quedó la solicitud.
type ResultadoCorte struct {
	Estado   string `json:"estado"`
	OrdenID  *int64 `json:"orden_id"`
	Repetido bool   `json:"repetido,omitempty"`
	// Ignorado: el aviso no se aplicó (descarte, otra empresa, servicio o acción); queda anotado.
	Ignorado string `json:"ignorado,omitempty"`
}

// CorteManualOrden es lo que la orden muestra de su solicitud de corte.
type CorteManualOrden struct {
	SolicitudID       int64      `json:"solicitudId" doc:"Id de la solicitud en Argos"`
	Accion            string     `json:"accion" enum:"corte,reconexion"`
	Causa             string     `json:"causa" doc:"Código de Argos (sin_onu, instancia_no_operativa, smartolt_fallo, catv_no_soportado u otra)"`
	CausaTexto        string     `json:"causaTexto" doc:"Por qué SmartOLT no pudo, para mostrar"`
	Componente        string     `json:"componente" doc:"Solo se toca este componente (cable; en un servicio mixto, el internet sigue igual)"`
	Estado            string     `json:"estado" enum:"abierta,en_curso,resuelta,no_realizada,cancelada" doc:"Lo que Iris informó (o informará) a Argos"`
	MotivoCodigo      *string    `json:"motivoCodigo,omitempty" enum:"sin_acceso,equipo_no_encontrado,otro"`
	CancelacionPedida bool       `json:"cancelacionPedida,omitempty" doc:"Argos canceló con el técnico ya en camino o en sitio: si no se hizo, al cerrar queda cancelada"`
	SolicitadaEn      *time.Time `json:"solicitadaEn,omitempty"`
}

func corteDeFila(c campobd.CampoCortesManuale) *CorteManualOrden {
	return &CorteManualOrden{SolicitudID: c.SolicitudID, Accion: c.Accion, Causa: c.Causa, CausaTexto: TextoCausa(c.Causa),
		Componente: c.Componente, Estado: c.Estado, MotivoCodigo: c.MotivoCodigo, CancelacionPedida: c.CancelacionPedida,
		SolicitadaEn: c.SolicitadaEn}
}

var reDescarte = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

func descarteValido(d string) string {
	if reDescarte.MatchString(d) {
		return d
	}
	return "orden_no_creada"
}

// nuevoRegistrarCorte aplica argos.corte_manual en una transacción (actor sistema).
func (m *Modulo) nuevoRegistrarCorte() *auditoria.Caso[EntradaCorteArgos, ResultadoCorte] {
	return auditoria.Nuevo(auditoria.Caso[EntradaCorteArgos, ResultadoCorte]{
		Accion: "ordenes.corte_manual", Permiso: nucleo.PermOrdenesCrear,
		AuditarExito: func(r ResultadoCorte) bool { return !r.Repetido || r.Ignorado != "" },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaCorteArgos) (ResultadoCorte, error) {
			if contexto.ActorDe(ctx).Tipo != contexto.ActorSistema {
				return ResultadoCorte{}, nucleo.Prohibido("El corte manual lo registra el sistema")
			}
			if in.SolicitudID <= 0 || in.Version <= 0 || in.ServicioID <= 0 || in.EmpresaID <= 0 ||
				(in.Accion != argos.CorteAccionCorte && in.Accion != argos.CorteAccionReconexion) {
				return ResultadoCorte{}, nucleo.ReglaNegocio("corte_invalido", "El aviso de corte manual no trae lo que pide el contrato")
			}
			t.FijarEntidad("corte_manual", in.SolicitudID)
			// Una solicitud a la vez (dos avisos de la misma solicitud en paralelo no la duplican). La
			// clave es del conector: uno reemplazado no choca con las solicitudes del anterior.
			if _, err := t.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('campo.corte_manual:' || $1::bigint::text || ':' || $2::bigint::text, 0))`,
				in.ConectorID, in.SolicitudID); err != nil {
				return ResultadoCorte{}, err
			}
			q := campobd.New(t)
			clave := campobd.CorteManualPorClaveParams{ConectorID: in.ConectorID, SolicitudID: in.SolicitudID}
			previa, err := q.CorteManualPorClave(ctx, clave)
			if errors.Is(err, pgx.ErrNoRows) {
				// La empresa del payload solo vale si el núcleo la validó (sin descarte).
				if in.Descarte == nil {
					t.FijarEmpresa(in.EmpresaID)
				}
				return m.corteNuevo(ctx, t, q, in)
			}
			if err != nil {
				return ResultadoCorte{}, err
			}
			// Candados en el orden de campo: primero la orden, después la solicitud.
			if previa.OrdenID != nil {
				if _, err := q.OrdenPorIDParaActualizar(ctx, *previa.OrdenID); err != nil {
					return ResultadoCorte{}, err
				}
			}
			c, err := q.CorteManualParaActualizar(ctx, campobd.CorteManualParaActualizarParams(clave))
			if err != nil {
				return ResultadoCorte{}, err
			}
			if !mismoID(c.OrdenID, previa.OrdenID) {
				// La orden cambió entre la lectura y el candado (no debería): el relay reintenta.
				return ResultadoCorte{}, fmt.Errorf("corte manual %d: la orden cambió mientras se bloqueaba", in.SolicitudID)
			}
			// La empresa es la de la solicitud guardada, nunca la del aviso.
			t.FijarEmpresa(c.EmpresaID)
			if motivo := noCoincide(c, in); motivo != "" {
				// B9 y revisión de #297: un aviso con descarte, o de otra empresa, servicio o acción, no toca
				// la solicitud ni su orden. Solo queda anotado.
				t.Anotar(map[string]any{"ignorado": motivo, "version": in.Version})
				return ResultadoCorte{Estado: c.Estado, OrdenID: c.OrdenID, Ignorado: motivo}, nil
			}
			if in.Version <= c.VersionArgos {
				// Repetido o atrasado (B3): Argos solo manda versiones crecientes por solicitud.
				t.Anotar(map[string]any{"repetido": true, "version": in.Version, "version_guardada": c.VersionArgos})
				return ResultadoCorte{Estado: c.Estado, OrdenID: c.OrdenID, Repetido: true}, nil
			}
			c.VersionArgos, c.EntranteID = in.Version, in.EntranteID
			if in.Evento == "cancelado" {
				return m.corteCancelado(ctx, t, q, c)
			}
			if c.Estado == CorteDescartada || c.Estado == CorteCanceladaAntes {
				// Un solicitado más nuevo que el descarte o la cancelación: se evalúa como uno nuevo.
				return m.corteAbrir(ctx, t, q, in, &c)
			}
			if !terminalCorte(c.Estado) {
				c.Causa = in.Causa
			}
			c, err = guardarCorte(ctx, q, c)
			return ResultadoCorte{Estado: c.Estado, OrdenID: c.OrdenID}, err
		},
	})
}

// noCoincide: por qué un aviso de una solicitud conocida no se aplica ("" = se aplica).
func noCoincide(c campobd.CampoCortesManuale, in EntradaCorteArgos) string {
	switch {
	case in.Descarte != nil:
		return "descarte_" + descarteValido(*in.Descarte)
	case c.EmpresaID != in.EmpresaID:
		return "empresa_no_coincide"
	case c.ServicioID != in.ServicioID:
		return "servicio_no_coincide"
	case c.Accion != in.Accion:
		return "accion_no_coincide"
	}
	return ""
}

func guardarCorte(ctx context.Context, q *campobd.Queries, c campobd.CampoCortesManuale) (campobd.CampoCortesManuale, error) {
	return q.GuardarCorteManual(ctx, campobd.GuardarCorteManualParams{
		ConectorID: c.ConectorID, SolicitudID: c.SolicitudID, VersionArgos: c.VersionArgos, VersionIris: c.VersionIris, Estado: c.Estado,
		Descarte: c.Descarte, MotivoCodigo: c.MotivoCodigo, CancelacionPedida: c.CancelacionPedida, OrdenID: c.OrdenID,
		Causa: c.Causa, EntranteID: c.EntranteID,
	})
}

// corteNuevo: la primera vez que Iris sabe de la solicitud.
func (m *Modulo) corteNuevo(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaCorteArgos) (ResultadoCorte, error) {
	if in.Evento == "cancelado" && in.Descarte != nil {
		// Un cancelado fuera del alcance: solo se anota (no se guarda nada de otra empresa).
		motivo := "descarte_" + descarteValido(*in.Descarte)
		t.Anotar(map[string]any{"ignorado": motivo})
		return ResultadoCorte{Ignorado: motivo}, nil
	}
	if in.Evento == "cancelado" {
		// B4: el cancelado llegó antes que el solicitado. Se guarda y no se abre nada.
		c, err := q.CrearCorteManual(ctx, paramsCorte(in, CorteCanceladaAntes, nil, nil))
		return ResultadoCorte{Estado: c.Estado}, err
	}
	return m.corteAbrir(ctx, t, q, in, nil)
}

func paramsCorte(in EntradaCorteArgos, estado string, descarte *string, orden *int64) campobd.CrearCorteManualParams {
	componente := in.Componente
	if componente == "" {
		componente = argos.TipoCable
	}
	return campobd.CrearCorteManualParams{SolicitudID: in.SolicitudID, ConectorID: in.ConectorID, EmpresaID: in.EmpresaID,
		ServicioID: in.ServicioID, ClienteID: in.ClienteID, Accion: in.Accion, Causa: in.Causa, Componente: componente,
		SolicitadaEn: in.SolicitadaEn, VersionArgos: in.Version, Estado: estado, Descarte: descarte, OrdenID: orden, EntranteID: in.EntranteID}
}

// corteAbrir abre la orden del solicitado (o lo descarta, B9). previa: la fila existente (descartada o
// cancelada_antes con una versión más nueva), o nil.
func (m *Modulo) corteAbrir(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaCorteArgos, previa *campobd.CampoCortesManuale) (ResultadoCorte, error) {
	var descarte *string
	var orden *Orden
	if in.Descarte != nil {
		d := descarteValido(*in.Descarte)
		descarte = &d
	} else {
		o, err := m.ordenDeCorte(ctx, q, in)
		var pr *nucleo.Problema
		switch {
		case errors.As(err, &pr) && pr.Status < 500:
			// La orden no se pudo crear (sin cliente o servicio en Argos): no se abre nada, queda a la vista.
			d := descarteValido(pr.Codigo())
			descarte = &d
		case err != nil:
			return ResultadoCorte{}, err
		default:
			orden = &o
		}
	}
	estado := CorteAbierta
	if descarte != nil {
		estado = CorteDescartada
	}
	var ordenID *int64
	if orden != nil {
		ordenID = &orden.ID
		t.Anotar(map[string]any{"orden_id": orden.ID, "numero": orden.Numero})
	} else {
		t.Anotar(map[string]any{"descarte": *descarte})
	}
	if previa == nil {
		c, err := q.CrearCorteManual(ctx, paramsCorte(in, estado, descarte, ordenID))
		if err != nil {
			return ResultadoCorte{}, err
		}
		return ResultadoCorte{Estado: c.Estado, OrdenID: c.OrdenID}, m.corteTrasAbrir(ctx, t, q, c, orden)
	}
	c := *previa
	c.Estado, c.Descarte, c.OrdenID, c.Causa = estado, descarte, ordenID, in.Causa
	c, err := guardarCorte(ctx, q, c)
	if err != nil {
		return ResultadoCorte{}, err
	}
	return ResultadoCorte{Estado: c.Estado, OrdenID: c.OrdenID}, m.corteTrasAbrir(ctx, t, q, c, orden)
}

// corteTrasAbrir: si la orden nació ya asignada (cuadrilla de la zona), avisa en_curso.
func (m *Modulo) corteTrasAbrir(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, _ campobd.CampoCortesManuale, orden *Orden) error {
	if orden == nil {
		return nil
	}
	f, err := q.OrdenPorID(ctx, orden.ID)
	if err != nil {
		return err
	}
	return avisarCorte(ctx, t, q, f)
}

// ordenDeCorte crea la orden corte_catv o reconexion_catv (idempotente por solicitud_id).
func (m *Modulo) ordenDeCorte(ctx context.Context, q *campobd.Queries, in EntradaCorteArgos) (Orden, error) {
	tipo, accion := TipoCorteCATV, "Cortar"
	prioridad := PrioridadNormal
	if in.Accion == argos.CorteAccionReconexion {
		tipo, accion, prioridad = TipoReconexionCATV, "Reconectar", PrioridadAlta
	}
	descripcion := fmt.Sprintf("%s el cable (TV) a mano en campo o en la OLT: Argos no pudo hacerlo por SmartOLT (%s). Solicitud %d de Argos.",
		accion, TextoCausa(in.Causa), in.SolicitudID)
	if s, err := q.ServicioArgos(ctx, in.ServicioID); err == nil && argos.IncluyeCable(s.TipoServicio, s.Componentes) &&
		argos.ConRouter(s.TipoServicio, s.Componentes, s.RouterID, true) {
		// B8: servicio mixto.
		descripcion += " Es un servicio mixto: solo se toca el cable; el internet sigue igual."
	}
	entrada := EntradaCrearOrden{Tipo: tipo, EmpresaID: &in.EmpresaID, ClienteID: in.ClienteID, ServicioID: &in.ServicioID,
		Descripcion: descripcion, ClaveIdempotencia: "argos.corte_manual:" + strconv.FormatInt(in.ConectorID, 10) + ":" + strconv.FormatInt(in.SolicitudID, 10), Origen: "argos",
		prioridad: prioridad}
	if entrada.ClienteID == nil {
		if s, err := q.ServicioArgos(ctx, in.ServicioID); err == nil {
			entrada.ClienteID = &s.ClienteID
		}
	}
	if entrada.ClienteID == nil {
		return Orden{}, nucleo.ReglaNegocio("servicio_desconocido", "Iris no conoce el cliente del servicio")
	}
	if !tieneDireccion(ctx, q, in.ServicioID, entrada.ClienteID) {
		entrada.Direccion = ptr(fmt.Sprintf("Por confirmar (servicio %d de Argos)", in.ServicioID))
	}
	return m.CrearOrden.Ejecutar(ctx, entrada)
}

// corteCancelado aplica servicio.corte_manual_cancelado a una solicitud conocida (B4).
func (m *Modulo) corteCancelado(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, c campobd.CampoCortesManuale) (ResultadoCorte, error) {
	switch c.Estado {
	case CorteResuelta:
		// Llegó tarde y el corte ya se hizo: se informa resuelta otra vez (Argos pide la reconexión).
		c.VersionIris++
		c, err := guardarCorte(ctx, q, c)
		if err != nil {
			return ResultadoCorte{}, err
		}
		return ResultadoCorte{Estado: c.Estado, OrdenID: c.OrdenID}, emitirEstadoCorte(ctx, t, q, c, m.ahora())
	case CorteAbierta, CorteEnCurso:
		if c.OrdenID == nil {
			break
		}
		f, err := q.OrdenPorIDParaActualizar(ctx, *c.OrdenID)
		if err != nil {
			return ResultadoCorte{}, err
		}
		if f.Estado == EstadoEnCamino || f.Estado == EstadoEnSitio || f.Estado == EstadoPausada {
			// El técnico ya va o está ahí (quizá sin señal): decide el cierre.
			c.CancelacionPedida = true
			c, err = guardarCorte(ctx, q, c)
			t.Anotar(map[string]any{"cancelacion_pedida": true, "orden_estado": f.Estado})
			return ResultadoCorte{Estado: c.Estado, OrdenID: c.OrdenID}, err
		}
		c.Estado, c.VersionIris = CorteCancelada, c.VersionIris+1
		if c, err = guardarCorte(ctx, q, c); err != nil {
			return ResultadoCorte{}, err
		}
		if err := emitirEstadoCorte(ctx, t, q, c, m.ahora()); err != nil {
			return ResultadoCorte{}, err
		}
		// La orden se cancela después de dejar la solicitud terminal: el aviso de la orden no la toca.
		if _, err := m.CancelarOrden.Ejecutar(ctx, EntradaMotivo{ID: f.ID, Version: f.Version, Motivo: "Argos canceló la solicitud de corte manual"}); err != nil {
			return ResultadoCorte{}, err
		}
		return ResultadoCorte{Estado: c.Estado, OrdenID: c.OrdenID}, nil
	case CorteDescartada:
		c.Estado, c.Descarte = CorteCancelada, nil
	}
	c, err := guardarCorte(ctx, q, c)
	return ResultadoCorte{Estado: c.Estado, OrdenID: c.OrdenID}, err
}

// avisarCorte: lo que pasa en la orden de un corte manual se informa a Argos (en la transacción del
// cambio). Sin solicitud o con la solicitud terminal no hace nada (los terminales no retroceden).
func avisarCorte(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, f campobd.CampoOrden) error {
	c, err := q.CorteManualDeOrdenParaActualizar(ctx, &f.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if c.Estado != CorteAbierta && c.Estado != CorteEnCurso {
		return nil
	}
	estado, motivo := "", (*string)(nil)
	otro := "otro"
	switch f.Estado {
	case EstadoCerrada:
		res, err := q.ResultadoCorteDeOrden(ctx, f.ID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		codigo, conocido := resultadosCorte[res]
		switch {
		case res == "" && c.CancelacionPedida:
			// Sin la marca (no debería: el ítem es obligatorio y la plantilla del sistema no lo deja quitar)
			// y con la cancelación pedida: no se da por hecho.
			estado = CorteCancelada
		case res == "" || (conocido && codigo == ""):
			estado = CorteResuelta // Hecho (o, sin la marca, el cierre lo da por hecho)
		case c.CancelacionPedida:
			estado = CorteCancelada
		default:
			if !conocido {
				codigo = otro
			}
			estado, motivo = CorteNoRealizada, &codigo
		}
	case EstadoFallida:
		if c.CancelacionPedida {
			estado = CorteCancelada
			break
		}
		codigo := otro
		if f.MotivoFalla != nil && *f.MotivoFalla == "sin_acceso" {
			codigo = "sin_acceso"
		}
		estado, motivo = CorteNoRealizada, &codigo
	case EstadoCancelada:
		// Cancelada por una persona en Iris: no se hará.
		estado = CorteCancelada
		if !c.CancelacionPedida {
			estado, motivo = CorteNoRealizada, &otro
		}
	default:
		if c.Estado == CorteAbierta && (f.TecnicoID != nil || f.CuadrillaID != nil) {
			estado = CorteEnCurso
		}
	}
	if estado == "" {
		return nil
	}
	c.Estado, c.MotivoCodigo, c.VersionIris = estado, motivo, c.VersionIris+1
	c, err = guardarCorte(ctx, q, c)
	if err != nil {
		return err
	}
	return emitirEstadoCorte(ctx, t, q, c, time.Now())
}

// emitirEstadoCorte deja corte_manual.estado_cambiado en el outbox (lo entrega mensajería a Argos,
// solo con contrato ≥ 0.15.0). referencia: el número de la orden.
func emitirEstadoCorte(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, c campobd.CampoCortesManuale, ahora time.Time) error {
	var referencia, tipoRef *string
	if c.OrdenID != nil {
		o, err := q.OrdenPorID(ctx, *c.OrdenID)
		if err != nil {
			return err
		}
		referencia, tipoRef = &o.Numero, ptr("orden")
	}
	return t.Emitir(EventoCorteEstado, map[string]any{
		"solicitud_id": c.SolicitudID, "empresa_id": c.EmpresaID, "version": c.VersionIris, "estado": c.Estado,
		"motivo_codigo": c.MotivoCodigo, "referencia": referencia, "referencia_tipo": tipoRef,
		"ocurrido_en": ahora.UTC().Format(time.RFC3339Nano), "orden_id": c.OrdenID, "servicio_id": c.ServicioID, "accion": c.Accion,
		// conector_id: mensajería solo lo manda al conector que pidió el corte (nunca viaja a Argos).
		"conector_id": c.ConectorID,
	})
}

// AlCorteManual procesa argos.corte_manual (actor sistema del relay). Un payload ilegible o una regla
// de negocio no se reintentan (log); un error de la base, sí.
func (m *Modulo) AlCorteManual(ctx context.Context, e integracion.Evento, log *slog.Logger) error {
	var in EntradaCorteArgos
	if err := json.Unmarshal(e.Payload, &in); err != nil {
		log.WarnContext(ctx, "campo: argos.corte_manual ilegible", "evento", e.ID, "error", err)
		return nil
	}
	r, err := m.RegistrarCorte.Ejecutar(ctx, in)
	var pr *nucleo.Problema
	if errors.As(err, &pr) && pr.Status < 500 {
		log.WarnContext(ctx, "campo: corte manual no aplicado", "solicitud", in.SolicitudID, "codigo", pr.Codigo())
		return nil
	}
	if err != nil {
		return err
	}
	log.InfoContext(ctx, "campo: corte manual", "solicitud", in.SolicitudID, "evento", in.Evento, "estado", r.Estado, "orden", r.OrdenID)
	return nil
}

// SuscribirCortes conecta argos.corte_manual (en cmd/worker).
func (m *Modulo) SuscribirCortes(bus *integracion.Bus, bd nucleo.BD, log *slog.Logger) {
	bus.Suscribir("campo.corte_manual", argos.EventoInternoCorteManual, func(ctx context.Context, e integracion.Evento) error {
		return m.AlCorteManual(nucleo.ConBD(ctx, bd), e, log)
	})
}
