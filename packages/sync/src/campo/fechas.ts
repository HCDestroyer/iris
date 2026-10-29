/** Zona en que se muestran las horas (en la base todo es UTC). */
export const ZONA = "America/Guatemala";

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

/** «08:00–10:00»: ventana de llegada acordada con el cliente (o `undefined` si la cita no tiene). */
export function ventanaCita(cita: { ventanaInicio: string | null; ventanaFin: string | null } | undefined): string | undefined {
    const inicio = horaCorta(cita?.ventanaInicio);
    const fin = horaCorta(cita?.ventanaFin);
    return inicio && fin ? `${inicio}–${fin}` : inicio ? `desde ${inicio}` : fin ? `hasta ${fin}` : undefined;
}

/** «1 h 05 min», «15 min» o «menos de 1 min» (tiempo pausado). */
export function duracionCorta(segundos: number): string {
    const min = Math.floor(Math.max(0, segundos) / 60);
    if (min < 1) {
        return "menos de 1 min";
    }
    const h = Math.floor(min / 60);
    const m = min % 60;
    return h > 0 ? `${h} h ${String(m).padStart(2, "0")} min` : `${m} min`;
}
