package mensajeria

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/auditoria"
	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Rutas de la bandeja de conversaciones con clientes (contratos/conversaciones.md).

// casosConv reúne los casos de la bandeja.
type casosConv struct {
	ConfigurarCanal    *auditoria.Caso[EntradaConfigurarCanal, Canal]
	ListarColas        *auditoria.Caso[struct{}, ListaColas]
	CrearCola          *auditoria.Caso[EntradaCola, ColaConversaciones]
	ActualizarCola     *auditoria.Caso[EntradaCola, ColaConversaciones]
	MiembrosCola       *auditoria.Caso[EntradaMiembrosCola, ColaConversaciones]
	ActivarCola        *auditoria.Caso[EntradaIDConv, ColaConversaciones]
	DesactivarCola     *auditoria.Caso[EntradaIDConv, ColaConversaciones]
	ListarEtiquetas    *auditoria.Caso[struct{}, ListaEtiquetas]
	CrearEtiqueta      *auditoria.Caso[EntradaEtiqueta, EtiquetaConversacion]
	ActualizarEtiqueta *auditoria.Caso[EntradaEtiqueta, EtiquetaConversacion]
	ActivarEtiqueta    *auditoria.Caso[EntradaIDConv, EtiquetaConversacion]
	DesactivarEtiqueta *auditoria.Caso[EntradaIDConv, EtiquetaConversacion]
	ListarRespuestas   *auditoria.Caso[EntradaListarRespuestas, ListaRespuestasRapidas]
	CrearRespuesta     *auditoria.Caso[EntradaRespuestaRapida, RespuestaRapida]
	ActualizarRespuest *auditoria.Caso[EntradaRespuestaRapida, RespuestaRapida]
	ActivarRespuesta   *auditoria.Caso[EntradaIDConv, RespuestaRapida]
	DesactivarRespuest *auditoria.Caso[EntradaIDConv, RespuestaRapida]

	Listar       *auditoria.Caso[EntradaListarConvs, nucleo.Pagina[ConversacionCliente]]
	Contadores   *auditoria.Caso[struct{}, ContadoresConversaciones]
	Numeros      *auditoria.Caso[struct{}, ListaNumeros]
	Agentes      *auditoria.Caso[EntradaAgentes, ListaAgentes]
	Plantillas   *auditoria.Caso[EntradaConversacion, ListaPlantillasConv]
	Ver          *auditoria.Caso[EntradaConversacion, ConversacionClienteDetalle]
	Mensajes     *auditoria.Caso[EntradaMensajesConv, nucleo.Pagina[MensajeConversacion]]
	Leer         *auditoria.Caso[EntradaLeerConv, LecturaConv]
	Responder    *auditoria.Caso[EntradaResponder, MensajeConvEnviado]
	Anotar       *auditoria.Caso[EntradaNota, MensajeConvEnviado]
	Tomar        *auditoria.Caso[EntradaAccionConv, ConversacionClienteDetalle]
	Asignar      *auditoria.Caso[EntradaAsignarConv, ConversacionClienteDetalle]
	Transferir   *auditoria.Caso[EntradaTransferirConv, ConversacionClienteDetalle]
	Etiquetar    *auditoria.Caso[EntradaEtiquetarConv, ConversacionClienteDetalle]
	Resolver     *auditoria.Caso[EntradaResolverConv, ConversacionClienteDetalle]
	Reabrir      *auditoria.Caso[EntradaAccionConv, ConversacionClienteDetalle]
	Coincidencia *BuscarCoincidencias
	Asociar      *AsociarCliente
	Desasociar   *auditoria.Caso[EntradaAccionConv, ConversacionClienteDetalle]
	ContactoIris *auditoria.Caso[EntradaContactoIrisConv, ConversacionClienteDetalle]
	EditarContac *auditoria.Caso[EntradaActualizarContactoIris, ContactoIris]
	UnirContacto *UnirContacto
	Contactos    *auditoria.Caso[EntradaBuscarContactos, nucleo.Pagina[ContactoIris]]
	CrearTicket  *CrearTicketConv
	CrearOrden   *CrearOrdenConv
	CrearProspec *auditoria.Caso[EntradaProspectoConv, VinculoCreado]
	Boleta       *auditoria.Caso[EntradaBoleta, Boleta]
	Promesas     *Promesas
	Cambios      *auditoria.Caso[EntradaCambiosConv, CambiosConversaciones]
}

