"use client";

import { Boton, Esqueleto, EstadoVacio, FilaLista, ListaFilas, MenuAcciones, VistaRapida } from "@iris/ui";
import { ChevronDown, Eye, Handshake, MapPin, SearchX, SquareArrowOutUpRight, UserRound } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState, type ReactNode } from "react";
import Link from "next/link";
import { InsigniaProspecto, nombreZona, type Zona } from "@/componentes/comercial/comun";
import { ErrorLectura } from "@/componentes/error-lectura";
import { PieLista, piePaginado } from "@/componentes/pie-lista";
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

/** Tarjeta de un prospecto en una columna del embudo (la lista usa `ListaFilas`). */
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
/** Tarjetas visibles por columna antes de «Ver N más»: las filas del embudo quedan parejas. */
const VISIBLES_COLUMNA = 5;

/**
 * Embudo: una columna por estado, cada una con su cursor y su conteo («20+» si hay más). Las
 * columnas pasan a la fila siguiente cuando no caben (sin scroll horizontal) y cada una crece
 * con sus tarjetas (sin scroll propio): la página es lo único que se desplaza.
 */
export function Embudo({ filtros, catalogos }: { filtros: Filtros; catalogos: Catalogos }) {
    return (
        <div role="region" aria-label="Embudo comercial">
            <div className="grid grid-cols-[repeat(auto-fill,minmax(14rem,1fr))] items-start gap-3">
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
    const [expandida, setExpandida] = useState(false);
    const ocultas = expandida ? 0 : Math.max(0, items.length - VISIBLES_COLUMNA);
    const visibles = ocultas > 0 ? items.slice(0, VISIBLES_COLUMNA) : items;
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
                <ul className="grid gap-2" aria-label={`Prospectos en ${p.texto}`}>
                    {visibles.map((x) => (
                        <li key={x.id}>
                            <TarjetaProspecto p={x} catalogos={catalogos} />
                        </li>
                    ))}
                </ul>
            )}
            {ocultas > 0 ? (
                <Boton variante="fantasma" tamano="sm" className="justify-self-center" onClick={() => setExpandida(true)}>
                    <ChevronDown />
                    Ver {ocultas} más
                </Boton>
            ) : (
                <PieLista paginado={paginado} que="prospectos" />
            )}
        </section>
    );
}

/** Lista con cursor (`ListaFilas`): cada fila abre la ficha; el menú `⋯` tiene la vista rápida. */
export function ListaProspectos({ filtros, catalogos }: { filtros: Filtros; catalogos: Catalogos }) {
    const paginado = usePagina(filtros, 25);
    const { primera, items } = paginado;
    const router = useRouter();
    const { yo } = useSesion();
    const [vistaId, setVistaId] = useState<number | null>(null);
    const vendedor = (id: number | null) => nombreUsuario(catalogos.usuarios, id, { id: yo.usuario.id, nombre: yo.usuario.nombre }) ?? "Sin vendedor";
    if (primera.estado === "cargando") {
        return (
            <ListaFilas
                etiqueta="Prospectos"
                vacio={
                    <div className="grid gap-3" aria-busy="true">
                        <span className="sr-only" role="status">
                            Cargando prospectos…
                        </span>
                        {Array.from({ length: 4 }, (_, i) => (
                            <Esqueleto key={i} className="h-12" />
                        ))}
                    </div>
                }
            >
                {[]}
            </ListaFilas>
        );
    }
    if (primera.estado === "error") {
        return <ErrorLectura error={primera.error} que="los prospectos" reintentar={primera.reintentar} />;
    }
    const enVista = items.find((p) => p.id === vistaId) ?? null;
    return (
        <>
            <ListaFilas
                etiqueta="Prospectos"
                {...piePaginado(paginado, "prospectos")}
                vacio={<EstadoVacio icono={SearchX} titulo="No hay prospectos con estos filtros" descripcion="Cambia los filtros o registra un prospecto nuevo." />}
            >
                {items.map((p) => {
                    const zona = nombreZona(catalogos.zonas, p.zonaId);
                    return (
                        <FilaLista
                            key={p.id}
                            data-prospecto={p.numero}
                            icono={Handshake}
                            tonoIcono="marca"
                            titulo={p.nombre}
                            detalle={
                                <>
                                    <span className="font-mono tabular-nums">{p.numero}</span>
                                    {[p.direccion, vendedor(p.vendedorId), zona].filter(Boolean).map((parte) => ` · ${parte}`).join("")}
                                </>
                            }
                            href={`/comercial/${p.id}`}
                            activa={vistaId === p.id}
                            extremo={
                                <>
                                    <InsigniaProspecto estado={p.estado} />
                                    <span className="text-xs whitespace-nowrap text-muted-foreground tabular-nums">{formatearFecha(p.creadoEn)}</span>
                                </>
                            }
                            extremoTelefono="debajo"
                            menu={
                                <MenuAcciones
                                    etiqueta={`Acciones de ${p.numero}`}
                                    acciones={[
                                        { id: "vista", texto: "Vista rápida", icono: Eye, alElegir: () => setVistaId(p.id) },
                                        { id: "abrir", texto: "Abrir ficha", icono: SquareArrowOutUpRight, alElegir: () => router.push(`/comercial/${p.id}`) },
                                    ]}
                                />
                            }
                        />
                    );
                })}
            </ListaFilas>
            {enVista ? (
                <VistaProspecto
                    p={enVista}
                    datos={[
                        ["Dirección", enVista.direccion],
                        ["Vendedor", vendedor(enVista.vendedorId)],
                        ["Zona", nombreZona(catalogos.zonas, enVista.zonaId) ?? "—"],
                        ["Origen", ORIGENES[enVista.origen]],
                        ["Creado", formatearFecha(enVista.creadoEn) ?? "—"],
                    ]}
                    alCerrar={() => setVistaId(null)}
                />
            ) : null}
        </>
    );
}

/** Vista rápida de un prospecto. */
function VistaProspecto({ p, datos, alCerrar }: { p: Prospecto; datos: [string, ReactNode][]; alCerrar: () => void }) {
    return (
        <VistaRapida
            abierto
            alCerrar={alCerrar}
            titulo={p.nombre}
            codigo={p.numero}
            insignias={<InsigniaProspecto estado={p.estado} />}
            pie={
                <Boton asChild>
                    <Link href={`/comercial/${p.id}`}>
                        <SquareArrowOutUpRight />
                        Abrir ficha
                    </Link>
                </Boton>
            }
        >
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
                {datos.map(([etiqueta, valor]) => (
                    <div key={etiqueta} className="contents">
                        <dt className="text-muted-foreground">{etiqueta}</dt>
                        <dd className="min-w-0">{valor}</dd>
                    </div>
                ))}
            </dl>
        </VistaRapida>
    );
}
