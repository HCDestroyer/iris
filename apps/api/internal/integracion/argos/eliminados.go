package argos

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
)

// Eliminaciones en Argos (contrato 0.3.1, §2.3): servicio.eliminado y router.eliminado. Argos
// solo elimina lo que nunca se usó (con historial, el servicio se retira y el router se
// desactiva). Iris:
//
//   - confirma en Argos: GET /servicios/:id o GET /routers/:id debe responder 404 (esperado, no se
//     reintenta). Si Argos aún lo devuelve, el webhook queda «ignorado» (no_confirmado);
//   - descarta lo que no es del alcance del conector (empresas[] y alcance_global del canje);
//   - escribe la lápida (integracion.argos_eliminados) y quita la instantánea en la misma
//     transacción. Con la lápida, ni un webhook atrasado de ese servicio ni una lectura vieja
//     (ficha, catálogos) la recrean;
//   - del servicio, refresca la ficha del cliente si está en la instantánea y emite
//     argos.servicio_eliminado; del router, emite argos.router_eliminado (factibilidad ya no lo
//     ve; una factibilidad evaluada conserva el id).

// Webhooks de eliminación (0.3.1).
const (
	EventoServicioEliminado = "servicio.eliminado"
	EventoRouterEliminado   = "router.eliminado"
)

// Eventos internos (outbox) de las eliminaciones.
const (
	EventoArgosServicioEliminado = "argos.servicio_eliminado"
	EventoArgosRouterEliminado   = "argos.router_eliminado"
)

// Tipos de lápida.
const (
	eliminadoServicio = "servicio"
	eliminadoRouter   = "router"
)

// Motivos de un webhook que queda «ignorado» sin efecto (van a la auditoría).
const (
	omitidoSinID             = "sin_id"
	omitidoFueraDeAlcance    = "fuera_de_alcance"
	omitidoNoConfirmado      = "no_confirmado"
	omitidoServicioEliminado = "servicio_eliminado"
	omitidoCamposSinEfecto   = "campos_sin_efecto"
)

// eliminacion es lo confirmado en Argos (404) para aplicar en la transacción.
type eliminacion struct {
	Tipo      string `json:"tipo"`
	ID        int64  `json:"id"`
	EmpresaID *int64 `json:"empresa_id"`
	ClienteID *int64 `json:"cliente_id,omitempty"`
	RouterID  *int64 `json:"router_id,omitempty"`
}

// enAlcance: la empresa del evento está en el alcance del usuario técnico «Iris» que informó el
// canje (§1.3). Sin empresa (router compartido), con alcance global o si Argos no informó su
// alcance (alcance_global ausente: versiones anteriores), cualquiera. Con alcance_global = false,
// solo las empresas[] del canje (vacía = ninguna).
func enAlcance(c conector.Leido, empresa *int64) bool {
	if empresa == nil || *empresa <= 0 || c.AlcanceGlobal == nil || *c.AlcanceGlobal {
		return true
	}
	return slices.Contains(c.Empresas, *empresa)
}

// leerEliminacion confirma en Argos (fuera de transacción) un servicio.eliminado o
// router.eliminado. Devuelve la eliminación a aplicar, o el motivo por el que se ignora; un
// error (Argos caído, conector apagado) hace que el job se reintente.
func (p *ProcesarEntrante) leerEliminacion(ctx context.Context, q conector.DBTX, e conector.Entrante, datos payloadEntrante) (*eliminacion, string, error) {
	el := eliminacion{Tipo: eliminadoServicio, EmpresaID: datos.EmpresaID}
	var id *int64
	if e.Evento == EventoRouterEliminado {
		el.Tipo, id = eliminadoRouter, datos.router()
	} else {
		id, el.ClienteID, el.RouterID = datos.servicio(), datos.cliente(), datos.RouterID
	}
	if id == nil || *id <= 0 {
		return nil, omitidoSinID, nil
	}
	el.ID = *id
	c, err := p.m.conexion.PorID(ctx, q, e.ConectorID)
	if err != nil {
		return nil, "", err
	}
	if !enAlcance(c, el.EmpresaID) {
		return nil, omitidoFueraDeAlcance, nil
	}
	cli := p.m.clienteDe(c)
	if el.Tipo == eliminadoRouter {
		_, err = cli.ObtenerRouter(ctx, el.ID)
	} else {
		_, err = cli.ObtenerServicio(ctx, el.ID)
	}
	switch {
	case err == nil:
		// Argos todavía lo tiene: no se borra nada (el payload no se cree).
		return nil, omitidoNoConfirmado, nil
	case EsNoEncontrado(err):
		return &el, "", nil
	}
	return nil, "", err
}

