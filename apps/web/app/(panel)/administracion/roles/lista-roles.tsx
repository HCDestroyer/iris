"use client";

import { Boton, Esqueleto, EstadoVacio, InsigniaEstado, Tarjeta } from "@iris/ui";
import { ChevronRight, CircleCheck, CirclePause, Lock, Plus, ShieldCheck, UserRound } from "lucide-react";
import Link from "next/link";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { PieLista } from "@/componentes/pie-lista";
import type { Rol } from "@/lib/accesos/presentacion";
import { api, resolver } from "@/lib/api/cliente";
import { usePaginado } from "@/lib/paginado";
import { useSesion } from "@/lib/sesion";

/** Administración → Roles y permisos: los base (no se editan) y los propios (activos o no). */
export function ListaRoles() {
    const { puede } = useSesion();
    const paginado = usePaginado<Rol>((cursor, signal) =>
        resolver(api().GET("/api/v1/roles", { params: { query: { limit: 100, ...(cursor ? { cursor } : {}) } }, ...(signal ? { signal } : {}) })),
    );
    const encabezado = (
        <EncabezadoPagina
            titulo="Roles y permisos"
            descripcion="Qué puede hacer cada rol. Los roles base los mantiene Iris; los propios se crean, editan y desactivan aquí (nunca se borran)."
            acciones={
                puede(["roles.crear"]) ? (
                    <Boton asChild>
                        <Link href="/administracion/roles/nuevo">
                            <Plus />
                            Nuevo rol
                        </Link>
                    </Boton>
                ) : null
            }
        />
    );
    const { primera } = paginado;
    if (primera.estado === "cargando") {
        return (
            <>
                {encabezado}
                <div className="grid gap-3" aria-busy="true">
                    <span className="sr-only" role="status">
                        Cargando roles…
                    </span>
                    <Esqueleto className="h-20" />
                    <Esqueleto className="h-20" />
                </div>
            </>
        );
    }
    if (primera.estado === "error") {
        return (
            <>
                {encabezado}
                <ErrorLectura error={primera.error} que="los roles" reintentar={primera.reintentar} />
            </>
        );
    }
    const roles = [...paginado.items].sort((a, b) => Number(b.sistema) - Number(a.sistema) || a.nombre.localeCompare(b.nombre, "es"));
    return (
        <>
            {encabezado}
            {roles.length === 0 ? (
                <Tarjeta>
                    <EstadoVacio icono={ShieldCheck} titulo="Sin roles" descripcion="Aún no hay roles registrados." />
                </Tarjeta>
            ) : (
                <ul className="grid gap-3 md:grid-cols-2" aria-label="Roles">
                    {roles.map((r) => (
                        <li key={r.id} data-rol={r.codigo}>
                            <Link href={`/administracion/roles/${r.id}`} className="block h-full rounded-[var(--hx-radius-card)]">
                                <Tarjeta className="flex h-full items-start gap-3 p-4 transition-colors hover:bg-elevated">
                                    <div className="grid min-w-0 flex-1 gap-2">
                                        <div className="grid gap-0.5">
                                            <span className="font-medium">{r.nombre}</span>
                                            <code className="text-xs text-muted-foreground">{r.codigo}</code>
                                        </div>
                                        {r.descripcion ? <p className="text-sm text-muted-foreground">{r.descripcion}</p> : null}
                                        <div className="flex flex-wrap items-center gap-2">
                                            {r.sistema ? (
                                                <InsigniaEstado tono="neutral" icono={Lock} texto="Rol base" />
                                            ) : r.activo ? (
                                                <InsigniaEstado tono="success" icono={CircleCheck} texto="Activo" />
                                            ) : (
                                                <InsigniaEstado tono="warning" icono={CirclePause} texto="Desactivado" />
                                            )}
                                            <span className="text-xs text-muted-foreground">
                                                {r.permisos?.length ?? 0} permisos ·{" "}
                                                <UserRound aria-hidden className="inline size-3.5 align-[-2px]" /> {r.usuarios === 1 ? "1 usuario" : `${r.usuarios} usuarios`}
                                            </span>
                                        </div>
                                    </div>
                                    <ChevronRight aria-hidden className="size-5 shrink-0 text-muted-foreground" />
                                </Tarjeta>
                            </Link>
                        </li>
                    ))}
                </ul>
            )}
            <div className="mt-3 grid">
                <PieLista paginado={paginado} que="roles" />
            </div>
        </>
    );
}
