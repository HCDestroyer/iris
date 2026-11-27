package argos

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Capacidades para Argos (contrato 0.14.2 §4.3): el token del panel de Argos (rol integracion_argos,
// permiso integraciones.ver_capacidades) lee solo { contrato_iris, capacidades } de su propio
// conector. Con el token de otro conector o de uno apagado, 404; con el token_portal, 403. El rol no
// recibe integraciones.ver: GET …/estado sigue siendo solo para personas.

// RutaCapacidades es la ruta de §4.3.
const RutaCapacidades = "/api/v1/integraciones/argosisp/capacidades"

// CapacidadesConector es la respuesta.
type CapacidadesConector struct {
	ContratoIris string          `json:"contrato_iris"`
	Capacidades  CapacidadesIris `json:"capacidades"`
}

// ContratoEntregaAuto es el primer contrato con los recordatorios que entrega Iris (0.15.0 §13.3,
// bloque C): con uno anterior, capacidades no trae mensajeria[].
const ContratoEntregaAuto = "0.15.0"

// CapacidadMensajeria es una fila de capacidades.mensajeria[] (0.15.0 §4.6 y §13.3, C1 y C8).
type CapacidadMensajeria struct {
	EmpresaID   int64                `json:"empresa_id"`
	EntregaAuto bool                 `json:"entrega_auto" doc:"true solo si la empresa tiene al menos un canal declarado (C12)"`
	Canales     []CanalMensajeriaCap `json:"canales" doc:"Solo los canales que Iris implementa y tiene operativos para la empresa (hoy solo whatsapp)"`
}

// CanalMensajeriaCap es un canal declarado con sus códigos usables.
type CanalMensajeriaCap struct {
	Canal   string   `json:"canal" enum:"whatsapp,correo,telegram"`
	Codigos []string `json:"codigos" doc:"Códigos de §6.3 aprobados y usables en ese canal; uno que no está no se pide (Argos usa su texto local)"`
}

// DeclararMensajeria calcula capacidades.mensajeria[] para esas empresas (la mensajería la conecta:
// este paquete no la importa).
type DeclararMensajeria func(ctx context.Context, q argosbd.DBTX, empresas []int64) ([]CapacidadMensajeria, error)

// FijarCapacidadesMensajeria conecta el cálculo de capacidades.mensajeria[] (nil = sin la marca).
func (m *Modulo) FijarCapacidadesMensajeria(f DeclararMensajeria) { m.capMensajeria = f }

// capacidadesMensajeria: con un conector cuyo contrato acordado es ≥ 0.15.0, las empresas de su alcance
// (todas las registradas con alcance global) con lo que la mensajería declara; nil si no aplica.
func (m *Modulo) capacidadesMensajeria(ctx context.Context, t *auditoria.Tx, conectorID int64) ([]CapacidadMensajeria, error) {
	if m.capMensajeria == nil {
		return nil, nil
	}
	var contrato *string
	var empresas []int64
	var global bool
	if err := t.QueryRow(ctx, `SELECT integracion.contrato_acordado(contrato_remoto, contrato_informado), empresas, coalesce(alcance_global, false)
		FROM integracion.conectores WHERE id = $1`,
		conectorID).Scan(&contrato, &empresas, &global); err != nil {
		return nil, err
	}
	if !conector.VersionAlMenos(contrato, ContratoEntregaAuto) {
		return nil, nil
	}
	if global {
		filas, err := t.Query(ctx, `SELECT id FROM nucleo.empresas ORDER BY id`)
		if err != nil {
			return nil, err
		}
		if empresas, err = pgx.CollectRows(filas, pgx.RowTo[int64]); err != nil {
			return nil, err
		}
	}
	return m.capMensajeria(ctx, t, empresas)
}

func nuevoVerCapacidades(m *Modulo) *auditoria.Caso[struct{}, CapacidadesConector] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, CapacidadesConector]{
		Accion: "integraciones.ver_capacidades", Permiso: nucleo.PermIntegracionesVerCapacidades, SinEmpresa: true,
		AuditarExito: soloLecturas[CapacidadesConector],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (CapacidadesConector, error) {
			t.FijarEmpresa(0)
			a := contexto.ActorDe(ctx)
			noEsta := nucleo.NoEncontrado("No hay un conector Argos activo para este token")
			if a.TokenID == 0 {
				return CapacidadesConector{}, noEsta
			}
			var derivado bool
			if err := t.QueryRow(ctx, `SELECT token_padre_id IS NOT NULL FROM nucleo.tokens_api WHERE id = $1`, a.TokenID).Scan(&derivado); err != nil {
				return CapacidadesConector{}, err
			}
			if derivado {
				return CapacidadesConector{}, nucleo.Prohibido("El token_portal no lee las capacidades")
			}
			c, err := argosbd.New(t).ConectorDeTokenPanel(ctx, &a.TokenID)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && c.Estado != "activo") {
				return CapacidadesConector{}, noEsta
			}
			if err != nil {
				return CapacidadesConector{}, err
			}
			t.FijarEntidad("conector", c.ID)
			caps := CapacidadesDeIris(m.contratoIris())
			// Los bloques de la 0.15.0 (§4.6) solo con el contrato acordado con este conector ≥ 0.15.0
			// (la menor entre la de Argos y la que Iris le informó y Argos aceptó).
			var acordado *string
			if err := t.QueryRow(ctx, `SELECT integracion.contrato_acordado(contrato_remoto, contrato_informado) FROM integracion.conectores WHERE id = $1`,
				c.ID).Scan(&acordado); err != nil {
				return CapacidadesConector{}, err
			}
			if !conector.VersionAlMenos(acordado, ContratoTipoServicio) {
				caps.TipoServicio, caps.CorteManual = false, false
			}
			if caps.Mensajeria, err = m.capacidadesMensajeria(ctx, t, c.ID); err != nil {
				return CapacidadesConector{}, err
			}
			return CapacidadesConector{ContratoIris: m.contratoIris(), Capacidades: caps}, nil
		},
	})
}

func (m *Modulo) registrarCapacidades(api huma.API) {
	op := conector.Operacion(conector.EtiquetaIntegraciones, "capacidadesArgos", http.MethodGet, RutaCapacidades,
		"Capacidades de Iris para el portal de Argos", nucleo.PermIntegracionesVerCapacidades, http.StatusOK, http.StatusNotFound)
	op.Description += " Contrato con Argos 0.14.2 §4.3: solo el token del panel de Argos (rol `integracion_argos`). Responde `{ contrato_iris, " +
		"capacidades }` del conector al que pertenece el token; con el de otro conector o uno apagado, 404; con el `token_portal`, 403."
	huma.Register(api, op, func(ctx context.Context, _ *struct{}) (*conector.Cuerpo[CapacidadesConector], error) {
		return conector.Responder(m.VerCapacidades.Ejecutar(ctx, struct{}{}))
	})
}
