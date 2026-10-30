package argos

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Webhooks Argos → Iris (§2.3): los recibe el núcleo de conectores (conector.RecibirWebhook:
// firma, ventana, deduplicación por el id firmado, integracion.entrantes) y encola
// ArgsProcesarEntrante. Aquí se procesan: se relee de Argos lo necesario y se refresca la
// instantánea o se emite el evento interno.

// payloadEntrante son los datos que Iris lee del payload de un webhook. Supuesto: ids planos
// (cliente_id, servicio_id) o anidados ({ cliente: { id } }, { servicio: { id, cliente_id } }).
type payloadEntrante struct {
	EmpresaID  *int64 `json:"empresa_id"`
	ClienteID  *int64 `json:"cliente_id"`
	ServicioID *int64 `json:"servicio_id"`
	ID         *int64 `json:"id"`
	Cliente    *Ref   `json:"cliente"`
	Servicio   *struct {
		ID        *int64 `json:"id"`
		ClienteID *int64 `json:"cliente_id"`
	} `json:"servicio"`
	De string `json:"de"`
	A  string `json:"a"`
	// RouterID: router.eliminado y servicio.eliminado (0.3.1).
	RouterID *int64 `json:"router_id"`
	Router   *Ref   `json:"router"`
	// Campos: los que cambiaron en cliente.actualizado (0.3.1; solo nombres).
	Campos []string `json:"campos"`
}

func leerPayload(crudo []byte) payloadEntrante {
	var p payloadEntrante
	_ = conector.Decodificar(crudo, &p)
	return p
}

func (p payloadEntrante) cliente() *int64 {
	switch {
	case p.ClienteID != nil:
		return p.ClienteID
	case p.Cliente != nil && p.Cliente.ID != nil:
		return p.Cliente.ID
	case p.Servicio != nil && p.Servicio.ClienteID != nil:
		return p.Servicio.ClienteID
	}
	return nil
}

func (p payloadEntrante) servicio() *int64 {
	switch {
	case p.ServicioID != nil:
		return p.ServicioID
	case p.Servicio != nil && p.Servicio.ID != nil:
		return p.Servicio.ID
	}
	return p.ID
}

// Eventos que refrescan la instantánea (§2.3).
const (
	EventoClienteActualizado     = "cliente.actualizado"
	EventoServicioCreado         = "servicio.creado"
	EventoServicioConfigCambiada = "servicio.config_cambiada"
	EventoAsignacionCambiada     = "asignacion.cambiada"
	EventoServicioEstadoCambiado = "servicio.estado_cambiado"
)

// EventoServicioRetirado es el evento interno (outbox) que emite Iris cuando Argos avisa que un
// servicio pasó a retirado: gancho para que I2 proponga la orden de retiro (Fase 2).
const EventoServicioRetirado = "argos.servicio_retirado"

// Procesar un entrante (job) ---------------------------------------------------------------

// ResultadoEntrante dice qué se hizo con un webhook.
type ResultadoEntrante struct {
	EntranteID int64  `json:"entrante_id"`
	Resultado  string `json:"resultado"` // aplicado | ignorado | ya_procesado
}

type entradaAplicar struct {
	EntranteID int64  `json:"entrante_id"`
	ClienteID  *int64 `json:"cliente_id,omitempty"`
	ficha      *fichaArgos
	borrar     bool
	// retiro es el servicio releído de Argos cuando el webhook dice «a = retirado»: el evento
	// interno solo sale si Argos confirma el estado (el payload no se cree).
	retiro *ServicioArgos
	// eliminado: servicio o router que Argos eliminó (confirmado con un 404); omitido: por qué el
	// webhook se ignora sin leer Argos (va a la auditoría).
	eliminado *eliminacion
	omitido   string
	errArgos  error
}

// ProcesarEntrante aplica un webhook guardado: lee de Argos lo que haga falta (fuera de
// transacción) y en un caso de uso refresca la instantánea, emite los eventos internos y marca
// el entrante procesado. Es idempotente: un entrante procesado no se vuelve a aplicar y el
// refresco es un upsert con la hora de lectura.
//
// Solo se refrescan clientes que ya están en la instantánea (los que Iris mostró alguna vez):
// el resto se marca «ignorado» y se leerá al abrir su ficha. Los eventos que aún no tienen
// efecto en Iris (inventario.equipo_por_recuperar, sesion.terminada…; Fase 2) quedan guardados
// como «ignorado».
type ProcesarEntrante struct {
	m       *Modulo
	aplicar *auditoria.Caso[entradaAplicar, ResultadoEntrante]
}

func nuevoProcesarEntrante(m *Modulo) *ProcesarEntrante {
	p := &ProcesarEntrante{m: m}
	p.aplicar = auditoria.Nuevo(auditoria.Caso[entradaAplicar, ResultadoEntrante]{
		Accion: "integraciones.procesar_webhook", Acceso: auditoria.Autenticado,
		AuditarExito: func(r ResultadoEntrante) bool { return r.Resultado != "ya_procesado" },
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaAplicar) (ResultadoEntrante, error) {
			var causa string
			r, err := p.aplicarEn(ctx, t, in, &causa)
			if err != nil {
				causa = err.Error()
			}
			return r, err
		},
	})
	return p
}

