"use client";

import { Disclosure } from "@heroui/react";
import { ChevronDown } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "../utilidades";

export type PropsDesplegable = {
    /** Texto del disparador («Avanzado», «Cómo comprobarlo…»). */
    titulo: ReactNode;
    children: ReactNode;
    /** Abierto al entrar. Cerrado por omisión, como un `<details>`. */
    abierto?: boolean;
    /** Nivel del encabezado que envuelve al disparador (3 por omisión). */
    nivel?: 2 | 3 | 4 | 5 | 6;
    /** Clases del contenedor (borde, márgenes). */
    className?: string;
    /** Clases del disparador (tamaño y color del texto). */
    claseTitulo?: string;
};

/**
 * «Ver más» que muestra u oculta un bloque de ayuda o de ajustes avanzados: el `Disclosure` de
 * HeroUI (React Aria: botón con `aria-expanded` y `aria-controls`, panel con su región) en lugar de
 * `<details>`. El chevron es el de Lucide y gira al abrir.
 */
export function Desplegable({ titulo, children, abierto = false, nivel = 3, className, claseTitulo }: PropsDesplegable) {
    return (
        <Disclosure defaultExpanded={abierto} data-slot="desplegable" className={className}>
            <Disclosure.Heading level={nivel} className="m-0 text-sm font-semibold">
                <Disclosure.Trigger className={cn("inline-flex items-center gap-1.5 rounded-sm text-left text-sm font-semibold text-muted-foreground hover:text-foreground", claseTitulo)}>
                    {titulo}
                    <Disclosure.Indicator className="ms-0">
                        <ChevronDown aria-hidden />
                    </Disclosure.Indicator>
                </Disclosure.Trigger>
            </Disclosure.Heading>
            <Disclosure.Content>
                <div>{children}</div>
            </Disclosure.Content>
        </Disclosure>
    );
}
