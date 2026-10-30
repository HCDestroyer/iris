"use client";

import { Boton, Esqueleto, EstadoVacio, InsigniaEstado, Tarjeta } from "@iris/ui";
import { ArrowLeft, CloudDownload, Map as IconoMapa, MapPinned, Pencil, Plus } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { CambiarEstado } from "@/componentes/equipo/cambiar-estado";
import { FormularioZona } from "@/componentes/equipo/formulario-catalogo";
import { InsigniaActivo } from "@/componentes/equipo/insignias";
import { ErrorLectura } from "@/componentes/error-lectura";
import { api, resolver } from "@/lib/api/cliente";
import type { Zona } from "@/lib/despacho/calendario";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";

const ORIGEN: Record<Zona["origen"], { texto: string; icono: typeof IconoMapa }> = {
    argos: { texto: "De Argos", icono: CloudDownload },
    local: { texto: "Local", icono: MapPinned },
    migrada: { texto: "Migrada", icono: IconoMapa },
};

/** Zonas (espejo de Argos y locales): alta de locales, renombrar, activar y desactivar. */
export function PantallaZonas() {
    const { puede } = useSesion();
    const zonas = useLectura<Zona[]>(async (signal) => (await resolver(api().GET("/api/v1/zonas", { params: { query: { todas: true } }, signal }))).data ?? []);
    const [formulario, setFormulario] = useState<{ zona?: Zona } | null>(null);

    let contenido;
    if (zonas.estado === "cargando") {
        contenido = (
            <div className="grid gap-3" aria-busy="true" aria-label="Cargando zonas">
                <Esqueleto className="h-20" />
                <Esqueleto className="h-20" />
            </div>
        );
    } else if (zonas.estado === "error") {
        contenido = <ErrorLectura error={zonas.error} que="las zonas" reintentar={zonas.reintentar} />;
    } else if (zonas.datos.length === 0) {
        contenido = (
            <Tarjeta>
                <EstadoVacio icono={IconoMapa} titulo="Aún no hay zonas" descripcion="Con Argos conectado, sus zonas llegan solas. Sin conector, crea las zonas locales aquí." />
            </Tarjeta>
        );
    } else {
        contenido = (
            <ul className="grid gap-3 lg:grid-cols-2" aria-label="Zonas">
                {zonas.datos.map((z) => {
                    const o = ORIGEN[z.origen];
                    return (
                        <li key={z.id} className="min-w-0">
                            <Tarjeta className="flex h-full flex-wrap items-center gap-3 p-4" data-zona={z.id}>
                                <span className="min-w-0 flex-1 font-semibold">{z.nombre}</span>
                                <InsigniaEstado tono="neutral" icono={o.icono} texto={o.texto} />
                                <InsigniaActivo activo={z.activa} femenino />
                                <div className="flex basis-full flex-wrap gap-2">
                                    {puede(["agenda.editar_zona"]) && z.origen !== "argos" ? (
                                        <Boton variante="contorno" tamano="sm" onClick={() => setFormulario({ zona: z })} aria-label={`Editar ${z.nombre}`}>
                                            <Pencil />
                                            Renombrar
                                        </Boton>
                                    ) : null}
                                    <CambiarEstado
                                        activo={z.activa}
                                        nombre={z.nombre}
                                        puede={puede([z.activa ? "agenda.desactivar_zona" : "agenda.activar_zona"])}
                                        consecuencia="Una zona inactiva no se puede usar en tickets ni órdenes nuevos."
                                        ejecutar={(accion, clave) =>
                                            resolver(
                                                api().POST(accion === "activar" ? "/api/v1/zonas/{id}/activar" : "/api/v1/zonas/{id}/desactivar", {
                                                    params: { path: { id: z.id }, header: { "Idempotency-Key": clave } },
                                                    body: { version: z.version },
                                                }),
                                            )
                                        }
                                        alHecho={zonas.refrescar}
                                    />
                                </div>
                            </Tarjeta>
                        </li>
                    );
                })}
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
                titulo="Zonas"
                descripcion="Las zonas de Argos llegan solas de su catálogo y no se renombran en Iris. Las locales se crean aquí."
                acciones={
                    puede(["agenda.crear_zona"]) ? (
                        <Boton onClick={() => setFormulario({})}>
                            <Plus />
                            Nueva zona
                        </Boton>
                    ) : null
                }
            />
            {contenido}
            {formulario ? (
                <FormularioZona
                    key={formulario.zona?.id ?? "nueva"}
                    zona={formulario.zona}
                    alCerrar={() => setFormulario(null)}
                    alGuardar={() => {
                        setFormulario(null);
                        void zonas.refrescar();
                    }}
                />
            ) : null}
        </>
    );
}
