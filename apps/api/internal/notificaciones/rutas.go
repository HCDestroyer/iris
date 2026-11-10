package notificaciones

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Handlers delgados: leen la petición, llaman al caso de uso y arman la respuesta.

const etiqueta = "notificaciones"

var seguridad = []map[string][]string{{"sesion": {}}, {"bearer": {}}}

func operacion(id, metodo, ruta, resumen, detalle string, status int, errores ...int) huma.Operation {
	return huma.Operation{
		OperationID: id, Method: metodo, Path: ruta, Summary: resumen,
		Description: "Una persona con sesión de la app o token propio (un usuario de integración: 403). " + detalle +
			" Contrato: `.claude/integration/contratos/push.md`.",
		Tags: []string{etiqueta}, DefaultStatus: status, Security: seguridad,
		Errors: append([]int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity}, errores...),
	}
}

type cuerpo[T any] struct {
	Body T
}

type registroHTTP struct {
	Status int
	Body   Dispositivo
}

type entradaRegistroHTTP struct {
	nucleo.ConIdempotencia
	Body EntradaRegistrarDispositivo
}

type entradaQuitarHTTP struct {
	nucleo.ConIdempotencia
	ID int64 `path:"id" minimum:"1"`
}

type entradaPreferenciasHTTP struct {
	nucleo.ConIdempotencia
	Body EntradaPreferencias
}

type entradaLeerAvisoHTTP struct {
	nucleo.ConIdempotencia
	ID int64 `path:"id" minimum:"1"`
}

type entradaLeerTodosHTTP struct {
	nucleo.ConIdempotencia
}

func operacionAviso(id, metodo, ruta, resumen, detalle string, status int, errores ...int) huma.Operation {
	op := operacion(id, metodo, ruta, resumen, detalle, status, errores...)
	op.Description = "Una persona con sesión del panel o token propio (un usuario de integración: 403). " + detalle +
		" Solo los tuyos de la empresa activa. Contrato: `.claude/integration/contratos/conversaciones.md` §16.9."
	return op
}

// Registrar publica las rutas en la API.
func (m *Modulo) Registrar(api huma.API) {
	m.registrarAvisos(api)

	reg := operacion("registrarDispositivo", http.MethodPost, "/api/v1/dispositivos", "Registrar el teléfono para recibir push",
		"Idempotente por token: 201 la primera vez, 200 después (renueva la versión de la app y la sesión). El dispositivo queda en la "+
			"empresa activa y atado a la sesión o al token de la petición: al cerrar sesión se quita solo. Si el token era de otro "+
			"usuario, se le quita. Nunca devuelve el token. Emite `dispositivo.registrado`.", http.StatusCreated)
	reg.Responses = map[string]*huma.Response{"200": {Description: "Ya estaba registrado: renovado"}}
	huma.Register(api, reg, func(ctx context.Context, in *entradaRegistroHTTP) (*registroHTTP, error) {
		d, err := m.RegistrarDispositivo.Ejecutar(ctx, in.Body)
		if err != nil {
			return nil, err
		}
		s := http.StatusOK
		if d.Nuevo {
			s = http.StatusCreated
		}
		return &registroHTTP{Status: s, Body: d.Dispositivo}, nil
	})

	huma.Register(api, operacion("listarDispositivos", http.MethodGet, "/api/v1/dispositivos", "Mis teléfonos registrados",
		"Solo los tuyos, en la empresa activa.", http.StatusOK),
		func(ctx context.Context, in *EntradaListarDispositivos) (*cuerpo[nucleo.Pagina[Dispositivo]], error) {
			p, err := m.ListarDispositivos.Ejecutar(ctx, *in)
			if err != nil {
				return nil, err
			}
			return &cuerpo[nucleo.Pagina[Dispositivo]]{Body: p}, nil
		})

	huma.Register(api, operacion("quitarDispositivo", http.MethodPost, "/api/v1/dispositivos/{id}/quitar", "Dejar de recibir push en un teléfono",
		"Uno tuyo de la empresa activa; si no existe o no es tuyo, 404. Emite `dispositivo.quitado` (motivo `usuario`).", http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *entradaQuitarHTTP) (*cuerpo[Dispositivo], error) {
			d, err := m.QuitarDispositivo.Ejecutar(ctx, EntradaQuitarDispositivo{ID: in.ID})
			if err != nil {
				return nil, err
			}
			return &cuerpo[Dispositivo]{Body: d}, nil
		})

	huma.Register(api, operacion("verPreferenciasNotificaciones", http.MethodGet, "/api/v1/yo/notificaciones", "Mis preferencias de notificaciones",
		"De la cuenta (valen en cualquier teléfono). Sin guardar: los valores por defecto con `version: 0`. `push` dice si el servidor "+
			"puede mandar push («Notificaciones push: sin configurar»).", http.StatusOK),
		func(ctx context.Context, _ *struct{}) (*cuerpo[Preferencias], error) {
			p, err := m.VerPreferencias.Ejecutar(ctx, struct{}{})
			if err != nil {
				return nil, err
			}
			return &cuerpo[Preferencias]{Body: p}, nil
		})

	huma.Register(api, operacion("guardarPreferenciasNotificaciones", http.MethodPost, "/api/v1/yo/notificaciones", "Guardar mis preferencias de notificaciones",
		"Reemplaza todo. `version` la que leíste (0 si nunca se guardaron): distinta, 409 `conflicto_version`. `desde` y `hasta` en "+
			"HH:MM de `zonaHoraria`; iguales, 422 `horario_invalido`. Emite `notificaciones.preferencias_cambiadas`.", http.StatusOK, http.StatusConflict),
		func(ctx context.Context, in *entradaPreferenciasHTTP) (*cuerpo[Preferencias], error) {
			p, err := m.GuardarPreferencias.Ejecutar(ctx, in.Body)
			if err != nil {
				return nil, err
			}
			return &cuerpo[Preferencias]{Body: p}, nil
		})
}

