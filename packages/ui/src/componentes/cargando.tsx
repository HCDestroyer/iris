import { spinnerVariants } from "@heroui/styles";
import { LoaderCircle } from "lucide-react";
import type { ComponentProps } from "react";
import { cn } from "../utilidades";

export type PropsCargando = ComponentProps<"span"> & {
    /** Texto para lectores de pantalla (y visible si `visible`). */
    texto?: string;
    visible?: boolean;
};

/** Indicador de carga (estilo de HeroUI `Spinner`, icono de Lucide) con su texto en `role="status"`. */
export function Cargando({ texto = "Cargando…", visible = false, className, ...props }: PropsCargando) {
    return (
        <span
            role="status"
            data-slot="cargando"
            className={cn("inline-flex items-center gap-2 text-sm text-muted-foreground", className)}
            {...props}
        >
            <LoaderCircle aria-hidden className={spinnerVariants({ color: "current", size: "sm", className: "size-4" })} />
            <span className={visible ? undefined : "sr-only"}>{texto}</span>
        </span>
    );
}