func (p *ProcesarEntrante) aplicarEn(ctx context.Context, t *auditoria.Tx, in entradaAplicar, causa *string) (ResultadoEntrante, error) {
	q := argosbd.New(t)
	r := ResultadoEntrante{EntranteID: in.EntranteID}
	e, procesado, err := conector.TomarEntrante(ctx, t, in.EntranteID, causa)
	if err != nil {
		return r, err
	}
	if procesado {
		r.Resultado = "ya_procesado"
		return r, nil
	}
	if in.errArgos != nil {
		return r, in.errArgos
	}
	r.Resultado = "ignorado"
	if in.omitido != "" {
		t.Anotar(map[string]any{"omitido": in.omitido})
	}
	if in.eliminado != nil {
		// Candados del servicio eliminado y de los de la ficha, juntos y en orden de id (N3).
		ids := []int64{in.eliminado.ID}
		if in.ficha != nil && in.eliminado.Tipo == eliminadoServicio {
			ids = append(ids, in.ficha.idsServicios()...)
		}
		if err := bloquear(ctx, q, in.eliminado.Tipo, ids); err != nil {
			return r, err
		}
		// La lápida va antes que el refresco de la ficha: así la ficha no lo recrea.
		if err := aplicarEliminacion(ctx, t, q, e, *in.eliminado); err != nil {
			return r, err
		}
		r.Resultado = "aplicado"
	}
	switch {
	case in.borrar && in.ClienteID != nil:
		if err := q.BorrarCliente(ctx, *in.ClienteID); err != nil {
			return r, err
		}
		r.Resultado = "aplicado"
	case in.ficha != nil:
		if err := guardarFicha(ctx, t, q, *in.ficha); err != nil {
			return r, err
		}
		r.Resultado = "aplicado"
	}
	if e.Evento == EventoServicioEstadoCambiado && strings.EqualFold(leerPayload(e.Payload).A, "retirado") {
		s := in.retiro
		if s == nil || !strings.EqualFold(strings.TrimSpace(s.Estado), "retirado") {
			// Argos no lo confirma (no existe o tiene otro estado): no se propone el retiro.
			t.Anotar(map[string]any{"retiro": "no_confirmado"})
		} else {
			if s.EmpresaID != 0 {
				t.FijarEmpresa(s.EmpresaID)
			}
			var clienteID *int64
			if s.ClienteID != 0 {
				clienteID = &s.ClienteID
			}
			if err := t.Emitir(EventoServicioRetirado, map[string]any{
				"servicio_id": s.ID, "cliente_id": clienteID, "de": leerPayload(e.Payload).De, "a": "retirado",
				"entrante_id": e.ID,
			}); err != nil {
				return r, err
			}
			r.Resultado = "aplicado"
		}
	}
	return r, conector.MarcarEntrante(ctx, t, e.ID, r.Resultado, nil)
}

