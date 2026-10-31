"use client";

import type { Schema } from "@iris/contratos";
import { Aviso } from "@iris/ui";
import { CircleCheck } from "lucide-react";
import Link from "next/link";

/**
 * Propuesta pendiente de resolver o cerrar el ticket (de Horus o del cierre de su orden de
 * trabajo). Iris nunca resuelve solo: un agente lo confirma con «Resolver» o «Cerrar».
 */
export function PropuestaResolucion({ propuesta }: { propuesta: Schema<"PropuestaTicket"> | undefined }) {
    if (!propuesta) return null;
    const verbo = propuesta.accion === "resolver" ? "resolver" : "cerrar";
    return (
        <Aviso tono="info" icono={CircleCheck} titulo={`Se propone ${verbo} el ticket`} data-testid="propuesta-resolucion">
            <p>
                {propuesta.motivo ?? (propuesta.origen === "orden" ? "Se cerró su orden de trabajo." : "Horus avisó que el problema terminó.")}{" "}
                {propuesta.origen === "orden" && propuesta.ordenId !== null ? (
                    <Link className="text-primary underline-offset-4 hover:underline" href={`/ordenes/${propuesta.ordenId}`}>
                        Ver la orden
                    </Link>
                ) : null}
            </p>
            <p className="text-muted-foreground">Revísalo y confírmalo con «{propuesta.accion === "resolver" ? "Resolver" : "Cerrar"}».</p>
        </Aviso>
    );
}
