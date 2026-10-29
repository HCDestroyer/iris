import type { Schema } from "@iris/contratos";
import { diaDe, esDia, hoyGuatemala, msInicioDia, sumarDias, type Dia } from "../despacho/fechas";
import { ORDEN_PRIORIDADES } from "../tickets/presentacion";

/*
 * Filtros de los reportes (Fase 5): período en días de Guatemala, zona, cuadrilla, técnico,
 * categoría y prioridades, más `dias` (ventana de primera visita y reincidencias). Viven en la
 * URL para compartir el enlace; `queryDe` arma la query de `GET /reportes/<reporte>`.
 */

export type Prioridad = Schema<"Ticket">["prioridad"];
export type Reporte = "sla" | "resolucion" | "tecnicos" | "primera-visita" | "reincidencias" | "satisfaccion";
export const REPORTES: readonly Reporte[] = ["sla", "resolucion", "tecnicos", "primera-visita", "reincidencias", "satisfaccion"];

/** Reportes de tickets: aceptan categoría y prioridad. */
export const DE_TICKETS: readonly Reporte[] = ["sla", "resolucion", "reincidencias"];
/** Reportes que aceptan categoría (los de tickets y satisfacción). */
export const CON_CATEGORIA: readonly Reporte[] = [...DE_TICKETS, "satisfaccion"];

export type Entidad = "ticket" | "orden";
/** Reportes con ventana `dias`, con sus límites y el valor por omisión de la API. */
export const DIAS: Partial<Record<Reporte, { min: number; max: number; porDefecto: number }>> = {
    "primera-visita": { min: 7, max: 90, porDefecto: 30 },
    reincidencias: { min: 1, max: 90, porDefecto: 30 },
};

/** Como mucho 366 días (422 `rango_excedido`). */
export const MAX_DIAS_RANGO = 366;

export type Filtros = {
    desde: Dia;
    hasta: Dia;
    zonaId: number | null;
    cuadrillaId: number | null;
    tecnicoId: number | null;
    categoriaId: number | null;
    prioridades: Prioridad[];
    dias: number | null;
    /** Solo en satisfacción: encuestas de tickets o de órdenes. */
    entidad: Entidad | null;
};

export type Atajo = "este_mes" | "mes_anterior" | "ultimos_90";

export const ATAJOS: Record<Atajo, string> = {
    este_mes: "Este mes",
    mes_anterior: "Mes anterior",
    ultimos_90: "Últimos 90 días",
};

/** Período de un atajo, con `hoy` en días de Guatemala. */
export function periodoDe(atajo: Atajo, hoy: Dia = hoyGuatemala()): { desde: Dia; hasta: Dia } {
    const inicioMes = `${hoy.slice(0, 8)}01`;
    switch (atajo) {
        case "este_mes":
            return { desde: inicioMes, hasta: hoy };
        case "mes_anterior": {
            const finAnterior = sumarDias(inicioMes, -1);
            return { desde: `${finAnterior.slice(0, 8)}01`, hasta: finAnterior };
        }
        case "ultimos_90":
            return { desde: sumarDias(hoy, -89), hasta: hoy };
    }
}

/** El atajo que corresponde al período, si alguno. */
export function atajoDe(f: Pick<Filtros, "desde" | "hasta">, hoy: Dia = hoyGuatemala()): Atajo | undefined {
    return (Object.keys(ATAJOS) as Atajo[]).find((a) => {
        const p = periodoDe(a, hoy);
        return p.desde === f.desde && p.hasta === f.hasta;
    });
}

/** Días del período (incluidos los dos extremos). */
export function diasDelRango(desde: Dia, hasta: Dia): number {
    return Math.round((msInicioDia(hasta) - msInicioDia(desde)) / 86_400_000) + 1;
}

/** Por qué el período no sirve (antes de llamar a la API), o `undefined`. */
export function errorDeRango(desde: string, hasta: string): string | undefined {
    if (!esDia(desde) || !esDia(hasta)) return "Elige las dos fechas";
    if (desde > hasta) return "«Desde» va antes de «hasta»";
    if (diasDelRango(desde, hasta) > MAX_DIAS_RANGO) return `El período admite hasta ${MAX_DIAS_RANGO} días`;
    return undefined;
}

