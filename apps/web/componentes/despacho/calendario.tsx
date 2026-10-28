"use client";

import { Aviso, Boton, EstadoVacio, InsigniaEstado, Tarjeta, cn, type Tono } from "@iris/ui";
import { CalendarOff, CalendarX2, GripVertical, MoveRight, UserRound } from "lucide-react";
import Link from "next/link";
import { useState, type DragEvent, type ReactNode } from "react";
import {
    ausenciasDelDia,
    citasDelDia,
    horarioDelDia,
    minutosEnFraccion,
    nombreDe,
    textoAusencia,
    tramoEnHorario,
    type AusenciaAgenda,
    type Calendario,
    type CitaAgenda,
    type Cuadrilla,
    type FilaTecnico,
    type Horario,
    type Zona,
} from "@/lib/despacho/calendario";
import { diasDesde, etiquetaDia, etiquetaDiaLarga, horaDe, minutosDeTexto, minutosEnDia, textoMinutos, type Dia } from "@/lib/despacho/fechas";
import { arrastrePermitido, movibleDeCita, movibleDeOrden, sePuedeMover, type Movible } from "@/lib/despacho/mover";
import { ESTADOS_ORDEN, TIPOS_ORDEN, type Orden } from "@/lib/ordenes/presentacion";
import { useSesion } from "@/lib/sesion";
import type { Destino } from "./mover-orden";

export type Vista = "dia" | "semana";

type Comun = {
    filas: readonly FilaTecnico[];
    calendario: Calendario;
    zonas: readonly Zona[] | undefined;
    cuadrillas: readonly Cuadrilla[] | undefined;
    /** Abre «Mover a…» (con el destino del arrastre, si lo hay). */
    alMover: (m: Movible, destino?: Destino) => void;
    /** Lo que se está arrastrando (cita o pendiente). */
    arrastre: Movible | null;
    setArrastre: (m: Movible | null) => void;
    ahora: number;
};

const BORDE_TONO: Record<Tono, string> = {
    neutral: "border-l-muted-foreground/60",
    info: "border-l-info",
    success: "border-l-success",
    warning: "border-l-warning",
    error: "border-l-error",
};

const TIPO_ARRASTRE = "application/x-iris-orden";
/** Órdenes de la bandeja visibles antes de «Ver las N». */
const VISIBLES_BANDEJA = 9;
/** Hora que se propone al soltar en la semana una orden que no tenía cita. */
const HORA_POR_DEFECTO = 8 * 60;

function textoCita(c: CitaAgenda): string {
    const ventana = c.ventanaInicio && c.ventanaFin ? ` · ventana ${horaDe(c.ventanaInicio)}–${horaDe(c.ventanaFin)}` : "";
    const llego = c.enSitioEn ? ` · llegó ${horaDe(c.enSitioEn)}` : "";
    return `${c.numero} · ${TIPOS_ORDEN[c.tipo].texto} · ${horaDe(c.inicio)}–${horaDe(c.fin)} · ${ESTADOS_ORDEN[c.estado].texto}${ventana}${llego}`;
}

/** Hace arrastrable un elemento que representa una orden. */
function propsArrastre(m: Movible, movible: boolean, setArrastre: (m: Movible | null) => void) {
    if (!movible) return {};
    return {
        draggable: true,
        onDragStart: (e: DragEvent<HTMLElement>) => {
            e.dataTransfer.effectAllowed = "move";
            e.dataTransfer.setData(TIPO_ARRASTRE, String(m.ordenId));
            e.dataTransfer.setData("text/plain", m.numero);
            setArrastre(m);
        },
        onDragEnd: () => setArrastre(null),
    };
}

/** Zona donde se puede soltar: resalta mientras pasa una orden por encima. */
function useSoltar(arrastre: Movible | null, alSoltar: (m: Movible, e: DragEvent<HTMLElement>) => void) {
    const [encima, setEncima] = useState(false);
    return {
        encima: encima && arrastre !== null,
        props: {
            onDragOver: (e: DragEvent<HTMLElement>) => {
                if (!arrastre) return;
                e.preventDefault();
                e.dataTransfer.dropEffect = "move";
                if (!encima) setEncima(true);
            },
            onDragLeave: () => setEncima(false),
            onDrop: (e: DragEvent<HTMLElement>) => {
                setEncima(false);
                if (!arrastre) return;
                e.preventDefault();
                alSoltar(arrastre, e);
            },
        },
    };
}

