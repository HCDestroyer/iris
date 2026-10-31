"use client";

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
import { CalendarClock, ClipboardList, MapPin, RotateCw, UserRound } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";
import { ErrorLectura } from "@/componentes/error-lectura";
import type { PendientesPorOrden } from "@/componentes/ordenes/catalogos";
import { MarcaDatosArgos } from "@/componentes/ordenes/datos-argos";
import { InsigniaEstadoOrden, InsigniaTipoOrden, MarcaPendienteArgos } from "@/componentes/ordenes/insignias";
import { cambiosDeArgos } from "@/lib/ordenes/datos-argos";
import { PieLista } from "@/componentes/pie-lista";
import { nombreUsuario, type Usuario } from "@/componentes/tickets/catalogos";
import { useAhora } from "@/componentes/tickets/insignias";
import { api, resolver } from "@/lib/api/cliente";
import { formatearFecha, haceCuanto } from "@/lib/fechas";
import type { Lectura } from "@/lib/lectura";
import type { ConsultaOrdenes } from "@/lib/ordenes/filtros";
import { COLUMNAS, resumirPendientes, type Orden } from "@/lib/ordenes/presentacion";
import { usePaginado } from "@/lib/paginado";

const TAMANO_PAGINA = 30;

export type Vista = "lista" | "columnas";

type Props = {
    consulta: ConsultaOrdenes;
    vista: Vista;
    usuarios: readonly Usuario[] | null | undefined;
    pendientes: Lectura<PendientesPorOrden | null>;
    yo: { id: number; nombre: string };
    hayFiltros: boolean;
};

/** Órdenes con cursor, en tabla/tarjetas o por columnas de estado. Se remonta (key) al cambiar los filtros. */
export function ListaOrdenes({ consulta, vista, usuarios, pendientes, yo, hayFiltros }: Props) {
    const ahora = useAhora();
    const paginado = usePaginado<Orden>((cursor, signal) =>
        resolver(
            api().GET("/api/v1/ordenes", {
                params: { query: { ...consulta, limit: TAMANO_PAGINA, ...(cursor ? { cursor } : {}) } },
                ...(signal ? { signal } : {}),
            }),
        ),
    );
    const { primera, items } = paginado;

    if (primera.estado === "cargando") {
        return (
            <Tarjeta className="grid gap-3 p-5" aria-busy="true">
                <span className="sr-only" role="status">
                    Cargando órdenes…
                </span>
                {Array.from({ length: 5 }, (_, i) => (
                    <Esqueleto key={i} className="h-12" />
                ))}
            </Tarjeta>
        );
    }
    if (primera.estado === "error") {
        return <ErrorLectura error={primera.error} que="las órdenes" reintentar={primera.reintentar} />;
    }
    if (items.length === 0) {
        return (
            <Tarjeta>
                <EstadoVacio
                    icono={ClipboardList}
                    titulo={hayFiltros ? "Ninguna orden coincide con los filtros" : "No hay órdenes"}
                    descripcion={hayFiltros ? "Prueba quitando algún filtro." : "Cuando se creen órdenes de trabajo aparecerán aquí."}
                />
            </Tarjeta>
        );
    }

    // La marca solo se calcula sobre una lectura correcta de los pendientes; si falló, se dice.
    const mapa = pendientes.estado === "listo" ? pendientes.datos : null;
    const marca = (o: Orden) => {
        const lista = mapa?.porOrden.get(String(o.id));
        const resumen = lista ? resumirPendientes(lista) : undefined;
        const cambios = cambiosDeArgos(o);
        if (!resumen && !cambios) return null;
        return (
            <>
                {resumen ? <MarcaPendienteArgos resumen={resumen} /> : null}
                {cambios ? <MarcaDatosArgos cambios={cambios} /> : null}
            </>
        );
    };
    const tecnico = (o: Orden) => nombreUsuario(usuarios, o.tecnicoId, yo);
    const props: PropsTarjeta = { marca, tecnico, ahora };

    return (
        <>
            {pendientes.estado === "error" ? (
                <Aviso
                    tono="warning"
                    titulo="No se pudo leer el estado de los pendientes en Argos"
                    className="mb-3"
                    accion={
                        <Boton variante="secundario" tamano="sm" onClick={pendientes.reintentar}>
                            <RotateCw />
                            Reintentar
                        </Boton>
                    }
                >
                    Las órdenes se muestran sin la marca «Pendiente en Argos» hasta que se pueda leer.
                </Aviso>
            ) : mapa?.parcial ? (
                <p className="mb-3 text-sm text-muted-foreground">Hay muchos pendientes en Argos: la marca se muestra para los 200 más recientes de cada estado.</p>
            ) : null}

            {vista === "columnas" ? <Columnas ordenes={items} completo={!paginado.siguiente} {...props} /> : <Lista ordenes={items} {...props} />}

            <div className="mt-4 grid justify-items-center gap-3">
                <PieLista paginado={paginado} que="órdenes" />
                {!paginado.siguiente && !paginado.errorMas ? <p className="text-sm text-muted-foreground">No hay más órdenes.</p> : null}
            </div>
        </>
    );
}

type PropsTarjeta = {
    marca: (o: Orden) => ReactNode;
    tecnico: (o: Orden) => string | undefined;
    ahora: number;
};

function Programada({ orden }: { orden: Orden }) {
    const inicio = formatearFecha(orden.programadaInicio);
    if (!inicio) return <span className="text-muted-foreground">Sin programar</span>;
    const fin = formatearFecha(orden.programadaFin);
    return (
        <span className="tabular-nums">
            {inicio}
            {fin ? <span className="text-muted-foreground"> – {fin.slice(-5)}</span> : null}
        </span>
    );
}

