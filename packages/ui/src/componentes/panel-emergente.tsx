"use client";

import { Popover } from "@heroui/react";
import type { ComponentProps, ReactElement, ReactNode } from "react";
import { Pressable } from "react-aria-components";
import { cn } from "../utilidades";
import { esBotonAria } from "./boton";

export type PropsPanelEmergente = {
    /** El disparador (p. ej. un `Boton`): recibe `aria-expanded` y abre/cierra el panel. */
    disparador: ReactElement;
    /** Título del panel (nombre accesible del diálogo). */
    titulo: string;
    /** Muestra el título arriba; si no, solo lo usan los lectores de pantalla. */
    tituloVisible?: boolean;
    children: ReactNode;
    lado?: "top" | "bottom" | "start" | "end" | "top start" | "top end" | "bottom start" | "bottom end";
    abierto?: boolean;
    alCambiar?: (abierto: boolean) => void;
    className?: string;
};

/**
 * Panel emergente: el `Popover` de HeroUI (`Popover`, `Popover.Content`, `Popover.Dialog`,
 * `Popover.Heading`) junto a su disparador, en la capa flotante sólida del tema (con borde y
 * sombra): filtros rápidos, detalle de un dato, ayuda con enlaces. Es un diálogo no modal: Escape
 * o tocar fuera lo cierran y el foco vuelve al disparador. Para formularios de más de 3 campos,
 * una pantalla propia.
 */
export function PanelEmergente({ disparador, titulo, tituloVisible = false, children, lado = "bottom", abierto, alCambiar, className }: PropsPanelEmergente) {
    return (
        <Popover {...(abierto !== undefined ? { isOpen: abierto } : {})} {...(alCambiar ? { onOpenChange: alCambiar } : {})}>
            {/* Un `Boton` (Button de React Aria) se conecta solo; otro elemento, con `Pressable`. */}
            {esBotonAria(disparador) ? disparador : <Pressable>{disparador as ComponentProps<typeof Pressable>["children"]}</Pressable>}
            <Popover.Content
                offset={8}
                placement={lado}
                containerPadding={8}
                data-slot="panel-emergente"
                className={cn("max-w-[min(24rem,calc(100vw-1rem))] border border-border p-0", className)}
            >
                <Popover.Dialog aria-label={tituloVisible ? undefined : titulo} className="grid gap-3 p-4 outline-none">
                    {tituloVisible ? <Popover.Heading className="text-sm font-semibold">{titulo}</Popover.Heading> : null}
                    {children}
                </Popover.Dialog>
            </Popover.Content>
        </Popover>
    );
}