/** Bloque de una cita: botón «Mover a…» si se puede mover; si no, enlace a la orden. */
function BloqueCita({ cita, comun, className, style, children }: { cita: CitaAgenda; comun: Comun; className?: string; style?: React.CSSProperties; children: ReactNode }) {
    const { puede } = useSesion();
    const movible = sePuedeMover(cita.estado, puede);
    const m = movibleDeCita(cita);
    const tono = ESTADOS_ORDEN[cita.estado].tono;
    const clases = cn(
        "flex min-w-0 items-center gap-1 overflow-hidden rounded-md border border-l-4 border-border bg-elevated px-1.5 text-left text-xs text-foreground shadow-sm",
        "hover:bg-muted focus-visible:outline-2 focus-visible:outline-offset-1",
        BORDE_TONO[tono],
        comun.arrastre?.ordenId === cita.ordenId && "opacity-50",
        movible && "cursor-grab active:cursor-grabbing",
        className,
    );
    if (!movible) {
        return (
            <Link href={`/ordenes/${cita.ordenId}`} className={clases} style={style} data-cita={cita.numero} data-estado={cita.estado} title={textoCita(cita)} aria-label={`${textoCita(cita)}. Ver orden`}>
                {children}
            </Link>
        );
    }
    return (
        <Boton
            variante="fantasma"
            className={cn(clases, "h-auto justify-start font-normal")}
            style={style}
            data-cita={cita.numero}
            data-estado={cita.estado}
            title={`${textoCita(cita)}. Arrastra para mover o pulsa para «Mover a…»`}
            aria-label={`${textoCita(cita)}. Mover a…`}
            aria-haspopup="dialog"
            onClick={() => comun.alMover(m)}
            {...propsArrastre(m, movible, comun.setArrastre)}
        >
            {children}
        </Boton>
    );
}

function ContenidoCita({ cita, compacto }: { cita: CitaAgenda; compacto?: boolean }) {
    const e = ESTADOS_ORDEN[cita.estado];
    const Estado = e.icono;
    if (compacto) {
        return (
            <>
                <Estado aria-hidden className="size-3.5 shrink-0" />
                <span className="min-w-0 truncate">
                    <span className="font-medium tabular-nums">{horaDe(cita.inicio)}</span> <span className="text-muted-foreground tabular-nums">{cita.numero}</span>
                </span>
            </>
        );
    }
    return (
        <span className="grid min-w-0 leading-tight">
            <span className="flex min-w-0 items-center gap-1">
                <Estado aria-hidden className="size-3.5 shrink-0" />
                <span className="truncate font-medium tabular-nums">{cita.numero}</span>
            </span>
            <span className="truncate text-[0.6875rem] text-muted-foreground tabular-nums">
                {horaDe(cita.inicio)}–{horaDe(cita.fin)} · {e.texto}
            </span>
        </span>
    );
}

function BloqueAusencia({ a, style, className }: { a: AusenciaAgenda; style?: React.CSSProperties; className?: string }) {
    return (
        <div
            data-ausencia={a.id}
            style={style}
            title={`${textoAusencia(a.motivo)} · ${horaDe(a.inicio)}–${horaDe(a.fin)}`}
            className={cn(
                "flex items-center gap-1 overflow-hidden rounded-md border border-dashed border-border px-1.5 text-xs text-muted-foreground",
                "bg-[repeating-linear-gradient(135deg,var(--hx-elevated)_0_6px,transparent_6px_12px)]",
                className,
            )}
        >
            <CalendarOff aria-hidden className="size-3.5 shrink-0" />
            <span className="truncate">{textoAusencia(a.motivo)}</span>
        </div>
    );
}

/** Nombre del técnico: también se puede soltar ahí una orden para asignarla sin cambiar la hora. */
function CabeceraFila({ fila, comun, detalle }: { fila: FilaTecnico; comun: Comun; detalle: string }) {
    const { puede } = useSesion();
    const soltar = useSoltar(arrastrePermitido(comun.arrastre, false, puede), (m) => comun.alMover(m, { tecnicoId: fila.usuarioId, dia: null, minutos: null }));
    return (
        <div {...soltar.props} className={cn("flex min-w-0 items-center gap-2 rounded-md px-2 py-1.5", soltar.encima && "bg-primary/10 outline-2 outline-primary")} data-cabecera-tecnico={fila.usuarioId}>
            <UserRound aria-hidden className="size-4 shrink-0 text-muted-foreground" />
            <div className="grid min-w-0">
                <span className="truncate text-sm font-medium">{fila.nombre}</span>
                <span className="truncate text-xs text-muted-foreground">{detalle}</span>
            </div>
        </div>
    );
}

