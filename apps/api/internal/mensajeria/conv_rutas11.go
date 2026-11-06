package mensajeria

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/HCDestroyer/iris/apps/api/internal/nucleo"
)

// Rutas de la bandeja 1.1 (contratos/conversaciones.md §14). Van antes de /conversaciones/{id}.

type salidaIniciada struct {
	Status   int
	Repetido string `header:"Idempotent-Replayed" doc:"true si el id ya se había recibido"`
	Body     ConversacionIniciada
}

func (m *Modulo) registrarBandeja11(api huma.API) {
	k := m.Conv
	const base = "/conversaciones"
	ver, iniciar, canalesConf := nucleo.PermConversacionesVer, nucleo.PermConversacionesIniciar, nucleo.PermCanalesConfigurar

	// Iniciar una conversación (§14.3).
	ini := op("iniciarConversacion", http.MethodPost, base+"/iniciar", "Escribir primero con una plantilla aprobada", iniciar,
		http.StatusCreated, http.StatusNotFound, http.StatusConflict)
	ini.Description += " A un cliente de Argos o un contacto de Iris con permiso de contacto, desde un número de tus colas. Idempotente por `id` " +
		"(200 + Idempotent-Replayed si se repite; 409 `mensaje_id_reutilizado` con otro contenido). Reutiliza la conversación del número " +
		"(409 `conversacion_de_otro` si la tiene otro agente). 422 `numero_no_permitido`, `sin_contacto`, `sin_permiso`, `baja`, " +
		"`plantilla_no_aprobada`, `cliente_fuera_de_empresa`, `servicio_fuera_de_empresa`."
	ini.Responses = map[string]*huma.Response{"200": {Description: "Reenvío del mismo id: la conversación y el mensaje tal como están ahora"}}
	huma.Register(api, ini, func(ctx context.Context, in *cuerpoHTTP[EntradaIniciarConv]) (*salidaIniciada, error) {
		in.Body.IdempotencyKey = in.IdempotencyKey
		r, err := k.Iniciar.Ejecutar(ctx, in.Body)
		if err != nil {
			return nil, err
		}
		s := &salidaIniciada{Status: http.StatusCreated, Body: r}
		if r.Repetido {
			s.Status, s.Repetido = http.StatusOK, "true"
		}
		return s, nil
	})
	reenvioConCuerpo(api, ini.OperationID)
	dest := op("destinatariosConversacion", http.MethodGet, base+"/destinatarios", "Buscar a quién escribir (clientes de Argos y contactos de Iris)",
		iniciar, http.StatusOK)
	dest.Description += " Hasta 20, con `puede` y `motivo` de cada uno (los códigos de iniciar). Con `canalId` evalúa también el número de salida."
	huma.Register(api, dest, func(ctx context.Context, in *EntradaDestinatarios) (*cuerpo[ListaDestinatarios], error) {
		return responder(k.Destinatarios.Ejecutar(ctx, *in))
	})
	huma.Register(api, op("destinatarioConversacion", http.MethodGet, base+"/destinatario", "Si se le puede escribir a un cliente o contacto",
		iniciar, http.StatusOK, http.StatusNotFound), func(ctx context.Context, in *EntradaDestinatario) (*cuerpo[Destinatario], error) {
		return responder(k.Destinatario.Ejecutar(ctx, *in))
	})
	huma.Register(api, op("plantillasNumeroConversaciones", http.MethodGet, base+"/canales/{id}/plantillas", "Plantillas aprobadas en un número de tus colas",
		iniciar, http.StatusOK, http.StatusNotFound), func(ctx context.Context, in *EntradaCanalConv) (*cuerpo[ListaPlantillasConv], error) {
		return responder(k.PlantillasCanal.Ejecutar(ctx, *in))
	})

	// Disponibilidad y ajustes (§14.5).
	huma.Register(api, op("verDisponibilidadConversaciones", http.MethodGet, base+"/disponibilidad", "Tu disponibilidad en la bandeja", ver, http.StatusOK),
		func(ctx context.Context, _ *struct{}) (*cuerpo[MiDisponibilidad], error) {
			return responder(k.VerDisp.Ejecutar(ctx, struct{}{}))
		})
	fd := op("fijarDisponibilidadConversaciones", http.MethodPost, base+"/disponibilidad", "Ponerte disponible o no disponible", ver,
		http.StatusOK, http.StatusConflict)
	fd.Description += " `version` la del GET (0 la primera vez; 409 `conflicto_version`). Pasa sola a no_disponible al cerrar sesión, por inactividad o al terminar tu horario."
	huma.Register(api, fd, func(ctx context.Context, in *cuerpoHTTP[EntradaDisponibilidad]) (*cuerpo[MiDisponibilidad], error) {
		return responder(k.FijarDisp.Ejecutar(ctx, in.Body))
	})
	lat := op("latidoConversaciones", http.MethodPost, base+"/disponibilidad/latido", "Actividad del panel (cada ~60 s)", ver, http.StatusOK)
	lat.Description += " Sin cuerpo. Solo anota la actividad (no audita, no emite eventos ni cambia el estado) y responde tu disponibilidad."
	huma.Register(api, lat, func(ctx context.Context, _ *struct{}) (*cuerpo[MiDisponibilidad], error) {
		r, err := k.Latido.Ejecutar(ctx, struct{}{})
		return responder(r.MiDisponibilidad, err)
	})
	huma.Register(api, op("verAjustesConversaciones", http.MethodGet, base+"/ajustes", "Ajustes de la bandeja de la empresa", ver, http.StatusOK),
		func(ctx context.Context, _ *struct{}) (*cuerpo[AjustesBandeja], error) {
			return responder(k.VerAjustes.Ejecutar(ctx, struct{}{}))
		})
	huma.Register(api, op("fijarAjustesConversaciones", http.MethodPost, base+"/ajustes", "Cambiar los ajustes de la bandeja", canalesConf,
		http.StatusOK, http.StatusConflict), func(ctx context.Context, in *cuerpoHTTP[EntradaAjustesBandeja]) (*cuerpo[AjustesBandeja], error) {
		return responder(k.FijarAjustes.Ejecutar(ctx, in.Body))
	})
}
