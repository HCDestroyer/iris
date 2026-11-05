"use client";

import type { Schema } from "@iris/contratos";
import { Boton, cn, Esqueleto, InsigniaEstado, Tarjeta } from "@iris/ui";
import { ChevronRight, ClipboardList, Inbox, type LucideIcon } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";
import { Bloque, claseEnlaceBloque } from "@/componentes/bloque";
import { ErrorLectura } from "@/componentes/error-lectura";
import { useAhora } from "@/componentes/tickets/insignias";
import { api, resolver } from "@/lib/api/cliente";
import type { Calendario, Tecnico } from "@/lib/despacho/calendario";
import { diaDesdeIso, diasDesde, etiquetaDiaLarga, hoyGuatemala, rangoUtc, sumarDias, type Dia } from "@/lib/despacho/fechas";
import { antiguedadAhora, type UltimasPosiciones } from "@/lib/despacho/posiciones";
import { haceCuanto } from "@/lib/fechas";
import { citasPorDia, equipoDeHoy, ESTADOS_TECNICO, puntosTendencia } from "@/lib/inicio/equipo";
import { useLectura, type Lectura } from "@/lib/lectura";
import { leerSLA } from "@/lib/reportes/api";
import { menuVisible } from "@/lib/menu";
import type { Orden } from "@/lib/ordenes/presentacion";
import { useSesion } from "@/lib/sesion";
import { slaCumplido, useResumenTickets, type DiaTickets } from "@/lib/tickets/resumen";
import { describirSla, type Ticket } from "@/lib/tickets/sla";

/** Órdenes sin técnico que se leen para «Necesita tu atención» (si hay más, «50 o más»). */
const LIMITE_SIN_TECNICO = 50;

const sinLectura = <T,>(): Lectura<T | null> => ({
    estado: "listo",
    datos: null,
    reintentar: () => undefined,
    refrescar: async () => undefined,
    reemplazar: () => undefined,
});

/**
 * Inicio del panel (nuevo diseño): cuatro indicadores con su tendencia de 7 días, «Necesita tu
 * atención», «Equipo hoy», «Tickets por día» y «SLA por categoría». Cada bloque sale de un
 * endpoint que ya existe y solo aparece con su permiso; si su lectura falla, el bloque muestra el
 * error con «Reintentar» y nunca un número calculado sobre datos que faltan.
 */
