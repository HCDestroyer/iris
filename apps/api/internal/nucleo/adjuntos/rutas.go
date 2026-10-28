package adjuntos

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

const etiqueta = "adjuntos"

var seguridad = []map[string][]string{{"sesion": {}}, {"bearer": {}}}

func operacion(id, metodo, ruta, resumen string, permiso nucleo.Permiso, status int, errores ...int) huma.Operation {
	return huma.Operation{
		OperationID: id, Method: metodo, Path: ruta, Summary: resumen, Tags: []string{etiqueta},
		DefaultStatus: status, Security: seguridad,
		Errors:      append([]int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusUnprocessableEntity}, errores...),
		Extensions:  map[string]any{"x-permiso": string(permiso)},
		Description: "Permiso: `" + string(permiso) + "`.",
	}
}

type entradaRegistrarHTTP struct {
	nucleo.ConIdempotencia
	Body EntradaRegistrar
}

type salidaRegistrar struct {
	Status   int
	Location string `header:"Location" doc:"URL de subida tus"`
	Body     Adjunto
}

type entradaID struct {
	ID string `path:"id" format:"uuid" doc:"Id del adjunto"`
}

type salidaLista struct {
	Body nucleo.Pagina[Adjunto]
}

// Registrar publica las rutas de adjuntos.
func (m *Modulo) RegistrarRutas(api huma.API) {
	alta := operacion("registrarAdjunto", http.MethodPost, "/api/v1/adjuntos", "Registrar un adjunto y obtener su URL de subida",
		nucleo.PermAdjuntosSubir, http.StatusCreated, http.StatusConflict, http.StatusRequestEntityTooLarge, http.StatusServiceUnavailable)
	alta.Description += " Idempotente por `id` (UUID generado en el cliente): repetir con los mismos datos responde 200 con el " +
		"adjunto (y su `recibido`, para retomar); con otros datos, 409 `adjunto_existente`. La entidad la valida su módulo " +
		"(404 si no existe, es de otra empresa o no puedes usarla; 422 `entidad_no_adjuntable` si no acepta adjuntos). " +
		"422 `mime_no_permitido` (IRIS_ADJUNTOS_MIME), 413 `tamano_excedido` (IRIS_ADJUNTOS_TAMANO_MAX). " +
		"Responde `urlSubida` (también en `Location`) para subir con tus 1.0 (HEAD/PATCH). Emite `adjunto.registrado`."
	huma.Register(api, alta, func(ctx context.Context, in *entradaRegistrarHTTP) (*salidaRegistrar, error) {
		r, err := m.Registrar.Ejecutar(ctx, in.Body)
		if err != nil {
			return nil, err
		}
		s := &salidaRegistrar{Status: http.StatusOK, Location: r.Adjunto.URLSubida, Body: r.Adjunto}
		if r.Nuevo {
			s.Status = http.StatusCreated
		}
		return s, nil
	})

	lista := operacion("listarAdjuntos", http.MethodGet, "/api/v1/adjuntos", "Adjuntos de una entidad", nucleo.PermAdjuntosVer, http.StatusOK)
	lista.Description += " Todos los estados (el panel muestra los completos; la app, lo que falta subir)."
	huma.Register(api, lista, func(ctx context.Context, in *EntradaListar) (*salidaLista, error) {
		p, err := m.Listar.Ejecutar(ctx, *in)
		if err != nil {
			return nil, err
		}
		return &salidaLista{Body: p}, nil
	})

	descarga := operacion("descargarAdjunto", http.MethodGet, "/api/v1/adjuntos/{id}", "Descargar un adjunto completo",
		nucleo.PermAdjuntosVer, http.StatusOK, http.StatusConflict, http.StatusServiceUnavailable)
	descarga.Description += " Lectura auditada. Solo con estado `completo` (409 `adjunto_incompleto`). Imágenes JPEG, PNG, WebP y GIF " +
		"(firma verificada al completar) van `inline`; todo lo demás (SVG, PDF…) como `attachment`. Siempre con " +
		"`X-Content-Type-Options: nosniff` y `Content-Security-Policy: default-src 'none'; sandbox`."
	descarga.Responses = map[string]*huma.Response{"200": {
		Description: "Contenido del adjunto",
		Content:     map[string]*huma.MediaType{"application/octet-stream": {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}}},
	}}
	huma.Register(api, descarga, func(ctx context.Context, in *entradaID) (*huma.StreamResponse, error) {
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, nucleo.NoEncontrado("")
		}
		a, err := m.Descargar.Ejecutar(ctx, EntradaDescargar{ID: id})
		if err != nil {
			return nil, err
		}
		if m.cfg.Almacen == nil {
			return nil, nucleo.NoDisponible("No hay almacenamiento de adjuntos configurado")
		}
		rc, err := m.cfg.Almacen.Abrir(ctx, a.clave)
		if err != nil {
			slog.ErrorContext(ctx, "adjuntos: abrir en el almacenamiento", "adjunto", a.ID, "err", err)
			if errors.Is(err, ErrNoExiste) {
				return nil, nucleo.NoEncontrado("El contenido del adjunto no está en el almacenamiento")
			}
			return nil, nucleo.NuevoProblema(http.StatusServiceUnavailable, "almacen_no_disponible", "Almacenamiento no disponible", "Vuelve a intentar en unos minutos")
		}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			defer func() { _ = rc.Close() }()
			// 25 MiB por una red lenta tardan más que el WriteTimeout del servidor (60 s).
			if _, w := humachi.Unwrap(hctx); w != nil {
				_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(m.cfg.TiempoDescarga))
			}
			EscribirCabecerasDescarga(hctx.SetHeader, a)
			hctx.SetStatus(http.StatusOK)
			if _, err := io.Copy(hctx.BodyWriter(), io.LimitReader(rc, a.Tamano)); err != nil {
				slog.WarnContext(ctx, "adjuntos: descarga interrumpida", "adjunto", a.ID, "err", err)
			}
		}}, nil
	})

	m.registrarTus(api)
}

