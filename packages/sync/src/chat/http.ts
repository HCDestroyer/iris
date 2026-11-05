// `TransporteChat` sobre el cliente `openapi-fetch` de `@iris/contratos` (rutas y formas del
// OpenAPI de la API): `/api/v1/mensajeria/chat/*`. Como el resto de la app de campo, cada petición
// lleva `X-Iris-Canal: campo` y, con `app`, `X-Iris-App`. Quien crea el cliente pone la sesión (cookie
// + CSRF) y la empresa fija (`X-Iris-Empresa`).
import type { ClienteIris } from "@iris/contratos";
import { ErrorTransporte } from "../errores";
import { conApp, conHora, resolver, type IdentidadApp } from "../http";
import type { ConversacionChatDetalle, MensajeChat, TransporteChat } from "./tipos";

const BASE = "/api/v1/mensajeria/chat" as const;

/** El cuerpo de `POST /mensajes` (201, o 200 en un reenvío: el OpenAPI no lo declara, pero viene). */
function mensajeDe(datos: MensajeChat | undefined): MensajeChat {
    if (!datos || typeof datos !== "object" || typeof datos.id !== "string") {
        throw new ErrorTransporte("http", "Respuesta sin el mensaje", 502);
    }
    return datos;
}

/**
 * Abrir responde 200 (existente) o 201 (nueva) con la conversación; el OpenAPI declara el 201 como
 * `Problema` (pedido a I1 en `i4.md`), así que se comprueba la forma.
 */
function conversacionDe(datos: unknown): ConversacionChatDetalle {
    const c = datos as Partial<ConversacionChatDetalle> | undefined;
    if (!c || typeof c.id !== "number" || typeof c.tipo !== "string") {
        throw new ErrorTransporte("http", "Respuesta sin la conversación", 502);
    }
    return c as ConversacionChatDetalle;
}

export function crearTransporteChatApi(cliente: ClienteIris, app?: IdentidadApp): TransporteChat {
    const h = () => conApp(app);
    return {
        async listarConversaciones(cursor) {
            const { datos } = await resolver(
                cliente.GET(`${BASE}/conversaciones`, { params: { query: { limit: 50, ...(cursor ? { cursor } : {}) } }, headers: h() }),
            );
            return datos;
        },
        async verConversacion(id) {
            const { datos } = await resolver(cliente.GET(`${BASE}/conversaciones/{id}`, { params: { path: { id } }, headers: h() }));
            return datos;
        },
        async abrirDirecta(usuarioId) {
            const { datos } = await resolver(cliente.POST(`${BASE}/directas`, { body: { usuario_id: usuarioId }, headers: h() }));
            return conversacionDe(datos);
        },
        async abrirCuadrilla(cuadrillaId) {
            const { datos } = await resolver(cliente.POST(`${BASE}/cuadrillas/{cuadrilla_id}/abrir`, { params: { path: { cuadrilla_id: cuadrillaId } }, headers: h() }));
            return conversacionDe(datos);
        },
        async abrirOrden(ordenId) {
            const { datos } = await resolver(cliente.POST(`${BASE}/ordenes/{orden_id}/abrir`, { params: { path: { orden_id: ordenId } }, headers: h() }));
            return conversacionDe(datos);
        },
        async listarMensajes(conversacionId, c) {
            const query = {
                ...(c.direccion ? { direccion: c.direccion } : {}),
                ...(c.cursor ? { cursor: c.cursor } : {}),
                ...(c.desdeSeq !== undefined ? { desde_seq: c.desdeSeq } : {}),
                limit: c.limit ?? 30,
            };
            const { datos } = await resolver(cliente.GET(`${BASE}/conversaciones/{id}/mensajes`, { params: { path: { id: conversacionId }, query }, headers: h() }));
            return datos;
        },
        async enviarMensaje(conversacionId, cuerpo) {
            const { datos, respuesta } = await resolver(
                cliente.POST(`${BASE}/conversaciones/{id}/mensajes`, {
                    params: { path: { id: conversacionId }, header: { "Idempotency-Key": cuerpo.id } },
                    body: cuerpo,
                    headers: h(),
                }),
            );
            return { mensaje: mensajeDe(datos as MensajeChat | undefined), reenvio: respuesta.status === 200 || respuesta.headers.get("Idempotent-Replayed") === "true" };
        },
        async eliminarMensaje(mensajeId) {
            const { datos } = await resolver(cliente.POST(`${BASE}/mensajes/{id}/eliminar`, { params: { path: { id: mensajeId } }, headers: h() }));
            return mensajeDe(datos);
        },
        async leer(conversacionId, hastaMensajeId) {
            const { datos } = await resolver(
                cliente.POST(`${BASE}/conversaciones/{id}/leer`, { params: { path: { id: conversacionId } }, body: { hasta_mensaje_id: hastaMensajeId }, headers: h() }),
            );
            return { no_leidos: datos.no_leidos, leido_hasta_mensaje_id: datos.leido_hasta_mensaje_id ?? null };
        },
        async cambios(cursor) {
            const { datos, respuesta } = await resolver(
                cliente.GET(`${BASE}/cambios`, { params: { query: { limit: 100, ...(cursor ? { cursor } : {}) } }, headers: h() }),
            );
            return conHora(datos, respuesta);
        },
    };
}
