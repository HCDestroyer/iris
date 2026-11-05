"use client";

import { Aviso, Boton, cn, Esqueleto, InsigniaEstado } from "@iris/ui";
import { CalendarOff, ChevronRight, TriangleAlert, UserRoundX } from "lucide-react";
import { useEffect, useRef } from "react";
import { Bloque } from "@/componentes/bloque";
import type { FiltrosDespacho } from "@/componentes/despacho/datos";
import { ErrorLectura } from "@/componentes/error-lectura";
import {
    conflictosDe,
    cuadriculaMes,
    mismoMes,
    resumenPorDia,
    sinTecnicoPorDia,
    textoOrdenes,
    trozos,
    unirCalendarios,
    type ResumenDia,
} from "@/lib/agenda/mes";
import { api, resolver } from "@/lib/api/cliente";
import { ausenciasDelDia, textoAusencia, type AusenciaAgenda, type CitaAgenda, type Tecnico } from "@/lib/despacho/calendario";
import { diaDesdeIso, etiquetaDiaLarga, horaDe, hoyGuatemala, rangoUtc, type Dia } from "@/lib/despacho/fechas";
import { useLectura, type Lectura } from "@/lib/lectura";
import { ESTADOS_ORDEN, TIPOS_ORDEN, type Orden } from "@/lib/ordenes/presentacion";
import { useSesion } from "@/lib/sesion";

const SEMANA = ["Lun", "Mar", "Mié", "Jue", "Vie", "Sáb", "Dom"];

/** Órdenes sin técnico del mes que se leen (con más, no se cuentan por día). */
const LIMITE_SIN_TECNICO = 200;

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

/**
 * Órdenes programadas sin técnico en el rango (`GET /ordenes?sinTecnico=true&desde&hasta`), con
 * `ordenes.ver`. `null` sin permiso; con más de las que caben, `cortada` (no se cuentan por día).
 */
function useSinTecnico(desde: Dia, dias: number, filtros: FiltrosDespacho, activo: boolean) {
    return useLectura<{ ordenes: Orden[]; cortada: boolean } | null>(async (signal) => {
        if (!activo) return null;
        const p = await resolver(
            api().GET("/api/v1/ordenes", {
                params: {
                    query: {
                        sinTecnico: true,
                        ...rangoUtc(desde, dias),
                        limit: LIMITE_SIN_TECNICO,
                        ...(filtros.cuadrillaId !== null ? { cuadrillaId: filtros.cuadrillaId } : {}),
                        ...(filtros.zonaId !== null ? { zonaId: filtros.zonaId } : {}),
                    },
                },
                signal,
            }),
        );
        return { ordenes: p.data ?? [], cortada: p.nextCursor !== null };
    });
}

function textoResumen(dia: Dia, r: ResumenDia | undefined, sinTecnico: number | undefined): string {
    const partes: string[] = [];
    if (r && r.total > 0) partes.push(textoOrdenes(r.total), ...r.porEstado.map((e) => `${e.cuantas} ${ESTADOS_ORDEN[e.estado].texto.toLowerCase()}`));
    if (r && r.conflictos > 0) partes.push(r.conflictos === 1 ? "1 con conflicto" : `${r.conflictos} con conflicto`);
    if (r && r.ausentes > 0) partes.push(r.ausentes === 1 ? "1 técnico ausente" : `${r.ausentes} técnicos ausentes`);
    if (sinTecnico) partes.push(`${sinTecnico} sin técnico`);
    return partes.length === 0 ? `${etiquetaDiaLarga(dia)}: sin órdenes` : `${etiquetaDiaLarga(dia)}: ${partes.join(", ")}`;
}

