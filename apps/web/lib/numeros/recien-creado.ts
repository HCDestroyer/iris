import type { Canal, PruebaCanal } from "./tipos";

/*
 * El verify token que Iris genera al crear un número se muestra una sola vez (§1.1). «Agregar
 * número» lo deja aquí, en la memoria de la pestaña, y la pantalla «Conectar» lo toma al abrirse:
 * nunca va en la URL, en el historial ni en el almacenamiento del navegador. Recargar lo pierde
 * (como la contraseña temporal de «Nuevo usuario»); para pegar uno nuevo está «Generar otro».
 */

export type RecienCreado = { canal: Canal; verifyToken: string | null; prueba: PruebaCanal | null; errorPrueba: unknown };

let pendiente: RecienCreado | null = null;

export function dejarRecienCreado(datos: RecienCreado): void {
    pendiente = datos;
}

/**
 * Lo de ese número, si lo hay. No lo borra: la pantalla lo guarda en su estado y luego llama
 * `olvidarRecienCreado` en un efecto (en modo estricto, React llama dos veces al inicializador).
 */
export function leerRecienCreado(canalId: number): RecienCreado | null {
    return pendiente && pendiente.canal.id === canalId ? pendiente : null;
}

/** Ya se mostró (o se salió): no queda en memoria. */
export function olvidarRecienCreado(): void {
    pendiente = null;
}
