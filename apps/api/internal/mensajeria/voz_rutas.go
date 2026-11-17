package mensajeria

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Rutas de la voz, etapa E1 (contratos/voz.md §3). Handlers delgados.

type llamadaHTTP struct {
	nucleo.ConIdempotencia
	ID uuid.UUID `path:"id" format:"uuid"`
}

type llamadaLecturaHTTP struct {
	ID uuid.UUID `path:"id" format:"uuid"`
}

type cuerpoContestar struct {
	SDPRespuesta string `json:"sdpRespuesta" minLength:"1" maxLength:"8192" doc:"La respuesta SDP del RTCPeerConnection del navegador"`
}

type cuerpoSoltar struct {
	Motivo string `json:"motivo,omitempty" maxLength:"100" doc:"Por qué no se pudo contestar (p. ej. sin_microfono, medio_no_conecta)"`
}

type cuerpoRechazar struct {
	Motivo    string `json:"motivo,omitempty" maxLength:"100"`
	ParaTodos bool   `json:"paraTodos,omitempty" doc:"Rechazarla para todos (solo quien supervisa): WhatsApp deja de sonar"`
}

type cuerpoAnotar struct {
	Nota      string `json:"nota" minLength:"1" maxLength:"2000"`
	Resultado string `json:"resultado,omitempty" enum:"resuelto,ticket_creado,devolver,equivocado,otro"`
}

type cuerpoMotivo struct {
	Motivo string `json:"motivo" minLength:"1" maxLength:"300"`
}

type llamadaCuerpoHTTP[T any] struct {
	nucleo.ConIdempotencia
	ID   uuid.UUID `path:"id" format:"uuid"`
	Body T
}

type activaHTTP struct {
	Status int
	Body   *Llamada
}

type conversacionLlamadasHTTP struct {
	ID     int64  `path:"id" minimum:"1"`
	Cursor string `query:"cursor" required:"false"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"30"`
}

