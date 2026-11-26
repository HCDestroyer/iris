package campo

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	campobd "github.com/HCDestroyer/iris/apps/api/internal/campo/bd"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/argos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/conector"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/equipos"
	"github.com/HCDestroyer/iris/apps/api/internal/integracion/pendientes"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Equipo del cliente desde la orden (contrato argosisp.md 0.9.0 §12.3 y §12.4, pedido B12): el
// estado del equipo (en línea, señal) y el cambio de Wi-Fi. El técnico solo en sus órdenes en curso;
// con la orden cerrada, un agente. Nunca la clave Wi-Fi ni credenciales del equipo.

// EntradaEquipoOrden: la orden de la que se lee el equipo.
type EntradaEquipoOrden struct {
	ID int64 `path:"id" minimum:"1"`
}

// EntradaWifiOrden es la entrada de servicios.cambiar_wifi desde la orden.
type EntradaWifiOrden struct {
	ID    int64   `json:"-"`
	SSID  *string `json:"ssid,omitempty" minLength:"1" maxLength:"32" auditoria:"sensible" doc:"Nombre nuevo del Wi-Fi (1 a 32 caracteres)"`
	Clave *string `json:"clave,omitempty" minLength:"8" maxLength:"63" auditoria:"sensible" doc:"Clave nueva (8 a 63). Nunca se devuelve ni queda en Iris: viaja cifrada hasta Argos y se borra"`
	// Metodo: cómo se comprobó que quien pide es el titular (decisión del PM: varias formas). Hoy:
	// datos_titular (la verificación §7.5, verificacionId) y presencial (el técnico en sitio, en su orden
	// en curso). codigo_telefono y codigo_correo (código de un solo uso) llegan en otra PR: 422
	// metodo_no_disponible. llamada_verificada se acepta como el nombre anterior de datos_titular.
	Metodo string `json:"metodo,omitempty" enum:"datos_titular,presencial,codigo_telefono,codigo_correo,llamada_verificada" doc:"Cómo se comprobó que quien pide es el titular. datos_titular: la verificación §7.5 (verificacionId); presencial: solo el técnico en sitio, en su orden en curso. codigo_telefono y codigo_correo aún no están disponibles (422 metodo_no_disponible). Sin metodo: presencial para el técnico, datos_titular desde el panel. llamada_verificada: nombre anterior de datos_titular"`
	// VerificacionID: un agente manda la verificación §7.5 del titular (de menos de 10 min, sin usar).
	VerificacionID *int64 `json:"verificacionId,omitempty" minimum:"1" doc:"Desde el panel, obligatoria: la verificación de identidad del titular (desafío de Argos, §7.5) de menos de 10 minutos y de un solo uso. Sin ella, 422 verificacion_requerida"`
}

// errorEquipo traduce lo que respondió Argos al leer el equipo (§12.3): 409 equipo_sin_gestion; lo
// demás (503, caída) es «no se pudo leer» con «Reintentar», sin reintentos solos.
func errorEquipo(err error) error {
	var er *conector.ErrorRemoto
	if errors.As(err, &er) && er.Status == http.StatusConflict && er.Codigo == "equipo_sin_gestion" {
		return nucleo.Conflicto("equipo_sin_gestion", "Argos no gestiona este equipo (por ejemplo, wireless sin TR-069)")
	}
	var p *nucleo.Problema
	if errors.As(err, &p) {
		return p
	}
	return nucleo.NuevoProblema(http.StatusServiceUnavailable, "equipo_sin_respuesta", "No se pudo leer el equipo",
		"Argos, SmartOLT o el ACS no respondieron a tiempo. Reintenta en un momento")
}

