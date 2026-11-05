package mensajeria

import (
	"context"
	"strconv"
	"sync"

	mensajeriabd "github.com/HCDestroyer/iris/apps/api/internal/mensajeria/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/adjuntos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

var registroAdjuntosChat sync.Once

// RegistrarAdjuntos registra la conversación del chat como entidad adjuntable (fotos de los
// mensajes; idempotente). POST /api/v1/adjuntos lo atiende la API (cmd/iris), no este proceso: lo
// llaman cmd/iris y cmd/worker. Ver = la conversación es visible para el actor; subir = además
// chat.enviar. Si no, 404 como la conversación.
func RegistrarAdjuntos() {
	registroAdjuntosChat.Do(func() {
		adjuntos.RegistrarEntidadAdjuntable(EntidadAdjuntosChat, func(ctx context.Context, db adjuntos.DBTX, id string, uso adjuntos.Uso) (int64, error) {
			conv, err := strconv.ParseInt(id, 10, 64)
			if err != nil || conv <= 0 {
				return 0, conversacionNoEncontrada()
			}
			if uso == adjuntos.UsoSubir && !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermChatEnviar)) {
				return 0, conversacionNoEncontrada()
			}
			if !contexto.PermisosDe(ctx).Tiene(string(nucleo.PermChatVer)) {
				return 0, conversacionNoEncontrada()
			}
			a, err := alcanceDe(ctx)
			if err != nil {
				return 0, conversacionNoEncontrada()
			}
			q := mensajeriabd.New(db)
			ok, err := q.ChatVisible(ctx, mensajeriabd.ChatVisibleParams{Usuario: a.usuario, Empresa: a.empresa, Todas: a.todas, SupGlobal: a.global,
				SupZonas: a.zonas, SupCuadrillas: a.cuadrillas, ConversacionID: conv})
			if err != nil {
				return 0, err
			}
			if !ok {
				return 0, conversacionNoEncontrada()
			}
			c, err := q.ConversacionPorID(ctx, conv)
			if err != nil {
				return 0, err
			}
			if c.EmpresaID == nil {
				return 0, nil
			}
			return *c.EmpresaID, nil
		})
	})
}
