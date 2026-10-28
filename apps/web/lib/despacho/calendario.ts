import type { Schema } from "@iris/contratos";
import { diaDesdeIso, minutosEnDia, type Dia } from "./fechas";

export type Calendario = Schema<"Calendario">;
export type CitaAgenda = Schema<"CitaAgenda">;
export type AusenciaAgenda = Schema<"AusenciaAgenda">;
export type Tecnico = Schema<"Tecnico">;
export type Zona = Schema<"Zona">;
export type Cuadrilla = Schema<"Cuadrilla">;

/** Fila del calendario: un técnico por su id de usuario (el `tecnicoId` de citas y órdenes). */
export type FilaTecnico = {
    usuarioId: number;
    nombre: string;
    cuadrillaId: number | null;
    /** No vino en `GET /tecnicos` (otra cuadrilla, inactivo o sin ficha): se muestra por su id. */
    sinFicha: boolean;
};

/**
 * Filas del calendario: los técnicos activos leídos (por nombre) y, al final, los que tienen
 * citas o ausencias en el rango pero no vinieron en la lista (para no esconder trabajo programado).
 */
export function filasDeTecnicos(tecnicos: readonly Tecnico[], citas: readonly CitaAgenda[], ausencias: readonly AusenciaAgenda[]): FilaTecnico[] {
    const filas = new Map<number, FilaTecnico>();
    for (const t of [...tecnicos].filter((x) => x.activo).sort((a, b) => a.nombre.localeCompare(b.nombre, "es"))) {
        filas.set(t.usuarioId, { usuarioId: t.usuarioId, nombre: t.nombre, cuadrillaId: t.cuadrillaId, sinFicha: false });
    }
    const extra = new Map<number, FilaTecnico>();
    for (const x of [...citas, ...ausencias]) {
        if (!filas.has(x.tecnicoId) && !extra.has(x.tecnicoId)) {
            const nombre = tecnicos.find((t) => t.usuarioId === x.tecnicoId)?.nombre ?? `Técnico ${x.tecnicoId}`;
            extra.set(x.tecnicoId, { usuarioId: x.tecnicoId, nombre, cuadrillaId: "cuadrillaId" in x ? x.cuadrillaId : null, sinFicha: true });
        }
    }
    return [...filas.values(), ...[...extra.values()].sort((a, b) => a.usuarioId - b.usuarioId)];
}

/** Citas del técnico que empiezan en el día (Guatemala), por hora. */
export function citasDelDia(citas: readonly CitaAgenda[], usuarioId: number, dia: Dia): CitaAgenda[] {
    return citas.filter((c) => c.tecnicoId === usuarioId && diaDesdeIso(c.inicio) === dia).sort((a, b) => a.inicio.localeCompare(b.inicio));
}

/** Ausencias del técnico que tocan el día. */
export function ausenciasDelDia(ausencias: readonly AusenciaAgenda[], usuarioId: number, dia: Dia): AusenciaAgenda[] {
    return ausencias.filter((a) => a.tecnicoId === usuarioId && minutosEnDia(a.inicio, dia) < 1440 && minutosEnDia(a.fin, dia) > 0);
}

/** Horas visibles de la vista de día, en minutos de Guatemala. */
export type Horario = { desde: number; hasta: number };

export const HORARIO_BASE: Horario = { desde: 6 * 60, hasta: 20 * 60 };

/** Horario que cubre las citas y ventanas del día (al menos 06:00–20:00), en horas enteras. */
export function horarioDelDia(citas: readonly CitaAgenda[], dia: Dia): Horario {
    let desde = HORARIO_BASE.desde;
    let hasta = HORARIO_BASE.hasta;
    for (const c of citas) {
        for (const iso of [c.inicio, c.ventanaInicio]) {
            if (iso && diaDesdeIso(iso) === dia) desde = Math.min(desde, Math.floor(minutosEnDia(iso, dia) / 60) * 60);
        }
        for (const iso of [c.fin, c.ventanaFin]) {
            if (iso && diaDesdeIso(c.inicio) === dia) hasta = Math.max(hasta, Math.min(1440, Math.ceil(minutosEnDia(iso, dia) / 60) * 60));
        }
    }
    return { desde: Math.max(0, desde), hasta };
}

/** Posición (en % del ancho) de un tramo del día dentro del horario; `null` si no se ve. */
export function tramoEnHorario(inicio: string, fin: string, dia: Dia, h: Horario): { izquierda: number; ancho: number; cortadoAntes: boolean; cortadoDespues: boolean } | null {
    const a = minutosEnDia(inicio, dia);
    const b = minutosEnDia(fin, dia);
    const desde = Math.max(a, h.desde);
    const hasta = Math.min(b, h.hasta);
    if (hasta <= desde) return null;
    const total = h.hasta - h.desde;
    return {
        izquierda: ((desde - h.desde) / total) * 100,
        ancho: ((hasta - desde) / total) * 100,
        cortadoAntes: a < h.desde,
        cortadoDespues: b > h.hasta,
    };
}

/** Minutos del día en que cae un punto (fracción 0–1 del ancho), redondeados al `paso`. */
export function minutosEnFraccion(fraccion: number, h: Horario, paso = 15): number {
    const f = Math.min(1, Math.max(0, fraccion));
    const crudo = h.desde + f * (h.hasta - h.desde);
    return Math.min(h.hasta - paso, Math.max(h.desde, Math.round(crudo / paso) * paso));
}

/** Duración de la cita en minutos. */
export function duracionMin(c: Pick<CitaAgenda, "inicio" | "fin">): number {
    return Math.max(0, Math.round((Date.parse(c.fin) - Date.parse(c.inicio)) / 60_000));
}

export const MOTIVOS_AUSENCIA: Record<string, string> = {
    vacaciones: "Vacaciones",
    enfermedad: "Enfermedad",
    capacitacion: "Capacitación",
    permiso: "Permiso",
    otro: "Ausente",
};

export function textoAusencia(motivo: string): string {
    return MOTIVOS_AUSENCIA[motivo] ?? "Ausente";
}

/** Nombre de una zona o cuadrilla del catálogo; si no está, «Zona N» / «Cuadrilla N». */
export function nombreDe(lista: readonly { id: number; nombre: string }[] | undefined, id: number | null, prefijo: string): string | undefined {
    if (id === null) return undefined;
    return lista?.find((x) => x.id === id)?.nombre ?? `${prefijo} ${id}`;
}
