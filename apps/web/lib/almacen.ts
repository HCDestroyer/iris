/*
 * Almacenamiento local del navegador para comodidades por usuario (empresa elegida, vistas
 * guardadas). Puede no existir o lanzar (navegación privada, datos bloqueados): todo va en
 * try/catch y la pantalla funciona igual sin él.
 */

function almacen(): Storage | undefined {
    try {
        return typeof window === "undefined" ? undefined : window.localStorage;
    } catch {
        return undefined;
    }
}

export function leerLocal(clave: string): string | undefined {
    try {
        return almacen()?.getItem(clave) ?? undefined;
    } catch {
        return undefined;
    }
}

export function escribirLocal(clave: string, valor: string): void {
    try {
        almacen()?.setItem(clave, valor);
    } catch {
        // Sin almacenamiento: la preferencia dura solo esta visita.
    }
}

export function borrarLocal(clave: string): void {
    try {
        almacen()?.removeItem(clave);
    } catch {
        // Nada que borrar.
    }
}

/** Lee JSON guardado; si falta o está corrupto, `porDefecto`. */
export function leerLocalJson<T>(clave: string, validar: (v: unknown) => v is T, porDefecto: T): T {
    const crudo = leerLocal(clave);
    if (crudo === undefined) {
        return porDefecto;
    }
    try {
        const valor: unknown = JSON.parse(crudo);
        return validar(valor) ? valor : porDefecto;
    } catch {
        return porDefecto;
    }
}