// registrarAvisos publica la campana del panel: avisos, su resumen y marcarlos leídos.
func (m *Modulo) registrarAvisos(api huma.API) {
	huma.Register(api, operacionAviso("listarAvisos", http.MethodGet, "/api/v1/yo/avisos", "Mis avisos (la campana del panel)",
		"Del más nuevo al más viejo, con paginación por cursor. `soloNoLeidos=true` para los pendientes. Hoy: `asistente_tope` (el asistente de IA "+
			"llegó al 80 % o al 100 % de su tope del mes, para quienes tienen `mensajeria.asistente_configurar`).", http.StatusOK),
		func(ctx context.Context, in *EntradaListarAvisos) (*cuerpo[nucleo.Pagina[AvisoPanel]], error) {
			p, err := m.ListarAvisos.Ejecutar(ctx, *in)
			if err != nil {
				return nil, err
			}
			return &cuerpo[nucleo.Pagina[AvisoPanel]]{Body: p}, nil
		})

	huma.Register(api, operacionAviso("resumenAvisos", http.MethodGet, "/api/v1/yo/avisos/resumen", "Cuántos avisos no he leído",
		"Para el punto de la campana.", http.StatusOK),
		func(ctx context.Context, _ *struct{}) (*cuerpo[ResumenAvisos], error) {
			r, err := m.ResumenAvisos.Ejecutar(ctx, struct{}{})
			if err != nil {
				return nil, err
			}
			return &cuerpo[ResumenAvisos]{Body: r}, nil
		})

	huma.Register(api, operacionAviso("leerAviso", http.MethodPost, "/api/v1/yo/avisos/{id}/leer", "Marcar un aviso como leído",
		"Repetirlo no cambia `leidoEn`. 404 si no es tuyo o no existe. No emite evento (es el estado de tu campana).", http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *entradaLeerAvisoHTTP) (*cuerpo[AvisoPanel], error) {
			a, err := m.LeerAviso.Ejecutar(ctx, EntradaLeerAviso{ID: in.ID})
			if err != nil {
				return nil, err
			}
			return &cuerpo[AvisoPanel]{Body: a}, nil
		})

	huma.Register(api, operacionAviso("leerTodosLosAvisos", http.MethodPost, "/api/v1/yo/avisos/leer-todos", "Marcar todos mis avisos como leídos",
		"Devuelve cuántos marcó. No emite evento.", http.StatusOK),
		func(ctx context.Context, _ *entradaLeerTodosHTTP) (*cuerpo[AvisosLeidos], error) {
			r, err := m.LeerAvisos.Ejecutar(ctx, struct{}{})
			if err != nil {
				return nil, err
			}
			return &cuerpo[AvisosLeidos]{Body: r}, nil
		})
}