function detalleFila(fila: FilaTecnico, comun: Comun, citas: number): string {
    const cuadrilla = nombreDe(comun.cuadrillas, fila.cuadrillaId, "Cuadrilla");
    return [cuadrilla, fila.sinFicha ? "sin ficha en este filtro" : null, citas === 1 ? "1 cita" : `${citas} citas`].filter(Boolean).join(" · ");
}

/** Carril de cada cita para que las que se solapan no se tapen (la primera libre, por hora). */
function carriles(citas: readonly CitaAgenda[]): { carril: Map<number, number>; total: number } {
    const fines: number[] = [];
    const carril = new Map<number, number>();
    for (const c of citas) {
        const inicio = Date.parse(c.inicio);
        let i = fines.findIndex((f) => f <= inicio);
        if (i === -1) i = fines.length;
        fines[i] = Date.parse(c.fin);
        carril.set(c.ordenId, i);
    }
    return { carril, total: Math.max(1, fines.length) };
}

const ALTO_CARRIL = 44;

/** Vista de día: una línea de tiempo por técnico; soltar en la línea fija técnico y hora. */
function PistaDia({ fila, dia, horario, comun }: { fila: FilaTecnico; dia: Dia; horario: Horario; comun: Comun }) {
    const citas = citasDelDia(comun.calendario.citas ?? [], fila.usuarioId, dia);
    const ausencias = ausenciasDelDia(comun.calendario.ausencias ?? [], fila.usuarioId, dia);
    const { puede } = useSesion();
    const soltar = useSoltar(arrastrePermitido(comun.arrastre, true, puede), (m, e) => {
        const caja = e.currentTarget.getBoundingClientRect();
        const minutos = minutosEnFraccion((e.clientX - caja.left) / Math.max(1, caja.width), horario);
        comun.alMover(m, { tecnicoId: fila.usuarioId, dia, minutos });
    });
    const horas = Array.from({ length: (horario.hasta - horario.desde) / 60 }, (_, i) => horario.desde + i * 60);
    const total = horario.hasta - horario.desde;
    const ahoraMin = minutosEnDia(new Date(comun.ahora).toISOString(), dia);
    const { carril, total: lineas } = carriles(citas);
    return (
        <div className="grid grid-cols-[12rem_minmax(0,1fr)] items-stretch border-t border-border" data-fila-tecnico={fila.usuarioId} role="row">
            <div role="rowheader">
                <CabeceraFila fila={fila} comun={comun} detalle={detalleFila(fila, comun, citas.length)} />
            </div>
            <div
                role="gridcell"
                aria-label={`Línea de ${fila.nombre}`}
                data-pista={fila.usuarioId}
                {...soltar.props}
                style={{ height: lineas * ALTO_CARRIL + 12 }}
                className={cn("relative", soltar.encima && "bg-primary/10 outline-2 -outline-offset-2 outline-primary")}
            >
                {horas.map((h) => (
                    <span key={h} aria-hidden className="absolute inset-y-0 border-l border-border/60" style={{ left: `${((h - horario.desde) / total) * 100}%` }} />
                ))}
                {ahoraMin > horario.desde && ahoraMin < horario.hasta ? (
                    <span aria-hidden className="absolute inset-y-0 z-10 w-0.5 bg-primary" style={{ left: `${((ahoraMin - horario.desde) / total) * 100}%` }} />
                ) : null}
                {ausencias.map((a) => {
                    const t = tramoEnHorario(a.inicio, a.fin, dia, horario);
                    return t ? <BloqueAusencia key={a.id} a={a} className="absolute inset-y-1 items-start pt-1" style={{ left: `${t.izquierda}%`, width: `${t.ancho}%` }} /> : null;
                })}
                {citas.map((c) => {
                    const v = c.ventanaInicio && c.ventanaFin ? tramoEnHorario(c.ventanaInicio, c.ventanaFin, dia, horario) : null;
                    return v ? (
                        <span
                            key={`v${c.ordenId}`}
                            aria-hidden
                            data-ventana={c.numero}
                            className="absolute inset-y-0.5 rounded-md border border-dashed border-info/60 bg-info/5"
                            style={{ left: `${v.izquierda}%`, width: `${v.ancho}%` }}
                        />
                    ) : null;
                })}
                {citas.map((c) => {
                    const t = tramoEnHorario(c.inicio, c.fin, dia, horario);
                    return t ? (
                        <BloqueCita
                            key={c.ordenId}
                            cita={c}
                            comun={comun}
                            className="absolute min-w-9"
                            style={{ left: `${t.izquierda}%`, width: `${t.ancho}%`, top: 6 + (carril.get(c.ordenId) ?? 0) * ALTO_CARRIL, height: ALTO_CARRIL - 4 }}
                        >
                            <ContenidoCita cita={c} />
                        </BloqueCita>
                    ) : null;
                })}
            </div>
        </div>
    );
}

