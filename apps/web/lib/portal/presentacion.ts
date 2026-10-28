import type { Schema } from "@iris/contratos";
import type { Tono } from "@iris/ui";
import { Archive, CircleCheck, CirclePause, Headset, Inbox, type LucideIcon } from "lucide-react";

export type TicketPortal = Schema<"TicketPortal">;
export type EstadoPortal = TicketPortal["estado"];

type Presentacion = { tono: Tono; icono: LucideIcon; texto: string; explicacion: string };

/** Estado en lenguaje del cliente (icono + texto + color), sin jerga interna. */
export const ESTADOS_PORTAL: Record<EstadoPortal, Presentacion> = {
    nuevo: { tono: "info", icono: Inbox, texto: "Recibido", explicacion: "Recibimos tu caso y pronto lo atenderemos." },
    abierto: { tono: "info", icono: Headset, texto: "En atención", explicacion: "Nuestro equipo está trabajando en tu caso." },
    en_espera: { tono: "warning", icono: CirclePause, texto: "En espera", explicacion: "Tu caso está en pausa mientras esperamos un dato o una visita." },
    resuelto: { tono: "success", icono: CircleCheck, texto: "Resuelto", explicacion: "Creemos que ya quedó resuelto. Confírmanos o respóndenos si algo sigue mal." },
    cerrado: { tono: "neutral", icono: Archive, texto: "Cerrado", explicacion: "Este caso está cerrado." },
};

/** Filtro de la lista: los que siguen en curso o todos. */
export const ESTADOS_EN_CURSO: EstadoPortal[] = ["nuevo", "abierto", "en_espera", "resuelto"];
