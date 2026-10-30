package argos

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
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
//   - si la instantánea tiene la fila, su empresa y su cliente mandan; si el payload no coincide
//     se anota la discrepancia (empresa_no_coincide, cliente_no_coincide) y se aplica igual con
//     los datos de la instantánea (la lápida queda de_instantanea);
//   - confirma en Argos: GET /servicios/:id o GET /routers/:id debe responder un 404 problem+json
//     urn:argosisp:problema:no_encontrado (conector.EsNoEncontradoConfirmado; no se reintenta).
//     Cualquier otro 404 (HTML de un proxy, ruta inexistente) o el 403 empresa_no_permitida (el
//     recurso existe fuera del alcance) es un error y el job se reintenta. Si Argos aún lo
//     devuelve, «ignorado» (no_confirmado). No se filtra por el alcance que informó el canje (es
//     una foto: Argos pudo ampliarlo después);
//   - en una transacción, con los candados (tipo, id) en orden de id: lápida en
//     integracion.argos_eliminados (conector, hora de Argos y hora de Postgres de antes del 404),
//     fuera de la instantánea y evento interno;
//   - un webhook atrasado del servicio no relee Argos y una lista (ficha, catálogos) nunca recrea
//     un id con lápida: solo un GET por id con 200 pedido después de la confirmación (relojes de
//     Postgres) lo resucita: borra la lápida, anota en la auditoría, deja un log de advertencia y
//     emite argos.servicio_restaurado / argos.router_restaurado.

// Webhooks de eliminación (0.3.1).
const (
	EventoServicioEliminado = "servicio.eliminado"
	EventoRouterEliminado   = "router.eliminado"
)

// Eventos internos (outbox) de las eliminaciones.
const (
	EventoArgosServicioEliminado = "argos.servicio_eliminado"
	EventoArgosRouterEliminado   = "argos.router_eliminado"
	// Restaurados: un GET por id posterior a la confirmación respondió 200 (lápida equivocada o
	// Argos lo restauró).
	EventoArgosServicioRestaurado = "argos.servicio_restaurado"
	EventoArgosRouterRestaurado   = "argos.router_restaurado"
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
	omitidoServicioEliminado = "servicio_eliminado"
)

// Discrepancias entre el payload de una eliminación y la instantánea (se anotan; manda la
// instantánea).
const (
	discrepanciaEmpresa = "empresa_no_coincide"
	discrepanciaCliente = "cliente_no_coincide"
)

// eliminacion es lo confirmado en Argos (404) para aplicar en la transacción.
type eliminacion struct {
	Tipo      string `json:"tipo"`
	ID        int64  `json:"id"`
	EmpresaID *int64 `json:"empresa_id"`
	ClienteID *int64 `json:"cliente_id,omitempty"`
	RouterID  *int64 `json:"router_id,omitempty"`
	// confirmadoEn: hora de Postgres antes de pedir el 404.
	confirmadoEn time.Time
	// clienteFicha: el cliente cuya ficha se relee (el de la instantánea si la había; si no, el del
	// payload). El payload queda intacto en ClienteID/EmpresaID para anotar discrepancias.
	clienteFicha *int64
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

// instantaneaDe completa la eliminación con la fila de la instantánea, si la hay: su empresa y su
// cliente mandan sobre el payload (también en el evento interno) y la lápida queda
// de_instantanea. Devuelve la lápida a escribir y, si el payload la contradice, la discrepancia
// (se anota en la auditoría; la eliminación se aplica igual con los datos de la instantánea).
func instantaneaDe(ctx context.Context, q *argosbd.Queries, el *eliminacion) (copia argosbd.RegistrarEliminadoParams, discrepancia string, err error) {
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
			discrepancia = discrepanciaEmpresa
		case distinto(el.ClienteID, &s.ClienteID):
			discrepancia = discrepanciaCliente
		}
		copia.EmpresaID, copia.ClienteID = empresa, &s.ClienteID
		copia.Codigo, copia.Nombre, copia.DeInstantanea = s.Codigo, s.PlanNombre, true
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
			discrepancia = discrepanciaEmpresa
		}
		copia.EmpresaID, copia.Nombre, copia.DeInstantanea = r.EmpresaID, r.Nombre, true
		el.EmpresaID = r.EmpresaID
	}
	return copia, discrepancia, nil
}

// leerEliminacion confirma en Argos (fuera de transacción) un servicio.eliminado o
// router.eliminado. Devuelve la eliminación a aplicar, o el motivo por el que se ignora; un
// error (Argos caído, un 403 de alcance, un 404 que no es «no encontrado», conector apagado) hace
// que el job se reintente.
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
	q := argosbd.New(bd)
	// Primera mirada a la instantánea (se repite en la transacción, con el candado): da el cliente
	// cuya ficha se relee.
	previa := el
	if _, _, err := instantaneaDe(ctx, q, &previa); err != nil {
		return nil, "", err
	}
	el.clienteFicha = previa.ClienteID
	c, err := p.m.conexion.PorID(ctx, bd, e.ConectorID)
	if err != nil {
		return nil, "", err
	}
	cli := p.m.clienteDe(c)
	if el.confirmadoEn, err = q.RelojBD(ctx); err != nil {
		return nil, "", err
	}
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

