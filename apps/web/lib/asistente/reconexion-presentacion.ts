import type { Schema } from "@iris/contratos";

/*
 * Reconexión temporal sin personal de caja (contrato con Argos 0.8.0 §7.3, B4): textos del
 * interruptor y del motivo en el hilo. Funciones puras (sin llamadas).
 */

type AjustesReconexion = Schema<"AjustesReconexion">;

/** Motivos de la reconexión temporal (§7.3). Lo decide el servidor, nunca el cliente. */
export type MotivoReconexion = "fuera_de_horario" | "sin_personal";

/** Texto corto del motivo; uno desconocido no se interpreta. */
export function textoMotivoReconexion(motivo: string | undefined): string | null {
    if (motivo === "fuera_de_horario") return "fuera de horario";
    if (motivo === "sin_personal") return "nadie de caja disponible";
    return null;
}

/**
 * Qué pasa hoy con el interruptor, en llano: si de verdad se pedirá o qué falta. El orden importa:
 * primero lo que el admin puede resolver aquí.
 */
export type EstadoSinPersonal = { tono: "success" | "info" | "warning"; titulo: string; detalle: string };

export function estadoSinPersonal(a: Pick<AjustesReconexion, "argosLoPermite" | "caja" | "contratoMinimo"> & { accionEncendida?: boolean }, encendido: boolean): EstadoSinPersonal {
    if (!encendido) {
        return { tono: "info", titulo: "Apagado", detalle: "En horario hábil, la boleta siempre la revisa una persona de caja." };
    }
    // Sin la acción «Reconexión temporal» en «¿Qué puede hacer solo?» el asistente no pide ninguna.
    if (a.accionEncendida === false) {
        return {
            tono: "warning",
            titulo: "«Reconexión temporal» está apagada",
            detalle: "Enciéndela en «¿Qué puede hacer solo?»: sin ella el asistente no pide ninguna reconexión.",
        };
    }
    if (!a.argosLoPermite) {
        return {
            tono: "warning",
            titulo: "Argos todavía no lo permite",
            detalle: `Hace falta un Argos con el contrato ${a.contratoMinimo} o más nuevo. Mientras tanto no se pide.`,
        };
    }
    if (a.caja.total === 0) {
        return { tono: "warning", titulo: "Nadie tiene el rol Caja", detalle: "Asigna el rol Caja a quien revisa las boletas. Sin nadie de caja no se pide nunca." };
    }
    const personas = a.caja.total === 1 ? "1 persona de caja" : `${a.caja.total} personas de caja`;
    const disponibles = a.caja.disponibles === 0 ? "ninguna está disponible ahora" : a.caja.disponibles === 1 ? "1 está disponible ahora" : `${a.caja.disponibles} están disponibles ahora`;
    return { tono: "success", titulo: "Listo", detalle: `${personas}; ${disponibles}.` };
}

/** Hora de Guatemala del `hasta` de Argos, para el hilo (no es la hora del corte). */
export function hastaCorto(hasta: string | undefined): string | null {
    if (!hasta) return null;
    const d = new Date(hasta);
    if (Number.isNaN(d.getTime())) return null;
    const f = new Intl.DateTimeFormat("es-GT", { timeZone: "America/Guatemala", weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
    return f.format(d);
}
