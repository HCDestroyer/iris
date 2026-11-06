"use client";

import { Boton, Esqueleto, EstadoVacio, Tarjeta } from "@iris/ui";
import { CalendarClock, CalendarDays, Pencil, Plus } from "lucide-react";
import Link from "next/link";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { api, resolver } from "@/lib/api/cliente";
import { resumirFranjas } from "@/lib/horarios/franjas";
import { useLectura } from "@/lib/lectura";

/** Horarios de atención (§10.1): se asignan a números y colas; cada uno con sus franjas y feriados. */
export function ListaHorarios() {
    const lectura = useLectura(async (signal) => (await resolver(api().GET("/api/v1/mensajeria/horarios", { signal }))).data ?? []);
    return (
        <>
            <EncabezadoPagina
                titulo="Horarios"
                descripcion="Franjas por día y feriados. Fuera de ellos el SLA no corre y el número puede contestar solo."
                acciones={
                    <Boton asChild>
                        <Link href="/administracion/horarios/nuevo">
                            <Plus />
                            Nuevo horario
                        </Link>
                    </Boton>
                }
            />
            {lectura.estado === "cargando" ? (
                <div className="grid gap-2" aria-busy="true">
                    <Esqueleto className="h-20" />
                    <Esqueleto className="h-20" />
                </div>
            ) : lectura.estado === "error" ? (
                <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="los horarios" />
            ) : lectura.datos.length === 0 ? (
                <Tarjeta>
                    <EstadoVacio icono={CalendarClock} titulo="Aún no hay horarios" descripcion="Sin horario, los números y las colas atienden siempre." />
                </Tarjeta>
            ) : (
                <ul className="grid gap-2" aria-label="Horarios">
                    {lectura.datos.map((h) => (
                        <li key={h.id} data-horario={h.id} className="flex min-w-0 flex-wrap items-center gap-3 rounded-xl border border-border bg-card p-3 sm:flex-nowrap">
                            <div className="grid min-w-0 flex-1 gap-0.5">
                                <span className="truncate font-semibold">{h.nombre}</span>
                                <span className="text-sm break-words text-muted-foreground">{resumirFranjas(h.franjas)}</span>
                                <span className="flex items-center gap-1 text-xs text-muted-foreground">
                                    <CalendarDays aria-hidden className="size-3.5" />
                                    {(h.feriados ?? []).length === 0 ? "Sin feriados" : `${(h.feriados ?? []).length} ${(h.feriados ?? []).length === 1 ? "feriado" : "feriados"}`} · {h.zona}
                                </span>
                            </div>
                            <Boton asChild variante="contorno" tamano="sm">
                                <Link href={`/administracion/horarios/${h.id}/editar`} aria-label={`Editar ${h.nombre}`}>
                                    <Pencil />
                                    Editar
                                </Link>
                            </Boton>
                        </li>
                    ))}
                </ul>
            )}
        </>
    );
}
