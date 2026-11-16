package argos

import (
	"context"
	"strconv"
	"time"
)

// Llamadas de §7 del contrato con Argos que usa el asistente de IA de mensajería cuando atiende solo
// (contratos/conversaciones.md §17.8): el desafío de identidad (§7.5) y la reconexión temporal (§7.3).
// Siguen el patrón de las promesas (conversaciones.go): X-Iris-Actor y X-Iris-Conversacion en cada
// llamada, y Argos decide.

// VerificarIdentidad: POST /integraciones/iris/verificar-identidad { cliente_id, metodo, valor }
// (§7.5). Argos compara y responde solo si coincide; nunca devuelve el dato. El valor no se guarda
// ni va al log en Iris. Un 422 (metodo_no_disponible) o un 429 (demasiados_intentos) vuelven como
// *conector.ErrorRemoto.
func (m *Modulo) VerificarIdentidad(ctx context.Context, a AtencionIris, clienteID int64, metodo, valor string) (bool, error) {
	cli, err := m.clienteAtencion(ctx, a)
	if err != nil {
		return false, err
	}
	var r struct {
		Coincide bool `json:"coincide"`
	}
	// Sin reintentos: cada intento cuenta para el límite de Argos.
	err = cli.Pedir(ctx, "POST", "/integraciones/iris/verificar-identidad", nil,
		map[string]any{"cliente_id": clienteID, "metodo": metodo, "valor": valor}, false, &r)
	return r.Coincide, err
}

// PedirReconexionTemporal: POST /comprobantes/:id/reconexion-temporal { servicio_id?, motivo } con
// Idempotency-Key iris:reconexion:<comprobante_id> (§7.3). Es una promesa de tipo
// reconexion_temporal ligada al comprobante pendiente: Argos decide con su política. Un 422
// (reconexion_no_permitida) o un 409 vuelven como *conector.ErrorRemoto y no se reintentan.
func (m *Modulo) PedirReconexionTemporal(ctx context.Context, a AtencionIris, comprobanteID int64, servicioID *int64, motivo, clave string) (int64, time.Time, error) {
	cli, err := m.clienteAtencion(ctx, a)
	if err != nil {
		return 0, time.Time{}, err
	}
	cuerpo := map[string]any{"motivo": motivo}
	if servicioID != nil {
		cuerpo["servicio_id"] = *servicioID
	}
	var r struct {
		PromesaID int64     `json:"promesa_id"`
		Hasta     time.Time `json:"hasta"`
	}
	err = cli.Escribir(ctx, "POST", "/comprobantes/"+strconv.FormatInt(comprobanteID, 10)+"/reconexion-temporal", cuerpo, clave, &r)
	return r.PromesaID, r.Hasta, err
}
