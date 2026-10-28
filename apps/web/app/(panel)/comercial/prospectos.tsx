"use client";

import { Esqueleto, EstadoVacio, Tabla, TablaCelda, TablaCuerpo, TablaEncabezado, TablaFila, TablaTitulo, Tarjeta } from "@iris/ui";
import { MapPin, SearchX, UserRound } from "lucide-react";
import Link from "next/link";
import { InsigniaProspecto, nombreZona, type Zona } from "@/componentes/comercial/comun";
import { ErrorLectura } from "@/componentes/error-lectura";
import { PieLista } from "@/componentes/pie-lista";
import { nombreUsuario, type Usuario } from "@/componentes/tickets/catalogos";
import { api, resolver } from "@/lib/api/cliente";
import { queryDe, textoConteo, type Filtros } from "@/lib/comercial/filtros";
import { ESTADOS_PROSPECTO, ORDEN_ESTADOS, ORIGENES, type EstadoProspecto, type Prospecto } from "@/lib/comercial/presentacion";
import { formatearFecha } from "@/lib/fechas";
import { usePaginado } from "@/lib/paginado";
import { useSesion } from "@/lib/sesion";

export type Catalogos = { zonas: readonly Zona[] | null; usuarios: readonly Usuario[] | null };

function usePagina(filtros: Filtros, limite: number, estado?: EstadoProspecto) {
    const { yo } = useSesion();
    const query = queryDe(filtros, yo.usuario.id, estado ? { estado } : {});
    return usePaginado<Prospecto>((cursor, signal) =>
        resolver(api().GET("/api/v1/prospectos", { params: { query: { ...query, limit: limite, ...(cursor ? { cursor } : {}) } }, ...(signal ? { signal } : {}) })),
    );
}

function Vendedor({ id, usuarios }: { id: number | null; usuarios: readonly Usuario[] | null }) {
    const { yo } = useSesion();
    const nombre = nombreUsuario(usuarios, id, { id: yo.usuario.id, nombre: yo.usuario.nombre });
    return (
        <span className="inline-flex min-w-0 items-center gap-1 text-muted-foreground">
            <UserRound aria-hidden className="size-3.5 shrink-0" />
            <span className="truncate">{nombre ?? "Sin vendedor"}</span>
        </span>
    );
}

/** Tarjeta de un prospecto (columna del embudo y lista del teléfono). */
export function TarjetaProspecto({ p, catalogos, conEstado }: { p: Prospecto; catalogos: Catalogos; conEstado?: boolean }) {
    const zona = nombreZona(catalogos.zonas, p.zonaId);
    return (
        <Link
            href={`/comercial/${p.id}`}
            className="grid gap-1.5 rounded-lg border bg-background p-3 text-sm transition-colors hover:border-primary/60 focus-visible:outline-2"
            data-prospecto={p.numero}
        >
            <span className="flex items-start justify-between gap-2">
                <span className="grid min-w-0">
                    <span className="truncate font-medium">{p.nombre}</span>
                    <span className="font-mono text-xs text-muted-foreground tabular-nums">{p.numero}</span>
                </span>
                {conEstado ? <InsigniaProspecto estado={p.estado} /> : null}
            </span>
            <span className="line-clamp-2 text-muted-foreground">{p.direccion}</span>
            <span className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
                <Vendedor id={p.vendedorId} usuarios={catalogos.usuarios} />
                {zona ? (
                    <span className="inline-flex items-center gap-1 text-muted-foreground">
                        <MapPin aria-hidden className="size-3.5" />
                        {zona}
                    </span>
                ) : null}
            </span>
        </Link>
    );
}

const LIMITE_COLUMNA = 20;

/** Embudo: una columna por estado, cada una con su cursor y su conteo («20+» si hay más). */
export function Embudo({ filtros, catalogos }: { filtros: Filtros; catalogos: Catalogos }) {
    return (
        <div className="-mx-1 overflow-x-auto px-1 pb-2" role="region" aria-label="Embudo comercial" tabIndex={0}>
            <div className="grid auto-cols-[minmax(15rem,1fr)] grid-flow-col gap-3">
                {ORDEN_ESTADOS.map((e) => (
                    <Columna key={e} estado={e} filtros={filtros} catalogos={catalogos} />
                ))}
            </div>
        </div>
    );
}

