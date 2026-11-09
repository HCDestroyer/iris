import { api, resolver } from "../api/cliente";
import { cabeceraIdem } from "../conversaciones/api";
import type { AjustesAsistente, EntradaAjustesAsistente, EstadoAsistente, ModeloIA, ProveedorIA, PruebaAsistente, ResumenConversacion, Sugerencia, UsoAsistente } from "./tipos";

/*
 * Llamadas del asistente de IA (contratos/conversaciones.md §16, base `/api/v1/mensajeria`).
 * Las acciones llevan `Idempotency-Key`.
 */

const M = "/api/v1/mensajeria";

// PROVISIONAL hasta mergear agente/i1-asistente (rutas aún fuera del OpenAPI).
type Opciones = { params?: { path?: Record<string, unknown>; query?: Record<string, unknown>; header?: Record<string, string> }; body?: unknown; signal?: AbortSignal | undefined };
type Llamada = (ruta: never, opciones: never) => Promise<{ data?: unknown; error?: unknown; response: Response }>;
function llamar<T>(metodo: "GET" | "POST", ruta: string, opciones: Opciones = {}): Promise<T> {
    const cliente = api() as unknown as Record<"GET" | "POST", Llamada>;
    return resolver(cliente[metodo](ruta as never, opciones as never)) as Promise<T>;
}

// §16.1 Estado (quien responde) y ajustes (admin).
export const leerEstadoAsistente = (signal?: AbortSignal) => llamar<EstadoAsistente>("GET", `${M}/asistente/estado`, { signal });
export const leerAjustesAsistente = (signal?: AbortSignal) => llamar<AjustesAsistente>("GET", `${M}/asistente/ajustes`, { signal });
export const guardarAjustesAsistente = (cuerpo: EntradaAjustesAsistente, clave: string) =>
    llamar<AjustesAsistente>("POST", `${M}/asistente/ajustes/actualizar`, { params: { header: cabeceraIdem(clave) }, body: cuerpo });
export const probarAsistente = (cuerpo: { proveedor: ProveedorIA; url: string; modelo: string; clave?: string }) => llamar<PruebaAsistente>("POST", `${M}/asistente/probar`, { body: cuerpo });
export const leerModelos = async (p: { proveedor: ProveedorIA; url: string }, signal?: AbortSignal) =>
    (await llamar<{ data?: ModeloIA[] | null }>("GET", `${M}/asistente/modelos`, { params: { query: { proveedor: p.proveedor, ...(p.url ? { url: p.url } : {}) } }, signal })).data ?? [];
export const leerUso = (mes: string | undefined, signal?: AbortSignal) => llamar<UsoAsistente>("GET", `${M}/asistente/uso`, { params: { query: mes ? { mes } : {} }, signal });

// §16.2 Sugerencias.
export const pedirSugerencia = (conversacionId: number, otra: boolean, clave: string) =>
    llamar<Sugerencia>("POST", `${M}/conversaciones/{id}/sugerencias`, { params: { path: { id: conversacionId }, header: cabeceraIdem(clave) }, body: otra ? { otra: true } : {} });
export const leerSugerenciaActual = (conversacionId: number, signal?: AbortSignal) =>
    llamar<Sugerencia>("GET", `${M}/conversaciones/{id}/sugerencias/actual`, { params: { path: { id: conversacionId } }, signal });
export const usarSugerencia = (id: string, clave: string) => llamar<Sugerencia>("POST", `${M}/sugerencias/{id}/usar`, { params: { path: { id }, header: cabeceraIdem(clave) } });
export const descartarSugerencia = (id: string, clave: string) => llamar<Sugerencia>("POST", `${M}/sugerencias/{id}/descartar`, { params: { path: { id }, header: cabeceraIdem(clave) } });
export const calificarSugerencia = (id: string, util: boolean, clave: string) =>
    llamar<Sugerencia>("POST", `${M}/sugerencias/{id}/calificar`, { params: { path: { id }, header: cabeceraIdem(clave) }, body: { util } });

// §16.4 Resumen.
export const resumirConversacion = (conversacionId: number, clave: string) =>
    llamar<ResumenConversacion>("POST", `${M}/conversaciones/{id}/resumen`, { params: { path: { id: conversacionId }, header: cabeceraIdem(clave) } });
