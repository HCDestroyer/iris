package argos

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Eliminaciones en Argos (contrato 0.3.1, §2.3): servicio.eliminado y router.eliminado. Argos
// solo elimina lo que nunca se usó (con historial, el servicio se retira y el router se
// desactiva). La lápida es difícil de deshacer, así que Iris no se fía del payload:
//
//   - el id sale solo de servicio_id / router_id (sin ellos, «ignorado», sin_id);
//   - si la instantánea tiene la fila, su empresa y su cliente mandan: un payload que no coincide
//     se ignora (empresa_no_coincide, cliente_no_coincide);
//   - confirma en Argos: GET /servicios/:id o GET /routers/:id debe responder un 404 problem+json
//     «no encontrado» (conector.EsNoEncontradoConfirmado; no se reintenta). Cualquier otro 404
//     (HTML de un proxy, ruta inexistente) es un error y el job se reintenta. Si Argos aún lo
//     devuelve, «ignorado» (no_confirmado). No se filtra por el alcance que informó el canje (es
//     una foto: Argos pudo ampliarlo después); Argos solo responde dentro del alcance real del
//     token y la instantánea ya descarta lo que conoce de otra empresa;
//   - en una transacción, con el candado (tipo, id) que también toma todo guardado de la
//     instantánea: lápida en integracion.argos_eliminados (conector, hora de Argos y hora de
//     Iris de la confirmación), fuera de la instantánea y evento interno;
//   - un webhook atrasado del servicio no relee Argos; una lectura directa empezada antes de la
//     confirmación no lo recrea; una empezada después que lo devuelve con 200 lo resucita (borra
//     la lápida, con anotación en la auditoría y log de advertencia).

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
	omitidoNoConfirmado      = "no_confirmado"
	omitidoEmpresaNoCoincide = "empresa_no_coincide"
	omitidoClienteNoCoincide = "cliente_no_coincide"
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
	// confirmadoEn: hora de Iris antes de pedir el 404.
	confirmadoEn time.Time
}

// idEliminado: el id del evento, solo de servicio_id / servicio.id o router_id / router.id
// (nunca el id genérico del payload).
func idEliminado(evento string, d payloadEntrante) *int64 {
	if evento == EventoRouterEliminado {
		if d.RouterID != nil {
			return d.RouterID
		}
		if d.Router != nil {
			return d.Router.ID
		}
		return nil
	}
	if d.ServicioID != nil {
		return d.ServicioID
	}
	if d.Servicio != nil {
		return d.Servicio.ID
	}
	return nil
}

// instantaneaDe completa la eliminación con la fila de la instantánea, si la hay (su empresa y su
// cliente mandan sobre el payload), y devuelve la lápida a escribir o el motivo si el payload la
// contradice.
func instantaneaDe(ctx context.Context, q *argosbd.Queries, el *eliminacion) (copia argosbd.RegistrarEliminadoParams, motivo string, err error) {
	copia = argosbd.RegistrarEliminadoParams{Tipo: el.Tipo, ID: el.ID, EmpresaID: el.EmpresaID, ClienteID: el.ClienteID}
	distinto := func(a, b *int64) bool { return a != nil && (b == nil || *a != *b) }
	switch el.Tipo {
	case eliminadoServicio:
		s, err := q.ServicioPorID(ctx, el.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return copia, "", nil
		}
		if err != nil {
			return copia, "", err
		}
		var empresa *int64
		if s.EmpresaID > 0 {
			empresa = &s.EmpresaID
		}
		switch {
		case el.EmpresaID != nil && (empresa == nil || *el.EmpresaID != *empresa):
			return copia, omitidoEmpresaNoCoincide, nil
		case distinto(el.ClienteID, &s.ClienteID):
			return copia, omitidoClienteNoCoincide, nil
		}
		copia.EmpresaID, copia.ClienteID = empresa, &s.ClienteID
		copia.Codigo, copia.Nombre = s.Codigo, s.PlanNombre
		el.EmpresaID, el.ClienteID = empresa, &s.ClienteID
		if s.RouterID != nil {
			el.RouterID = s.RouterID
		}
	case eliminadoRouter:
		r, err := q.RouterPorID(ctx, el.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return copia, "", nil
		}
		if err != nil {
			return copia, "", err
		}
		// null = router compartido: un payload con empresa sobre uno compartido (o al revés) no
		// coincide.
		if (el.EmpresaID == nil) != (r.EmpresaID == nil) || distinto(el.EmpresaID, r.EmpresaID) {
			return copia, omitidoEmpresaNoCoincide, nil
		}
		copia.Nombre = r.Nombre
	}
	return copia, "", nil
}

