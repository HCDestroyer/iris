"use client";

import type { Schema } from "@iris/contratos";
import { avisar, Aviso, Boton } from "@iris/ui";
import { ArrowLeftRight, RotateCw } from "lucide-react";
import { useRef, useState } from "react";
import type { Usuario } from "@/componentes/tickets/catalogos";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import type { Permiso } from "@/lib/menu";
import { igualacionesPosibles, type Desde } from "@/lib/ordenes/ticket-orden";
import { useSesion } from "@/lib/sesion";
import { tecnicosDe } from "./catalogos";

type TicketAsignado = Pick<Schema<"Ticket">, "asignadoA" | "cuadrillaId" | "estado" | "version" | "numero">;
type OrdenAsignada = Pick<Schema<"Orden">, "id" | "numero" | "tecnicoId" | "cuadrillaId" | "estado" | "version">;
export type AsignacionTicketOrden = Schema<"AsignacionTicketOrden">;

const PERMISO: Record<Desde, Permiso> = { ticket: "ordenes.asignar", orden: "tickets.asignar" };

function describir(id: number | null, cuadrilla: number | null, usuarios: readonly Usuario[] | null | undefined, vacio: string): string {
    const partes = [
        id !== null ? (usuarios?.find((u) => u.id === id)?.nombre ?? `Usuario ${id}`) : null,
        cuadrilla !== null ? `Cuadrilla ${cuadrilla}` : null,
    ].filter(Boolean);
    return partes.length ? partes.join(" · ") : vacio;
}

/**
 * Cuando el ticket y su orden no tienen la misma asignación, lo dice y ofrece igualarla con un
 * botón (`POST /ordenes/{id}/igualar-asignacion`), en el sentido que tenga sentido. Nunca iguala
 * solo. Sin diferencia (o sin permiso) no muestra nada.
 */
export function IgualarAsignacion({
    ticket,
    orden,
    usuarios,
    alHecho,
    recargar,
}: {
    ticket: TicketAsignado;
    orden: OrdenAsignada;
    usuarios: readonly Usuario[] | null | undefined;
    alHecho: (r: AsignacionTicketOrden) => void;
    recargar: () => void;
}) {
    const { puede } = useSesion();
    const clave = useRef(nuevaClaveIdempotencia());
    const [enviando, setEnviando] = useState<Desde | null>(null);
    const [error, setError] = useState<unknown>(null);
    const tecnicos = usuarios ? tecnicosDe(usuarios) : null;
    const esTecnico = tecnicos ? (id: number) => tecnicos.some((t) => t.id === id) : null;
    const opciones = igualacionesPosibles(ticket, orden, esTecnico).filter((d) => puede(["ordenes.igualar_asignacion"]) && puede([PERMISO[d]]));
    if (opciones.length === 0 && error === null) {
        return null;
    }

    async function igualar(desde: Desde) {
        setError(null);
        setEnviando(desde);
        try {
            const r = await resolver(
                api().POST("/api/v1/ordenes/{id}/igualar-asignacion", {
                    params: { path: { id: orden.id }, header: { "Idempotency-Key": clave.current } },
                    body: { desde, version: orden.version, versionTicket: ticket.version },
                }),
            );
            clave.current = nuevaClaveIdempotencia();
            avisar({ titulo: `Asignación igualada entre el ticket ${ticket.numero} y la orden ${orden.numero}` });
            alHecho(r);
        } catch (e) {
            clave.current = claveTrasError(e, clave.current);
            setError(e);
        } finally {
            setEnviando(null);
        }
    }

    const conflicto = error instanceof ErrorApi && error.estado === 409 && error.codigo !== "idempotencia_en_proceso";
    const delTicket = describir(ticket.asignadoA, ticket.cuadrillaId, usuarios, "sin asignar");
    const deLaOrden = describir(orden.tecnicoId, orden.cuadrillaId, usuarios, "sin asignar");
    return (
        <div className="grid gap-2" data-testid={`igualar-${orden.id}`}>
            {opciones.length ? (
                <Aviso tono="warning" icono={ArrowLeftRight} titulo={`El ticket y la orden ${orden.numero} tienen distinta asignación`}>
                    <p>
                        Ticket: <span className="font-medium">{delTicket}</span> · Orden: <span className="font-medium">{deLaOrden}</span>
                    </p>
                    <div className="mt-2 flex flex-wrap gap-2">
                        {opciones.includes("ticket") ? (
                            <Boton type="button" variante="secundario" tamano="sm" disabled={enviando !== null} onClick={() => void igualar("ticket")}>
                                {enviando === "ticket" ? "Asignando…" : "Asignar la orden como el ticket"}
                            </Boton>
                        ) : null}
                        {opciones.includes("orden") ? (
                            <Boton type="button" variante="secundario" tamano="sm" disabled={enviando !== null} onClick={() => void igualar("orden")}>
                                {enviando === "orden" ? "Asignando…" : "Asignar el ticket como la orden"}
                            </Boton>
                        ) : null}
                    </div>
                </Aviso>
            ) : null}
            {conflicto ? (
                <Aviso
                    tono="warning"
                    titulo="Alguien más cambió el ticket o la orden"
                    accion={
                        <Boton type="button" variante="secundario" tamano="sm" onClick={recargar}>
                            <RotateCw />
                            Recargar
                        </Boton>
                    }
                >
                    Recarga para ver la asignación actual antes de igualarla.
                </Aviso>
            ) : error ? (
                <Aviso tono="error" titulo="No se pudo igualar la asignación">
                    {[mensajeDeError(error).titulo, mensajeDeError(error).detalle, ...(error instanceof ErrorApi ? error.errores.map((e) => e.message) : [])]
                        .filter(Boolean)
                        .join(". ")}
                </Aviso>
            ) : null}
        </div>
    );
}
