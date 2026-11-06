// Textos de «Notificaciones» (diseño aprobado por el PM): el resumen de la fila de «Yo», la línea de
// No molestar y la validación de las horas. Puro: lo prueban las pruebas sin pantallas.
import type { PreferenciasPush } from "./api";
import type { PermisoPush } from "./servicio";

export const HORA = /^([01]\d|2[0-3]):[0-5]\d$/;

const minutos = (hhmm: string) => {
    const [h, m] = hhmm.split(":").map(Number);
    return (h ?? 0) * 60 + (m ?? 0);
};

/** Minutos del día en la zona (America/Guatemala por defecto). */
export function minutosEnZona(ahora: number, zona: string): number {
    try {
        const partes = new Intl.DateTimeFormat("en-GB", { timeZone: zona, hour: "2-digit", minute: "2-digit", hourCycle: "h23" }).formatToParts(new Date(ahora));
        const h = Number(partes.find((p) => p.type === "hour")?.value ?? 0);
        const m = Number(partes.find((p) => p.type === "minute")?.value ?? 0);
        return h * 60 + m;
    } catch {
        return new Date(ahora).getUTCHours() * 60 + new Date(ahora).getUTCMinutes() - 6 * 60;
    }
}

/** ¿Está dentro del horario de No molestar? (`desde > hasta` cruza la medianoche). */
export function enNoMolestar(nm: PreferenciasPush["noMolestar"], ahoraMin: number): boolean {
    if (!nm.activo) {
        return false;
    }
    const d = minutos(nm.desde);
    const h = minutos(nm.hasta);
    return d < h ? ahoraMin >= d && ahoraMin < h : ahoraMin >= d || ahoraMin < h;
}

/** La línea bajo el horario: «Hoy empieza a las 21:00…» o «Activo hasta las 06:00…». */
export function lineaNoMolestar(p: PreferenciasPush, ahora: number): string {
    const nm = p.noMolestar;
    if (!nm.activo) {
        return "Apagado: te avisamos a cualquier hora.";
    }
    const urgente = nm.urgenteIgual ? " Lo urgente te llega igual." : "";
    if (enNoMolestar(nm, minutosEnZona(ahora, p.zonaHoraria))) {
        return `Activo hasta las ${nm.hasta}. Lo que llegue se ve al abrir Iris.${urgente}`;
    }
    return `Hoy empieza a las ${nm.desde}. Lo que llegue se ve al abrir Iris.${urgente}`;
}

/** El detalle de la fila «Notificaciones» en «Yo». */
export function resumenNotificaciones(p: PreferenciasPush | null, permiso: PermisoPush | null): string | null {
    if (permiso === "denegado") {
        return "Bloqueadas en este teléfono";
    }
    if (!p) {
        return null;
    }
    if (p.push.estado !== "configurado") {
        return p.push.texto || "Notificaciones push: sin configurar";
    }
    if (!p.tipos.chat && !p.tipos.mencion && !p.tipos.orden && !(p.bandejaDisponible && p.tipos.bandeja)) {
        return "Todas apagadas";
    }
    return p.noMolestar.activo ? `No molestar de ${p.noMolestar.desde} a ${p.noMolestar.hasta}` : "Activas";
}

/** Valida una hora nueva de No molestar: el mensaje de error o `null`. */
export function errorHora(valor: string, otra: string): string | null {
    if (!HORA.test(valor.trim())) {
        return "Escribe la hora como 21:00 (de 00:00 a 23:59).";
    }
    if (valor.trim() === otra) {
        return "«Desde» y «Hasta» no pueden ser iguales.";
    }
    return null;
}
