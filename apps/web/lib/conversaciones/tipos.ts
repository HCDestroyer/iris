import type { Schema } from "@iris/contratos";

// Nombres cortos de los tipos generados de la bandeja (contratos/conversaciones.md). Solo alias:
// la fuente es packages/contratos.
export type Conversacion = Schema<"ConversacionCliente">;
export type ConversacionDetalle = Schema<"ConversacionClienteDetalle">;
export type Mensaje = Schema<"MensajeConversacion">;
export type EventoSistema = Schema<"EventoSistema">;
export type CanalRef = Schema<"CanalRef">;
export type Etiqueta = Schema<"EtiquetaConversacion">;
export type Cola = Schema<"ColaConversaciones">;
export type RespuestaRapida = Schema<"RespuestaRapida">;
export type Contadores = Schema<"ContadoresConversaciones">;
export type EventoBandeja = Schema<"EventoConversaciones">;