// Ejecutar procesa el entrante. ctx necesita la base (nucleo.ConBD) y un actor (el worker usa
// el actor sistema).
func (p *ProcesarEntrante) Ejecutar(ctx context.Context, entranteID int64) (ResultadoEntrante, error) {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return ResultadoEntrante{}, nucleo.ErrSinBD
	}
	q := argosbd.New(bd)
	e, err := conector.LeerEntrante(ctx, bd, entranteID)
	if err != nil {
		return ResultadoEntrante{}, err
	}
	in := entradaAplicar{EntranteID: e.ID}
	if e.ProcesadoEn != nil {
		return ResultadoEntrante{EntranteID: e.ID, Resultado: "ya_procesado"}, nil
	}
	datos := leerPayload(e.Payload)
	switch e.Evento {
	case EventoServicioEliminado, EventoRouterEliminado:
		in.eliminado, in.omitido, in.errArgos = p.leerEliminacion(ctx, bd, e, datos)
		if in.eliminado == nil || in.eliminado.Tipo == eliminadoRouter || in.eliminado.clienteFicha == nil {
			return p.aplicar.Ejecutar(ctx, in)
		}
		// Se relee la ficha del cliente del servicio (el de la instantánea, si la había), si Iris
		// lo tiene.
		if _, err := q.ClientePorID(ctx, *in.eliminado.clienteFicha); errors.Is(err, pgx.ErrNoRows) {
			return p.aplicar.Ejecutar(ctx, in)
		} else if err != nil {
			return ResultadoEntrante{}, err
		}
		in.ClienteID = in.eliminado.clienteFicha
		in.ficha, in.borrar, in.errArgos = p.leerDeArgos(ctx, bd, e.ConectorID, *in.ClienteID)
		return p.aplicar.Ejecutar(ctx, in)
	case EventoServicioCreado, EventoServicioConfigCambiada, EventoAsignacionCambiada, EventoServicioEstadoCambiado:
		// Un evento atrasado de un servicio que Argos eliminó no recrea la instantánea.
		if s := datos.servicio(); s != nil {
			eliminado, err := servicioEliminado(ctx, q, *s)
			if err != nil {
				return ResultadoEntrante{}, err
			}
			if eliminado {
				in.omitido = omitidoServicioEliminado
				return p.aplicar.Ejecutar(ctx, in)
			}
		}
	case EventoClienteActualizado:
		// Solo cambiaron campos que la instantánea no guarda (nit, tipo, móvil…): no se relee.
		if soloCamposSinEfecto(datos.Campos) {
			in.omitido = omitidoCamposSinEfecto
			return p.aplicar.Ejecutar(ctx, in)
		}
	}
	clienteID, err := clienteARefrescar(ctx, q, e)
	if err != nil {
		return ResultadoEntrante{}, err
	}
	if clienteID != nil {
		in.ClienteID = clienteID
		in.ficha, in.borrar, in.errArgos = p.leerDeArgos(ctx, bd, e.ConectorID, *clienteID)
	}
	if in.errArgos == nil && e.Evento == EventoServicioEstadoCambiado && strings.EqualFold(datos.A, "retirado") {
		in.retiro, in.errArgos = p.confirmarRetiro(ctx, bd, e.ConectorID, datos.servicio())
	}
	return p.aplicar.Ejecutar(ctx, in)
}

// confirmarRetiro relee el servicio en Argos (GET /servicios/:id). nil si no hay id o Argos ya
// no lo tiene; un error (Argos caído) hace que el job se reintente.
func (p *ProcesarEntrante) confirmarRetiro(ctx context.Context, q conector.DBTX, conectorID int64, servicioID *int64) (*ServicioArgos, error) {
	if servicioID == nil {
		return nil, nil
	}
	cli, err := p.clienteArgos(ctx, q, conectorID)
	if err != nil {
		return nil, err
	}
	s, err := cli.ObtenerServicio(ctx, *servicioID)
	if EsNoEncontrado(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if s.ID == 0 {
		s.ID = *servicioID
	}
	return &s, nil
}

// clienteArgos arma el cliente HTTP del conector del entrante (debe estar activo).
func (p *ProcesarEntrante) clienteArgos(ctx context.Context, q conector.DBTX, conectorID int64) (*Cliente, error) {
	c, err := p.m.conexion.PorID(ctx, q, conectorID)
	if err != nil {
		return nil, err
	}
	return p.m.clienteDe(c), nil
}

// clienteARefrescar decide qué cliente refrescar: el del evento, si ya está en la instantánea.
func clienteARefrescar(ctx context.Context, q *argosbd.Queries, e conector.Entrante) (*int64, error) {
	datos := leerPayload(e.Payload)
	var clienteID *int64
	switch e.Evento {
	case EventoClienteActualizado:
		clienteID = datos.cliente()
		if clienteID == nil {
			clienteID = datos.ID
		}
	case EventoServicioCreado, EventoServicioConfigCambiada, EventoAsignacionCambiada, EventoServicioEstadoCambiado:
		clienteID = datos.cliente()
		if s := datos.servicio(); clienteID == nil && s != nil {
			srv, err := q.ServicioPorID(ctx, *s)
			if err == nil {
				clienteID = &srv.ClienteID
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
		}
	default:
		return nil, nil
	}
	if clienteID == nil {
		return nil, nil
	}
	if _, err := q.ClientePorID(ctx, *clienteID); errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return clienteID, nil
}

func (p *ProcesarEntrante) leerDeArgos(ctx context.Context, q conector.DBTX, conectorID, clienteID int64) (*fichaArgos, bool, error) {
	cli, err := p.clienteArgos(ctx, q, conectorID)
	if err != nil {
		return nil, false, err
	}
	ficha, err := leerFichaArgos(ctx, q, cli, clienteID)
	if EsNoEncontrado(err) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &ficha, false, nil
}