// EscribirCabecerasDescarga fija Content-Type y Content-Disposition seguros: en línea solo las
// imágenes de mapa de bits verificadas; el resto como descarga. El nombre lo arma Iris (id +
// extensión), nunca el cliente.
func EscribirCabecerasDescarga(fijar func(nombre, valor string), a Adjunto) {
	nombre := a.ID.String() + extension(a.Mime)
	tipo, disposicion := "application/octet-stream", "attachment"
	switch {
	case esImagenSegura(a.Mime):
		tipo, disposicion = a.Mime, "inline"
	case a.Mime == "application/pdf" || a.Mime == "image/svg+xml":
		tipo = a.Mime
	}
	fijar("Content-Type", tipo)
	fijar("Content-Disposition", disposicion+`; filename="`+nombre+`"`)
	fijar("Content-Length", strconv.FormatInt(a.Tamano, 10))
	fijar("X-Content-Type-Options", "nosniff")
	fijar("Content-Security-Policy", "default-src 'none'; sandbox")
	fijar("Cache-Control", "private, no-store")
	fijar("ETag", `"`+a.SHA256+`"`)
}

// registrarTus publica HEAD y PATCH de la subida (núcleo de tus 1.0). Se atienden fuera de
// huma (el cuerpo se lee por partes, sin cargarlo entero) pero pasan por los mismos
// middlewares (identidad con sesión+CSRF o Bearer, empresa activa, canal) y quedan en el
// OpenAPI.
func (m *Modulo) registrarTus(api huma.API) {
	const ruta = "/api/v1/adjuntos/{id}/subida"
	param := func(nombre, en, desc string, requerido bool, tipo string) *huma.Param {
		return &huma.Param{Name: nombre, In: en, Description: desc, Required: requerido, Schema: &huma.Schema{Type: tipo}}
	}
	id := &huma.Param{Name: "id", In: "path", Required: true, Schema: &huma.Schema{Type: huma.TypeString, Format: "uuid"}}
	version := param("Tus-Resumable", "header", "Siempre 1.0.0 (412 `tus_version` si no)", true, huma.TypeString)
	cabOffset := &huma.Header{Description: "Bytes recibidos", Schema: &huma.Schema{Type: huma.TypeInteger}}
	problema := func(desc string) *huma.Response {
		return &huma.Response{Description: desc, Content: map[string]*huma.MediaType{
			nucleo.TipoContenidoProblema: {Schema: &huma.Schema{Ref: "#/components/schemas/Problema"}}}}
	}
	errores := map[string]*huma.Response{
		"401": problema("Sin credenciales"), "403": problema("Sin el permiso adjuntos.subir"),
		"404": problema("No existe o no puedes usar su entidad"), "409": problema("`offset_distinto` (trae Upload-Offset)"),
		"410": problema("`adjunto_rechazado`: regístralo con otro id"), "412": problema("`tus_version`"),
	}
	cabeza := &huma.Operation{
		OperationID: "consultarSubidaAdjunto", Method: http.MethodHead, Path: ruta, Tags: []string{etiqueta}, Security: seguridad,
		Summary:     "Consultar cuánto se ha subido (tus HEAD)",
		Description: "Permiso: `adjuntos.subir`. Responde `Upload-Offset` (bytes recibidos) y `Upload-Length`; el cliente sigue con PATCH desde ese offset.",
		Extensions:  map[string]any{"x-permiso": string(nucleo.PermAdjuntosSubir)},
		Parameters:  []*huma.Param{id, version},
		Responses: map[string]*huma.Response{"200": {Description: "Estado de la subida", Headers: map[string]*huma.Header{
			"Upload-Offset": cabOffset, "Upload-Length": {Description: "Tamaño declarado", Schema: &huma.Schema{Type: huma.TypeInteger}},
		}}},
	}
	parche := &huma.Operation{
		OperationID: "subirParteAdjunto", Method: http.MethodPatch, Path: ruta, Tags: []string{etiqueta}, Security: seguridad,
		Summary: "Subir bytes desde Upload-Offset (tus PATCH)",
		Description: "Permiso: `adjuntos.subir`. Con sesión, `X-CSRF-Token`. Cuerpo crudo `application/offset+octet-stream` (415 si no). " +
			"Se guarda a medida que llega: si la conexión se corta, lo recibido cuenta (consulta con HEAD). Al completar el tamaño declarado " +
			"se verifica el SHA-256 y que el contenido sea del MIME declarado: si no, 422 `sha256_distinto` / `mime_distinto` y el adjunto " +
			"queda `rechazado`; más bytes que los declarados, 413 `tamano_excedido` (rechazado). Si el almacenamiento falla, 503 " +
			"`almacen_no_disponible`: repite el PATCH con Upload-Offset = tamaño y sin cuerpo. Completo emite `adjunto.completado`.",
		Extensions: map[string]any{"x-permiso": string(nucleo.PermAdjuntosSubir)},
		Parameters: []*huma.Param{id, version, param("Upload-Offset", "header", "Offset desde el que van estos bytes (debe ser el recibido)", true, huma.TypeInteger)},
		RequestBody: &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{
			TipoParte: {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}}}},
		Responses: map[string]*huma.Response{"204": {Description: "Parte guardada", Headers: map[string]*huma.Header{"Upload-Offset": cabOffset}},
			"413": problema("`tamano_excedido`"), "415": problema("Content-Type distinto de application/offset+octet-stream"),
			"422": problema("`sha256_distinto` o `mime_distinto`"), "503": problema("`almacen_no_disponible`")},
	}
	for k, v := range errores {
		cabeza.Responses[k] = v
		parche.Responses[k] = v
	}
	api.OpenAPI().AddOperation(cabeza)
	api.OpenAPI().AddOperation(parche)
	api.Adapter().Handle(cabeza, func(hctx huma.Context) {
		r, w := humachi.Unwrap(hctx)
		m.Consultar(r.Context(), w, r, hctx.Param("id"))
	})
	api.Adapter().Handle(parche, func(hctx huma.Context) {
		r, w := humachi.Unwrap(hctx)
		m.Subir(r.Context(), w, r, hctx.Param("id"))
	})
}