export function Inicio() {
    const { yo, puede } = useSesion();
    const ahora = useAhora(60_000);
    const hoy = hoyGuatemala(ahora);
    const desde = sumarDias(hoy, -6);
    const dias = diasDesde(desde, 7);
    const resumen = useResumenTickets();
    const veAgenda = puede(["agenda.ver"]);
    const veOrdenes = puede(["ordenes.ver"]);
    const vePosiciones = puede(["campo.ver_posiciones"]);
    const veReportes = puede(["reportes.ver"]);

    const agenda = useLectura<Calendario | null>((signal) =>
        veAgenda ? resolver(api().GET("/api/v1/agenda", { params: { query: rangoUtc(desde, 7) }, signal })) : Promise.resolve(null),
    );
    const tecnicos = useLectura<Tecnico[] | null>(async (signal) =>
        veAgenda ? ((await resolver(api().GET("/api/v1/tecnicos", { params: { query: { activo: "true", limit: 200 } }, signal }))).data ?? []) : null,
    );
    const posiciones = useLectura<UltimasPosiciones | null>((signal) =>
        vePosiciones ? resolver(api().GET("/api/v1/campo/posiciones/ultimas", { signal })) : Promise.resolve(null),
    );
    const sinTecnico = useLectura<{ ordenes: Orden[]; cortada: boolean } | null>(async (signal) => {
        if (!veOrdenes) return null;
        const p = await resolver(
            api().GET("/api/v1/ordenes", { params: { query: { sinTecnico: true, estado: ["pendiente", "programada"], limit: LIMITE_SIN_TECNICO } }, signal }),
        );
        return { ordenes: p.data ?? [], cortada: p.nextCursor !== null };
    });
    const sla = useLectura<Schema<"ReporteSLA"> | null>((signal) => (veReportes ? leerSLA({ desde, hasta: hoy }, signal) : Promise.resolve(null)));

    const nombre = yo.usuario.nombre.split(" ")[0] ?? yo.usuario.nombre;
    const hayBloques = resumen !== null || veAgenda || veOrdenes || veReportes;
    const actualizado = resumen?.estado === "listo" ? resumen.datos.calculadoEn : null;

    return (
        <div className="grid gap-4 [&>*]:min-w-0">
            <div className="flex flex-wrap items-end justify-between gap-3">
                <div className="grid gap-1">
                    <h1 className="text-[1.375rem] font-semibold tracking-[-0.015em]">Hola, {nombre}</h1>
                    <p className="text-sm text-muted-foreground first-letter:uppercase">
                        {etiquetaDiaLarga(hoy)}
                        {actualizado ? ` · actualizado ${haceCuanto(actualizado, ahora)}` : ""}
                    </p>
                </div>
            </div>

            {hayBloques ? (
                <>
                    <Indicadores
                        resumen={resumen}
                        agenda={veAgenda ? agenda : null}
                        sinTecnico={veOrdenes ? sinTecnico : null}
                        dias={dias}
                        hoy={hoy}
                        reportes={veReportes}
                    />
                    <div className="grid gap-4 [&>*]:min-w-0 xl:grid-cols-[minmax(0,1.6fr)_minmax(20rem,1fr)]">
                        <Atencion resumen={resumen} sinTecnico={veOrdenes ? sinTecnico : null} ahora={ahora} />
                        {veAgenda ? (
                            <EquipoHoy agenda={agenda} tecnicos={tecnicos} posiciones={vePosiciones ? posiciones : sinLectura()} hoy={hoy} ahora={ahora} />
                        ) : null}
                    </div>
                    <div className="grid gap-4 [&>*]:min-w-0 lg:grid-cols-2">
                        {resumen ? <TicketsPorDia resumen={resumen} /> : null}
                        {veReportes ? <SlaPorCategoria sla={sla} /> : null}
                    </div>
                </>
            ) : (
                <Accesos />
            )}
        </div>
    );
}

/* ---------------------------------------------------------------- Indicadores */

function Tendencia({ valores, etiqueta }: { valores: readonly number[]; etiqueta: string }) {
    return (
        <svg width="96" height="32" viewBox="0 0 96 32" role="img" aria-label={etiqueta} className="hidden shrink-0 overflow-visible text-primary sm:block">
            <polyline points={puntosTendencia(valores)} fill="none" stroke="currentColor" strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" />
        </svg>
    );
}

type Indicador = {
    id: string;
    nombre: string;
    href: string;
    lectura: "cargando" | "error" | "listo";
    reintentar?: () => void;
    valor?: string;
    nota?: { texto: string; tono: "bien" | "atencion" | "neutro" };
    detalle?: string;
    tendencia?: number[];
    textoTendencia?: string;
};

