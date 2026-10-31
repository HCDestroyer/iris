import type { Tono } from "@iris/ui";
import { Ban, CircleCheck, CircleDashed, CircleMinus, Gauge, type LucideIcon } from "lucide-react";

/** Cómo se muestra el estado de un servicio de Argos (y el resumen del cliente): icono + texto + color. */
export type PresentacionEstado = { tono: Tono; icono: LucideIcon; texto: string };

export const ESTADOS_SERVICIO: Record<string, PresentacionEstado> = {
    pendiente_instalacion: { tono: "info", icono: CircleDashed, texto: "Pendiente de instalación" },
    activo: { tono: "success", icono: CircleCheck, texto: "Activo" },
    limitado: { tono: "warning", icono: Gauge, texto: "Limitado" },
    suspendido: { tono: "error", icono: Ban, texto: "Suspendido" },
    retirado: { tono: "neutral", icono: CircleMinus, texto: "Retirado" },
};

/** Estado de Argos para mostrar; uno que Iris no conoce se muestra tal cual, neutro. null si no hay estado. */
export function presentacionEstado(estado: string | null | undefined): PresentacionEstado | null {
    const e = (estado ?? "").trim();
    if (e === "") return null;
    return ESTADOS_SERVICIO[e] ?? { tono: "neutral", icono: CircleMinus, texto: e.replaceAll("_", " ") };
}

export const TIPOS_CLIENTE: Record<string, string> = { persona: "Persona", empresa: "Empresa" };

export const TIPOS_CONEXION: Record<string, string> = { pppoe: "PPPoE", ip_estatica: "IP estática", dhcp_fija: "DHCP fija" };

/** Etiqueta del documento fiscal según su tipo en Argos. */
export function etiquetaDocumento(tipo: string): string {
    switch (tipo) {
        case "cui":
            return "CUI";
        case "pasaporte":
            return "Pasaporte";
        case "cf":
            return "Consumidor final";
        default:
            return "NIT";
    }
}

/** «hace 5 min», «hace 3 h», «hace 2 días»: cuánto hace que se leyó de Argos. */
export function haceTiempo(iso: string, ahora: number): string {
    const ms = Date.parse(iso);
    if (Number.isNaN(ms)) return "";
    const min = Math.max(0, Math.floor((ahora - ms) / 60_000));
    if (min < 1) return "hace un momento";
    if (min < 60) return `hace ${min} min`;
    const h = Math.floor(min / 60);
    if (h < 24) return `hace ${h} h`;
    const d = Math.floor(h / 24);
    return d === 1 ? "hace 1 día" : `hace ${d} días`;
}

/** Una instantánea de más de 24 h ya no se considera al día (la API la relee al abrir la ficha). */
export const VIGENCIA_MS = 24 * 60 * 60 * 1000;

export function desactualizado(iso: string, ahora: number): boolean {
    const ms = Date.parse(iso);
    return !Number.isNaN(ms) && ahora - ms > VIGENCIA_MS;
}