const id = (v: string | null) => {
    const n = Number(v);
    return v !== null && Number.isSafeInteger(n) && n > 0 ? n : null;
};

export function esReporte(v: string | null): v is Reporte {
    return v !== null && (REPORTES as readonly string[]).includes(v);
}

/** Filtros desde la URL; lo que falta o no es válido toma el valor por omisión («este mes»). */
export function filtrosDesdeUrl(p: URLSearchParams, hoy: Dia = hoyGuatemala()): Filtros {
    const porDefecto = periodoDe("este_mes", hoy);
    const desde = p.get("desde") ?? "";
    const hasta = p.get("hasta") ?? "";
    const valido = esDia(desde) && esDia(hasta);
    const prioridades = (p.get("prioridad") ?? "")
        .split(",")
        .filter((x): x is Prioridad => (ORDEN_PRIORIDADES as readonly string[]).includes(x));
    const dias = id(p.get("dias"));
    return {
        desde: valido ? desde : porDefecto.desde,
        hasta: valido ? hasta : porDefecto.hasta,
        zonaId: id(p.get("zona")),
        cuadrillaId: id(p.get("cuadrilla")),
        tecnicoId: id(p.get("tecnico")),
        categoriaId: id(p.get("categoria")),
        prioridades: ORDEN_PRIORIDADES.filter((x) => prioridades.includes(x)),
        dias: dias !== null && dias <= 90 ? dias : null,
        entidad: p.get("entidad") === "ticket" || p.get("entidad") === "orden" ? (p.get("entidad") as Entidad) : null,
    };
}

export function urlDe(reporte: Reporte, f: Filtros): string {
    const p = new URLSearchParams();
    p.set("reporte", reporte);
    p.set("desde", f.desde);
    p.set("hasta", f.hasta);
    if (f.zonaId !== null) p.set("zona", String(f.zonaId));
    if (f.cuadrillaId !== null) p.set("cuadrilla", String(f.cuadrillaId));
    if (f.tecnicoId !== null) p.set("tecnico", String(f.tecnicoId));
    if (f.categoriaId !== null) p.set("categoria", String(f.categoriaId));
    if (f.prioridades.length) p.set("prioridad", f.prioridades.join(","));
    if (f.dias !== null) p.set("dias", String(f.dias));
    if (f.entidad !== null) p.set("entidad", f.entidad);
    return `/reportes?${p}`;
}

/** Ventana `dias` válida para el reporte (o la de la API por omisión). */
export function diasPara(reporte: Reporte, f: Pick<Filtros, "dias">): number | undefined {
    const d = DIAS[reporte];
    if (!d) return undefined;
    const v = f.dias ?? d.porDefecto;
    return Math.min(d.max, Math.max(d.min, v));
}

export type QueryReporte = {
    desde: string;
    hasta: string;
    zonaId?: number;
    cuadrillaId?: number;
    tecnicoId?: number;
    categoriaId?: number;
    prioridad?: Prioridad[];
    dias?: number;
    entidad?: Entidad;
};

/** Query de `GET /reportes/<reporte>`: solo los filtros que el reporte acepta. */
export function queryDe(reporte: Reporte, f: Filtros): QueryReporte {
    const q: QueryReporte = { desde: f.desde, hasta: f.hasta };
    if (f.zonaId !== null) q.zonaId = f.zonaId;
    if (f.cuadrillaId !== null) q.cuadrillaId = f.cuadrillaId;
    if (f.tecnicoId !== null) q.tecnicoId = f.tecnicoId;
    if (CON_CATEGORIA.includes(reporte) && f.categoriaId !== null) q.categoriaId = f.categoriaId;
    if (DE_TICKETS.includes(reporte) && f.prioridades.length) q.prioridad = [...f.prioridades];
    if (reporte === "satisfaccion" && f.entidad !== null) q.entidad = f.entidad;
    const dias = diasPara(reporte, f);
    if (dias !== undefined) q.dias = dias;
    return q;
}

/** Nombre del CSV: `reporte-sla_2026-10-01_2026-10-31.csv`. */
export function nombreCsv(reporte: Reporte, f: Pick<Filtros, "desde" | "hasta">): string {
    return `reporte-${reporte}_${f.desde}_${f.hasta}.csv`;
}

export { diaDe };
