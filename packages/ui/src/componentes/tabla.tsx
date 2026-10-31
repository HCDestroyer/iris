import { tableVariants } from "@heroui/styles";
import type { ComponentProps } from "react";
import { cn } from "../utilidades";

/*
 * Tabla semántica (`<table>`, filas y celdas nativas) con las clases de HeroUI `Table`. No usa la
 * `Table` de React Aria (rejilla con colección y teclado propio) porque las filas de Iris llevan
 * enlaces y botones libres; eso queda para cuando una pantalla necesite orden o selección.
 * Cifras tabulares; se desplaza en horizontal dentro de su contenedor.
 */
const piezas = tableVariants({ variant: "secondary" });

export function Tabla({ className, ...props }: ComponentProps<"table">) {
    return (
        <div data-slot="tabla-contenedor" className={piezas.scrollContainer({ className: "relative w-full overflow-x-auto" })}>
            <table data-slot="tabla" className={piezas.content({ className: cn("w-full caption-bottom border-collapse text-sm tabular-nums", className) })} {...props} />
        </div>
    );
}

export function TablaEncabezado({ className, ...props }: ComponentProps<"thead">) {
    return <thead className={piezas.header({ className: cn("border-0 bg-transparent [&_tr]:border-b", className) })} {...props} />;
}

export function TablaCuerpo({ className, ...props }: ComponentProps<"tbody">) {
    return <tbody className={piezas.body({ className: cn("[&_tr:last-child]:border-0", className) })} {...props} />;
}

export function TablaFila({ className, ...props }: ComponentProps<"tr">) {
    return <tr className={piezas.row({ className: cn("border-b border-border transition-colors hover:bg-background-secondary/60", className) })} {...props} />;
}

export function TablaCelda({ className, ...props }: ComponentProps<"td">) {
    return <td className={piezas.cell({ className: cn("h-auto border-0 bg-transparent px-3 py-2.5 align-middle", className) })} {...props} />;
}

export function TablaTitulo({ className, ...props }: ComponentProps<"th">) {
    return (
        <th
            className={piezas.column({ className: cn("h-10 bg-transparent px-3 py-0 text-left align-middle text-xs font-medium text-muted-foreground", className) })}
            {...props}
        />
    );
}
