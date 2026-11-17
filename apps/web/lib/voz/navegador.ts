/*
 * Id de este navegador para «Recibir llamadas» (contratos/voz.md §2.4: de 8 a 64 caracteres
 * `[A-Za-z0-9_-]`, generado y guardado por el panel). No identifica a la persona: solo distingue
 * este navegador de otro del mismo usuario.
 */

const CLAVE = "iris-navegador-llamadas";
const VALIDO = /^[A-Za-z0-9_-]{8,64}$/;
let enMemoria: string | null = null;

function generar(): string {
    const bytes = new Uint8Array(16);
    crypto.getRandomValues(bytes);
    return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

export function idNavegador(): string {
    if (enMemoria) return enMemoria;
    try {
        const guardado = window.localStorage.getItem(CLAVE);
        if (guardado && VALIDO.test(guardado)) {
            enMemoria = guardado;
            return guardado;
        }
    } catch {
        // Sin almacenamiento (privado, bloqueado): el id dura lo que la pestaña.
    }
    const nuevo = generar();
    enMemoria = nuevo;
    try {
        window.localStorage.setItem(CLAVE, nuevo);
    } catch {
        // Igual que arriba.
    }
    return nuevo;
}

/*
 * Id de esta pestaña (revisión de seguridad de E1b): 128 bits al azar, solo en memoria. El id de
 * navegador es el mismo en todas las pestañas, así que el servidor ata el latido de una saliente a
 * la pestaña que la hizo: otra pestaña abierta no la sostiene. En memoria (no `sessionStorage`,
 * que se copia al duplicar la pestaña): una recarga es otra pestaña y la llamada vieja, sin audio,
 * deja de tener latido.
 */
let pestana: string | null = null;

export function idPestana(): string {
    pestana ??= generar();
    return pestana;
}

export const CABECERA_PESTANA = "X-Iris-Pestana";

/*
 * Marca de «`llamar` en vuelo»: si la pestaña se cierra con `llamar` enviado y sin respuesta, no
 * hay id que colgar. Se deja esta marca para que otra pestaña (evento `storage`) o la carga
 * siguiente busque esa saliente y la cuelgue.
 */
const CLAVE_EN_VUELO = "iris-llamar-en-vuelo";

/** Sin el id de la pestaña (no hace falta y es lo que ata el latido): quién llamaba, en qué empresa y a quién. */
export type LlamarEnVuelo = { usuarioId: number; empresa: number | null; conversacionId: number; desde: number };

export function marcarLlamarEnVuelo(marca: LlamarEnVuelo): void {
    try {
        window.localStorage.setItem(CLAVE_EN_VUELO, JSON.stringify(marca satisfies LlamarEnVuelo));
    } catch {
        // Sin almacenamiento no hay a quién avisar: el servidor la corta sin el latido de esta pestaña.
    }
}

/**
 * Toma la marca (la borra) si es de este usuario en esta empresa y sigue vigente; `null` si no hay
 * nada que hacer. La de otro usuario (otra sesión en este navegador) no se toca ni se consume.
 */
export function tomarLlamarEnVuelo(usuarioId: number, empresa: number | undefined, vigenciaMs: number, ahora = Date.now()): LlamarEnVuelo | null {
    try {
        const cruda = window.localStorage.getItem(CLAVE_EN_VUELO);
        if (!cruda) return null;
        const m = JSON.parse(cruda) as Partial<LlamarEnVuelo>;
        const valida = typeof m.conversacionId === "number" && typeof m.desde === "number" && typeof m.usuarioId === "number";
        if (!valida || ahora - (m.desde ?? 0) > vigenciaMs) {
            window.localStorage.removeItem(CLAVE_EN_VUELO);
            return null;
        }
        if (m.usuarioId !== usuarioId || (m.empresa ?? null) !== (empresa ?? null)) return null;
        window.localStorage.removeItem(CLAVE_EN_VUELO);
        return m as LlamarEnVuelo;
    } catch {
        return null;
    }
}

export const esMarcaEnVuelo = (clave: string | null) => clave === CLAVE_EN_VUELO;
