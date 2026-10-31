"use client";

import { Boton, Campo, CampoSelector, Esqueleto, EstadoVacio, GrupoSegmentado, Tarjeta } from "@iris/ui";
import { HardHat, Map as IconoMapa, Pencil, Phone, Plus, UserRound, Users } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { AvisoCatalogos } from "@/componentes/despacho/catalogos";
import { useCatalogosAgenda } from "@/componentes/despacho/datos";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { CambiarEstado } from "@/componentes/equipo/cambiar-estado";
import { textoZonas } from "@/componentes/equipo/formulario-tecnico";
import { InsigniaActivo } from "@/componentes/equipo/insignias";
import { ErrorLectura } from "@/componentes/error-lectura";
import { PieLista } from "@/componentes/pie-lista";
import type { Tecnico } from "@/lib/agenda/esquemas";
import { PERMISOS_CUADRILLAS, PERMISOS_ZONAS } from "@/lib/agenda/permisos";
import { api, resolver } from "@/lib/api/cliente";
import type { Cuadrilla, Zona } from "@/lib/despacho/calendario";
import { useDiferido } from "@/lib/diferido";
import { usePaginado } from "@/lib/paginado";
import { useSesion } from "@/lib/sesion";

type Estado = "true" | "false" | "todos";
type Filtros = { q: string; cuadrillaId: string; activo: Estado };

const LIMITE = 50;

/** Fichas de técnicos: lista con filtros, alta, edición y activar/desactivar. */
export function PantallaTecnicos() {
    const { puede } = useSesion();
    const catalogos = useCatalogosAgenda();
    const [filtros, setFiltros] = useState<Filtros>({ q: "", cuadrillaId: "", activo: "true" });
    const q = useDiferido(filtros.q.trim(), 300);
    const [version, setVersion] = useState(0);
    const cuadrillas = catalogos.cuadrillas.estado === "listo" ? catalogos.cuadrillas.datos : undefined;
    const zonas = catalogos.zonas.estado === "listo" ? catalogos.zonas.datos : undefined;
    const clave = `${q}|${filtros.cuadrillaId}|${filtros.activo}|${version}`;

    return (
        <>
            <EncabezadoPagina
                titulo="Técnicos"
                descripcion="Fichas de agenda: cuadrilla (y con ella las zonas que atiende), contacto, turnos y ausencias."
                acciones={
                    <div className="flex flex-wrap gap-2">
                        {puede(PERMISOS_CUADRILLAS) ? (
                            <Boton asChild variante="secundario">
                                <Link href="/administracion/cuadrillas">
                                    <Users />
                                    Cuadrillas
                                </Link>
                            </Boton>
                        ) : null}
                        {puede(PERMISOS_ZONAS) ? (
                            <Boton asChild variante="secundario">
                                <Link href="/administracion/zonas">
                                    <IconoMapa />
                                    Zonas
                                </Link>
                            </Boton>
                        ) : null}
                        {puede(["agenda.crear_tecnico"]) ? (
                            <Boton asChild>
                                <Link href="/tecnicos/nuevo">
                                    <Plus />
                                    Nuevo técnico
                                </Link>
                            </Boton>
                        ) : null}
                    </div>
                }
            />
            <AvisoCatalogos {...catalogos} />
            <Tarjeta className="mb-4 grid gap-3 p-4 sm:grid-cols-[minmax(0,1fr)_14rem_auto] sm:items-end">
                <Campo etiqueta="Buscar" type="search" placeholder="Nombre o correo" value={filtros.q} onChange={(e) => setFiltros({ ...filtros, q: e.target.value })} />
                <CampoSelector
                    etiqueta="Cuadrilla"
                    value={filtros.cuadrillaId}
                    disabled={!cuadrillas}
                    onChange={(e) => setFiltros({ ...filtros, cuadrillaId: e.target.value })}
                >
                    <option value="">Todas</option>
                    {(cuadrillas ?? []).map((c) => (
                        <option key={c.id} value={c.id}>
                            {c.nombre}
                        </option>
                    ))}
                </CampoSelector>
                <GrupoSegmentado
                    etiqueta="Estado"
                    valor={filtros.activo}
                    alCambiar={(v) => setFiltros({ ...filtros, activo: v })}
                    opciones={[
                        { valor: "true", etiqueta: "Activos" },
                        { valor: "false", etiqueta: "Inactivos" },
                        { valor: "todos", etiqueta: "Todos" },
                    ]}
                />
            </Tarjeta>
            <Lista
                key={clave}
                q={q}
                cuadrillaId={filtros.cuadrillaId}
                activo={filtros.activo}
                cuadrillas={cuadrillas}
                zonas={zonas}
                alCambiar={() => setVersion((n) => n + 1)}
            />
        </>
    );
}

