package mensajeria

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Rutas del teléfono de Iris, E4c (contratos/voz.md §3.12). Handlers delgados.

const descTelefono = " Solo personas con su sesión (panel o app); un token de API o una integración: 404. La contraseña SIP nunca se devuelve. " +
	"Contrato: `.claude/integration/contratos/voz.md` §3.12 (E4c)."

type entradaTelefonoSIPHTTP struct {
	Protocolos string `header:"Sec-WebSocket-Protocol" maxLength:"200" doc:"sip, iris-ticket.<ticket> (el ticket de un solo uso de POST /telefono/abrir; no va en la URL)"`
	Origin     string `header:"Origin" doc:"Si viene, tiene que ser el de IRIS_BASE_URL"`
}

type grupoAccionHTTP struct {
	nucleo.ConIdempotencia
	ID      int64 `path:"id" minimum:"1"`
	GrupoID int64 `path:"grupoId" minimum:"1"`
	Body    EntradaEditarGrupo
}

type extensionAccionHTTP[T any] struct {
	nucleo.ConIdempotencia
	ID          int64 `path:"id" minimum:"1"`
	ExtensionID int64 `path:"extensionId" minimum:"1"`
	Body        T
}

func (m *Modulo) registrarTelefono(api huma.API) {
	k := m.Centralita.Telefono
	conf, ext := nucleo.PermCentralitaConfigurar, nucleo.PermCentralitaAsignarExtension
	d := func(o huma.Operation, detalle string) huma.Operation {
		o.Description += " " + detalle + descTelefono
		return o
	}
	// Mi teléfono.
	huma.Register(api, d(op("verTelefono", http.MethodGet, "/telefono", "Mi teléfono de Iris", "", http.StatusOK),
		"Sin permiso: es el tuyo. `disponible` y, si no, `motivo`. `puede` (llamar, transferir, recibir) y el límite de hoy."),
		func(ctx context.Context, _ *struct{}) (*cuerpo[TelefonoMio], error) {
			return responder(k.Mio.Ejecutar(ctx, struct{}{}))
		})
	huma.Register(api, d(op("abrirTelefono", http.MethodPost, "/telefono/abrir", "Abrir el teléfono (ticket del WebSocket y TURN)", "",
		http.StatusCreated, http.StatusServiceUnavailable),
		"Devuelve la ruta del WebSocket, un ticket de un solo uso (30 s, atado a esta sesión, la empresa y la extensión), la identidad SIP (sin "+
			"contraseña) y el TURN con credencial efímera (`politicaIce: relay`). 422 `sin_centralita`, `sin_extension`, `extension_escritorio`, "+
			"`sin_credencial`, `telefono_apagado`; 503 `turn_no_configurado`. `dispositivo: app` solo marca (no registra ni recibe)."),
		func(ctx context.Context, in *cuerpoHTTP[EntradaAbrirTelefono]) (*cuerpo[SesionTelefono], error) {
			r, err := k.Abrir.Ejecutar(ctx, in.Body)
			if err == nil {
				nucleo.NoRepetible(ctx)
			}
			return responder(r, err)
		})
	sip := d(op("telefonoSIP", http.MethodGet, "/telefono/sip", "WebSocket SIP del teléfono (subprotocolo sip, RFC 7118)", "",
		http.StatusOK, http.StatusForbidden, http.StatusServiceUnavailable),
		"Se abre con los subprotocolos `sip` e `iris-ticket.<ticket>` (el ticket no va en la URL). Iris es el puente con la Issabel: responde los desafíos de autenticación en el servidor, deja marcar solo los destinos de `llamar` "+
			"(o extensiones y grupos de la centralita) y transferir solo a `transferir/destinos`. 403 `ticket_no_valido` u `origen_no_permitido`; 503 "+
			"`centralita_no_responde`. Cierres: 4001 `reemplazada`, 4003 `sin_acceso`, 4008 `limite`, 4502 `centralita_no_responde`.")
	sip.Responses = map[string]*huma.Response{"101": {Description: "Cambio a WebSocket (subprotocolo sip)"}}
	huma.Register(api, sip, func(ctx context.Context, in *entradaTelefonoSIPHTTP) (*huma.StreamResponse, error) {
		tp, err := m.prepararTelefono(ctx, ticketDeProtocolos(in.Protocolos), in.Origin)
		if err != nil {
			return nil, err
		}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			r, w := humachi.Unwrap(hctx)
			if r == nil || w == nil {
				_ = tp.cen.CloseNow()
				_, _ = k.cerrarConexio.Ejecutar(context.WithoutCancel(ctx), entradaCerrarConexion{ConexionID: tp.cx.id, Motivo: "sin_upgrade"})
				return
			}
			m.atenderTelefono(ctx, w, r, tp, slog.Default())
		}}, nil
	})
	huma.Register(api, d(op("llamarTelefono", http.MethodPost, "/telefono/llamar", "Llamar con tu extensión", nucleo.PermCentralitaLlamar,
		http.StatusCreated, http.StatusConflict, http.StatusNotFound),
		"Uno de `clienteId`, `ticketId`, `ordenId` (con `telefono` opcional), `usuarioId` (un compañero) o `numero` (teclado). El número externo lo "+
			"pone el servidor y solo puede ser de Guatemala (+502 y 8 dígitos que empiezan por 2 a 7); el teclado acepta además extensiones y grupos "+
			"de la centralita. Devuelve el `destino` que el teléfono marca (60 s) y la llamada en `marcando`. 422 `destino_no_permitido`, "+
			"`sin_contacto`, `limite_llamadas`, los de `abrir`; 409 `llamadas_simultaneas`; 404 si no ves el cliente, el ticket o la orden. Evento "+
			"`llamada.saliente_iniciada`."),
		func(ctx context.Context, in *cuerpoHTTP[EntradaLlamarTelefono]) (*cuerpo[MarcacionTelefono], error) {
			r, err := k.Llamar.Ejecutar(ctx, in.Body)
			if err == nil {
				nucleo.NoRepetible(ctx)
			}
			return responder(r, err)
		})
	huma.Register(api, d(op("llamadaDirecta", http.MethodPost, "/telefono/llamada-directa", "Anotar una llamada desde el celular (orden)",
		nucleo.PermCentralitaLlamar, http.StatusCreated, http.StatusNotFound),
		"«Llamar desde mi celular»: deja la llamada en la orden (`origen: celular`, sin duración ni audio) y devuelve el número para abrir `tel:`. "+
			"No necesita extensión ni cuenta para el límite. 422 `sin_contacto`; 404 si no ves la orden. Evento `llamada.directa_registrada`."),
		func(ctx context.Context, in *cuerpoHTTP[EntradaLlamadaDirecta]) (*cuerpo[LlamadaDirecta], error) {
			return responder(k.Directa.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, d(op("destinosTransferencia", http.MethodGet, "/telefono/transferir/destinos", "A quién se puede transferir",
		nucleo.PermCentralitaTransferir, http.StatusOK),
		"Las personas con extensión de tu empresa (sin ti) y los grupos activos. El teléfono transfiere con REFER solo a estos números."),
		func(ctx context.Context, _ *struct{}) (*cuerpo[DestinosTransferencia], error) {
			return responder(k.Destinos.Ejecutar(ctx, struct{}{}))
		})
	// Administración.
	huma.Register(api, d(op("telefonoCentralita", http.MethodPost, "/centralitas/{id}/telefono", "Configurar el teléfono de Iris", conf,
		http.StatusOK, http.StatusConflict, http.StatusNotFound),
		"Con `version`. Cambiar puerto, ruta, huella o dominio apaga el teléfono y borra la última prueba. Evento `centralita.telefono_cambiado`."),
		func(ctx context.Context, in *centralitaAccionHTTP[EntradaTelefonoCentralita]) (*cuerpo[Centralita], error) {
			in.Body.ID = in.ID
			return responder(k.Configurar.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, d(op("probarTelefonoCentralita", http.MethodPost, "/centralitas/{id}/telefono/probar", "Probar el WSS de la centralita", conf,
		http.StatusOK, http.StatusNotFound),
		"Abre el WSS y manda un OPTIONS sin credenciales: `ok`, `huella_distinta`, `no_es_sip` o `no_conecta`, con la huella para compararla. Sin datos "+
			"(o con los guardados) usa la huella fijada y, si pasa, deja `probadoEn`. Evento `centralita.telefono_probado`."),
		func(ctx context.Context, in *centralitaAccionHTTP[EntradaProbarTelefono]) (*cuerpo[PruebaTelefono], error) {
			in.Body.ID = in.ID
			r, err := k.Probar.Ejecutar(ctx, in.Body)
			if err == nil {
				nucleo.NoRepetible(ctx)
			}
			return responder(r, err)
		})
	for _, a := range []struct {
		id, ruta, resumen string
		caso              func(context.Context, EntradaCambioTelefono) (Centralita, error)
	}{
		{"activarTelefono", "activar", "Encender el teléfono de Iris", k.Activar.Ejecutar},
		{"desactivarTelefono", "desactivar", "Apagar el teléfono de Iris", k.Desactivar.Ejecutar},
	} {
		huma.Register(api, d(op(a.id, http.MethodPost, "/centralitas/{id}/telefono/"+a.ruta, a.resumen, conf, http.StatusOK, http.StatusConflict, http.StatusNotFound),
			"Con `version`. Encender exige una prueba del WSS que pasó (422 `sin_probar`) y el TURN de la instalación (422 `turn_no_configurado`). Apagar "+
				"corta las conexiones de la empresa (4003). Evento `centralita.telefono_cambiado`."),
			func(ctx context.Context, in *centralitaAccionHTTP[EntradaCambioTelefono]) (*cuerpo[Centralita], error) {
				in.Body.ID = in.ID
				return responder(a.caso(ctx, in.Body))
			})
	}
	huma.Register(api, d(op("gruposCentralita", http.MethodGet, "/centralitas/{id}/grupos", "Grupos de la centralita", conf, http.StatusOK, http.StatusNotFound),
		"Grupos de timbrado y colas de Issabel a los que se puede transferir."),
		func(ctx context.Context, in *centralitaIDHTTP) (*cuerpo[ListaGrupos], error) {
			return responder(k.Grupos.Ejecutar(ctx, EntradaVerCentralita{ID: in.ID}))
		})
	huma.Register(api, d(op("crearGrupoCentralita", http.MethodPost, "/centralitas/{id}/grupos", "Agregar un grupo", conf,
		http.StatusOK, http.StatusConflict, http.StatusNotFound), "409 `grupo_existente`. Evento `grupo_centralita.creado`."),
		func(ctx context.Context, in *centralitaAccionHTTP[EntradaCrearGrupo]) (*cuerpo[GrupoCentralita], error) {
			in.Body.CentralitaID = in.ID
			return responder(k.CrearGrupo.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, d(op("editarGrupoCentralita", http.MethodPost, "/centralitas/{id}/grupos/{grupoId}/actualizar", "Editar un grupo", conf,
		http.StatusOK, http.StatusConflict, http.StatusNotFound), "Con `version`. 409 `grupo_existente`. Evento `grupo_centralita.actualizado`."),
		func(ctx context.Context, in *grupoAccionHTTP) (*cuerpo[GrupoCentralita], error) {
			in.Body.CentralitaID, in.Body.GrupoID = in.ID, in.GrupoID
			return responder(k.EditarGrupo.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, d(op("telefonoExtension", http.MethodPost, "/centralitas/{id}/extensiones/{extensionId}/telefono", "Tipo y credencial SIP de una extensión",
		ext, http.StatusOK, http.StatusNotFound),
		"`navegador` exige `sipUsuario` (de fábrica la extensión) y `sipSecreto` la primera vez o al cambiar el usuario (422 `clave_requerida`); "+
			"`escritorio` borra la credencial. La contraseña se guarda cifrada y nunca se devuelve. Cualquier cambio corta la conexión de esa persona. "+
			"Evento `extension.telefono_cambiado`."),
		func(ctx context.Context, in *extensionAccionHTTP[EntradaTelefonoExtension]) (*cuerpo[Extension], error) {
			in.Body.CentralitaID, in.Body.ExtensionID = in.ID, in.ExtensionID
			return responder(k.Extension.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, d(op("probarRegistroExtension", http.MethodPost, "/centralitas/{id}/extensiones/{extensionId}/probar-registro", "Probar registro",
		ext, http.StatusOK, http.StatusNotFound, http.StatusTooManyRequests),
		"REGISTER de consulta (sin Contact: no agrega ni quita registros) con la credencial guardada: `ok`, `credenciales`, `huella_distinta`, "+
			"`no_conecta` o `sin_credencial`. Con `ok` deja `sipProbadoEn` (y borra `sipRechazadoEn`); con `credenciales`, `sipRechazadoEn`. "+
			"Evento `extension.registro_probado`. Para no cebar fail2ban: 429 `credencial_rechazada` si la Issabel ya rechazó esta "+
			"contraseña y no se escribió otra (30 min), y 429 `demasiados_intentos` pasadas 3 pruebas por minuto de la extensión o 10 de "+
			"la centralita; los dos con `Retry-After`."),
		func(ctx context.Context, in *extensionAccionHTTP[struct{}]) (*cuerpo[PruebaRegistro], error) {
			r, err := k.ProbarReg.Ejecutar(ctx, EntradaProbarRegistro{CentralitaID: in.ID, ExtensionID: in.ExtensionID})
			if err == nil {
				nucleo.NoRepetible(ctx)
			}
			return responder(r, err)
		})
}

// ticketDeProtocolos saca el ticket del subprotocolo iris-ticket.<ticket> (o "").
func ticketDeProtocolos(v string) string {
	for _, p := range strings.Split(v, ",") {
		if t, ok := strings.CutPrefix(strings.TrimSpace(p), ProtocoloTicket); ok && t != "" && len(t) <= 64 {
			return t
		}
	}
	return ""
}
