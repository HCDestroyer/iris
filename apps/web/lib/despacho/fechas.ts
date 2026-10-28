/*
 * Días y horas del despacho en America/Guatemala (UTC−6 todo el año, sin horario de verano). La
 * API trabaja en UTC: un día de Guatemala va de las 06:00 UTC a las 06:00 UTC del día siguiente.
 */

const MS_MIN = 60_000;
const MS_DIA = 86_400_000;
const DESFASE = 6 * 3_600_000;

/** Día de Guatemala como `AAAA-MM-DD`. */
export type Dia = string;

const PATRON_DIA = /^(\d{4})-(\d{2})-(\d{2})$/;

export function esDia(valor: string): valor is Dia {
    const m = PATRON_DIA.exec(valor);
    if (!m) return false;
    const d = new Date(Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3])));
    return d.toISOString().slice(0, 10) === valor;
}

/** Medianoche de Guatemala del día, en ms UTC. */
export function msInicioDia(dia: Dia): number {
    return Date.parse(`${dia}T00:00:00Z`) + DESFASE;
}

/** Día de Guatemala de un instante. */
export function diaDe(ms: number): Dia {
    return new Date(ms - DESFASE).toISOString().slice(0, 10);
}

export function diaDesdeIso(iso: string): Dia | undefined {
    const ms = Date.parse(iso);
    return Number.isNaN(ms) ? undefined : diaDe(ms);
}

export function hoyGuatemala(ahora: number = Date.now()): Dia {
    return diaDe(ahora);
}

export function sumarDias(dia: Dia, n: number): Dia {
    return new Date(Date.parse(`${dia}T00:00:00Z`) + n * MS_DIA).toISOString().slice(0, 10);
}

/** Lunes de la semana del día. */
export function inicioSemana(dia: Dia): Dia {
    const semana = new Date(`${dia}T00:00:00Z`).getUTCDay(); // 0 = domingo
    return sumarDias(dia, -((semana + 6) % 7));
}

export function diasDesde(desde: Dia, cuantos: number): Dia[] {
    return Array.from({ length: cuantos }, (_, i) => sumarDias(desde, i));
}

/** Rango UTC `[desde, hasta)` de `dias` días de Guatemala a partir de `desde` (para `GET /agenda`). */
export function rangoUtc(desde: Dia, dias: number): { desde: string; hasta: string } {
    const inicio = msInicioDia(desde);
    return { desde: isoSinMs(inicio), hasta: isoSinMs(inicio + dias * MS_DIA) };
}

/** Minutos desde la medianoche de Guatemala del `dia` (negativo o > 1440 si cae en otro día). */
export function minutosEnDia(iso: string, dia: Dia): number {
    return (Date.parse(iso) - msInicioDia(dia)) / MS_MIN;
}

/** Instante UTC del `dia` a los `minutos` de Guatemala. */
export function isoDesdeDia(dia: Dia, minutos: number): string {
    return isoSinMs(msInicioDia(dia) + minutos * MS_MIN);
}

export function isoSinMs(ms: number): string {
    return new Date(ms).toISOString().replace(".000Z", "Z");
}

const dos = (n: number) => String(n).padStart(2, "0");

/** `HH:MM` de unos minutos del día. */
export function textoMinutos(minutos: number): string {
    const m = Math.round(minutos);
    return `${dos(Math.floor(m / 60))}:${dos(m % 60)}`;
}

/** `HH:MM` en Guatemala de un instante. */
export function horaDe(iso: string | null | undefined): string {
    if (!iso) return "";
    const ms = Date.parse(iso);
    if (Number.isNaN(ms)) return "";
    return new Date(ms - DESFASE).toISOString().slice(11, 16);
}

/** `HH:MM` → minutos; `undefined` si no es una hora válida. */
export function minutosDeTexto(hora: string): number | undefined {
    const m = /^([01]\d|2[0-3]):([0-5]\d)$/.exec(hora);
    return m ? Number(m[1]) * 60 + Number(m[2]) : undefined;
}

const DIAS_SEMANA = ["dom", "lun", "mar", "mié", "jue", "vie", "sáb"];
const DIAS_SEMANA_LARGO = ["domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"];
const MESES = ["enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"];

/** «mar 28/10». */
export function etiquetaDia(dia: Dia): string {
    const d = new Date(`${dia}T00:00:00Z`);
    return `${DIAS_SEMANA[d.getUTCDay()]} ${dos(d.getUTCDate())}/${dos(d.getUTCMonth() + 1)}`;
}

/** «martes 28 de octubre». */
export function etiquetaDiaLarga(dia: Dia): string {
    const d = new Date(`${dia}T00:00:00Z`);
    return `${DIAS_SEMANA_LARGO[d.getUTCDay()]} ${d.getUTCDate()} de ${MESES[d.getUTCMonth()]}`;
}

/** «Hace 45 s», «hace 3 min», «hace 2 h 5 min», «hace 3 días». */
export function textoAntiguedad(segundos: number): string {
    const s = Math.max(0, Math.round(segundos));
    if (s < 60) return `hace ${s} s`;
    const min = Math.floor(s / 60);
    if (min < 60) return `hace ${min} min`;
    const h = Math.floor(min / 60);
    if (h < 24) return min % 60 ? `hace ${h} h ${min % 60} min` : `hace ${h} h`;
    const d = Math.floor(h / 24);
    return d === 1 ? "hace 1 día" : `hace ${d} días`;
}
