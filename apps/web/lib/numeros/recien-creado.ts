import type { Canal, PruebaCanal } from "./tipos";

/*
 * El verify token que Iris genera al crear un número se muestra una sola vez (§1.1). «Agregar
 * número» lo deja aquí, en la memoria de la pestaña, y la pantalla «Conectar» lo toma al abrirse:
 * nunca va en la URL, en el historial ni en el almacenamiento del navegador. Recargar lo pierde
 * (como la contraseña temporal de «Nuevo usuario»); para pegar uno nuevo está «Generar otro».
 */

export type RecienCreado = { canal: Canal; verifyToken: string | null; prueba: PruebaCanal | null; errorPrueba: unknown };

/** Lo que tarda «Conectar» en abrir es mucho menos; pasado esto, el token se olvida aunque nadie lo tome. */
export const VIDA_RECIEN_CREADO_MS = 60_000;

let pendiente: (RecienCreado & { expira: number }) | null = null;

export function dejarRecienCreado(datos: RecienCreado, ahora: number = Date.now()): void {
    pendiente = { ...datos, expira: ahora + VIDA_RECIEN_CREADO_MS };
}

/**
 * Lo de ese número, si lo hay y no venció. No lo borra: la pantalla lo guarda en su estado y luego
 * llama `olvidarRecienCreado` en un efecto (en modo estricto, React llama dos veces al
 * inicializador).
 */
export function leerRecienCreado(canalId: number, ahora: number = Date.now()): RecienCreado | null {
    if (!pendiente) return null;
    if (ahora > pendiente.expira) {
        pendiente = null;
        return null;
    }
    if (pendiente.canal.id !== canalId) return null;
    return { canal: pendiente.canal, verifyToken: pendiente.verifyToken, prueba: pendiente.prueba, errorPrueba: pendiente.errorPrueba };
}

/** Ya se mostró (o se salió): no queda en memoria. */
export function olvidarRecienCreado(): void {
    pendiente = null;
}
