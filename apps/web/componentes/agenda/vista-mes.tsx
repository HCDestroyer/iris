"use client";

import { Aviso, Boton, Esqueleto, InsigniaEstado, cn } from "@iris/ui";
import { CalendarOff, ChevronRight, TriangleAlert } from "lucide-react";
import { ErrorLectura } from "@/componentes/error-lectura";
import type { FiltrosDespacho } from "@/componentes/despacho/datos";
import { conflictosDe, cuadriculaMes, mismoMes, resumenPorDia, textoOrdenes, trozos, unirCalendarios, type ResumenDia } from "@/lib/agenda/mes";
import { api, resolver } from "@/lib/api/cliente";
import type { AusenciaAgenda, CitaAgenda } from "@/lib/despacho/calendario";
import { etiquetaDiaLarga, hoyGuatemala, rangoUtc, type Dia } from "@/lib/despacho/fechas";
import { useLectura } from "@/lib/lectura";
import { ESTADOS_ORDEN } from "@/lib/ordenes/presentacion";

const SEMANA = ["Lun", "Mar", "Mié", "Jue", "Vie", "Sáb", "Dom"];

type DatosMes = { citas: CitaAgenda[]; ausencias: AusenciaAgenda[]; truncado: boolean };

/**
 * `GET /agenda` del mes visible (semanas completas, hasta 42 días) en trozos de 31 días. Si un
 * trozo falla, falla el mes entero: nunca se muestran conteos de un mes leído a medias.
 */
function useDatosMes(desde: Dia, dias: number, filtros: FiltrosDespacho) {
    return useLectura<DatosMes>(async (signal) => {
        const partes = await Promise.all(
            trozos(desde, dias).map((t) =>
                resolver(
                    api().GET("/api/v1/agenda", {
                        params: {
                            query: {
                                ...rangoUtc(t.desde, t.dias),
                                ...(filtros.cuadrillaId !== null ? { cuadrillaId: filtros.cuadrillaId } : {}),
                                ...(filtros.zonaId !== null ? { zonaId: filtros.zonaId } : {}),
                                ...(filtros.tecnicoId ? { tecnicoId: filtros.tecnicoId } : {}),
                            },
                        },
                        signal,
                    }),
                ),
            ),
        );
        return unirCalendarios(partes);
    });
}

function textoResumen(dia: Dia, r: ResumenDia | undefined): string {
    if (!r || (r.total === 0 && r.ausentes === 0)) return `${etiquetaDiaLarga(dia)}: sin órdenes`;
    const partes = [textoOrdenes(r.total), ...r.porEstado.map((e) => `${e.cuantas} ${ESTADOS_ORDEN[e.estado].texto.toLowerCase()}`)];
    if (r.conflictos > 0) partes.push(r.conflictos === 1 ? "1 con conflicto" : `${r.conflictos} con conflicto`);
    if (r.ausentes > 0) partes.push(r.ausentes === 1 ? "1 técnico ausente" : `${r.ausentes} técnicos ausentes`);
    return `${etiquetaDiaLarga(dia)}: ${partes.join(", ")}`;
}

/** Insignias del día: estados (icono + texto + número), conflictos y ausentes. */
function Insignias({ r, max }: { r: ResumenDia; max: number }) {
    const estados = r.porEstado.slice(0, max);
    const resto = r.porEstado.slice(max).reduce((n, e) => n + e.cuantas, 0);
    return (
        <>
            {r.conflictos > 0 ? (
                <InsigniaEstado tono="error" icono={TriangleAlert} texto={r.conflictos === 1 ? "1 conflicto" : `${r.conflictos} conflictos`} data-conflictos={r.conflictos} />
            ) : null}
            {estados.map(({ estado, cuantas }) => {
                const p = ESTADOS_ORDEN[estado];
                return <InsigniaEstado key={estado} tono={p.tono} icono={p.icono} texto={`${cuantas} ${p.texto.toLowerCase()}`} data-estado-dia={estado} className="tabular-nums" />;
            })}
            {resto > 0 ? <span className="text-xs text-muted-foreground">+{resto} más</span> : null}
            {r.ausentes > 0 ? <InsigniaEstado tono="neutral" icono={CalendarOff} texto={r.ausentes === 1 ? "1 ausente" : `${r.ausentes} ausentes`} /> : null}
        </>
    );
}

/**
 * Vista de mes: en escritorio, la cuadrícula de lunes a domingo; en el teléfono, una lista por
 * día. Cada día dice cuántas órdenes tiene y en qué estado; al tocarlo se abre su detalle.
 */
