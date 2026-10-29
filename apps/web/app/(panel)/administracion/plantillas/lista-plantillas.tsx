"use client";

import { Boton, Casilla, Esqueleto, EstadoVacio, Etiqueta, Selector, Tabla, TablaCelda, TablaCuerpo, TablaEncabezado, TablaFila, TablaTitulo, Tarjeta } from "@iris/ui";
import { ArrowLeft, ClipboardList, Plus } from "lucide-react";
import Link from "next/link";
import { useId, useState } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { PieLista } from "@/componentes/pie-lista";
import { InsigniasPlantilla, textoTipoPlantilla } from "@/componentes/plantillas/insignias";
import { api, resolver } from "@/lib/api/cliente";
import { usePaginado } from "@/lib/paginado";
import { TIPOS_ORDEN } from "@/lib/ordenes/presentacion";
import type { Plantilla, TipoOrdenPlantilla } from "@/lib/plantillas/esquemas";
import { useSesion } from "@/lib/sesion";

const TAMANO_PAGINA = 25;

/** Plantillas de formulario: filtro por tipo de orden y activas o todas, con cursor. */
export function ListaPlantillas() {
    const { puede } = useSesion();
    const id = useId();
    const [tipo, setTipo] = useState<TipoOrdenPlantilla | "">("");
    const [todas, setTodas] = useState(false);
    return (
        <>
            <Link href="/administracion" className="mb-3 inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground">
                <ArrowLeft aria-hidden className="size-4" />
                Administración
            </Link>
            <EncabezadoPagina
                titulo="Plantillas de formulario"
                descripcion="Checklists que el técnico llena en cada orden. Editar una plantilla que ya se usó crea una versión nueva."
                acciones={
                    puede(["plantillas.crear"]) ? (
                        <Boton asChild>
                            <Link href="/administracion/plantillas/nueva">
                                <Plus />
                                Nueva plantilla
                            </Link>
                        </Boton>
                    ) : undefined
                }
            />
            <div className="mb-4 flex flex-wrap items-end gap-4" role="group" aria-label="Filtros">
                <div className="grid min-w-48 gap-1.5">
                    <Etiqueta htmlFor={`${id}-tipo`}>Tipo de orden</Etiqueta>
                    <Selector id={`${id}-tipo`} value={tipo} onChange={(e) => setTipo(e.target.value as TipoOrdenPlantilla | "")}>
                        <option value="">Todos</option>
                        {(Object.keys(TIPOS_ORDEN) as TipoOrdenPlantilla[]).map((t) => (
                            <option key={t} value={t}>
                                {TIPOS_ORDEN[t].texto}
                            </option>
                        ))}
                    </Selector>
                </div>
                <Casilla etiqueta="Incluir las inactivas" checked={todas} onChange={(e) => setTodas(e.target.checked)} className="pb-2" />
            </div>
            <Resultados key={`${tipo}|${todas}`} tipo={tipo} todas={todas} />
        </>
    );
}

function Resultados({ tipo, todas }: { tipo: TipoOrdenPlantilla | ""; todas: boolean }) {
    // Paginada por nombre (`nextCursor`): «Cargar más» como el resto de las listas.
    const paginado = usePaginado<Plantilla>((cursor, signal) =>
        resolver(
            api().GET("/api/v1/plantillas", {
                params: { query: { limit: TAMANO_PAGINA, ...(tipo ? { tipo } : {}), ...(todas ? { todas: true } : {}), ...(cursor ? { cursor } : {}) } },
                ...(signal ? { signal } : {}),
            }),
        ),
    );
    const lectura = paginado.primera;
    if (lectura.estado === "cargando") {
        return (
            <Tarjeta className="grid gap-3 p-4" aria-busy="true">
                <span className="sr-only" role="status">
                    Cargando plantillas…
                </span>
                {Array.from({ length: 3 }, (_, i) => (
                    <Esqueleto key={i} className="h-12" />
                ))}
            </Tarjeta>
        );
    }
    if (lectura.estado === "error") return <ErrorLectura error={lectura.error} que="las plantillas" reintentar={lectura.reintentar} />;
    const lista = paginado.items;
    if (lista.length === 0) {
        return (
            <Tarjeta>
                <EstadoVacio icono={ClipboardList} titulo="No hay plantillas con estos filtros" descripcion={todas ? undefined : "Prueba incluir las inactivas."} />
            </Tarjeta>
        );
    }
    return (
        <>
            <ul className="grid gap-2 sm:hidden" aria-label="Plantillas">
                {lista.map((p) => (
                    <li key={p.id}>
                        <Link href={`/administracion/plantillas/${p.id}`} className="grid gap-1.5 rounded-lg border bg-background p-3 text-sm hover:border-primary/60" data-plantilla={p.codigo}>
                            <span className="font-medium">{p.nombre}</span>
                            <span className="font-mono text-xs text-muted-foreground">
                                {p.codigo} · v{p.versionActual} · {textoTipoPlantilla(p.tipo)}
                            </span>
                            <InsigniasPlantilla p={p} />
                        </Link>
                    </li>
                ))}
            </ul>
            <Tarjeta className="hidden overflow-hidden sm:block">
                <Tabla aria-label="Plantillas">
                    <TablaEncabezado>
                        <TablaFila className="hover:bg-transparent">
                            <TablaTitulo>Plantilla</TablaTitulo>
                            <TablaTitulo>Tipo de orden</TablaTitulo>
                            <TablaTitulo>Versión</TablaTitulo>
                            <TablaTitulo>Estado</TablaTitulo>
                        </TablaFila>
                    </TablaEncabezado>
                    <TablaCuerpo>
                        {lista.map((p) => (
                            <TablaFila key={p.id} data-plantilla={p.codigo}>
                                <TablaCelda>
                                    <Link href={`/administracion/plantillas/${p.id}`} className="grid hover:underline">
                                        <span className="font-medium">{p.nombre}</span>
                                        <span className="font-mono text-xs text-muted-foreground">{p.codigo}</span>
                                    </Link>
                                </TablaCelda>
                                <TablaCelda className="text-muted-foreground">{textoTipoPlantilla(p.tipo)}</TablaCelda>
                                <TablaCelda className="tabular-nums">v{p.versionActual}</TablaCelda>
                                <TablaCelda>
                                    <InsigniasPlantilla p={p} />
                                </TablaCelda>
                            </TablaFila>
                        ))}
                    </TablaCuerpo>
                </Tabla>
            </Tarjeta>
            <div className="mt-4 grid justify-items-center">
                <PieLista paginado={paginado} que="plantillas" />
            </div>
        </>
    );
}

