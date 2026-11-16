import { enviarProvisional, leerProvisional } from "../api/provisional";
import type { Conversacion } from "../conversaciones/tipos";
import type { AjustesDePrueba, AjustesFase3, Categoria, EfectoAsistente, EntradaAjustesFase3, EntradaCategoria, PruebaMensaje } from "./fase3";

/*
 * Llamadas de la fase 3 del asistente (contratos/conversaciones.md §17, base
 * `/api/v1/mensajeria`). Provisionales (`lib/api/provisional.ts`) hasta que el OpenAPI de §17
 * llegue a `packages/contratos`. Las acciones llevan `Idempotency-Key`.
 */

const M = "/api/v1/mensajeria";

export const leerAjustesFase3 = (signal?: AbortSignal) => leerProvisional<AjustesFase3>(`${M}/asistente/ajustes`, { signal });
export const guardarAjustesFase3 = (cuerpo: EntradaAjustesFase3, clave: string) => enviarProvisional<AjustesFase3>(`${M}/asistente/ajustes/actualizar`, { cuerpo, idempotencia: clave });

export const leerCategorias = async (signal?: AbortSignal) => (await leerProvisional<{ data: Categoria[] | null }>(`${M}/asistente/categorias`, { signal })).data ?? [];
export const crearCategoria = (cuerpo: EntradaCategoria, clave: string) => enviarProvisional<Categoria>(`${M}/asistente/categorias`, { cuerpo, idempotencia: clave });
export const actualizarCategoria = (id: number, cuerpo: EntradaCategoria & { version: number }, clave: string) =>
    enviarProvisional<Categoria>(`${M}/asistente/categorias/${id}/actualizar`, { cuerpo, idempotencia: clave });
export const activarCategoria = (id: number, activa: boolean, version: number, clave: string) =>
    enviarProvisional<Categoria>(`${M}/asistente/categorias/${id}/${activa ? "activar" : "desactivar"}`, { cuerpo: { version }, idempotencia: clave });

/** Prueba un mensaje con los ajustes del formulario **sin guardar**: no envía ni guarda nada. */
export const probarMensaje = (texto: string, ajustes: AjustesDePrueba, signal?: AbortSignal) =>
    enviarProvisional<PruebaMensaje>(`${M}/asistente/probar-mensaje`, { cuerpo: { texto, ajustes }, signal });

export const leerEfecto = (mes: string | undefined, signal?: AbortSignal) => leerProvisional<EfectoAsistente>(`${M}/asistente/efecto`, { query: { mes }, signal });

/** Cambia la categoría de una conversación (`null` la quita). Devuelve la conversación. */
export const categorizarConversacion = (id: number, categoriaId: number | null, version: number, clave: string) =>
    enviarProvisional<Conversacion>(`${M}/conversaciones/${id}/categorizar`, { cuerpo: { categoriaId, version }, idempotencia: clave });