/** Lo que dice cada día de la cuadrícula: total de órdenes, conflictos, ausencias y órdenes sin técnico. */
function Marcas({ r, sinTecnico, ausentes }: { r: ResumenDia | undefined; sinTecnico: number | undefined; ausentes: string[] }) {
    return (
        <>
            {r && r.total > 0 ? (
                <span
                    className="block truncate rounded-[0.3125rem] bg-success/10 px-1.5 py-0.5 text-[0.6875rem] font-semibold text-success tabular-nums"
                    data-ordenes-dia={r.total}
                >
                    {textoOrdenes(r.total)}
                </span>
            ) : null}
            {r && r.conflictos > 0 ? (
                <InsigniaEstado
                    tono="error"
                    icono={TriangleAlert}
                    texto={r.conflictos === 1 ? "1 conflicto" : `${r.conflictos} conflictos`}
                    data-conflictos={r.conflictos}
                />
            ) : null}
            {ausentes.length > 0 ? (
                <InsigniaEstado
                    tono="neutral"
                    icono={CalendarOff}
                    texto={ausentes.length === 1 ? `Ausencia · ${ausentes[0]}` : `${ausentes.length} ausencias`}
                    data-ausencias={ausentes.length}
                />
            ) : null}
            {sinTecnico ? <InsigniaEstado tono="warning" icono={UserRoundX} texto={`${sinTecnico} sin técnico`} data-sin-tecnico={sinTecnico} /> : null}
        </>
    );
}

type Props = {
    dia: Dia;
    filtros: FiltrosDespacho;
    /** Elegir un día: queda marcado y su resumen aparece al lado. */
    alElegirDia: (d: Dia) => void;
    /** «Abrir el día» / «Programar»: la vista de día por técnico. */
    alAbrirDia: (d: Dia, programar: boolean) => void;
    /** Para los nombres de técnicos en el resumen (los del filtro); sin ellos, «Técnico N». */
    tecnicos: Lectura<{ lista: Tecnico[]; cortada: boolean }> | undefined;
};

/**
 * Agenda del mes (nuevo diseño): cuadrícula de lunes a domingo (5 o 6 semanas) donde cada día dice
 * cuántas órdenes tiene, sus conflictos, las ausencias y las órdenes sin técnico; hoy va marcado.
 * Al elegir un día, su resumen aparece al lado con «Abrir el día» y «Programar». En el teléfono,
 * una lista por día con el resumen arriba.
 */
