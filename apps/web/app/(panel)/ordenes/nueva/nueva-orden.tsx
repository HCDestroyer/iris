"use client";

import type { Schema } from "@iris/contratos";
import { Aviso, Boton } from "@iris/ui";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { CargaRegistro, CargandoRegistro } from "@/componentes/carga-registro";
import { FormularioCrearOrden } from "@/componentes/ordenes/crear-orden";
import { FormularioCrearOrdenTicket } from "@/componentes/ordenes/crear-orden-ticket";
import { useUsuarios, type Usuario } from "@/componentes/tickets/catalogos";
import { api, resolver } from "@/lib/api/cliente";
import { useLectura } from "@/lib/lectura";
import { idDeParametro } from "@/lib/parametros";
import { useSesion } from "@/lib/sesion";

/**
 * `/ordenes/nueva`: orden desde cero. Con `?ticket=<id>` la orden sale del ticket, que se lee de
 * la API (nunca se confía en textos de la URL): con `tickets.crear_orden`, «Crear orden y
 * asignar»; si no, la orden ligada sin asignar.
 */
export function NuevaOrden() {
    const params = useSearchParams();
    const ticketId = idDeParametro(params.get("ticket"));
    return ticketId === null ? <FormularioCrearOrden /> : <DesdeTicket key={ticketId} ticketId={ticketId} />;
}

function DesdeTicket({ ticketId }: { ticketId: number }) {
    const { puede } = useSesion();
    const ticket = useLectura((signal) => resolver(api().GET("/api/v1/tickets/{id}", { params: { path: { id: ticketId } }, signal })));
    const conAsignacion = puede(["tickets.crear_orden"]);
    const usuarios = useUsuarios();
    if (conAsignacion && usuarios.estado === "cargando") {
        return <CargandoRegistro que="el ticket" />;
    }
    return (
        <CargaRegistro lectura={ticket} que="el ticket" noEncontrado="Ticket no encontrado">
            {(t) => <OrdenDeTicket ticket={t} conAsignacion={conAsignacion} usuarios={usuarios.estado === "listo" ? usuarios.datos : undefined} />}
        </CargaRegistro>
    );
}

function OrdenDeTicket({ ticket: t, conAsignacion, usuarios }: { ticket: Schema<"Ticket">; conAsignacion: boolean; usuarios: readonly Usuario[] | null | undefined }) {
    if (t.estado === "cerrado") {
        return (
            <Aviso
                tono="warning"
                titulo={`El ticket ${t.numero} está cerrado`}
                accion={
                    <Boton asChild variante="secundario" tamano="sm">
                        <Link href={`/tickets/${t.id}`}>Volver al ticket</Link>
                    </Boton>
                }
            >
                Un ticket cerrado no recibe órdenes nuevas. Reábrelo si hace falta otra visita.
            </Aviso>
        );
    }
    if (conAsignacion) {
        return <FormularioCrearOrdenTicket ticket={t} usuarios={usuarios} />;
    }
    return <FormularioCrearOrden ticket={t} />;
}
