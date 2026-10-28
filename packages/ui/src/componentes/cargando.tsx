import { LoaderCircle } from "lucide-react";
import type { ComponentProps } from "react";
import { cn } from "../utilidades";

export type PropsCargando = ComponentProps<"span"> & {
    /** Texto para lectores de pantalla (y visible si `visible`). */
    texto?: string;
    visible?: boolean;
};

export function Cargando({ texto = "Cargando…", visible = false, className, ...props }: PropsCargando) {
    return (
        <span
            role="status"
            data-slot="cargando"
            className={cn("inline-flex items-center gap-2 text-sm text-muted-foreground", className)}
            {...props}
        >
            <LoaderCircle aria-hidden className="size-4 animate-spin" />
            <span className={visible ? undefined : "sr-only"}>{texto}</span>
        </span>
    );
}
