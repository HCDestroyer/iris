package argos

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// AsegurarInstantanea deja en la instantánea el cliente (y su servicio) de un ticket u orden que
// se va a crear, para que el caso de uso que lo crea pueda validar contra Argos en su transacción
// (ValidarClienteEnEmpresa, completarDesdeArgos). Se llama ANTES de abrir esa transacción: aquí se
// habla con Argos.
//
//   - Sin conector activo: nada que verificar (Iris trabaja con datos locales).
//   - Con instantánea, se valida primero localmente: si cliente y servicio sirven, no se llama a
//     Argos. Si no sirven (otra empresa, servicio ajeno o desconocido), se relee de Argos solo si
//     la instantánea tiene más de RefrescoMinimo; si no, el 422 único (errNoDisponible).
//   - La lectura pasa por la protección de la ficha: singleflight, cuota por usuario y por conector
//     (agotada: 429) y la espera tras una caída (fallo_en).
//   - Lo leído se valida (cliente de la empresa activa, servicio suyo y de la empresa) ANTES de
//     guardarlo: un cliente de otra empresa no entra a la instantánea. Cualquier incumplimiento, y
//     un cliente que Argos no tiene, da el mismo 422.
//   - Un 404 de Argos borra la instantánea y anonimiza las copias, como la ficha.
//   - Argos caído: con servicioId, 503 argos_no_disponible (nunca se acepta un servicio sin
//     verificar); solo con clienteId, se sigue (no hay servicio que atar).
func (m *Modulo) AsegurarInstantanea(ctx context.Context, clienteID int64, servicioID *int64) error {
	bd := nucleo.BDDe(ctx)
	if bd == nil {
		return nucleo.ErrSinBD
	}
	c, err := m.conectorActivo(ctx, bd)
	var p *nucleo.Problema
	if errors.As(err, &p) && p.Codigo() == "conector_apagado" {
		return nil
	}
	if err != nil {
		return err
	}
	e := contexto.EmpresaDe(ctx)
	q := argosbd.New(bd)
	var previa *argosbd.IntegracionArgosCliente
	if f, err := q.ClientePorID(ctx, clienteID); err == nil {
		previa = &f
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	ahora := time.Now()
	if previa != nil {
		sirve, err := sirveLocal(ctx, q, *previa, servicioID, e)
		if err != nil || sirve {
			return err
		}
		if ahora.Sub(previa.SincronizadoEn) <= RefrescoMinimo {
			return errNoDisponible()
		}
		if enEspera(previa, ahora) {
			if servicioID == nil {
				return nil
			}
			return Problema(errEnEspera())
		}
	}
	ficha, err := m.refrescos.leerUnaVez(ctx, c.ID, clienteID, contexto.ActorDe(ctx).ID, true, func(cx context.Context) (fichaArgos, error) {
		return leerFichaArgos(cx, bd, m.clienteDe(c), clienteID)
	})
	switch {
	case err == nil:
	case errors.Is(err, errCuota):
		return nucleo.DemasiadosIntentos("cuota_argos", "Demasiadas consultas a Argos en el último minuto; intenta en un momento", time.Minute)
	case EsNoEncontrado(err):
		if previa != nil {
			if _, err := m.guardarFichaInterna.Ejecutar(ctx, entradaGuardarFicha{ClienteID: clienteID, borrar: true}); err != nil {
				return err
			}
		}
		return errNoDisponible()
	case EsCaido(err):
		if previa != nil {
			if _, err := m.guardarFichaInterna.Ejecutar(ctx, entradaGuardarFicha{ClienteID: clienteID, fallo: true}); err != nil {
				return err
			}
		}
		if servicioID == nil {
			return nil
		}
		return Problema(err)
	default:
		return Problema(err)
	}
	if !fichaSirve(ficha, servicioID, e) {
		return errNoDisponible()
	}
	_, err = m.guardarFichaInterna.Ejecutar(ctx, entradaGuardarFicha{ClienteID: clienteID, ficha: &ficha})
	return err
}

// sirveLocal: la instantánea ya basta (cliente de la empresa y, si se pidió, servicio suyo y de
// la empresa).
func sirveLocal(ctx context.Context, q *argosbd.Queries, c argosbd.IntegracionArgosCliente, servicioID *int64, e int64) (bool, error) {
	if e != 0 && !enEmpresa(empresasDeFila(c), c.SinServicios, e) {
		return false, nil
	}
	if servicioID == nil {
		return true, nil
	}
	s, err := q.ServicioPorID(ctx, *servicioID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return s.ClienteID == c.ID && (e == 0 || s.EmpresaID == e), nil
}

// fichaSirve valida lo leído de Argos antes de guardarlo: el cliente es de la empresa (o no tiene
// servicios, según la lista completa) y el servicio, si se pidió, es suyo y de la empresa.
func fichaSirve(f fichaArgos, servicioID *int64, e int64) bool {
	if e != 0 && !slices.Contains(EmpresasDelCliente(f.cliente, f.servicios), e) && (!f.completa || len(f.servicios) > 0) {
		return false
	}
	if servicioID == nil {
		return true
	}
	for _, s := range f.servicios {
		if s.ID == *servicioID {
			return (s.ClienteID == 0 || s.ClienteID == f.cliente.ID) && (e == 0 || s.EmpresaID == e)
		}
	}
	return false
}

type entradaGuardarFicha struct {
	ClienteID int64 `json:"cliente_id"`
	// Uno de: la ficha leída, el 404 de Argos (borrar) o una caída (fallo).
	ficha  *fichaArgos
	borrar bool
	fallo  bool
}

// nuevoGuardarFichaInterna aplica lo que se leyó de Argos para validar un alta (sin permiso de
// clientes.ver: basta el actor que crea el ticket o la orden). Auditada: es una lectura de datos
// personales.
func nuevoGuardarFichaInterna() *auditoria.Caso[entradaGuardarFicha, struct{}] {
	return auditoria.Nuevo(auditoria.Caso[entradaGuardarFicha, struct{}]{
		Accion: "clientes.sincronizar", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaGuardarFicha) (struct{}, error) {
			t.FijarEntidad("cliente_argos", in.ClienteID)
			q := argosbd.New(t)
			switch {
			case in.borrar:
				t.Anotar(map[string]any{"resultado_argos": "no_existe"})
				if err := q.AnonimizarCopiasDeCliente(ctx, in.ClienteID); err != nil {
					return struct{}{}, err
				}
				return struct{}{}, q.BorrarCliente(ctx, in.ClienteID)
			case in.fallo:
				t.Anotar(map[string]any{"resultado_argos": "no_disponible"})
				ahora := time.Now()
				return struct{}{}, q.MarcarFalloCliente(ctx, argosbd.MarcarFalloClienteParams{ID: in.ClienteID, FalloEn: &ahora})
			case in.ficha != nil:
				return struct{}{}, guardarFicha(ctx, t, q, *in.ficha)
			}
			return struct{}{}, nil
		},
	})
}

// Asegurador es AsegurarInstantanea (cmd/iris lo pasa a soporte y campo). nil = sin conector
// Argos en ese proceso (OpenAPI, pruebas): no se verifica.
type Asegurador func(ctx context.Context, clienteID int64, servicioID *int64) error

// ConInstantanea envuelve el caso de uso que crea un ticket o una orden: antes de abrir su
// transacción, asegura en la instantánea el cliente y el servicio pedidos (AsegurarInstantanea),
// para que el caso los valide contra Argos (cliente y servicio de la empresa, servicio del
// cliente). Solo si el actor tiene el permiso del caso (si no, el caso responde 403 sin que se
// haya consultado Argos) y solo para usuarios, tokens e integraciones: el actor cliente del portal
// (su cliente y servicio los fija el portal desde la instantánea) y el sistema (órdenes de retiro y
// de alta, que salen de datos de Argos, a veces dentro de otra transacción) no consultan.
type ConInstantanea[In, Out any] struct {
	Caso     *auditoria.Caso[In, Out]
	Permiso  nucleo.Permiso
	Asegurar Asegurador
	// Ids devuelve el cliente y el servicio de la entrada.
	Ids func(In) (clienteID, servicioID *int64)
}

// Ejecutar asegura la instantánea y ejecuta el caso.
func (c *ConInstantanea[In, Out]) Ejecutar(ctx context.Context, in In) (Out, error) {
	tipo := contexto.ActorDe(ctx).Tipo
	externo := tipo == contexto.ActorUsuario || tipo == contexto.ActorToken || tipo == contexto.ActorIntegracion
	if cliente, servicio := c.Ids(in); c.Asegurar != nil && cliente != nil && externo && contexto.PermisosDe(ctx).Tiene(string(c.Permiso)) {
		if err := c.Asegurar(ctx, *cliente, servicio); err != nil {
			var cero Out
			return cero, err
		}
	}
	return c.Caso.Ejecutar(ctx, in)
}