export function VistaMes({ dia, filtros, alElegirDia }: { dia: Dia; filtros: FiltrosDespacho; alElegirDia: (d: Dia) => void }) {
    const { desde, dias, semanas } = cuadriculaMes(dia);
    const datos = useDatosMes(desde, dias.length, filtros);
    const hoy = hoyGuatemala();

    if (datos.estado === "error") {
        return <ErrorLectura error={datos.error} que="la agenda del mes" reintentar={datos.reintentar} />;
    }
    if (datos.estado === "cargando") {
        return (
            <div className="grid gap-2" aria-busy="true" aria-label="Cargando la agenda del mes">
                <Esqueleto className="h-24" />
                <Esqueleto className="h-24" />
                <Esqueleto className="h-24" />
            </div>
        );
    }
    if (datos.datos.truncado) {
        // §10: nunca totales ni «Sin órdenes» sobre una lectura cortada.
        return <AvisoDemasiadas />;
    }
    const conflictos = conflictosDe(datos.datos.citas, datos.datos.ausencias);
    const resumen = resumenPorDia(dias, datos.datos.citas, datos.datos.ausencias, conflictos);
    const delMes = dias.filter((d) => mismoMes(d, dia));
    const totalMes = delMes.reduce((n, d) => n + (resumen.get(d)?.total ?? 0), 0);
    const conflictosMes = delMes.reduce((n, d) => n + (resumen.get(d)?.conflictos ?? 0), 0);

    return (
        <div className="grid gap-3 [&>*]:min-w-0">
            <p className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground" data-testid="resumen-mes">
                <span className="tabular-nums">{textoOrdenes(totalMes)} con cita este mes.</span>
                {conflictosMes > 0 ? <InsigniaEstado tono="error" icono={TriangleAlert} texto={conflictosMes === 1 ? "1 orden con conflicto" : `${conflictosMes} órdenes con conflicto`} /> : null}
            </p>
            {/* Escritorio: cuadrícula del mes. */}
            <div className="hidden md:block" data-testid="cuadricula-mes">
                <div role="grid" aria-label="Calendario del mes" className="grid grid-cols-7 overflow-hidden rounded-lg border border-border">
                    <div role="row" className="contents">
                        {SEMANA.map((s) => (
                            <span key={s} role="columnheader" className="border-b border-border bg-elevated px-2 py-1.5 text-xs font-medium text-muted-foreground">
                                {s}
                            </span>
                        ))}
                    </div>
                    {semanas.map((semana) => (
                        <div role="row" key={semana[0]} className="contents">
                            {semana.map((d) => {
                                const r = resumen.get(d);
                                const fuera = !mismoMes(d, dia);
                                return (
                                    <div role="gridcell" key={d} className="min-w-0 border-b border-l border-border first:border-l-0">
                                        <Boton
                                            variante="fantasma"
                                            className={cn(
                                                "h-full min-h-28 w-full flex-col items-stretch justify-start gap-1 rounded-none p-1.5 text-left font-normal whitespace-normal",
                                                fuera && "opacity-55",
                                            )}
                                            data-dia={d}
                                            data-total={r?.total ?? 0}
                                            aria-label={`${textoResumen(d, r)}. Ver el día`}
                                            onClick={() => alElegirDia(d)}
                                        >
                                            <span className="flex items-center justify-between gap-1">
                                                <span
                                                    className={cn(
                                                        "grid size-6 place-items-center rounded-full text-xs font-semibold tabular-nums",
                                                        d === hoy && "bg-primary text-primary-foreground",
                                                    )}
                                                >
                                                    {Number(d.slice(8))}
                                                </span>
                                                {r && r.total > 0 ? <span className="text-xs font-medium tabular-nums">{textoOrdenes(r.total)}</span> : null}
                                            </span>
                                            {r ? (
                                                <span className="flex flex-wrap content-start gap-1 [&_[data-slot=insignia-estado]]:max-w-full [&_[data-slot=insignia-estado]]:truncate">
                                                    <Insignias r={r} max={3} />
                                                </span>
                                            ) : null}
                                        </Boton>
                                    </div>
                                );
                            })}
                        </div>
                    ))}
                </div>
            </div>
            {/* Teléfono: lista por día. */}
            <ul className="grid gap-2 md:hidden" aria-label="Días del mes" data-testid="lista-mes">
                {delMes.map((d) => {
                    const r = resumen.get(d);
                    const vacio = !r || (r.total === 0 && r.ausentes === 0);
                    return (
                        <li key={d} className="min-w-0">
                            <Boton
                                variante="fantasma"
                                className={cn(
                                    "h-auto w-full items-start justify-between gap-2 rounded-lg border border-border px-3 py-2 text-left font-normal whitespace-normal",
                                    vacio && "py-1.5",
                                    d === hoy && "border-primary",
                                )}
                                data-dia={d}
                                data-total={r?.total ?? 0}
                                aria-label={`${textoResumen(d, r)}. Ver el día`}
                                onClick={() => alElegirDia(d)}
                            >
                                <span className="grid min-w-0 flex-1 gap-1">
                                    <span className="flex flex-wrap items-baseline gap-x-2">
                                        <span className="text-sm font-semibold first-letter:uppercase">{etiquetaDiaLarga(d)}</span>
                                        {d === hoy ? <span className="text-xs font-medium text-primary">Hoy</span> : null}
                                        <span className="text-xs text-muted-foreground tabular-nums">{vacio ? "Sin órdenes" : textoOrdenes(r.total)}</span>
                                    </span>
                                    {!vacio && r ? (
                                        <span className="flex flex-wrap gap-1">
                                            <Insignias r={r} max={8} />
                                        </span>
                                    ) : null}
                                </span>
                                <ChevronRight aria-hidden className="mt-0.5 shrink-0 text-muted-foreground" />
                            </Boton>
                        </li>
                    );
                })}
            </ul>
        </div>
    );
}

/** La API cortó la respuesta (más citas de las que caben): se pide acotar, sin mostrar conteos. */
export function AvisoDemasiadas() {
    return (
        <Aviso tono="warning" titulo="Demasiadas citas para mostrarlas todas" data-testid="agenda-truncada">
            Filtra por técnico, cuadrilla o zona para ver el calendario completo. Mientras tanto no se muestran totales ni días vacíos, porque
            faltarían citas.
        </Aviso>
    );
}
