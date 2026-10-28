import type { ComponentProps, ReactNode } from "react";
import { cn } from "../utilidades";
import { PieCampo, useIdsCampo } from "./campo";

export type PropsCasilla = Omit<ComponentProps<"input">, "type"> & {
    etiqueta: ReactNode;
    ayuda?: ReactNode;
    error?: string | undefined;
};

/**
 * Casilla de verificación nativa (accesible y sin trampas de foco) con etiqueta clicable, ayuda y
 * error. El color de marca solo marca la selección (`accent-color`).
 */
export function Casilla({ etiqueta, ayuda, error, id, className, ...props }: PropsCasilla) {
    const ids = useIdsCampo(id, error, ayuda);
    return (
        <div data-slot="casilla" className={cn("grid gap-1", className)}>
            <label htmlFor={ids.idCampo} className="inline-flex min-h-6 cursor-pointer items-center gap-2.5 text-sm font-medium">
                <input
                    type="checkbox"
                    className={cn(
                        "size-4 shrink-0 cursor-pointer rounded-sm border border-input accent-primary",
                        "focus-visible:outline-2 focus-visible:outline-offset-2 disabled:cursor-not-allowed disabled:opacity-55",
                    )}
                    {...ids.control}
                    {...props}
                />
                {etiqueta}
            </label>
            <div className="grid gap-1 pl-6.5">
                <PieCampo idAyuda={ids.idAyuda} idError={ids.idError} ayuda={ayuda} error={error} />
            </div>
        </div>
    );
}
