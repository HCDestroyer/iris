/*
 * Portapapeles de datos sensibles (contraseña temporal, secreto TOTP): lo copiado se borra a los
 * 45 s y al cerrar la pantalla, salvo que el usuario ya haya copiado otra cosa.
 */

/** Tiempo que un dato sensible queda en el portapapeles. */
export const BORRAR_PORTAPAPELES_MS = 45_000;

/** Lo que se usa del portapapeles (inyectable para pruebas). */
export type Portapapeles = Pick<Clipboard, "writeText"> & Partial<Pick<Clipboard, "readText">>;

/**
 * Borra el portapapeles si aún contiene `copiado`. Si no se puede leer (sin permiso, sin
 * `readText`), lo borra igual: es preferible perder lo que haya a dejar el secreto. Nunca lanza.
 */
export async function borrarSiSigue(copiado: string, portapapeles: Portapapeles | undefined): Promise<boolean> {
    if (!portapapeles) {
        return false;
    }
    let actual: string | undefined;
    try {
        actual = portapapeles.readText ? await portapapeles.readText() : undefined;
    } catch {
        actual = undefined;
    }
    if (actual !== undefined && actual !== copiado) {
        return false;
    }
    try {
        await portapapeles.writeText("");
        return true;
    } catch {
        return false;
    }
}

/**
 * Programa el borrado de lo último copiado: `copiado(texto)` reinicia la cuenta de 45 s y
 * `cerrar()` (al desmontar) borra ya lo pendiente.
 */
export function borradorPortapapeles(
    obtener: () => Portapapeles | undefined,
    reloj: { setTimeout: (f: () => void, ms: number) => unknown; clearTimeout: (id: unknown) => void } = {
        setTimeout: (f, ms) => globalThis.setTimeout(f, ms),
        clearTimeout: (id) => globalThis.clearTimeout(id as ReturnType<typeof setTimeout>),
    },
    espera = BORRAR_PORTAPAPELES_MS,
) {
    let pendiente: string | null = null;
    let temporizador: unknown = null;
    const borrar = () => {
        if (temporizador !== null) {
            reloj.clearTimeout(temporizador);
            temporizador = null;
        }
        if (pendiente === null) {
            return Promise.resolve(false);
        }
        const texto = pendiente;
        pendiente = null;
        return borrarSiSigue(texto, obtener());
    };
    return {
        copiado(texto: string) {
            if (temporizador !== null) {
                reloj.clearTimeout(temporizador);
            }
            pendiente = texto;
            temporizador = reloj.setTimeout(() => {
                temporizador = null;
                void borrar();
            }, espera);
        },
        cerrar: borrar,
    };
}