function Indicadores({
    resumen,
    agenda,
    sinTecnico,
    dias,
    hoy,
    reportes,
}: {
    resumen: ReturnType<typeof useResumenTickets>;
    agenda: Lectura<Calendario | null> | null;
    sinTecnico: Lectura<{ ordenes: Orden[]; cortada: boolean } | null> | null;
    dias: readonly Dia[];
    hoy: Dia;
    reportes: boolean;
}) {
    const lista: Indicador[] = [];
    if (resumen) {
        const r = resumen.estado === "listo" ? resumen.datos : null;
        const base = { lectura: resumen.estado, reintentar: resumen.reintentar } as const;
        const diasR = r?.dias ?? [];
        const creadosHoy = diasR.at(-1)?.creados ?? 0;
        lista.push({
            id: "abiertos",
            nombre: "Tickets activos",
            href: "/tickets",
            ...base,
            ...(r
                ? {
                      valor: String(r.activos),
                      nota: { texto: `+${creadosHoy} hoy`, tono: creadosHoy > 0 ? "atencion" : "neutro" },
                      detalle: `${r.mios} tuyos · ${r.sinAsignar} sin asignar`,
                      tendencia: diasR.map((d) => d.creados),
                      textoTendencia: `Tickets nuevos por día en 7 días: ${diasR.map((d) => d.creados).join(", ")}`,
                  }
                : {}),
        });
        lista.push({
            id: "sla",
            nombre: "SLA en riesgo",
            href: "/tickets",
            ...base,
            ...(r
                ? {
                      valor: String(r.slaEnRiesgo + r.slaVencidos),
                      nota: { texto: r.slaVenceHoy === 1 ? "1 vence hoy" : `${r.slaVenceHoy} vencen hoy`, tono: r.slaVenceHoy > 0 ? "atencion" : "neutro" },
                      detalle: r.slaVencidos === 1 ? "1 vencido" : `${r.slaVencidos} vencidos`,
                      tendencia: diasR.map((d) => d.vencidos),
                      textoTendencia: `SLA vencidos por día en 7 días: ${diasR.map((d) => d.vencidos).join(", ")}`,
                  }
                : {}),
        });
    }
    if (agenda) {
        const datos = agenda.estado === "listo" ? agenda.datos : null;
        // §10: con la agenda cortada no hay total que mostrar.
        const cortada = datos?.truncado === true;
        const citas = datos?.citas ?? [];
        const porDia = citasPorDia(citas, dias);
        const cerradas = citas.filter((c) => c.estado === "cerrada" && diaDesdeIso(c.inicio) === hoy).length;
        const st = sinTecnico?.estado === "listo" ? sinTecnico.datos : null;
        lista.push({
            id: "ordenes",
            nombre: "Órdenes de hoy",
            href: "/agenda?vista=dia",
            lectura: agenda.estado,
            reintentar: agenda.reintentar,
            ...(datos && !cortada
                ? {
                      valor: String(porDia.at(-1) ?? 0),
                      nota: { texto: cerradas === 1 ? "1 cerrada" : `${cerradas} cerradas`, tono: "bien" },
                      ...(st ? { detalle: `${st.cortada ? `${st.ordenes.length} o más` : st.ordenes.length} sin técnico asignado` } : {}),
                      tendencia: porDia,
                      textoTendencia: `Órdenes por día en 7 días: ${porDia.join(", ")}`,
                  }
                : datos && cortada
                  ? { valor: "—", detalle: "Demasiadas citas: filtra la agenda para ver el total" }
                  : {}),
        });
    }
    if (resumen) {
        const r = resumen.estado === "listo" ? resumen.datos : null;
        const diasR = r?.dias ?? [];
        const total = r ? slaCumplido(diasR) : null;
        const porDia = r ? diasR.map((d) => (d.resueltosConSla > 0 ? Math.round((d.resueltosEnSla / d.resueltosConSla) * 100) : null)) : [];
        const serie = porDia.filter((v): v is number => v !== null);
        const conSla = r ? diasR.reduce((n, d) => n + d.resueltosConSla, 0) : 0;
        lista.push({
            id: "cumplido",
            nombre: "SLA cumplido · 7 días",
            href: reportes ? "/reportes" : "/tickets?vista=todos",
            lectura: resumen.estado,
            reintentar: resumen.reintentar,
            ...(r
                ? {
                      valor: total === null ? "—" : `${total} %`,
                      detalle: total === null ? "Sin tickets resueltos con SLA" : `${conSla} ${conSla === 1 ? "resuelto" : "resueltos"} con SLA`,
                      ...(serie.length > 1 ? { tendencia: serie, textoTendencia: `% de SLA cumplido por día: ${serie.join(", ")}` } : {}),
                  }
                : {}),
        });
    }
    if (lista.length === 0) return null;
    return (
        <section aria-label="Indicadores" className="grid grid-cols-2 gap-3 xl:grid-cols-4">
            {lista.map((k) => (
                <TarjetaIndicador key={k.id} indicador={k} />
            ))}
        </section>
    );
}

