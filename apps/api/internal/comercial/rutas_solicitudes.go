package comercial

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/soporte"
)

// Rutas de solicitudes de servicio del portal del cliente de Argos (contrato 0.9.0 §11.3 y §11.7).

type entradaSolicitudesHTTP struct {
	soporte.CabecerasPortalHTTP
	Estado string `query:"estado" maxLength:"120" doc:"Uno o varios estado_cliente separados por coma"`
	Cursor string `query:"cursor" maxLength:"200"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"20"`
}

type entradaSolicitudHTTP struct {
	soporte.CabecerasPortalHTTP
	SolicitudID int64 `path:"solicitud_id" minimum:"1"`
}

type entradaCrearSolicitudHTTP struct {
	soporte.CabecerasPortalHTTP
	soporte.ClavePortalHTTP
	Body CuerpoSolicitud
}

type cuerpoHTTP[T any] struct {
	Body T
}

func responderHTTP[T any](v T, err error) (*cuerpoHTTP[T], error) {
	if err != nil {
		return nil, err
	}
	return &cuerpoHTTP[T]{Body: v}, nil
}

func (m *Modulo) registrarSolicitudesPortal(api huma.API) {
	nota := " Solo el titular con sesión de documento: con `X-Iris-Contacto: <contacto_id>`, 403 `solo_titular`; con sesión `ip`, 403 `sesion_insuficiente`."
	lista := soporte.OperacionPortalArgos("solicitudesPortalArgos", http.MethodGet, "/solicitudes", "Solicitudes de servicio del cliente",
		nucleo.PermPortalArgosVerSolicitudes, http.StatusOK)
	lista.Description += " Más recientes primero; solo `estado_cliente`, nunca el vendedor, las actividades, la cotización ni el motivo." + nota
	huma.Register(api, lista, func(ctx context.Context, in *entradaSolicitudesHTTP) (*cuerpoHTTP[nucleo.Pagina[SolicitudServicio]], error) {
		ctx, s, err := soporte.SesionPortalArgos(ctx, m.cfg.PortalArgos, in.CabecerasPortalHTTP)
		if err != nil {
			return nil, err
		}
		e := EntradaSolicitudes{Sesion: s, Cursor: in.Cursor, Limite: in.Limit}
		for _, v := range strings.Split(in.Estado, ",") {
			if v = strings.TrimSpace(v); v != "" {
				e.Estados = append(e.Estados, v)
			}
		}
		return responderHTTP(m.ListarSolicitudes.Ejecutar(ctx, e))
	})

	ver := soporte.OperacionPortalArgos("verSolicitudPortalArgos", http.MethodGet, "/solicitudes/{solicitud_id}", "Una solicitud de servicio",
		nucleo.PermPortalArgosVerSolicitudes, http.StatusOK, http.StatusNotFound)
	ver.Description += " De otro cliente, de otra empresa o inexistente: 404 `no_encontrado`." + nota
	huma.Register(api, ver, func(ctx context.Context, in *entradaSolicitudHTTP) (*cuerpoHTTP[SolicitudServicio], error) {
		ctx, s, err := soporte.SesionPortalArgos(ctx, m.cfg.PortalArgos, in.CabecerasPortalHTTP)
		if err != nil {
			return nil, err
		}
		return responderHTTP(m.VerSolicitud.Ejecutar(ctx, EntradaVerSolicitud{Sesion: s, SolicitudID: in.SolicitudID}))
	})

	crear := soporte.OperacionPortalArgos("crearSolicitudPortalArgos", http.MethodPost, "/solicitudes", "Pedir un servicio",
		nucleo.PermPortalArgosSolicitarServicio, http.StatusCreated)
	crear.Description += " §11.7: llega a ventas como prospecto con origen `portal_argos`, con nombre, correo y teléfono de la ficha de Argos, " +
		"sin vendedor (o el de la zona). 409 `solicitud_abierta` (con `solicitud_id`) si ya hay una abierta del mismo tipo y servicio; 422 " +
		"`servicio_requerido` (cambio de plan o traslado sin servicio); 429 `demasiadas_solicitudes` (5 por día, con `Retry-After`). Emite " +
		"`prospecto.creado`." + nota
	huma.Register(api, crear, func(ctx context.Context, in *entradaCrearSolicitudHTTP) (*cuerpoHTTP[SolicitudServicio], error) {
		if err := in.ExigirClave(); err != nil {
			return nil, err
		}
		ctx, s, err := soporte.SesionPortalArgos(ctx, m.cfg.PortalArgos, in.CabecerasPortalHTTP)
		if err != nil {
			return nil, err
		}
		return responderHTTP(m.CrearSolicitud(ctx, EntradaCrearSolicitud{Sesion: s, CuerpoSolicitud: in.Body, ClaveIdempotencia: in.IdempotencyKey}))
	})
}
