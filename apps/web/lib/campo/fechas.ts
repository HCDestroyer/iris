// Fechas de la app de campo en Guatemala: viven en `@iris/sync/campo` (las comparte la app nativa).
export { diaLargo, duracionCorta, horaCorta, hoyEnGuatemala, ventanaCita } from "@iris/sync/campo";

const corta = new Intl.DateTimeFormat("es-GT", { timeZone: "America/Guatemala", weekday: "short", day: "numeric", month: "short" });

/**
 * Fecha corta del encabezado de la app en pantallas angostas (menos de 640 px): «Sáb 21 nov».
 * La larga («Sábado 21 de noviembre») es `fechaEncabezado` de `@iris/sync/campo`.
 */
export function fechaEncabezadoCorta(ms = Date.now()): string {
    const partes = corta.formatToParts(ms);
    const de = (t: Intl.DateTimeFormatPartTypes) => (partes.find((p) => p.type === t)?.value ?? "").replace(/\./g, "");
    const dia = de("weekday");
    return `${dia.charAt(0).toUpperCase()}${dia.slice(1)} ${de("day")} ${de("month")}`;
}
