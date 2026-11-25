"use client";

import { Spinner } from "@heroui/react";
import type { ComponentProps } from "react";
import { cn } from "../utilidades";

const TAMANO = { sm: "size-4", md: "size-5", lg: "size-7" } as const;

export type PropsCargando = ComponentProps<"span"> & {
    /** Texto para lectores de pantalla (y visible si `visible`). */
    texto?: string;
    visible?: boolean;
    /** 16 px (por omisión), 20 px o 28 px. */
    tamano?: keyof typeof TAMANO;
    /**
     * Solo el indicador, oculto para lectores de pantalla: cuando el texto ya está al lado (un
     * título «Conectando…», la etapa de una lista) o dentro de un botón que dice qué hace.
     */
    decorativo?: boolean;
};

/**
 * Indicador de carga: el `Spinner` de HeroUI (un indicador animado, no un icono) en el color del
 * texto, con su texto en `role="status"`.
 */
export function Cargando({ texto = "Cargando…", visible = false, tamano = "sm", decorativo = false, className, ...props }: PropsCargando) {
    const indicador = <Spinner color="current" size="sm" role="presentation" aria-label={undefined} aria-hidden className={TAMANO[tamano]} />;
    if (decorativo) {
        return (
            <span aria-hidden data-slot="cargando" className={cn("inline-flex shrink-0 items-center", className)} {...props}>
                {indicador}
            </span>
        );
    }
    return (
        <span role="status" data-slot="cargando" className={cn("inline-flex items-center gap-2 text-sm text-muted-foreground", className)} {...props}>
            {indicador}
            <span className={visible ? undefined : "sr-only"}>{texto}</span>
        </span>
    );
}
