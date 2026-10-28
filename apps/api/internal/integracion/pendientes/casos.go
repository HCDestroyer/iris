package pendientes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	pendientesbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Pendiente es una acción hacia Argos tal como la ve el panel.
type Pendiente struct {
	ID               int64          `json:"id"`
	Sistema          string         `json:"sistema" enum:"argosisp,horus"`
	Operacion        string         `json:"operacion" doc:"activar_instalacion, asignar_comodato, consumo, devolucion, perdida, retiro, cargo"`
	IdempotencyKey   string         `json:"idempotencyKey" doc:"Idempotency-Key del contrato (iris:<operacion>:<id>)"`
	Entidad          string         `json:"entidad" doc:"orden (más adelante prospecto)"`
	EntidadID        string         `json:"entidadId"`
	Metodo           string         `json:"metodo"`
	Ruta             string         `json:"ruta" doc:"Ruta en la API de Argos (bajo /api/v1)"`
	Payload          map[string]any `json:"payload" doc:"Cuerpo de la llamada"`
	DependeDe        []int64        `json:"dependeDe" doc:"Pendientes que deben estar hechos antes"`
	DatosIncompletos bool           `json:"datosIncompletos" doc:"La orden no traía un dato obligatorio: no se puede reintentar"`
	Estado           string         `json:"estado" enum:"pendiente,en_curso,hecho,fallido_negocio,agotado,bloqueado" doc:"bloqueado: una dependencia quedó fallido_negocio o agotado (ultimoError = depende_de_<id>_fallido); vuelve a pendiente cuando la dependencia termina hecha"`
	Intentos         int32          `json:"intentos"`
	ProximoIntento   *time.Time     `json:"proximoIntento" nullable:"true" doc:"Solo en pendiente"`
	UltimoError      *string        `json:"ultimoError" nullable:"true" doc:"Resumen del último error (con el detail de Argos en un 4xx de negocio)"`
	UltimoStatus     *int32         `json:"ultimoStatus" nullable:"true" doc:"HTTP de Argos del último intento (null si no respondió)"`
	HechoEn          *time.Time     `json:"hechoEn" nullable:"true"`
	Conflicto        *string        `json:"conflicto" nullable:"true" doc:"cierre_repetido_con_otros_datos: La orden se volvió a cerrar con otros datos para esta acción y no se pisó: corrígelo a mano en Argos (payloadConflicto = datos nuevos)"`
	PayloadConflicto map[string]any `json:"payloadConflicto" nullable:"true"`
	ConflictoEn      *time.Time     `json:"conflictoEn" nullable:"true"`
	CreadoEn         time.Time      `json:"creadoEn"`
	Version          int32          `json:"version"`

	empresaID *int64
}

func aPendiente(f pendientesbd.IntegracionPendiente) Pendiente {
	p := Pendiente{
		ID: f.ID, Sistema: f.Sistema, Operacion: f.Operacion, IdempotencyKey: f.IdempotencyKey, Entidad: f.Entidad,
		EntidadID: f.EntidadID, Metodo: f.Metodo, Ruta: f.Ruta, DependeDe: f.DependeDe, DatosIncompletos: f.DatosIncompletos,
		Estado: f.Estado, Intentos: f.Intentos, UltimoError: f.UltimoError, UltimoStatus: f.UltimoStatus,
		HechoEn: f.HechoEn, CreadoEn: f.CreatedAt, Version: f.Version, empresaID: f.EmpresaID,
	}
	if p.DependeDe == nil {
		p.DependeDe = []int64{}
	}
	if f.Estado == EstadoPendiente {
		t := f.ProximoIntento
		p.ProximoIntento = &t
	}
	_ = json.Unmarshal(f.Payload, &p.Payload)
	p.Conflicto, p.ConflictoEn = f.Conflicto, f.ConflictoEn
	if len(f.ConflictoPayload) > 0 {
		_ = json.Unmarshal(f.ConflictoPayload, &p.PayloadConflicto)
	}
	if p.Payload == nil {
		p.Payload = map[string]any{}
	}
	return p
}

// filtroEmpresa: sin conector → empresa_id null; con empresa activa → esa; actor sistema → todas.
func filtroEmpresa(ctx context.Context) (todas bool, empresa *int64) {
	switch contexto.EmpresasDe(ctx).Estado {
	case contexto.EmpresaActiva:
		e := contexto.EmpresaDe(ctx)
		return false, &e
	case contexto.EmpresaNoResuelta:
		return true, nil
	}
	return false, nil
}

func visible(ctx context.Context, empresa *int64) bool {
	todas, activa := filtroEmpresa(ctx)
	if todas {
		return true
	}
	if activa == nil || empresa == nil {
		return activa == nil && empresa == nil
	}
	return *activa == *empresa
}

// EntradaListar filtra la cola.
type EntradaListar struct {
	nucleo.ParamsPagina
	Entidad   string `query:"entidad" pattern:"^[a-z][a-z_]{0,39}$" doc:"orden…"`
	EntidadID string `query:"entidadId" maxLength:"64"`
	Estado    string `query:"estado" enum:"pendiente,en_curso,hecho,fallido_negocio,agotado,bloqueado,"`
	Conflicto bool   `query:"conflicto" doc:"Solo los que tienen un conflicto por cierre repetido"`
}