/** Tarjeta de una orden (teléfono y columnas). */
function TarjetaOrden({ orden: o, marca, tecnico }: PropsTarjeta & { orden: Orden }) {
    return (
        <Link href={`/ordenes/${o.id}`} className="grid gap-2 p-4 transition-colors hover:bg-background-secondary/60" data-orden={o.numero}>
            <span className="flex flex-wrap items-center justify-between gap-2">
                <span className="font-mono text-xs whitespace-nowrap text-muted-foreground">{o.numero}</span>
                <InsigniaTipoOrden tipo={o.tipo} />
            </span>
            <span className="flex flex-wrap items-center gap-1.5">
                <InsigniaEstadoOrden estado={o.estado} />
                {marca(o)}
            </span>
            {o.direccion ? (
                <span className="flex items-start gap-1.5 text-sm">
                    <MapPin aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                    <span className="line-clamp-2">{o.direccion}</span>
                </span>
            ) : null}
            <span className="flex items-center gap-1.5 text-sm">
                <CalendarClock aria-hidden className="size-4 shrink-0 text-muted-foreground" />
                <Programada orden={o} />
            </span>
            <span className="flex items-center gap-1.5 text-sm text-muted-foreground">
                <UserRound aria-hidden className="size-4 shrink-0" />
                {tecnico(o) ?? "Sin técnico"}
            </span>
        </Link>
    );
}

function Lista({ ordenes, ...props }: PropsTarjeta & { ordenes: readonly Orden[] }) {
    const { marca, tecnico, ahora } = props;
    return (
        <Tarjeta className="overflow-hidden">
            {/* Teléfono: tarjetas */}
            <ul className="divide-y md:hidden" aria-label="Órdenes">
                {ordenes.map((o) => (
                    <li key={o.id}>
                        <TarjetaOrden orden={o} {...props} />
                    </li>
                ))}
            </ul>
            {/* Escritorio: tabla */}
            <div className="hidden md:block">
                <Tabla aria-label="Órdenes">
                    <TablaEncabezado>
                        <TablaFila className="hover:bg-transparent">
                            <TablaTitulo>Orden</TablaTitulo>
                            <TablaTitulo>Tipo</TablaTitulo>
                            <TablaTitulo>Estado</TablaTitulo>
                            <TablaTitulo>Programada</TablaTitulo>
                            <TablaTitulo>Técnico</TablaTitulo>
                            <TablaTitulo className="text-right">Actualizada</TablaTitulo>
                        </TablaFila>
                    </TablaEncabezado>
                    <TablaCuerpo>
                        {ordenes.map((o) => (
                            <TablaFila key={o.id} data-orden={o.numero}>
                                <TablaCelda className="w-[32%] max-w-[20rem] min-w-[13rem]">
                                    <Link href={`/ordenes/${o.id}`} className="grid min-w-0 gap-0.5 rounded-sm hover:text-primary">
                                        <span className="font-mono text-xs text-muted-foreground">{o.numero}</span>
                                        <span className="truncate font-medium">{o.direccion || "Sin dirección"}</span>
                                        {o.contacto.nombre ? <span className="truncate text-xs text-muted-foreground">{o.contacto.nombre}</span> : null}
                                    </Link>
                                </TablaCelda>
                                <TablaCelda>
                                    <InsigniaTipoOrden tipo={o.tipo} />
                                </TablaCelda>
                                <TablaCelda>
                                    <span className="flex flex-wrap items-center gap-1.5">
                                        <InsigniaEstadoOrden estado={o.estado} />
                                        {marca(o)}
                                    </span>
                                </TablaCelda>
                                <TablaCelda className="text-sm whitespace-nowrap">
                                    <Programada orden={o} />
                                </TablaCelda>
                                <TablaCelda className="max-w-[12rem] truncate text-muted-foreground">{tecnico(o) ?? "Sin técnico"}</TablaCelda>
                                <TablaCelda className="text-right whitespace-nowrap text-muted-foreground">{haceCuanto(o.actualizadoEn, ahora)}</TablaCelda>
                            </TablaFila>
                        ))}
                    </TablaCuerpo>
                </Tabla>
            </div>
        </Tarjeta>
    );
}

/** Vista por columnas de estado: en el teléfono, una debajo de otra; en escritorio, lado a lado. */
function Columnas({ ordenes, completo, ...props }: PropsTarjeta & { ordenes: readonly Orden[]; completo: boolean }) {
    return (
        <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-5" aria-label="Órdenes por estado" role="region">
            {COLUMNAS.map((c) => {
                const de = ordenes.filter((o) => c.estados.includes(o.estado));
                return (
                    <section key={c.id} aria-labelledby={`col-${c.id}`} data-columna={c.id} className="grid min-w-0 content-start gap-2">
                        <h2 id={`col-${c.id}`} className="flex items-center justify-between gap-2 px-1 text-sm font-semibold">
                            {c.titulo}
                            <span className="text-xs font-normal text-muted-foreground tabular-nums" title={completo ? undefined : "Hay más órdenes por cargar"}>
                                {completo ? de.length : `${de.length}+`}
                            </span>
                        </h2>
                        {de.length === 0 ? (
                            <p className="rounded-[var(--hx-radius-card)] border border-dashed p-4 text-center text-sm text-muted-foreground">Sin órdenes</p>
                        ) : (
                            <ul className="grid gap-2">
                                {de.map((o) => (
                                    <li key={o.id}>
                                        <Tarjeta className="overflow-hidden">
                                            <TarjetaOrden orden={o} {...props} />
                                        </Tarjeta>
                                    </li>
                                ))}
                            </ul>
                        )}
                    </section>
                );
            })}
        </div>
    );
}
