// Turno del técnico según la agenda (`Paquete.turno`, guardado en el motor con el día del paquete)
// y el estado local del turno de la ubicación en vivo (`localStorage`, por usuario).
import type { TurnoDelDia } from "@iris/sync";

/** Tramo del turno en ms (UTC): `[inicio, fin)`. */
export interface Tramo {
    inicio: number;
    fin: number;
}

/**
 * Tramos de hoy según la agenda, en orden. `null` = sin turno en la agenda para hoy (no hay
 * paquete, es de otro día, ese día no trabaja o no tiene ficha): manda el turno de siempre
 * («En camino» / «Llegué» lo inician y cerrar la última orden lo termina).
 */
export function tramosDeHoy(turno: TurnoDelDia | null, hoy: string): Tramo[] | null {
    if (!turno || turno.fecha !== hoy) {
        return null;
    }
    const crudos = turno.tramos ?? [{ inicio: turno.inicio, fin: turno.fin }];
    const tramos = crudos
        .map((t) => ({ inicio: Date.parse(t.inicio), fin: Date.parse(t.fin) }))
        .filter((t) => !Number.isNaN(t.inicio) && !Number.isNaN(t.fin) && t.fin > t.inicio)
        .sort((a, b) => a.inicio - b.inicio);
    return tramos.length > 0 ? tramos : null;
}

/** Tramo en curso a esa hora (o `null`). */
export function tramoEn(tramos: readonly Tramo[], ms: number): Tramo | null {
    return tramos.find((t) => t.inicio <= ms && ms < t.fin) ?? null;
}

/** Próximo tramo que todavía no empezó (o `null`). */
export function siguienteTramo(tramos: readonly Tramo[], ms: number): Tramo | null {
    return tramos.find((t) => t.inicio > ms) ?? null;
}

/** Próximo inicio o fin de tramo después de `ms` (o `null` si ya no quedan). */
export function proximaFrontera(tramos: readonly Tramo[], ms: number): number | null {
    let proxima: number | null = null;
    for (const t of tramos) {
        for (const borde of [t.inicio, t.fin]) {
            if (borde > ms && (proxima === null || borde < proxima)) {
                proxima = borde;
            }
        }
    }
    return proxima;
}

/** Estado local del turno de la ubicación, del día en Guatemala. */
export interface TurnoLocal {
    fecha: string;
    enTurno: boolean;
    /**
     * Hubo trabajo real («En camino» / «Llegué»): si el tramo termina con órdenes abiertas, el turno
     * sigue hasta cerrar la última (termina lo que ocurra después).
     */
    trabajo: boolean;
    /** Fin (ms) del tramo de la agenda en el que está el turno: al llegar, termina solo. */
    hastaTramo: number | null;
    /** Lo terminó a mano dentro de un tramo: no se enciende solo hasta el fin de ese tramo (ms). */
    apagadoHasta: number | null;
}

const RE_FECHA = /^\d{4}-\d{2}-\d{2}$/;

const numeroONulo = (v: unknown): v is number | null => v === null || (typeof v === "number" && Number.isFinite(v));

/** Lee el turno guardado. Acepta el formato anterior (solo la fecha = en turno ese día). */
export function leerTurnoLocal(valor: string | undefined): TurnoLocal | null {
    if (valor === undefined) {
        return null;
    }
    if (RE_FECHA.test(valor)) {
        return { fecha: valor, enTurno: true, trabajo: true, hastaTramo: null, apagadoHasta: null };
    }
    try {
        const v: unknown = JSON.parse(valor);
        if (typeof v !== "object" || v === null) {
            return null;
        }
        const t = v as Record<string, unknown>;
        if (
            typeof t.fecha !== "string" ||
            !RE_FECHA.test(t.fecha) ||
            typeof t.enTurno !== "boolean" ||
            typeof t.trabajo !== "boolean" ||
            !numeroONulo(t.hastaTramo) ||
            !numeroONulo(t.apagadoHasta)
        ) {
            return null;
        }
        return { fecha: t.fecha, enTurno: t.enTurno, trabajo: t.trabajo, hastaTramo: t.hastaTramo, apagadoHasta: t.apagadoHasta };
    } catch {
        return null;
    }
}

export function escribirTurnoLocal(turno: TurnoLocal): string {
    return JSON.stringify(turno);
}