// leerEliminacion confirma en Argos (fuera de transacción) un servicio.eliminado o
// router.eliminado. Devuelve la eliminación a aplicar, o el motivo por el que se ignora; un
// error (Argos caído, un 404 que no es «no encontrado», conector apagado) hace que el job se
// reintente.
func (p *ProcesarEntrante) leerEliminacion(ctx context.Context, bd nucleo.BD, e conector.Entrante, datos payloadEntrante) (*eliminacion, string, error) {
	el := eliminacion{Tipo: eliminadoServicio, EmpresaID: datos.EmpresaID}
	if e.Evento == EventoRouterEliminado {
		el.Tipo = eliminadoRouter
	} else {
		el.ClienteID, el.RouterID = datos.cliente(), datos.RouterID
	}
	id := idEliminado(e.Evento, datos)
	if id == nil || *id <= 0 {
		return nil, omitidoSinID, nil
	}
	el.ID = *id
	// Primera mirada a la instantánea (se repite en la transacción, con el candado).
	if _, motivo, err := instantaneaDe(ctx, argosbd.New(bd), &el); err != nil || motivo != "" {
		return nil, motivo, err
	}
	c, err := p.m.conexion.PorID(ctx, bd, e.ConectorID)
	if err != nil {
		return nil, "", err
	}
	cli := p.m.clienteDe(c)
	el.confirmadoEn = time.Now().UTC()
	if el.Tipo == eliminadoRouter {
		_, err = cli.ObtenerRouter(ctx, el.ID)
	} else {
		_, err = cli.ObtenerServicio(ctx, el.ID)
	}
	switch {
	case err == nil:
		// Argos todavía lo tiene: no se borra nada (el payload no se cree).
		return nil, omitidoNoConfirmado, nil
	case conector.EsNoEncontradoConfirmado(err):
		return &el, "", nil
	}
	return nil, "", err
}

// aplicarEliminacion escribe la lápida y quita la instantánea (en la transacción del webhook,
// con el candado del servicio o router). Copia de la instantánea lo que se muestra después
// («eliminado en Argos»). Devuelve el motivo si al final no se aplica (la instantánea cambió y ya
// no coincide). El evento interno sale una sola vez.
func aplicarEliminacion(ctx context.Context, t *auditoria.Tx, q *argosbd.Queries, e conector.Entrante, el eliminacion) (string, error) {
	if err := q.BloquearArgos(ctx, argosbd.BloquearArgosParams{Tipo: el.Tipo, ID: el.ID}); err != nil {
		return "", err
	}
	r, motivo, err := instantaneaDe(ctx, q, &el)
	if err != nil || motivo != "" {
		return motivo, err
	}
	en := e.RecibidoEn
	if e.OcurridoEn != nil {
		en = *e.OcurridoEn
	}
	r.ConectorID, r.EntranteID, r.EliminadoEn, r.ConfirmadoEn = e.ConectorID, &e.ID, en, el.confirmadoEn
	var evento string
	var payload map[string]any
	switch el.Tipo {
	case eliminadoServicio:
		if err := q.BorrarServicio(ctx, el.ID); err != nil {
			return "", err
		}
		evento, payload = EventoArgosServicioEliminado, map[string]any{
			"servicio_id": el.ID, "cliente_id": r.ClienteID, "empresa_id": r.EmpresaID, "router_id": el.RouterID, "entrante_id": e.ID,
		}
	case eliminadoRouter:
		if err := q.BorrarRouter(ctx, el.ID); err != nil {
			return "", err
		}
		evento, payload = EventoArgosRouterEliminado, map[string]any{"router_id": el.ID, "empresa_id": r.EmpresaID, "entrante_id": e.ID}
	}
	if r.EmpresaID != nil {
		t.FijarEmpresa(*r.EmpresaID)
	}
	n, err := q.RegistrarEliminado(ctx, r)
	if err != nil {
		return "", err
	}
	t.Anotar(map[string]any{"eliminado": el.Tipo, "id": el.ID})
	if n == 0 {
		t.Anotar(map[string]any{"ya_eliminado": true})
		return "", nil
	}
	return "", t.Emitir(evento, payload)
}

