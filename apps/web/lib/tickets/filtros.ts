import type { operations } from "@iris/contratos";
import { leerLocalJson, escribirLocal } from "../almacen";
import { ORDEN_ESTADOS, ORDEN_PRIORIDADES, type EstadoTicket, type Prioridad } from "./presentacion";

export type ConsultaTickets = NonNullable<operations["listarTickets"]["parameters"]["query"]>;

/** Filtros de la bandeja (lo que se guarda en una vista). */
export type Filtros = {
    /** `activos` = nuevo, abierto y en espera. */
    estado: "activos" | "todos" | EstadoTicket;
    prioridad: "todas" | Prioridad;
    /** Id de categoría o `todas`. */
    categoria: "todas" | number;
    asignacion: "todos" | "mios" | "sin_asignar";
    q: string;
};

export const FILTROS_INICIALES: Filtros = { estado: "activos", prioridad: "todas", categoria: "todas", asignacion: "todos", q: "" };

export const ESTADOS_ACTIVOS: EstadoTicket[] = ["nuevo", "abierto", "en_espera"];

/** Query de `GET /tickets` para unos filtros. */
export function consultaDeFiltros(f: Filtros, usuarioId: number): ConsultaTickets {
    const consulta: ConsultaTickets = {};
    if (f.estado === "activos") {
        consulta.estado = ESTADOS_ACTIVOS;
    } else if (f.estado !== "todos") {
        consulta.estado = [f.estado];
    }
    if (f.prioridad !== "todas") {
        consulta.prioridad = [f.prioridad];
    }
    if (f.categoria !== "todas") {
        consulta.categoriaId = f.categoria;
    }
    if (f.asignacion === "mios") {
        consulta.asignadoA = usuarioId;
    } else if (f.asignacion === "sin_asignar") {
        consulta.sinAsignar = true;
    }
    const q = f.q.trim();
    if (q) {
        consulta.q = q.slice(0, 120);
    }
    return consulta;
}

export type Vista = { id: string; nombre: string; filtros: Filtros; propia: boolean };

export const VISTAS_PREDEFINIDAS: readonly Vista[] = [
    { id: "activos", nombre: "Activos", filtros: FILTROS_INICIALES, propia: false },
    { id: "mios", nombre: "Asignados a mí", filtros: { ...FILTROS_INICIALES, asignacion: "mios" }, propia: false },
    { id: "sin_asignar", nombre: "Sin asignar", filtros: { ...FILTROS_INICIALES, asignacion: "sin_asignar" }, propia: false },
    { id: "todos", nombre: "Todos", filtros: { ...FILTROS_INICIALES, estado: "todos" }, propia: false },
];

export function mismosFiltros(a: Filtros, b: Filtros): boolean {
    return a.estado === b.estado && a.prioridad === b.prioridad && a.categoria === b.categoria && a.asignacion === b.asignacion && a.q.trim() === b.q.trim();
}

function esFiltros(v: unknown): v is Filtros {
    if (typeof v !== "object" || v === null) return false;
    const f = v as Record<string, unknown>;
    const estadoOk = f.estado === "activos" || f.estado === "todos" || ORDEN_ESTADOS.includes(f.estado as EstadoTicket);
    const prioridadOk = f.prioridad === "todas" || ORDEN_PRIORIDADES.includes(f.prioridad as Prioridad);
    const categoriaOk = f.categoria === "todas" || (typeof f.categoria === "number" && Number.isSafeInteger(f.categoria));
    const asignacionOk = f.asignacion === "todos" || f.asignacion === "mios" || f.asignacion === "sin_asignar";
    return estadoOk && prioridadOk && categoriaOk && asignacionOk && typeof f.q === "string";
}

function esVistas(v: unknown): v is Vista[] {
    return (
        Array.isArray(v) &&
        v.every((x: unknown) => {
            if (typeof x !== "object" || x === null) return false;
            const o = x as Record<string, unknown>;
            return typeof o.id === "string" && typeof o.nombre === "string" && esFiltros(o.filtros);
        })
    );
}

/** Las vistas propias se guardan por usuario en este navegador. */
const clave = (usuarioId: number) => `iris-vistas-tickets-${usuarioId}`;

export function leerVistas(usuarioId: number): Vista[] {
    return leerLocalJson(clave(usuarioId), esVistas, []).map((v) => ({ ...v, propia: true }));
}

export function guardarVistas(usuarioId: number, vistas: readonly Vista[]): void {
    escribirLocal(clave(usuarioId), JSON.stringify(vistas.map(({ id, nombre, filtros }) => ({ id, nombre, filtros }))));
}