// aplicarEliminacion escribe la lápida y quita la instantánea (en la transacción del webhook).
// Copia de la instantánea lo que se muestra después («eliminado en Argos»). El evento interno
// sale una sola vez: un segundo aviso (otro webhook_id) encuentra la lápida y no emite.
func aplicarEliminacion(ctx context.Context, t *auditoria.Tx, q *argosbd.Queries, e conector.Entrante, el eliminacion) error {
	en := e.RecibidoEn
	if e.OcurridoEn != nil {
		en = *e.OcurridoEn
	}
	r := argosbd.RegistrarEliminadoParams{Tipo: el.Tipo, ID: el.ID, EmpresaID: el.EmpresaID, EntranteID: &e.ID, EliminadoEn: en}
	var evento string
	var payload map[string]any
	switch el.Tipo {
	case eliminadoServicio:
		r.ClienteID = el.ClienteID
		s, err := q.ServicioPorID(ctx, el.ID)
		switch {
		case err == nil:
			r.Codigo, r.Nombre = s.Codigo, s.PlanNombre
			if r.ClienteID == nil {
				r.ClienteID = &s.ClienteID
			}
			if r.EmpresaID == nil && s.EmpresaID > 0 {
				r.EmpresaID = &s.EmpresaID
			}
			if el.RouterID == nil {
				el.RouterID = s.RouterID
			}
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		if err := q.BorrarServicio(ctx, el.ID); err != nil {
			return err
		}
		evento, payload = EventoArgosServicioEliminado, map[string]any{
			"servicio_id": el.ID, "cliente_id": r.ClienteID, "empresa_id": r.EmpresaID, "router_id": el.RouterID, "entrante_id": e.ID,
		}
	case eliminadoRouter:
		f, err := q.RouterPorID(ctx, el.ID)
		switch {
		case err == nil:
			r.Nombre = f.Nombre
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		if err := q.BorrarRouter(ctx, el.ID); err != nil {
			return err
		}
		evento, payload = EventoArgosRouterEliminado, map[string]any{"router_id": el.ID, "empresa_id": r.EmpresaID, "entrante_id": e.ID}
	}
	if r.EmpresaID != nil {
		t.FijarEmpresa(*r.EmpresaID)
	}
	n, err := q.RegistrarEliminado(ctx, r)
	if err != nil {
		return err
	}
	t.Anotar(map[string]any{"eliminado": el.Tipo, "id": el.ID})
	if n == 0 {
		t.Anotar(map[string]any{"ya_eliminado": true})
		return nil
	}
	return t.Emitir(evento, payload)
}

// servicioEliminado indica si Argos eliminó el servicio (hay lápida).
func servicioEliminado(ctx context.Context, q *argosbd.Queries, id int64) (bool, error) {
	_, err := q.Eliminado(ctx, argosbd.EliminadoParams{Tipo: eliminadoServicio, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// ServiciosEliminados devuelve, de ids, los servicios que Argos eliminó (para que campo y soporte
// muestren «eliminado en Argos» en órdenes y tickets). Con empresa ≠ 0, solo los de esa empresa.
func ServiciosEliminados(ctx context.Context, q argosbd.DBTX, ids []int64, empresa int64) ([]ServicioEliminado, error) {
	if len(ids) == 0 {
		return []ServicioEliminado{}, nil
	}
	filas, err := argosbd.New(q).ServiciosEliminados(ctx, argosbd.ServiciosEliminadosParams{Ids: ids, EmpresaID: empresa})
	if err != nil {
		return nil, err
	}
	return aEliminados(filas), nil
}

func aEliminados(filas []argosbd.IntegracionArgosEliminado) []ServicioEliminado {
	salida := make([]ServicioEliminado, 0, len(filas))
	for _, f := range filas {
		salida = append(salida, ServicioEliminado{
			ID: f.ID, EmpresaID: f.EmpresaID, ClienteID: f.ClienteID, Codigo: f.Codigo, PlanNombre: f.Nombre, EliminadoEn: f.EliminadoEn,
		})
	}
	return salida
}

// camposSinEfecto: campos de cliente.actualizado (§2.3, 0.3.1) que la instantánea no guarda. Si
// todos los que cambiaron son de estos, no se relee el cliente. Cualquier otro (nombre, codigo,
// telefono, direccion, coordenadas, zona_id o uno desconocido) refresca la instantánea.
var camposSinEfecto = map[string]bool{"tipo": true, "nit_cui": true, "tipo_documento_fiscal": true, "movil": true}

func soloCamposSinEfecto(campos []string) bool {
	if len(campos) == 0 {
		return false
	}
	for _, c := range campos {
		if !camposSinEfecto[c] {
			return false
		}
	}
	return true
}
