// Lo que las notificaciones guardan en el teléfono (almacén de claves, nada sensible fuera de él): las
// líneas de cada grupo (los textos de una conversación apilada), las respuestas ya hechas (dedupe) y
// las acciones pendientes (sin señal). Con un índice de grupos, porque el almacén no lista claves.
// Al cerrar sesión se borra todo (`olvidarPushLocal`).
import type { AlmacenClaves } from "../entorno";

export const CLAVE_GRUPOS = "iris-push-grupos";
export const CLAVE_HECHAS = "iris-push-hechas";
export const CLAVE_PENDIENTES = "iris-push-pendientes";
export const claveGrupo = (grupo: string) => `iris-push-grupo-${grupo}`;

/** Tope de grupos recordados (los más viejos se olvidan). */
const MAX_GRUPOS = 50;

function lista(claves: AlmacenClaves, clave: string): unknown[] {
    try {
        const v: unknown = JSON.parse(claves.leer(clave) ?? "[]");
        return Array.isArray(v) ? v : [];
    } catch {
        return [];
    }
}

const textos = (v: unknown[]) => v.filter((x): x is string => typeof x === "string");

export function lineasDeGrupo(claves: AlmacenClaves, grupo: string): string[] {
    return textos(lista(claves, claveGrupo(grupo)));
}

export function guardarLineasDeGrupo(claves: AlmacenClaves, grupo: string, lineas: readonly string[]): void {
    claves.escribir(claveGrupo(grupo), JSON.stringify(lineas));
    const grupos = textos(lista(claves, CLAVE_GRUPOS)).filter((g) => g !== grupo);
    const todos = [...grupos, grupo];
    for (const viejo of todos.slice(0, Math.max(0, todos.length - MAX_GRUPOS))) {
        claves.borrar(claveGrupo(viejo));
    }
    claves.escribir(CLAVE_GRUPOS, JSON.stringify(todos.slice(-MAX_GRUPOS)));
}

export function borrarGrupo(claves: AlmacenClaves, grupo: string): void {
    claves.borrar(claveGrupo(grupo));
    const grupos = textos(lista(claves, CLAVE_GRUPOS)).filter((g) => g !== grupo);
    if (grupos.length === 0) {
        claves.borrar(CLAVE_GRUPOS);
    } else {
        claves.escribir(CLAVE_GRUPOS, JSON.stringify(grupos));
    }
}

/**
 * Cerrar sesión: borra las líneas de todos los grupos y las respuestas hechas. Las acciones
 * pendientes se borran si no se conservan los pendientes; si se conservan, solo quedan las de esta
 * persona (las de otro usuario, siempre fuera).
 */
export function olvidarPushLocal(claves: AlmacenClaves, usuarioId: number, conservarPendientes: boolean): void {
    for (const g of textos(lista(claves, CLAVE_GRUPOS))) {
        claves.borrar(claveGrupo(g));
    }
    claves.borrar(CLAVE_GRUPOS);
    claves.borrar(CLAVE_HECHAS);
    const propias = conservarPendientes ? lista(claves, CLAVE_PENDIENTES).filter((p) => (p as { usuarioId?: unknown } | null)?.usuarioId === usuarioId) : [];
    if (propias.length === 0) {
        claves.borrar(CLAVE_PENDIENTES);
    } else {
        claves.escribir(CLAVE_PENDIENTES, JSON.stringify(propias));
    }
}

/** Tope para quitar los dispositivos al cerrar sesión: nunca cuelga la salida. */
export const ESPERA_QUITAR_MS = 3_000;

export interface CierrePush {
    servicio: { quitarTodas(): Promise<void> } | null;
    registro: { quitarTodos(usuarioId: number, senal?: AbortSignal): Promise<void> } | null;
    claves: AlmacenClaves;
    usuarioId: number;
    conservarPendientes: boolean;
    esperaMs?: number;
}

/**
 * Cerrar sesión: primero lo local (lo guardado y las notificaciones visibles), después quitar los
 * dispositivos con un tope de 3 s (`AbortSignal.timeout` si existe). Ningún error detiene la salida.
 */
export async function cerrarPushDeSesion(o: CierrePush): Promise<void> {
    olvidarPushLocal(o.claves, o.usuarioId, o.conservarPendientes);
    try {
        await o.servicio?.quitarTodas();
    } catch {
        // Sin notificaciones que quitar.
    }
    if (!o.registro) {
        return;
    }
    const espera = o.esperaMs ?? ESPERA_QUITAR_MS;
    const senal = typeof AbortSignal.timeout === "function" ? AbortSignal.timeout(espera) : undefined;
    let reloj: ReturnType<typeof setTimeout> | undefined;
    const tope = new Promise<void>((resolver) => {
        reloj = setTimeout(resolver, espera);
    });
    try {
        await Promise.race([o.registro.quitarTodos(o.usuarioId, senal).catch(() => undefined), tope]);
    } finally {
        clearTimeout(reloj);
    }
}
