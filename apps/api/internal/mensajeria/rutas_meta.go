package mensajeria

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo/contexto"
)

// Rutas de la conexión con Meta, el QR (coexistencia) y el préstamo de números
// (contratos/conversaciones.md §15).

// casosMeta reúne los casos de §15.
type casosMeta struct {
	VerConexion        *auditoria.Caso[struct{}, ConexionMeta]
	ActualizarConexion *auditoria.Caso[EntradaConexionMeta, ConexionMeta]
	ProbarConexion     *ProbarConexionMeta
	Registro           *auditoria.Caso[struct{}, RegistroQR]
	CrearQR            *CrearCanalQR
	Sincronizar        *Sincronizar
	RecibirApp         *auditoria.Caso[EntradaMetaApp, MetaRecibido]
	VerificarApp       *auditoria.Caso[EntradaVerificarMetaApp, string]

	ListarPrestamos    *auditoria.Caso[EntradaCanalPrestamos, ListaPrestamos]
	OpcionesPrestamo   *auditoria.Caso[EntradaCanalPrestamos, OpcionesPrestamo]
	GuardarPrestamo    *auditoria.Caso[EntradaPrestamo, Prestamo]
	RevocarPrestamo    *auditoria.Caso[EntradaRevocarPrestamo, Prestamo]
	ConfigurarPrestamo *auditoria.Caso[EntradaConfigurarPrestamo, Canal]
	PasarEmpresa       *auditoria.Caso[EntradaPasarEmpresa, ConversacionCliente]
	EmpresasDestino    *auditoria.Caso[EntradaEmpresasDestino, ListaEmpresasDestino]
}

func nuevosCasosMeta(m *Modulo) casosMeta {
	return casosMeta{
		VerConexion: nuevoVerConexionMeta(m), ActualizarConexion: nuevoActualizarConexionMeta(m), ProbarConexion: nuevoProbarConexionMeta(m),
		Registro: nuevoRegistroQR(m), CrearQR: nuevoCrearCanalQR(m), Sincronizar: nuevoSincronizar(m),
		RecibirApp: nuevoRecibirMetaApp(m), VerificarApp: nuevoVerificarMetaApp(m),
		ListarPrestamos: nuevoListarPrestamos(m), OpcionesPrestamo: nuevoOpcionesPrestamo(m), GuardarPrestamo: nuevoGuardarPrestamo(m),
		RevocarPrestamo: nuevoRevocarPrestamo(m), ConfigurarPrestamo: nuevoConfigurarPrestamo(m), PasarEmpresa: nuevoPasarEmpresa(m),
		EmpresasDestino: nuevoEmpresasDestino(),
	}
}

type soloIdempotenciaHTTP struct {
	nucleo.ConIdempotencia
}

type salidaCanalQR struct {
	Status int
	Body   Canal
}

type entradaRevocarHTTP struct {
	nucleo.ConIdempotencia
	ID      int64 `path:"id" minimum:"1"`
	Empresa int64 `path:"empresa" minimum:"1"`
	Body    struct {
		Version int32  `json:"version" minimum:"1"`
		Motivo  string `json:"motivo" minLength:"5" maxLength:"300" doc:"La confirmación: por qué deja de compartirse"`
	}
}

type entradaSincronizarHTTP struct {
	nucleo.ConIdempotencia
	ID   int64 `path:"id" minimum:"1"`
	Body struct {
		Contactos bool `json:"contactos,omitempty" doc:"Pedir los contactos del teléfono (smb_app_state_sync)"`
		Mensajes  bool `json:"mensajes,omitempty" doc:"Pedir el historial de 180 días (history)"`
	}
}

type entradaPrestamoHTTP struct {
	nucleo.ConIdempotencia
	ID   int64 `path:"id" minimum:"1"`
	Body struct {
		EmpresaID int64    `json:"empresaId" minimum:"1"`
		Usos      []string `json:"usos" minItems:"1" maxItems:"4" uniqueItems:"true" doc:"avisos, soporte, cobros, ventas: para esa empresa sí limitan"`
		ColaID    *int64   `json:"colaId,omitempty" minimum:"1" nullable:"true" doc:"Cola de esa empresa donde caen sus conversaciones (null = sin cola)"`
		Version   int32    `json:"version,omitempty" minimum:"0" doc:"La del préstamo si ya existe"`
	}
}

