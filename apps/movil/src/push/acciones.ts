// Lo que pasa al tocar una notificación o uno de sus botones (`contratos/push.md` §5), igual con la
// app abierta que desde la tarea en segundo plano: «Responder» y «Marcar como leído» llaman a la API
// (con la app abierta, «Responder» del chat va por la cola de `@iris/sync/chat`, que lo manda al
// volver la señal); lo que no salió queda guardado en el teléfono y se manda al abrir la app.
// «Ver orden», «Cómo llegar», «Ver» y tocar la notificación devuelven el destino.
import { ErrorApi } from "../api/errores";
import type { AlmacenClaves } from "../entorno";
import type { ApiPush } from "./api";
import { CLAVE_HECHAS, CLAVE_PENDIENTES } from "./locales";
import { destinoDeEnlace, type AccionPush, type DestinoPush, type MensajePush } from "./mensaje";

export interface RespuestaPush {
    /** `abrir` = tocó la notificación (no un botón). */
    accion: AccionPush | "abrir";
    /** Lo escrito en «Responder». */
    texto?: string;
    mensaje: MensajePush;
}

/** Una acción que no salió (sin señal): se manda al abrir la app. */
export type AccionPendiente =
    | { tipo: "responder_chat"; usuarioId: number; empresa: number | null; conversacionId: number; id: string; texto: string }
    | { tipo: "leer_chat"; usuarioId: number; empresa: number | null; conversacionId: number; hasta: string }
    | { tipo: "nota_bandeja"; usuarioId: number; empresa: number | null; conversacionId: number; id: string; texto: string; menciones: number[] }
    | { tipo: "leer_bandeja"; usuarioId: number; empresa: number | null; conversacionId: number; hasta: string };

/** Tope de acciones guardadas (nunca crece sin límite). */
const MAX_PENDIENTES = 50;

export function leerPendientes(claves: AlmacenClaves): AccionPendiente[] {
    try {
        const v: unknown = JSON.parse(claves.leer(CLAVE_PENDIENTES) ?? "[]");
        return Array.isArray(v) ? (v as AccionPendiente[]) : [];
    } catch {
        return [];
    }
}

function guardarPendientes(claves: AlmacenClaves, lista: readonly AccionPendiente[]): void {
    if (lista.length === 0) {
        claves.borrar(CLAVE_PENDIENTES);
    } else {
        claves.escribir(CLAVE_PENDIENTES, JSON.stringify(lista.slice(-MAX_PENDIENTES)));
    }
}

/** Marca una respuesta como hecha; `true` si ya lo estaba (se guardan las últimas 50). */
function yaHecha(claves: AlmacenClaves, llave: string): boolean {
    let lista: string[] = [];
    try {
        const v: unknown = JSON.parse(claves.leer(CLAVE_HECHAS) ?? "[]");
        lista = Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
    } catch {
        lista = [];
    }
    if (lista.includes(llave)) {
        return true;
    }
    claves.escribir(CLAVE_HECHAS, JSON.stringify([...lista, llave].slice(-50)));
    return false;
}

export interface DepsAccion {
    api: ApiPush;
    claves: AlmacenClaves;
    generarId: () => string;
    /** Usuario con sesión en el teléfono; otro (o ninguno) = la notificación se descarta. */
    usuarioId: number | null;
    /** Con la app abierta: la cola del chat (`MotorChat.escribir`), que también funciona sin señal. */
    escribirChat?: (conversacionId: number, texto: string) => Promise<void>;
    quitarGrupo: (grupo: string) => Promise<void>;
}

/** Sin respuesta de la API (sin señal): se reintenta; otro error (403, 404, 422) no. */
const esDeRed = (e: unknown) => !(e instanceof ErrorApi) || e.estado === 0 || e.estado >= 500 || e.estado === 429;

async function mandar(p: AccionPendiente, api: ApiPush): Promise<void> {
    switch (p.tipo) {
        case "responder_chat":
            return api.responderChat(p.empresa, p.conversacionId, p.id, p.texto);
        case "leer_chat":
            return api.leerChat(p.empresa, p.conversacionId, p.hasta);
        case "nota_bandeja":
            return api.notaBandeja(p.empresa, p.conversacionId, p.id, p.texto, p.menciones);
        case "leer_bandeja":
            return api.leerBandeja(p.empresa, p.conversacionId, p.hasta);
    }
}

