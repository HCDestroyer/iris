package mensajeria

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Rutas de la centralita Issabel, E4a (contratos/voz.md §3.12). Handlers delgados.

type centralitaAccionHTTP[T any] struct {
	nucleo.ConIdempotencia
	ID   int64 `path:"id" minimum:"1"`
	Body T
}

type extensionQuitarHTTP struct {
	nucleo.ConIdempotencia
	ID          int64 `path:"id" minimum:"1"`
	ExtensionID int64 `path:"extensionId" minimum:"1"`
}

type centralitaIDHTTP struct {
	ID int64 `path:"id" minimum:"1"`
}

type llamadaCentralitaHTTP struct {
	ID uuid.UUID `path:"id" format:"uuid"`
}

type llamadaCentralitaAccionHTTP[T any] struct {
	nucleo.ConIdempotencia
	ID   uuid.UUID `path:"id" format:"uuid"`
	Body T
}

const descCentralita = " Solo personas con sesión del panel. La contraseña del AMI nunca se devuelve ni sale en la auditoría. Contrato: `.claude/integration/contratos/voz.md` §3.12 (E4a)."

func (m *Modulo) registrarCentralita(api huma.API) {
	k := m.Centralita
	conf, ext := nucleo.PermCentralitaConfigurar, nucleo.PermCentralitaAsignarExtension
	d := func(o huma.Operation, detalle string) huma.Operation {
		o.Description += " " + detalle + descCentralita
		return o
	}
	huma.Register(api, d(op("verCentralita", http.MethodGet, "/centralita", "La centralita de la empresa activa", ext, http.StatusOK),
		"`centralita` es null si la empresa no tiene."),
		func(ctx context.Context, _ *struct{}) (*cuerpo[CentralitaDeEmpresa], error) {
			return responder(k.Ver.Ejecutar(ctx, struct{}{}))
		})
	huma.Register(api, d(op("crearCentralita", http.MethodPost, "/centralitas", "Conectar la centralita Issabel (solo lectura)", conf,
		http.StatusCreated, http.StatusConflict),
		"Queda apagada: se enciende con `activar` después de una prueba que pasa. `huellaTls` es la huella que mostró `probar` y que el administrador "+
			"comparó con la de Issabel. 409 `centralita_existente`; 422 `url_no_permitida`, `puerto_no_permitido`, `cifrado_no_configurado`. Evento `centralita.creada`."),
		func(ctx context.Context, in *cuerpoHTTP[EntradaCrearCentralita]) (*cuerpo[Centralita], error) {
			return responder(k.Crear.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, d(op("probarDatosCentralita", http.MethodPost, "/centralitas/probar", "Probar datos de conexión sin guardarlos", conf, http.StatusOK),
		"Conecta por TLS, entra, hace Ping y ListCommands: `ok`, `permisos_de_mas` (el usuario puede escribir; no se podrá encender) o `no_conecta` "+
			"(un solo error para todo lo demás: la prueba no sirve para escanear la red). Devuelve la huella del certificado para compararla. Sin `secreto`, "+
			"usa la guardada de `centralitaId` solo si no cambian host, puerto ni usuario (422 `clave_requerida`). No guarda nada."),
		func(ctx context.Context, in *cuerpoHTTP[EntradaProbarCentralita]) (*cuerpo[PruebaCentralita], error) {
			r, err := k.Probar.Datos(ctx, in.Body)
			if err == nil {
				nucleo.NoRepetible(ctx)
			}
			return responder(r, err)
		})
	huma.Register(api, d(op("actualizarCentralita", http.MethodPost, "/centralitas/{id}/actualizar", "Editar la conexión con la centralita", conf,
		http.StatusOK, http.StatusConflict, http.StatusNotFound),
		"Con `version` (409 `conflicto_version`). Cambiar host, puerto, usuario, contraseña o huella la apaga y borra la última prueba: hay que probar y "+
			"encender otra vez. 422 `clave_requerida` si cambian host, puerto o usuario sin volver a escribir la contraseña. Evento `centralita.actualizada`."),
		func(ctx context.Context, in *centralitaAccionHTTP[EntradaActualizarCentralita]) (*cuerpo[Centralita], error) {
			in.Body.ID = in.ID
			return responder(k.Actualizar.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, d(op("probarCentralita", http.MethodPost, "/centralitas/{id}/probar", "Probar la conexión guardada", conf, http.StatusOK, http.StatusNotFound),
		"Con la huella fijada (`huella_distinta` si cambió). Si pasa, deja `probadaEn` (encender la exige); con `permisos_de_mas` la apaga."),
		func(ctx context.Context, in *centralitaAccionHTTP[struct{}]) (*cuerpo[PruebaCentralita], error) {
			r, err := k.Probar.Guardada(ctx, in.ID)
			if err == nil {
				nucleo.NoRepetible(ctx)
			}
			return responder(r, err)
		})
	for _, a := range []struct {
		id, ruta, resumen string
		caso              func(context.Context, EntradaCambioCentralita) (Centralita, error)
	}{
		{"activarCentralita", "activar", "Encender la centralita (Iris empieza a escuchar)", k.Activar.Ejecutar},
		{"desactivarCentralita", "desactivar", "Apagar la centralita en Iris", k.Desactivar.Ejecutar},
	} {
		huma.Register(api, d(op(a.id, http.MethodPost, "/centralitas/{id}/"+a.ruta, a.resumen, conf, http.StatusOK, http.StatusConflict, http.StatusNotFound),
			"Con `version`. Encender exige una prueba que pasó con los datos actuales (422 `sin_probar`). Eventos `centralita.actualizada` y `centralita.estado_cambiado`."),
			func(ctx context.Context, in *centralitaAccionHTTP[EntradaCambioCentralita]) (*cuerpo[Centralita], error) {
				in.Body.ID = in.ID
				return responder(a.caso(ctx, in.Body))
			})
	}
	huma.Register(api, d(op("confirmarHuellaCentralita", http.MethodPost, "/centralitas/{id}/confirmar-huella", "Confirmar el certificado nuevo de la centralita",
		conf, http.StatusOK, http.StatusConflict, http.StatusNotFound),
		"Cuando la conexión se detuvo por `huella_cambiada`: la huella debe ser exactamente `huellaVista` (422 `huella_no_coincide`). La conexión vuelve a intentarse."),
		func(ctx context.Context, in *centralitaAccionHTTP[EntradaConfirmarHuella]) (*cuerpo[Centralita], error) {
			in.Body.ID = in.ID
			return responder(k.ConfirmarHuella.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, d(op("extensionesCentralita", http.MethodGet, "/centralitas/{id}/extensiones", "Extensiones asignadas", ext, http.StatusOK, http.StatusNotFound),
		"Solo a estas personas les sale la ventana; las demás extensiones de la centralita Iris ni las mira."),
		func(ctx context.Context, in *centralitaIDHTTP) (*cuerpo[ListaExtensiones], error) {
			return responder(k.Extensiones.Ejecutar(ctx, EntradaVerCentralita{ID: in.ID}))
		})
	huma.Register(api, d(op("asignarExtension", http.MethodPost, "/centralitas/{id}/extensiones/asignar", "Asignar una extensión a una persona", ext,
		http.StatusOK, http.StatusConflict, http.StatusNotFound),
		"`extension`: 2 a 8 dígitos. 409 `extension_ocupada` o `usuario_con_extension` (quítala primero); 422 `usuario_no_valido`. Evento `extension.asignada`."),
		func(ctx context.Context, in *centralitaAccionHTTP[EntradaAsignarExtension]) (*cuerpo[Extension], error) {
			in.Body.CentralitaID = in.ID
			return responder(k.Asignar.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, d(op("quitarExtension", http.MethodPost, "/centralitas/{id}/extensiones/{extensionId}/quitar", "Quitar una extensión", ext,
		http.StatusOK, http.StatusNotFound),
		"Ya no le sale la ventana; sus llamadas anteriores se quedan. Evento `extension.quitada`."),
		func(ctx context.Context, in *extensionQuitarHTTP) (*cuerpo[Extension], error) {
			return responder(k.Quitar.Ejecutar(ctx, EntradaQuitarExtension{CentralitaID: in.ID, ExtensionID: in.ExtensionID}))
		})
	// Llamadas.
	huma.Register(api, d(op("listarLlamadasCentralita", http.MethodGet, "/centralitas/llamadas", "Llamadas de la centralita", nucleo.PermLlamadasVer, http.StatusOK),
		"Por cliente (`clienteId`, ficha del cliente), por ticket (`ticketId`) o las tuyas (`mias=true`, con `hoy=true` las de hoy). `{ data, nextCursor }`, "+
			"de la más nueva a la más vieja. Nombres de clientes solo con `clientes.ver`."),
		func(ctx context.Context, in *EntradaListarLlamadasCentralita) (*cuerpo[ListaLlamadasCentralita], error) {
			return responder(k.Llamadas.Ejecutar(ctx, *in))
		})
	huma.Register(api, d(op("verLlamadaCentralita", http.MethodGet, "/centralitas/llamadas/{id}", "Una llamada de la centralita", nucleo.PermLlamadasVer,
		http.StatusOK, http.StatusNotFound), "Con varios clientes posibles y sin cliente elegido trae `candidatos` (con `clientes.ver`)."),
		func(ctx context.Context, in *llamadaCentralitaHTTP) (*cuerpo[Llamada], error) {
			return responder(k.VerLlamada.Ejecutar(ctx, EntradaLlamadaCentralita{ID: in.ID}))
		})
	huma.Register(api, d(op("anotarLlamadaCentralita", http.MethodPost, "/centralitas/llamadas/{id}/anotar", "Nota y resultado de una llamada de la centralita",
		nucleo.PermLlamadasAnotar, http.StatusOK, http.StatusConflict, http.StatusNotFound),
		"Solo quien la recibió, contestó o marcó, o quien supervisa las extensiones (`centralita.asignar_extension`); si no, 404. Con `version`. Evento `llamada.anotada`."),
		func(ctx context.Context, in *llamadaCentralitaAccionHTTP[EntradaAnotarCentralita]) (*cuerpo[Llamada], error) {
			in.Body.ID = in.ID
			return responder(k.Anotar.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, d(op("asociarClienteCentralita", http.MethodPost, "/centralitas/llamadas/{id}/asociar-cliente", "Elegir el cliente de una llamada",
		nucleo.PermLlamadasAnotar, http.StatusOK, http.StatusNotFound),
		"Varios posibles o número desconocido. Exige además `clientes.ver`. 422 `cliente_no_valido`, `servicio_no_valido`, `cliente_distinto`. Evento `llamada.cliente_asociado`."),
		func(ctx context.Context, in *llamadaCentralitaAccionHTTP[EntradaAsociarCentralita]) (*cuerpo[Llamada], error) {
			in.Body.ID = in.ID
			return responder(k.Asociar.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, d(op("vincularTicketCentralita", http.MethodPost, "/centralitas/llamadas/{id}/vincular-ticket", "Agregar la llamada a un ticket",
		nucleo.PermLlamadasAnotar, http.StatusOK, http.StatusNotFound),
		"El ticket debe verse con `tickets.ver` (si no, 404). Si la llamada no tenía cliente toma el del ticket; si tenía otro, 422 `cliente_distinto`. "+
			"Para crear un ticket desde la llamada, el panel crea el ticket (`POST /tickets`) y después lo vincula aquí. Evento `llamada.vinculada`."),
		func(ctx context.Context, in *llamadaCentralitaAccionHTTP[EntradaVincularTicket]) (*cuerpo[Llamada], error) {
			in.Body.ID = in.ID
			return responder(k.Vincular.Ejecutar(ctx, in.Body))
		})
}