// ordenConEquipo toma la orden (lectura o con candado) y exige el permiso, el alcance y el servicio.
func ordenConEquipo(ctx context.Context, t *auditoria.Tx, q *campobd.Queries, id int64, permiso nucleo.Permiso) (campobd.CampoOrden, error) {
	t.FijarEntidad("orden", id)
	f, err := q.OrdenPorID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, noEncontrada()
	}
	if err != nil {
		return f, err
	}
	if err := verificarAcceso(ctx, permiso, f); err != nil {
		return f, err
	}
	t.FijarEmpresa(deref(f.EmpresaID))
	if err := accesoEquipo(ctx, f, permiso); err != nil {
		return f, err
	}
	if f.ServicioID == nil || f.ClienteID == nil {
		return f, nucleo.ReglaNegocio("orden_sin_servicio", "La orden no tiene el cliente y el servicio de Argos")
	}
	return f, nil
}

func (m *Modulo) nuevoAutorizarEquipo() *auditoria.Caso[EntradaEquipoOrden, int64] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEquipoOrden, int64]{
		Accion: "servicios.ver_equipo", Permiso: nucleo.PermServiciosVerEquipo,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEquipoOrden) (int64, error) {
			f, err := ordenConEquipo(ctx, t, campobd.New(t), in.ID, nucleo.PermServiciosVerEquipo)
			if err != nil {
				return 0, err
			}
			// 0.14.3 §4.3: solo con la marca estado_equipo de la empresa.
			if m.cfg.ArgosEquipos.Capacidad != nil {
				if ok, err := m.cfg.ArgosEquipos.Capacidad(ctx, t, f.EmpresaID, argos.Funcion(argos.FuncionEstadoEquipo)); err != nil {
					return 0, err
				} else if !ok {
					return 0, nucleo.ReglaNegocio(argos.CodigoCapacidadNoDisponible, "El estado del equipo no está disponible para esta empresa")
				}
			}
			return *f.ServicioID, nil
		},
	})
}

// VerEquipo lee el estado del equipo del servicio de la orden (la lectura queda auditada; Argos se
// llama fuera de la transacción).
func (m *Modulo) VerEquipo(ctx context.Context, in EntradaEquipoOrden) (argos.EstadoEquipo, error) {
	servicio, err := m.autorizarEquipo.Ejecutar(ctx, in)
	if err != nil {
		return argos.EstadoEquipo{}, err
	}
	if m.cfg.ArgosEquipos.LeerEquipo == nil {
		return argos.EstadoEquipo{}, nucleo.ReglaNegocio("equipo_no_disponible", "Iris no tiene conexión con Argos para leer el equipo")
	}
	a := contexto.ActorDe(ctx)
	r, err := m.cfg.ArgosEquipos.LeerEquipo(ctx, servicio, pendientes.ActorUsuario(a.ID), false)
	if err != nil {
		return r, errorEquipo(err)
	}
	return r, nil
}