/** Manda o, si no hay señal, guarda para después. */
async function mandarOGuardar(p: AccionPendiente, d: DepsAccion): Promise<void> {
    try {
        await mandar(p, d.api);
    } catch (e) {
        if (esDeRed(e)) {
            guardarPendientes(d.claves, [...leerPendientes(d.claves), p]);
        }
    }
}

/**
 * Ejecuta la respuesta. Devuelve el destino a abrir (tocar, «Ver orden», «Ver»; «Cómo llegar» con
 * `comoLlegar: true`) o `null` si no hay que abrir nada.
 */
export async function ejecutarRespuesta(r: RespuestaPush, d: DepsAccion): Promise<{ destino: DestinoPush; comoLlegar?: boolean } | null> {
    const m = r.mensaje;
    if (d.usuarioId === null || m.usuarioId !== d.usuarioId) {
        // Es de otra persona (o no hay sesión): no se hace nada en su nombre (ni se marca).
        await d.quitarGrupo(m.grupo);
        return null;
    }
    // Con la app en segundo plano puede llegar dos veces (al oyente y a la tarea): una sola respuesta.
    // La llave nunca lleva el texto escrito.
    if ((r.accion === "responder" || r.accion === "leido") && yaHecha(d.claves, `${m.id}:${r.accion}`)) {
        return null;
    }
    const base = { usuarioId: m.usuarioId, empresa: m.empresaId };
    switch (r.accion) {
        case "responder": {
            const texto = r.texto?.trim() ?? "";
            if (!texto) {
                return null;
            }
            if (m.datos.tipo === "chat" || (m.datos.tipo === "mencion" && m.datos.origen === "chat")) {
                const conversacionId = m.datos.conversacionId;
                let encolado = false;
                if (d.escribirChat) {
                    try {
                        await d.escribirChat(conversacionId, texto);
                        encolado = true;
                    } catch {
                        // Conversación que el teléfono no tiene: va directo a la API.
                    }
                }
                if (!encolado) {
                    await mandarOGuardar({ ...base, tipo: "responder_chat", conversacionId, id: d.generarId(), texto }, d);
                }
            } else if (m.datos.tipo === "mencion") {
                const menciones = m.datos.remitente ? [m.datos.remitente.id] : [];
                await mandarOGuardar({ ...base, tipo: "nota_bandeja", conversacionId: m.datos.conversacionId, id: d.generarId(), texto, menciones }, d);
            }
            await d.quitarGrupo(m.grupo);
            return null;
        }
        case "leido": {
            if ((m.datos.tipo === "chat" || m.datos.tipo === "mencion") && m.datos.mensajeId) {
                const bandeja = m.datos.tipo === "mencion" && m.datos.origen === "bandeja";
                await mandarOGuardar({ ...base, tipo: bandeja ? "leer_bandeja" : "leer_chat", conversacionId: m.datos.conversacionId, hasta: m.datos.mensajeId }, d);
            }
            await d.quitarGrupo(m.grupo);
            return null;
        }
        default: {
            const destino = destinoDeEnlace(m.enlace);
            await d.quitarGrupo(m.grupo);
            return destino ? { destino, ...(r.accion === "como_llegar" ? { comoLlegar: true } : {}) } : null;
        }
    }
}

/**
 * Con la app abierta: manda lo que quedó guardado de este usuario (el «Responder» del chat por la
 * cola del chat). Lo de otro usuario se descarta.
 */
export async function vaciarPendientes(d: DepsAccion): Promise<void> {
    const lista = leerPendientes(d.claves);
    if (lista.length === 0) {
        return;
    }
    guardarPendientes(d.claves, []);
    const quedan: AccionPendiente[] = [];
    for (const p of lista) {
        if (p.usuarioId !== d.usuarioId) {
            continue;
        }
        if (p.tipo === "responder_chat" && d.escribirChat) {
            try {
                await d.escribirChat(p.conversacionId, p.texto);
                continue;
            } catch {
                // Sigue directo a la API.
            }
        }
        try {
            await mandar(p, d.api);
        } catch (e) {
            if (esDeRed(e)) {
                quedan.push(p);
            }
        }
    }
    guardarPendientes(d.claves, [...quedan, ...leerPendientes(d.claves)]);
}
