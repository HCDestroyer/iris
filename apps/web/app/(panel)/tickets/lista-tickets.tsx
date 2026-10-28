"use client";

import type { Schema } from "@iris/contratos";
import {
    Aviso,
    Boton,
    Esqueleto,
    EstadoVacio,
    Tabla,
    TablaCelda,
    TablaCuerpo,
    TablaEncabezado,
    TablaFila,
    TablaTitulo,
    Tarjeta,
} from "@iris/ui";
import { ChevronDown, Inbox, RotateCw, UserRound } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { ErrorLectura } from "@/componentes/error-lectura";
import { nombreCategoria, nombreUsuario, type Categoria, type Usuario } from "@/componentes/tickets/catalogos";
import { InsigniaEstadoTicket, InsigniaPrioridad, SlaVisible, useAhora } from "@/componentes/tickets/insignias";
import { api, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { haceCuanto } from "@/lib/fechas";
import { useLectura } from "@/lib/lectura";
import type { ConsultaTickets } from "@/lib/tickets/filtros";
import { relojPrincipal, type Ticket } from "@/lib/tickets/sla";

const TAMANO_PAGINA = 25;

function leerPagina(consulta: ConsultaTickets, cursor: string | undefined, signal?: AbortSignal): Promise<Schema<"PaginaTicket">> {
    return resolver(
        api().GET("/api/v1/tickets", {
            params: { query: { ...consulta, limit: TAMANO_PAGINA, ...(cursor ? { cursor } : {}) } },
            ...(signal ? { signal } : {}),
        }),
    );
}

type Props = {
    consulta: ConsultaTickets;
    categorias: readonly Categoria[] | undefined;
    usuarios: readonly Usuario[] | null | undefined;
    yo: { id: number; nombre: string };
    hayFiltros: boolean;
};

/** Lista de la bandeja con cursor. Se remonta (key) al cambiar los filtros. */
export function ListaTickets({ consulta, categorias, usuarios, yo, hayFiltros }: Props) {
    const primera = useLectura((signal) => leerPagina(consulta, undefined, signal));
    const [extra, setExtra] = useState<{ tickets: Ticket[]; cursor: string | null } | null>(null);
    const [cargandoMas, setCargandoMas] = useState(false);
    const [errorMas, setErrorMas] = useState<unknown>(null);
    const ahora = useAhora();

    if (primera.estado === "cargando") {
        return (
            <Tarjeta className="grid gap-3 p-5" aria-busy="true">
                <span className="sr-only" role="status">
                    Cargando tickets…
                </span>
                {Array.from({ length: 5 }, (_, i) => (
                    <Esqueleto key={i} className="h-12" />
                ))}
            </Tarjeta>
        );
    }
    if (primera.estado === "error") {
        return (
            <ErrorLectura
                error={primera.error}
                que="los tickets"
                reintentar={() => {
                    setExtra(null);
                    primera.reintentar();
                }}
            />
        );
    }

    const tickets = [...(primera.datos.data ?? []), ...(extra?.tickets ?? [])];
    const siguiente = extra ? extra.cursor : primera.datos.nextCursor;

    async function cargarMas() {
        if (!siguiente) {
            return;
        }
        setCargandoMas(true);
        setErrorMas(null);
        try {
            const pagina = await leerPagina(consulta, siguiente);
            setExtra((prev) => ({ tickets: [...(prev?.tickets ?? []), ...(pagina.data ?? [])], cursor: pagina.nextCursor }));
        } catch (error) {
            setErrorMas(error);
        } finally {
            setCargandoMas(false);
        }
    }

    if (tickets.length === 0) {
        return (
            <Tarjeta>
                <EstadoVacio
                    icono={Inbox}
                    titulo={hayFiltros ? "Ningún ticket coincide con los filtros" : "No hay tickets"}
                    descripcion={hayFiltros ? "Prueba con otra vista o quita algún filtro." : "Cuando entren tickets aparecerán aquí."}
                />
            </Tarjeta>
        );
    }

    const asignado = (t: Ticket) => nombreUsuario(usuarios, t.asignadoA, yo);

    return (
        <>
            <Tarjeta className="overflow-hidden">
                {/* Teléfono: tarjetas */}
                <ul className="divide-y md:hidden" aria-label="Tickets">
                    {tickets.map((t) => {
                        const { tipo, reloj } = relojPrincipal(t);
                        return (
                            <li key={t.id} data-ticket={t.numero}>
                                <Link href={`/tickets/${t.id}`} className="grid gap-2 p-4 transition-colors hover:bg-muted/60">
                                    <span className="flex items-baseline justify-between gap-2">
                                        <span className="font-mono text-xs text-muted-foreground">{t.numero}</span>
                                        <span className="text-xs text-muted-foreground">{haceCuanto(t.actualizadoEn, ahora)}</span>
                                    </span>
                                    <span className="line-clamp-2 font-medium">{t.asunto}</span>
                                    <span className="flex flex-wrap items-center gap-1.5">
                                        <InsigniaEstadoTicket estado={t.estado} />
                                        <InsigniaPrioridad prioridad={t.prioridad} />
                                    </span>
                                    <SlaVisible reloj={reloj} tipo={tipo} ahora={ahora} compacto />
                                    <span className="flex items-center gap-1.5 text-sm text-muted-foreground">
                                        <UserRound aria-hidden className="size-4" />
                                        {asignado(t) ?? "Sin asignar"}
                                        {t.categoriaId !== null ? ` · ${nombreCategoria(categorias, t.categoriaId)}` : null}
                                    </span>
                                </Link>
                            </li>
                        );
                    })}
                </ul>
                {/* Escritorio: tabla */}
                <div className="hidden md:block">
                    <Tabla aria-label="Tickets">
                        <TablaEncabezado>
                            <TablaFila className="hover:bg-transparent">
                                <TablaTitulo>Ticket</TablaTitulo>
                                <TablaTitulo>Estado</TablaTitulo>
                                <TablaTitulo>Prioridad</TablaTitulo>
                                <TablaTitulo>SLA</TablaTitulo>
                                <TablaTitulo>Asignado</TablaTitulo>
                                <TablaTitulo className="text-right">Actualizado</TablaTitulo>
                            </TablaFila>
                        </TablaEncabezado>
                        <TablaCuerpo>
                            {tickets.map((t) => {
                                const { tipo, reloj } = relojPrincipal(t);
                                return (
                                    <TablaFila key={t.id} data-ticket={t.numero}>
                                        <TablaCelda className="max-w-[22rem]">
                                            <Link href={`/tickets/${t.id}`} className="grid min-w-0 gap-0.5 rounded-sm hover:text-primary">
                                                <span className="font-mono text-xs text-muted-foreground">{t.numero}</span>
                                                <span className="truncate font-medium">{t.asunto}</span>
                                                {t.categoriaId !== null ? (
                                                    <span className="truncate text-xs text-muted-foreground">{nombreCategoria(categorias, t.categoriaId)}</span>
                                                ) : null}
                                            </Link>
                                        </TablaCelda>
                                        <TablaCelda>
                                            <InsigniaEstadoTicket estado={t.estado} />
                                        </TablaCelda>
                                        <TablaCelda>
                                            <InsigniaPrioridad prioridad={t.prioridad} />
                                        </TablaCelda>
                                        <TablaCelda>
                                            <SlaVisible reloj={reloj} tipo={tipo} ahora={ahora} compacto />
                                        </TablaCelda>
                                        <TablaCelda className="max-w-[12rem] truncate text-muted-foreground">{asignado(t) ?? "Sin asignar"}</TablaCelda>
                                        <TablaCelda className="text-right whitespace-nowrap text-muted-foreground">{haceCuanto(t.actualizadoEn, ahora)}</TablaCelda>
                                    </TablaFila>
                                );
                            })}
                        </TablaCuerpo>
                    </Tabla>
                </div>
            </Tarjeta>
            <div className="mt-4 grid justify-items-center gap-3">
                {errorMas ? (
                    <Aviso
                        tono="error"
                        titulo="No se pudo cargar la página siguiente"
                        className="w-full"
                        accion={
                            <Boton variante="secundario" tamano="sm" onClick={cargarMas}>
                                <RotateCw />
                                Reintentar
                            </Boton>
                        }
                    >
                        {mensajeDeError(errorMas).detalle}
                    </Aviso>
                ) : siguiente ? (
                    <Boton variante="contorno" onClick={cargarMas} disabled={cargandoMas}>
                        <ChevronDown />
                        {cargandoMas ? "Cargando…" : "Cargar más"}
                    </Boton>
                ) : (
                    <p className="text-sm text-muted-foreground">No hay más tickets.</p>
                )}
            </div>
        </>
    );
}
