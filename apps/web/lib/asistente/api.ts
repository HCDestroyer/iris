import type { Schema } from "@iris/contratos";
import { api, resolver } from "../api/cliente";
import { cabeceraIdem } from "../conversaciones/api";
import type { EntradaAjustesAsistente } from "./tipos";

/*
 * Llamadas del asistente de IA (contratos/conversaciones.md §16, base `/api/v1/mensajeria`), con
 * el cliente generado de packages/contratos. Las acciones llevan `Idempotency-Key`.
 */

const M = "/api/v1/mensajeria";

// §16.1 Estado (quien responde) y ajustes (admin).
export const leerEstadoAsistente = (signal?: AbortSignal) => resolver(api().GET(`${M}/asistente/estado`, { signal }));
export const leerAjustesAsistente = (signal?: AbortSignal) => resolver(api().GET(`${M}/asistente/ajustes`, { signal }));
export const guardarAjustesAsistente = (cuerpo: EntradaAjustesAsistente, clave: string) =>
    resolver(api().POST(`${M}/asistente/ajustes/actualizar`, { params: { header: cabeceraIdem(clave) }, body: cuerpo }));
export const probarAsistente = (cuerpo: Schema<"EntradaProbarAsistente">) => resolver(api().POST(`${M}/asistente/probar`, { body: cuerpo }));
/** Modelos del proveedor guardado (`GET`, sin parámetros). */
export const leerModelosGuardados = async (signal?: AbortSignal) => (await resolver(api().GET(`${M}/asistente/modelos`, { signal }))).data ?? [];
/** Modelos de otro proveedor, dirección o clave (`POST` con CSRF; la clave no va en la URL). */
export const consultarModelos = async (cuerpo: Schema<"EntradaModelosAsistente">, signal?: AbortSignal) =>
    (await resolver(api().POST(`${M}/asistente/modelos`, { body: cuerpo, signal }))).data ?? [];
export const leerUso = (mes: string | undefined, signal?: AbortSignal) => resolver(api().GET(`${M}/asistente/uso`, { params: { query: mes ? { mes } : {} }, signal }));

// §16.2 Sugerencias.
export const pedirSugerencia = (conversacionId: number, otra: boolean, clave: string) =>
    resolver(api().POST(`${M}/conversaciones/{id}/sugerencias`, { params: { path: { id: conversacionId }, header: cabeceraIdem(clave) }, body: otra ? { otra: true } : {} }));
export const leerSugerenciaActual = (conversacionId: number, signal?: AbortSignal) =>
    resolver(api().GET(`${M}/conversaciones/{id}/sugerencias/actual`, { params: { path: { id: conversacionId } }, signal }));
export const usarSugerencia = (id: string, clave: string) => resolver(api().POST(`${M}/sugerencias/{id}/usar`, { params: { path: { id }, header: cabeceraIdem(clave) } }));
export const descartarSugerencia = (id: string, clave: string) => resolver(api().POST(`${M}/sugerencias/{id}/descartar`, { params: { path: { id }, header: cabeceraIdem(clave) } }));
export const calificarSugerencia = (id: string, util: boolean, clave: string) =>
    resolver(api().POST(`${M}/sugerencias/{id}/calificar`, { params: { path: { id }, header: cabeceraIdem(clave) }, body: { util } }));

// §16.4 Resumen.
export const resumirConversacion = (conversacionId: number, clave: string) =>
    resolver(api().POST(`${M}/conversaciones/{id}/resumen`, { params: { path: { id: conversacionId }, header: cabeceraIdem(clave) } }));
