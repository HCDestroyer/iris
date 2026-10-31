import type { Schema } from "@iris/contratos";
import Link from "next/link";
import type { ReactNode } from "react";

/**
 * De qué ticket sale la orden, con lo que se lee de la API (nunca de la URL): número (enlazado a
 * su ficha), cliente, servicio y dirección. Con un enlace compartido de otro ticket, se nota antes
 * de guardar.
 */
export function TicketOrigen({ ticket }: { ticket: Schema<"Ticket"> }) {
    const copia = ticket.datosArgos ?? null;
    const cliente = copia
        ? `${copia.cliente.nombre} · ${copia.cliente.codigo}`
        : ticket.clienteId !== null
          ? `Cliente #${ticket.clienteId}`
          : (ticket.contacto.nombre ?? "Sin cliente de Argos");
    const servicio = copia?.servicio
        ? [copia.servicio.planNombre, copia.servicio.ip].filter(Boolean).join(" · ")
        : ticket.servicioId !== null
          ? `Servicio #${ticket.servicioId}`
          : "Sin servicio";
    const direccion = copia?.direccion || copia?.cliente.direccion || "Sin dirección registrada";
    return (
        <dl className="grid gap-x-4 gap-y-2 rounded-md border bg-background-secondary p-3 text-sm sm:grid-cols-2" data-testid="ticket-origen" aria-label="Ticket de origen">
            <Dato etiqueta="Ticket">
                <Link href={`/tickets/${ticket.id}`} className="font-medium text-primary underline-offset-4 hover:underline">
                    {ticket.numero}
                </Link>
                <span className="block text-muted-foreground">{ticket.asunto}</span>
            </Dato>
            <Dato etiqueta="Cliente">{cliente}</Dato>
            <Dato etiqueta="Servicio">{servicio}</Dato>
            <Dato etiqueta="Dirección">{direccion}</Dato>
        </dl>
    );
}

function Dato({ etiqueta, children }: { etiqueta: string; children: ReactNode }) {
    return (
        <div className="grid min-w-0 gap-0.5">
            <dt className="text-xs font-medium text-muted-foreground">{etiqueta}</dt>
            <dd className="min-w-0 break-words">{children}</dd>
        </div>
    );
}
