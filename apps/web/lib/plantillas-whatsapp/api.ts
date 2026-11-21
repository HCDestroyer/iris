"use client";

import type { Schema } from "@iris/contratos";
import { api, resolver } from "@/lib/api/cliente";
import type { PlantillaWA } from "./tipos";

/* Llamadas de «Plantillas de WhatsApp» (contratos/plantillas-whatsapp.md §3). */

const sig = (signal?: AbortSignal) => (signal ? { signal } : {});
const idem = (clave: string) => ({ "Idempotency-Key": clave });

export type FiltrosLista = { wabaId?: string; grupo?: "activas" | "revision" | "atender"; categoria?: PlantillaWA["categoria"]; idioma?: string; buscar?: string; cursor?: string };

export function leerLista(f: FiltrosLista, signal?: AbortSignal) {
    const query: Record<string, string | number> = { limit: 50 };
    for (const [k, v] of Object.entries(f)) if (v) query[k] = v;
    return resolver(api().GET("/api/v1/mensajeria/plantillas-whatsapp", { params: { query }, ...sig(signal) }));
}

export async function leerCuentas(signal?: AbortSignal) {
    return (await resolver(api().GET("/api/v1/mensajeria/plantillas-whatsapp/cuentas", sig(signal)))).data ?? [];
}

export async function leerDatos(signal?: AbortSignal) {
    return (await resolver(api().GET("/api/v1/mensajeria/plantillas-whatsapp/datos", sig(signal)))).data ?? [];
}

export function leerPlantilla(id: number, signal?: AbortSignal) {
    return resolver(api().GET("/api/v1/mensajeria/plantillas-whatsapp/{id}", { params: { path: { id } }, ...sig(signal) }));
}

export function leerSugeridas(wabaId: string, signal?: AbortSignal) {
    return resolver(api().GET("/api/v1/mensajeria/plantillas-whatsapp/sugeridas", { params: { query: { wabaId } }, ...sig(signal) }));
}

export function leerAjustes(signal?: AbortSignal) {
    return resolver(api().GET("/api/v1/mensajeria/plantillas-whatsapp/ajustes", sig(signal)));
}

export function guardarAjustes(body: Schema<"EntradaAjustesPWA">, clave: string) {
    return resolver(api().POST("/api/v1/mensajeria/plantillas-whatsapp/ajustes", { params: { header: idem(clave) }, body }));
}

export function sincronizar(waba: string, clave: string) {
    return resolver(api().POST("/api/v1/mensajeria/plantillas-whatsapp/cuentas/{waba}/sincronizar", { params: { path: { waba }, header: idem(clave) } }));
}

export function crearPlantilla(body: Schema<"EntradaCrearPWA">, clave: string) {
    return resolver(api().POST("/api/v1/mensajeria/plantillas-whatsapp", { params: { header: idem(clave) }, body }));
}

export function crearSugerida(clave: string, body: Schema<"EntradaCrearSugeridaPWA">, idempotencia: string) {
    return resolver(api().POST("/api/v1/mensajeria/plantillas-whatsapp/sugeridas/{clave}/crear", { params: { path: { clave }, header: idem(idempotencia) }, body }));
}

export function actualizarBorrador(id: number, body: Schema<"EntradaEditarPWA">, clave: string) {
    return resolver(api().POST("/api/v1/mensajeria/plantillas-whatsapp/{id}/actualizar", { params: { path: { id }, header: idem(clave) }, body }));
}

export function enviar(id: number, version: number, clave: string) {
    return resolver(api().POST("/api/v1/mensajeria/plantillas-whatsapp/{id}/enviar", { params: { path: { id }, header: idem(clave) }, body: { version } }));
}

export function corregirEnMeta(id: number, body: Schema<"EntradaEditarMetaPWA">, clave: string) {
    return resolver(api().POST("/api/v1/mensajeria/plantillas-whatsapp/{id}/editar", { params: { path: { id }, header: idem(clave) }, body }));
}

export function cambiarUsos(id: number, body: Schema<"EntradaUsosPWA">, clave: string) {
    return resolver(api().POST("/api/v1/mensajeria/plantillas-whatsapp/{id}/usos", { params: { path: { id }, header: idem(clave) }, body }));
}

export function borrar(id: number, version: number, clave: string) {
    return resolver(api().POST("/api/v1/mensajeria/plantillas-whatsapp/{id}/borrar", { params: { path: { id }, header: idem(clave) }, body: { version } }));
}

export function reanudar(id: number, version: number, clave: string) {
    return resolver(api().POST("/api/v1/mensajeria/plantillas-whatsapp/{id}/reanudar", { params: { path: { id }, header: idem(clave) }, body: { version } }));
}
