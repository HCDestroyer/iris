"use client";

import { Boton, cn, Esqueleto, EstadoVacio, InsigniaEstado, Tarjeta } from "@iris/ui";
import { Bot, CalendarClock, MessageSquareText, Phone, Settings2 } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { api, resolver } from "@/lib/api/cliente";
import { CLASE_PILDORA, formatearNumero, nombreUso, tonoDeCanal } from "@/lib/conversaciones/presentacion";
import { resumirFranjas } from "@/lib/horarios/franjas";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import type { Schema } from "@iris/contratos";

type Canal = Schema<"NumeroConversaciones">;

/** Lo que pasa fuera de horario, en una línea («Mensaje y guardia para fallas masivas»). */
export function textoFueraDeHorario(f: Canal["fueraDeHorario"]): string {
    if (!f) return "Nada: los mensajes esperan al abrir";
    const partes: string[] = [];
    if (f.responder) partes.push("Mensaje automático");
    if (f.guardia?.activa) partes.push(f.guardia.fallaMasiva ? "guardia para fallas masivas" : "guardia por etiqueta");
    if (partes.length === 0) return "Nada: los mensajes esperan al abrir";
    const texto = partes.join(" y ");
    return texto.charAt(0).toUpperCase() + texto.slice(1);
}

/**
 * «Números de WhatsApp» (Administración): cada número con sus usos, la cola de las conversaciones
 * nuevas, el horario y qué pasa fuera de horario. «Configurar» abre su pantalla propia.
 */
