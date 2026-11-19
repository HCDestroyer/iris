package conector

import (
	"context"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	conectorbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/conector/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Eliminar conectores apagados que ya no sirven (integraciones.eliminar): borrado lógico. La fila
// queda (entrantes, pendientes y auditoría la referencian), sus credenciales cifradas se borran y
// deja de salir en la lista. El iris_pat_ que Iris le entregó se revoca (si aún valía) y se
// elimina con él. Emite conector.eliminado.

// EntradaEliminarConector es la entrada de integraciones.eliminar.
type EntradaEliminarConector struct {
	ID     int64  `json:"-"`
	Motivo string `json:"motivo,omitempty" maxLength:"500" doc:"Nota libre (opcional); va en la auditoría"`
	// origen es el motivo del evento (manual, lote); no sale de la entrada HTTP.
	origen string
}

// ConectoresEliminables cuenta lo que eliminaría POST …/conectores/eliminar-apagados.
type ConectoresEliminables struct {
	Cantidad int `json:"cantidad" doc:"Conectores apagados que se pueden eliminar"`
}

func nuevoContarConectoresEliminables() *auditoria.Caso[struct{}, ConectoresEliminables] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, ConectoresEliminables]{
		Accion: "integraciones.contar_eliminables", Permiso: nucleo.PermIntegracionesEliminar, SinEmpresa: true,
		AuditarExito: soloLecturas[ConectoresEliminables],
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (ConectoresEliminables, error) {
			t.FijarEmpresa(0)
			ids, err := conectorbd.New(t).ConectoresEliminables(ctx)
			return ConectoresEliminables{Cantidad: len(ids)}, err
		},
	})
}

// ConectoresEliminados es la respuesta del lote.
type ConectoresEliminados struct {
	Eliminados int     `json:"eliminados"`
	IDs        []int64 `json:"ids" doc:"Ids de los conectores eliminados"`
}

// esEliminado: un conector eliminado no existe para la API (404) ni para los webhooks.
func esEliminado(f conectorbd.IntegracionConectore) bool { return f.EliminadoEn != nil }

func nuevoEliminarConector(n *Nucleo) *auditoria.Caso[EntradaEliminarConector, Conector] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEliminarConector, Conector]{
		Accion: "integraciones.eliminar", Permiso: nucleo.PermIntegracionesEliminar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEliminarConector) (Conector, error) {
			q := conectorbd.New(t)
			t.FijarEntidad("conector", in.ID)
			t.FijarEmpresa(0)
			antes, err := q.ConectorPorIDParaActualizar(ctx, in.ID)
			if err != nil {
				return Conector{}, err
			}
			// Idempotente: eliminarlo otra vez responde lo mismo, sin cambios ni evento.
			if esEliminado(antes) {
				t.Anotar(map[string]any{"ya_eliminado": true})
				return n.AConector(antes), nil
			}
			if antes.Estado == "activo" {
				return Conector{}, nucleo.Conflicto("conector_activo", "El conector está activo: desactívalo primero y luego elimínalo")
			}
			reemplazado, err := q.ConectorReemplazadoPorActivo(ctx, in.ID)
			if err != nil {
				return Conector{}, err
			}
			if reemplazado {
				return Conector{}, nucleo.Conflicto("conector_en_uso",
					"El conector activo lo reemplazó hace poco y el otro sistema aún usa su acceso (como mucho 24 h). Vuelve a intentarlo cuando la reconexión se confirme")
			}
			var por *int64
			if a := t.Actor(); a.EsUsuario() {
				por = &a.ID
			}
			despues, err := q.EliminarConector(ctx, conectorbd.EliminarConectorParams{ID: in.ID, EliminadoPor: por})
			if err != nil {
				return Conector{}, err
			}
			t.RegistrarCambios("conector", in.ID, n.AConector(antes), n.AConector(despues))
			// Su iris_pat_ (revocado al apagarse o vencido) se elimina con él.
			if antes.TokenEntregadoID != nil {
				tokens := n.tokensFijados()
				if tokens == nil {
					return Conector{}, errSinTokens
				}
				if err := tokens.EliminarTokenIntegracion(ctx, *antes.TokenEntregadoID); err != nil {
					return Conector{}, err
				}
			}
			motivo := in.origen
			if motivo == "" {
				motivo = "manual"
			}
			return n.AConector(despues), t.Emitir("conector.eliminado", map[string]any{
				"conector_id": in.ID, "sistema": despues.Sistema, "motivo": motivo, "token_id": antes.TokenEntregadoID,
			})
		},
	})
}

// nuevoEliminarConectoresApagados elimina en lote los apagados que ningún activo reemplaza; cada
// uno pasa por integraciones.eliminar (anidado: su auditoría y su evento, motivo lote).
func nuevoEliminarConectoresApagados(uno *auditoria.Caso[EntradaEliminarConector, Conector]) *auditoria.Caso[struct{}, ConectoresEliminados] {
	return auditoria.Nuevo(auditoria.Caso[struct{}, ConectoresEliminados]{
		Accion: "integraciones.eliminar_apagados", Permiso: nucleo.PermIntegracionesEliminar, SinEmpresa: true,
		Logica: func(ctx context.Context, t *auditoria.Tx, _ struct{}) (ConectoresEliminados, error) {
			t.FijarEmpresa(0)
			ids, err := conectorbd.New(t).ConectoresEliminables(ctx)
			if err != nil {
				return ConectoresEliminados{}, err
			}
			for _, id := range ids {
				if _, err := uno.Ejecutar(ctx, EntradaEliminarConector{ID: id, origen: "lote"}); err != nil {
					return ConectoresEliminados{}, err
				}
			}
			if ids == nil {
				ids = []int64{}
			}
			t.Anotar(map[string]any{"eliminados": ids})
			return ConectoresEliminados{Eliminados: len(ids), IDs: ids}, nil
		},
	})
}