function RejillaDia({ dia, comun }: { dia: Dia; comun: Comun }) {
    const horario = horarioDelDia(comun.calendario.citas ?? [], dia);
    const horas = Array.from({ length: (horario.hasta - horario.desde) / 60 }, (_, i) => horario.desde + i * 60);
    const total = horario.hasta - horario.desde;
    return (
        <div role="grid" aria-label={`Calendario del ${etiquetaDiaLarga(dia)}`} aria-readonly="false" className="min-w-[48rem]">
            <div role="row" className="grid grid-cols-[12rem_minmax(0,1fr)]">
                <span role="columnheader" className="px-2 py-1 text-xs font-medium text-muted-foreground">
                    Técnico
                </span>
                <div role="columnheader" className="relative h-6" aria-label="Horas">
                    {horas.map((h) => (
                        <span key={h} className="absolute top-1 -translate-x-1/2 text-[0.6875rem] text-muted-foreground tabular-nums first:translate-x-0" style={{ left: `${((h - horario.desde) / total) * 100}%` }}>
                            {textoMinutos(h)}
                        </span>
                    ))}
                </div>
            </div>
            {comun.filas.map((f) => (
                <PistaDia key={f.usuarioId} fila={f} dia={dia} horario={horario} comun={comun} />
            ))}
        </div>
    );
}

/** Celda de la semana: soltar ahí conserva la hora de la cita (o 08:00 si no tenía). */
function CeldaSemana({ fila, dia, comun }: { fila: FilaTecnico; dia: Dia; comun: Comun }) {
    const citas = citasDelDia(comun.calendario.citas ?? [], fila.usuarioId, dia);
    const ausencias = ausenciasDelDia(comun.calendario.ausencias ?? [], fila.usuarioId, dia);
    const { puede } = useSesion();
    const soltar = useSoltar(arrastrePermitido(comun.arrastre, true, puede), (m) => {
        const minutos = (m.inicio ? minutosDeTexto(horaDe(m.inicio)) : undefined) ?? HORA_POR_DEFECTO;
        comun.alMover(m, { tecnicoId: fila.usuarioId, dia, minutos });
    });
    return (
        <div
            role="gridcell"
            aria-label={`${fila.nombre}, ${etiquetaDia(dia)}`}
            data-celda={`${fila.usuarioId}:${dia}`}
            {...soltar.props}
            className={cn("grid min-h-14 content-start gap-1 border-l border-border p-1", soltar.encima && "bg-primary/10 outline-2 -outline-offset-2 outline-primary")}
        >
            {ausencias.map((a) => (
                <BloqueAusencia key={a.id} a={a} className="h-6" />
            ))}
            {citas.map((c) => (
                <BloqueCita key={c.ordenId} cita={c} comun={comun} className="h-7">
                    <ContenidoCita cita={c} compacto />
                </BloqueCita>
            ))}
        </div>
    );
}

function RejillaSemana({ dias, comun }: { dias: Dia[]; comun: Comun }) {
    return (
        <div role="grid" aria-label={`Semana del ${etiquetaDiaLarga(dias[0] ?? "")}`} aria-readonly="false" className="min-w-[56rem]">
            <div role="row" className="grid grid-cols-[12rem_repeat(7,minmax(0,1fr))]">
                <span role="columnheader" className="px-2 py-1 text-xs font-medium text-muted-foreground">
                    Técnico
                </span>
                {dias.map((d) => (
                    <span key={d} role="columnheader" className="border-l border-border px-2 py-1 text-xs font-medium text-muted-foreground tabular-nums">
                        {etiquetaDia(d)}
                    </span>
                ))}
            </div>
            {comun.filas.map((f) => {
                const total = dias.reduce((n, d) => n + citasDelDia(comun.calendario.citas ?? [], f.usuarioId, d).length, 0);
                return (
                    <div key={f.usuarioId} role="row" data-fila-tecnico={f.usuarioId} className="grid grid-cols-[12rem_repeat(7,minmax(0,1fr))] border-t border-border">
                        <div role="rowheader">
                            <CabeceraFila fila={f} comun={comun} detalle={detalleFila(f, comun, total)} />
                        </div>
                        {dias.map((d) => (
                            <CeldaSemana key={d} fila={f} dia={d} comun={comun} />
                        ))}
                    </div>
                );
            })}
        </div>
    );
}

