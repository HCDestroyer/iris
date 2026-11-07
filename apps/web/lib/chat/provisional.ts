import type { components, Schema } from "@iris/contratos";
import { COOKIE_CSRF, CABECERA_CSRF, leerCookie } from "../api/cliente";
import { ErrorApi, errorDesdeRespuesta, errorSinConexion } from "../api/errores";
import { CABECERA_EMPRESA, empresaGuardada } from "../empresa";

/*
 * Lo del contrato del chat para el panel (`contratos/chat.md`, rama `agente/i1-chat-panel`, commit
 * f846176) que aún no está en `@iris/contratos`: rutas nuevas, campos nuevos y sus formas.
 *
 * TEMPORAL. Cuando I1 suba `packages/contratos` regenerado, `pnpm typecheck` falla en
 * `camposPendientes` / `esquemasPendientes` (las aserciones de abajo): entonces se borran estas
 * formas, se usa `Schema<…>` y las llamadas pasan al cliente tipado (`api().GET(…)`). Mientras
 * tanto, nada de esto se adivina: cada forma copia el contrato publicado y se valida al leerla.
 */

type ConversacionChat = Schema<"ConversacionChat">;
type MensajeChat = Schema<"MensajeChat">;
type Esquemas = components["schemas"];

type FaltaCampo<T, K extends string> = K extends keyof T ? false : true;
type FaltaEsquema<K extends string> = K extends keyof Esquemas ? false : true;

/** Falla en `pnpm typecheck` en cuanto el contrato traiga estos campos (ver arriba). */
export const camposPendientes = {
    "ConversacionChat.menciones": true satisfies FaltaCampo<ConversacionChat, "menciones">,
    "ConversacionChat.solo_lectura": true satisfies FaltaCampo<ConversacionChat, "solo_lectura">,
    "ConversacionChat.orden_tipo": true satisfies FaltaCampo<ConversacionChat, "orden_tipo">,
    "MensajeChat.menciones": true satisfies FaltaCampo<MensajeChat, "menciones">,
} as const;

/** Falla en `pnpm typecheck` en cuanto el contrato traiga estos esquemas. */
export const esquemasPendientes = {
    MiembroChat: true satisfies FaltaEsquema<"MiembroChat">,
    PersonaChat: true satisfies FaltaEsquema<"PersonaChat">,
    CuadrillaChat: true satisfies FaltaEsquema<"CuadrillaChat">,
    ResumenChat: true satisfies FaltaEsquema<"ResumenChat">,
} as const;

/** §2.2: quién está en el chat y por qué (con varios motivos, el primero de esta lista). */
export type MotivoMiembro = "directa" | "tecnico" | "despacho" | "agregado" | "supervision";
export type MiembroChat = { usuario_id: number; nombre: string; motivo: MotivoMiembro };
/** §2.3: con quién se puede abrir una directa (`conversacion_id`: la que ya existe). */
export type PersonaChat = { id: number; nombre: string; conversacion_id: number | null };
/** §2.3: cuadrillas cuyo chat se puede abrir (`conversacion_id` no nulo = «Ya existe»). */
export type CuadrillaChat = { id: number; nombre: string; tecnicos: number; zonas: string[]; conversacion_id: number | null };
export type ContadoresChat = { no_leidos: number; menciones: number; conversaciones_no_leidas: number; requiere_accion: number };
/** §2.1: contadores del ícono del encabezado y de las pestañas. */
export type ResumenChat = ContadoresChat & { por_tipo: Record<ConversacionChat["tipo"], ContadoresChat> };

const MOTIVOS: readonly MotivoMiembro[] = ["directa", "tecnico", "despacho", "agregado", "supervision"];

const esObjeto = (v: unknown): v is Record<string, unknown> => typeof v === "object" && v !== null;
const numero = (v: unknown, defecto = 0) => (typeof v === "number" && Number.isFinite(v) ? v : defecto);

/* --- Campos nuevos de las formas que ya existen (el motor guarda el objeto entero) ----------- */

/** Menciones sin leer de la conversación (§1); 0 si el servidor aún no lo manda. */
export function mencionesDe(c: ConversacionChat): number {
    return Math.max(0, numero((c as unknown as Record<string, unknown>).menciones));
}

