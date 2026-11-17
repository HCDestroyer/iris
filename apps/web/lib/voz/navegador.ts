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