function TarjetaIndicador({ indicador: k }: { indicador: Indicador }) {
    if (k.lectura === "error") {
        return (
            <div className="grid content-start gap-2 rounded-xl border border-border bg-card px-4 py-3.5" data-indicador={k.id}>
                <span className="text-[0.8125rem] font-medium text-muted-foreground">{k.nombre}</span>
                <p className="text-sm text-error">No se pudo leer.</p>
                {k.reintentar ? (
                    <Boton variante="secundario" tamano="sm" className="justify-self-start" onClick={k.reintentar}>
                        Reintentar
                    </Boton>
                ) : null}
            </div>
        );
    }
    return (
        <Link
            href={k.href}
            data-indicador={k.id}
            aria-label={k.valor !== undefined ? `${k.nombre}: ${k.valor}. ${k.detalle ?? ""}` : k.nombre}
            className="grid content-start gap-2 rounded-xl border border-border bg-card px-4 py-3.5 transition-colors hover:border-primary/50"
        >
            <span className="flex flex-wrap items-center justify-between gap-x-2 gap-y-1 text-[0.8125rem] font-medium text-muted-foreground">
                {k.nombre}
                {k.nota ? (
                    <span
                        className={cn(
                            "inline-flex h-5 shrink-0 items-center rounded-md px-1.5 text-[0.6875rem] font-semibold whitespace-nowrap",
                            k.nota.tono === "bien"
                                ? "bg-success/10 text-success"
                                : k.nota.tono === "atencion"
                                  ? "bg-warning/10 text-warning"
                                  : "bg-elevated text-muted-foreground",
                        )}
                    >
                        {k.nota.texto}
                    </span>
                ) : null}
            </span>
            {k.lectura === "cargando" || k.valor === undefined ? (
                <Esqueleto className="h-8 w-20" />
            ) : (
                <span className="flex items-end justify-between gap-2">
                    <span className="text-[1.75rem] leading-none font-semibold tracking-[-0.02em] tabular-nums">{k.valor}</span>
                    {k.tendencia && k.tendencia.length > 1 ? <Tendencia valores={k.tendencia} etiqueta={k.textoTendencia ?? ""} /> : null}
                </span>
            )}
            {k.detalle ? <span className="text-xs text-muted-foreground">{k.detalle}</span> : null}
        </Link>
    );
}

/* ---------------------------------------------------------------- Necesita tu atención */

type Pendiente = {
    id: string;
    letra: ReactNode;
    tono: "error" | "warning" | "info";
    titulo: string;
    detalle: string;
    tiempo?: string;
    accion: string;
    href: string;
    primaria: boolean;
};

