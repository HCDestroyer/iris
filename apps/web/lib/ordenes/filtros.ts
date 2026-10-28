import type { operations } from "@iris/contratos";
import { ESTADOS_ACTIVOS_ORDEN, type EstadoOrden, type TipoOrden } from "./presentacion";

export type ConsultaOrdenes = NonNullable<operations["listarOrdenes"]["parameters"]["query"]>;

/** Filtros del tablero de órdenes. */
export type FiltrosOrdenes = {
    /** `activas` = pendiente, programada, en camino y en sitio. */
    estado: "todos" | "activas" | EstadoOrden;
    tipo: "todos" | TipoOrden;
    /** Id del técnico, `sin` (sin técnico) o `todos`. */
    tecnico: "todos" | "sin" | number;
    /** Día programado `AAAA-MM-DD` en America/Guatemala, o vacío. */
    fecha: string;
    /** Id de zona de Argos (texto del campo), o vacío. */
    zona: string;
    q: string;
};

export const FILTROS_ORDENES_INICIALES: FiltrosOrdenes = { estado: "todos", tipo: "todos", tecnico: "todos", fecha: "", zona: "", q: "" };

const PATRON_FECHA = /^(\d{4})-(\d{2})-(\d{2})$/;

/**
 * Rango UTC de un día en America/Guatemala (UTC−6 todo el año, sin horario de verano):
 * `2026-10-28` → desde `2026-10-28T06:00:00Z` hasta `2026-10-29T06:00:00Z` (exclusivo).
 */
export function rangoDiaGuatemala(fecha: string): { desde: string; hasta: string } | undefined {
    const m = PATRON_FECHA.exec(fecha);
    if (!m) return undefined;
    const inicio = Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3]), 6);
    const d = new Date(inicio);
    if (d.getUTCDate() !== Number(m[3]) || d.getUTCMonth() !== Number(m[2]) - 1) return undefined;
    return { desde: d.toISOString().replace(".000", ""), hasta: new Date(inicio + 86_400_000).toISOString().replace(".000", "") };
}

/** Query de `GET /ordenes` para unos filtros (arreglos separados por coma, como los lee la API). */
export function consultaDeFiltrosOrdenes(f: FiltrosOrdenes): ConsultaOrdenes {
    const consulta: ConsultaOrdenes = {};
    if (f.estado === "activas") {
        consulta.estado = [...ESTADOS_ACTIVOS_ORDEN];
    } else if (f.estado !== "todos") {
        consulta.estado = [f.estado];
    }
    if (f.tipo !== "todos") {
        consulta.tipo = [f.tipo];
    }
    if (f.tecnico === "sin") {
        consulta.sinTecnico = true;
    } else if (typeof f.tecnico === "number") {
        consulta.tecnicoId = f.tecnico;
    }
    const rango = rangoDiaGuatemala(f.fecha);
    if (rango) {
        consulta.desde = rango.desde;
        consulta.hasta = rango.hasta;
    }
    const zona = f.zona.trim();
    if (/^\d{1,12}$/.test(zona) && Number(zona) > 0) {
        consulta.zonaId = Number(zona);
    }
    const q = f.q.trim();
    if (q) {
        consulta.q = q.slice(0, 120);
    }
    return consulta;
}

export function hayFiltrosOrdenes(f: FiltrosOrdenes): boolean {
    const c = consultaDeFiltrosOrdenes(f);
    return Object.keys(c).length > 0;
}
