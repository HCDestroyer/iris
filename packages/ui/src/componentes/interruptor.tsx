"use client";

import { Switch } from "@heroui/react";
import { cn } from "../utilidades";

export type PropsInterruptor = {
    etiqueta: string;
    descripcion?: string;
    seleccionado: boolean;
    alCambiar: (seleccionado: boolean) => void;
    deshabilitado?: boolean;
    className?: string;
};

/**
 * Interruptor encendido/apagado: el `Switch` de HeroUI (React Aria, `role="switch"`), con su
 * etiqueta y descripción en una fila de 48 px o más; toda la fila lo cambia.
 */
export function Interruptor({ etiqueta, descripcion, seleccionado, alCambiar, deshabilitado = false, className }: PropsInterruptor) {
    return (
        <Switch isSelected={seleccionado} onChange={alCambiar} isDisabled={deshabilitado} data-slot="interruptor" className={cn("flex min-h-12 w-full items-center", className)}>
            <Switch.Content className="flex min-h-12 w-full items-center justify-between gap-3">
                <span className="grid min-w-0 gap-0.5 text-left">
                    <span className="font-semibold">{etiqueta}</span>
                    {descripcion ? <span className="text-sm text-muted-foreground">{descripcion}</span> : null}
                </span>
                <Switch.Control className="shrink-0">
                    <Switch.Thumb />
                </Switch.Control>
            </Switch.Content>
        </Switch>
    );
}
