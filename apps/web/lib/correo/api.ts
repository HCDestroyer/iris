import { api, resolver } from "@/lib/api/cliente";
import type { Ambito } from "./correo";
import type { EntradaCorreo, EntradaPrueba } from "./tipos";

/* Llamadas del correo del sistema (contratos/correo-sistema.md §3), con el cliente tipado. */

const BASE = "/api/v1/correo-sistema/{ambito}";
const ruta = (ambito: Ambito) => ({ ambito: String(ambito) });

export function leerCorreo(ambito: Ambito, signal?: AbortSignal) {
    return resolver(api().GET(BASE, { params: { path: ruta(ambito) }, signal }));
}

export function guardarCorreo(ambito: Ambito, entrada: EntradaCorreo, clave: string) {
    return resolver(api().POST(`${BASE}/guardar`, { params: { path: ruta(ambito), header: { "Idempotency-Key": clave } }, body: entrada }));
}

/** Manda un correo de prueba con lo que hay en el formulario, sin guardar. Responde 200 con el resultado. */
export function probarCorreo(ambito: Ambito, entrada: EntradaPrueba) {
    return resolver(api().POST(`${BASE}/probar`, { params: { path: ruta(ambito) }, body: entrada }));
}

export function activarCorreo(ambito: Ambito, activo: boolean, version: number, clave: string) {
    const params = { path: ruta(ambito), header: { "Idempotency-Key": clave } };
    return resolver(activo ? api().POST(`${BASE}/activar`, { params, body: { version } }) : api().POST(`${BASE}/desactivar`, { params, body: { version } }));
}
