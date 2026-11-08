"use client";

import type { Schema } from "@iris/contratos";
import { api, resolver } from "@/lib/api/cliente";
import type { Canal, PruebaCanal } from "./tipos";

/*
 * Llamadas de la pantalla «Números de WhatsApp» (contratos/conversaciones.md §1 y §1.1), todas con
 * `mensajeria.configurar`. Pasan por el cliente del panel (cookies, CSRF, empresa activa y
 * re-autenticación). Las credenciales van solo en el cuerpo de la petición: nunca en la URL, en
 * un log ni en un aviso.
 */

/** Todos los números de la empresa activa (activos e inactivos). La lista no se pagina. */
export async function leerCanales(signal?: AbortSignal): Promise<Canal[]> {
    const r = await resolver(api().GET("/api/v1/mensajeria/canales", { ...(signal ? { signal } : {}) }));
    return r.data ?? [];
}

/** Un número (404 si no existe o es de otra empresa). */
export function leerCanal(id: number, signal?: AbortSignal): Promise<Canal> {
    return resolver(api().GET("/api/v1/mensajeria/canales/{id}", { params: { path: { id } }, ...(signal ? { signal } : {}) }));
}

/**
 * Respuesta de crear o actualizar: el número **sin** `verifyTokenGenerado` y, aparte, el token
 * generado (solo cuando Iris lo generó). Así el token nunca queda guardado dentro del número en el
 * estado de una pantalla: la pantalla decide cuánto lo muestra y lo olvida.
 */
export type CanalConToken = { canal: Canal; verifyToken: string | null };

export function separarToken(respuesta: Canal): CanalConToken {
    const { verifyTokenGenerado, ...canal } = respuesta;
    return { canal, verifyToken: verifyTokenGenerado || null };
}

/** Alta: sin `verifyToken`, Iris lo genera y lo devuelve una sola vez. */
export async function crearCanal(body: Schema<"EntradaCrearCanal">, clave: string): Promise<CanalConToken> {
    return separarToken(await resolver(api().POST("/api/v1/mensajeria/canales", { params: { header: { "Idempotency-Key": clave } }, body })));
}

/** Datos y credenciales; un secreto vacío no cambia. `generarVerifyToken` = «Generar otro». */
export async function actualizarCanal(id: number, body: Schema<"EntradaActualizarCanal">, clave: string): Promise<CanalConToken> {
    return separarToken(await resolver(api().POST("/api/v1/mensajeria/canales/{id}/actualizar", { params: { path: { id }, header: { "Idempotency-Key": clave } }, body })));
}

/** Prueba con Meta las credenciales guardadas y dice si el webhook ya se verificó. */
export function probarCanal(id: number, clave: string): Promise<PruebaCanal> {
    return resolver(api().POST("/api/v1/mensajeria/canales/{id}/probar", { params: { path: { id }, header: { "Idempotency-Key": clave } } }));
}

export function desactivarCanal(id: number, clave: string): Promise<Canal> {
    return resolver(api().POST("/api/v1/mensajeria/canales/{id}/desactivar", { params: { path: { id }, header: { "Idempotency-Key": clave } } }));
}

export function activarCanal(id: number, clave: string): Promise<Canal> {
    return resolver(api().POST("/api/v1/mensajeria/canales/{id}/activar", { params: { path: { id }, header: { "Idempotency-Key": clave } } }));
}

/** Lo que manda «Credenciales» sin perder lo que no se toca (plantillas). */
export function cuerpoActualizar(canal: Pick<Canal, "version" | "nombre" | "numero" | "phoneNumberId" | "wabaId" | "plantillas">, cambios: Partial<Schema<"EntradaActualizarCanal">>): Schema<"EntradaActualizarCanal"> {
    return {
        version: canal.version,
        nombre: canal.nombre,
        numero: canal.numero,
        phoneNumberId: canal.phoneNumberId,
        wabaId: canal.wabaId,
        plantillas: canal.plantillas,
        ...cambios,
    };
}
