import type { Schema } from "@iris/contratos";
import { api, COOKIE_CSRF, CABECERA_CSRF, leerCookie, resolver } from "../api/cliente";
import { ErrorApi } from "../api/errores";
import { CABECERA_EMPRESA } from "../empresa";

/*
 * Llamadas del chat del equipo que el panel hace fuera del `MotorChat` (`contratos/chat.md` §2.1 a
 * §2.3 y §3.1): contadores, miembros, «Nuevo mensaje» y el hilo de la orden sin unirse.
 */

type ConversacionChat = Schema<"ConversacionChat">;
export type MiembroChat = Schema<"MiembroChat">;
export type MotivoMiembro = MiembroChat["motivo"];
export type PersonaChat = Schema<"PersonaChat">;
export type CuadrillaChat = Schema<"CuadrillaChat">;
export type ResumenChat = Schema<"ResumenChat">;

const BASE = "/api/v1/mensajeria/chat";

/** Menciones sin leer de la conversación (§1). */
export function mencionesDe(c: ConversacionChat): number {
    return Math.max(0, c.menciones ?? 0);
}

/** Hilo de una orden cerrada, fallida o cancelada (§1). */
export function soloLecturaDe(c: ConversacionChat): boolean {
    return c.solo_lectura === true;
}

/** Nombre del tipo de la orden («Instalación»), o null. */
export function ordenTipoDe(c: ConversacionChat): string | null {
    return c.orden_tipo?.trim() ? c.orden_tipo : null;
}

/** `GET /resumen` (§2.1). */
export function pedirResumen(signal?: AbortSignal): Promise<ResumenChat> {
    return resolver(api().GET(`${BASE}/resumen`, signal ? { signal } : {}));
}

/** `GET /conversaciones/{id}/miembros` (§2.2). */
export async function pedirMiembros(conversacionId: number, signal?: AbortSignal): Promise<MiembroChat[]> {
    const r = await resolver(api().GET(`${BASE}/conversaciones/{id}/miembros`, { params: { path: { id: conversacionId } }, ...(signal ? { signal } : {}) }));
    return r.data ?? [];
}

/** `POST /conversaciones/{id}/miembros` (§2.2, `chat.supervisar`): la lista de miembros. */
export async function agregarMiembro(conversacionId: number, usuarioId: number, idempotencia: string): Promise<MiembroChat[]> {
    const r = await resolver(
        api().POST(`${BASE}/conversaciones/{id}/miembros`, { params: { path: { id: conversacionId }, header: { "Idempotency-Key": idempotencia } }, body: { usuario_id: usuarioId } }),
    );
    return r.data ?? [];
}

/** `GET /personas?q=` (§2.3). */
export async function buscarPersonas(q: string, signal?: AbortSignal): Promise<PersonaChat[]> {
    const r = await resolver(api().GET(`${BASE}/personas`, { params: { query: { limit: 50, ...(q.trim() ? { q: q.trim() } : {}) } }, ...(signal ? { signal } : {}) }));
    return r.data ?? [];
}

/** `GET /cuadrillas?q=` (§2.3). */
export async function buscarCuadrillas(q: string, signal?: AbortSignal): Promise<CuadrillaChat[]> {
    const r = await resolver(api().GET(`${BASE}/cuadrillas`, { params: { query: q.trim() ? { q: q.trim() } : {} }, ...(signal ? { signal } : {}) }));
    return r.data ?? [];
}

/** `GET /ordenes/{orden_id}/conversacion` (no crea ni une): el id del hilo, o null con 404. */
export async function hiloDeOrden(ordenId: number, signal?: AbortSignal): Promise<number | null> {
    try {
        const c = await resolver(api().GET(`${BASE}/ordenes/{orden_id}/conversacion`, { params: { path: { orden_id: ordenId } }, ...(signal ? { signal } : {}) }));
        return c.id;
    } catch (e) {
        if (e instanceof ErrorApi && e.estado === 404) return null;
        throw e;
    }
}

/**
 * §3.1, cuando la página se descarga (`pagehide`) con una eliminación pendiente:
 * `POST /mensajes/{id}/eliminar` con `keepalive` (sale aunque la página se vaya). `openapi-fetch` no
 * deja pedir `keepalive`, así que va con `fetch`, con la cookie, el CSRF y la empresa **del chat que
 * la pidió** (nunca la activa de ese momento: tras cambiar de empresa iría a otra y daría 404).
 *
 * No se puede comprobar antes con `GET /yo` que la sesión siga siendo de quien la pidió: la página se
 * está yendo y no hay tiempo de esperar otra respuesta. Si en otra pestaña entró otra persona, el
 * servidor la rechaza (solo el autor elimina: 403 `no_es_autor`) y no pasa nada. Cerrar sesión desde
 * el panel no llega aquí: antes vacía lo pendiente por la cola del motor, que sí verifica la sesión.
 */
export function eliminarAlSalir(mensajeId: string, empresaId: number | undefined): void {
    const cabeceras: Record<string, string> = {};
    if (empresaId !== undefined) cabeceras[CABECERA_EMPRESA] = String(empresaId);
    const csrf = leerCookie(COOKIE_CSRF, document.cookie);
    if (csrf) cabeceras[CABECERA_CSRF] = csrf;
    void fetch(`${BASE}/mensajes/${encodeURIComponent(mensajeId)}/eliminar`, { method: "POST", credentials: "include", keepalive: true, headers: cabeceras }).catch(() => undefined);
}
