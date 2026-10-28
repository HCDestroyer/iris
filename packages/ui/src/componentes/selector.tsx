import { ChevronDown } from "lucide-react";
import type { ComponentProps } from "react";
import { cn } from "../utilidades";
import { PieCampo, useIdsCampo, type PropsEnvolturaCampo } from "./campo";
import { Etiqueta } from "./etiqueta";

export type PropsSelector = ComponentProps<"select"> & { tamano?: "md" | "sm" };

/**
 * Lista desplegable nativa con el estilo de Horus: en el teléfono abre el selector del sistema,
 * accesible y sin trampas de foco.
 */
export function Selector({ className, tamano = "md", children, ...props }: PropsSelector) {
    return (
        <span data-slot="selector" className={cn("relative inline-flex w-full min-w-0", className)}>
            <select
                className={cn(
                    "w-full min-w-0 appearance-none rounded-md border border-input bg-background pr-8 pl-3 text-foreground",
                    "transition-[border-color,box-shadow] duration-150 focus-visible:border-primary focus-visible:outline-2 focus-visible:outline-offset-0",
                    "disabled:cursor-not-allowed disabled:opacity-55 aria-invalid:border-error",
                    tamano === "sm" ? "h-8 text-sm" : "h-10 text-base sm:text-sm",
                )}
                {...props}
            >
                {children}
            </select>
            <ChevronDown aria-hidden className="pointer-events-none absolute top-1/2 right-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
        </span>
    );
}

export type PropsCampoSelector = ComponentProps<"select"> & PropsEnvolturaCampo;

/** Etiqueta + lista desplegable + ayuda/error. */
export function CampoSelector({ etiqueta, error, ayuda, id, className, children, ...props }: PropsCampoSelector) {
    const ids = useIdsCampo(id, error, ayuda);
    return (
        <div className={cn("grid gap-1.5", className)}>
            <Etiqueta htmlFor={ids.idCampo}>{etiqueta}</Etiqueta>
            <Selector {...ids.control} {...props}>
                {children}
            </Selector>
            <PieCampo idAyuda={ids.idAyuda} idError={ids.idError} ayuda={ayuda} error={error} />
        </div>
    );
}
