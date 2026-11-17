import { api, resolver } from "@/lib/api/cliente";
import type { EnlaceCreado, Motor, Rango, ResultadoPrueba, Tablero, Umbrales } from "./tipos";

/*
 * Cliente del Servidor de IA (contratos/voz.md §4.8 y §4.12). PROVISIONAL: estas rutas aún no
 * están en el OpenAPI generado, así que se llaman por su texto con el mismo cliente (sesión, CSRF,
 * empresa activa y errores RFC 9457 como siempre). Al llegar los tipos, cada función pasa a la
 * forma tipada sin cambiar su firma.
 */

type Opciones = { signal?: AbortSignal | undefined; body?: unknown; headers?: Record<string, string>; params?: { query?: Record<string, string> } };
type Respuesta = Promise<{ data?: unknown; error?: unknown; response: Response }>;
type Suelto = { GET: (ruta: string, o?: Opciones) => Respuesta; POST: (ruta: string, o?: Opciones) => Respuesta };

const cliente = () => api() as unknown as Suelto;
const leer = <T>(ruta: string, o: Opciones = {}) => resolver(cliente().GET(ruta, o)) as Promise<T>;
const mandar = <T>(ruta: string, clave: string, body?: unknown) => resolver(cliente().POST(ruta, { headers: { "Idempotency-Key": clave }, ...(body === undefined ? {} : { body }) })) as Promise<T>;

const MOTORES = "/api/v1/mensajeria/voz/motores";
const SERVIDOR = "/api/v1/mensajeria/servidor-ia";

/** El servidor de la instalación, o `null` si no hay ninguno enlazado. */
export async function leerMotor(signal?: AbortSignal): Promise<Motor | null> {
    const r = await leer<{ data: Motor[] | null }>(MOTORES, { signal });
    return r.data?.[0] ?? null;
}

/** Crea el enlace: devuelve el código de un solo uso, que se muestra una vez (no se guarda por `Idempotency-Key`). */
export const enlazarMotor = (entrada: { nombre: string; url: string }, clave: string) => mandar<EnlaceCreado>(`${MOTORES}/enlazar`, clave, entrada);
/** Iris se presenta al servidor con el código ya escrito allá. Código vencido, usado o equivocado: 401, sin detalle. */
export const conectarMotor = (id: number, clave: string) => mandar<Motor>(`${MOTORES}/${id}/conectar`, clave);
export const activarMotor = (id: number, version: number, clave: string) => mandar<Motor>(`${MOTORES}/${id}/activar`, clave, { version });
export const desactivarMotor = (id: number, version: number, motivo: string, clave: string) => mandar<Motor>(`${MOTORES}/${id}/desactivar`, clave, { version, motivo });
export const probarMotor = (id: number, clave: string) => mandar<ResultadoPrueba>(`${MOTORES}/${id}/probar`, clave);
/** «Rotar credenciales»; con `revocar`, la anterior deja de valer en el acto. */
export const rotarCredenciales = (id: number, revocar: boolean, clave: string) => mandar<Motor>(`${MOTORES}/${id}/rotar-token`, clave, revocar ? { revocar: true } : {});
export const guardarEmpresasPermitidas = (id: number, entrada: { empresas: number[] | null; simultaneasPorEmpresa: number; version: number }, clave: string) =>
    mandar<Motor>(`${MOTORES}/${id}/empresas/actualizar`, clave, entrada);

export const leerTablero = (rango: Rango, signal?: AbortSignal) => leer<Tablero>(`${SERVIDOR}/tablero`, { signal, params: { query: { rango } } });
export const leerUmbrales = (signal?: AbortSignal) => leer<Umbrales>(`${SERVIDOR}/umbrales`, { signal });
export const guardarUmbrales = (entrada: Umbrales, clave: string) => mandar<Umbrales>(`${SERVIDOR}/umbrales/actualizar`, clave, entrada);
