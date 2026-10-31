"use client";

import { Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { enlaceArgosCliente, type EnlacesExternos } from "@/lib/enlaces";
import type { Ticket } from "@/lib/tickets/sla";
import { Contacto, DatosClienteServicioConPermiso } from "./datos-cliente-servicio";

export { Contacto, DatosClienteServicio as FichaArgos, type ContactoCapturado } from "./datos-cliente-servicio";

/** Cliente del ticket: «Datos del cliente / servicio» si hay `clienteId`; si no, el contacto capturado. */
export function FichaCliente({ ticket, enlaces }: { ticket: Ticket; enlaces: EnlacesExternos | null }) {
    if (ticket.clienteId !== null) {
        return (
            <DatosClienteServicioConPermiso
                clienteId={ticket.clienteId}
                servicioId={ticket.servicioId}
                copia={ticket.datosArgos ?? null}
                contacto={ticket.contacto}
                origen="ticket"
                verArgos={enlaceArgosCliente(enlaces, ticket.clienteId)}
            />
        );
    }
    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>Contacto</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido>
                <Contacto contacto={ticket.contacto} vacio="Sin datos de contacto." />
            </TarjetaContenido>
        </Tarjeta>
    );
}
