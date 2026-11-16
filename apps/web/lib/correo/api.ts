import { api, resolver } from "@/lib/api/cliente";
import type { Ambito } from "./correo";
import type { AjusteCorreo, EntradaCorreo, EntradaPrueba, ResultadoPrueba } from "./tipos";

/*
 * Llamadas del correo del sistema (contratos/correo-sistema.md §3).
 *
 * PROVISIONAL: estas rutas todavía no están en `packages/contratos`, así que el cliente tipado no
 * las conoce. Se usa el mismo cliente del panel (cookies, CSRF, empresa activa, re-autenticación)
 * con una firma suelta, y las formas de `./tipos`. Cuando el servidor publique el OpenAPI, cada
 * función pasa a `api().GET("/api/v1/correo-sistema/{ambito}", …)` con los tipos generados.
 */

type Respuesta = { data?: unknown; error?: unknown; response: Response };
type Opciones = { body?: unknown; headers?: Record<string, string>; signal?: AbortSignal };
type ClienteSuelto = { GET: (ruta: string, o?: Opciones) => Promise<Respuesta>; POST: (ruta: string, o?: Opciones) => Promise<Respuesta> };

const suelto = () => api() as unknown as ClienteSuelto;
const ruta = (ambito: Ambito, accion?: string) => `/api/v1/correo-sistema/${ambito}${accion ? `/${accion}` : ""}`;
const idem = (clave: string) => ({ "Idempotency-Key": clave });

export function leerCorreo(ambito: Ambito, signal?: AbortSignal): Promise<AjusteCorreo> {
    return resolver(suelto().GET(ruta(ambito), signal ? { signal } : {})) as Promise<AjusteCorreo>;
}

export function guardarCorreo(ambito: Ambito, entrada: EntradaCorreo, clave: string): Promise<AjusteCorreo> {
    return resolver(suelto().POST(ruta(ambito, "guardar"), { body: entrada, headers: idem(clave) })) as Promise<AjusteCorreo>;
}

/** Manda un correo de prueba con lo que hay en el formulario, sin guardar. Responde 200 con el resultado. */
export function probarCorreo(ambito: Ambito, entrada: EntradaPrueba): Promise<ResultadoPrueba> {
    return resolver(suelto().POST(ruta(ambito, "probar"), { body: entrada })) as Promise<ResultadoPrueba>;
}

export function activarCorreo(ambito: Ambito, activo: boolean, version: number, clave: string): Promise<AjusteCorreo> {
    return resolver(suelto().POST(ruta(ambito, activo ? "activar" : "desactivar"), { body: { version }, headers: idem(clave) })) as Promise<AjusteCorreo>;
}
