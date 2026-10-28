package campo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Pausar y reanudar una orden en curso (en camino o en sitio). Una orden pausada sigue abierta:
// conserva su cita (no sale de la ruta ni libera la agenda del técnico), admite evidencias y se
// puede cancelar o reprogramar (eso cierra la pausa). No se cierra, falla, sale ni llega sin
// reanudar antes (422 orden_pausada): reanudar devuelve el estado de antes de la pausa.

// MotivosPausa son los motivos de ordenes.pausar (con otro, la nota es obligatoria).
var MotivosPausa = []string{"esperando_material", "cliente_ausente", "clima", "acceso", "otro"}

// Cierres de una pausa.
const (
	CierreReanudada    = "reanudada"
	CierreCancelada    = "cancelada"
	CierreReprogramada = "reprogramada"
)

// EntradaPausar es la entrada de ordenes.pausar.
type EntradaPausar struct {
	ID      int64   `json:"-"`
	Version int32   `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Motivo  string  `json:"motivo" enum:"esperando_material,cliente_ausente,clima,acceso,otro"`
	Nota    string  `json:"nota,omitempty" maxLength:"2000" doc:"Obligatoria con motivo otro"`
	Sync    *OpSync `json:"-"`
}

// EntradaReanudar es la entrada de ordenes.reanudar.
type EntradaReanudar struct {
	ID      int64   `json:"-"`
	Version int32   `json:"version" minimum:"1" doc:"Versión leída; si cambió, 409"`
	Sync    *OpSync `json:"-"`
}

func (e EntradaPausar) ordenID() int64   { return e.ID }
func (e EntradaReanudar) ordenID() int64 { return e.ID }

// noPausada: una orden pausada no avanza ni termina sin reanudar (422 orden_pausada).
func noPausada(f campobd.CampoOrden, accion string) error {
	if f.Estado != EstadoPausada {
		return nil
	}
	return nucleo.NuevoProblema(http.StatusUnprocessableEntity, "orden_pausada", "Orden pausada",
		fmt.Sprintf("La orden %s está pausada: reanúdala antes de %s", f.Numero, accion))
}

func (m *Modulo) nuevoPausarOrden() *auditoria.Caso[EntradaPausar, Orden] {
	return accionOrden("ordenes.pausar", nucleo.PermOrdenesPausar, func(e EntradaPausar) *OpSync { return e.Sync },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaPausar) (campobd.CampoOrden, string, error) {
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesPausar, versionDe(in.Version, in.Sync), in.Sync)
			if err != nil {
				return antes, "", err
			}
			if antes.Estado == EstadoPausada {
				return antes, "", nucleo.Conflicto("orden_ya_pausada", "La orden ya está pausada")
			}
			if err := exigirEstado(antes, "pausar", EstadoEnCamino, EstadoEnSitio); err != nil {
				return antes, "", err
			}
			if !slices.Contains(MotivosPausa, in.Motivo) {
				return antes, "", campoInvalido("motivo", "Motivo de pausa desconocido", "enum")
			}
			nota, err := textoOpcional("nota", &in.Nota, 2000)
			if err != nil {
				return antes, "", err
			}
			if in.Motivo == "otro" && nota == nil {
				return antes, "", campoObligatorio("nota")
			}
			cuando := m.cuandoEn(in.Sync, antes)
			p, err := q.CrearPausa(ctx, campobd.CrearPausaParams{OrdenID: antes.ID, Motivo: in.Motivo, Nota: nota,
				DesdeEstado: antes.Estado, Inicio: cuando, PausadaPor: actorUsuario(t), Canal: string(contexto.CanalDe(ctx)),
				OperacionID: operacionDe(in.Sync)})
			if err != nil {
				return antes, "", err
			}
			t.RegistrarCambios("orden_pausa", p.ID, nil, p)
			despues := antes
			desde := antes.Estado
			despues.Estado, despues.PausadaDesde, despues.PausadaEn = EstadoPausada, &desde, &cuando
			f, err := guardar(ctx, t, q, antes, despues)
			if err != nil {
				return f, "", err
			}
			motivo := in.Motivo
			if nota != nil {
				motivo += ": " + *nota
			}
			if err := cambioDeEstado(ctx, t, q, antes, f, &motivo, map[string]any{"pausa_id": p.ID, "motivo": in.Motivo, "nota": nota}, cuando); err != nil {
				return f, "", err
			}
			return f, "", t.Emitir("orden.pausada", map[string]any{
				"orden_id": f.ID, "numero": f.Numero, "pausa_id": p.ID, "motivo": in.Motivo, "nota": nota, "desde": desde,
				"inicio": cuando, "tecnico_id": f.TecnicoID,
			})
		})
}

func (m *Modulo) nuevoReanudarOrden() *auditoria.Caso[EntradaReanudar, Orden] {
	return accionOrden("ordenes.reanudar", nucleo.PermOrdenesReanudar, func(e EntradaReanudar) *OpSync { return e.Sync },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaReanudar) (campobd.CampoOrden, string, error) {
			antes, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesReanudar, versionDe(in.Version, in.Sync), in.Sync)
			if err != nil {
				return antes, "", err
			}
			if antes.Estado != EstadoPausada {
				return antes, "", nucleo.Conflicto("orden_no_pausada", fmt.Sprintf("La orden no está pausada (está %s)", antes.Estado))
			}
			cuando := m.cuandoEn(in.Sync, antes)
			despues := antes
			p, err := cerrarPausa(ctx, t, q, &despues, cuando, CierreReanudada, in.Sync)
			if err != nil {
				return antes, "", err
			}
			f, err := guardar(ctx, t, q, antes, despues)
			if err != nil {
				return f, "", err
			}
			duracion := int64(p.Fin.Sub(p.Inicio) / time.Second)
			if err := cambioDeEstado(ctx, t, q, antes, f, ptr("reanudada"), map[string]any{"pausa_id": p.ID, "duracion_seg": duracion}, cuando); err != nil {
				return f, "", err
			}
			return f, "", t.Emitir("orden.reanudada", map[string]any{
				"orden_id": f.ID, "numero": f.Numero, "pausa_id": p.ID, "a": f.Estado, "duracion_seg": duracion,
				"tiempo_pausado_seg": f.PausadaSeg, "tecnico_id": f.TecnicoID,
			})
		})
}

// cerrarPausa termina la pausa en curso de la orden (reanudar, cancelar o reprogramar): la orden
// vuelve al estado de antes (el que la acción cambie después) y suma la duración al total.
func cerrarPausa(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, f *campobd.CampoOrden, cuando time.Time, cierre string, s *OpSync) (campobd.CampoOrdenPausa, error) {
	abierta, err := q.PausaAbierta(ctx, f.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return abierta, fmt.Errorf("campo: la orden %d está pausada sin pausa abierta", f.ID)
	}
	if err != nil {
		return abierta, err
	}
	if cuando.Before(abierta.Inicio) {
		cuando = abierta.Inicio
	}
	canal := string(contexto.CanalDe(ctx))
	p, err := q.CerrarPausa(ctx, campobd.CerrarPausaParams{ID: abierta.ID, Fin: &cuando, Cierre: &cierre,
		CerradaPor: actorUsuario(t), CanalCierre: &canal, OperacionCierre: operacionDe(s)})
	if err != nil {
		return p, err
	}
	t.RegistrarCambios("orden_pausa", p.ID, abierta, p)
	if f.PausadaDesde != nil {
		f.Estado = *f.PausadaDesde
	}
	f.PausadaDesde, f.PausadaEn = nil, nil
	f.PausadaSeg += int64(cuando.Sub(p.Inicio) / time.Second)
	return p, nil
}
