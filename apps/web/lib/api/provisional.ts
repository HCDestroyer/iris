import { api, resolver } from "./cliente";

/*
 * Llamadas a rutas que todavía no están en el OpenAPI de `packages/contratos` (el contrato del
 * servidor está en curso en otra rama). Usan el mismo cliente del panel (cookies, CSRF, empresa
 * activa, re-autenticación y errores `problem+json`), solo que sin los tipos generados: quien
 * llama declara la forma de la respuesta.
 *
 * Es un puente: cuando la ruta llega a `packages/contratos`, la llamada pasa a `api().GET/POST`
 * tipado y deja de usar este archivo.
 */

type Opciones = { query?: Record<string, string | number | undefined>; cuerpo?: unknown; idempotencia?: string; signal?: AbortSignal | undefined };
type Resultado = Promise<{ data?: unknown; error?: unknown; response: Response }>;
type Suelto = { GET: (ruta: string, o: unknown) => Resultado; POST: (ruta: string, o: unknown) => Resultado };

const suelto = () => api() as unknown as Suelto;

const consulta = (q: Opciones["query"]) => Object.fromEntries(Object.entries(q ?? {}).filter(([, v]) => v !== undefined));

export function leerProvisional<T>(ruta: string, o: Opciones = {}): Promise<T> {
    return resolver(suelto().GET(ruta, { params: { query: consulta(o.query) }, signal: o.signal })) as Promise<T>;
}

export function enviarProvisional<T>(ruta: string, o: Opciones = {}): Promise<T> {
    return resolver(
        suelto().POST(ruta, {
            params: { header: o.idempotencia ? { "Idempotency-Key": o.idempotencia } : {} },
            body: o.cuerpo ?? {},
            signal: o.signal,
        }),
    ) as Promise<T>;
}
