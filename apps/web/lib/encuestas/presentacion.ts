import type { Schema } from "@iris/contratos";
import type { Tono } from "@iris/ui";
import { Annoyed, Ban, CircleCheck, Clock, Frown, Laugh, Meh, Smile, TimerOff, type LucideIcon } from "lucide-react";

/*
 * Presentación de las encuestas de satisfacción (Fase 5 · punto 2): calificación 1–5 con texto
 * (nunca solo estrellas o caras) y estado con icono + texto + color.
 */

export type Encuesta = Schema<"Encuesta">;
export type EstadoEncuesta = Encuesta["estado"];
export type EntidadEncuesta = Encuesta["entidad"];

type Presentacion = { tono: Tono; icono: LucideIcon; texto: string };

export const CALIFICACIONES = [1, 2, 3, 4, 5] as const;
export type Calificacion = (typeof CALIFICACIONES)[number];

/** Texto de cada calificación; ≤ 2 es una alerta (`alerta: true` en la API). */
export const TEXTO_CALIFICACION: Record<Calificacion, { texto: string; icono: LucideIcon }> = {
    1: { texto: "Muy mal", icono: Frown },
    2: { texto: "Mal", icono: Annoyed },
    3: { texto: "Regular", icono: Meh },
    4: { texto: "Bien", icono: Smile },
    5: { texto: "Muy bien", icono: Laugh },
};

export const UMBRAL_ALERTA = 2;

export function esCalificacion(n: unknown): n is Calificacion {
    return typeof n === "number" && (CALIFICACIONES as readonly number[]).includes(n);
}

/** «4 · Bien»; sin calificación, «Sin calificar». */
export function textoCalificacion(n: number | null | undefined): string {
    return esCalificacion(n) ? `${n} · ${TEXTO_CALIFICACION[n].texto}` : "Sin calificar";
}

/** Estados de flujo en `neutral`; la urgencia la da la alerta de la calificación. */
export const ESTADOS_ENCUESTA: Record<string, Presentacion> = {
    pendiente: { tono: "neutral", icono: Clock, texto: "Pendiente" },
    respondida: { tono: "success", icono: CircleCheck, texto: "Respondida" },
    vencida: { tono: "neutral", icono: TimerOff, texto: "Vencida" },
    anulada: { tono: "neutral", icono: Ban, texto: "Anulada" },
};

export function presentacionEstado(estado: string): Presentacion {
    return ESTADOS_ENCUESTA[estado] ?? { tono: "neutral", icono: Clock, texto: estado };
}

export const ENTIDADES: Record<EntidadEncuesta, string> = { ticket: "Ticket", orden: "Orden" };

/** Promedio 1–5 con un decimal; `null` = sin respuestas → «Sin datos». */
export function textoPromedio(v: number | null | undefined): string {
    if (v === null || v === undefined || !Number.isFinite(v)) return "Sin datos";
    return `${v.toLocaleString("es-GT", { minimumFractionDigits: 1, maximumFractionDigits: 1 })} de 5`;
}

/** Largo máximo del comentario (`CuerpoResponderEncuesta.comentario`). */
export const MAX_COMENTARIO = 1000;
