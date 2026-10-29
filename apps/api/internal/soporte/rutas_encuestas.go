package soporte

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

const etiquetaEncuestas = "encuestas"

type entradaConsultarPublica struct {
	Body EntradaEncuestaPublica
}

// entradaResponderPublica lleva además la IP del cliente (RemoteAddr ya viene corregido por el
// servidor con los proxies confiables) y su User-Agent.
type entradaResponderPublica struct {
	UserAgent string `header:"User-Agent"`
	Body      EntradaResponderEncuesta
	ip        string
}

// Resolve implementa huma.Resolver: toma la IP remota.
func (e *entradaResponderPublica) Resolve(ctx huma.Context) []error {
	e.ip = nucleo.HostDe(ctx.RemoteAddr())
	return nil
}

type salidaEnlace struct {
	CacheControl string `header:"Cache-Control"`
	Body         EnlaceEncuesta
}

// operacionPublica: sin sesión (el middleware de identidad ignora la cookie del panel en
// /api/v1/encuestas/publica/).
func operacionPublica(id, ruta, resumen, detalle string, errores ...int) huma.Operation {
	return huma.Operation{
		OperationID: id, Method: http.MethodPost, Path: "/api/v1/encuestas/publica/" + ruta, Summary: resumen,
		Description: detalle, Tags: []string{etiquetaEncuestas}, DefaultStatus: http.StatusOK,
		Errors: append([]int{http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusTooManyRequests}, errores...),
	}
}

// registrarEncuestas publica la lista del panel, compartir el enlace y los accesos públicos.
func (m *Modulo) registrarEncuestas(api huma.API) {
	lista := operacion("listarEncuestas", http.MethodGet, "/api/v1/encuestas", "Encuestas de satisfacción", nucleo.PermEncuestasVer, http.StatusOK)
	lista.Tags = []string{etiquetaEncuestas}
	lista.Description += " Más recientes primero (cursor). Respeta la empresa activa y el alcance por zona o cuadrilla. " +
		"`ticketId` / `ordenId` dan la encuesta del detalle del ticket o la orden. Nunca trae el enlace: con " +
		"`enlaceDisponible` se pide aparte (`POST /encuestas/{id}/enlace`, permiso `encuestas.compartir`). " +
		"`alerta` = calificación 1 o 2. Lectura no auditada (como las demás listas)."
	huma.Register(api, lista, func(ctx context.Context, in *EntradaListarEncuestas) (*cuerpo[nucleo.Pagina[Encuesta]], error) {
		return responder(m.ListarEncuestas.Ejecutar(ctx, *in))
	})

	compartir := operacion("compartirEncuesta", http.MethodPost, "/api/v1/encuestas/{id}/enlace", "Pedir el enlace de una encuesta para compartirlo",
		nucleo.PermEncuestasCompartir, http.StatusOK, http.StatusNotFound, http.StatusConflict, http.StatusGone)
	compartir.Tags = []string{etiquetaEncuestas}
	compartir.Description += " Acción auditada (quién lo pidió; el token queda como `***`). Solo una encuesta pendiente de " +
		"canal `enlace`: si salió por correo, 409 `encuesta_por_correo`; respondida, 409 `encuesta_respondida`; vencida o " +
		"anulada, 410 `encuesta_vencida` / `encuesta_anulada`; sin clave para derivarlo, 422 `enlace_no_disponible`. " +
		"Respuesta con `Cache-Control: no-store`."
	huma.Register(api, compartir, func(ctx context.Context, in *struct {
		ID int64 `path:"id" minimum:"1"`
	}) (*salidaEnlace, error) {
		e, err := m.CompartirEncuesta.Ejecutar(ctx, EntradaCompartirEncuesta{ID: in.ID})
		if err != nil {
			return nil, err
		}
		// La respuesta lleva el token: no se guarda en nucleo.idempotencia.
		nucleo.NoRepetible(ctx)
		return &salidaEnlace{CacheControl: "no-store", Body: e}, nil
	})

	huma.Register(api, operacionPublica("consultarEncuestaPublica", "consultar", "Consultar una encuesta por su token (público)",
		"Sin sesión (la cookie del panel no cuenta aquí). El token va en el cuerpo, nunca en la ruta. Datos mínimos "+
			"(número del ticket u orden, fecha, empresa, estado `pendiente`, `respondida`, `vencida` o `anulada`); nada "+
			"personal. Token inválido: 404 `encuesta_no_encontrada` (no se audita); pasados 1000 tokens inválidos por minuto "+
			"(de todos), 429 `demasiados_intentos` con `Retry-After`, sin tocar la base."),
		func(ctx context.Context, in *entradaConsultarPublica) (*cuerpo[EncuestaPublica], error) {
			if err := m.TopeEncuestasExcedido(); err != nil {
				return nil, err
			}
			return responder(m.VerEncuestaPublica.Ejecutar(ctx, in.Body))
		})

	huma.Register(api, operacionPublica("responderEncuestaPublica", "responder", "Responder una encuesta (público)",
		"Sin sesión. `{ token, calificacion (1–5), comentario? (≤ 1000) }`. Un solo uso: repetir la misma respuesta "+
			"devuelve 200 sin cambios; otra distinta, 409 `encuesta_respondida`. Vencida (7 días): 410 `encuesta_vencida`; "+
			"anulada (el ticket se reabrió): 410 `encuesta_anulada`. Token inválido: 404 (no se audita) o 429 como en "+
			"consultar. Guarda la IP y el User-Agent. Emite `encuesta.respondida` (`alerta: true` con calificación ≤ 2). "+
			"El token y el comentario quedan como `***` en la auditoría.", http.StatusConflict, http.StatusGone),
		func(ctx context.Context, in *entradaResponderPublica) (*cuerpo[EncuestaPublica], error) {
			if err := m.TopeEncuestasExcedido(); err != nil {
				return nil, err
			}
			e := in.Body
			e.IP, e.UserAgent = in.ip, in.UserAgent
			return responder(m.ResponderEncuesta.Ejecutar(ctx, e))
		})
}
