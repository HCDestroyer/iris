package mensajeria

import (
	"context"

	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Para el emisor de push (internal/notificaciones, contratos/push.md §3): al enviar, vuelve a
// comprobar con las reglas de siempre si el destinatario todavía ve la conversación. ctx trae al
// destinatario como actor, con sus permisos de ahora (identidad.Revalidar) y la empresa de su
// dispositivo; bd es la transacción del emisor.

// ChatVisiblePara indica si el actor de ctx ve la conversación del chat interno (chat.ver y
// miembro, o la cubre su alcance: contratos/chat.md §1).
func ChatVisiblePara(ctx context.Context, bd mensajeriabd.DBTX, conversacionID int64) (bool, error) {
	if !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermChatVer)) {
		return false, nil
	}
	a, err := alcanceDe(ctx)
	if err != nil {
		return false, nil //nolint:nilerr // sin un usuario de Iris como actor, no la ve
	}
	return mensajeriabd.New(bd).ChatVisible(ctx, mensajeriabd.ChatVisibleParams{Usuario: a.usuario, Empresa: a.empresa, Todas: a.todas,
		SupGlobal: a.global, SupZonas: a.zonas, SupCuadrillas: a.cuadrillas, OrdGlobal: a.ordGlobal, OrdZonas: a.ordZonas,
		OrdCuadrillas: a.ordCuadrillas, ConversacionID: conversacionID})
}

// ConversacionVisiblePara indica si el actor de ctx ve la conversación con el cliente
// (conversaciones.ver, persona con sesión, y es de sus colas, sin cola, asignada a él o lo
// mencionaron: contratos/conversaciones.md).
func ConversacionVisiblePara(ctx context.Context, bd mensajeriabd.DBTX, conversacionID int64) (bool, error) {
	a, err := alcanceConvDe(ctx)
	if err != nil {
		return false, nil //nolint:nilerr // sin conversaciones.ver o sin sesión de persona, no la ve
	}
	return mensajeriabd.New(bd).ConversacionVisible(ctx, mensajeriabd.ConversacionVisibleParams{
		ID: conversacionID, Empresa: a.empresa, Usuario: a.usuario, Supervisa: a.supervisa,
	})
}
