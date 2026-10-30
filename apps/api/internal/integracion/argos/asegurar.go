package argos

import (
	"context"
	"errors"
	"slices"

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
//   - Si la instantánea ya tiene el cliente y el servicio pedido: no llama a Argos.
//   - Si no: lee la ficha (GET /clientes/:id y sus servicios) y la guarda (acción
//     clientes.sincronizar, auditada). Un servicio que no está entre los del cliente: 422
//     servicioId. Un cliente que Argos no tiene: 422 clienteId.
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
	q := argosbd.New(bd)
	_, err = q.ClientePorID(ctx, clienteID)
	tieneCliente := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if tieneCliente && servicioID == nil {
		return nil
	}
	if tieneCliente && servicioID != nil {
		s, err := q.ServicioPorID(ctx, *servicioID)
		if err == nil && s.ClienteID == clienteID {
			return nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	ficha, err := leerFichaArgos(ctx, bd, m.clienteDe(c), clienteID)
	switch {
	case err == nil:
	case EsNoEncontrado(err):
		return nucleo.Validacion(nucleo.ErrorCampo{Field: "clienteId", Message: "El cliente no existe en Argos", Rule: "cliente"})
	case EsCaido(err) && servicioID == nil:
		return nil
	default:
		return Problema(err)
	}
	if _, err := m.guardarFichaInterna.Ejecutar(ctx, entradaGuardarFicha{ClienteID: clienteID, ficha: ficha}); err != nil {
		return err
	}
	if servicioID != nil && !slices.Contains(ficha.idsServicios(), *servicioID) {
		return nucleo.Validacion(nucleo.ErrorCampo{Field: "servicioId", Message: "El servicio no es de ese cliente según Argos", Rule: "servicio"})
	}
	return nil
}

type entradaGuardarFicha struct {
	ClienteID int64 `json:"cliente_id"`
	ficha     fichaArgos
}

// nuevoGuardarFichaInterna guarda una ficha leída de Argos para validar un alta (sin permiso de
// clientes.ver: basta el actor que crea el ticket o la orden). Auditada: es una lectura de
// datos personales.
func nuevoGuardarFichaInterna() *auditoria.Caso[entradaGuardarFicha, struct{}] {
	return auditoria.Nuevo(auditoria.Caso[entradaGuardarFicha, struct{}]{
		Accion: "clientes.sincronizar", Acceso: auditoria.Autenticado,
		Logica: func(ctx context.Context, t *auditoria.Tx, in entradaGuardarFicha) (struct{}, error) {
			t.FijarEntidad("cliente_argos", in.ClienteID)
			return struct{}{}, guardarFicha(ctx, t, argosbd.New(t), in.ficha)
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
