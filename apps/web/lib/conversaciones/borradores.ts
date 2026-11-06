import type { Identidad } from "./sesion";

/*
 * Borradores del compositor: en `sessionStorage` (mueren con la pestaña), con la clave del usuario
 * y la empresa. Se leen solo si son de quien está en pantalla y se borran todos al cerrar sesión o
 * con un 401. Nunca se guarda un borrador sin una identidad confirmada por `GET /yo`.
 */
const PREFIJO = "iris-bandeja-borrador:";

type Almacen = Pick<Storage, "getItem" | "setItem" | "removeItem" | "length" | "key">;

function almacenNavegador(): Almacen | undefined {
    try {
        return typeof window === "undefined" ? undefined : window.sessionStorage;
    } catch {
        return undefined;
    }
}

const clave = (identidad: Identidad, conversacionId: number, modo: string) =>
    `${PREFIJO}${identidad.usuarioId}:${identidad.empresaId ?? 0}:${conversacionId}:${modo}`;

export function leerBorrador(identidad: Identidad, conversacionId: number, modo: string, almacen = almacenNavegador()): string {
    try {
        return almacen?.getItem(clave(identidad, conversacionId, modo)) ?? "";
    } catch {
        return "";
    }
}

export function guardarBorrador(identidad: Identidad, conversacionId: number, modo: string, texto: string, almacen = almacenNavegador()): void {
    try {
        if (texto.trim() === "") {
            almacen?.removeItem(clave(identidad, conversacionId, modo));
        } else {
            almacen?.setItem(clave(identidad, conversacionId, modo), texto.slice(0, 4096));
        }
    } catch {
        // Sin almacenamiento (privado, lleno): el borrador vive lo que la pantalla.
    }
}

/** Borra los borradores de la bandeja (todos, o los que no son de `identidad`). */
export function limpiarBorradores(identidad?: Identidad, almacen = almacenNavegador()): void {
    if (!almacen) return;
    try {
        const propio = identidad ? `${PREFIJO}${identidad.usuarioId}:${identidad.empresaId ?? 0}:` : null;
        const claves: string[] = [];
        for (let i = 0; i < almacen.length; i += 1) {
            const k = almacen.key(i);
            if (k?.startsWith(PREFIJO) && (propio === null || !k.startsWith(propio))) claves.push(k);
        }
        for (const k of claves) almacen.removeItem(k);
    } catch {
        // Nada que borrar.
    }
}
