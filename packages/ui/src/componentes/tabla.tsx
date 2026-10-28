import type { ComponentProps } from "react";
import { cn } from "../utilidades";

/** Tabla con cifras tabulares; se desplaza en horizontal dentro de su contenedor. */
export function Tabla({ className, ...props }: ComponentProps<"table">) {
    return (
        <div data-slot="tabla-contenedor" className="relative w-full overflow-x-auto">
            <table data-slot="tabla" className={cn("w-full caption-bottom text-sm tabular-nums", className)} {...props} />
        </div>
    );
}

export function TablaEncabezado({ className, ...props }: ComponentProps<"thead">) {
    return <thead className={cn("[&_tr]:border-b", className)} {...props} />;
}

export function TablaCuerpo({ className, ...props }: ComponentProps<"tbody">) {
    return <tbody className={cn("[&_tr:last-child]:border-0", className)} {...props} />;
}

export function TablaFila({ className, ...props }: ComponentProps<"tr">) {
    return <tr className={cn("border-b transition-colors hover:bg-muted/60", className)} {...props} />;
}

export function TablaCelda({ className, ...props }: ComponentProps<"td">) {
    return <td className={cn("px-3 py-2.5 align-middle", className)} {...props} />;
}

export function TablaTitulo({ className, ...props }: ComponentProps<"th">) {
    return (
        <th
            className={cn("h-10 px-3 text-left align-middle text-xs font-medium text-muted-foreground", className)}
            {...props}
        />
    );
}
