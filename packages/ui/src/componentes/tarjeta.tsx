import type { ComponentProps } from "react";
import { cn } from "../utilidades";

/** Tarjeta de contenido (material casi sólido). Nunca dentro de vidrio sobre vidrio. */
export function Tarjeta({ className, ...props }: ComponentProps<"div">) {
    return <div data-slot="tarjeta" className={cn("surface-card text-card-foreground", className)} {...props} />;
}

export function TarjetaEncabezado({ className, ...props }: ComponentProps<"div">) {
    return <div data-slot="tarjeta-encabezado" className={cn("grid gap-1 p-5 pb-3", className)} {...props} />;
}

export function TarjetaTitulo({ className, ...props }: ComponentProps<"h2">) {
    return (
        <h2
            data-slot="tarjeta-titulo"
            className={cn("text-base leading-tight font-semibold tracking-[-0.015em]", className)}
            {...props}
        />
    );
}

export function TarjetaDescripcion({ className, ...props }: ComponentProps<"p">) {
    return <p data-slot="tarjeta-descripcion" className={cn("text-sm text-muted-foreground", className)} {...props} />;
}

export function TarjetaContenido({ className, ...props }: ComponentProps<"div">) {
    return <div data-slot="tarjeta-contenido" className={cn("p-5 pt-0", className)} {...props} />;
}

export function TarjetaPie({ className, ...props }: ComponentProps<"div">) {
    return <div data-slot="tarjeta-pie" className={cn("flex items-center gap-2 p-5 pt-0", className)} {...props} />;
}
