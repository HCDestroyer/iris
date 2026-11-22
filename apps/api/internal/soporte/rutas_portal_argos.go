package soporte

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Rutas del portal del cliente de Argos (contrato 0.9.0 §11.3), bajo
// /api/v1/integraciones/argosisp/portal/clientes/{cliente_id}. Handlers delgados: resuelven la
// sesión (argos.SesionPortal), separan el JSON y los archivos y llaman al caso de uso.

// RutaPortalArgos es el prefijo de §11.3 (también lo usan las solicitudes de comercial).
const RutaPortalArgos = "/api/v1/integraciones/argosisp/portal/clientes/{cliente_id}"

// CabecerasPortalHTTP son las cabeceras y el cliente de toda ruta de §11. Ninguna se marca
// obligatoria para huma: si falta una, el contrato responde 400 cabecera_requerida (no el 422 de
// la validación).
type CabecerasPortalHTTP struct {
	ClienteID       int64  `path:"cliente_id" minimum:"1" maximum:"999999999999999999" doc:"Id del cliente en Argos"`
	Empresa         string `header:"X-Iris-Empresa" maxLength:"20" doc:"Obligatoria: la empresa en que se atiende (fuera del alcance del token, 403 empresa_no_permitida; si falta, 400 cabecera_requerida)"`
	Sesion          string `header:"X-Argos-Portal-Sesion" maxLength:"80" doc:"Obligatoria: <documento|ip>:<id opaco de 8 a 64> (400 cabecera_requerida)"`
	Contacto        string `header:"X-Iris-Contacto" maxLength:"20" doc:"<contacto_id> o titular (obligatoria con contrato ≥ 0.12.1: 400 cabecera_iris_requerida)"`
	ContactoVersion string `header:"X-Iris-Contacto-Version" maxLength:"12" doc:"Con un <contacto_id>: su version en Argos (422 validacion si está mal formada)"`
}

// Cabeceras devuelve lo que necesita argos.SesionPortal.
func (c CabecerasPortalHTTP) Cabeceras() argos.CabecerasPortal {
	return argos.CabecerasPortal{ClienteID: c.ClienteID, Empresa: c.Empresa, Sesion: c.Sesion, Contacto: c.Contacto, ContactoVersion: c.ContactoVersion}
}

// ClavePortalHTTP: la Idempotency-Key obligatoria de los POST (§11.2).
type ClavePortalHTTP struct {
	IdempotencyKey string `header:"Idempotency-Key" maxLength:"255" doc:"Obligatoria: argos:portal:<operacion>:<id> (400 idempotency_key_requerida)"`
}

// ExigirClave: 400 idempotency_key_requerida sin la cabecera.
func (c ClavePortalHTTP) ExigirClave() error {
	if strings.TrimSpace(c.IdempotencyKey) == "" {
		return nucleo.NuevoProblema(http.StatusBadRequest, "idempotency_key_requerida", "Idempotency-Key requerida",
			"Todo POST del portal de Argos lleva Idempotency-Key")
	}
	return nil
}

// OperacionPortalArgos documenta una ruta de §11.
func OperacionPortalArgos(id, metodo, ruta, resumen string, permiso nucleo.Permiso, status int, errores ...int) huma.Operation {
	return huma.Operation{
		OperationID: id,
		Method:      metodo,
		Path:        RutaPortalArgos + ruta,
		Summary:     resumen,
		Description: "Contrato con Argos 0.9.0 §11 (portal del cliente). Solo el `token_portal` de un conector Argos (otra credencial, 403 " +
			"`solo_integracion_argos`), con el permiso `" + string(permiso) + "`. 409 `argos_apagado` con el conector apagado; 429 " +
			"`demasiadas_peticiones` (600 por minuto por conector, con `Retry-After`); 422 `empresa_no_coincide` / `cliente_no_existe`; " +
			"503 `argos_no_disponible` si Iris no puede confirmar el cliente con Argos (salvo al abrir un ticket). Con `X-Iris-Contacto: " +
			"<contacto_id>` (§11.10), la sesión es de un contacto a cargo del servicio: 403 `servicio_fuera_del_contacto` o `permiso_de_contacto`.",
		Tags:          []string{"portal-argos"},
		DefaultStatus: status,
		Errors: append([]int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict,
			http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusServiceUnavailable}, errores...),
		Security:   []map[string][]string{{"bearer": {}}},
		Extensions: map[string]any{"x-permiso": string(permiso)},
	}
}

