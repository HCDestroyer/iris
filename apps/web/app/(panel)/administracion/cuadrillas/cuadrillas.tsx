"use client";

import { Boton, Esqueleto, EstadoVacio, Tarjeta } from "@iris/ui";
import { ArrowLeft, Pencil, Plus, Users } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { CambiarEstado } from "@/componentes/equipo/cambiar-estado";
import { FormularioCuadrilla } from "@/componentes/equipo/formulario-catalogo";
import { textoZonas } from "@/componentes/equipo/formulario-tecnico";
import { InsigniaActivo } from "@/componentes/equipo/insignias";
import { ErrorLectura } from "@/componentes/error-lectura";
import { api, resolver } from "@/lib/api/cliente";
import type { Cuadrilla, Zona } from "@/lib/despacho/calendario";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";

/** Cuadrillas (activas e inactivas): alta, nombre y zonas, activar y desactivar. */
export function PantallaCuadrillas() {
    const { puede } = useSesion();
    const datos = useLectura<{ cuadrillas: Cuadrilla[]; zonas: Zona[] }>(async (signal) => {
        const [c, z] = await Promise.all([
            resolver(api().GET("/api/v1/cuadrillas", { params: { query: { todas: true } }, signal })),
            resolver(api().GET("/api/v1/zonas", { params: { query: { todas: true } }, signal })),
        ]);
        return { cuadrillas: c.data ?? [], zonas: z.data ?? [] };
    });
    const [formulario, setFormulario] = useState<{ cuadrilla?: Cuadrilla } | null>(null);

    let contenido;
    if (datos.estado === "cargando") {
        contenido = (
            <div className="grid gap-3" aria-busy="true" aria-label="Cargando cuadrillas">
                <Esqueleto className="h-24" />
                <Esqueleto className="h-24" />
            </div>
        );
    } else if (datos.estado === "error") {
        contenido = <ErrorLectura error={datos.error} que="las cuadrillas" reintentar={datos.reintentar} />;
    } else if (datos.datos.cuadrillas.length === 0) {
        contenido = (
            <Tarjeta>
                <EstadoVacio icono={Users} titulo="Aún no hay cuadrillas" descripcion="Crea una cuadrilla para agrupar a los técnicos y decir qué zonas atienden." />
            </Tarjeta>
        );
    } else {
        const { cuadrillas, zonas } = datos.datos;
        contenido = (
            <ul className="grid gap-3 lg:grid-cols-2" aria-label="Cuadrillas">
                {cuadrillas.map((c) => (
                    <li key={c.id} className="min-w-0">
                        <Tarjeta className="flex h-full flex-wrap items-start gap-3 p-4" data-cuadrilla={c.id}>
                            <div className="grid min-w-0 flex-1 gap-0.5">
                                <span className="font-semibold">{c.nombre}</span>
                                <span className="text-sm text-muted-foreground">{textoZonas(c, zonas)}</span>
                            </div>
                            <InsigniaActivo activo={c.activa} femenino />
                            <div className="flex basis-full flex-wrap gap-2">
                                {puede(["agenda.editar_cuadrilla"]) ? (
                                    <Boton variante="contorno" tamano="sm" onClick={() => setFormulario({ cuadrilla: c })} aria-label={`Editar ${c.nombre}`}>
                                        <Pencil />
                                        Editar
                                    </Boton>
                                ) : null}
                                <CambiarEstado
                                    activo={c.activa}
                                    nombre={c.nombre}
                                    puede={puede([c.activa ? "agenda.desactivar_cuadrilla" : "agenda.activar_cuadrilla"])}
                                    consecuencia="Una cuadrilla inactiva no se puede usar en tickets ni órdenes nuevos. Sus técnicos conservan la ficha."
                                    ejecutar={(accion, clave) =>
                                        resolver(
                                            api().POST(accion === "activar" ? "/api/v1/cuadrillas/{id}/activar" : "/api/v1/cuadrillas/{id}/desactivar", {
                                                params: { path: { id: c.id }, header: { "Idempotency-Key": clave } },
                                                body: { version: c.version },
                                            }),
                                        )
                                    }
                                    alHecho={datos.refrescar}
                                />
                            </div>
                        </Tarjeta>
                    </li>
                ))}
            </ul>
        );
    }

    return (
        <>
            <Boton asChild variante="enlace" tamano="sm" className="mb-3">
                <Link href="/tecnicos">
                    <ArrowLeft />
                    Técnicos
                </Link>
            </Boton>
            <EncabezadoPagina
                titulo="Cuadrillas"
                descripcion="Equipos de técnicos. Las zonas de la cuadrilla son las que atienden sus técnicos en la agenda y el despacho."
                acciones={
                    puede(["agenda.crear_cuadrilla"]) ? (
                        <Boton onClick={() => setFormulario({})} disabled={datos.estado !== "listo"}>
                            <Plus />
                            Nueva cuadrilla
                        </Boton>
                    ) : null
                }
            />
            {contenido}
            {formulario && datos.estado === "listo" ? (
                <FormularioCuadrilla
                    key={formulario.cuadrilla?.id ?? "nueva"}
                    cuadrilla={formulario.cuadrilla}
                    zonas={datos.datos.zonas}
                    alCerrar={() => setFormulario(null)}
                    alGuardar={() => {
                        setFormulario(null);
                        void datos.refrescar();
                    }}
                />
            ) : null}
        </>
    );
}
