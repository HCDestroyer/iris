"use client";

import { TriangleAlert } from "lucide-react";
import Link from "next/link";
import { createContext, useContext } from "react";
import { explicarFalla } from "@/lib/conversaciones/falla";
import type { Mensaje } from "@/lib/conversaciones/tipos";
import { useSesion } from "@/lib/sesion";

/** El número (canal) de la conversación abierta, para llevar a quien lo administra a su pantalla. */
const CanalDeLaConversacion = createContext<number | null>(null);
export const ProveedorCanal = CanalDeLaConversacion.Provider;

/**
 * Bajo un mensaje que no se envió: por qué, en llano (contratos/conversaciones.md §1.1). Si Meta
 * rechazó el token o los permisos del número, a quien tiene `mensajeria.configurar` se le lleva a
 * la pantalla del número; a los demás se les dice que avisen. El código de Meta se muestra solo si
 * vino (el servidor lo manda únicamente a quien administra los números).
 */
export function FallaMensaje({ m }: { m: Mensaje }) {
    const { puede } = useSesion();
    const canalId = useContext(CanalDeLaConversacion);
    const falla = explicarFalla(m, puede(["mensajeria.configurar"]));
    if (!falla) return null;
    return (
        <span role="note" data-slot="falla-mensaje" data-motivo={m.motivoFalla ?? undefined} className="mt-0.5 grid min-w-0 gap-0.5 border-t border-error/25 pt-1 text-xs text-error">
            {falla.texto ? (
                <span className="flex min-w-0 items-start gap-1.5">
                    <TriangleAlert aria-hidden className="mt-px size-3.5 shrink-0" />
                    <span className="min-w-0 [overflow-wrap:anywhere]">{falla.texto}</span>
                </span>
            ) : null}
            {falla.revisarNumero && canalId !== null ? (
                <Link href={`/administracion/numeros/${canalId}/credenciales`} className="w-fit font-semibold underline underline-offset-2 hover:no-underline">
                    Revisar el número
                </Link>
            ) : null}
            {falla.codigo ? (
                <span className="text-muted-foreground tabular-nums" data-slot="codigo-falla">
                    {falla.codigo}
                </span>
            ) : null}
        </span>
    );
}