func (m *Modulo) nuevoCambiarWifi() *auditoria.Caso[EntradaWifiOrden, equipos.CambioWifi] {
	return auditoria.Nuevo(auditoria.Caso[EntradaWifiOrden, equipos.CambioWifi]{
		Accion: "servicios.cambiar_wifi", Permiso: nucleo.PermServiciosCambiarWifi,
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaWifiOrden) (equipos.CambioWifi, error) {
			if err := sinNulos(in); err != nil {
				return equipos.CambioWifi{}, err
			}
			q := campobd.New(t)
			f, err := ordenConEquipo(ctx, t, q, in.ID, nucleo.PermServiciosCambiarWifi)
			if err != nil {
				return equipos.CambioWifi{}, err
			}
			a := contexto.ActorDe(ctx)
			if !a.EsUsuario() {
				return equipos.CambioWifi{}, nucleo.Prohibido("El cambio de Wi-Fi lo pide una persona (X-Iris-Actor)")
			}
			if m.cfg.ArgosEquipos.Capacidad == nil {
				return equipos.CambioWifi{}, nucleo.ReglaNegocio("wifi_no_disponible", "Iris no tiene conexión con Argos 0.9.0 para cambiar el Wi-Fi")
			}
			if ok, err := m.cfg.ArgosEquipos.Capacidad(ctx, t, f.EmpresaID, argos.Funcion(argos.FuncionWifi)); err != nil {
				return equipos.CambioWifi{}, err
			} else if !ok {
				return equipos.CambioWifi{}, nucleo.ReglaNegocio(argos.CodigoCapacidadNoDisponible, "Cambiar el Wi-Fi no está disponible para esta empresa")
			}
			pedido := in.Metodo
			if pedido == "" {
				pedido = equipos.MetodoDatosTitular
				if esTecnico(ctx) {
					pedido = equipos.MetodoPresencial
				}
			}
			// A Argos (§12.4, «Persona»): presencial, o llamada_verificada para la verificación §7.5 que
			// confirmó una persona de Iris.
			metodo := equipos.MetodoLlamadaVerificada
			switch pedido {
			case equipos.MetodoCodigoTelefono, equipos.MetodoCodigoCorreo:
				return equipos.CambioWifi{}, nucleo.ReglaNegocio("metodo_no_disponible",
					"La verificación por código al teléfono o al correo del titular aún no está disponible: usa datos_titular o presencial")
			case equipos.MetodoPresencial:
				// Solo el técnico en sitio, en su orden en curso.
				if !esTecnico(ctx) {
					return equipos.CambioWifi{}, campoInvalido("metodo", "presencial es solo del técnico en sitio; desde el panel usa datos_titular", "enum")
				}
				if f.Estado != EstadoEnSitio {
					return equipos.CambioWifi{}, transicionInvalida("cambiar el Wi-Fi de", f.Estado)
				}
				metodo = equipos.MetodoPresencial
			}
			var verificacion *int64
			if metodo != equipos.MetodoPresencial {
				verificacion = in.VerificacionID
			}
			ahora := m.ahora()
			return equipos.Encolar(ctx, t, m.cfg.Titular.Cifrador, equipos.Pedido{
				EmpresaID: f.EmpresaID, ServicioID: *f.ServicioID, ClienteID: *f.ClienteID, OrigenTipo: equipos.OrigenOrden,
				OrigenID: f.ID, Actor: pendientes.ActorUsuario(a.ID), UsuarioID: &a.ID, Metodo: metodo, VerificadoEn: ahora,
				SSID: in.SSID, Clave: in.Clave, Ahora: ahora, Zona: m.cfg.ZonaHoraria, VerificacionID: verificacion,
			}, m.cfg.Titular.Encolar)
		},
	})
}

func (m *Modulo) nuevoCambiosWifi() *auditoria.Caso[EntradaEquipoOrden, []equipos.CambioWifi] {
	return auditoria.Nuevo(auditoria.Caso[EntradaEquipoOrden, []equipos.CambioWifi]{
		Accion: "servicios.ver_cambios_wifi", Acceso: auditoria.Autenticado, AuditarExito: soloLecturas[[]equipos.CambioWifi],
		Logica: func(ctx context.Context, t *auditoria.Tx, in EntradaEquipoOrden) ([]equipos.CambioWifi, error) {
			q := campobd.New(t)
			t.FijarEntidad("orden", in.ID)
			f, err := q.OrdenPorID(ctx, in.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, noEncontrada()
			}
			if err != nil {
				return nil, err
			}
			if err := verificarAcceso(ctx, nucleo.PermOrdenesVer, f); err != nil {
				return nil, err
			}
			return equipos.DeOrigen(ctx, t, equipos.OrigenOrden, f.ID)
		},
	})
}

