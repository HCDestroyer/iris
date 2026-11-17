import { api, COOKIE_CSRF, CABECERA_CSRF, leerCookie, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { CABECERA_EMPRESA, empresaGuardada } from "@/lib/empresa";
import type { EntradaAjustesVoz, EntradaLlamadasCanal, ResultadoLlamada } from "./tipos";

/* Llamadas del panel de voz (contratos/voz.md §3), con el cliente tipado. */

const BASE = "/api/v1/mensajeria/llamadas";
const idem = (clave = nuevaClaveIdempotencia()) => ({ "Idempotency-Key": clave });

// --- «Recibir llamadas» (por usuario y navegador) ----------------------------------------------

export function leerRecibir(navegador: string, signal?: AbortSignal) {
    return resolver(api().GET(`${BASE}/recibir`, { params: { query: { navegador } }, signal }));
}

export function encenderRecibir(navegador: string) {
    return resolver(api().POST(`${BASE}/recibir/encender`, { params: { header: idem() }, body: { navegador } }));
}

export function apagarRecibir(navegador: string) {
    return resolver(api().POST(`${BASE}/recibir/apagar`, { params: { header: idem() }, body: { navegador } }));
}

// --- La llamada --------------------------------------------------------------------------------

/** La llamada viva del usuario, o `null` (204). */
export async function leerLlamadaActiva(signal?: AbortSignal) {
    return (await resolver(api().GET(`${BASE}/activa`, { signal }))) ?? null;
}

export function leerLlamada(id: string, signal?: AbortSignal) {
    return resolver(api().GET(`${BASE}/{id}`, { params: { path: { id } }, signal }));
}

/** Atómico: gana quien actualiza la fila. Es idempotente por naturaleza (no lleva `Idempotency-Key`). */
export function reclamarLlamada(id: string, signal?: AbortSignal) {
    return resolver(api().POST(`${BASE}/{id}/reclamar`, { params: { path: { id } }, signal }));
}

export function contestarLlamada(id: string, sdpRespuesta: string, clave: string, signal?: AbortSignal) {
    return resolver(api().POST(`${BASE}/{id}/contestar`, { params: { path: { id }, header: idem(clave) }, body: { sdpRespuesta }, signal }));
}

export function confirmarMedio(id: string, clave: string, signal?: AbortSignal) {
    return resolver(api().POST(`${BASE}/{id}/confirmar-medio`, { params: { path: { id }, header: idem(clave) }, signal }));
}

export function soltarLlamada(id: string, motivo: string) {
    return resolver(api().POST(`${BASE}/{id}/soltar`, { params: { path: { id }, header: idem() }, body: { motivo } }));
}

export function colgarLlamada(id: string, clave: string) {
    return resolver(api().POST(`${BASE}/{id}/colgar`, { params: { path: { id }, header: idem(clave) } }));
}

/** Deja de sonarle a este usuario (no la rechaza para todos). */
export function rechazarLlamada(id: string) {
    return resolver(api().POST(`${BASE}/{id}/rechazar`, { params: { path: { id }, header: idem() }, body: {} }));
}

export function anotarLlamada(id: string, nota: string, resultado: ResultadoLlamada | undefined, clave: string) {
    return resolver(api().POST(`${BASE}/{id}/anotar`, { params: { path: { id }, header: idem(clave) }, body: { nota, ...(resultado ? { resultado } : {}) } }));
}

/**
 * Al cerrarse la pestaña no hay tiempo para esperar una respuesta: `soltar` (aún sin aceptar) o
 * `colgar` (ya en curso) salen con `fetch` y `keepalive`, que el navegador termina de mandar aunque
 * la página ya no exista. Lleva a mano lo que el cliente pone solo (CSRF y empresa activa).
 */
export function avisarAlCerrar(id: string, accion: "soltar" | "colgar"): void {
    if (typeof fetch === "undefined" || typeof document === "undefined") return;
    const cabeceras: Record<string, string> = { "Content-Type": "application/json", "Idempotency-Key": nuevaClaveIdempotencia() };
    const csrf = leerCookie(COOKIE_CSRF, document.cookie);
    if (csrf) cabeceras[CABECERA_CSRF] = csrf;
    const empresa = empresaGuardada();
    if (empresa !== undefined) cabeceras[CABECERA_EMPRESA] = String(empresa);
    try {
        void fetch(`${BASE}/${encodeURIComponent(id)}/${accion}`, {
            method: "POST",
            credentials: "include",
            keepalive: true,
            headers: cabeceras,
            body: accion === "soltar" ? JSON.stringify({ motivo: "pestana_cerrada" }) : "{}",
        }).catch(() => undefined);
    } catch {
        // Sin red al cerrar: el servidor la termina solo a los 15 s sin el flujo del usuario.
    }
}

// --- Devoluciones ------------------------------------------------------------------------------

export function leerDevoluciones(signal?: AbortSignal) {
    return resolver(api().GET(`${BASE}/devoluciones`, { signal }));
}

export function descartarDevolucion(id: number, motivo: string, clave: string) {
    return resolver(api().POST(`${BASE}/devoluciones/{id}/descartar`, { params: { path: { id }, header: idem(clave) }, body: { motivo } }));
}

// --- Ajustes -----------------------------------------------------------------------------------

const CANAL = "/api/v1/mensajeria/canales/{id}/llamadas";

export function leerLlamadasCanal(id: number, signal?: AbortSignal) {
    return resolver(api().GET(CANAL, { params: { path: { id } }, signal }));
}

export function configurarLlamadasCanal(id: number, entrada: EntradaLlamadasCanal, clave: string) {
    return resolver(api().POST(`${CANAL}/configurar`, { params: { path: { id }, header: idem(clave) }, body: entrada }));
}

export function comprobarLlamadasCanal(id: number) {
    return resolver(api().POST(`${CANAL}/comprobar`, { params: { path: { id }, header: idem() } }));
}

export function sincronizarLlamadasCanal(id: number) {
    return resolver(api().POST(`${CANAL}/sincronizar`, { params: { path: { id }, header: idem() } }));
}

export function leerAjustesVoz(signal?: AbortSignal) {
    return resolver(api().GET("/api/v1/mensajeria/voz/ajustes", { signal }));
}

export function guardarAjustesVoz(entrada: EntradaAjustesVoz, clave: string) {
    return resolver(api().POST("/api/v1/mensajeria/voz/ajustes/actualizar", { params: { header: idem(clave) }, body: entrada }));
}