function Atencion({
    resumen,
    sinTecnico,
    ahora,
}: {
    resumen: ReturnType<typeof useResumenTickets>;
    sinTecnico: Lectura<{ ordenes: Orden[]; cortada: boolean } | null> | null;
    ahora: number;
}) {
    const { puede } = useSesion();
    if (!resumen && !sinTecnico) return null;
    const errores: ReactNode[] = [];
    const cargando = resumen?.estado === "cargando" || sinTecnico?.estado === "cargando";
    if (resumen?.estado === "error") errores.push(<ErrorLectura key="r" error={resumen.error} reintentar={resumen.reintentar} que="los tickets urgentes" />);
    if (sinTecnico?.estado === "error")
        errores.push(<ErrorLectura key="o" error={sinTecnico.error} reintentar={sinTecnico.reintentar} que="las órdenes sin técnico" />);

    const pendientes: Pendiente[] = [];
    if (resumen?.estado === "listo") {
        for (const t of resumen.datos.urgentes ?? []) pendientes.push(pendienteDeTicket(t, ahora, puede(["tickets.asignar"])));
    }
    if (sinTecnico?.estado === "listo" && sinTecnico.datos && sinTecnico.datos.ordenes.length > 0) {
        const { ordenes, cortada } = sinTecnico.datos;
        const n = cortada ? `${ordenes.length} o más` : String(ordenes.length);
        pendientes.push({
            id: "sin-tecnico",
            letra: <ClipboardList aria-hidden className="size-4" />,
            tono: "warning",
            titulo: ordenes.length === 1 && !cortada ? "1 orden sin técnico" : `${n} órdenes sin técnico`,
            detalle: ordenes
                .slice(0, 3)
                .map((o) => o.numero)
                .join(" · "),
            accion: "Programar",
            href: "/agenda?vista=dia",
            primaria: false,
        });
    }
    const vacio = !cargando && errores.length === 0 && pendientes.length === 0;
    return (
        <Bloque
            aria-label="Necesita tu atención"
            titulo="Necesita tu atención"
            contador={cargando ? undefined : pendientes.length}
            accion={
                resumen ? (
                    <Link href="/tickets" className={claseEnlaceBloque}>
                        Ver todo
                    </Link>
                ) : null
            }
        >
            {errores.length > 0 ? <div className="grid gap-2 p-4">{errores}</div> : null}
            {cargando ? (
                <div className="grid gap-2 p-4">
                    <Esqueleto className="h-10" />
                    <Esqueleto className="h-10" />
                </div>
            ) : vacio ? (
                <p className="flex items-center gap-2 px-4 py-6 text-sm text-muted-foreground">
                    <Inbox aria-hidden className="size-4" />
                    Nada urgente: ningún SLA en riesgo ni orden sin técnico.
                </p>
            ) : (
                <ul aria-label="Pendientes" className="divide-y divide-border">
                    {pendientes.map((p) => (
                        <li key={p.id} className="flex items-center gap-3 px-4 py-3" data-pendiente={p.id}>
                            <span
                                aria-hidden
                                className={cn(
                                    "grid size-8 shrink-0 place-items-center rounded-lg text-[0.8125rem] font-bold",
                                    p.tono === "error"
                                        ? "bg-error/10 text-error"
                                        : p.tono === "warning"
                                          ? "bg-warning/10 text-warning"
                                          : "bg-info/10 text-info",
                                )}
                            >
                                {p.letra}
                            </span>
                            <span className="grid min-w-0 flex-1 gap-0.5">
                                <span className="truncate text-sm font-semibold">{p.titulo}</span>
                                <span className="truncate text-[0.8125rem] text-muted-foreground">{p.detalle}</span>
                            </span>
                            {p.tiempo ? (
                                <span
                                    className={cn(
                                        "hidden shrink-0 font-mono text-xs font-semibold sm:inline",
                                        p.tono === "error" ? "text-error" : "text-warning",
                                    )}
                                >
                                    {p.tiempo}
                                </span>
                            ) : null}
                            <Boton asChild variante={p.primaria ? "primario" : "secundario"} tamano="sm" className="shrink-0 rounded-lg text-xs font-semibold">
                                <Link href={p.href} aria-label={`${p.accion}: ${p.titulo}`}>
                                    {p.accion}
                                </Link>
                            </Boton>
                        </li>
                    ))}
                </ul>
            )}
        </Bloque>
    );
}