function Columna({ estado, filtros, catalogos }: { estado: EstadoProspecto; filtros: Filtros; catalogos: Catalogos }) {
    const paginado = usePagina(filtros, LIMITE_COLUMNA, estado);
    const { primera, items } = paginado;
    const p = ESTADOS_PROSPECTO[estado];
    return (
        <section className="grid content-start gap-2 rounded-lg bg-elevated/60 p-2" aria-label={`${p.texto}`} data-columna={estado}>
            <header className="flex items-center justify-between gap-2 px-1 py-1">
                <InsigniaProspecto estado={estado} />
                <span className="text-sm font-medium text-muted-foreground tabular-nums" data-testid="conteo" aria-label={primera.estado === "listo" ? `${textoConteo(items.length, paginado.siguiente !== null)} prospectos` : undefined}>
                    {primera.estado === "listo" ? textoConteo(items.length, paginado.siguiente !== null) : "—"}
                </span>
            </header>
            {primera.estado === "cargando" ? (
                <Esqueleto className="h-24" />
            ) : primera.estado === "error" ? (
                <ErrorLectura error={primera.error} que={`«${p.texto}»`} reintentar={primera.reintentar} />
            ) : items.length === 0 ? (
                <p className="px-1 py-3 text-center text-xs text-muted-foreground">Nada aquí</p>
            ) : (
                <ul className="grid gap-2">
                    {items.map((x) => (
                        <li key={x.id}>
                            <TarjetaProspecto p={x} catalogos={catalogos} />
                        </li>
                    ))}
                </ul>
            )}
            <PieLista paginado={paginado} que="prospectos" />
        </section>
    );
}

/** Lista con cursor: tabla en pantalla ancha, tarjetas en el teléfono. */
export function ListaProspectos({ filtros, catalogos, telefono }: { filtros: Filtros; catalogos: Catalogos; telefono: boolean }) {
    const paginado = usePagina(filtros, 25);
    const { primera, items } = paginado;
    if (primera.estado === "cargando") {
        return (
            <Tarjeta className="grid gap-3 p-4" aria-busy="true">
                <span className="sr-only" role="status">
                    Cargando prospectos…
                </span>
                {Array.from({ length: 4 }, (_, i) => (
                    <Esqueleto key={i} className="h-12" />
                ))}
            </Tarjeta>
        );
    }
    if (primera.estado === "error") {
        return <ErrorLectura error={primera.error} que="los prospectos" reintentar={primera.reintentar} />;
    }
    if (items.length === 0) {
        return (
            <Tarjeta>
                <EstadoVacio icono={SearchX} titulo="No hay prospectos con estos filtros" descripcion="Cambia los filtros o registra un prospecto nuevo." />
            </Tarjeta>
        );
    }
    return (
        <div className="grid gap-3">
            {telefono ? (
                <ul className="grid gap-2" aria-label="Prospectos">
                    {items.map((p) => (
                        <li key={p.id}>
                            <TarjetaProspecto p={p} catalogos={catalogos} conEstado />
                        </li>
                    ))}
                </ul>
            ) : (
                <Tarjeta className="overflow-hidden">
                    <Tabla aria-label="Prospectos">
                        <TablaEncabezado>
                            <TablaFila className="hover:bg-transparent">
                                <TablaTitulo>Prospecto</TablaTitulo>
                                <TablaTitulo>Estado</TablaTitulo>
                                <TablaTitulo>Vendedor</TablaTitulo>
                                <TablaTitulo>Zona</TablaTitulo>
                                <TablaTitulo>Origen</TablaTitulo>
                                <TablaTitulo>Creado</TablaTitulo>
                            </TablaFila>
                        </TablaEncabezado>
                        <TablaCuerpo>
                            {items.map((p) => (
                                <TablaFila key={p.id} data-prospecto={p.numero}>
                                    <TablaCelda>
                                        <Link href={`/comercial/${p.id}`} className="grid min-w-0 hover:underline">
                                            <span className="font-medium">{p.nombre}</span>
                                            <span className="text-xs text-muted-foreground">
                                                <span className="font-mono tabular-nums">{p.numero}</span> · {p.direccion}
                                            </span>
                                        </Link>
                                    </TablaCelda>
                                    <TablaCelda>
                                        <InsigniaProspecto estado={p.estado} />
                                    </TablaCelda>
                                    <TablaCelda className="max-w-48">
                                        <Vendedor id={p.vendedorId} usuarios={catalogos.usuarios} />
                                    </TablaCelda>
                                    <TablaCelda className="text-muted-foreground">{nombreZona(catalogos.zonas, p.zonaId) ?? "—"}</TablaCelda>
                                    <TablaCelda className="text-muted-foreground">{ORIGENES[p.origen]}</TablaCelda>
                                    <TablaCelda className="text-muted-foreground tabular-nums">{formatearFecha(p.creadoEn)}</TablaCelda>
                                </TablaFila>
                            ))}
                        </TablaCuerpo>
                    </Tabla>
                </Tarjeta>
            )}
            <PieLista paginado={paginado} que="prospectos" />
        </div>
    );
}
