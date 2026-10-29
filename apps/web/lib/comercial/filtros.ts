import type { EstadoProspecto, OrigenProspecto } from "./presentacion";
import { ORDEN_ESTADOS, ORIGENES } from "./presentacion";

/*
 * Filtros del embudo y la lista (`GET /prospectos`): estado, responsable, zona, origen y texto.
 * Viven en la URL (`?estado=&responsable=&zona=&origen=&q=&vista=`) para compartir un enlace.
 */

export type Responsable = "" | "mios" | "sin" | `u${number}`;
export type Vista = "embudo" | "lista" | "mapa";

export type Filtros = {
    estado: EstadoProspecto | "";
    responsable: Responsable;
    zonaId: number | null;
    origen: OrigenProspecto | "";
    q: string;
};

export const FILTROS_VACIOS: Filtros = { estado: "", responsable: "", zonaId: null, origen: "", q: "" };

export type QueryProspectos = {
    estado?: string;
    origen?: OrigenProspecto;
    vendedorId?: number;
    sinVendedor?: boolean;
    zonaId?: number;
    conCoordenadas?: boolean;
    q?: string;
};

/** Query de `GET /prospectos` (sin cursor ni límite). `estado` fuerza la columna del embudo. */
export function queryDe(f: Filtros, yoId: number, opciones: { estado?: EstadoProspecto; conCoordenadas?: boolean } = {}): QueryProspectos {
    const estado = opciones.estado ?? f.estado;
    const q: QueryProspectos = {};
    if (estado) q.estado = estado;
    if (f.origen) q.origen = f.origen;
    if (f.zonaId !== null) q.zonaId = f.zonaId;
    if (f.responsable === "mios") q.vendedorId = yoId;
    else if (f.responsable === "sin") q.sinVendedor = true;
    else if (f.responsable.startsWith("u")) {
        const id = Number(f.responsable.slice(1));
        if (Number.isSafeInteger(id) && id > 0) q.vendedorId = id;
    }
    const texto = f.q.trim();
    if (texto) q.q = texto.slice(0, 120);
    if (opciones.conCoordenadas) q.conCoordenadas = true;
    return q;
}

const esEstado = (v: string): v is EstadoProspecto => (ORDEN_ESTADOS as readonly string[]).includes(v);
const esOrigen = (v: string): v is OrigenProspecto => v in ORIGENES;
const esResponsable = (v: string): v is Responsable => v === "" || v === "mios" || v === "sin" || /^u[1-9]\d{0,15}$/.test(v);

export function filtrosDesdeUrl(p: URLSearchParams): Filtros {
    const estado = p.get("estado") ?? "";
    const origen = p.get("origen") ?? "";
    const responsable = p.get("responsable") ?? "";
    const zona = Number(p.get("zona"));
    return {
        estado: esEstado(estado) ? estado : "",
        origen: esOrigen(origen) ? origen : "",
        responsable: esResponsable(responsable) ? responsable : "",
        zonaId: Number.isSafeInteger(zona) && zona > 0 ? zona : null,
        q: (p.get("q") ?? "").slice(0, 120),
    };
}

export function vistaDesdeUrl(p: URLSearchParams): Vista {
    const v = p.get("vista");
    return v === "lista" || v === "mapa" ? v : "embudo";
}

export function urlDe(f: Filtros, vista: Vista): string {
    const p = new URLSearchParams();
    if (vista !== "embudo") p.set("vista", vista);
    if (f.estado) p.set("estado", f.estado);
    if (f.responsable) p.set("responsable", f.responsable);
    if (f.zonaId !== null) p.set("zona", String(f.zonaId));
    if (f.origen) p.set("origen", f.origen);
    if (f.q.trim()) p.set("q", f.q.trim());
    const s = p.toString();
    return s ? `/comercial?${s}` : "/comercial";
}

export function hayFiltros(f: Filtros): boolean {
    return f.estado !== "" || f.responsable !== "" || f.zonaId !== null || f.origen !== "" || f.q.trim() !== "";
}

/** «20» o «20+» (hay más páginas): nunca un total inventado sobre lo que no se leyó. */
export function textoConteo(leidos: number, hayMas: boolean): string {
    return hayMas ? `${leidos}+` : String(leidos);
}
