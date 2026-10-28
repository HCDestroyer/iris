package soporte

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// registrarBuzones publica la administración de buzones de correo. Nunca devuelve contraseñas.
func (m *Modulo) registrarBuzones(api huma.API) {
	huma.Register(api, operacion("listarBuzones", http.MethodGet, "/api/v1/buzones", "Buzones de correo (entrada de tickets)", nucleo.PermBuzonesVer, http.StatusOK),
		func(ctx context.Context, in *EntradaListarBuzones) (*cuerpo[nucleo.Pagina[Buzon]], error) {
			return responder(m.ListarBuzones.Ejecutar(ctx, *in))
		})

	crear := operacion("crearBuzon", http.MethodPost, "/api/v1/buzones", "Registrar un buzón de correo", nucleo.PermBuzonesCrear, http.StatusCreated)
	crear.Description += " Nace inactivo. Solo TLS (`tls` o `starttls`). Las contraseñas se guardan cifradas (IRIS_CLAVE_CIFRADO; sin ella, 422 `cifrado_no_configurado`) y nunca se devuelven. Un host que es una IP interna da 422 `destino_no_permitido`. Emite `buzon.creado`."
	huma.Register(api, crear, func(ctx context.Context, in *conClave[EntradaCrearBuzon]) (*cuerpo[Buzon], error) {
		return responder(m.CrearBuzon.Ejecutar(ctx, in.Body))
	})

	huma.Register(api, operacion("verBuzon", http.MethodGet, "/api/v1/buzones/{id}", "Ver un buzón de correo", nucleo.PermBuzonesVer, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *EntradaVerBuzon) (*cuerpo[Buzon], error) {
			return responder(m.VerBuzon.Ejecutar(ctx, *in))
		})

	editar := operacion("editarBuzon", http.MethodPost, "/api/v1/buzones/{id}/editar", "Editar un buzón de correo", nucleo.PermBuzonesEditar, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	editar.Description += " Reemplaza los datos editables (con `version`). Una contraseña omitida se conserva, salvo que cambie el host, el puerto, el TLS o el usuario: entonces es obligatoria (la guardada no viaja a otro servidor). Cambiar servidor, usuario o carpeta reinicia la lectura (solo los no leídos). Emite `buzon.actualizado`."
	huma.Register(api, editar, func(ctx context.Context, in *conID[EntradaEditarBuzon]) (*cuerpo[Buzon], error) {
		e := in.Body
		e.ID = in.ID
		return responder(m.EditarBuzon.Ejecutar(ctx, e))
	})

	activar := operacion("activarBuzon", http.MethodPost, "/api/v1/buzones/{id}/activar", "Activar un buzón (empieza a leerse cada minuto)", nucleo.PermBuzonesActivar, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	activar.Description += " La primera lectura toma solo los correos no leídos. Emite `buzon.activado`."
	huma.Register(api, activar, func(ctx context.Context, in *conID[EntradaCambiarBuzon]) (*cuerpo[Buzon], error) {
		e := in.Body
		e.ID = in.ID
		return responder(m.ActivarBuzon.Ejecutar(ctx, e))
	})

	desactivar := operacion("desactivarBuzon", http.MethodPost, "/api/v1/buzones/{id}/desactivar", "Desactivar un buzón (deja de leerse)", nucleo.PermBuzonesDesactivar, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	desactivar.Description += " Emite `buzon.desactivado`."
	huma.Register(api, desactivar, func(ctx context.Context, in *conID[EntradaCambiarBuzon]) (*cuerpo[Buzon], error) {
		e := in.Body
		e.ID = in.ID
		return responder(m.DesactivarBuzon.Ejecutar(ctx, e))
	})

	probar := operacion("probarBuzon", http.MethodPost, "/api/v1/buzones/{id}/probar", "Probar la conexión IMAP (y SMTP) del buzón", nucleo.PermBuzonesEditar, http.StatusOK, http.StatusNotFound)
	probar.Description += " Conecta con TLS, inicia sesión y abre la carpeta (IMAP); con SMTP, negocia TLS y se autentica sin enviar nada. Responde 200 con el resultado de cada uno: `ok` o un `codigo` y `detalle` resumidos (nunca la contraseña ni la respuesta cruda del servidor)."
	huma.Register(api, probar, func(ctx context.Context, in *struct {
		nucleo.ConIdempotencia
		ID int64 `path:"id" minimum:"1"`
	}) (*cuerpo[ResultadoPrueba], error) {
		return responder(m.ProbarBuzon.Ejecutar(ctx, EntradaVerBuzon{ID: in.ID}))
	})
}
