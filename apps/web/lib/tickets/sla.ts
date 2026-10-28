import type { Schema } from "@iris/contratos";
import type { Tono } from "@iris/ui";
import {
    AlarmClock,
    CircleCheck,
    CircleDashed,
    CirclePause,
    CircleX,
    Clock,
    TimerOff,
    type LucideIcon,
} from "lucide-react";
import { formatearFecha } from "../fechas";

export type Ticket = Schema<"Ticket">;
export type RelojSLA = Schema<"RelojSLA">;
export type EstadoSLA = RelojSLA["estado"];
export type TipoReloj = "respuesta" | "resolucion";

/*
 * SLA visible (icono + texto + color). El estado viene calculado de la API al leer; mientras la
 * pantalla sigue abierta, un reloj «en tiempo» o «en riesgo» pasa a «en riesgo» o «vencido» en
 * cuanto la hora local cruza `riesgoEn` o `venceEn` (mismos datos leídos, sin inventar nada).
 */

export function estadoEfectivo(reloj: RelojSLA, ahora: number): EstadoSLA {
    if (reloj.estado !== "en_tiempo" && reloj.estado !== "en_riesgo") {
        return reloj.estado;
    }
    const vence = reloj.venceEn ? Date.parse(reloj.venceEn) : Number.NaN;
    if (!Number.isNaN(vence) && ahora >= vence) {
        return "vencido";
    }
    const riesgo = reloj.riesgoEn ? Date.parse(reloj.riesgoEn) : Number.NaN;
    if (reloj.estado === "en_tiempo" && !Number.isNaN(riesgo) && ahora >= riesgo) {
        return "en_riesgo";
    }
    return reloj.estado;
}

/** Reloj que manda en la bandeja: la primera respuesta mientras esté pendiente; si no, la resolución. */
export function relojPrincipal(ticket: Ticket): { tipo: TipoReloj; reloj: RelojSLA } {
    const pendiente = (r: RelojSLA) => r.estado === "en_tiempo" || r.estado === "en_riesgo" || r.estado === "vencido";
    if (ticket.estado !== "resuelto" && ticket.estado !== "cerrado" && pendiente(ticket.slaRespuesta)) {
        return { tipo: "respuesta", reloj: ticket.slaRespuesta };
    }
    return { tipo: "resolucion", reloj: ticket.slaResolucion };
}

/** «2 h 15 min», «3 d 4 h», «45 min», «menos de 1 min». */
export function formatearDuracion(ms: number): string {
    const minutos = Math.floor(Math.abs(ms) / 60_000);
    if (minutos < 1) {
        return "menos de 1 min";
    }
    if (minutos < 60) {
        return `${minutos} min`;
    }
    const horas = Math.floor(minutos / 60);
    if (horas < 24) {
        const resto = minutos % 60;
        return resto ? `${horas} h ${resto} min` : `${horas} h`;
    }
    const dias = Math.floor(horas / 24);
    const restoH = horas % 24;
    return restoH ? `${dias} d ${restoH} h` : `${dias} d`;
}

export const PRESENTACION_SLA: Record<EstadoSLA, { tono: Tono; icono: LucideIcon; texto: string }> = {
    en_tiempo: { tono: "success", icono: Clock, texto: "En tiempo" },
    en_riesgo: { tono: "warning", icono: AlarmClock, texto: "En riesgo" },
    vencido: { tono: "error", icono: TimerOff, texto: "Vencido" },
    pausado: { tono: "neutral", icono: CirclePause, texto: "Pausado" },
    cumplido: { tono: "success", icono: CircleCheck, texto: "Cumplido" },
    incumplido: { tono: "warning", icono: CircleX, texto: "Incumplido" },
    sin_sla: { tono: "neutral", icono: CircleDashed, texto: "Sin SLA" },
};

export const NOMBRE_RELOJ: Record<TipoReloj, string> = { respuesta: "Respuesta", resolucion: "Resolución" };

export type DescripcionSLA = {
    estado: EstadoSLA;
    tono: Tono;
    icono: LucideIcon;
    texto: string;
    /** Tiempo legible: «vence en 2 h», «venció hace 15 min», «en espera»… */
    detalle: string | undefined;
    /** Fecha y hora absolutas (America/Guatemala) del vencimiento o del cumplimiento. */
    fecha: string | undefined;
};

export function describirSla(reloj: RelojSLA, ahora: number): DescripcionSLA {
    const estado = estadoEfectivo(reloj, ahora);
    const base = PRESENTACION_SLA[estado];
    const vence = reloj.venceEn ? Date.parse(reloj.venceEn) : Number.NaN;
    let detalle: string | undefined;
    let fecha: string | undefined;
    switch (estado) {
        case "en_tiempo":
        case "en_riesgo":
            if (!Number.isNaN(vence)) {
                detalle = `vence en ${formatearDuracion(vence - ahora)}`;
                fecha = formatearFecha(reloj.venceEn);
            }
            break;
        case "vencido":
            if (!Number.isNaN(vence)) {
                detalle = `venció hace ${formatearDuracion(ahora - vence)}`;
                fecha = formatearFecha(reloj.venceEn);
            }
            break;
        case "pausado":
            detalle = "en espera, sin contar";
            break;
        case "cumplido":
        case "incumplido":
            if (reloj.cumplidoEn) {
                fecha = formatearFecha(reloj.cumplidoEn);
                detalle = estado === "cumplido" ? "a tiempo" : "fuera de plazo";
            }
            break;
        case "sin_sla":
            break;
    }
    return { estado, ...base, detalle, fecha };
}
