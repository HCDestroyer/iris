import { textoAusencia, type AusenciaAgenda, type CitaAgenda } from "@/lib/despacho/calendario";
import { diaDesdeIso, diasDesde, etiquetaDia, horaDe, inicioSemana, minutosEnDia, sumarDias, type Dia } from "@/lib/despacho/fechas";
import { ORDEN_ESTADOS_ORDEN, type EstadoOrden } from "@/lib/ordenes/presentacion";

/*
 * Vista de mes de la agenda: la cuadrícula (semanas de lunes a domingo), los trozos de hasta 31
 * días en que se pide `GET /agenda`, el resumen de cada día y los conflictos (una cita que cae en
 * una ausencia del técnico o que se cruza con otra cita viva suya).
 */

/** Días que `GET /agenda` acepta por petición. */
export const MAX_DIAS_AGENDA = 31;

/** Vistas del calendario. */
export type VistaAgenda = "mes" | "semana" | "dia";

/** Primer día del mes del `dia`. */
export function inicioMes(dia: Dia): Dia {
    return `${dia.slice(0, 7)}-01`;
}

/** Mismo día (o el último que exista) `n` meses después. */
export function sumarMeses(dia: Dia, n: number): Dia {
    const [a, m, d] = dia.split("-").map(Number) as [number, number, number];
    const total = a * 12 + (m - 1) + n;
    const anio = Math.floor(total / 12);
    const mes = (total % 12) + 1;
    const ultimo = new Date(Date.UTC(anio, mes, 0)).getUTCDate();
    return `${String(anio).padStart(4, "0")}-${String(mes).padStart(2, "0")}-${String(Math.min(d, ultimo)).padStart(2, "0")}`;
}

/** ¿Es del mismo mes que `dia`? */
export function mismoMes(a: Dia, b: Dia): boolean {
    return a.slice(0, 7) === b.slice(0, 7);
}

/** Cuadrícula del mes: semanas completas de lunes a domingo que cubren el mes (5 o 6). */
export function cuadriculaMes(dia: Dia): { desde: Dia; dias: Dia[]; semanas: Dia[][] } {
    const primero = inicioMes(dia);
    const desde = inicioSemana(primero);
    const ultimo = sumarDias(sumarMeses(primero, 1), -1);
    const hasta = sumarDias(inicioSemana(ultimo), 6);
    const total = Math.round((Date.parse(`${hasta}T00:00:00Z`) - Date.parse(`${desde}T00:00:00Z`)) / 86_400_000) + 1;
    const dias = diasDesde(desde, total);
    const semanas: Dia[][] = [];
    for (let i = 0; i < dias.length; i += 7) semanas.push(dias.slice(i, i + 7));
    return { desde, dias, semanas };
}

/** Parte `dias` días desde `desde` en trozos de hasta `max` días (para `GET /agenda`). */
export function trozos(desde: Dia, dias: number, max = MAX_DIAS_AGENDA): { desde: Dia; dias: number }[] {
    const salida: { desde: Dia; dias: number }[] = [];
    for (let i = 0; i < dias; i += max) salida.push({ desde: sumarDias(desde, i), dias: Math.min(max, dias - i) });
    return salida;
}

/** Estados en que una cita ocupa al técnico (no cuentan las terminadas ni las canceladas). */
const VIVOS: readonly EstadoOrden[] = ["programada", "en_camino", "en_sitio", "pausada"];

export type Conflicto =
    | { tipo: "ausencia"; ordenId: number; ausencia: AusenciaAgenda }
    | { tipo: "solape"; ordenId: number; con: CitaAgenda };

const cruzan = (a: { inicio: string; fin: string }, b: { inicio: string; fin: string }) =>
    Date.parse(a.inicio) < Date.parse(b.fin) && Date.parse(b.inicio) < Date.parse(a.fin);

/**
 * Conflictos de las citas vivas: caer dentro de una ausencia del técnico (la API no mueve las
 * citas al registrar una ausencia) o cruzarse con otra cita viva del mismo técnico.
 */
