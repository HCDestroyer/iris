import { ZONA } from "@/lib/fechas";

const dia = new Intl.DateTimeFormat("en-CA", { timeZone: ZONA, year: "numeric", month: "2-digit", day: "2-digit" });
const hora = new Intl.DateTimeFormat("es-GT", { timeZone: ZONA, hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
const largo = new Intl.DateTimeFormat("es-GT", { timeZone: ZONA, weekday: "long", day: "numeric", month: "long" });

/** Día de hoy en Guatemala (`YYYY-MM-DD`), el del paquete. */
export function hoyEnGuatemala(ms = Date.now()): string {
    return dia.format(ms);
}

/** `HH:mm` en Guatemala (o `undefined`). */
export function horaCorta(iso: string | null | undefined): string | undefined {
    const ms = iso ? Date.parse(iso) : Number.NaN;
    return Number.isNaN(ms) ? undefined : hora.format(ms);
}

/** «miércoles, 28 de octubre» para el encabezado del día. */
export function diaLargo(fecha: string): string {
    const ms = Date.parse(`${fecha}T12:00:00-06:00`);
    return Number.isNaN(ms) ? fecha : largo.format(ms);
}
