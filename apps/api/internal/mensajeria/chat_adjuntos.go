package mensajeria

import (
	"context"
	"strconv"
	"sync"

	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
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
		registrarAdjuntosConversacion()
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
				SupZonas: a.zonas, SupCuadrillas: a.cuadrillas, OrdGlobal: a.ordGlobal, OrdZonas: a.ordZonas, OrdCuadrillas: a.ordCuadrillas, ConversacionID: conv})
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
		// Una foto se ve solo mientras un mensaje sin eliminar de la conversación la muestra: ni la
		// de un mensaje eliminado ni la subida y nunca enviada. Subir (tus) solo su autor.
		adjuntos.RegistrarFiltro(EntidadAdjuntosChat, func(ctx context.Context, db adjuntos.DBTX, a adjuntos.Adjunto, uso adjuntos.Uso) (bool, error) {
			if uso == adjuntos.UsoSubir {
				actor := contexto.ActorDe(ctx)
				return a.SubidoPor != nil && actor.EsUsuario() && *a.SubidoPor == actor.ID, nil
			}
			conv, err := strconv.ParseInt(a.EntidadID, 10, 64)
			if err != nil {
				return false, nil
			}
			return mensajeriabd.New(db).MensajeVivoConAdjunto(ctx, mensajeriabd.MensajeVivoConAdjuntoParams{AdjuntoID: pgUUID(&a.ID), ConversacionID: conv})
		})
	})
}

// Fotos sin enviar --------------------------------------------------------------------------------

// retirarFotoSinEnviar retira una foto del chat que ningún mensaje muestra (subida y nunca
// enviada) y devuelve la clave del almacenamiento para borrarla después del COMMIT.
func nuevoRetirarFotoSinEnviar(*Modulo) *auditoria.Caso[uuid.UUID, string] {
	return auditoria.Nuevo(auditoria.Caso[uuid.UUID, string]{
		Accion: "chat.retirar_foto_sin_enviar", Acceso: auditoria.Autenticado, AuditarExito: func(c string) bool { return c != "" },
		Logica: func(ctx context.Context, t *auditoria.Tx, id uuid.UUID) (string, error) {
			t.FijarEntidad("adjunto", id)
			return adjuntos.Retirar(ctx, t, id)
		},
	})
}

// purgarFotosSinEnviar retira las fotos del chat sin mensaje y sin actividad desde hace
// ChatFotosSinEnviar (reloj del módulo) y las borra del almacenamiento.
func (m *Modulo) purgarFotosSinEnviar(ctx context.Context, bd nucleo.BD) error {
	ids, err := mensajeriabd.New(bd).FotosChatSinMensaje(ctx, mensajeriabd.FotosChatSinMensajeParams{
		Antes: m.ahora().Add(-m.cfg.ChatFotosSinEnviar), Limite: 200})
	if err != nil {
		return err
	}
	for _, id := range ids {
		clave, err := m.retirarFoto.Ejecutar(ctx, id)
		if err != nil {
			return err
		}
		adjuntos.BorrarContenido(ctx, m.cfg.Almacen, clave)
	}
	return nil
}