export function ListaNumeros() {
    const { yo } = useSesion();
    const lectura = useLectura(async (signal) => {
        const [canales, colas, horarios] = await Promise.all([
            resolver(api().GET("/api/v1/mensajeria/conversaciones/canales", { signal })),
            resolver(api().GET("/api/v1/mensajeria/colas", { signal })),
            resolver(api().GET("/api/v1/mensajeria/horarios", { signal })),
        ]);
        return { canales: canales.data ?? [], colas: colas.data ?? [], horarios: horarios.data ?? [] };
    });
    const empresa = yo.empresaActiva?.nombre;

    let contenido: ReactNode;
    if (lectura.estado === "cargando") {
        contenido = (
            <div className="grid gap-2" aria-busy="true">
                <span className="sr-only" role="status">
                    Cargando números…
                </span>
                <Esqueleto className="h-20" />
                <Esqueleto className="h-20" />
            </div>
        );
    } else if (lectura.estado === "error") {
        contenido = <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="los números" />;
    } else if (lectura.datos.canales.length === 0) {
        contenido = (
            <Tarjeta>
                <EstadoVacio icono={Phone} titulo="Aún no hay números de WhatsApp" descripcion="Un número se conecta con la API oficial de Meta; luego eliges su uso, su cola y su horario." />
            </Tarjeta>
        );
    } else {
        const { canales, colas, horarios } = lectura.datos;
        contenido = (
            <div className="overflow-hidden rounded-xl border border-border bg-card">
                <div
                    aria-hidden
                    className="hidden grid-cols-[minmax(12rem,1.4fr)_minmax(8rem,1fr)_minmax(6rem,0.8fr)_minmax(9rem,1.1fr)_minmax(10rem,1.3fr)_auto] gap-4 border-b border-border px-4 py-2.5 text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase lg:grid"
                >
                    <span>Número</span>
                    <span>Uso</span>
                    <span>Cola</span>
                    <span>Horario</span>
                    <span>Fuera de horario</span>
                    <span className="w-28 text-right">Asistente</span>
                </div>
                <ul aria-label="Números de WhatsApp">
                    {canales.map((c) => {
                        const horario = horarios.find((h) => h.id === c.horarioId);
                        const cola = colas.find((x) => x.id === c.colaId);
                        return (
                            <li
                                key={c.id}
                                data-numero={c.id}
                                className="grid gap-x-4 gap-y-2 border-b border-border px-4 py-3 last:border-b-0 lg:grid-cols-[minmax(12rem,1.4fr)_minmax(8rem,1fr)_minmax(6rem,0.8fr)_minmax(9rem,1.1fr)_minmax(10rem,1.3fr)_auto] lg:items-center"
                            >
                                <div className="grid min-w-0 gap-0.5">
                                    <span className="flex min-w-0 flex-wrap items-center gap-1.5">
                                        <span className="truncate font-semibold">{c.nombre}</span>
                                        {c.avisosEmpresa ? <span className="inline-flex h-5 items-center rounded-full bg-info/10 px-2 text-[0.6875rem] font-semibold whitespace-nowrap text-info">Avisos de la empresa</span> : null}
                                        {!c.activo ? <InsigniaEstado tono="neutral" texto="Inactivo" /> : null}
                                    </span>
                                    <span className="truncate font-mono text-xs text-muted-foreground">{formatearNumero(c.numero)}</span>
                                </div>
                                <Celda etiqueta="Uso">
                                    <span className="flex flex-wrap gap-1">
                                        {(c.usos ?? []).map((u) => (
                                            <span key={u} className={cn("inline-flex h-5 items-center rounded-full px-2 text-[0.6875rem] font-semibold", CLASE_PILDORA[tonoDeCanal({ usos: [u] })])}>
                                                {nombreUso(u)}
                                            </span>
                                        ))}
                                    </span>
                                </Celda>
                                <Celda etiqueta="Cola">{cola?.nombre ?? <span className="text-muted-foreground">Sin cola</span>}</Celda>
                                <Celda etiqueta="Horario" icono={CalendarClock}>
                                    {horario ? <span title={resumirFranjas(horario.franjas)}>{resumirFranjas(horario.franjas)}</span> : <span className="text-muted-foreground">Siempre abierto</span>}
                                </Celda>
                                <Celda etiqueta="Fuera de horario" icono={MessageSquareText}>
                                    <span className="text-muted-foreground">{textoFueraDeHorario(c.fueraDeHorario)}</span>
                                </Celda>
                                <div className="flex items-center justify-between gap-3 lg:w-28 lg:flex-col lg:items-end lg:gap-1">
                                    <span className="inline-flex items-center gap-1 text-xs whitespace-nowrap text-muted-foreground" title="Que atienda un asistente: disponible en la fase 2">
                                        <Bot aria-hidden className="size-3.5" />
                                        Fase 2
                                    </span>
                                    <Boton asChild variante="contorno" tamano="sm">
                                        <Link href={`/administracion/numeros/${c.id}/configurar`} aria-label={`Configurar ${c.nombre}`}>
                                            <Settings2 />
                                            Configurar
                                        </Link>
                                    </Boton>
                                </div>
                            </li>
                        );
                    })}
                </ul>
            </div>
        );
    }

    return (
        <>
            <EncabezadoPagina
                titulo="Números de WhatsApp"
                descripcion={
                    lectura.estado === "listo"
                        ? `${empresa ? `${empresa} · ` : ""}${lectura.datos.canales.length} ${lectura.datos.canales.length === 1 ? "número conectado" : "números conectados"} por la API oficial de Meta`
                        : "Números conectados por la API oficial de Meta"
                }
                acciones={
                    <Boton asChild variante="secundario">
                        <Link href="/administracion/horarios">
                            <CalendarClock />
                            Horarios
                        </Link>
                    </Boton>
                }
            />
            {contenido}
        </>
    );
}

/** Dato de la fila: en el teléfono con su etiqueta delante; en escritorio, en su columna. */
function Celda({ etiqueta, icono: Icono, children }: { etiqueta: string; icono?: typeof Phone; children: ReactNode }) {
    return (
        <div className="flex min-w-0 items-start gap-2 text-sm lg:block">
            <span className="w-28 shrink-0 text-xs text-muted-foreground lg:hidden">
                {Icono ? <Icono aria-hidden className="mr-1 inline size-3.5" /> : null}
                {etiqueta}
            </span>
            <span className="min-w-0 break-words">{children}</span>
        </div>
    );
}
