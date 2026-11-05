"use client";

import { ButtonGroup } from "@heroui/react";
import type { ReactNode } from "react";
import { cn } from "../utilidades";

export type PropsGrupoBotones = {
    /** Nombre accesible del grupo (p. ej. «Más acciones de la orden»). */
    etiqueta: string;
    /** Ocupa todo el ancho y reparte los botones por igual. */
    anchoCompleto?: boolean;
    className?: string;
    children: ReactNode;
};

/**
 * Grupo de botones unidos (`ButtonGroup` de HeroUI, `Group` de React Aria): acciones hermanas de
 * igual peso, como «Pausar | No se pudo» bajo la acción principal de la orden. Los hijos son
 * `Boton`; entre ellos va un separador fino. Mismo alto que un `Boton` grande (48 px).
 */
export function GrupoBotones({ etiqueta, anchoCompleto = false, className, children }: PropsGrupoBotones) {
    return (
        <ButtonGroup
            aria-label={etiqueta}
            fullWidth={anchoCompleto}
            data-slot="grupo-botones"
            className={cn(
                "flex overflow-hidden rounded-full border border-border bg-surface",
                "[&_[data-slot=boton]]:h-12 [&_[data-slot=boton]]:min-w-0 [&_[data-slot=boton]]:flex-1 [&_[data-slot=boton]]:rounded-none [&_[data-slot=boton]]:bg-transparent [&_[data-slot=boton]]:shadow-none",
                "[&_[data-slot=boton]+[data-slot=boton]]:border-l [&_[data-slot=boton]+[data-slot=boton]]:border-border",
                className,
            )}
        >
            {children}
        </ButtonGroup>
    );
}
