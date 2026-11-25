package campo

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// El titular cambió en Argos (servicio.titular_cambiado, contrato 0.10.0 §2.3; pedido B17): las
// órdenes abiertas del servicio pasan al cliente nuevo. Emite orden.titular_movido por orden.

// EventoTitularMovido: una orden abierta pasó al titular nuevo.
const EventoTitularMovido = "orden.titular_movido"

// EntradaTitularArgos es el payload de argos.titular_cambiado.
type EntradaTitularArgos struct {
	ServicioID  int64  `json:"servicio_id"`
	DeClienteID *int64 `json:"de_cliente_id"`
	AClienteID  int64  `json:"a_cliente_id"`
	EntranteID  int64  `json:"entrante_id"`
	// TipoTitular: persona o empresa (contrato 0.14.1; ausente con un Argos anterior).
	TipoTitular *string `json:"tipo_titular,omitempty"`
}

// ResultadoTitularArgos: las órdenes movidas.
type ResultadoTitularArgos struct {
	Ordenes []int64 `json:"ordenes"`
}

func (m *Modulo) nuevoMoverAlTitular() *auditoria.Caso[EntradaTitularArgos, ResultadoTitularArgos] {
	return auditoria.Nuevo(auditoria.Caso[EntradaTitularArgos, ResultadoTitularArgos]{
		Accion: "ordenes.titular_cambiado", Acceso: auditoria.Autenticado, SinEmpresa: true,
		AuditarExito: func(r ResultadoTitularArgos) bool { return len(r.Ordenes) > 0 },
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaTitularArgos) (ResultadoTitularArgos, error) {
			r := ResultadoTitularArgos{Ordenes: []int64{}}
			if contexto.ActorDe(ctx).Tipo != contexto.ActorSistema {
				return r, nucleo.Prohibido("Solo el sistema aplica los avisos de Argos")
			}
			if in.ServicioID <= 0 || in.AClienteID <= 0 {
				return r, nil
			}
			q := campobd.New(t)
			// Solo si la instantánea sigue diciendo que el servicio es de a_cliente_id (MEDIA 2 de
			// #280): un aviso atrasado (A→B después de B→C) no devuelve las órdenes a B.
			actual, err := q.TitularDeServicioArgos(ctx, in.ServicioID)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && actual != in.AClienteID) {
				t.Anotar(map[string]any{"titular": "no_coincide_instantanea", "entrante_id": in.EntranteID})
				return r, nil
			}
			if err != nil {
				return r, err
			}
			filas, err := q.OrdenesAbiertasDeServicio(ctx, campobd.OrdenesAbiertasDeServicioParams{
				ServicioID: &in.ServicioID, ClienteID: &in.AClienteID,
			})
			if err != nil {
				return r, err
			}
			for _, antes := range filas {
				// Con guardar: versión nueva, feed del técnico y diff en la auditoría.
				d := antes
				d.ClienteID = &in.AClienteID
				f, err := guardar(ctx, t, q, antes, d)
				if err != nil {
					return r, err
				}
				r.Ordenes = append(r.Ordenes, f.ID)
				if f.EmpresaID != nil {
					t.FijarEmpresa(*f.EmpresaID)
				} else {
					t.FijarEmpresa(0)
				}
				if err := t.Emitir(EventoTitularMovido, map[string]any{
					"orden_id": f.ID, "numero": f.Numero, "servicio_id": in.ServicioID, "de_cliente_id": in.DeClienteID,
					"a_cliente_id": in.AClienteID, "tipo_titular": in.TipoTitular, "entrante_id": in.EntranteID,
				}); err != nil {
					return r, err
				}
			}
			t.FijarEmpresa(0)
			t.Anotar(map[string]any{"ordenes": r.Ordenes, "entrante_id": in.EntranteID})
			return r, nil
		},
	})
}

// SuscribirTitularArgos conecta argos.titular_cambiado (en cmd/worker). Idempotente.
func (m *Modulo) SuscribirTitularArgos(bus *integracion.Bus, bd nucleo.BD, log *slog.Logger) {
	bus.Suscribir("campo.titular_argos", argos.EventoTitularCambiado, func(ctx context.Context, e integracion.Evento) error {
		var in EntradaTitularArgos
		if err := json.Unmarshal(e.Payload, &in); err != nil {
			log.ErrorContext(ctx, "campo: argos.titular_cambiado ilegible", "evento", e.ID, "err", err)
			return nil
		}
		_, err := m.MoverAlTitular.Ejecutar(nucleo.ConBD(ctx, bd), in)
		return err
	})
}