function pendienteDeTicket(t: Ticket, ahora: number, puedeAsignar: boolean): Pendiente {
    const d = describirSla(t.slaResolucion, ahora);
    const vencido = d.estado === "vencido";
    const asignar = puedeAsignar && t.asignadoA === null;
    return {
        id: `ticket-${t.id}`,
        letra: vencido ? "!" : "S",
        tono: vencido ? "error" : "warning",
        titulo: `${t.numero} · ${t.asunto}`,
        detalle: [
            t.contacto.nombre,
            d.detalle ? `SLA ${d.texto.toLowerCase()} · ${d.detalle}` : `SLA ${d.texto.toLowerCase()}`,
            t.asignadoA === null ? "sin asignar" : null,
        ]
            .filter(Boolean)
            .join(" · "),
        tiempo: d.texto,
        accion: asignar ? "Asignar" : "Abrir",
        href: asignar ? `/tickets/${t.id}?accion=asignar` : `/tickets/${t.id}`,
        primaria: true,
    };
}

/* ---------------------------------------------------------------- Equipo hoy */

function EquipoHoy({
    agenda,
    tecnicos,
    posiciones,
    hoy,
    ahora,
}: {
    agenda: Lectura<Calendario | null>;
    tecnicos: Lectura<Tecnico[] | null>;
    posiciones: Lectura<UltimasPosiciones | null>;
    hoy: Dia;
    ahora: number;
}) {
    const { puede } = useSesion();
    const error = agenda.estado === "error" ? agenda : tecnicos.estado === "error" ? tecnicos : posiciones.estado === "error" ? posiciones : null;
    const listo = agenda.estado === "listo" && tecnicos.estado === "listo" && posiciones.estado === "listo";
    const filas =
        listo && agenda.datos && tecnicos.datos
            ? equipoDeHoy(
                  tecnicos.datos,
                  agenda.datos.citas ?? [],
                  agenda.datos.ausencias ?? [],
                  posiciones.datos
                      ? (posiciones.datos.data ?? []).map((p) => ({
                            ...p,
                            antiguedad: antiguedadAhora(p, Date.parse(posiciones.datos?.servidorEn ?? "") || ahora, ahora),
                        }))
                      : null,
                  hoy,
                  ahora,
              )
            : [];
    const verMapa = puede(["agenda.ver", "campo.ver_posiciones"]);
    return (
        <Bloque
            aria-label="Equipo hoy"
            titulo="Equipo hoy"
            accion={
                verMapa ? (
                    <Link href="/mapa" className={claseEnlaceBloque}>
                        Ver en el mapa
                    </Link>
                ) : null
            }
        >
            {error && error.estado === "error" ? (
                <div className="p-4">
                    <ErrorLectura error={error.error} reintentar={error.reintentar} que="el equipo de hoy" />
                </div>
            ) : !listo ? (
                <div className="grid gap-2 p-4">
                    <Esqueleto className="h-9" />
                    <Esqueleto className="h-9" />
                </div>
            ) : agenda.estado === "listo" && agenda.datos?.truncado ? (
                <p className="p-4 text-sm text-muted-foreground">Hay demasiadas citas para resumir el día: ábrelo en la agenda.</p>
            ) : filas.length === 0 ? (
                <p className="px-4 py-6 text-sm text-muted-foreground">No hay técnicos activos.</p>
            ) : (
                <ul aria-label="Técnicos" className="grid gap-0.5 py-1.5">
                    {filas.slice(0, 8).map((f) => {
                        const e = ESTADOS_TECNICO[f.estado];
                        const pct = f.total > 0 ? Math.round((f.terminadas / f.total) * 100) : 0;
                        return (
                            <li
                                key={f.usuarioId}
                                className="grid grid-cols-[2rem_minmax(0,1fr)_auto] items-center gap-2.5 px-4 py-2"
                                data-tecnico={f.usuarioId}
                                data-estado-tecnico={f.estado}
                            >
                                <span aria-hidden className="grid size-8 place-items-center rounded-full bg-elevated text-[0.6875rem] font-semibold">
                                    {iniciales(f.nombre)}
                                </span>
                                <span className="grid min-w-0 gap-1.5">
                                    <span className="flex justify-between gap-2">
                                        <span className="truncate text-[0.8125rem] font-semibold">{f.nombre}</span>
                                        <span className="shrink-0 text-xs font-medium text-muted-foreground tabular-nums">
                                            {f.total > 0 ? `${f.terminadas} de ${f.total}` : "—"}
                                        </span>
                                    </span>
                                    <span
                                        role="progressbar"
                                        aria-label={`Avance de ${f.nombre}`}
                                        aria-valuemin={0}
                                        aria-valuemax={f.total}
                                        aria-valuenow={f.terminadas}
                                        className="h-1.5 overflow-hidden rounded-full bg-elevated"
                                    >
                                        <span className="block h-full rounded-full bg-primary" style={{ width: `${pct}%` }} />
                                    </span>
                                </span>
                                <InsigniaEstado tono={e.tono} icono={e.icono} texto={e.texto} />
                            </li>
                        );
                    })}
                </ul>
            )}
        </Bloque>
    );
}