/** En el teléfono: lista por día y técnico; «Mover a…» reemplaza el arrastre. */
function ListaTelefono({ dias, comun }: { dias: Dia[]; comun: Comun }) {
    const { puede } = useSesion();
    return (
        <div className="grid gap-5 [&>*]:min-w-0">
            {dias.map((d) => (
                <section key={d} aria-label={etiquetaDiaLarga(d)} className="grid gap-2 [&>*]:min-w-0">
                    {dias.length > 1 ? <h3 className="text-sm font-semibold first-letter:uppercase">{etiquetaDiaLarga(d)}</h3> : null}
                    {comun.filas.map((f) => {
                        const citas = citasDelDia(comun.calendario.citas ?? [], f.usuarioId, d);
                        const ausencias = ausenciasDelDia(comun.calendario.ausencias ?? [], f.usuarioId, d);
                        return (
                            <div key={f.usuarioId} className="grid gap-1.5 rounded-lg border border-border p-2.5 [&>*]:min-w-0" data-fila-tecnico={f.usuarioId}>
                                <div className="flex flex-wrap items-center gap-x-2">
                                    <UserRound aria-hidden className="size-4 text-muted-foreground" />
                                    <span className="text-sm font-medium">{f.nombre}</span>
                                    <span className="ml-auto text-xs text-muted-foreground">{detalleFila(f, comun, citas.length)}</span>
                                </div>
                                {ausencias.map((a) => (
                                    <BloqueAusencia key={a.id} a={a} className="h-7" />
                                ))}
                                {citas.length === 0 && ausencias.length === 0 ? <p className="text-xs text-muted-foreground">Sin citas</p> : null}
                                <ul className="grid gap-1.5 [&>*]:min-w-0">
                                    {citas.map((c) => {
                                        const p = ESTADOS_ORDEN[c.estado];
                                        return (
                                            <li key={c.ordenId} data-cita={c.numero} data-estado={c.estado} className="flex flex-wrap items-center gap-x-2 gap-y-1 rounded-md bg-elevated px-2 py-1.5 text-sm">
                                                <span className="font-medium tabular-nums">
                                                    {horaDe(c.inicio)}–{horaDe(c.fin)}
                                                </span>
                                                <Link href={`/ordenes/${c.ordenId}`} className="font-medium text-primary underline-offset-4 hover:underline">
                                                    {c.numero}
                                                </Link>
                                                <InsigniaEstado tono={p.tono} icono={p.icono} texto={p.texto} />
                                                <span className="w-full min-w-0 truncate text-xs text-muted-foreground">
                                                    {TIPOS_ORDEN[c.tipo].texto} · {c.direccion || "Sin dirección"}
                                                    {c.ventanaInicio && c.ventanaFin ? ` · ventana ${horaDe(c.ventanaInicio)}–${horaDe(c.ventanaFin)}` : ""}
                                                    {c.enSitioEn ? ` · llegó ${horaDe(c.enSitioEn)}` : ""}
                                                </span>
                                                {sePuedeMover(c.estado, puede) ? (
                                                    <Boton variante="secundario" tamano="sm" className="ml-auto" aria-label={`Mover a… ${c.numero}`} onClick={() => comun.alMover(movibleDeCita(c))}>
                                                        <MoveRight />
                                                        Mover a…
                                                    </Boton>
                                                ) : null}
                                            </li>
                                        );
                                    })}
                                </ul>
                            </div>
                        );
                    })}
                </section>
            ))}
        </div>
    );
}

export type PropsCalendario = Comun & { vista: Vista; desde: Dia };

