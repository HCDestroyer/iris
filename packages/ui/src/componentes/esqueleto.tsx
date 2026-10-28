import type { ComponentProps } from "react";
import { cn } from "../utilidades";

/** Marcador de carga. Nunca se rellena con ceros: es un hueco hasta que llega el dato. */
export function Esqueleto({ className, ...props }: ComponentProps<"div">) {
    return (
        <div
            data-slot="esqueleto"
            aria-hidden
            className={cn("animate-pulse rounded-md bg-elevated", className)}
            {...props}
        />
    );
}