export function conflictosDe(citas: readonly CitaAgenda[], ausencias: readonly AusenciaAgenda[]): Map<number, Conflicto[]> {
    const salida = new Map<number, Conflicto[]>();
    const agregar = (c: Conflicto) => salida.set(c.ordenId, [...(salida.get(c.ordenId) ?? []), c]);
    const vivas = citas.filter((c) => VIVOS.includes(c.estado));
    for (const c of vivas) {
        for (const a of ausencias) {
            if (a.tecnicoId === c.tecnicoId && cruzan(c, a)) agregar({ tipo: "ausencia", ordenId: c.ordenId, ausencia: a });
        }
        for (const otra of vivas) {
            if (otra.ordenId !== c.ordenId && otra.tecnicoId === c.tecnicoId && cruzan(c, otra)) agregar({ tipo: "solape", ordenId: c.ordenId, con: otra });
        }
    }
    return salida;
}

/** «cae en la ausencia del técnico (Vacaciones, mar 28/10 00:00 → mié 29/10 00:00)» / «se cruza con OT-000004 (10:00–11:00)». */
export function textoConflicto(c: Conflicto): string {
    if (c.tipo === "solape") return `se cruza con ${c.con.numero} (${horaDe(c.con.inicio)}–${horaDe(c.con.fin)})`;
    const a = c.ausencia;
    return `cae en una ausencia del técnico (${textoAusencia(a.motivo)}, ${etiquetaDia(diaDesdeIso(a.inicio) ?? "")} ${horaDe(a.inicio)} → ${etiquetaDia(diaDesdeIso(a.fin) ?? "")} ${horaDe(a.fin)})`;
}

export type ResumenDia = {
    total: number;
    /** Cuántas citas hay en cada estado, en el orden del tablero (solo los que tienen alguna). */
    porEstado: { estado: EstadoOrden; cuantas: number }[];
    /** Técnicos con alguna ausencia ese día. */
    ausentes: number;
    /** Citas del día con algún conflicto. */
    conflictos: number;
};

/** Resumen de cada día: citas por estado, técnicos ausentes y citas con conflicto. */
export function resumenPorDia(dias: readonly Dia[], citas: readonly CitaAgenda[], ausencias: readonly AusenciaAgenda[], conflictos: Map<number, Conflicto[]>): Map<Dia, ResumenDia> {
    const salida = new Map<Dia, ResumenDia>();
    for (const d of dias) {
        const delDia = citas.filter((c) => diaDesdeIso(c.inicio) === d);
        const cuenta = new Map<EstadoOrden, number>();
        for (const c of delDia) cuenta.set(c.estado, (cuenta.get(c.estado) ?? 0) + 1);
        const ausentes = new Set(ausencias.filter((a) => minutosEnDia(a.inicio, d) < 1440 && minutosEnDia(a.fin, d) > 0).map((a) => a.tecnicoId));
        salida.set(d, {
            total: delDia.length,
            porEstado: ORDEN_ESTADOS_ORDEN.filter((e) => cuenta.has(e)).map((e) => ({ estado: e, cuantas: cuenta.get(e) ?? 0 })),
            ausentes: ausentes.size,
            conflictos: delDia.filter((c) => conflictos.has(c.ordenId)).length,
        });
    }
    return salida;
}

/** «1 orden», «3 órdenes». */
export function textoOrdenes(n: number): string {
    return n === 1 ? "1 orden" : `${n} órdenes`;
}

/** Une los calendarios de varios trozos (sin repetir citas ni ausencias). */
export function unirCalendarios(partes: readonly { citas: CitaAgenda[] | null; ausencias: AusenciaAgenda[] | null }[]): { citas: CitaAgenda[]; ausencias: AusenciaAgenda[] } {
    const citas = new Map<number, CitaAgenda>();
    const ausencias = new Map<number, AusenciaAgenda>();
    for (const p of partes) {
        for (const c of p.citas ?? []) citas.set(c.ordenId, c);
        for (const a of p.ausencias ?? []) ausencias.set(a.id, a);
    }
    return { citas: [...citas.values()], ausencias: [...ausencias.values()] };
}

const MESES = ["enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"];

/** «octubre de 2026». */
export function etiquetaMes(dia: Dia): string {
    return `${MESES[Number(dia.slice(5, 7)) - 1]} de ${dia.slice(0, 4)}`;
}

/** Lee `?vista=` y `?dia=` de la URL; lo inválido cae en el mes de hoy. */
export function vistaDeUrl(vista: string | null, dia: string | null, hoy: Dia, esDia: (v: string) => boolean): { vista: VistaAgenda; dia: Dia } {
    const v: VistaAgenda = vista === "semana" || vista === "dia" || vista === "mes" ? vista : "mes";
    return { vista: v, dia: dia !== null && esDia(dia) ? dia : hoy };
}
