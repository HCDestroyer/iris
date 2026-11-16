import { api, resolver } from "../api/cliente";
import { cabeceraIdem } from "../conversaciones/api";
import type { AjustesDePrueba, EntradaCategoria } from "./fase3";

/*
 * Llamadas de la fase 3 del asistente (contratos/conversaciones.md §17, base
 * `/api/v1/mensajeria`), con el cliente generado de packages/contratos. Las acciones llevan
 * `Idempotency-Key`.
 */

const M = "/api/v1/mensajeria";

/** Categorías de la empresa por `orden` (`conversaciones.ver`); con `todas`, también las inactivas. */
export const leerCategorias = async (todas: boolean, signal?: AbortSignal) =>
    (await resolver(api().GET(`${M}/asistente/categorias`, { params: { query: todas ? { incluirInactivas: true } : {} }, signal }))).data ?? [];
export const crearCategoria = (cuerpo: EntradaCategoria, clave: string) => resolver(api().POST(`${M}/asistente/categorias`, { params: { header: cabeceraIdem(clave) }, body: cuerpo }));
export const actualizarCategoria = (id: number, cuerpo: EntradaCategoria & { version: number }, clave: string) =>
    resolver(api().POST(`${M}/asistente/categorias/{id}/actualizar`, { params: { path: { id }, header: cabeceraIdem(clave) }, body: cuerpo }));
export const activarCategoria = (id: number, activa: boolean, version: number, clave: string) => {
    const opciones = { params: { path: { id }, header: cabeceraIdem(clave) }, body: { version } };
    return resolver(activa ? api().POST(`${M}/asistente/categorias/{id}/activar`, opciones) : api().POST(`${M}/asistente/categorias/{id}/desactivar`, opciones));
};

/** Prueba un mensaje con los ajustes del formulario **sin guardar**: no envía ni ejecuta nada. */
export const probarMensaje = (texto: string, ajustes: AjustesDePrueba, signal?: AbortSignal) => resolver(api().POST(`${M}/asistente/probar-mensaje`, { body: { texto, ajustes }, signal }));

export const leerEfecto = (mes: string | undefined, signal?: AbortSignal) => resolver(api().GET(`${M}/asistente/efecto`, { params: { query: mes ? { mes } : {} }, signal }));

/** Corrige la categoría de una conversación (`null` la quita). Devuelve el detalle. */
export const categorizarConversacion = (id: number, categoriaId: number | null, version: number, clave: string) =>
    resolver(api().POST(`${M}/conversaciones/{id}/categorizar`, { params: { path: { id }, header: cabeceraIdem(clave) }, body: { categoriaId, version } }));
