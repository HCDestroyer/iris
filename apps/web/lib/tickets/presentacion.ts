import type { Schema } from "@iris/contratos";
import type { Tono } from "@iris/ui";
import {
    Archive,
    CircleAlert,
    CircleCheck,
    CircleDot,
    CirclePause,
    CirclePlus,
    Info,
    OctagonAlert,
    TriangleAlert,
    type LucideIcon,
} from "lucide-react";

export type EstadoTicket = Schema<"Ticket">["estado"];
export type Prioridad = Schema<"Ticket">["prioridad"];
export type Origen = Schema<"Ticket">["origen"];

type Presentacion = { tono: Tono; icono: LucideIcon; texto: string };

/** Estados de flujo: siempre `neutral` (la urgencia la dan la prioridad y el SLA). */
export const ESTADOS: Record<EstadoTicket, Presentacion> = {
    nuevo: { tono: "neutral", icono: CirclePlus, texto: "Nuevo" },
    abierto: { tono: "neutral", icono: CircleDot, texto: "Abierto" },
    en_espera: { tono: "neutral", icono: CirclePause, texto: "En espera" },
    resuelto: { tono: "neutral", icono: CircleCheck, texto: "Resuelto" },
    cerrado: { tono: "neutral", icono: Archive, texto: "Cerrado" },
};

/** Severidad (skill horus-identity, estados.md): crítica y alta comparten color, no icono. */
export const PRIORIDADES: Record<Prioridad, Presentacion> = {
    critica: { tono: "error", icono: OctagonAlert, texto: "Crítica" },
    alta: { tono: "error", icono: TriangleAlert, texto: "Alta" },
    media: { tono: "warning", icono: CircleAlert, texto: "Media" },
    baja: { tono: "neutral", icono: Info, texto: "Baja" },
};

export const ORDEN_ESTADOS: readonly EstadoTicket[] = ["nuevo", "abierto", "en_espera", "resuelto", "cerrado"];
/** De menor a mayor. */
export const ORDEN_PRIORIDADES: readonly Prioridad[] = ["baja", "media", "alta", "critica"];

export function prioridadMayor(a: Prioridad, b: Prioridad): boolean {
    return ORDEN_PRIORIDADES.indexOf(a) > ORDEN_PRIORIDADES.indexOf(b);
}

export const ORIGENES: Record<Origen, string> = {
    panel: "Panel",
    portal: "Portal",
    correo: "Correo",
    telegram: "Telegram",
    horus: "Horus",
    argos: "Argos",
    api: "API",
};
