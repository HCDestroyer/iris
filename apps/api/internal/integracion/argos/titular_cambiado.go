package argos

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	argosbd "github.com/HCDestroyer/iris/apps/api/internal/integracion/argos/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
)

// servicio.titular_cambiado (contrato 0.10.0 §2.3 y §3.4.4; pedido B17): Argos pasó un servicio
// pendiente a otro titular (A39). Iris relee al titular nuevo (con sus servicios) y, si Argos
// confirma que el servicio ya es suyo, refresca la instantánea y emite argos.titular_cambiado:
// comercial pasa el prospecto al cliente nuevo y campo, las órdenes abiertas del servicio. El
// payload nunca trae el documento ni la identidad, y el motivo en texto queda solo en Argos.

// Eventos del cambio de titular.
const (
	EventoServicioTitularCambiado = "servicio.titular_cambiado"
	// EventoTitularCambiado es el evento interno (outbox).
	EventoTitularCambiado = "argos.titular_cambiado"
)

// payloadTitular: los campos de servicio.titular_cambiado que usa Iris.
type payloadTitular struct {
	ServicioID         *int64  `json:"servicio_id"`
	EmpresaID          *int64  `json:"empresa_id"`
	DeClienteID        *int64  `json:"de_cliente_id"`
	AClienteID         *int64  `json:"a_cliente_id"`
	DeClienteEliminado bool    `json:"de_cliente_eliminado"`
	OrigenTipo         *string `json:"origen_tipo"`
	OrigenID           *string `json:"origen_id"`
}

// titularCambiado: lo leído para aplicar el cambio.
type titularCambiado struct {
	payloadTitular
	entranteID int64
}

// leerTitularCambiado decide si el webhook aplica (el servicio está en la instantánea) y relee al
// titular nuevo de Argos. omitido dice por qué no se aplica.
func (p *ProcesarEntrante) leerTitularCambiado(ctx context.Context, bd conector.DBTX, e conector.Entrante, in *entradaAplicar) (string, error) {
	var d payloadTitular
	if err := conector.Decodificar(e.Payload, &d); err != nil || d.ServicioID == nil || d.AClienteID == nil || *d.AClienteID <= 0 {
		return "payload_incompleto", nil
	}
	if _, err := argosbd.New(bd).ServicioPorID(ctx, *d.ServicioID); errors.Is(err, pgx.ErrNoRows) {
		return "servicio_sin_instantanea", nil
	} else if err != nil {
		return "", err
	}
	in.titular = &titularCambiado{payloadTitular: d, entranteID: e.ID}
	in.ClienteID = d.AClienteID
	in.ficha, in.borrar, in.errArgos = p.leerDeArgos(ctx, bd, e.ConectorID, *d.AClienteID)
	if in.borrar {
		// Argos no tiene al titular nuevo (404): no se borra nada por este aviso.
		in.borrar, in.ficha = false, nil
		in.titular = nil
		return "titular_no_confirmado", nil
	}
	return "", nil
}

// emitirTitularCambiado emite argos.titular_cambiado si la ficha leída confirma que el servicio
// es del titular nuevo.
func emitirTitularCambiado(t *auditoria.Tx, in entradaAplicar) (bool, error) {
	c := in.titular
	if c == nil || in.ficha == nil {
		return false, nil
	}
	confirmado := false
	for _, s := range in.ficha.servicios {
		if s.ID == *c.ServicioID && s.ClienteID == *c.AClienteID {
			confirmado = true
		}
	}
	if !confirmado {
		t.Anotar(map[string]any{"titular": "no_confirmado"})
		return false, nil
	}
	if c.EmpresaID != nil {
		t.FijarEmpresa(*c.EmpresaID)
	}
	return true, t.Emitir(EventoTitularCambiado, map[string]any{
		"servicio_id": *c.ServicioID, "empresa_id": c.EmpresaID, "de_cliente_id": c.DeClienteID, "a_cliente_id": *c.AClienteID,
		"de_cliente_eliminado": c.DeClienteEliminado, "origen_tipo": c.OrigenTipo, "origen_id": c.OrigenID,
		"entrante_id": c.entranteID,
	})
}