func (m *Modulo) registrarVoz(api huma.API) {
	k := m.Voz
	ver, contestar, rechazar, colgar, anotar := nucleo.PermLlamadasVer, nucleo.PermLlamadasContestar, nucleo.PermLlamadasRechazar,
		nucleo.PermLlamadasColgar, nucleo.PermLlamadasAnotar
	voz := func(o huma.Operation, detalle string) huma.Operation {
		o.Description += " " + detalle + " Solo personas con sesión del panel y conversaciones.ver; lo que no se ve responde 404. Contrato: `.claude/integration/contratos/voz.md`."
		return o
	}

	// Consultas ---------------------------------------------------------------------------------
	huma.Register(api, voz(op("listarLlamadas", http.MethodGet, "/llamadas", "Llamadas de las conversaciones que ves", ver, http.StatusOK),
		"De la más nueva a la más vieja, con cursor."),
		func(ctx context.Context, in *EntradaListarLlamadas) (*cuerpo[nucleo.Pagina[Llamada]], error) {
			return responder(k.Listar.Ejecutar(ctx, *in))
		})
	act := voz(op("llamadaActiva", http.MethodGet, "/llamadas/activa", "La llamada que tienes ahora", ver, http.StatusOK),
		"La que reclamaste o tienes en curso, o 204 si no tienes ninguna. El audio no se puede retomar desde otra pestaña.")
	act.Responses = map[string]*huma.Response{"204": {Description: "No tienes una llamada viva"}}
	huma.Register(api, act, func(ctx context.Context, _ *struct{}) (*activaHTTP, error) {
		l, err := k.Activa.Ejecutar(ctx, struct{}{})
		if err != nil {
			return nil, err
		}
		if l == nil {
			return &activaHTTP{Status: http.StatusNoContent}, nil
		}
		return &activaHTTP{Status: http.StatusOK, Body: l}, nil
	})
	huma.Register(api, voz(op("verRecibirLlamadas", http.MethodGet, "/llamadas/recibir", "¿Este navegador recibe llamadas?", contestar, http.StatusOK),
		"`recibe`: el interruptor de este navegador; `vivo`: hay un flujo de eventos abierto con latido reciente (sin él no suena nada)."),
		func(ctx context.Context, in *EntradaVerRecibir) (*cuerpo[RecibirLlamadas], error) {
			return responder(k.VerRecibir.Ejecutar(ctx, *in))
		})
	for _, r := range []struct {
		id, ruta, resumen string
		recibe            bool
	}{{"encenderRecibirLlamadas", "/llamadas/recibir/encender", "Encender «Recibir llamadas» en este navegador", true},
		{"apagarRecibirLlamadas", "/llamadas/recibir/apagar", "Apagar «Recibir llamadas» en este navegador", false}} {
		huma.Register(api, voz(op(r.id, http.MethodPost, r.ruta, r.resumen, contestar, http.StatusOK),
			"Un interruptor por usuario y navegador (`navegador`: un id que el panel genera y guarda). El panel pide el micrófono al encenderlo. "+
				"Solo le suena a quien lo tiene encendido, está disponible y tiene abierto el flujo `GET /conversaciones/eventos`."),
			func(ctx context.Context, in *cuerpoHTTP[EntradaRecibir]) (*cuerpo[RecibirLlamadas], error) {
				in.Body.recibe = r.recibe
				return responder(k.Recibir.Ejecutar(ctx, in.Body))
			})
	}
	huma.Register(api, voz(op("listarDevolucionesLlamada", http.MethodGet, "/llamadas/devoluciones", "Llamadas por devolver", ver, http.StatusOK),
		"Una pendiente por conversación; la crea una llamada perdida y vence a las 72 h."),
		func(ctx context.Context, in *EntradaListarDevoluciones) (*cuerpo[nucleo.Pagina[DevolucionLlamada]], error) {
			return responder(k.Devoluciones.Ejecutar(ctx, *in))
		})
	huma.Register(api, voz(op("descartarDevolucionLlamada", http.MethodPost, "/llamadas/devoluciones/{id}/descartar", "Cerrar una devolución sin llamar",
		nucleo.PermLlamadasDescartarDevolucion, http.StatusOK, http.StatusNotFound, http.StatusConflict), "409 `devolucion_cerrada` si ya no está pendiente. Emite `llamada.devolucion_cerrada`."),
		func(ctx context.Context, in *idCuerpoHTTP[cuerpoMotivo]) (*cuerpo[DevolucionLlamada], error) {
			return responder(k.Descartar.Ejecutar(ctx, EntradaDescartarDevolucion{ID: in.ID, Motivo: in.Body.Motivo}))
		})
	huma.Register(api, voz(op("verLlamada", http.MethodGet, "/llamadas/{id}", "Ver una llamada", ver, http.StatusOK, http.StatusNotFound),
		"Nunca trae el SDP, el id de la llamada en Meta ni el token. `puede` lo calcula el servidor para ti."),
		func(ctx context.Context, in *llamadaLecturaHTTP) (*cuerpo[Llamada], error) {
			return responder(k.Ver.Ejecutar(ctx, EntradaLlamada{ID: in.ID}))
		})
	huma.Register(api, voz(op("listarLlamadasDeConversacion", http.MethodGet, "/conversaciones/{id}/llamadas", "Llamadas de una conversación", ver, http.StatusOK, http.StatusNotFound), ""),
		func(ctx context.Context, in *conversacionLlamadasHTTP) (*cuerpo[nucleo.Pagina[Llamada]], error) {
			return responder(k.Listar.Ejecutar(ctx, EntradaListarLlamadas{ConversacionID: in.ID, Cursor: in.Cursor, Limit: in.Limit}))
		})

	// Contestar una entrante (§3.3) ---------------------------------------------------------------
	huma.Register(api, voz(op("reclamarLlamada", http.MethodPost, "/llamadas/{id}/reclamar", "Reclamar una llamada que suena", contestar,
		http.StatusOK, http.StatusNotFound, http.StatusConflict),
		"Atómico: gana uno solo. Responde `{ llamada, sdpOferta, ice, venceEn }`: aplica `sdpOferta` a un `RTCPeerConnection` y manda `contestar` antes de "+
			"`venceEn` (12 s). Solo un candidato de **esa** llamada puede reclamarla (si no, 404). 409 `llamada_ya_contestada` si otro se adelantó o ya no suena "+
			"(`errors[0].rule = misma_persona` si fuiste tú en otra pestaña: solo cierra el aviso). 422 `no_recibe_llamadas` sin «Recibir llamadas» o sin el flujo de "+
			"eventos abierto. La respuesta no se guarda por `Idempotency-Key` (lleva el SDP): repetir devuelve la misma, releída de la llamada."),
		func(ctx context.Context, in *llamadaHTTP) (*cuerpo[LlamadaReclamada], error) {
			nucleo.NoRepetible(ctx)
			return responder(k.Reclamar.Ejecutar(ctx, EntradaLlamada{ID: in.ID}))
		})
	huma.Register(api, voz(op("contestarLlamada", http.MethodPost, "/llamadas/{id}/contestar", "Contestar: mandar la respuesta SDP", contestar,
		http.StatusOK, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway),
		"Solo quien la reclamó (cualquier otro, 404). El servidor valida el SDP (422 `sdp_invalido`: una sola sección de audio, solo Opus y telephone-event, "+
			"sin candidatos a redes internas, máximo 8 KB) y lo manda a WhatsApp (`pre_accept`). Después tienes 5 s para `confirmar-medio`. "+
			"409 `reserva_vencida` si se pasó el tiempo; 502 `meta_rechazo` si WhatsApp no lo aceptó (la llamada vuelve a sonar)."),
		func(ctx context.Context, in *llamadaCuerpoHTTP[cuerpoContestar]) (*cuerpo[Llamada], error) {
			return responder(m.Contestar(ctx, EntradaContestar{ID: in.ID, SDPRespuesta: in.Body.SDPRespuesta}))
		})
	huma.Register(api, voz(op("confirmarMedioLlamada", http.MethodPost, "/llamadas/{id}/confirmar-medio", "Confirmar que el audio conectó", contestar,
		http.StatusOK, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway),
		"Cuando el `RTCPeerConnection` está `connected`. El servidor manda `accept` y la llamada pasa a `en_curso` (`llamada.contestada`). "+
			"409 `reserva_vencida` pasados 5 s desde `contestar`; 502 `meta_rechazo` (la llamada queda `fallida`)."),
		func(ctx context.Context, in *llamadaHTTP) (*cuerpo[Llamada], error) {
			return responder(m.ConfirmarMedio(ctx, EntradaLlamada{ID: in.ID}))
		})
	huma.Register(api, voz(op("soltarLlamada", http.MethodPost, "/llamadas/{id}/soltar", "Soltar la llamada que reclamaste", contestar,
		http.StatusOK, http.StatusNotFound, http.StatusConflict),
		"Algo falló antes de contestar (micrófono, medio): vuelve a sonar a los demás. A la segunda vez dejas de recibir esa llamada."),
		func(ctx context.Context, in *llamadaCuerpoHTTP[cuerpoSoltar]) (*cuerpo[Llamada], error) {
			return responder(k.Soltar.Ejecutar(ctx, EntradaSoltar{ID: in.ID, Motivo: in.Body.Motivo}))
		})
	huma.Register(api, voz(op("colgarLlamada", http.MethodPost, "/llamadas/{id}/colgar", "Colgar", colgar, http.StatusOK, http.StatusNotFound, http.StatusConflict),
		"Quien la tiene, o quien supervisa. 409 `llamada_terminada` si ya terminó. Emite `llamada.terminada`."),
		func(ctx context.Context, in *llamadaHTTP) (*cuerpo[Llamada], error) {
			return responder(m.Colgar(ctx, EntradaLlamada{ID: in.ID}))
		})
	huma.Register(api, voz(op("rechazarLlamada", http.MethodPost, "/llamadas/{id}/rechazar", "Rechazar una llamada que suena", rechazar,
		http.StatusOK, http.StatusNotFound, http.StatusConflict),
		"Por defecto solo deja de sonarte a ti. Con `paraTodos: true` (quien supervisa) la rechaza en WhatsApp y queda `rechazada`."),
		func(ctx context.Context, in *llamadaCuerpoHTTP[cuerpoRechazar]) (*cuerpo[Llamada], error) {
			return responder(m.Rechazar(ctx, EntradaRechazar{ID: in.ID, Motivo: in.Body.Motivo, ParaTodos: in.Body.ParaTodos}))
		})
	huma.Register(api, voz(op("anotarLlamada", http.MethodPost, "/llamadas/{id}/anotar", "Nota y resultado de una llamada", anotar,
		http.StatusOK, http.StatusNotFound, http.StatusConflict),
		"Quien la atendió o quien supervisa. La nota también queda en el hilo como nota interna. Emite `llamada.anotada` (sin la nota)."),
		func(ctx context.Context, in *llamadaCuerpoHTTP[cuerpoAnotar]) (*cuerpo[Llamada], error) {
			return responder(k.Anotar.Ejecutar(ctx, EntradaAnotarLlamada{ID: in.ID, Nota: in.Body.Nota, Resultado: in.Body.Resultado}))
		})

	// Ajustes -------------------------------------------------------------------------------------
	conf := nucleo.PermLlamadasConfigurar
	huma.Register(api, voz(op("verLlamadasCanal", http.MethodGet, "/canales/{id}/llamadas", "Ajustes de llamadas de un número", conf, http.StatusOK, http.StatusNotFound),
		"Sin configurar: los de fábrica con `version: 0`. `disponible: false` en un número por QR."),
		func(ctx context.Context, in *struct {
			ID int64 `path:"id" minimum:"1"`
		}) (*cuerpo[LlamadasCanal], error) {
			return responder(k.VerCanal.Ejecutar(ctx, EntradaIDConv{ID: in.ID}))
		})
	huma.Register(api, voz(op("configurarLlamadasCanal", http.MethodPost, "/canales/{id}/llamadas/configurar", "Configurar las llamadas de un número", conf,
		http.StatusOK, http.StatusNotFound, http.StatusConflict),
		"422 `llamadas_no_disponibles_qr` al encender en un número por QR; 422 `requisitos_sin_cumplir` (con `errors[]` por requisito) si la última "+
			"comprobación no cumple; 422 `asistente_voz_no_disponible` con `fueraDeHorario: asistente` (llega en otra etapa). Emite `canal_mensajeria.llamadas_cambiadas`."),
		func(ctx context.Context, in *idCuerpoHTTP[EntradaLlamadasCanal]) (*cuerpo[LlamadasCanal], error) {
			in.Body.ID = in.ID
			return responder(k.ConfigCanal.Ejecutar(ctx, in.Body))
		})
	for _, r := range []struct {
		id, ruta, resumen, detalle string
		sincronizar                bool
	}{{"comprobarLlamadasCanal", "/canales/{id}/llamadas/comprobar", "Comprobar los requisitos de WhatsApp para tener llamadas",
		"Pregunta a WhatsApp y guarda el resultado en `requisitos` (`conexion_api`, `limite_mensajeria`, `webhook_calls`, `calling_habilitado` y, como aviso, " +
			"`plantilla_permiso`). Nunca el mensaje crudo de Meta.", false},
		{"sincronizarLlamadasCanal", "/canales/{id}/llamadas/sincronizar", "Traer de WhatsApp los ajustes de llamadas del número",
			"Como `comprobar` y además copia a Iris el icono de llamada y la devolución que tiene el número en WhatsApp.", true}} {
		huma.Register(api, voz(op(r.id, http.MethodPost, r.ruta, r.resumen, conf, http.StatusOK, http.StatusNotFound), r.detalle),
			func(ctx context.Context, in *idHTTP) (*cuerpo[LlamadasCanal], error) {
				return responder(m.ComprobarLlamadas(ctx, in.ID, r.sincronizar))
			})
	}
	vozConf := nucleo.PermVozConfigurar
	huma.Register(api, voz(op("verAjustesVoz", http.MethodGet, "/voz/ajustes", "Ajustes de voz de la empresa", vozConf, http.StatusOK),
		"E1: grabación, duración máxima de una llamada, cuánto suena primero a quien tiene la conversación y retención de grabaciones."),
		func(ctx context.Context, _ *struct{}) (*cuerpo[AjustesVoz], error) {
			return responder(k.VerAjustes.Ejecutar(ctx, struct{}{}))
		})
	huma.Register(api, voz(op("actualizarAjustesVoz", http.MethodPost, "/voz/ajustes/actualizar", "Guardar los ajustes de voz de la empresa", vozConf,
		http.StatusOK, http.StatusConflict), "`version` la que leíste (0 la primera vez): distinta, 409. Emite `voz.ajustes_cambiados`."),
		func(ctx context.Context, in *cuerpoHTTP[EntradaAjustesVoz]) (*cuerpo[AjustesVoz], error) {
			return responder(k.FijarAjustes.Ejecutar(ctx, in.Body))
		})
}
