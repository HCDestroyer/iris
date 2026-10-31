"use client";

import { tooltipVariants } from "@heroui/styles";
import type { ComponentProps, ReactElement, ReactNode } from "react";
import { Focusable, Tooltip, TooltipTrigger } from "react-aria-components";
import { cn } from "../utilidades";

export type PropsGlobo = {
    /** Texto del globo. No reemplaza al nombre accesible: un botón solo-icono sigue con `aria-label`. */
    texto: ReactNode;
    /** El elemento que lo muestra al pasar el puntero o al enfocarlo (p. ej. un `Boton`). */
    children: ReactElement;
    lado?: "top" | "bottom" | "start" | "end";
    /** Milisegundos antes de mostrarse (600 por defecto, como el tema). */
    retraso?: number;
    className?: string;
};

/**
 * Globo de ayuda (Tooltip de HeroUI sobre React Aria): aparece al pasar el puntero o al enfocar
 * con el teclado, y Escape lo cierra. El disparador es el propio hijo (`Focusable`): no agrega
 * otro elemento enfocable.
 */
export function Globo({ texto, children, lado = "top", retraso = 600, className }: PropsGlobo) {
    return (
        <TooltipTrigger delay={retraso} closeDelay={200}>
            <Focusable>{children as ComponentProps<typeof Focusable>["children"]}</Focusable>
            <Tooltip
                offset={6}
                placement={lado}
                data-slot="globo"
                className={tooltipVariants().base({
                    className: cn("glass-strong max-w-64 rounded-md px-2.5 py-1.5 text-xs text-foreground shadow-lg", className),
                })}
            >
                {texto}
            </Tooltip>
        </TooltipTrigger>
    );
}
