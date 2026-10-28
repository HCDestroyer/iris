import type { paths } from "@iris/contratos";
import type { Orden } from "@/lib/ordenes/presentacion";

/*
 * Ruta propuesta (OSRM, I2) frente a la real. La API de rutas aún no está en packages/contratos:
 * el panel no inventa su forma. `RUTAS_EN_CONTRATO` es `false` mientras no exista ninguna ruta
 * con «/ruta» en el contrato; cuando I2 la publique, la aserción de tipos falla en `pnpm
 * typecheck` para conectar el componente en la misma PR que regenera packages/contratos (como
 * los permisos de lib/menu.ts). Qué falta está en .claude/integration/i3.md.
 */
type RutasEnContrato = Extract<keyof paths, `${string}/ruta${string}`>;
export const RUTAS_EN_CONTRATO = false satisfies [RutasEnContrato] extends [never] ? false : true;

/** Parada de la ruta propuesta, ya traducida del contrato de rutas (cuando exista). */
export type ParadaPropuesta = { ordenId: number; numero: string; posicion: number; llegadaEstimada: string | null; lat: number | null; lon: number | null };

/** Llegada real: la orden pasó a «en sitio» (`enSitioEn`), en el orden en que ocurrió. */
export type ParadaReal = { ordenId: number; numero: string; posicion: number; llegadaEn: string; lat: number | null; lon: number | null };

/** Paradas reales del técnico: órdenes del día con llegada, por hora de llegada. */
export function paradasReales(ordenes: readonly Orden[]): ParadaReal[] {
    return ordenes
        .filter((o): o is Orden & { enSitioEn: string } => o.enSitioEn !== null)
        .sort((a, b) => a.enSitioEn.localeCompare(b.enSitioEn))
        .map((o, i) => ({ ordenId: o.id, numero: o.numero, posicion: i + 1, llegadaEn: o.enSitioEn, lat: o.lat, lon: o.lon }));
}

export type ComparacionParada = {
    ordenId: number;
    numero: string;
    propuesta: number | null;
    real: number | null;
    /** en_orden, antes / despues de lo propuesto, sin_visitar aún, o fuera de la propuesta. */
    resultado: "en_orden" | "antes" | "despues" | "sin_visitar" | "fuera_de_propuesta" | "sin_propuesta";
};

/**
 * Compara el orden propuesto con el real parada por parada. Sin propuesta (API de rutas aún no
 * publicada) cada parada real queda `sin_propuesta`: nunca se calcula un desvío sin ella.
 */
export function compararRutas(propuesta: readonly ParadaPropuesta[] | null, real: readonly ParadaReal[]): ComparacionParada[] {
    if (propuesta === null) {
        return real.map((r) => ({ ordenId: r.ordenId, numero: r.numero, propuesta: null, real: r.posicion, resultado: "sin_propuesta" }));
    }
    const reales = new Map(real.map((r) => [r.ordenId, r]));
    // Posición real relativa entre las paradas propuestas ya visitadas (una parada extra no desplaza a las demás).
    const visitadas = real.filter((r) => propuesta.some((p) => p.ordenId === r.ordenId));
    const relativa = new Map(visitadas.map((r, i) => [r.ordenId, i + 1]));
    const propuestasVisitadas = [...propuesta].sort((a, b) => a.posicion - b.posicion).filter((p) => reales.has(p.ordenId));
    const esperada = new Map(propuestasVisitadas.map((p, i) => [p.ordenId, i + 1]));
    const filas: ComparacionParada[] = [...propuesta]
        .sort((a, b) => a.posicion - b.posicion)
        .map((p) => {
            const r = reales.get(p.ordenId);
            if (!r) return { ordenId: p.ordenId, numero: p.numero, propuesta: p.posicion, real: null, resultado: "sin_visitar" };
            const rel = relativa.get(p.ordenId) ?? 0;
            const esp = esperada.get(p.ordenId) ?? 0;
            return { ordenId: p.ordenId, numero: p.numero, propuesta: p.posicion, real: r.posicion, resultado: rel === esp ? "en_orden" : rel < esp ? "antes" : "despues" };
        });
    for (const r of real) {
        if (!propuesta.some((p) => p.ordenId === r.ordenId)) {
            filas.push({ ordenId: r.ordenId, numero: r.numero, propuesta: null, real: r.posicion, resultado: "fuera_de_propuesta" });
        }
    }
    return filas;
}

export const RESULTADOS_PARADA: Record<ComparacionParada["resultado"], string> = {
    en_orden: "En el orden propuesto",
    antes: "Antes de lo propuesto",
    despues: "Después de lo propuesto",
    sin_visitar: "Sin visitar",
    fuera_de_propuesta: "Fuera de la propuesta",
    sin_propuesta: "Sin propuesta",
};
