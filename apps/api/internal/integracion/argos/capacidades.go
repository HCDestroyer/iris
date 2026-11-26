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
			return CapacidadesConector{ContratoIris: m.contratoIris(), Capacidades: CapacidadesDeIris(m.contratoIris())}, nil
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