func nuevosCasosConv(m *Modulo) casosConv {
	return casosConv{
		ConfigurarCanal: nuevoConfigurarCanal(m), ListarColas: nuevoListarColas(m), CrearCola: nuevoCrearCola(m), ActualizarCola: nuevoActualizarCola(m),
		MiembrosCola: nuevoMiembrosCola(m), ActivarCola: nuevoCambioCola(m, "canales.activar_cola", true), DesactivarCola: nuevoCambioCola(m, "canales.desactivar_cola", false),
		ListarEtiquetas: nuevoListarEtiquetas(m), CrearEtiqueta: nuevoGuardarEtiqueta(m, true), ActualizarEtiqueta: nuevoGuardarEtiqueta(m, false),
		ActivarEtiqueta:    nuevoCambioEtiqueta(m, "conversaciones.activar_etiqueta", true),
		DesactivarEtiqueta: nuevoCambioEtiqueta(m, "conversaciones.desactivar_etiqueta", false),
		ListarRespuestas:   nuevoListarRespuestas(m), CrearRespuesta: nuevoGuardarRespuesta(m, true), ActualizarRespuest: nuevoGuardarRespuesta(m, false),
		ActivarRespuesta:   nuevoCambioRespuesta(m, "conversaciones.activar_respuesta", true),
		DesactivarRespuest: nuevoCambioRespuesta(m, "conversaciones.desactivar_respuesta", false),
		Listar:             nuevoListarConvs(m), Contadores: nuevoContadores(m), Numeros: nuevoListarNumeros(m), Agentes: nuevoListarAgentes(m),
		Plantillas: nuevoPlantillasConv(m), Ver: nuevoVerConv(m), Mensajes: nuevoMensajesConv(m), Leer: nuevoLeerConv(m),
		Responder: nuevoResponder(m), Anotar: nuevoAnotar(m), Tomar: nuevoTomarConv(m), Asignar: nuevoAsignarConv(m),
		Transferir: nuevoTransferirConv(m), Etiquetar: nuevoEtiquetarConv(m), Resolver: nuevoResolverConv(m), Reabrir: nuevoReabrirConv(m),
		Coincidencia: nuevoBuscarCoincidencias(m), Asociar: nuevoAsociarCliente(m), Desasociar: nuevoDesasociar(m),
		ContactoIris: nuevoContactoIrisConv(m), EditarContac: nuevoActualizarContactoIris(m), UnirContacto: nuevoUnirContacto(m),
		Contactos: nuevoBuscarContactos(m), CrearTicket: nuevoCrearTicketConv(m), CrearOrden: nuevoCrearOrdenConv(m),
		CrearProspec: nuevoCrearProspectoConv(m), Boleta: nuevoRegistrarBoleta(m), Promesas: nuevasPromesas(m), Cambios: nuevoCambiosConv(m),
	}
}

type idHTTP struct {
	nucleo.ConIdempotencia
	ID int64 `path:"id" minimum:"1"`
}

type idCuerpoHTTP[T any] struct {
	nucleo.ConIdempotencia
	ID   int64 `path:"id" minimum:"1"`
	Body T
}

type cuerpoHTTP[T any] struct {
	nucleo.ConIdempotencia
	Body T
}

type salidaMensajeConv struct {
	Status   int
	Repetido string `header:"Idempotent-Replayed" doc:"true si el id ya se había recibido"`
	Body     MensajeConversacion
}

