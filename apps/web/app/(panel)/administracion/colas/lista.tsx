"use client";

import { Boton, Esqueleto, EstadoVacio, InsigniaEstado, Tarjeta } from "@iris/ui";
import { Inbox, Pencil, Plus, Settings2 } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { leerNumeros, leerColas } from "@/lib/conversaciones/api-equipo";
import { useLectura } from "@/lib/lectura";

/** «Colas» (Administración): cada cola con sus miembros, supervisores, números y tiempos. */
export function ListaColas() {
    const lectura = useLectura(async (signal) => {
        const [colas, canales] = await Promise.all([leerColas(signal), leerNumeros(signal)]);
        return { colas, canales };
    });
    let contenido: ReactNode;
    if (lectura.estado === "cargando") {
        contenido = (
            <div className="grid gap-2" aria-busy="true">
                <span className="sr-only" role="status">
                    Cargando colas…
                </span>
                <Esqueleto className="h-20" />
                <Esqueleto className="h-20" />
            </div>
        );
    } else if (lectura.estado === "error") {
        contenido = <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="las colas" />;
    } else if (lectura.datos.colas.length === 0) {
        contenido = (
            <Tarjeta>
                <EstadoVacio icono={Inbox} titulo="Aún no hay colas" descripcion="Una cola reúne a las personas que atienden las conversaciones de uno o varios números." />
            </Tarjeta>
        );
    } else {
        const { colas, canales } = lectura.datos;
        contenido = (
            <ul aria-label="Colas" className="overflow-hidden rounded-xl border border-border bg-card">
                {colas.map((c) => {
                    const numeros = canales.filter((n) => n.colaId === c.id);
                    const disponibles = c.disponibles?.length ?? 0;
                    return (
                        <li key={c.id} data-cola={c.id} className="grid gap-x-4 gap-y-2 border-b border-border px-4 py-3 last:border-b-0 md:grid-cols-[minmax(10rem,1.2fr)_minmax(10rem,1.4fr)_minmax(8rem,1fr)_auto] md:items-center">
                            <div className="grid min-w-0 gap-0.5">
                                <span className="flex min-w-0 items-center gap-1.5">
                                    <span className="truncate font-semibold">{c.nombre}</span>
                                    {c.activa ? null : <InsigniaEstado tono="neutral" texto="Inactiva" />}
                                </span>
                                <span className="truncate text-xs text-muted-foreground">{numeros.length ? numeros.map((n) => n.nombre).join(", ") : "Ningún número llega aquí"}</span>
                            </div>
                            <div className="grid min-w-0 gap-0.5 text-sm">
                                <span className="truncate">
                                    {c.miembros.length} {c.miembros.length === 1 ? "miembro" : "miembros"} · {c.supervisores?.length ?? 0} {(c.supervisores?.length ?? 0) === 1 ? "supervisor" : "supervisores"}
                                </span>
                                <span className={disponibles === 0 ? "truncate text-xs font-medium text-warning" : "truncate text-xs text-muted-foreground"}>
                                    {disponibles === 0 ? "Nadie disponible ahora" : disponibles === 1 ? "1 disponible ahora" : `${disponibles} disponibles ahora`}
                                </span>
                            </div>
                            <div className="grid min-w-0 gap-0.5 text-xs text-muted-foreground">
                                <span className="truncate">{c.slaRespuestaMin ? `Responder en ${c.slaRespuestaMin} min` : "Sin tiempo de respuesta"}</span>
                                <span className="truncate">{c.botSinConfirmacionMin || c.botSinDisponiblesMin ? "Tiempos del bot propios" : "Tiempos del bot del número"}</span>
                            </div>
                            <Boton asChild variante="contorno" tamano="sm" className="justify-self-start md:justify-self-end">
                                <Link href={`/administracion/colas/${c.id}/editar`} aria-label={`Editar ${c.nombre}`}>
                                    <Pencil />
                                    Editar
                                </Link>
                            </Boton>
                        </li>
                    );
                })}
            </ul>
        );
    }
    return (
        <>
            <EncabezadoPagina
                titulo="Colas"
                descripcion="Quién atiende las conversaciones de cada número y quién las supervisa."
                acciones={
                    <>
                        <Boton asChild variante="secundario">
                            <Link href="/administracion/bandeja">
                                <Settings2 />
                                Ajustes de la bandeja
                            </Link>
                        </Boton>
                        <Boton asChild>
                            <Link href="/administracion/colas/nueva">
                                <Plus />
                                Nueva cola
                            </Link>
                        </Boton>
                    </>
                }
            />
            {contenido}
        </>
    );
}
