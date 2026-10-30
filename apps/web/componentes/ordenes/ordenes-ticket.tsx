"use client";

import type { Schema } from "@iris/contratos";
import { Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import Link from "next/link";
import { ErrorLectura } from "@/componentes/error-lectura";
import type { Usuario } from "@/componentes/tickets/catalogos";
import { api, resolver } from "@/lib/api/cliente";
import { formatearFecha } from "@/lib/fechas";
import { useLectura } from "@/lib/lectura";
import { IgualarAsignacion, type AsignacionTicketOrden } from "./asignacion-ticket-orden";
import { InsigniaEstadoOrden, InsigniaTipoOrden } from "./insignias";

type Props = {
    ticket: Schema<"Ticket">;
    usuarios: readonly Usuario[] | null | undefined;
    /** Se igualó la asignación: el ticket que devolvió la API. */
    alCambiarTicket: (ticket: Schema<"Ticket">) => void;
};

/**
 * Órdenes creadas desde un ticket (`GET /ordenes?ticketId=`) con estado, técnico y fecha. Si una
 * tiene otra asignación que el ticket, ofrece igualarla. Sin órdenes no se muestra nada.
 */
export function OrdenesDelTicket({ ticket, usuarios, alCambiarTicket }: Props) {
    const ordenes = useLectura((signal) => resolver(api().GET("/api/v1/ordenes", { params: { query: { ticketId: ticket.id, limit: 20 } }, signal })));
    if (ordenes.estado === "cargando") return null;
    if (ordenes.estado === "error") {
        return <ErrorLectura error={ordenes.error} que="las órdenes del ticket" reintentar={ordenes.reintentar} />;
    }
    const lista = ordenes.datos.data ?? [];
    if (lista.length === 0) return null;
    const nombre = (id: number | null) => (id === null ? null : (usuarios?.find((u) => u.id === id)?.nombre ?? `Usuario ${id}`));
    const alIgualar = (r: AsignacionTicketOrden) => {
        alCambiarTicket(r.ticket);
        void ordenes.refrescar();
    };
    return (
        <Tarjeta data-testid="ordenes-del-ticket">
            <TarjetaEncabezado>
                <TarjetaTitulo>Órdenes de trabajo</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido>
                <ul className="grid gap-3" aria-label="Órdenes del ticket">
                    {lista.map((o) => (
                        <li key={o.id} className="grid gap-2 text-sm" data-orden={o.id}>
                            <div className="flex flex-wrap items-center gap-2">
                                <Link className="font-mono text-primary underline-offset-4 hover:underline" href={`/ordenes/${o.id}`}>
                                    {o.numero}
                                </Link>
                                <InsigniaTipoOrden tipo={o.tipo} />
                                <InsigniaEstadoOrden estado={o.estado} />
                            </div>
                            <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-xs">
                                <dt className="text-muted-foreground">Técnico</dt>
                                <dd data-testid="orden-tecnico">
                                    {nombre(o.tecnicoId) ?? "Sin técnico"}
                                    {o.cuadrillaId !== null ? <span className="text-muted-foreground"> · Cuadrilla {o.cuadrillaId}</span> : null}
                                </dd>
                                <dt className="text-muted-foreground">Cita</dt>
                                <dd className="tabular-nums">{o.programadaInicio ? formatearFecha(o.programadaInicio) : "Sin programar"}</dd>
                            </dl>
                            <IgualarAsignacion ticket={ticket} orden={o} usuarios={usuarios} alHecho={alIgualar} recargar={ordenes.reintentar} />
                        </li>
                    ))}
                </ul>
                {ordenes.datos.nextCursor ? <p className="mt-2 text-xs text-muted-foreground">Se muestran las 20 más recientes.</p> : null}
            </TarjetaContenido>
        </Tarjeta>
    );
}
