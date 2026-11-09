import type { Schema } from "@iris/contratos";

// Nombres cortos de los tipos generados del asistente de IA (contratos/conversaciones.md §16).
// Solo alias: la fuente es packages/contratos.
export type Sugerencia = Schema<"Sugerencia">;
export type FuenteSugerencia = Schema<"FuenteSugerencia">;
export type EstadoSugerencia = Sugerencia["estado"];
export type EstadoAsistente = Schema<"EstadoAsistente">;
export type AjustesAsistente = Schema<"AjustesAsistente">;
export type EntradaAjustesAsistente = Schema<"EntradaAjustesAsistente">;
export type ProveedorIA = AjustesAsistente["proveedor"];
export type PruebaAsistente = Schema<"PruebaAsistente">;
export type ModeloIA = Schema<"ModeloIA">;
export type UsoAsistente = Schema<"UsoAsistente">;
export type ResumenConversacion = Schema<"ResumenConversacion">;
