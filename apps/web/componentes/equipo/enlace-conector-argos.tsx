"use client";

import { Stethoscope } from "lucide-react";
import Link from "next/link";
import { RUTA_DIAGNOSTICO_ARGOS } from "@/lib/integraciones/presentacion";
import { useSesion } from "@/lib/sesion";

/**
 * «Ver el conector de Argos»: junto a «Argos < 1.6.0: inventario por técnico no disponible», lleva
 * al diagnóstico del conector (versión leída, última revisión y por qué falló). Sin
 * `integraciones.ver` no hay pantalla que abrir y no aparece.
 */
export function EnlaceConectorArgos() {
    const { puede } = useSesion();
    if (!puede(["integraciones.ver"])) return null;
    return (
        <Link href={RUTA_DIAGNOSTICO_ARGOS} className="mt-1 inline-flex items-center gap-1.5 font-medium text-primary underline-offset-4 hover:underline" data-testid="enlace-conector-argos">
            <Stethoscope aria-hidden className="size-4 shrink-0" />
            Ver el conector de Argos
        </Link>
    );
}