function iniciales(nombre: string): string {
    const partes = nombre.trim().split(/\s+/).filter(Boolean);
    const letras = partes.length >= 2 ? [partes[0], partes[partes.length - 1]] : partes;
    return letras.map((p) => p?.[0]?.toUpperCase() ?? "").join("") || "?";
}

/* ---------------------------------------------------------------- Gráficos */

const DIAS_CORTOS = ["Dom", "Lun", "Mar", "Mié", "Jue", "Vie", "Sáb"];
const diaCorto = (dia: string) => DIAS_CORTOS[new Date(`${dia}T12:00:00Z`).getUTCDay()] ?? dia;

function TicketsPorDia({ resumen }: { resumen: NonNullable<ReturnType<typeof useResumenTickets>> }) {
    return (
        <Bloque aria-label="Tickets por día" cuerpo="grid gap-3 px-4 py-3.5">
            <div className="flex flex-wrap items-baseline justify-between gap-2">
                <h2 className="text-[0.9375rem] font-semibold">Tickets por día</h2>
                <span className="flex gap-3 text-xs font-medium text-muted-foreground">
                    <span className="inline-flex items-center gap-1.5">
                        <span aria-hidden className="size-2 rounded-[2px] bg-primary" />
                        Nuevos
                    </span>
                    <span className="inline-flex items-center gap-1.5">
                        <span aria-hidden className="size-2 rounded-[2px] bg-muted-foreground/60" />
                        Resueltos
                    </span>
                </span>
            </div>
            {resumen.estado === "error" ? (
                <ErrorLectura error={resumen.error} reintentar={resumen.reintentar} que="los tickets por día" />
            ) : resumen.estado === "cargando" ? (
                <Esqueleto className="h-36" />
            ) : (
                <BarrasPorDia dias={resumen.datos.dias ?? []} />
            )}
        </Bloque>
    );
}

function BarrasPorDia({ dias }: { dias: readonly DiaTickets[] }) {
    const max = Math.max(1, ...dias.flatMap((d) => [d.creados, d.resueltos]));
    const descripcion = dias.map((d) => `${diaCorto(d.dia)}: ${d.creados} nuevos y ${d.resueltos} resueltos`).join("; ");
    return (
        <>
            <div
                role="img"
                aria-label={`Últimos 7 días. ${descripcion}`}
                className="grid h-36 grid-cols-7 items-end gap-2 sm:gap-3.5"
                data-testid="barras-tickets"
            >
                {dias.map((d) => (
                    <div key={d.dia} className="flex h-full flex-col items-center justify-end gap-1.5">
                        <div className="flex h-28 items-end gap-[3px]">
                            <span
                                className="block w-2.5 rounded-t-[3px] bg-primary sm:w-3.5"
                                style={{ height: `${(d.creados / max) * 100}%` }}
                                title={`${d.creados} nuevos`}
                            />
                            <span
                                className="block w-2.5 rounded-t-[3px] bg-muted-foreground/60 sm:w-3.5"
                                style={{ height: `${(d.resueltos / max) * 100}%` }}
                                title={`${d.resueltos} resueltos`}
                            />
                        </div>
                        <span className="text-[0.6875rem] font-medium text-muted-foreground">{diaCorto(d.dia)}</span>
                    </div>
                ))}
            </div>
        </>
    );
}

