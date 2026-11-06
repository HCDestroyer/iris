package campo

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Notas editables (ordenes.editar_nota y ordenes.eliminar_nota, permiso ordenes.registrar). Como
// agregar una nota, en cualquier estado de la orden (también pausada o cancelada: evidencia). Solo
// su autor, o despacho y supervisión con ordenes.asignar que cubra la orden. Cada nota lleva su
// version (409 conflicto_version si cambió). Eliminar es lógico: la nota sale de la orden y queda
// en la auditoría (con su diff). Cada una sube la versión de la orden (el feed avisa al teléfono).
// No emiten evento: las notas no tienen consumidores fuera de Iris.

// EntradaEditarNota es la entrada de ordenes.editar_nota (panel: POST /ordenes/{id}/notas/{nota}/editar).
type EntradaEditarNota struct {
	ID      int64     `json:"-"`
	NotaID  uuid.UUID `json:"-"`
	Version int32     `json:"version" minimum:"1" doc:"Versión leída de la nota (notas[].version); si cambió, 409 conflicto_version"`
	Texto   string    `json:"texto" minLength:"1" maxLength:"4000"`
	Sync    *OpSync   `json:"-"`
}

// EntradaEliminarNota es la entrada de ordenes.eliminar_nota (panel: POST /ordenes/{id}/notas/{nota}/eliminar).
type EntradaEliminarNota struct {
	ID      int64     `json:"-"`
	NotaID  uuid.UUID `json:"-"`
	Version int32     `json:"version" minimum:"1" doc:"Versión leída de la nota (notas[].version); si cambió, 409 conflicto_version"`
	Sync    *OpSync   `json:"-"`
}

// EntradaEditarNotaCampo son los datos de nota.editar (lote).
type EntradaEditarNotaCampo struct {
	NotaID      uuid.UUID `json:"notaId" doc:"Nota de la orden (notas[].id; la de un nota.agregar aún sin enviar es el id de esa operación)"`
	VersionNota int32     `json:"versionNota" minimum:"1" doc:"Versión de la nota que vio el teléfono; si cambió, rechazada con conflicto_version"`
	Texto       string    `json:"texto" minLength:"1" maxLength:"4000"`
}

// EntradaEliminarNotaCampo son los datos de nota.eliminar (lote).
type EntradaEliminarNotaCampo struct {
	NotaID      uuid.UUID `json:"notaId" doc:"Nota de la orden (notas[].id)"`
	VersionNota int32     `json:"versionNota" minimum:"1" doc:"Versión de la nota que vio el teléfono; si cambió, rechazada con conflicto_version"`
}

func (e EntradaEditarNota) ordenID() int64   { return e.ID }
func (e EntradaEliminarNota) ordenID() int64 { return e.ID }

func notaNoEncontrada() *nucleo.Problema {
	return nucleo.NuevoProblema(http.StatusNotFound, "nota_no_encontrada", "No encontrada", "La nota no existe en esta orden")
}

// tomarNota lee la nota con FOR UPDATE y verifica quién la cambia y su versión. Una ya
// eliminada: 409 nota_eliminada (eliminar de nuevo la responde sin cambios, ver abajo).
func tomarNota(ctx context.Context, q *campobd.Queries, f campobd.CampoOrden, id uuid.UUID, version int32) (campobd.CampoOrdenNota, error) {
	n, err := q.NotaParaActualizar(ctx, campobd.NotaParaActualizarParams{ID: id, OrdenID: f.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return n, notaNoEncontrada()
	}
	if err != nil {
		return n, err
	}
	if err := puedeCambiarNota(ctx, f, n); err != nil {
		return n, err
	}
	if n.EliminadaEn != nil {
		return n, nucleo.Conflicto("nota_eliminada", "La nota ya se eliminó")
	}
	if err := nucleo.ExigirVersion(version, n.Version, "nota", n.ID); err != nil {
		return n, err
	}
	return n, nil
}

// puedeCambiarNota: su autor, o quien asigna la orden (despacho o supervisión con
// ordenes.asignar con alcance sobre ella). 403 nota_ajena.
func puedeCambiarNota(ctx context.Context, f campobd.CampoOrden, n campobd.CampoOrdenNota) error {
	a := contexto.ActorDe(ctx)
	if a.EsUsuario() && n.AutorID != nil && *n.AutorID == a.ID {
		return nil
	}
	if cubre(ctx, nucleo.PermOrdenesAsignar, f) {
		return nil
	}
	return nucleo.NuevoProblema(http.StatusForbidden, "nota_ajena", "Nota de otra persona",
		"Solo quien escribió la nota (o despacho y supervisión con ordenes.asignar) la edita o elimina")
}

func (m *Modulo) nuevoEditarNota() *auditoria.Caso[EntradaEditarNota, Orden] {
	return accionOrden("ordenes.editar_nota", nucleo.PermOrdenesRegistrar, func(e EntradaEditarNota) *OpSync { return e.Sync },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaEditarNota) (campobd.CampoOrden, string, error) {
			f, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesRegistrar, nil, in.Sync)
			if err != nil {
				return f, "", err
			}
			cuerpo, err := texto("texto", in.Texto, 1, 4000)
			if err != nil {
				return f, "", err
			}
			n, err := tomarNota(ctx, q, f, in.NotaID, in.Version)
			if err != nil {
				return f, "", err
			}
			if cuerpo == n.Texto {
				return f, "", nil // sin cambios: ni versión nueva ni diff
			}
			cuando := m.cuandoEn(in.Sync, f)
			editada, err := q.EditarNota(ctx, campobd.EditarNotaParams{ID: n.ID, Texto: cuerpo, EditadaEn: &cuando, EditadaPor: actorUsuario(t)})
			if err != nil {
				return f, "", err
			}
			t.RegistrarCambios("orden_nota", n.ID, n, editada)
			f, err = tocar(ctx, t, q, f)
			return f, "", err
		})
}

func (m *Modulo) nuevoEliminarNota() *auditoria.Caso[EntradaEliminarNota, Orden] {
	return accionOrden("ordenes.eliminar_nota", nucleo.PermOrdenesRegistrar, func(e EntradaEliminarNota) *OpSync { return e.Sync },
		func(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, in EntradaEliminarNota) (campobd.CampoOrden, string, error) {
			f, err := tomar(ctx, t, q, in.ID, nucleo.PermOrdenesRegistrar, nil, in.Sync)
			if err != nil {
				return f, "", err
			}
			n, err := tomarNota(ctx, q, f, in.NotaID, in.Version)
			var p *nucleo.Problema
			if errors.As(err, &p) && p.Codigo() == "nota_eliminada" {
				return f, "", nil // ya eliminada (otro teléfono o el panel): el resultado es el mismo
			}
			if err != nil {
				return f, "", err
			}
			cuando := m.cuandoEn(in.Sync, f)
			eliminada, err := q.EliminarNota(ctx, campobd.EliminarNotaParams{ID: n.ID, EliminadaEn: &cuando, EliminadaPor: actorUsuario(t)})
			if err != nil {
				return f, "", err
			}
			t.RegistrarCambios("orden_nota", n.ID, n, eliminada)
			f, err = tocar(ctx, t, q, f)
			return f, "", err
		})
}
