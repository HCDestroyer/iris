"use client";

import { popoverVariants } from "@heroui/styles";
import type { ComponentProps, ReactElement, ReactNode } from "react";
import { Dialog, DialogTrigger, Heading, Popover, Pressable } from "react-aria-components";
import { cn } from "../utilidades";

export type PropsPanelEmergente = {
    /** El disparador (p. ej. un `Boton`): recibe `aria-expanded` y abre/cierra el panel. */
    disparador: ReactElement;
    /** Título del panel (nombre accesible del diálogo). */
    titulo: string;
    /** Muestra el título arriba; si no, solo lo usan los lectores de pantalla. */
    tituloVisible?: boolean;
    children: ReactNode;
    lado?: "top" | "bottom" | "start" | "end" | "bottom start" | "bottom end";
    abierto?: boolean;
    alCambiar?: (abierto: boolean) => void;
    className?: string;
};

/**
 * Panel emergente (Popover de HeroUI sobre React Aria) en vidrio fuerte, junto a su disparador:
 * filtros rápidos, detalle de un dato, ayuda con enlaces. Es un diálogo no modal: Escape o tocar
 * fuera lo cierran y el foco vuelve al disparador. Para formularios de más de 3 campos, una
 * pantalla propia.
 */
export function PanelEmergente({ disparador, titulo, tituloVisible = false, children, lado = "bottom", abierto, alCambiar, className }: PropsPanelEmergente) {
    const piezas = popoverVariants();
    return (
        <DialogTrigger {...(abierto !== undefined ? { isOpen: abierto } : {})} {...(alCambiar ? { onOpenChange: alCambiar } : {})}>
            <Pressable>{disparador as ComponentProps<typeof Pressable>["children"]}</Pressable>
            <Popover
                offset={8}
                placement={lado}
                containerPadding={8}
                data-slot="panel-emergente"
                className={piezas.base({ className: cn("glass-strong max-w-[min(24rem,calc(100vw-1rem))] p-0 shadow-xl", className) })}
            >
                <Dialog aria-label={tituloVisible ? undefined : titulo} className={piezas.dialog({ className: "grid gap-3 p-4 outline-none" })}>
                    {tituloVisible ? (
                        <Heading slot="title" className={piezas.heading({ className: "text-sm font-semibold" })}>
                            {titulo}
                        </Heading>
                    ) : null}
                    {children}
                </Dialog>
            </Popover>
        </DialogTrigger>
    );
}
