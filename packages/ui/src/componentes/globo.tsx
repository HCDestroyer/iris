"use client";

import { Tooltip } from "@heroui/react";
import type { ComponentProps, ReactElement, ReactNode } from "react";
import { Focusable } from "react-aria-components";
import { cn } from "../utilidades";
import { esBotonAria } from "./boton";

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
 * Globo de ayuda: el `Tooltip` de HeroUI (`Tooltip` + `Tooltip.Content`, sobre React Aria) en la
 * capa flotante sólida del tema. Aparece al pasar el puntero o al enfocar con el teclado, y Escape
 * lo cierra. El disparador es el propio hijo: un `Boton` se conecta solo y otro elemento va con
 * `Focusable`; no se agrega otro elemento enfocable.
 */
export function Globo({ texto, children, lado = "top", retraso = 600, className }: PropsGlobo) {
    return (
        <Tooltip delay={retraso} closeDelay={200}>
            {esBotonAria(children) ? children : <Focusable>{children as ComponentProps<typeof Focusable>["children"]}</Focusable>}
            <Tooltip.Content
                offset={6}
                placement={lado}
                data-slot="globo"
                className={cn("max-w-64 rounded-md border border-border px-2.5 py-1.5 text-xs break-normal text-foreground", className)}
            >
                {texto}
            </Tooltip.Content>
        </Tooltip>
    );
}