// cuerpoConArchivos documenta un POST que acepta JSON o multipart/form-data (§11.6): el handler
// separa el JSON y los archivos (huma no valida el cuerpo: lo hace el caso de uso).
func cuerpoConArchivos(api huma.API, op *huma.Operation, tipo reflect.Type, nombre string) {
	esquema := api.OpenAPI().Components.Schemas.Schema(tipo, true, nombre)
	op.RequestBody = &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{
		"application/json": {Schema: esquema},
		"multipart/form-data": {Schema: &huma.Schema{Type: huma.TypeObject, Required: []string{"datos"}, Properties: map[string]*huma.Schema{
			"datos": {Type: huma.TypeString, Description: "El mismo JSON que en application/json"},
			"archivos": {Type: huma.TypeArray, MaxItems: ptr(MaxAdjuntosPortal), Description: "Hasta 3 archivos de 10 MB (JPEG, PNG, WebP o PDF; se comprueban por el contenido)",
				Items: &huma.Schema{Type: huma.TypeString, Format: "binary"}},
		}}},
	}}
	op.SkipValidateBody = true
	op.MaxBodyBytes = MaxCuerpoPortal
}

// SesionPortalArgos resuelve la sesión de la petición y deja el actor con el nombre de §11.1.
func SesionPortalArgos(ctx context.Context, p PortalArgos, c CabecerasPortalHTTP) (context.Context, argos.SesionPortal, error) {
	if p == nil {
		return ctx, argos.SesionPortal{}, nucleo.NoDisponible("El portal de Argos no está configurado en esta instancia")
	}
	s, err := p.SesionPortal(ctx, c.Cabeceras())
	if err != nil {
		return ctx, s, err
	}
	return contextoPortalArgos(ctx, s), s, nil
}

type entradaListaPortalArgos struct {
	CabecerasPortalHTTP
	Estado     string `query:"estado" maxLength:"80" doc:"Uno o varios separados por coma: nuevo, abierto, en_espera, resuelto, cerrado"`
	ServicioID int64  `query:"servicio_id" minimum:"0" doc:"Filtro por servicio (obligatorio con un contacto, §11.10)"`
	Cursor     string `query:"cursor" maxLength:"200"`
	Limit      int    `query:"limit" minimum:"1" maximum:"100" default:"20"`
}

type entradaTicketPortalArgosHTTP struct {
	CabecerasPortalHTTP
	TicketID int64 `path:"ticket_id" minimum:"1"`
}

type entradaMensajesPortalArgosHTTP struct {
	CabecerasPortalHTTP
	TicketID int64  `path:"ticket_id" minimum:"1"`
	Cursor   string `query:"cursor" maxLength:"200"`
	Limit    int    `query:"limit" minimum:"1" maximum:"100" default:"20"`
}

type entradaAdjuntoPortalArgosHTTP struct {
	CabecerasPortalHTTP
	TicketID  int64  `path:"ticket_id" minimum:"1"`
	AdjuntoID string `path:"adjunto_id" maxLength:"36"`
}

type salidaAdjuntoPortalArgos struct {
	ContentType        string `header:"Content-Type"`
	ContentDisposition string `header:"Content-Disposition"`
	Nosniff            string `header:"X-Content-Type-Options"`
	CSP                string `header:"Content-Security-Policy"`
	Body               []byte
}

type entradaPostPortalArgos struct {
	CabecerasPortalHTTP
	ClavePortalHTTP
	ContentType string `header:"Content-Type"`
	RawBody     []byte
}

type entradaPostTicketPortalArgos struct {
	CabecerasPortalHTTP
	ClavePortalHTTP
	TicketID    int64  `path:"ticket_id" minimum:"1"`
	ContentType string `header:"Content-Type"`
	RawBody     []byte
}

type entradaConfirmarPortalArgosHTTP struct {
	CabecerasPortalHTTP
	ClavePortalHTTP
	TicketID int64 `path:"ticket_id" minimum:"1"`
	Body     CuerpoConfirmarArgos
}