func respMensaje(r MensajeConvEnviado, err error) (*salidaMensajeConv, error) {
	if err != nil {
		return nil, err
	}
	s := &salidaMensajeConv{Status: http.StatusCreated, Body: r.MensajeConversacion}
	if r.Repetido {
		s.Status, s.Repetido = http.StatusOK, "true"
	}
	return s, nil
}

func (m *Modulo) registrarConversaciones(api huma.API) {
	k := m.Conv
	ver, resp, conf, canalesConf := nucleo.PermConversacionesVer, nucleo.PermConversacionesResponder, nucleo.PermConversacionesConfigurar, nucleo.PermCanalesConfigurar

	// Configuración ----------------------------------------------------------------------------
	cc := op("configurarCanalMensajeria", http.MethodPost, "/canales/{id}/configurar", "Uso, cola y número de avisos de un número", canalesConf,
		http.StatusOK, http.StatusNotFound, http.StatusConflict)
	cc.Description += " Sin credenciales (esas son de mensajeria.configurar). Marcar `avisosEmpresa` lo quita del número anterior de la empresa (422 `numero_avisos_requiere_uso` sin el uso avisos)."
	huma.Register(api, cc, func(ctx context.Context, in *idCuerpoHTTP[EntradaConfigurarCanal]) (*cuerpo[Canal], error) {
		in.Body.ID = in.ID
		return responder(k.ConfigurarCanal.Ejecutar(ctx, in.Body))
	})
	huma.Register(api, op("listarColasConversaciones", http.MethodGet, "/colas", "Colas de conversaciones (con miembros)", ver, http.StatusOK),
		func(ctx context.Context, _ *struct{}) (*cuerpo[ListaColas], error) { return responder(k.ListarColas.Ejecutar(ctx, struct{}{})) })
	huma.Register(api, op("crearColaConversaciones", http.MethodPost, "/colas", "Crear una cola", canalesConf, http.StatusCreated, http.StatusConflict),
		func(ctx context.Context, in *cuerpoHTTP[EntradaCola]) (*cuerpo[ColaConversaciones], error) {
			return responder(k.CrearCola.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, op("actualizarColaConversaciones", http.MethodPost, "/colas/{id}/actualizar", "Nombre, descripción y SLA de una cola", canalesConf,
		http.StatusOK, http.StatusNotFound, http.StatusConflict), func(ctx context.Context, in *idCuerpoHTTP[EntradaCola]) (*cuerpo[ColaConversaciones], error) {
		in.Body.ID = in.ID
		return responder(k.ActualizarCola.Ejecutar(ctx, in.Body))
	})
	huma.Register(api, op("miembrosColaConversaciones", http.MethodPost, "/colas/{id}/miembros", "Fijar los agentes de una cola", canalesConf,
		http.StatusOK, http.StatusNotFound, http.StatusConflict), func(ctx context.Context, in *idCuerpoHTTP[EntradaMiembrosCola]) (*cuerpo[ColaConversaciones], error) {
		in.Body.ID = in.ID
		return responder(k.MiembrosCola.Ejecutar(ctx, in.Body))
	})
	huma.Register(api, op("activarColaConversaciones", http.MethodPost, "/colas/{id}/activar", "Activar una cola", canalesConf, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *idHTTP) (*cuerpo[ColaConversaciones], error) {
			return responder(k.ActivarCola.Ejecutar(ctx, EntradaIDConv{ID: in.ID}))
		})
	huma.Register(api, op("desactivarColaConversaciones", http.MethodPost, "/colas/{id}/desactivar", "Desactivar una cola", canalesConf, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *idHTTP) (*cuerpo[ColaConversaciones], error) {
			return responder(k.DesactivarCola.Ejecutar(ctx, EntradaIDConv{ID: in.ID}))
		})

	huma.Register(api, op("listarEtiquetasConversaciones", http.MethodGet, "/etiquetas", "Etiquetas de conversaciones", ver, http.StatusOK),
		func(ctx context.Context, _ *struct{}) (*cuerpo[ListaEtiquetas], error) {
			return responder(k.ListarEtiquetas.Ejecutar(ctx, struct{}{}))
		})
	huma.Register(api, op("crearEtiquetaConversaciones", http.MethodPost, "/etiquetas", "Crear una etiqueta", conf, http.StatusCreated, http.StatusConflict),
		func(ctx context.Context, in *cuerpoHTTP[EntradaEtiqueta]) (*cuerpo[EtiquetaConversacion], error) {
			return responder(k.CrearEtiqueta.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, op("actualizarEtiquetaConversaciones", http.MethodPost, "/etiquetas/{id}/actualizar", "Actualizar una etiqueta", conf,
		http.StatusOK, http.StatusNotFound, http.StatusConflict), func(ctx context.Context, in *idCuerpoHTTP[EntradaEtiqueta]) (*cuerpo[EtiquetaConversacion], error) {
		in.Body.ID = in.ID
		return responder(k.ActualizarEtiqueta.Ejecutar(ctx, in.Body))
	})
	huma.Register(api, op("activarEtiquetaConversaciones", http.MethodPost, "/etiquetas/{id}/activar", "Activar una etiqueta", conf, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *idHTTP) (*cuerpo[EtiquetaConversacion], error) {
			return responder(k.ActivarEtiqueta.Ejecutar(ctx, EntradaIDConv{ID: in.ID}))
		})
	huma.Register(api, op("desactivarEtiquetaConversaciones", http.MethodPost, "/etiquetas/{id}/desactivar", "Desactivar una etiqueta", conf, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *idHTTP) (*cuerpo[EtiquetaConversacion], error) {
			return responder(k.DesactivarEtiqueta.Ejecutar(ctx, EntradaIDConv{ID: in.ID}))
		})

	huma.Register(api, op("listarRespuestasRapidas", http.MethodGet, "/respuestas-rapidas", "Respuestas rápidas (las de la cola y las generales)", resp, http.StatusOK),
		func(ctx context.Context, in *EntradaListarRespuestas) (*cuerpo[ListaRespuestasRapidas], error) {
			return responder(k.ListarRespuestas.Ejecutar(ctx, *in))
		})
	huma.Register(api, op("crearRespuestaRapida", http.MethodPost, "/respuestas-rapidas", "Crear una respuesta rápida", conf, http.StatusCreated, http.StatusConflict),
		func(ctx context.Context, in *cuerpoHTTP[EntradaRespuestaRapida]) (*cuerpo[RespuestaRapida], error) {
			return responder(k.CrearRespuesta.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, op("actualizarRespuestaRapida", http.MethodPost, "/respuestas-rapidas/{id}/actualizar", "Actualizar una respuesta rápida", conf,
		http.StatusOK, http.StatusNotFound, http.StatusConflict), func(ctx context.Context, in *idCuerpoHTTP[EntradaRespuestaRapida]) (*cuerpo[RespuestaRapida], error) {
		in.Body.ID = in.ID
		return responder(k.ActualizarRespuest.Ejecutar(ctx, in.Body))
	})
	huma.Register(api, op("activarRespuestaRapida", http.MethodPost, "/respuestas-rapidas/{id}/activar", "Activar una respuesta rápida", conf, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *idHTTP) (*cuerpo[RespuestaRapida], error) {
			return responder(k.ActivarRespuesta.Ejecutar(ctx, EntradaIDConv{ID: in.ID}))
		})
	huma.Register(api, op("desactivarRespuestaRapida", http.MethodPost, "/respuestas-rapidas/{id}/desactivar", "Desactivar una respuesta rápida", conf, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *idHTTP) (*cuerpo[RespuestaRapida], error) {
			return responder(k.DesactivarRespuest.Ejecutar(ctx, EntradaIDConv{ID: in.ID}))
		})

	// Bandeja ------------------------------------------------------------------------------------
	const base = "/conversaciones"
	lista := op("listarConversacionesClientes", http.MethodGet, base, "Bandeja de conversaciones con clientes", ver, http.StatusOK)
	lista.Description += " Las que ves (tus colas, sin cola, asignadas a ti o en que te mencionaron; todas con conversaciones.supervisar), de la más reciente a la más vieja."
	huma.Register(api, lista, func(ctx context.Context, in *EntradaListarConvs) (*cuerpo[nucleo.Pagina[ConversacionCliente]], error) {
		return responder(k.Listar.Ejecutar(ctx, *in))
	})
	huma.Register(api, op("contadoresConversaciones", http.MethodGet, base+"/contadores", "Contadores de la bandeja", ver, http.StatusOK),
		func(ctx context.Context, _ *struct{}) (*cuerpo[ContadoresConversaciones], error) {
			return responder(k.Contadores.Ejecutar(ctx, struct{}{}))
		})
	huma.Register(api, op("numerosConversaciones", http.MethodGet, base+"/canales", "Números de WhatsApp de la empresa (sin credenciales)", ver, http.StatusOK),
		func(ctx context.Context, _ *struct{}) (*cuerpo[ListaNumeros], error) { return responder(k.Numeros.Ejecutar(ctx, struct{}{})) })
	huma.Register(api, op("agentesConversaciones", http.MethodGet, base+"/agentes", "Agentes que atienden conversaciones", ver, http.StatusOK),
		func(ctx context.Context, in *EntradaAgentes) (*cuerpo[ListaAgentes], error) { return responder(k.Agentes.Ejecutar(ctx, *in)) })
	huma.Register(api, op("cambiosConversaciones", http.MethodGet, base+"/cambios", "Cambios de la bandeja desde un cursor (sondeo)", ver, http.StatusOK, http.StatusGone),
		func(ctx context.Context, in *EntradaCambiosConv) (*cuerpo[CambiosConversaciones], error) {
			return responder(k.Cambios.Ejecutar(ctx, *in))
		})
	eventos := op("eventosConversaciones", http.MethodGet, base+"/eventos", "Tiempo real de la bandeja (Server-Sent Events)", ver,
		http.StatusOK, http.StatusGone, http.StatusTooManyRequests, http.StatusServiceUnavailable)
	eventos.Description += " Igual que el flujo del chat: `id` (cursor), `event` (mensaje_nuevo, mensaje_actualizado, conversacion_actualizada, mencion, guardia), " +
		"`data` (un EventoConversacion). Last-Event-ID o ?desde=; latido cada 25 s; cierre a los 30 min; revalidación de la sesión."
	eventos.Responses = map[string]*huma.Response{"200": {Description: "Flujo de eventos",
		Content: map[string]*huma.MediaType{"text/event-stream": {Schema: &huma.Schema{Type: huma.TypeString}}}}}
	huma.Register(api, eventos, func(ctx context.Context, in *entradaEventosHTTP) (*huma.StreamResponse, error) {
		desde := in.UltimoID
		if desde == "" {
			desde = in.Desde
		}
		f, sub, quitar, err := m.abrirFlujoConvSSE(ctx, desde)
		if err != nil {
			return nil, err
		}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			r, w := humachi.Unwrap(hctx)
			if r == nil || w == nil {
				quitar()
				return
			}
			m.transmitirConv(r.Context(), w, f, sub, quitar)
		}}, nil
	})
	huma.Register(api, op("verConversacionCliente", http.MethodGet, base+"/{id}", "Una conversación con vínculos, participantes y el resumen del cliente",
		ver, http.StatusOK, http.StatusNotFound), func(ctx context.Context, in *EntradaConversacion) (*cuerpo[ConversacionClienteDetalle], error) {
		return responder(k.Ver.Ejecutar(ctx, *in))
	})
	huma.Register(api, op("plantillasConversacion", http.MethodGet, base+"/{id}/plantillas", "Plantillas aprobadas en el número de la conversación",
		resp, http.StatusOK, http.StatusNotFound), func(ctx context.Context, in *EntradaConversacion) (*cuerpo[ListaPlantillasConv], error) {
		return responder(k.Plantillas.Ejecutar(ctx, *in))
	})
	huma.Register(api, op("mensajesConversacion", http.MethodGet, base+"/{id}/mensajes", "Mensajes de una conversación (seq ascendente)", ver,
		http.StatusOK, http.StatusNotFound), func(ctx context.Context, in *EntradaMensajesConv) (*cuerpo[nucleo.Pagina[MensajeConversacion]], error) {
		return responder(k.Mensajes.Ejecutar(ctx, *in))
	})
	env := op("responderConversacion", http.MethodPost, base+"/{id}/mensajes", "Responder al cliente", resp,
		http.StatusCreated, http.StatusNotFound, http.StatusConflict)
	env.Description += " Idempotente por `id` (200 + Idempotent-Replayed si se repite; 409 `mensaje_id_reutilizado` con otro contenido). Texto y adjuntos solo " +
		"dentro de la ventana de 24 h (422 `fuera_de_ventana`); fuera, una plantilla aprobada en el número (422 `plantilla_no_aprobada`) con permiso de contacto (422 `sin_permiso`, `baja`)."
	env.Responses = map[string]*huma.Response{"200": {Description: "Reenvío del mismo id"}}
	huma.Register(api, env, func(ctx context.Context, in *idCuerpoHTTP[EntradaResponder]) (*salidaMensajeConv, error) {
		in.Body.ConversacionID, in.Body.IdempotencyKey = in.ID, in.IdempotencyKey
		return respMensaje(k.Responder.Ejecutar(ctx, in.Body))
	})
	nota := op("notaConversacion", http.MethodPost, base+"/{id}/notas", "Nota interna (con menciones)", resp, http.StatusCreated, http.StatusNotFound, http.StatusConflict)
	nota.Responses = map[string]*huma.Response{"200": {Description: "Reenvío del mismo id"}}
	huma.Register(api, nota, func(ctx context.Context, in *idCuerpoHTTP[EntradaNota]) (*salidaMensajeConv, error) {
		in.Body.ConversacionID, in.Body.IdempotencyKey = in.ID, in.IdempotencyKey
		return respMensaje(k.Anotar.Ejecutar(ctx, in.Body))
	})
	huma.Register(api, op("leerConversacionCliente", http.MethodPost, base+"/{id}/leer", "Marcar leído hasta un mensaje", ver, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *idCuerpoHTTP[EntradaLeerConv]) (*cuerpo[LecturaConv], error) {
			in.Body.ID = in.ID
			return responder(k.Leer.Ejecutar(ctx, in.Body))
		})

	accion := func(id, ruta, resumen string, permiso nucleo.Permiso) huma.Operation {
		return op(id, http.MethodPost, base+"/{id}/"+ruta, resumen, permiso, http.StatusOK, http.StatusNotFound, http.StatusConflict)
	}
	huma.Register(api, accion("tomarConversacion", "tomar", "Tomar una conversación sin asignar", nucleo.PermConversacionesTomar),
		func(ctx context.Context, in *idCuerpoHTTP[EntradaAccionConv]) (*cuerpo[ConversacionClienteDetalle], error) {
			in.Body.ID = in.ID
			return responder(k.Tomar.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, accion("asignarConversacion", "asignar", "Asignar a un agente (o quitar)", nucleo.PermConversacionesAsignar),
		func(ctx context.Context, in *idCuerpoHTTP[EntradaAsignarConv]) (*cuerpo[ConversacionClienteDetalle], error) {
			in.Body.ID = in.ID
			return responder(k.Asignar.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, accion("transferirConversacion", "transferir", "Pasar a otra cola con un resumen", nucleo.PermConversacionesTransferir),
		func(ctx context.Context, in *idCuerpoHTTP[EntradaTransferirConv]) (*cuerpo[ConversacionClienteDetalle], error) {
			in.Body.ID = in.ID
			return responder(k.Transferir.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, accion("etiquetarConversacion", "etiquetar", "Fijar las etiquetas", nucleo.PermConversacionesEtiquetar),
		func(ctx context.Context, in *idCuerpoHTTP[EntradaEtiquetarConv]) (*cuerpo[ConversacionClienteDetalle], error) {
			in.Body.ID = in.ID
			return responder(k.Etiquetar.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, accion("resolverConversacion", "resolver", "Resolver", nucleo.PermConversacionesResolver),
		func(ctx context.Context, in *idCuerpoHTTP[EntradaResolverConv]) (*cuerpo[ConversacionClienteDetalle], error) {
			in.Body.ID = in.ID
			return responder(k.Resolver.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, accion("reabrirConversacion", "reabrir", "Reabrir una resuelta", nucleo.PermConversacionesResolver),
		func(ctx context.Context, in *idCuerpoHTTP[EntradaAccionConv]) (*cuerpo[ConversacionClienteDetalle], error) {
			in.Body.ID = in.ID
			return responder(k.Reabrir.Ejecutar(ctx, in.Body))
		})

	// Contactos ----------------------------------------------------------------------------------
	huma.Register(api, op("coincidenciasConversacion", http.MethodGet, base+"/{id}/coincidencias", "Clientes de Argos y contactos de Iris con el mismo número",
		ver, http.StatusOK, http.StatusNotFound), func(ctx context.Context, in *EntradaConversacion) (*cuerpo[CoincidenciasConv], error) {
		return responder(k.Coincidencia.Ejecutar(ctx, *in))
	})
	huma.Register(api, accion("asociarClienteConversacion", "asociar-cliente", "Asociar un cliente de Argos (solo su id)", nucleo.PermConversacionesAsociar),
		func(ctx context.Context, in *idCuerpoHTTP[EntradaAsociarCliente]) (*cuerpo[ConversacionClienteDetalle], error) {
			in.Body.ID = in.ID
			return responder(k.Asociar.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, accion("desasociarConversacion", "desasociar", "Quitar cliente, contacto de Iris y prospecto", nucleo.PermConversacionesAsociar),
		func(ctx context.Context, in *idCuerpoHTTP[EntradaAccionConv]) (*cuerpo[ConversacionClienteDetalle], error) {
			in.Body.ID = in.ID
			return responder(k.Desasociar.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, accion("contactoIrisConversacion", "contacto-iris", "Guardar como contacto de Iris", nucleo.PermConversacionesAsociar),
		func(ctx context.Context, in *idCuerpoHTTP[EntradaContactoIrisConv]) (*cuerpo[ConversacionClienteDetalle], error) {
			in.Body.ID = in.ID
			return responder(k.ContactoIris.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, op("buscarContactosIris", http.MethodGet, "/contactos-iris", "Buscar contactos de Iris", ver, http.StatusOK),
		func(ctx context.Context, in *EntradaBuscarContactos) (*cuerpo[nucleo.Pagina[ContactoIris]], error) {
			return responder(k.Contactos.Ejecutar(ctx, *in))
		})
	huma.Register(api, op("actualizarContactoIris", http.MethodPost, "/contactos-iris/{id}/actualizar", "Actualizar un contacto de Iris", nucleo.PermConversacionesAsociar,
		http.StatusOK, http.StatusNotFound, http.StatusConflict), func(ctx context.Context, in *idCuerpoHTTP[EntradaActualizarContactoIris]) (*cuerpo[ContactoIris], error) {
		in.Body.ID = in.ID
		return responder(k.EditarContac.Ejecutar(ctx, in.Body))
	})
	huma.Register(api, op("unirContactoIris", http.MethodPost, "/contactos-iris/{id}/unir-cliente", "Unir el contacto a su ficha de Argos", nucleo.PermConversacionesAsociar,
		http.StatusOK, http.StatusNotFound, http.StatusConflict), func(ctx context.Context, in *idCuerpoHTTP[EntradaUnirContacto]) (*cuerpo[ContactoIris], error) {
		in.Body.ID = in.ID
		return responder(k.UnirContacto.Ejecutar(ctx, in.Body))
	})

	// Acciones -----------------------------------------------------------------------------------
	creada := func(id, ruta, resumen string, permiso nucleo.Permiso) huma.Operation {
		return op(id, http.MethodPost, base+"/{id}/"+ruta, resumen, permiso, http.StatusCreated, http.StatusNotFound)
	}
	huma.Register(api, creada("crearTicketConversacion", "crear-ticket", "Crear un ticket ligado a la conversación", nucleo.PermTicketsCrear),
		func(ctx context.Context, in *idCuerpoHTTP[EntradaTicketConv]) (*cuerpo[VinculoCreado], error) {
			in.Body.ID, in.Body.Clave = in.ID, in.IdempotencyKey
			return responder(k.CrearTicket.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, creada("crearOrdenConversacion", "crear-orden", "Crear una orden ligada a la conversación", nucleo.PermOrdenesCrear),
		func(ctx context.Context, in *idCuerpoHTTP[EntradaOrdenConv]) (*cuerpo[VinculoCreado], error) {
			in.Body.ID, in.Body.Clave = in.ID, in.IdempotencyKey
			return responder(k.CrearOrden.Ejecutar(ctx, in.Body))
		})
	huma.Register(api, creada("crearProspectoConversacion", "crear-prospecto", "Crear un prospecto desde la conversación", nucleo.PermProspectosCrear),
		func(ctx context.Context, in *idCuerpoHTTP[EntradaProspectoConv]) (*cuerpo[VinculoCreado], error) {
			in.Body.ID, in.Body.ClaveIdempotencia = in.ID, in.IdempotencyKey
			return responder(k.CrearProspec.Ejecutar(ctx, in.Body))
		})
	bol := creada("registrarBoletaConversacion", "boletas", "Mandar a Argos la boleta del cliente", nucleo.PermConversacionesRegistrarBoleta)
	bol.Description += " Queda `por_enviar` y sale a Argos (POST /comprobantes) con reintentos; caja la aprueba o rechaza en Argos. 422 `sin_cliente`, `adjunto_no_listo`."
	huma.Register(api, bol, func(ctx context.Context, in *idCuerpoHTTP[EntradaBoleta]) (*cuerpo[Boleta], error) {
		in.Body.ID = in.ID
		return responder(k.Boleta.Ejecutar(ctx, in.Body))
	})
	huma.Register(api, op("simularPromesaConversacion", http.MethodPost, base+"/{id}/promesas/simular", "Política de Argos para una promesa (sin crear nada)",
		nucleo.PermConversacionesRegistrarPromesa, http.StatusOK, http.StatusNotFound),
		func(ctx context.Context, in *idCuerpoHTTP[EntradaSimularPromesa]) (*cuerpo[SimulacionPromesaConv], error) {
			in.Body.ID = in.ID
			return responder(k.Promesas.Simular(ctx, in.Body))
		})
	pro := creada("registrarPromesaConversacion", "promesas", "Pedir a Argos una promesa de pago", nucleo.PermConversacionesRegistrarPromesa)
	pro.Description += " Argos decide (activa, rechazada o pendiente_aprobacion) y la respuesta lo dice. 409 `promesa_abierta`; 503 si Argos no responde (nada queda a medias)."
	huma.Register(api, pro, func(ctx context.Context, in *idCuerpoHTTP[EntradaPromesa]) (*cuerpo[PromesaConv], error) {
		in.Body.ID, in.Body.Clave = in.ID, in.IdempotencyKey
		return responder(k.Promesas.Crear(ctx, in.Body))
	})
}

var _ = uuid.Nil
