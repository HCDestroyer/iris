import { api, COOKIE_CSRF, CABECERA_CSRF, leerCookie, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { CABECERA_EMPRESA, empresaGuardada } from "@/lib/empresa";
import { CABECERA_PESTANA, idNavegador, idPestana } from "./navegador";
import type { EntradaAjustesVoz, EntradaLlamadasCanal, ResultadoLlamada } from "./tipos";

/* Llamadas del panel de voz (contratos/voz.md §3), con el cliente tipado. */

const BASE = "/api/v1/mensajeria/llamadas";
const idem = (clave = nuevaClaveIdempotencia()) => ({ "Idempotency-Key": clave });
/**
 * La llamada es de una empresa: `soltar`, `colgar` y compañía van con **esa** empresa aunque la
 * activa del panel ya haya cambiado (si no, el servidor responde 404 y la llamada queda viva).
 */
/**
 * El id de «Recibir llamadas» de este navegador. La reserva de `reclamar` queda atada a él: desde
 * otro navegador de la misma persona, `contestar`, `confirmar-medio` y `soltar` responden 409
 * `llamada_ya_contestada` (`otro_navegador`) y no tocan la reserva. Va en cabecera (no en la URL)
 * para que no quede en los registros de acceso.
 */
export const CABECERA_NAVEGADOR = "X-Iris-Navegador";
const navegador = () => ({ [CABECERA_NAVEGADOR]: idNavegador() });
const de = (empresa: number | undefined) => (empresa === undefined ? {} : { headers: { [CABECERA_EMPRESA]: String(empresa) } });
/** Lo de una saliente va con el id de esta pestaña (su latido y su respuesta SDP son de ella). */
const dePestana = (empresa: number | undefined) => ({ headers: { [CABECERA_PESTANA]: idPestana(), ...(empresa === undefined ? {} : { [CABECERA_EMPRESA]: String(empresa) }) } });

// --- «Recibir llamadas» (por usuario y navegador) ----------------------------------------------

export function leerRecibir(signal?: AbortSignal) {
    return resolver(api().GET(`${BASE}/recibir`, { params: { header: navegador() }, signal }));
}

export function encenderRecibir() {
    return resolver(api().POST(`${BASE}/recibir/encender`, { params: { header: idem() }, body: { navegador: idNavegador() } }));
}

export function apagarRecibir() {
    return resolver(api().POST(`${BASE}/recibir/apagar`, { params: { header: idem() }, body: { navegador: idNavegador() } }));
}

// --- La llamada --------------------------------------------------------------------------------

/** La llamada viva del usuario, o `null` (204). */
export async function leerLlamadaActiva(signal?: AbortSignal, empresa?: number) {
    return (await resolver(api().GET(`${BASE}/activa`, { signal, ...dePestana(empresa) }))) ?? null;
}

export function leerLlamada(id: string, signal?: AbortSignal, empresa?: number) {
    return resolver(api().GET(`${BASE}/{id}`, { params: { path: { id } }, signal, ...de(empresa) }));
}

/** Atómico: gana quien actualiza la fila. Es idempotente por naturaleza (no lleva `Idempotency-Key`). */
export function reclamarLlamada(id: string, signal?: AbortSignal) {
    return resolver(api().POST(`${BASE}/{id}/reclamar`, { params: { path: { id }, header: navegador() }, signal }));
}

export function contestarLlamada(id: string, sdpRespuesta: string, clave: string, signal?: AbortSignal) {
    return resolver(api().POST(`${BASE}/{id}/contestar`, { params: { path: { id }, header: { ...idem(clave), ...navegador() } }, body: { sdpRespuesta }, signal }));
}

export function confirmarMedio(id: string, clave: string, signal?: AbortSignal) {
    return resolver(api().POST(`${BASE}/{id}/confirmar-medio`, { params: { path: { id }, header: { ...idem(clave), ...navegador() } }, signal }));
}

export function soltarLlamada(id: string, motivo: string, empresa?: number) {
    return resolver(api().POST(`${BASE}/{id}/soltar`, { params: { path: { id }, header: { ...idem(), ...navegador() } }, body: { motivo }, ...de(empresa) }));
}

export function colgarLlamada(id: string, clave: string, empresa?: number) {
    return resolver(api().POST(`${BASE}/{id}/colgar`, { params: { path: { id }, header: idem(clave) }, ...de(empresa) }));
}

/** Deja de sonarle a este usuario. Con `paraTodos` (quien supervisa), la rechaza para todos. */
export function rechazarLlamada(id: string, empresa?: number, paraTodos = false) {
    return resolver(api().POST(`${BASE}/{id}/rechazar`, { params: { path: { id }, header: idem() }, body: paraTodos ? { paraTodos: true } : {}, ...de(empresa) }));
}

export function anotarLlamada(id: string, nota: string, resultado: ResultadoLlamada | undefined, clave: string) {
    return resolver(api().POST(`${BASE}/{id}/anotar`, { params: { path: { id }, header: idem(clave) }, body: { nota, ...(resultado ? { resultado } : {}) } }));
}

/**
 * Al cerrarse la pestaña no hay tiempo para esperar una respuesta: `soltar` (aún sin aceptar) o
 * `colgar` (ya en curso) salen con `fetch` y `keepalive`, que el navegador termina de mandar aunque
 * la página ya no exista. Lleva a mano lo que el cliente pone solo (CSRF y empresa activa).
 */
export function avisarAlCerrar(id: string, accion: "soltar" | "colgar", empresaDeLaLlamada?: number, propia = false): void {
    if (typeof fetch === "undefined" || typeof document === "undefined") return;
    const cabeceras: Record<string, string> = { "Content-Type": "application/json", "Idempotency-Key": nuevaClaveIdempotencia(), [CABECERA_NAVEGADOR]: idNavegador() };
    const csrf = leerCookie(COOKIE_CSRF, document.cookie);
    if (csrf) cabeceras[CABECERA_CSRF] = csrf;
    // La empresa de la llamada, no la activa: al cambiar de empresa la activa ya es otra.
    const empresa = empresaDeLaLlamada ?? empresaGuardada();
    if (empresa !== undefined) cabeceras[CABECERA_EMPRESA] = String(empresa);
    try {
        // `colgar?propia=true`: solo cuelga quien tiene la llamada (voz.md §2.3).
        void fetch(`${BASE}/${encodeURIComponent(id)}/${accion}${accion === "colgar" && propia ? "?propia=true" : ""}`, {
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

// --- Salientes y permiso de llamada (E1b) ---------------------------------------------------------

const CONV = "/api/v1/mensajeria/conversaciones/{id}";

/** Lo que Iris sabe del permiso del contacto para que lo llamen (no pregunta a Meta). */
export function leerPermisoLlamada(conversacionId: number, signal?: AbortSignal) {
    return resolver(api().GET(`${CONV}/permiso-llamada`, { params: { path: { id: conversacionId } }, signal }));
}

/** Manda al cliente la solicitud de permiso (queda en el hilo). Devuelve el permiso como quedó. */
export function solicitarPermisoLlamada(conversacionId: number, clave: string) {
    return resolver(api().POST(`${CONV}/solicitar-permiso-llamada`, { params: { path: { id: conversacionId }, header: idem(clave) } }));
}

/** Llama al contacto de la conversación con la oferta SDP del navegador. Responde la llamada en `marcando`. */
export function llamarContacto(conversacionId: number, entrada: { sdpOferta: string; devolucionId?: number | undefined; confirmarRiesgo?: boolean | undefined }, clave: string, empresa?: number, signal?: AbortSignal) {
    return resolver(
        api().POST(`${CONV}/llamar`, {
            signal,
            params: { path: { id: conversacionId }, header: { ...idem(clave), ...navegador() } },
            body: { sdpOferta: entrada.sdpOferta, ...(entrada.devolucionId !== undefined ? { devolucionId: entrada.devolucionId } : {}), ...(entrada.confirmarRiesgo ? { confirmarRiesgo: true } : {}) },
            ...dePestana(empresa),
        }),
    );
}

/** La respuesta SDP de Meta a mi saliente. 409 `sin_respuesta` si aún no llegó. */
export function leerRespuestaSdp(id: string, empresa?: number) {
    return resolver(api().GET(`${BASE}/{id}/respuesta-sdp`, { params: { path: { id }, header: navegador() }, ...dePestana(empresa) }));
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

/** Reintenta ya la escritura de los ajustes del número en WhatsApp (también desde `error`). */
export function aplicarLlamadasCanal(id: number) {
    return resolver(api().POST(`${CANAL}/aplicar`, { params: { path: { id }, header: idem() } }));
}

export function leerAjustesVoz(signal?: AbortSignal) {
    return resolver(api().GET("/api/v1/mensajeria/voz/ajustes", { signal }));
}

export function guardarAjustesVoz(entrada: EntradaAjustesVoz, clave: string) {
    return resolver(api().POST("/api/v1/mensajeria/voz/ajustes/actualizar", { params: { header: idem(clave) }, body: entrada }));
}
