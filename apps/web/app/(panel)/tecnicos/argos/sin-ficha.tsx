"use client";

import type { Schema } from "@iris/contratos";
import { Aviso, Boton, Esqueleto, EstadoVacio, Tarjeta } from "@iris/ui";
import { CircleAlert, CircleCheck, Plus, RotateCw, Unplug, UserRound, Warehouse } from "lucide-react";
import Link from "next/link";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { PieLista } from "@/componentes/pie-lista";
import { EnlaceConectorArgos } from "@/componentes/equipo/enlace-conector-argos";
import { bodegaEnEmpresa, nombreBodega, rutaCrearFicha, SIN_INVENTARIO_POR_TECNICO, sinListaDeArgos } from "@/lib/agenda/argos";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { usePaginado } from "@/lib/paginado";
import { useSesion } from "@/lib/sesion";

type TecnicoArgos = Schema<"TecnicoDeArgos">;

const LIMITE = 50;

/**
 * `/tecnicos/argos`: técnicos de Argos con bodega que aún no tienen ficha en Iris
 * (`GET /integraciones/argosisp/tecnicos?sinVincular=true`). «Crear ficha» abre el alta con su
 * bodega elegida; quien solo tiene bodega en otra empresa se muestra sin el botón.
 */
export function TecnicosArgosSinFicha() {
    const { puede, yo } = useSesion();
    const empresa = yo.empresaActiva?.id ?? null;
    const paginado = usePaginado<TecnicoArgos>((cursor, signal) =>
        resolver(
            api().GET("/api/v1/integraciones/argosisp/tecnicos", {
                params: { query: { limit: LIMITE, sinVincular: true, ...(cursor ? { cursor } : {}) } },
                ...(signal ? { signal } : {}),
            }),
        ),
    );
    const { primera } = paginado;
    return (
        <>
            <EncabezadoPagina
                titulo="Técnicos de Argos sin ficha"
                descripcion="Usuarios de Argos con bodega técnico que aún no tienen ficha de agenda en Iris. Crea su ficha para programarles órdenes y descontar de su bodega."
            />
            {primera.estado === "cargando" ? (
                <div className="grid gap-3 lg:grid-cols-2" aria-busy="true" aria-label="Cargando técnicos de Argos">
                    <Esqueleto className="h-28" />
                    <Esqueleto className="h-28" />
                </div>
            ) : primera.estado === "error" ? (
                primera.error instanceof ErrorApi && primera.error.codigo === "argos_sin_inventario_tecnico" ? (
                    <Aviso tono="warning" titulo={SIN_INVENTARIO_POR_TECNICO} icono={Unplug}>
                        Actualiza Argos a 1.6.0 o más para ver sus técnicos con bodega.
                        <span className="block">
                            <EnlaceConectorArgos />
                        </span>
                    </Aviso>
                ) : sinListaDeArgos(primera.error) ? (
                    <Aviso
                        tono="error"
                        titulo="No se pudo leer la lista de Argos"
                        accion={
                            <Boton variante="secundario" tamano="sm" onClick={primera.reintentar}>
                                <RotateCw />
                                Reintentar
                            </Boton>
                        }
                    >
                        {[mensajeDeError(primera.error).titulo, mensajeDeError(primera.error).detalle].filter(Boolean).join(". ")}
                    </Aviso>
                ) : (
                    <ErrorLectura error={primera.error} que="los técnicos de Argos" reintentar={primera.reintentar} />
                )
            ) : paginado.items.length === 0 ? (
                <Tarjeta>
                    <EstadoVacio
                        icono={CircleCheck}
                        titulo="Todos los técnicos de Argos tienen ficha"
                        descripcion="Cuando Argos tenga un usuario nuevo con bodega técnico, aparecerá aquí."
                    />
                </Tarjeta>
            ) : (
                <div className="grid gap-3">
                    <ul className="grid gap-3 lg:grid-cols-2" aria-label="Técnicos de Argos sin ficha">
                        {paginado.items.map((a) => (
                            <li key={a.idExterno} className="min-w-0">
                                <TarjetaArgos tecnico={a} empresa={empresa} puedeCrear={puede(["agenda.crear_tecnico"])} />
                            </li>
                        ))}
                    </ul>
                    <PieLista paginado={paginado} que="técnicos de Argos" />
                </div>
            )}
        </>
    );
}

function TarjetaArgos({ tecnico: a, empresa, puedeCrear }: { tecnico: TecnicoArgos; empresa: number | null; puedeCrear: boolean }) {
    const elegible = a.enEmpresa && bodegaEnEmpresa(a, empresa) !== undefined;
    return (
        <Tarjeta className="grid h-full gap-3 p-4 [&>*]:min-w-0" data-argos={a.idExterno}>
            <div className="flex min-w-0 items-center gap-2">
                <UserRound aria-hidden className="size-5 shrink-0 text-muted-foreground" />
                <div className="grid min-w-0">
                    <span className="truncate font-semibold">{a.nombre}</span>
                    <span className="truncate text-sm text-muted-foreground">{a.correo}</span>
                </div>
            </div>
            <ul className="grid gap-1 text-sm" aria-label={`Bodegas de ${a.nombre}`}>
                {(a.bodegas ?? []).map((b) => (
                    <li key={b.id} className="flex min-w-0 items-center gap-2">
                        <Warehouse aria-hidden className="size-4 shrink-0 text-muted-foreground" />
                        <span className="truncate">{nombreBodega(b)}</span>
                        {b.activa ? null : <span className="text-xs text-muted-foreground">(inactiva)</span>}
                    </li>
                ))}
            </ul>
            {elegible ? (
                puedeCrear ? (
                    <Boton asChild tamano="sm" className="justify-self-start">
                        <Link href={rutaCrearFicha(a)} aria-label={`Crear ficha de ${a.nombre}`}>
                            <Plus />
                            Crear ficha
                        </Link>
                    </Boton>
                ) : null
            ) : (
                <p className="flex items-center gap-1.5 text-sm text-warning">
                    <CircleAlert aria-hidden className="size-4 shrink-0" />
                    Sin bodega activa en esta empresa: cambia de empresa o dásela en Argos.
                </p>
            )}
        </Tarjeta>
    );
}
