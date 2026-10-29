import { ORDEN_PRIORIDADES } from "../tickets/presentacion";
import type { Prioridad, Reporte } from "./filtros";

export const NOMBRES_REPORTE: Record<Reporte, { titulo: string; descripcion: string }> = {
    sla: { titulo: "SLA cumplido", descripcion: "Tickets resueltos en el período que respondieron y se resolvieron dentro de su SLA." },
    resolucion: { titulo: "Tiempo de resolución", descripcion: "Desde que se abrió el ticket hasta que se resolvió (reloj, no horario hábil)." },
    tecnicos: { titulo: "Órdenes por técnico", descripcion: "Órdenes finalizadas (cerradas y fallidas), su resultado y el tiempo en sitio." },
    "primera-visita": { titulo: "Primera visita resuelta", descripcion: "Instalaciones y visitas cerradas con éxito sin otra orden ni ticket del mismo servicio después." },
    reincidencias: { titulo: "Reincidencias", descripcion: "Tickets resueltos tras los que el mismo servicio (o cliente) volvió a abrir otro." },
};

/** Porcentaje de la API (0–100, dos decimales); `null` = sin base → «Sin datos», nunca 0 %. */
export function textoPorcentaje(v: number | null | undefined): string {
    if (v === null || v === undefined || !Number.isFinite(v)) return "Sin datos";
    return `${v.toLocaleString("es-GT", { maximumFractionDigits: 1 })} %`;
}

/** Minutos legibles: «45 min», «2 h 5 min», «3 d 4 h». `null` → «Sin datos». */
export function textoMinutos(v: number | null | undefined): string {
    if (v === null || v === undefined || !Number.isFinite(v)) return "Sin datos";
    const min = Math.round(v);
    if (min < 60) return `${min} min`;
    const h = Math.floor(min / 60);
    const m = min % 60;
    if (h < 24) return m ? `${h} h ${m} min` : `${h} h`;
    const d = Math.floor(h / 24);
    const hr = h % 24;
    return hr ? `${d} d ${hr} h` : `${d} d`;
}

/** Horas legibles para «volvió a las…»: «5 h», «2 d 3 h». */
export function textoHoras(h: number): string {
    return textoMinutos(h * 60);
}

export function textoEntero(n: number): string {
    return n.toLocaleString("es-GT");
}

export const TIPOS_PRIMERA_VISITA: Record<string, string> = {
    instalacion: "Instalación",
    visita: "Visita técnica",
};

const MESES_CORTOS = ["ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"];

/** «1 oct – 31 oct 2026» o «15 dic 2025 – 14 ene 2026» (días de Guatemala, `AAAA-MM-DD`). */
export function textoPeriodo(desde: string, hasta: string): string {
    const parte = (d: string, conAnio: boolean) => {
        const [a, m, dia] = d.split("-");
        const mes = MESES_CORTOS[Number(m) - 1] ?? m;
        return `${Number(dia)} ${mes}${conAnio ? ` ${a}` : ""}`;
    };
    const mismoAnio = desde.slice(0, 4) === hasta.slice(0, 4);
    return `${parte(desde, !mismoAnio)} – ${parte(hasta, true)}`;
}

/** Fecha y hora en Guatemala: «28/10/2026 14:05». */
export function textoFechaHora(iso: string): string {
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return iso;
    return new Intl.DateTimeFormat("es-GT", {
        timeZone: "America/Guatemala",
        day: "2-digit",
        month: "2-digit",
        year: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        hour12: false,
    }).format(d);
}

/** Series del gráfico de SLA, en el orden fijo de la paleta categórica. */
export const SERIES_SLA = [
    { nombre: "Respuesta", clase: "bg-grafico-1" },
    { nombre: "Resolución", clase: "bg-grafico-2" },
] as const;

/** Filas por prioridad de la más urgente a la menos; las sin prioridad, al final. */
export function ordenarPorPrioridad<F extends { prioridad?: Prioridad }>(filas: readonly F[]): F[] {
    const rango = (p: Prioridad | undefined) => (p ? ORDEN_PRIORIDADES.length - ORDEN_PRIORIDADES.indexOf(p) : 99);
    return [...filas].sort((a, b) => rango(a.prioridad) - rango(b.prioridad));
}

/** Nombre de la categoría de una fila; sin categoría, «Sin categoría». */
export function etiquetaCategoria(f: { categoria?: string | null; categoriaId?: number | null }): string {
    if (f.categoria) return f.categoria;
    return f.categoriaId ? `Categoría ${f.categoriaId}` : "Sin categoría";
}
