"use client";

import { Spinner } from "@heroui/react";
import type { ComponentProps } from "react";
import { cn } from "../utilidades";

export type PropsCargando = ComponentProps<"span"> & {
    /** Texto para lectores de pantalla (y visible si `visible`). */
    texto?: string;
    visible?: boolean;
};

/**
 * Indicador de carga: el `Spinner` de HeroUI (un indicador animado, no un icono) en el color del
 * texto, con su texto en `role="status"`.
 */
export function Cargando({ texto = "Cargando…", visible = false, className, ...props }: PropsCargando) {
    return (
        <span role="status" data-slot="cargando" className={cn("inline-flex items-center gap-2 text-sm text-muted-foreground", className)} {...props}>
            <Spinner color="current" size="sm" role="presentation" aria-label={undefined} aria-hidden className="size-4" />
            <span className={visible ? undefined : "sr-only"}>{texto}</span>
        </span>
    );
}