function Lista({
    q,
    cuadrillaId,
    activo,
    cuadrillas,
    zonas,
    alCambiar,
}: {
    q: string;
    cuadrillaId: string;
    activo: Estado;
    cuadrillas: readonly Cuadrilla[] | undefined;
    zonas: readonly Zona[] | undefined;
    alCambiar: () => void;
}) {
    const paginado = usePaginado<Tecnico>((cursor, signal) =>
        resolver(
            api().GET("/api/v1/tecnicos", {
                params: {
                    query: {
                        limit: LIMITE,
                        ...(cursor ? { cursor } : {}),
                        ...(q ? { q } : {}),
                        ...(cuadrillaId ? { cuadrillaId: Number(cuadrillaId) } : {}),
                        ...(activo !== "todos" ? { activo } : {}),
                    },
                },
                ...(signal ? { signal } : {}),
            }),
        ),
    );
    const { primera } = paginado;
    if (primera.estado === "cargando") {
        return (
            <div className="grid gap-3 lg:grid-cols-2" aria-busy="true" aria-label="Cargando técnicos">
                <Esqueleto className="h-36" />
                <Esqueleto className="h-36" />
            </div>
        );
    }
    if (primera.estado === "error") {
        return <ErrorLectura error={primera.error} que="los técnicos" reintentar={primera.reintentar} />;
    }
    if (paginado.items.length === 0) {
        return (
            <Tarjeta>
                <EstadoVacio
                    icono={HardHat}
                    titulo={q || cuadrillaId || activo !== "true" ? "Ningún técnico coincide con el filtro" : "Aún no hay técnicos con ficha"}
                    descripcion="Da de alta la ficha de agenda de un usuario con el rol de técnico para poder programarle órdenes."
                />
            </Tarjeta>
        );
    }
    return (
        <div className="grid gap-3">
            <ul className="grid gap-3 lg:grid-cols-2" aria-label="Técnicos">
                {paginado.items.map((t) => (
                    <li key={t.id} className="min-w-0">
                        <TarjetaTecnico tecnico={t} cuadrillas={cuadrillas} zonas={zonas} alCambiar={alCambiar} />
                    </li>
                ))}
            </ul>
            <PieLista paginado={paginado} que="técnicos" />
        </div>
    );
}

function TarjetaTecnico({
    tecnico: t,
    cuadrillas,
    zonas,
    alCambiar,
}: {
    tecnico: Tecnico;
    cuadrillas: readonly Cuadrilla[] | undefined;
    zonas: readonly Zona[] | undefined;
    alCambiar: () => void;
}) {
    const { puede } = useSesion();
    const cuadrilla = cuadrillas?.find((c) => c.id === t.cuadrillaId);
    const nombreCuadrilla = t.cuadrillaId === null ? "Sin cuadrilla" : (cuadrilla?.nombre ?? `Cuadrilla ${t.cuadrillaId}`);
    return (
        <Tarjeta className="grid h-full gap-3 p-4 [&>*]:min-w-0" data-tecnico={t.id}>
            <div className="flex flex-wrap items-start justify-between gap-2">
                <div className="flex min-w-0 items-center gap-2">
                    <UserRound aria-hidden className="size-5 shrink-0 text-muted-foreground" />
                    <div className="grid min-w-0">
                        <Link href={`/tecnicos/${t.id}`} className="truncate font-semibold text-primary underline-offset-4 hover:underline">
                            {t.nombre}
                        </Link>
                        <span className="truncate text-sm text-muted-foreground">{t.correo}</span>
                    </div>
                </div>
                <InsigniaActivo activo={t.activo} />
            </div>
            <dl className="grid gap-2 text-sm sm:grid-cols-2">
                <div className="grid gap-0.5">
                    <dt className="text-xs font-medium text-muted-foreground">Cuadrilla</dt>
                    <dd>
                        {nombreCuadrilla}
                        {cuadrilla ? <span className="block text-xs text-muted-foreground">{textoZonas(cuadrilla, zonas)}</span> : null}
                    </dd>
                </div>
                <div className="grid gap-0.5">
                    <dt className="text-xs font-medium text-muted-foreground">Teléfono</dt>
                    <dd>
                        {t.telefono ? (
                            <a href={`tel:${t.telefono.replace(/[^\d+]/g, "")}`} className="inline-flex items-center gap-1 text-primary underline-offset-4 hover:underline">
                                <Phone aria-hidden className="size-3.5" />
                                {t.telefono}
                            </a>
                        ) : (
                            <span className="text-muted-foreground">Sin teléfono</span>
                        )}
                    </dd>
                </div>
                {(t.habilidades ?? []).length > 0 ? (
                    <div className="grid gap-0.5 sm:col-span-2">
                        <dt className="text-xs font-medium text-muted-foreground">Habilidades</dt>
                        <dd>{(t.habilidades ?? []).join(", ")}</dd>
                    </div>
                ) : null}
            </dl>
            <div className="flex flex-wrap gap-2">
                <Boton asChild variante="secundario" tamano="sm">
                    <Link href={`/tecnicos/${t.id}`}>Ver ficha</Link>
                </Boton>
                {puede(["agenda.editar_tecnico"]) && cuadrillas ? (
                    <Boton asChild variante="contorno" tamano="sm">
                        <Link href={`/tecnicos/${t.id}/editar?desde=lista`} aria-label={`Editar a ${t.nombre}`}>
                            <Pencil />
                            Editar
                        </Link>
                    </Boton>
                ) : null}
                <CambiarEstado
                    activo={t.activo}
                    nombre={t.nombre}
                    puede={puede([t.activo ? "agenda.desactivar_tecnico" : "agenda.activar_tecnico"])}
                    consecuencia="Deja de ser asignable en la agenda y el despacho. Sus citas ya programadas no se mueven: reprográmalas si hace falta."
                    ejecutar={(accion, clave) =>
                        resolver(
                            api().POST(accion === "activar" ? "/api/v1/tecnicos/{id}/activar" : "/api/v1/tecnicos/{id}/desactivar", {
                                params: { path: { id: t.id }, header: { "Idempotency-Key": clave } },
                                body: { version: t.version },
                            }),
                        )
                    }
                    alHecho={alCambiar}
                />
            </div>
        </Tarjeta>
    );
}
