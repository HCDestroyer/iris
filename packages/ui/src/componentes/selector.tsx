"use client";

import { inputVariants } from "@heroui/styles";
import { ChevronDown } from "lucide-react";
import type { ComponentProps } from "react";
import { cn } from "../utilidades";
import { PieCampo, useIdsCampo, type PropsEnvolturaCampo } from "./campo";
import { useErrorEstable } from "./error-estable";
import { Etiqueta } from "./etiqueta";

export type PropsSelector = ComponentProps<"select"> & { tamano?: "md" | "sm" };

/**
 * Lista desplegable nativa con el estilo de campo de HeroUI (`input`): en el teléfono abre el
 * selector del sistema, accesible y sin trampas de foco. Sigue nativa a propósito (fase 0):
 * `register` de react-hook-form y `selectOption` de las pruebas la usan tal cual.
 */
export function Selector({ className, tamano = "md", children, ...props }: PropsSelector) {
    return (
        <span data-slot="selector" className={cn("relative inline-flex w-full min-w-0", className)}>
            <select
                className={cn(
                    inputVariants({ fullWidth: true }),
                    "min-w-0 cursor-pointer appearance-none py-0 pr-8 pl-3",
                    "disabled:cursor-not-allowed aria-invalid:border-error",
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
    // Un error que aparece al salir del campo no mueve el botón que se está pulsando.
    const visible = useErrorEstable(error);
    const ids = useIdsCampo(id, visible, ayuda);
    return (
        <div className={cn("grid gap-1.5", className)}>
            <Etiqueta htmlFor={ids.idCampo}>{etiqueta}</Etiqueta>
            <Selector {...ids.control} {...props}>
                {children}
            </Selector>
            <PieCampo idAyuda={ids.idAyuda} idError={ids.idError} ayuda={ayuda} error={visible} />
        </div>
    );
}
