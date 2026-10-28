package argos

import (
	"net/netip"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
)

// Nombres que el conector Argos exportaba antes de que la base común pasara a
// internal/integracion/conector.

// ErrDestinoBloqueado: la URL de Argos resuelve a una red interna no permitida.
var ErrDestinoBloqueado = conector.ErrDestinoBloqueado

// DestinoPermitido indica si Iris puede conectarse a ip (permitidas tiene prioridad).
func DestinoPermitido(ip netip.Addr, permitidas []netip.Prefix) bool {
	return conector.DestinoPermitido(ip, permitidas)
}

// FirmarWebhook calcula la cabecera X-Webhook-Signature.
func FirmarWebhook(secreto, marca string, cuerpo []byte) string {
	return conector.FirmarWebhook(secreto, marca, cuerpo)
}
