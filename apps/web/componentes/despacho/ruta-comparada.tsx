"use client";

import { Aviso, CampoSelector, Esqueleto, InsigniaEstado, type Tono } from "@iris/ui";
import { ArrowDown, ArrowUp, Check, CircleDashed, Clock, Route, Shuffle, type LucideIcon } from "lucide-react";
import { ErrorLectura } from "@/componentes/error-lectura";
import type { FilaTecnico } from "@/lib/despacho/calendario";
import { horaDe } from "@/lib/despacho/fechas";
import { RESULTADOS_PARADA, RUTAS_EN_CONTRATO, compararRutas, type ComparacionParada, type ParadaPropuesta, type ParadaReal } from "@/lib/despacho/rutas";
import type { Lectura } from "@/lib/lectura";
import type { Orden } from "@/lib/ordenes/presentacion";

const PRESENTACION: Record<ComparacionParada["resultado"], { tono: Tono; icono: LucideIcon }> = {
    en_orden: { tono: "success", icono: Check },
    antes: { tono: "info", icono: ArrowUp },
    despues: { tono: "warning", icono: ArrowDown },
    sin_visitar: { tono: "neutral", icono: CircleDashed },
    fuera_de_propuesta: { tono: "warning", icono: Shuffle },
    sin_propuesta: { tono: "neutral", icono: Clock },
};

/**
 * Ruta propuesta frente a la real de un técnico en el día. La real son sus llegadas («en sitio»)
 * en el orden en que ocurrieron. La propuesta llega con la API de rutas de I2: hasta que esté en
 * packages/contratos (`RUTAS_EN_CONTRATO`), el componente lo dice y no calcula desvíos.
 */
export function RutaComparada({
    filas,
    tecnicoId,
    alElegir,
    ordenes,
    propuesta,
    reales,
}: {
    filas: readonly FilaTecnico[];
    tecnicoId: number | null;
    alElegir: (id: number | null) => void;
    ordenes: Lectura<Orden[] | null> | null;
    /** Paradas propuestas ya traducidas del contrato de rutas; `null` mientras no exista. */
    propuesta: readonly ParadaPropuesta[] | null;
    reales: readonly ParadaReal[];
}) {
    return (
        <div className="grid gap-3">
            <CampoSelector etiqueta="Técnico de la ruta" className="sm:max-w-xs" value={tecnicoId ?? ""} onChange={(e) => alElegir(e.target.value === "" ? null : Number(e.target.value))}>
                <option value="">Elige un técnico</option>
                {filas.map((f) => (
                    <option key={f.usuarioId} value={f.usuarioId}>
                        {f.nombre}
                    </option>
                ))}
            </CampoSelector>
            {!RUTAS_EN_CONTRATO ? (
                <Aviso tono="info" titulo="Ruta propuesta: todavía no disponible" icono={Route} data-testid="ruta-propuesta-pendiente">
                    La propuesta del día (orden óptimo con OSRM) llega con el módulo de rutas. Mientras tanto se muestra solo el recorrido real por
                    llegadas.
                </Aviso>
            ) : null}
            {tecnicoId === null ? null : ordenes === null ? (
                <p className="text-sm text-muted-foreground">Ver el recorrido real requiere el permiso «ordenes.ver».</p>
            ) : ordenes.estado === "error" ? (
                <ErrorLectura error={ordenes.error} reintentar={ordenes.reintentar} que="el recorrido del técnico" />
            ) : ordenes.estado === "cargando" ? (
                <Esqueleto className="h-20" />
            ) : (
                <TablaParadas filas={compararRutas(propuesta, reales)} reales={reales} />
            )}
        </div>
    );
}

function TablaParadas({ filas, reales }: { filas: ComparacionParada[]; reales: readonly ParadaReal[] }) {
    if (filas.length === 0) {
        return <p className="text-sm text-muted-foreground">Todavía no ha llegado a ninguna orden hoy.</p>;
    }
    return (
        <ol className="grid gap-1.5" aria-label="Paradas" data-testid="paradas">
            {filas.map((f) => {
                const p = PRESENTACION[f.resultado];
                const real = reales.find((r) => r.ordenId === f.ordenId);
                return (
                    <li key={f.ordenId} data-parada={f.numero} className="flex flex-wrap items-center gap-2 rounded-md bg-elevated px-2.5 py-2 text-sm">
                        <span className="grid size-6 place-items-center rounded-full bg-background text-xs font-semibold tabular-nums" aria-label={`Parada real ${f.real ?? "—"}`}>
                            {f.real ?? "—"}
                        </span>
                        <span className="font-medium">{f.numero}</span>
                        {real ? <span className="text-xs text-muted-foreground tabular-nums">llegó {horaDe(real.llegadaEn)}</span> : null}
                        {f.propuesta !== null ? <span className="text-xs text-muted-foreground">propuesta: {f.propuesta}.ª</span> : null}
                        <InsigniaEstado tono={p.tono} icono={p.icono} texto={RESULTADOS_PARADA[f.resultado]} className="ml-auto" />
                    </li>
                );
            })}
        </ol>
    );
}