/** Calendario por técnico: día (línea de tiempo) o semana; en el teléfono, lista con «Mover a…». */
export function CalendarioTecnicos({ vista, desde, ...comun }: PropsCalendario) {
    const dias = vista === "dia" ? [desde] : diasDesde(desde, 7);
    if (comun.filas.length === 0) {
        return (
            <EstadoVacio
                icono={CalendarX2}
                titulo="No hay técnicos en este filtro"
                descripcion="Cambia la cuadrilla o la zona, o registra la ficha de agenda de los técnicos."
            />
        );
    }
    return (
        <>
            <div className="hidden overflow-x-auto md:block" data-testid="rejilla">
                {vista === "dia" ? <RejillaDia dia={desde} comun={comun} /> : <RejillaSemana dias={dias} comun={comun} />}
            </div>
            <div className="md:hidden" data-testid="lista-telefono">
                <ListaTelefono dias={dias} comun={comun} />
            </div>
        </>
    );
}

/** Órdenes pendientes sin programar: se arrastran al calendario o se programan con «Mover a…». */
export function BandejaSinProgramar({
    ordenes,
    cortada,
    zonas,
    alMover,
    arrastre,
    setArrastre,
}: {
    ordenes: readonly Orden[];
    cortada: boolean;
    zonas: readonly Zona[] | undefined;
    alMover: (m: Movible) => void;
    arrastre: Movible | null;
    setArrastre: (m: Movible | null) => void;
}) {
    const { puede } = useSesion();
    const [todas, setTodas] = useState(false);
    if (ordenes.length === 0) {
        return <p className="text-sm text-muted-foreground">No hay órdenes pendientes sin programar.</p>;
    }
    const visibles = todas ? ordenes : ordenes.slice(0, VISIBLES_BANDEJA);
    return (
        <>
            <ul className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3 [&>*]:min-w-0" aria-label="Órdenes sin programar">
                {visibles.map((o) => {
                    const m = movibleDeOrden(o);
                    const movible = sePuedeMover(o.estado, puede);
                    const Tipo = TIPOS_ORDEN[o.tipo].icono;
                    return (
                        <li
                            key={o.id}
                            data-sin-programar={o.numero}
                            {...propsArrastre(m, movible, setArrastre)}
                            className={cn(
                                "flex items-center gap-2 rounded-lg border border-border bg-elevated p-2 text-sm",
                                movible && "cursor-grab active:cursor-grabbing",
                                arrastre?.ordenId === o.id && "opacity-50",
                            )}
                        >
                            {movible ? <GripVertical aria-hidden className="hidden size-4 shrink-0 text-muted-foreground md:block" /> : null}
                            <Tipo aria-hidden className="size-4 shrink-0 text-muted-foreground" />
                            <div className="grid min-w-0 flex-1">
                                <Link href={`/ordenes/${o.id}`} className="font-medium text-primary underline-offset-4 hover:underline">
                                    {o.numero}
                                </Link>
                                <span className="truncate text-xs text-muted-foreground">
                                    {TIPOS_ORDEN[o.tipo].texto} · {nombreDe(zonas, o.zonaId, "Zona") ?? "Sin zona"} · {o.direccion || "Sin dirección"}
                                </span>
                            </div>
                            {movible ? (
                                <Boton variante="secundario" tamano="sm" aria-label={`Programar ${o.numero}`} onClick={() => alMover(m)}>
                                    <MoveRight />
                                    Programar…
                                </Boton>
                            ) : null}
                        </li>
                    );
                })}
            </ul>
            {ordenes.length > VISIBLES_BANDEJA ? (
                <Boton variante="enlace" tamano="sm" className="justify-self-start" onClick={() => setTodas((t) => !t)}>
                    {todas ? "Ver menos" : `Ver las ${ordenes.length}`}
                </Boton>
            ) : null}
            {cortada ? (
                <Aviso tono="info" titulo="Hay más órdenes pendientes" className="mt-3">
                    Se muestran las {ordenes.length} más recientes; el resto está en el tablero de órdenes.
                </Aviso>
            ) : null}
        </>
    );
}

/** Tarjeta envolvente con título para secciones del despacho. */
export function Seccion({ titulo, descripcion, acciones, children, className, ...props }: { titulo: string; descripcion?: ReactNode; acciones?: ReactNode; children: ReactNode; className?: string; "data-testid"?: string }) {
    return (
        <Tarjeta className={cn("grid content-start gap-3 p-4 [&>*]:min-w-0", className)} {...props}>
            <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="grid gap-0.5">
                    <h2 className="text-base font-semibold tracking-[-0.015em]">{titulo}</h2>
                    {descripcion ? <p className="text-sm text-muted-foreground">{descripcion}</p> : null}
                </div>
                {acciones}
            </div>
            {children}
        </Tarjeta>
    );
}