// Guardado de la instantánea con lápidas ---------------------------------------------------

// guardarServicio guarda un servicio leído de Argos (SincronizadoEn = inicio de la lectura) con
// el candado del servicio. Una lápida confirmada antes de la lectura se borra (resucita); una
// confirmada después gana y el servicio no se guarda.
func guardarServicio(ctx context.Context, t *auditoria.Tx, q *argosbd.Queries, p argosbd.GuardarServicioParams) error {
	if err := q.BloquearArgos(ctx, argosbd.BloquearArgosParams{Tipo: eliminadoServicio, ID: p.ID}); err != nil {
		return err
	}
	if err := resucitar(ctx, t, q, eliminadoServicio, p.ID, p.SincronizadoEn); err != nil {
		return err
	}
	return q.GuardarServicio(ctx, p)
}

// guardarRouter: como guardarServicio, para la carga de catálogos.
func guardarRouter(ctx context.Context, t *auditoria.Tx, q *argosbd.Queries, p argosbd.GuardarRouterParams) error {
	if err := q.BloquearArgos(ctx, argosbd.BloquearArgosParams{Tipo: eliminadoRouter, ID: p.ID}); err != nil {
		return err
	}
	if err := resucitar(ctx, t, q, eliminadoRouter, p.ID, p.SincronizadoEn); err != nil {
		return err
	}
	return q.GuardarRouter(ctx, p)
}

func resucitar(ctx context.Context, t *auditoria.Tx, q *argosbd.Queries, tipo string, id int64, leidoEn time.Time) error {
	f, err := q.ResucitarEliminado(ctx, argosbd.ResucitarEliminadoParams{Tipo: tipo, ID: id, LeidoEn: leidoEn})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	slog.WarnContext(ctx, "argos: Argos devolvió con 200 algo que había confirmado como eliminado; se restaura la instantánea",
		"tipo", tipo, "id", id, "confirmado_en", f.ConfirmadoEn, "entrante", f.EntranteID)
	t.Anotar(map[string]any{fmt.Sprintf("resucitado_%s_%d", tipo, id): map[string]any{
		"confirmado_en": f.ConfirmadoEn, "eliminado_en": f.EliminadoEn, "entrante_id": f.EntranteID,
	}})
	return nil
}

// servicioEliminado indica si Argos eliminó el servicio (hay lápida).
func servicioEliminado(ctx context.Context, q *argosbd.Queries, id int64) (bool, error) {
	_, err := q.Eliminado(ctx, argosbd.EliminadoParams{Tipo: eliminadoServicio, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// ServiciosEliminados devuelve, de ids, los servicios de la empresa que Argos eliminó (para que
// campo y soporte muestren «eliminado en Argos» en órdenes y tickets). La empresa es obligatoria
// (> 0); sin filtro, ServiciosEliminadosTodasLasEmpresas.
func ServiciosEliminados(ctx context.Context, q argosbd.DBTX, ids []int64, empresa int64) ([]ServicioEliminado, error) {
	if empresa <= 0 {
		return nil, errors.New("argos.ServiciosEliminados: empresa obligatoria (usa ServiciosEliminadosTodasLasEmpresas para uso interno)")
	}
	if len(ids) == 0 {
		return []ServicioEliminado{}, nil
	}
	filas, err := argosbd.New(q).ServiciosEliminados(ctx, argosbd.ServiciosEliminadosParams{Ids: ids, EmpresaID: empresa})
	if err != nil {
		return nil, err
	}
	return aEliminados(filas), nil
}

// ServiciosEliminadosTodasLasEmpresas es ServiciosEliminados sin filtro de empresa: solo para
// uso interno del sistema (jobs), nunca para responder a un usuario.
func ServiciosEliminadosTodasLasEmpresas(ctx context.Context, q argosbd.DBTX, ids []int64) ([]ServicioEliminado, error) {
	if len(ids) == 0 {
		return []ServicioEliminado{}, nil
	}
	filas, err := argosbd.New(q).ServiciosEliminadosTodasLasEmpresas(ctx, ids)
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