func (m *Modulo) registrarPortalArgos(api huma.API) {
	huma.Register(api, OperacionPortalArgos("categoriasPortalArgos", http.MethodGet, "/categorias", "Categorías que el cliente puede elegir",
		nucleo.PermPortalArgosVerTickets, http.StatusOK),
		func(ctx context.Context, in *CabecerasPortalHTTP) (*cuerpo[nucleo.Pagina[CategoriaPortal]], error) {
			ctx, s, err := SesionPortalArgos(ctx, m.cfg.PortalArgos, *in)
			if err != nil {
				return nil, err
			}
			return responder(m.CategoriasPortalArgos.Ejecutar(ctx, s))
		})

	lista := OperacionPortalArgos("ticketsPortalArgos", http.MethodGet, "/tickets", "Tickets del cliente",
		nucleo.PermPortalArgosVerTickets, http.StatusOK)
	lista.Description += " Sus tickets en la empresa, de cualquier origen, más recientes primero. Con una sesión `ip`, la vista reducida " +
		"(sin `asunto`). Con un contacto, `servicio_id` es obligatorio (422 `validacion`) y ve completos solo los que abrió él."
	huma.Register(api, lista, func(ctx context.Context, in *entradaListaPortalArgos) (*cuerpo[nucleo.Pagina[TicketPortalArgos]], error) {
		ctx, s, err := SesionPortalArgos(ctx, m.cfg.PortalArgos, in.CabecerasPortalHTTP)
		if err != nil {
			return nil, err
		}
		e := EntradaTicketsPortalArgos{Sesion: s, Cursor: in.Cursor, Limite: in.Limit}
		for _, v := range strings.Split(in.Estado, ",") {
			if v = strings.TrimSpace(v); v != "" {
				e.Estados = append(e.Estados, v)
			}
		}
		if in.ServicioID > 0 {
			e.ServicioID = &in.ServicioID
		}
		return responder(m.TicketsPortalArgos.Ejecutar(ctx, e))
	})

	ver := OperacionPortalArgos("verTicketPortalArgos", http.MethodGet, "/tickets/{ticket_id}", "Un ticket del cliente",
		nucleo.PermPortalArgosVerTickets, http.StatusOK, http.StatusNotFound)
	ver.Description += " De otro cliente, de otra empresa o inexistente: 404 `no_encontrado`. Sesión `ip`: 403 `sesion_insuficiente`."
	huma.Register(api, ver, func(ctx context.Context, in *entradaTicketPortalArgosHTTP) (*cuerpo[TicketPortalArgos], error) {
		ctx, s, err := SesionPortalArgos(ctx, m.cfg.PortalArgos, in.CabecerasPortalHTTP)
		if err != nil {
			return nil, err
		}
		return responder(m.VerTicketPortalArgos.Ejecutar(ctx, EntradaTicketPortalArgos{Sesion: s, TicketID: in.TicketID}))
	})

	msjs := OperacionPortalArgos("mensajesPortalArgos", http.MethodGet, "/tickets/{ticket_id}/mensajes", "Mensajes públicos del ticket",
		nucleo.PermPortalArgosVerTickets, http.StatusOK, http.StatusNotFound)
	msjs.Description += " Más antiguos primero; nunca notas internas. El agente aparece con su nombre de pila."
	huma.Register(api, msjs, func(ctx context.Context, in *entradaMensajesPortalArgosHTTP) (*cuerpo[nucleo.Pagina[MensajePortalArgos]], error) {
		ctx, s, err := SesionPortalArgos(ctx, m.cfg.PortalArgos, in.CabecerasPortalHTTP)
		if err != nil {
			return nil, err
		}
		return responder(m.MensajesPortalArgos.Ejecutar(ctx, EntradaTicketPortalArgos{Sesion: s, TicketID: in.TicketID, Cursor: in.Cursor, Limite: in.Limit}))
	})

	adj := OperacionPortalArgos("adjuntoPortalArgos", http.MethodGet, "/tickets/{ticket_id}/adjuntos/{adjunto_id}", "Descargar un adjunto",
		nucleo.PermPortalArgosVerTickets, http.StatusOK, http.StatusNotFound)
	adj.Description += " Solo si es de un mensaje público de ese ticket del cliente (si no, 404). `Content-Disposition: attachment` con el " +
		"nombre saneado en `filename*`, `X-Content-Type-Options: nosniff` y `Content-Security-Policy: sandbox`."
	huma.Register(api, adj, func(ctx context.Context, in *entradaAdjuntoPortalArgosHTTP) (*salidaAdjuntoPortalArgos, error) {
		id, err := uuid.Parse(in.AdjuntoID)
		if err != nil {
			return nil, nucleo.NoEncontrado("El adjunto no existe")
		}
		ctx, s, err := SesionPortalArgos(ctx, m.cfg.PortalArgos, in.CabecerasPortalHTTP)
		if err != nil {
			return nil, err
		}
		a, err := m.AdjuntoPortalArgos.Ejecutar(ctx, EntradaAdjuntoPortalArgos{Sesion: s, TicketID: in.TicketID, AdjuntoID: id})
		if err != nil {
			return nil, err
		}
		return &salidaAdjuntoPortalArgos{ContentType: a.Mime, ContentDisposition: "attachment; filename*=UTF-8''" + url.PathEscape(a.Nombre),
			Nosniff: "nosniff", CSP: "sandbox", Body: a.Datos}, nil
	})

	crear := OperacionPortalArgos("crearTicketPortalArgos", http.MethodPost, "/tickets", "Abrir un ticket",
		nucleo.PermPortalArgosCrearTicket, http.StatusCreated, http.StatusRequestEntityTooLarge)
	crear.Description += " §11.4: origen `portal_argos`, prioridad media, contacto y zona de la ficha de Argos; se liga a una falla general " +
		"abierta como cualquier otro. JSON o `multipart/form-data` (parte `datos` y hasta 3 `archivos`, §11.6; con sesión `ip`, 422 " +
		"`adjuntos_no_permitidos`). Topes compartidos con `/soporte` (429 `demasiados_tickets`). Si Argos no responde y Iris no conocía al " +
		"cliente, se crea igual y queda para conciliar. Emite `ticket.creado`."
	cuerpoConArchivos(api, &crear, reflect.TypeOf(CuerpoCrearTicketArgos{}), "CuerpoCrearTicketArgos")
	huma.Register(api, crear, func(ctx context.Context, in *entradaPostPortalArgos) (*cuerpo[TicketPortalArgos], error) {
		if err := in.ExigirClave(); err != nil {
			return nil, err
		}
		ctx, s, err := SesionPortalArgos(ctx, m.cfg.PortalArgos, in.CabecerasPortalHTTP)
		if err != nil {
			return nil, err
		}
		var c CuerpoCrearTicketArgos
		archivos, err := cuerpoPortal(in.ContentType, in.RawBody, &c, s.EsIP())
		if err != nil {
			return nil, err
		}
		return responder(m.CrearTicketPortalArgos.Ejecutar(ctx, EntradaCrearTicketArgos{Sesion: s, CuerpoCrearTicketArgos: c,
			Adjuntos: len(archivos), ClaveIdempotencia: in.IdempotencyKey, archivos: archivos}))
	})

	resp := OperacionPortalArgos("responderPortalArgos", http.MethodPost, "/tickets/{ticket_id}/responder", "Mensaje del cliente",
		nucleo.PermPortalArgosResponderTicket, http.StatusOK, http.StatusNotFound, http.StatusRequestEntityTooLarge)
	resp.Description += " §11.5: en_espera se reanuda y resuelto se reabre; cerrado, 422 `transicion_invalida`; `version` vieja, 409 " +
		"`conflicto_version`. JSON o `multipart/form-data` (§11.6). Tope 429 `demasiados_mensajes`. Emite `ticket.mensaje_cliente` (canal " +
		"`portal_argos`) y, si cambió, `ticket.estado_cambiado`."
	cuerpoConArchivos(api, &resp, reflect.TypeOf(CuerpoResponderArgos{}), "CuerpoResponderArgos")
	huma.Register(api, resp, func(ctx context.Context, in *entradaPostTicketPortalArgos) (*cuerpo[TicketYMensajeArgos], error) {
		if err := in.ExigirClave(); err != nil {
			return nil, err
		}
		ctx, s, err := SesionPortalArgos(ctx, m.cfg.PortalArgos, in.CabecerasPortalHTTP)
		if err != nil {
			return nil, err
		}
		var c CuerpoResponderArgos
		archivos, err := cuerpoPortal(in.ContentType, in.RawBody, &c, s.EsIP())
		if err != nil {
			return nil, err
		}
		return responder(m.ResponderPortalArgos.Ejecutar(ctx, EntradaResponderArgos{Sesion: s, TicketID: in.TicketID, CuerpoResponderArgos: c,
			Adjuntos: len(archivos), ClaveIdempotencia: in.IdempotencyKey, archivos: archivos}))
	})

	conf := OperacionPortalArgos("confirmarPortalArgos", http.MethodPost, "/tickets/{ticket_id}/confirmar-solucion", "El cliente confirma la solución",
		nucleo.PermPortalArgosConfirmarSolucion, http.StatusOK, http.StatusNotFound)
	conf.Description += " §11.5: solo resuelto → cerrado (si no, 422 `transicion_invalida`); `version` obligatoria. Emite `ticket.estado_cambiado`."
	huma.Register(api, conf, func(ctx context.Context, in *entradaConfirmarPortalArgosHTTP) (*cuerpo[TicketPortalArgos], error) {
		if err := in.ExigirClave(); err != nil {
			return nil, err
		}
		ctx, s, err := SesionPortalArgos(ctx, m.cfg.PortalArgos, in.CabecerasPortalHTTP)
		if err != nil {
			return nil, err
		}
		return responder(m.ConfirmarPortalArgos.Ejecutar(ctx, EntradaConfirmarArgos{Sesion: s, TicketID: in.TicketID, CuerpoConfirmarArgos: in.Body}))
	})
}