/** Hilo de una orden cerrada, fallida o cancelada (§1). Sin el campo, se deriva del estado. */
export function soloLecturaDe(c: ConversacionChat): boolean {
    const v = (c as unknown as Record<string, unknown>).solo_lectura;
    if (typeof v === "boolean") return v;
    return c.tipo === "orden" && (c.orden_estado === "cerrada" || c.orden_estado === "fallida" || c.orden_estado === "cancelada");
}

/** Nombre del tipo de la orden («Instalación»), o null. */
export function ordenTipoDe(c: ConversacionChat): string | null {
    const v = (c as unknown as Record<string, unknown>).orden_tipo;
    return typeof v === "string" && v.trim() ? v : null;
}

/* --- Rutas nuevas ------------------------------------------------------------------------- */

const BASE = "/api/v1/mensajeria/chat";

async function llamar(metodo: "GET" | "POST", ruta: string, opciones: { cuerpo?: unknown; idempotencia?: string; signal?: AbortSignal } = {}): Promise<unknown> {
    const cabeceras = new Headers({ Accept: "application/json" });
    const empresa = empresaGuardada();
    if (empresa !== undefined) cabeceras.set(CABECERA_EMPRESA, String(empresa));
    if (metodo !== "GET") {
        const csrf = leerCookie(COOKIE_CSRF, typeof document === "undefined" ? "" : document.cookie);
        if (csrf) cabeceras.set(CABECERA_CSRF, csrf);
        if (opciones.idempotencia) cabeceras.set("Idempotency-Key", opciones.idempotencia);
    }
    if (opciones.cuerpo !== undefined) cabeceras.set("Content-Type", "application/json");
    let respuesta: Response;
    try {
        respuesta = await fetch(`${BASE}${ruta}`, {
            method: metodo,
            credentials: "include",
            headers: cabeceras,
            ...(opciones.cuerpo !== undefined ? { body: JSON.stringify(opciones.cuerpo) } : {}),
            ...(opciones.signal ? { signal: opciones.signal } : {}),
        });
    } catch (causa) {
        throw errorSinConexion(causa);
    }
    let cuerpo: unknown = undefined;
    if (respuesta.status !== 204 && respuesta.headers.get("Content-Type")?.includes("json")) {
        try {
            cuerpo = await respuesta.json();
        } catch {
            cuerpo = undefined;
        }
    }
    if (!respuesta.ok) throw errorDesdeRespuesta(respuesta, cuerpo);
    return cuerpo;
}

const formaInvalida = (que: string) => new ErrorApi({ estado: 502, codigo: "respuesta_invalida", titulo: `La API respondió ${que} con otra forma` });

export function leerMiembros(cuerpo: unknown): MiembroChat[] {
    const datos = esObjeto(cuerpo) ? cuerpo.data : undefined;
    if (!Array.isArray(datos)) throw formaInvalida("los miembros");
    return datos.filter(esObjeto).flatMap((m) => {
        const motivo = MOTIVOS.find((x) => x === m.motivo) ?? "supervision";
        return typeof m.usuario_id === "number" && typeof m.nombre === "string" ? [{ usuario_id: m.usuario_id, nombre: m.nombre, motivo }] : [];
    });
}

function leerContadores(v: unknown): ContadoresChat {
    const o = esObjeto(v) ? v : {};
    return { no_leidos: numero(o.no_leidos), menciones: numero(o.menciones), conversaciones_no_leidas: numero(o.conversaciones_no_leidas), requiere_accion: numero(o.requiere_accion) };
}

export function leerResumen(cuerpo: unknown): ResumenChat {
    if (!esObjeto(cuerpo) || typeof cuerpo.requiere_accion !== "number") throw formaInvalida("el resumen del chat");
    const por = esObjeto(cuerpo.por_tipo) ? cuerpo.por_tipo : {};
    return { ...leerContadores(cuerpo), por_tipo: { directa: leerContadores(por.directa), cuadrilla: leerContadores(por.cuadrilla), orden: leerContadores(por.orden) } };
}

