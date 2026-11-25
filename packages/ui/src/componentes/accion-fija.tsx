import type { ComponentProps, ReactNode } from "react";
import { cn } from "../utilidades";

export type PropsAccionFija = Omit<ComponentProps<"div">, "role"> & {
    /** Nombre accesible del grupo («Acciones de la orden»). */
    etiqueta: string;
    children: ReactNode;
    /**
     * `telefono` (por omisión): fija abajo solo en el teléfono (debajo de `lg`); en escritorio queda
     * en su lugar al pie del contenido. `siempre`: fija abajo en todo tamaño (columna de la app de
     * campo).
     */
    modo?: "telefono" | "siempre";
};

/**
 * Acción fija abajo (rediseño, regla 12): en el teléfono la acción principal de una ficha o de un
 * paso queda a la vista, sobre una franja de tarjeta con borde arriba y el área segura del teléfono
 * (barra de inicio del iPhone). Mientras está, ocupa el lugar de la isla de menú. Los botones van
 * dentro: el principal con `className="flex-1"` (crece) y los secundarios redondos.
 */
export function AccionFija({ etiqueta, children, modo = "telefono", className, ...props }: PropsAccionFija) {
    return (
        <div
            role="group"
            aria-label={etiqueta}
            data-slot="accion-fija"
            data-modo={modo}
            className={cn(
                "z-30 flex min-w-0 items-center gap-2.5 border-t border-border bg-[var(--iris-tarjeta)] px-4 pt-3 pb-[max(1rem,env(safe-area-inset-bottom))]",
                modo === "siempre" ? "sticky bottom-0" : "fixed inset-x-0 bottom-0 lg:static lg:rounded-[var(--iris-radio-tarjeta)] lg:border lg:pb-3",
                "[&_.button]:h-[3.125rem] [&_.button]:text-[0.9375rem]",
                className,
            )}
            {...props}
        >
            {children}
        </div>
    );
}
