import type { LucideIcon } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";
import { cn } from "../utilidades";

export type PropsEstadoVacio = Omit<ComponentProps<"div">, "title"> & {
    icono: LucideIcon;
    titulo: ReactNode;
    descripcion?: ReactNode;
    accion?: ReactNode;
};

/** Estado vacío: icono, título, explicación y, si aplica, la acción para salir de él. */
export function EstadoVacio({ icono: Icono, titulo, descripcion, accion, className, ...props }: PropsEstadoVacio) {
    return (
        <div
            data-slot="estado-vacio"
            className={cn("flex flex-col items-center gap-3 px-6 py-12 text-center", className)}
            {...props}
        >
            <span className="grid size-12 place-items-center rounded-full bg-elevated text-muted-foreground">
                <Icono aria-hidden className="size-6" />
            </span>
            <div className="grid max-w-md gap-1">
                <h2 className="text-base font-semibold tracking-[-0.015em]">{titulo}</h2>
                {descripcion ? <p className="text-sm text-muted-foreground">{descripcion}</p> : null}
            </div>
            {accion ? <div className="mt-1">{accion}</div> : null}
        </div>
    );
}