function SlaPorCategoria({ sla }: { sla: Lectura<Schema<"ReporteSLA"> | null> }) {
    return (
        <Bloque aria-label="SLA por categoría" cuerpo="grid content-start gap-3 px-4 py-3.5">
            <h2 className="text-[0.9375rem] font-semibold">SLA cumplido por categoría · 7 días</h2>
            {sla.estado === "error" ? (
                <ErrorLectura error={sla.error} reintentar={sla.reintentar} que="el SLA por categoría" />
            ) : sla.estado === "cargando" || !sla.datos ? (
                <Esqueleto className="h-28" />
            ) : (sla.datos.porCategoria ?? []).filter((c) => c.porcentajeResolucion !== null).length === 0 ? (
                <p className="text-sm text-muted-foreground">Sin tickets resueltos con SLA en estos 7 días.</p>
            ) : (
                <ul className="grid gap-2.5" aria-label="Categorías">
                    {(sla.datos.porCategoria ?? [])
                        .filter((c): c is typeof c & { porcentajeResolucion: number } => c.porcentajeResolucion !== null)
                        .sort((a, b) => b.conSlaResolucion - a.conSlaResolucion)
                        .slice(0, 6)
                        .map((c) => {
                            const pct = Math.round(c.porcentajeResolucion);
                            return (
                                <li
                                    key={c.categoriaId ?? "sin"}
                                    className="grid grid-cols-[minmax(0,8.75rem)_minmax(0,1fr)_2.75rem] items-center gap-2.5 text-[0.8125rem] font-medium"
                                >
                                    <span className="truncate">{c.categoria ?? "Sin categoría"}</span>
                                    <span
                                        role="meter"
                                        aria-label={`SLA cumplido en ${c.categoria ?? "Sin categoría"}`}
                                        aria-valuemin={0}
                                        aria-valuemax={100}
                                        aria-valuenow={pct}
                                        className="h-2 overflow-hidden rounded-full bg-elevated"
                                    >
                                        <span className="block h-full rounded-full bg-primary" style={{ width: `${pct}%` }} />
                                    </span>
                                    <span className="text-right font-semibold tabular-nums">{pct} %</span>
                                </li>
                            );
                        })}
                </ul>
            )}
        </Bloque>
    );
}

/* ---------------------------------------------------------------- Sin bloques */

/** Sin permisos para ningún bloque: accesos directos a lo que sí puede abrir. */
function Accesos() {
    const { permisos } = useSesion();
    const modulos = menuVisible(permisos).filter((e) => e.href !== "/" && !e.fueraDelMenu);
    if (modulos.length === 0) return null;
    return (
        <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {modulos.map((m) => (
                <li key={m.href}>
                    <AccesoModulo href={m.href} etiqueta={m.etiqueta} icono={m.icono} />
                </li>
            ))}
        </ul>
    );
}

function AccesoModulo({ href, etiqueta, icono: Icono }: { href: string; etiqueta: string; icono: LucideIcon }) {
    return (
        <Tarjeta className="h-full rounded-xl transition-shadow duration-150 hover:shadow-[0_0_0_1px_var(--hx-primary)]">
            <Link href={href} className="flex h-full items-center gap-3 rounded-[inherit] p-4">
                <span className="grid size-10 shrink-0 place-items-center rounded-lg bg-primary/10 text-primary">
                    <Icono aria-hidden className="size-5" />
                </span>
                <span className="min-w-0 flex-1 font-medium">{etiqueta}</span>
                <ChevronRight aria-hidden className="size-4 text-dimmed" />
            </Link>
        </Tarjeta>
    );
}