func texto(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nuevoListar(*Modulo) *auditoria.Caso[EntradaListar, nucleo.Pagina[Pendiente]] {
	return auditoria.Nuevo(auditoria.Caso[EntradaListar, nucleo.Pagina[Pendiente]]{
		Accion: "integraciones.ver_pendientes", AuditarExito: func(nucleo.Pagina[Pendiente]) bool { return false },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaListar) (nucleo.Pagina[Pendiente], error) {
			var despues int64
			if _, err := nucleo.DecodificarCursor(in.Cursor, &despues); err != nil {
				return nucleo.Pagina[Pendiente]{}, err
			}
			todas, empresa := filtroEmpresa(ctx)
			limite := in.Limite()
			filas, err := pendientesbd.New(t).ListarPendientes(ctx, pendientesbd.ListarPendientesParams{
				DespuesDe: despues, Todas: todas, EmpresaID: empresa, Entidad: texto(in.Entidad),
				EntidadID: texto(in.EntidadID), Estado: texto(in.Estado), SoloConflicto: in.Conflicto, Limite: int32(limite + 1), //nolint:gosec // acotado a 201
			})
			if err != nil {
				return nucleo.Pagina[Pendiente]{}, err
			}
			return nucleo.Paginar(filas, limite, aPendiente, func(f pendientesbd.IntegracionPendiente) []any { return []any{f.ID} }), nil
		},
	})
}

// EntradaReintentar es el reintento manual de un pendiente.
type EntradaReintentar struct {
	ID      int64 `json:"-"`
	Version int32 `json:"version" minimum:"1"`
}

type eventoReintentado struct {
	PendienteID int64  `json:"pendiente_id"`
	Operacion   string `json:"operacion"`
	Entidad     string `json:"entidad"`
	EntidadID   string `json:"entidad_id"`
	De          string `json:"de"`
}

func nuevoReintentar(m *Modulo) *auditoria.Caso[EntradaReintentar, Pendiente] {
	return auditoria.Nuevo(auditoria.Caso[EntradaReintentar, Pendiente]{
		Accion: "integraciones.reintentar",
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaReintentar) (Pendiente, error) {
			q := pendientesbd.New(t)
			f, err := q.PendientePorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return Pendiente{}, err
			}
			if !visible(ctx, f.EmpresaID) {
				return Pendiente{}, nucleo.NoEncontrado("")
			}
			t.FijarEntidad("pendiente", f.ID)
			if f.EmpresaID != nil {
				t.FijarEmpresa(*f.EmpresaID)
			}
			if err := nucleo.ExigirVersion(f.Version, in.Version, "pendiente", f.ID); err != nil {
				return Pendiente{}, err
			}
			switch {
			case f.DatosIncompletos:
				return Pendiente{}, nucleo.ReglaNegocio("pendiente_incompleto",
					"La orden no traía los datos para esta acción ("+deref(f.UltimoError)+"); vuelve a cerrar la orden con los datos "+
						"(el pendiente se corrige y vuelve a la cola) o hazlo a mano en Argos")
			case f.Estado == EstadoBloqueado:
				return Pendiente{}, nucleo.ReglaNegocio("pendiente_no_reintentable",
					"Está bloqueado ("+deref(f.UltimoError)+"): reintenta su dependencia; al quedar hecha, este vuelve a la cola")
			case f.Estado == EstadoHecho || f.Estado == EstadoEnCurso:
				return Pendiente{}, nucleo.ReglaNegocio("pendiente_no_reintentable", "El pendiente está "+f.Estado)
			}
			nueva, err := q.ReintentarPendiente(ctx, pendientesbd.ReintentarPendienteParams{Ahora: m.ahora(), ID: f.ID, Version: in.Version})
			if errors.Is(err, pgx.ErrNoRows) {
				return Pendiente{}, nucleo.ConflictoVersion("pendiente", f.ID)
			}
			if err != nil {
				return Pendiente{}, err
			}
			t.RegistrarCambios("pendiente", f.ID, map[string]any{"estado": f.Estado, "intentos": f.Intentos},
				map[string]any{"estado": nueva.Estado, "intentos": nueva.Intentos})
			if err := t.Emitir(EventoReintentado, eventoReintentado{
				PendienteID: f.ID, Operacion: f.Operacion, Entidad: f.Entidad, EntidadID: f.EntidadID, De: f.Estado,
			}); err != nil {
				return Pendiente{}, err
			}
			return aPendiente(nueva), m.avisarCola(ctx, t.Tx)
		},
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// DeEntidad devuelve los pendientes de una entidad (p. ej. para mostrar en la orden «cerrada
// con pendiente en Argos»). No filtra por empresa: quien llama ya validó la entidad.
func DeEntidad(ctx context.Context, q pendientesbd.DBTX, entidad, entidadID string) ([]Pendiente, error) {
	filas, err := pendientesbd.New(q).PendientesDeEntidad(ctx, pendientesbd.PendientesDeEntidadParams{Entidad: entidad, EntidadID: entidadID})
	if err != nil {
		return nil, err
	}
	salida := make([]Pendiente, 0, len(filas))
	for _, f := range filas {
		salida = append(salida, aPendiente(f))
	}
	return salida, nil
}

// ConPendienteEnArgos indica si alguno no está hecho («cerrada con pendiente en Argos»).
func ConPendienteEnArgos(ps []Pendiente) bool {
	for _, p := range ps {
		if p.Estado != EstadoHecho {
			return true
		}
	}
	return false
}

func errorf(formato string, args ...any) *string {
	s := fmt.Sprintf(formato, args...)
	return &s
}