export function VistaMes({ dia, filtros, alElegirDia, alAbrirDia, tecnicos }: Props) {
    const { puede } = useSesion();
    const { desde, dias, semanas } = cuadriculaMes(dia);
    const datos = useDatosMes(desde, dias.length, filtros);
    const sinTecnico = useSinTecnico(desde, dias.length, filtros, puede(["ordenes.ver"]));
    const hoy = hoyGuatemala();
    const resumenRef = useRef<HTMLElement>(null);
    const elegido = useRef(false);
    useEffect(() => {
        // En el teléfono el resumen está arriba de la lista: al elegir un día se lleva a la vista.
        if (elegido.current && window.matchMedia("(max-width: 767px)").matches) resumenRef.current?.scrollIntoView({ block: "start" });
        elegido.current = false;
    }, [dia]);
    const elegir = (d: Dia) => {
        elegido.current = true;
        alElegirDia(d);
    };

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
    const sinTec = sinTecnico.estado === "listo" && sinTecnico.datos && !sinTecnico.datos.cortada ? sinTecnicoPorDia(sinTecnico.datos.ordenes, dias) : null;
    const delMes = dias.filter((d) => mismoMes(d, dia));
    const totalMes = delMes.reduce((n, d) => n + (resumen.get(d)?.total ?? 0), 0);
    const conflictosMes = delMes.reduce((n, d) => n + (resumen.get(d)?.conflictos ?? 0), 0);
    const listaTecnicos = tecnicos?.estado === "listo" ? tecnicos.datos.lista : [];
    const nombreTecnico = (usuarioId: number) => listaTecnicos.find((t) => t.usuarioId === usuarioId)?.nombre ?? `Técnico ${usuarioId}`;
    const ausentesDe = (d: Dia) => [
        ...new Set(
            datos.datos.ausencias.filter((a) => ausenciasDelDia([a], a.tecnicoId, d).length > 0).map((a) => nombreTecnico(a.tecnicoId).split(" ")[0] ?? ""),
        ),
    ];

    const resumenDia = (
        <ResumenDelDia
            ref={resumenRef}
            dia={dia}
            citas={datos.datos.citas}
            ausencias={datos.datos.ausencias}
            r={resumen.get(dia)}
            sinTecnico={sinTec?.get(dia)}
            nombreTecnico={nombreTecnico}
            conflictos={conflictos}
            alAbrir={alAbrirDia}
        />
    );

    return (
        <div className="grid gap-3 [&>*]:min-w-0">
            <p className="flex flex-wrap items-center gap-2 text-[0.8125rem] text-muted-foreground" data-testid="resumen-mes">
                <span className="tabular-nums">{textoOrdenes(totalMes)} con cita este mes.</span>
                {conflictosMes > 0 ? (
                    <InsigniaEstado
                        tono="error"
                        icono={TriangleAlert}
                        texto={conflictosMes === 1 ? "1 orden con conflicto" : `${conflictosMes} órdenes con conflicto`}
                    />
                ) : null}
                {sinTecnico.estado === "listo" && sinTecnico.datos?.cortada ? (
                    <span>Más de {LIMITE_SIN_TECNICO} órdenes sin técnico: filtra para verlas por día.</span>
                ) : null}
            </p>
            {sinTecnico.estado === "error" ? <ErrorLectura error={sinTecnico.error} que="las órdenes sin técnico" reintentar={sinTecnico.reintentar} /> : null}
            <div className="grid items-start gap-4 [&>*]:min-w-0 md:grid-cols-[minmax(0,1fr)_18rem] xl:grid-cols-[minmax(0,1fr)_20rem]">
                {/* Teléfono: el resumen del día elegido va arriba de la lista. */}
                <div className="md:hidden">{resumenDia}</div>
                {/* Escritorio: cuadrícula del mes. */}
                <section
                    aria-label="Calendario del mes"
                    className="hidden overflow-hidden rounded-xl border border-border bg-card md:block"
                    data-testid="cuadricula-mes"
                >
                    <div role="grid" aria-label="Calendario del mes" className="grid grid-cols-7">
                        <div role="row" className="contents">
                            {SEMANA.map((s) => (
                                <span
                                    key={s}
                                    role="columnheader"
                                    className="border-b border-border bg-background-secondary px-2.5 py-2 text-xs font-semibold text-muted-foreground"
                                >
                                    {s}
                                </span>
                            ))}
                        </div>
                        {semanas.map((semana) => (
                            <div role="row" key={semana[0]} className="contents">
                                {semana.map((d) => {
                                    const r = resumen.get(d);
                                    const fuera = !mismoMes(d, dia);
                                    const st = sinTec?.get(d);
                                    return (
                                        <div
                                            role="gridcell"
                                            key={d}
                                            aria-selected={d === dia}
                                            className="min-w-0 border-b border-l border-border/70 first:border-l-0"
                                        >
                                            <Boton
                                                variante="fantasma"
                                                className={cn(
                                                    "h-full min-h-28 w-full flex-col items-stretch justify-start gap-1 rounded-none p-2 text-left font-normal whitespace-normal",
                                                    fuera && "bg-background-secondary opacity-55",
                                                    d === dia && "bg-primary/5 shadow-[inset_0_0_0_2px_var(--hx-primary)]",
                                                    "[&_[data-slot=insignia-estado]]:max-w-full [&_[data-slot=insignia-estado]]:truncate",
                                                )}
                                                data-dia={d}
                                                data-total={r?.total ?? 0}
                                                aria-pressed={d === dia}
                                                aria-label={`${textoResumen(d, r, st)}. Ver el resumen del día`}
                                                onClick={() => elegir(d)}
                                            >
                                                <span
                                                    className={cn(
                                                        "grid h-[1.375rem] min-w-[1.375rem] place-items-center self-start rounded-full px-1 text-xs font-semibold tabular-nums",
                                                        d === hoy ? "bg-primary text-primary-foreground" : "text-muted-foreground",
                                                    )}
                                                    {...(d === hoy ? { "aria-current": "date" as const } : {})}
                                                >
                                                    {Number(d.slice(8))}
                                                </span>
                                                <Marcas r={r} sinTecnico={st} ausentes={ausentesDe(d)} />
                                            </Boton>
                                        </div>
                                    );
                                })}
                            </div>
                        ))}
                    </div>
                </section>
                <div className="hidden md:block md:sticky md:top-20">{resumenDia}</div>
                {/* Teléfono: lista por día. */}
                <ul className="grid gap-2 md:hidden" aria-label="Días del mes" data-testid="lista-mes">
                    {delMes.map((d) => {
                        const r = resumen.get(d);
                        const st = sinTec?.get(d);
                        const ausentes = ausentesDe(d);
                        const vacio = (!r || r.total === 0) && ausentes.length === 0 && !st;
                        return (
                            <li key={d} className="min-w-0">
                                <Boton
                                    variante="fantasma"
                                    className={cn(
                                        "h-auto w-full items-start justify-between gap-2 rounded-lg border border-border bg-card px-3 py-2 text-left font-normal whitespace-normal",
                                        vacio && "py-1.5",
                                        d === dia && "border-primary bg-primary/5",
                                    )}
                                    data-dia={d}
                                    data-total={r?.total ?? 0}
                                    aria-pressed={d === dia}
                                    aria-label={`${textoResumen(d, r, st)}. Ver el resumen del día`}
                                    onClick={() => elegir(d)}
                                >
                                    <span className="grid min-w-0 flex-1 gap-1">
                                        <span className="flex flex-wrap items-baseline gap-x-2">
                                            <span className="text-sm font-semibold first-letter:uppercase">{etiquetaDiaLarga(d)}</span>
                                            {d === hoy ? <span className="text-xs font-medium text-primary">Hoy</span> : null}
                                            {vacio ? <span className="text-xs text-muted-foreground">Sin órdenes</span> : null}
                                        </span>
                                        {!vacio ? (
                                            <span className="flex flex-wrap gap-1">
                                                <Marcas r={r} sinTecnico={st} ausentes={ausentes} />
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
        </div>
    );
}

/** Resumen del día elegido: sus citas por hora, las ausencias, «Abrir el día» y «Programar». */
function ResumenDelDia({
    ref,
    dia,
    citas,
    ausencias,
    r,
    sinTecnico,
    nombreTecnico,
    conflictos,
    alAbrir,
}: {
    ref: React.Ref<HTMLElement>;
    dia: Dia;
    citas: readonly CitaAgenda[];
    ausencias: readonly AusenciaAgenda[];
    r: ResumenDia | undefined;
    sinTecnico: number | undefined;
    nombreTecnico: (usuarioId: number) => string;
    conflictos: ReturnType<typeof conflictosDe>;
    alAbrir: (d: Dia, programar: boolean) => void;
}) {
    const { puede } = useSesion();
    const delDia = citas.filter((c) => diaDesdeIso(c.inicio) === dia).sort((a, b) => a.inicio.localeCompare(b.inicio));
    const ausentes = ausencias.filter((a) => ausenciasDelDia([a], a.tecnicoId, dia).length > 0);
    const partes = [textoOrdenes(r?.total ?? 0)];
    if (ausentes.length > 0) partes.push(ausentes.length === 1 ? "1 ausencia" : `${ausentes.length} ausencias`);
    if (sinTecnico) partes.push(`${sinTecnico} sin técnico`);
    return (
        <Bloque ref={ref} aria-label={`Resumen del ${etiquetaDiaLarga(dia)}`} data-testid="resumen-dia" className="scroll-mt-20">
            <div className="grid gap-1 border-b border-border px-4 py-3.5">
                <h2 className="text-[0.9375rem] font-semibold first-letter:uppercase">{etiquetaDiaLarga(dia)}</h2>
                <p className="text-xs text-muted-foreground">{partes.join(" · ")}</p>
            </div>
            {delDia.length === 0 && ausentes.length === 0 ? (
                <p className="px-4 py-5 text-[0.8125rem] text-muted-foreground">Sin órdenes ni ausencias este día.</p>
            ) : (
                <ul className="max-h-[26rem] divide-y divide-border/70 overflow-y-auto" aria-label="Citas del día">
                    {delDia.map((c) => {
                        const e = ESTADOS_ORDEN[c.estado];
                        const Icono = e.icono;
                        return (
                            <li key={c.ordenId} className="flex items-center gap-2.5 px-4 py-2.5" data-cita-resumen={c.numero}>
                                <span className="w-11 shrink-0 font-mono text-xs font-semibold">{horaDe(c.inicio)}</span>
                                <span title={e.texto} className="shrink-0">
                                    <Icono
                                        aria-hidden
                                        className={cn(
                                            "size-4",
                                            e.tono === "error"
                                                ? "text-error"
                                                : e.tono === "warning"
                                                  ? "text-warning"
                                                  : e.tono === "success"
                                                    ? "text-success"
                                                    : e.tono === "info"
                                                      ? "text-info"
                                                      : "text-muted-foreground",
                                        )}
                                    />
                                    <span className="sr-only">{e.texto}</span>
                                </span>
                                <span className="grid min-w-0 flex-1">
                                    <span className="truncate text-[0.8125rem] font-semibold">
                                        {TIPOS_ORDEN[c.tipo].texto} · {c.numero}
                                    </span>
                                    <span className="truncate text-xs text-muted-foreground">
                                        {nombreTecnico(c.tecnicoId)}
                                        {conflictos.has(c.ordenId) ? " · con conflicto" : ""}
                                    </span>
                                </span>
                            </li>
                        );
                    })}
                    {ausentes.map((a) => (
                        <li key={`a${a.id}`} className="flex items-center gap-2.5 px-4 py-2.5">
                            <span className="w-11 shrink-0 font-mono text-xs font-semibold">{horaDe(a.inicio)}</span>
                            <CalendarOff aria-hidden className="size-4 shrink-0 text-muted-foreground" />
                            <span className="grid min-w-0 flex-1">
                                <span className="truncate text-[0.8125rem] font-semibold">Ausencia · {textoAusencia(a.motivo)}</span>
                                <span className="truncate text-xs text-muted-foreground">{nombreTecnico(a.tecnicoId)}</span>
                            </span>
                        </li>
                    ))}
                </ul>
            )}
            <div className="flex gap-2 border-t border-border px-4 py-3">
                <Boton variante="secundario" tamano="sm" className="h-[2.125rem] flex-1 rounded-lg" onClick={() => alAbrir(dia, false)}>
                    Abrir el día
                </Boton>
                {puede(["ordenes.programar"]) ? (
                    <Boton tamano="sm" className="h-[2.125rem] flex-1 rounded-lg" onClick={() => alAbrir(dia, true)}>
                        Programar
                    </Boton>
                ) : null}
            </div>
        </Bloque>
    );
}

/** La API cortó la respuesta (más citas de las que caben): se pide acotar, sin mostrar conteos. */
export function AvisoDemasiadas() {
    return (
        <Aviso tono="warning" titulo="Demasiadas citas para mostrarlas todas" data-testid="agenda-truncada">
            Filtra por técnico, cuadrilla o zona para ver el calendario completo. Mientras tanto no se muestran totales ni días vacíos, porque faltarían citas.
        </Aviso>
    );
}
