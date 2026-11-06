import { crearGeneradorUuid7 } from "@iris/sync";
import { api, resolver } from "../api/cliente";
import type { Filtros } from "./estado";
import type { EventoBandeja } from "./tipos";

/*
 * Llamadas de la bandeja (contratos/conversaciones.md, base `/api/v1/mensajeria`). Todas con el
 * cliente generado de packages/contratos; las acciones con `Idempotency-Key`.
 */

const BASE = "/api/v1/mensajeria/conversaciones";

/** Id de mensaje generado en el panel (UUID v7): reintentar con el mismo id no lo duplica. */
export const nuevoIdMensaje = crearGeneradorUuid7();

export const cabeceraIdem = (clave: string) => ({ "Idempotency-Key": clave });

export function leerLista(f: Filtros, cursor: string | undefined, signal?: AbortSignal) {
    return resolver(
        api().GET(BASE, {
            params: {
                query: {
                    vista: f.vista,
                    estado: f.estado,
                    ...(f.canalId ? { canalId: f.canalId } : {}),
                    ...(f.colaId ? { colaId: f.colaId } : {}),
                    ...(f.etiquetaId ? { etiquetaId: f.etiquetaId } : {}),
                    ...(f.sla ? { sla: f.sla } : {}),
                    ...(f.q.trim() ? { q: f.q.trim() } : {}),
                    ...(cursor ? { cursor } : {}),
                    limit: 30,
                },
            },
            signal,
        }),
    );
}

export function leerContadores(signal?: AbortSignal) {
    return resolver(api().GET(`${BASE}/contadores`, { signal }));
}

export function leerDetalle(id: number, signal?: AbortSignal) {
    return resolver(api().GET(`${BASE}/{id}`, { params: { path: { id } }, signal }));
}

export function leerMensajes(id: number, cursor: string | undefined, signal?: AbortSignal) {
    return resolver(
        api().GET(`${BASE}/{id}/mensajes`, {
            params: { path: { id }, query: { direccion: "anteriores", limit: 50, ...(cursor ? { cursor } : {}) } },
            signal,
        }),
    );
}

/** `GET …/cambios`: lanza `ErrorApi` con el estado (410, 401…) para que el feed decida. */
export async function leerCambios(cursor: string | undefined, signal: AbortSignal) {
    const r = await resolver(api().GET(`${BASE}/cambios`, { params: { query: { ...(cursor ? { cursor } : {}), limit: 100 } }, signal }));
    return { data: r.data ?? [], nextCursor: r.nextCursor, hayMas: r.hayMas ?? false };
}

/**
 * `GET …/eventos` como flujo: con `fetch` para mandar `X-Iris-Empresa`, `Last-Event-ID` y leer el
 * estado. `parseAs: "stream"` deja el cuerpo sin leer.
 */
export async function abrirFlujo(cursor: string | undefined, signal: AbortSignal): Promise<Response> {
    const { response } = await api().GET(`${BASE}/eventos`, {
        params: { header: cursor ? { "Last-Event-ID": cursor } : {} },
        headers: { Accept: "text/event-stream" },
        parseAs: "stream",
        signal,
    });
    return response;
}

/** Evento del flujo SSE (`data:` en JSON); `null` si no se entiende. */
export function leerEventoFlujo(datos: string): EventoBandeja | null {
    try {
        const v: unknown = JSON.parse(datos);
        return typeof v === "object" && v !== null && typeof (v as { id?: unknown }).id === "string" ? (v as EventoBandeja) : null;
    } catch {
        return null;
    }
}
