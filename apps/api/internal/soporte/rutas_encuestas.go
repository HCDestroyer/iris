package soporte

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

const etiquetaEncuestas = "encuestas"

type entradaResponderPublica struct {
	Token string `path:"token" maxLength:"200" doc:"Lo que sigue a # en /encuesta#<token>"`
	Body  CuerpoResponderEncuesta
}

// registrarEncuestas publica la lista del panel y los accesos públicos de la encuesta.
func (m *Modulo) registrarEncuestas(api huma.API) {
	lista := operacion("listarEncuestas", http.MethodGet, "/api/v1/encuestas", "Encuestas de satisfacción", nucleo.PermEncuestasVer, http.StatusOK)
	lista.Tags = []string{etiquetaEncuestas}
	lista.Description += " Más recientes primero (cursor). Respeta la empresa activa y el alcance por zona o cuadrilla. " +
		"`ticketId` / `ordenId` dan la encuesta del detalle del ticket o la orden. Mientras está pendiente trae `enlace` " +
		"(`<IRIS_BASE_URL>/encuesta#<token>`) para que soporte lo comparta; `enlaceDisponible: false` si no se puede " +
		"volver a mostrar (sin IRIS_CLAVE_CIFRADO o con la clave rotada). `alerta` = calificación 1 o 2. Lectura no auditada " +
		"(como las demás listas)."
	huma.Register(api, lista, func(ctx context.Context, in *EntradaListarEncuestas) (*cuerpo[nucleo.Pagina[Encuesta]], error) {
		return responder(m.ListarEncuestas.Ejecutar(ctx, *in))
	})

	ver := huma.Operation{
		OperationID: "verEncuestaPublica", Method: http.MethodGet, Path: "/api/v1/encuestas/publica/{token}",
		Summary: "Encuesta por su enlace (público)", Tags: []string{etiquetaEncuestas}, DefaultStatus: http.StatusOK,
		Errors: []int{http.StatusNotFound, http.StatusTooManyRequests},
		Description: "Sin sesión. Datos mínimos (número del ticket u orden, fecha, empresa, estado `pendiente`, " +
			"`respondida` o `vencida`); nada personal. Token inválido: 404 `encuesta_no_encontrada` (no se audita); " +
			"pasados 1000 tokens inválidos por minuto (de todos), 429 `demasiados_intentos` con `Retry-After`.",
	}
	huma.Register(api, ver, func(ctx context.Context, in *EntradaEncuestaPublica) (*cuerpo[EncuestaPublica], error) {
		return responder(m.VerEncuestaPublica.Ejecutar(ctx, *in))
	})

	resp := huma.Operation{
		OperationID: "responderEncuestaPublica", Method: http.MethodPost, Path: "/api/v1/encuestas/publica/{token}/responder",
		Summary: "Responder la encuesta (público)", Tags: []string{etiquetaEncuestas}, DefaultStatus: http.StatusOK,
		Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusGone, http.StatusUnprocessableEntity, http.StatusTooManyRequests},
		Description: "Sin sesión. `calificacion` 1–5 y `comentario` opcional (≤ 1000). Un solo uso: repetir la misma " +
			"respuesta devuelve 200 sin cambios (idempotente); otra distinta, 409 `encuesta_respondida`. Vencida (7 días): " +
			"410 `encuesta_vencida`. Token inválido: 404 (no se audita) o 429 como en el GET. Emite `encuesta.respondida` " +
			"(`alerta: true` con calificación ≤ 2). El token y el comentario quedan como `***` en la auditoría.",
	}
	huma.Register(api, resp, func(ctx context.Context, in *entradaResponderPublica) (*cuerpo[EncuestaPublica], error) {
		return responder(m.ResponderEncuesta.Ejecutar(ctx, EntradaResponderEncuesta{
			Token: in.Token, Calificacion: in.Body.Calificacion, Comentario: in.Body.Comentario,
		}))
	})
}