// registrarEquipos publica la activación, el equipo y el Wi-Fi de la orden (0.9.0 §12).
func (m *Modulo) registrarEquipos(api huma.API) {
	registrarAccion(api, accion("activarServicioOrden", "activar-servicio", "Activar el servicio (aprovisionar en Argos)",
		"FTTH (ONT por su serie, con la planta opcional) o wireless (CPE o radio por su MAC; AP y sector opcionales), con el Wi-Fi inicial "+
			"opcional. Argos aprovisiona y activa; el resultado llega por webhook (o Iris consulta a los 10 min): GET /ordenes/{id}/activacion. "+
			"El técnico, en sitio y en su orden (también sin señal: orden.activar_servicio); un agente desde el panel, también con la orden cerrada. "+
			"Un intento vivo por orden y por servicio: 409 `activacion_en_curso`, `activacion_lista` o `activacion_al_cerrar` (el cierre ya la mandó). "+
			"422 `activacion_no_disponible` (la empresa activa al cerrar o Argos no puede aprovisionar ese equipo), `orden_no_es_instalacion`, "+
			"`orden_sin_servicio` o la validación del equipo y el Wi-Fi. La clave Wi-Fi nunca se guarda: viaja cifrada hasta Argos y se borra. "+
			"Emite `orden.activacion_pedida`.",
		nucleo.PermOrdenesActivarServicio), m.Activacion.Activar.Ejecutar, func(e *EntradaActivarServicio, id int64) { e.ID = id })

	ver := operacion("verActivacionOrden", http.MethodGet, "/api/v1/ordenes/{id}/activacion", "Estado de la activación de la orden",
		nucleo.PermOrdenesVer, http.StatusOK, http.StatusNotFound)
	ver.Description = "Permiso: `ordenes.ver` u `ordenes.ver_propias`. El modo efectivo (al_cerrar si Argos no puede aprovisionar en la empresa), " +
		"si se puede pedir «Activar servicio» y por qué no, y cada intento con su estado, causa y pasos. Nunca el Wi-Fi."
	huma.Register(api, ver, func(ctx context.Context, in *EntradaVerOrden) (*cuerpo[EstadoActivacion], error) {
		return responder(m.Activacion.Ver.Ejecutar(ctx, *in))
	})

	eq := operacion("verEquipoOrden", http.MethodGet, "/api/v1/ordenes/{id}/equipo", "Estado del equipo del cliente",
		nucleo.PermServiciosVerEquipo, http.StatusOK, http.StatusNotFound, http.StatusConflict, http.StatusServiceUnavailable)
	eq.Description += " En línea, señal, serie, MAC, modelo y firmware leídos de Argos (SmartOLT o el ACS) en el momento. El técnico solo en sus " +
		"órdenes en curso. 409 `equipo_sin_gestion`; 503 `equipo_sin_respuesta` (Iris no reintenta solo: «Reintentar»). Nunca la clave Wi-Fi."
	huma.Register(api, eq, func(ctx context.Context, in *EntradaEquipoOrden) (*cuerpo[argos.EstadoEquipo], error) {
		return responder(m.VerEquipo(ctx, *in))
	})

	wifi := operacion("cambiarWifiOrden", http.MethodPost, "/api/v1/ordenes/{id}/wifi", "Cambiar el Wi-Fi del cliente",
		nucleo.PermServiciosCambiarWifi, http.StatusAccepted, http.StatusNotFound, http.StatusTooManyRequests)
	wifi.Description += " Nombre o clave nuevos (al menos uno). Argos lo aplica en el equipo y avisa por webhook; el estado queda en " +
		"GET /ordenes/{id}/wifi. `metodo`: `presencial` (solo el técnico en sitio, en su orden en curso) o `datos_titular` (un agente " +
		"con la verificación §7.5 del titular en `verificacionId`); `codigo_telefono` y `codigo_correo` aún no: 422 `metodo_no_disponible`. " +
		"429 `demasiados_cambios_wifi` (3 por servicio al día). La clave nunca se guarda ni se devuelve. Emite `wifi.cambio_pedido`."
	huma.Register(api, wifi, func(ctx context.Context, in *conID[EntradaWifiOrden]) (*cuerpo[equipos.CambioWifi], error) {
		e := in.Body
		e.ID = in.ID
		return responder(m.CambiarWifi.Ejecutar(ctx, e))
	})

	cambios := operacion("cambiosWifiOrden", http.MethodGet, "/api/v1/ordenes/{id}/wifi", "Cambios de Wi-Fi pedidos desde la orden",
		nucleo.PermOrdenesVer, http.StatusOK, http.StatusNotFound)
	huma.Register(api, cambios, func(ctx context.Context, in *EntradaEquipoOrden) (*cuerpo[[]equipos.CambioWifi], error) {
		return responder(m.CambiosWifi.Ejecutar(ctx, *in))
	})
}