// aplicarEliminacion escribe la lápida y quita la instantánea (en la transacción del webhook; el
// candado lo toma antes quien llama, junto con los de la ficha, en orden de id). Copia de la
// instantánea lo que se muestra después («eliminado en Argos»). El evento interno sale una sola
// vez.
func aplicarEliminacion(ctx context.Context, t *auditoria.Tx, q *argosbd.Queries, e conector.Entrante, el eliminacion) error {
	if err := bloquear(ctx, q, el.Tipo, []int64{el.ID}); err != nil {
		return err
	}
	r, discrepancia, err := instantaneaDe(ctx, q, &el)
	if err != nil {
		return err
	}
	if discrepancia != "" {
		// El payload venía desfasado: se aplica con los datos de la instantánea.
		t.Anotar(map[string]any{"discrepancia": discrepancia})
		slog.WarnContext(ctx, "argos: el webhook de eliminación no coincide con la instantánea; se usa la instantánea",
			"tipo", el.Tipo, "id", el.ID, "discrepancia", discrepancia)
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
			return err
		}
		evento, payload = EventoArgosServicioEliminado, map[string]any{
			"servicio_id": el.ID, "cliente_id": r.ClienteID, "empresa_id": r.EmpresaID, "router_id": el.RouterID, "entrante_id": e.ID,
		}
	case eliminadoRouter:
		if err := q.BorrarRouter(ctx, el.ID); err != nil {
			return err
		}
		evento, payload = EventoArgosRouterEliminado, map[string]any{"router_id": el.ID, "empresa_id": r.EmpresaID, "entrante_id": e.ID}
	}
	if r.EmpresaID != nil {
		t.FijarEmpresa(*r.EmpresaID)
	}
	insertado, err := q.RegistrarEliminado(ctx, r)
	if err != nil {
		return err
	}
	t.Anotar(map[string]any{"eliminado": el.Tipo, "id": el.ID})
	if !insertado {
		t.Anotar(map[string]any{"ya_eliminado": true})
		return nil
	}
	return t.Emitir(evento, payload)
}

// Guardado de la instantánea con lápidas ---------------------------------------------------

// bloquear toma los candados (tipo, id) en orden de id, sin repetir. Todo el que guarda o borra
// la instantánea los toma así al principio (los advisory locks son reentrantes en la misma
// sesión), para no interbloquearse.
func bloquear(ctx context.Context, q *argosbd.Queries, tipo string, ids []int64) error {
	orden := slices.Clone(ids)
	slices.Sort(orden)
	for _, id := range slices.Compact(orden) {
		if err := q.BloquearArgos(ctx, argosbd.BloquearArgosParams{Tipo: tipo, ID: id}); err != nil {
			return err
		}
	}
	return nil
}

// tiempoConfirmarVivo: tope del GET por id de confirmarVivos.
const tiempoConfirmarVivo = 5 * time.Second

// confirmarVivos: de los ids que trae una lista de Argos, los que tienen lápida se confirman con
// un GET por id (fuera de transacción). Devuelve la hora de Postgres de antes de cada GET que
// respondió 200 (solo esos resucitan); un 404 o un error los deja fuera (no se guardan).
func confirmarVivos(ctx context.Context, bd argosbd.DBTX, tipo string, ids []int64, obtener func(context.Context, int64) error) (map[int64]time.Time, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	q := argosbd.New(bd)
	conLapida, err := q.EliminadosEntre(ctx, argosbd.EliminadosEntreParams{Tipo: tipo, Ids: ids})
	if err != nil || len(conLapida) == 0 {
		return nil, err
	}
	vivos := map[int64]time.Time{}
	for _, id := range conLapida {
		antes, err := q.RelojBD(ctx)
		if err != nil {
			return nil, err
		}
		// Un solo intento y poco tiempo: es una comprobación extra por id, no la lectura principal.
		corto, cancelar := context.WithTimeout(ctx, tiempoConfirmarVivo)
		err = obtener(corto, id)
		cancelar()
		if err == nil {
			vivos[id] = antes
		} else if !conector.EsNoEncontradoConfirmado(err) {
			slog.WarnContext(ctx, "argos: no se pudo confirmar un id con lápida que trae la lista; queda fuera", "tipo", tipo, "id", id, "error", err)
		}
	}
	return vivos, nil
}

// guardarServicio guarda un servicio leído de Argos con su candado ya tomado. Si tiene lápida,
// solo se guarda cuando un GET por id posterior a la confirmación respondió 200 (vivoEn): la
// lápida se borra y sale argos.servicio_restaurado. Si no, la lápida gana.
func guardarServicio(ctx context.Context, t *auditoria.Tx, q *argosbd.Queries, p argosbd.GuardarServicioParams, vivoEn *time.Time) error {
	if vivoEn != nil {
		var empresa *int64
		if p.EmpresaID > 0 {
			empresa = &p.EmpresaID
		}
		if err := resucitar(ctx, t, q, eliminadoServicio, p.ID, *vivoEn, EventoArgosServicioRestaurado, empresa,
			map[string]any{"servicio_id": p.ID, "cliente_id": p.ClienteID, "empresa_id": empresa}); err != nil {
			return err
		}
	}
	return q.GuardarServicio(ctx, p)
}

// guardarRouter: como guardarServicio, para la carga de catálogos (argos.router_restaurado).
func guardarRouter(ctx context.Context, t *auditoria.Tx, q *argosbd.Queries, p argosbd.GuardarRouterParams, vivoEn *time.Time) error {
	if vivoEn != nil {
		if err := resucitar(ctx, t, q, eliminadoRouter, p.ID, *vivoEn, EventoArgosRouterRestaurado, p.EmpresaID,
			map[string]any{"router_id": p.ID, "empresa_id": p.EmpresaID}); err != nil {
			return err
		}
	}
	return q.GuardarRouter(ctx, p)
}

func resucitar(ctx context.Context, t *auditoria.Tx, q *argosbd.Queries, tipo string, id int64, leidoEn time.Time, evento string, empresa *int64, payload map[string]any) error {
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
	payload["eliminado_entrante_id"] = f.EntranteID
	// El evento lleva la empresa del servicio o router, sin cambiar la de la acción que refresca.
	return t.EmitirEnEmpresa(evento, empresa, payload)
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
