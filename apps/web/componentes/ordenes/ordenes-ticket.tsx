"use client";

import { Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import Link from "next/link";
import { ErrorLectura } from "@/componentes/error-lectura";
import { api, resolver } from "@/lib/api/cliente";
import { formatearFecha } from "@/lib/fechas";
import { useLectura } from "@/lib/lectura";
import { InsigniaEstadoOrden, InsigniaTipoOrden } from "./insignias";

/** Órdenes creadas desde un ticket (`GET /ordenes?ticketId=`). Sin órdenes no se muestra nada. */
export function OrdenesDelTicket({ ticketId }: { ticketId: number }) {
    const ordenes = useLectura((signal) => resolver(api().GET("/api/v1/ordenes", { params: { query: { ticketId, limit: 20 } }, signal })));
    if (ordenes.estado === "cargando") return null;
    if (ordenes.estado === "error") {
        return <ErrorLectura error={ordenes.error} que="las órdenes del ticket" reintentar={ordenes.reintentar} />;
    }
    const lista = ordenes.datos.data ?? [];
    if (lista.length === 0) return null;
    return (
        <Tarjeta data-testid="ordenes-del-ticket">
            <TarjetaEncabezado>
                <TarjetaTitulo>Órdenes de trabajo</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido>
                <ul className="grid gap-2" aria-label="Órdenes del ticket">
                    {lista.map((o) => (
                        <li key={o.id} className="flex flex-wrap items-center gap-2 text-sm">
                            <Link className="font-mono text-primary underline-offset-4 hover:underline" href={`/ordenes/${o.id}`}>
                                {o.numero}
                            </Link>
                            <InsigniaTipoOrden tipo={o.tipo} />
                            <InsigniaEstadoOrden estado={o.estado} />
                            {o.programadaInicio ? <span className="text-xs text-muted-foreground tabular-nums">{formatearFecha(o.programadaInicio)}</span> : null}
                        </li>
                    ))}
                </ul>
                {ordenes.datos.nextCursor ? <p className="mt-2 text-xs text-muted-foreground">Se muestran las 20 más recientes.</p> : null}
            </TarjetaContenido>
        </Tarjeta>
    );
}
