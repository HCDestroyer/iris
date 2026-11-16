import type { Schema } from "@iris/contratos";
import type { ActividadAsistente, CategoriaRef } from "../asistente/fase3";

// Nombres cortos de los tipos generados de la bandeja (contratos/conversaciones.md). Solo alias:
// la fuente es packages/contratos.

/**
 * Fase 3 del asistente (§17, provisional hasta que su OpenAPI llegue a `packages/contratos`): la
 * categoría de la conversación, la línea «qué hizo el asistente» y si está atendiendo solo. Todo
 * opcional: un servidor anterior no los manda.
 */
type ExtrasConversacion = { categoria?: CategoriaRef | null; actividadAsistente?: ActividadAsistente | null; asistenteAtiende?: boolean };
/** Fase 3 (provisional): quién escribió el mensaje cuando no fue una persona. */
type ExtrasMensaje = { autorTipo?: "persona" | "asistente_ia" | null };

export type Conversacion = Schema<"ConversacionCliente"> & ExtrasConversacion;
export type ConversacionDetalle = Schema<"ConversacionClienteDetalle"> & ExtrasConversacion;
export type Mensaje = Schema<"MensajeConversacion"> & ExtrasMensaje;
export type EventoSistema = Schema<"EventoSistema">;
export type CanalRef = Schema<"CanalRef">;
export type Etiqueta = Schema<"EtiquetaConversacion">;
export type Cola = Schema<"ColaConversaciones">;
export type RespuestaRapida = Schema<"RespuestaRapida">;
export type Contadores = Schema<"ContadoresConversaciones"> & { porCategoria?: { id: number; total: number }[] | null };
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
