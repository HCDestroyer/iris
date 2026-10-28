import type { ComponentProps } from "react";
import { cn } from "../utilidades";
import { PieCampo, useIdsCampo, type PropsEnvolturaCampo } from "./campo";
import { Etiqueta } from "./etiqueta";

/** Texto de varias líneas (16 px para que iOS no haga zoom). */
export function AreaTexto({ className, rows = 4, ...props }: ComponentProps<"textarea">) {
    return (
        <textarea
            data-slot="area-texto"
            rows={rows}
            className={cn(
                "min-h-20 w-full min-w-0 rounded-md border border-input bg-background px-3 py-2 text-base text-foreground",
                "placeholder:text-dimmed transition-[border-color,box-shadow] duration-150",
                "focus-visible:border-primary focus-visible:outline-2 focus-visible:outline-offset-0",
                "disabled:cursor-not-allowed disabled:opacity-55 aria-invalid:border-error",
                className,
            )}
            {...props}
        />
    );
}

export type PropsCampoArea = ComponentProps<"textarea"> & PropsEnvolturaCampo;

/** Etiqueta + área de texto + ayuda/error. */
export function CampoArea({ etiqueta, error, ayuda, id, className, ...props }: PropsCampoArea) {
    const ids = useIdsCampo(id, error, ayuda);
    return (
        <div className={cn("grid gap-1.5", className)}>
            <Etiqueta htmlFor={ids.idCampo}>{etiqueta}</Etiqueta>
            <AreaTexto {...ids.control} {...props} />
            <PieCampo idAyuda={ids.idAyuda} idError={ids.idError} ayuda={ayuda} error={error} />
        </div>
    );
}