type entradaConfigurarPrestamoHTTP struct {
	nucleo.ConIdempotencia
	ID   int64 `path:"id" minimum:"1"`
	Body struct {
		Version       int32 `json:"version" minimum:"1" doc:"Canal.prestamo.version"`
		AvisosEmpresa bool  `json:"avisosEmpresa" doc:"Número de avisos de tu empresa (exige el uso avisos)"`
	}
}

type entradaMetaAppHTTP struct {
	Firma   string `header:"X-Hub-Signature-256" doc:"sha256=<hex(HMAC-SHA256(app_secret, cuerpo))>"`
	RawBody []byte
}

type entradaVerificarAppHTTP struct {
	Modo      string `query:"hub.mode"`
	Token     string `query:"hub.verify_token"`
	Challenge string `query:"hub.challenge" maxLength:"256"`
}

func (m *Modulo) registrarMeta(api huma.API) {
	k := m.Meta
	conexion, configurar, canalesConf := nucleo.PermMensajeriaConexionMeta, nucleo.PermMensajeriaConfigurar, nucleo.PermCanalesConfigurar

	// Conexión con Meta (§15.1).
	verC := op("verConexionMeta", http.MethodGet, "/conexion-meta", "Conexión con Meta de la instalación (sin secretos)", conexion,
		http.StatusOK, http.StatusNotFound)
	verC.Description += " Una sola para todo el Iris: `propia`, `compartida`, `saas` o `sin_configurar`. Solo personas con sesión del panel (contratos/conversaciones.md §15.1)."
	huma.Register(api, verC, func(ctx context.Context, _ *struct{}) (*cuerpo[ConexionMeta], error) {
		return responder(k.VerConexion.Ejecutar(ctx, struct{}{}))
	})
	actC := op("actualizarConexionMeta", http.MethodPost, "/conexion-meta/actualizar", "Guardar la conexión con Meta", conexion,
		http.StatusOK, http.StatusNotFound, http.StatusConflict)
	actC.Description += " `version` 0 la primera vez. App secret y clave de enlace son de solo escritura (vacío = no cambia). El verify token del webhook de la app " +
		"se genera la primera vez (o con `generarVerifyToken`) y se devuelve **una vez** en `verifyTokenGenerado` (la respuesta no se guarda con la Idempotency-Key). " +
		"422 `conexion_gestionada` en modo saas. Evento `meta.conexion_cambiada`."
	huma.Register(api, actC, func(ctx context.Context, in *cuerpoHTTP[EntradaConexionMeta]) (*cuerpo[ConexionMeta], error) {
		c, err := k.ActualizarConexion.Ejecutar(ctx, in.Body)
		if err == nil && c.VerifyTokenGenerado != nil {
			nucleo.NoRepetible(ctx)
		}
		return responder(c, err)
	})
	probC := op("probarConexionMeta", http.MethodPost, "/conexion-meta/probar", "Probar la conexión con Meta", conexion,
		http.StatusOK, http.StatusNotFound, http.StatusConflict)
	probC.Description += " Valida el app ID y el app secret con la Graph API (`GET /{app_id}` con el token de la app) y revisa el webhook de la app; " +
		"el ID de configuración y la aprobación como Tech Provider quedan `sin_verificar`. 422 `conexion_meta_sin_configurar`. Evento `meta.conexion_probada`."
	huma.Register(api, probC, func(ctx context.Context, _ *soloIdempotenciaHTTP) (*cuerpo[ConexionMeta], error) {
		return responder(k.ProbarConexion.Ejecutar(ctx))
	})
	reg := op("registroQRMeta", http.MethodGet, "/conexion-meta/registro", "Datos para abrir el registro con QR de Meta (sin secretos)", configurar,
		http.StatusOK, http.StatusNotFound)
	reg.Description += " `appId`, `configId`, `graphVersion`, `featureType` y `sessionInfoVersion` para `FB.login` (Embedded Signup con coexistencia, §15.2)."
	huma.Register(api, reg, func(ctx context.Context, _ *struct{}) (*cuerpo[RegistroQR], error) {
		return responder(k.Registro.Ejecutar(ctx, struct{}{}))
	})

	// Número por QR (§15.2).
	qr := op("crearCanalQR", http.MethodPost, "/canales/qr", "Conectar un número de WhatsApp Business con QR (coexistencia)", configurar,
		http.StatusCreated, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway)
	qr.Description += " Llamar en cuanto llega el `code` de FB.login (vive ~30 s). Iris lo canjea una sola vez, lee el número, suscribe la app a la WABA " +
		"(nunca `/register`) y guarda el número con `tipoConexion: qr`. 200 si reconecta un número QR de la empresa con ese phone number ID. " +
		"422 `codigo_vencido`, `app_no_aprobada`, `numero_no_elegible`, `servicio_central_no_disponible`, `conexion_meta_sin_configurar`; 502 `error_meta`; 409 `canal_existente`."
	qr.Responses = map[string]*huma.Response{"200": {Description: "Reconectado: el mismo número con un token nuevo"}}
	huma.Register(api, qr, func(ctx context.Context, in *cuerpoHTTP[EntradaCanalQR]) (*salidaCanalQR, error) {
		r, err := k.CrearQR.Ejecutar(ctx, in.Body)
		if err != nil {
			return nil, err
		}
		s := &salidaCanalQR{Status: http.StatusOK, Body: r.Canal}
		if r.Creado {
			s.Status = http.StatusCreated
		}
		return s, nil
	})
	reenvioConCuerpo(api, qr.OperationID)
	sinc := op("sincronizarCanalQR", http.MethodPost, "/canales/{id}/sincronizar", "Traer contactos e historial de un número QR", configurar,
		http.StatusOK, http.StatusNotFound, http.StatusBadGateway)
	sinc.Description += " Una vez cada uno y dentro de 24 h desde la conexión (`smb_app_data`). 422 `no_es_coexistencia`, `sincronizacion_ya_pedida`, " +
		"`sincronizacion_vencida`; 502 `error_meta` si Meta rechazó todo lo pedido (queda `fallido` y se puede pedir otra vez)."
	huma.Register(api, sinc, func(ctx context.Context, in *entradaSincronizarHTTP) (*cuerpo[Canal], error) {
		return responder(k.Sincronizar.Ejecutar(ctx, EntradaSincronizar{ID: in.ID, Contactos: in.Body.Contactos, Mensajes: in.Body.Mensajes}))
	})

	// Préstamos (§15.3).
	huma.Register(api, op("listarPrestamosCanal", http.MethodGet, "/canales/{id}/prestamos", "Empresas con las que se comparte un número", canalesConf,
		http.StatusOK, http.StatusNotFound), func(ctx context.Context, in *idHTTP) (*cuerpo[ListaPrestamos], error) {
		return responder(k.ListarPrestamos.Ejecutar(ctx, EntradaCanalPrestamos{ID: in.ID}))
	})
	huma.Register(api, op("opcionesPrestamoCanal", http.MethodGet, "/canales/{id}/prestamos/opciones", "Empresas y colas a las que se puede prestar",
		canalesConf, http.StatusOK, http.StatusNotFound), func(ctx context.Context, in *idHTTP) (*cuerpo[OpcionesPrestamo], error) {
		return responder(k.OpcionesPrestamo.Ejecutar(ctx, EntradaCanalPrestamos{ID: in.ID}))
	})
	prest := op("prestarCanal", http.MethodPost, "/canales/{id}/prestamos", "Compartir un número con otra empresa (o cambiar el préstamo)", canalesConf,
		http.StatusOK, http.StatusNotFound, http.StatusConflict)
	prest.Description += " Solo la empresa dueña. 422 `empresa_no_valida`, `canal_inactivo`; `colaId` debe ser una cola activa de esa empresa. Evento `canal_mensajeria.prestado`."
	huma.Register(api, prest, func(ctx context.Context, in *entradaPrestamoHTTP) (*cuerpo[Prestamo], error) {
		return responder(k.GuardarPrestamo.Ejecutar(ctx, EntradaPrestamo{ID: in.ID, EmpresaID: in.Body.EmpresaID, Usos: in.Body.Usos,
			ColaID: in.Body.ColaID, Version: in.Body.Version}))
	})
	rev := op("revocarPrestamoCanal", http.MethodPost, "/canales/{id}/prestamos/{empresa}/revocar", "Dejar de compartir un número con una empresa",
		canalesConf, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	rev.Description += " Sus conversaciones abiertas por el número se resuelven y quedan de solo lectura; sus avisos en cola fallan; lo nuevo llega a la dueña. " +
		"Evento `canal_mensajeria.prestamo_revocado`."
	huma.Register(api, rev, func(ctx context.Context, in *entradaRevocarHTTP) (*cuerpo[Prestamo], error) {
		return responder(k.RevocarPrestamo.Ejecutar(ctx, EntradaRevocarPrestamo{ID: in.ID, EmpresaID: in.Empresa, Version: in.Body.Version, Motivo: in.Body.Motivo}))
	})
	confP := op("configurarPrestamoCanal", http.MethodPost, "/canales/{id}/prestamo/configurar", "Marcar un número prestado como el de avisos de tu empresa",
		canalesConf, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	confP.Description += " La empresa que usa el número. 422 `uso_no_permitido` si el préstamo no tiene el uso avisos."
	huma.Register(api, confP, func(ctx context.Context, in *entradaConfigurarPrestamoHTTP) (*cuerpo[Canal], error) {
		return responder(k.ConfigurarPrestamo.Ejecutar(ctx, EntradaConfigurarPrestamo{ID: in.ID, Version: in.Body.Version, AvisosEmpresa: in.Body.AvisosEmpresa}))
	})
	pe := op("pasarConversacionEmpresa", http.MethodPost, "/conversaciones/{id}/pasar-empresa", "Pasar una conversación a otra empresa que usa el número",
		nucleo.PermConversacionesTransferir, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	pe.Description += " Solo la empresa dueña del número y solo a una empresa con préstamo activo y un uso de conversación. 422 `empresa_no_valida`, " +
		"`conversacion_con_vinculos`; 409 `conversacion_existente`. Evento `conversacion.empresa_asignada` (motivo `pasada`)."
	huma.Register(api, pe, func(ctx context.Context, in *idCuerpoHTTP[EntradaPasarEmpresa]) (*cuerpo[ConversacionCliente], error) {
		in.Body.ID = in.ID
		return responder(k.PasarEmpresa.Ejecutar(ctx, in.Body))
	})

	ed := op("empresasDestinoConversacion", http.MethodGet, "/conversaciones/{id}/empresas-destino", "Empresas a las que se puede pasar la conversación",
		nucleo.PermConversacionesTransferir, http.StatusOK, http.StatusNotFound)
	ed.Description += " Mismo permiso que pasar-empresa. Solo empresas con préstamo activo del número y un uso de conversación; `[]` si la " +
		"conversación no es de la dueña del número o no hay ninguna. Lectura sin auditar."
	huma.Register(api, ed, func(ctx context.Context, in *EntradaEmpresasDestino) (*cuerpo[ListaEmpresasDestino], error) {
		return responder(k.EmpresasDestino.Ejecutar(ctx, *in))
	})

	// Webhook de la app (§15.1).
	wh := op("webhookMetaApp", http.MethodPost, "/meta/webhook", "Webhook de la app de Meta (números conectados con QR)", "", http.StatusOK,
		http.StatusRequestEntityTooLarge)
	wh.Security = nil
	wh.MaxBodyBytes = MaxCuerpoMeta
	wh.BodyReadTimeout = 10 * time.Second
	wh.Description = "Firmado con el app secret de la conexión (`X-Hub-Signature-256`; 401 `firma_invalida`, sin auditar). Cada `entry` se reparte al número QR " +
		"de su `phone_number_id` (o de su WABA en `account_update`) y se procesa en un job."
	huma.Register(api, wh, func(ctx context.Context, in *entradaMetaAppHTTP) (*cuerpo[MetaRecibido], error) {
		ctx = contexto.ConCanal(ctx, contexto.CanalWebhook)
		return responder(k.RecibirApp.Ejecutar(ctx, EntradaMetaApp{Firma: in.Firma, Cuerpo: in.RawBody}))
	})
	vw := op("verificarWebhookMetaApp", http.MethodGet, "/meta/webhook", "Verificación del webhook de la app de Meta", "", http.StatusOK, http.StatusForbidden)
	vw.Security = nil
	vw.Description = "Meta llama con `hub.mode=subscribe`, `hub.verify_token` (el de la conexión) y `hub.challenge`; responde el challenge. 403 `verificacion_invalida`."
	huma.Register(api, vw, func(ctx context.Context, in *entradaVerificarAppHTTP) (*salidaTexto, error) {
		ctx = contexto.ConCanal(ctx, contexto.CanalWebhook)
		c, err := k.VerificarApp.Ejecutar(ctx, EntradaVerificarMetaApp{Modo: in.Modo, Token: in.Token, Challenge: in.Challenge})
		if err != nil {
			return nil, err
		}
		return &salidaTexto{ContentType: "text/plain; charset=utf-8", Body: []byte(c)}, nil
	})
}
