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
export type EventoBandeja = Schema<"EventoConversacion">;

// Fase 1.1 (§14).
export type Agente = Schema<"AgenteConversaciones">;
export type Numero = Schema<"NumeroConversaciones">;
export type Disponibilidad = Schema<"MiDisponibilidad">;
export type EstadoDisponibilidad = Disponibilidad["estado"];
export type OrigenDisponibilidad = Disponibilidad["origen"];
export type AjustesBandeja = Schema<"AjustesBandeja">;
export type BotNumero = Schema<"BotNumero">;
export type OpcionMenuBot = Schema<"OpcionMenuBot">;
export type Espera = Schema<"EsperaConversacion">;
export type Destinatario = Schema<"Destinatario">;
export type MotivoDestinatario = NonNullable<Destinatario["motivo"]>;
export type PlantillaDisponible = Schema<"PlantillaDisponible">;
export type MiembroEquipo = Schema<"MiembroEquipo">;
