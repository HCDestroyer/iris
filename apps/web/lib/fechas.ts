/** Zona en que se muestran las fechas (en la base y la API todo es UTC). */
export const ZONA = "America/Guatemala";

const formato = new Intl.DateTimeFormat("es-GT", {
    timeZone: ZONA,
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
});

/** `dd/mm/aaaa HH:mm` en America/Guatemala; `undefined` si no hay fecha válida. */
export function formatearFecha(iso: string | null | undefined): string | undefined {
    if (!iso) {
        return undefined;
    }
    const ms = Date.parse(iso);
    if (Number.isNaN(ms)) {
        return undefined;
    }
    const partes = Object.fromEntries(formato.formatToParts(ms).map((p) => [p.type, p.value]));
    return `${partes.day}/${partes.month}/${partes.year} ${partes.hour}:${partes.minute}`;
}

/** «hace 5 min», «hace 2 h», o la fecha completa si pasó más de un día. */
export function haceCuanto(iso: string, ahora: number): string {
    const ms = Date.parse(iso);
    if (Number.isNaN(ms)) {
        return "";
    }
    const min = Math.floor((ahora - ms) / 60_000);
    if (min < 1) {
        return "hace un momento";
    }
    if (min < 60) {
        return `hace ${min} min`;
    }
    if (min < 24 * 60) {
        return `hace ${Math.floor(min / 60)} h`;
    }
    return formatearFecha(iso) ?? "";
}