/** `GET /resumen` (§2.1). */
export async function pedirResumen(signal?: AbortSignal): Promise<ResumenChat> {
    return leerResumen(await llamar("GET", "/resumen", signal ? { signal } : {}));
}

/** `GET /conversaciones/{id}/miembros` (§2.2). */
export async function pedirMiembros(conversacionId: number, signal?: AbortSignal): Promise<MiembroChat[]> {
    return leerMiembros(await llamar("GET", `/conversaciones/${conversacionId}/miembros`, signal ? { signal } : {}));
}

/** `POST /conversaciones/{id}/miembros` (§2.2, `chat.supervisar`): la lista de miembros. */
export async function agregarMiembro(conversacionId: number, usuarioId: number, idempotencia: string): Promise<MiembroChat[]> {
    return leerMiembros(await llamar("POST", `/conversaciones/${conversacionId}/miembros`, { cuerpo: { usuario_id: usuarioId }, idempotencia }));
}

/** `GET /personas?q=` (§2.3). */
export async function buscarPersonas(q: string, signal?: AbortSignal): Promise<PersonaChat[]> {
    const cuerpo = await llamar("GET", `/personas?limit=50${q.trim() ? `&q=${encodeURIComponent(q.trim())}` : ""}`, signal ? { signal } : {});
    const datos = esObjeto(cuerpo) ? cuerpo.data : undefined;
    if (!Array.isArray(datos)) throw formaInvalida("las personas");
    return datos.filter(esObjeto).flatMap((p) =>
        typeof p.id === "number" && typeof p.nombre === "string" ? [{ id: p.id, nombre: p.nombre, conversacion_id: typeof p.conversacion_id === "number" ? p.conversacion_id : null }] : [],
    );
}

/** `GET /cuadrillas?q=` (§2.3). */
export async function buscarCuadrillas(q: string, signal?: AbortSignal): Promise<CuadrillaChat[]> {
    const cuerpo = await llamar("GET", `/cuadrillas${q.trim() ? `?q=${encodeURIComponent(q.trim())}` : ""}`, signal ? { signal } : {});
    const datos = esObjeto(cuerpo) ? cuerpo.data : undefined;
    if (!Array.isArray(datos)) throw formaInvalida("las cuadrillas");
    return datos.filter(esObjeto).flatMap((c) =>
        typeof c.id === "number" && typeof c.nombre === "string"
            ? [
                  {
                      id: c.id,
                      nombre: c.nombre,
                      tecnicos: numero(c.tecnicos),
                      zonas: Array.isArray(c.zonas) ? c.zonas.filter((z): z is string => typeof z === "string") : [],
                      conversacion_id: typeof c.conversacion_id === "number" ? c.conversacion_id : null,
                  },
              ]
            : [],
    );
}

/** `GET /ordenes/{orden_id}/conversacion` (no crea ni une): el id del hilo, o null con 404. */
export async function hiloDeOrden(ordenId: number, signal?: AbortSignal): Promise<number | null> {
    try {
        const cuerpo = await llamar("GET", `/ordenes/${ordenId}/conversacion`, signal ? { signal } : {});
        if (!esObjeto(cuerpo) || typeof cuerpo.id !== "number") throw formaInvalida("el hilo de la orden");
        return cuerpo.id;
    } catch (e) {
        if (e instanceof ErrorApi && e.estado === 404) return null;
        throw e;
    }
}

/**
 * §3.1, al cerrar u ocultar la página con una eliminación pendiente: `POST /mensajes/{id}/eliminar`
 * con `keepalive` (sale aunque la página se vaya). No espera la respuesta.
 */
export function eliminarAlSalir(mensajeId: string): void {
    const cabeceras: Record<string, string> = {};
    const empresa = empresaGuardada();
    if (empresa !== undefined) cabeceras[CABECERA_EMPRESA] = String(empresa);
    const csrf = leerCookie(COOKIE_CSRF, document.cookie);
    if (csrf) cabeceras[CABECERA_CSRF] = csrf;
    void fetch(`${BASE}/mensajes/${encodeURIComponent(mensajeId)}/eliminar`, { method: "POST", credentials: "include", keepalive: true, headers: cabeceras }).catch(() => undefined);
}
