"use client";

import type { LucideIcon } from "lucide-react";
import { DropdownMenu } from "radix-ui";
import type { ComponentProps } from "react";
import { cn } from "../utilidades";

/**
 * Menú desplegable (shadcn `DropdownMenu`) en vidrio fuerte: menú del usuario y acciones de una
 * fila. Teclado y foco los resuelve Radix (flechas, Esc, vuelve al disparador).
 */
export const MenuDesplegable = DropdownMenu.Root;
export const MenuDesplegableDisparador = DropdownMenu.Trigger;

export function MenuDesplegableContenido({ className, sideOffset = 6, align = "end", ...props }: ComponentProps<typeof DropdownMenu.Content>) {
    return (
        <DropdownMenu.Portal>
            <DropdownMenu.Content
                sideOffset={sideOffset}
                align={align}
                collisionPadding={8}
                className={cn(
                    "glass-strong z-50 grid max-w-[calc(100vw-1rem)] min-w-56 gap-0.5 rounded-lg border p-1 shadow-xl",
                    className,
                )}
                {...props}
            />
        </DropdownMenu.Portal>
    );
}

export type PropsMenuDesplegableElemento = ComponentProps<typeof DropdownMenu.Item> & {
    icono?: LucideIcon;
};

export function MenuDesplegableElemento({ icono: Icono, className, children, ...props }: PropsMenuDesplegableElemento) {
    return (
        <DropdownMenu.Item
            className={cn(
                "flex min-h-9 cursor-pointer items-center gap-2.5 rounded-md px-2.5 text-sm outline-none select-none",
                "data-[highlighted]:bg-elevated data-[disabled]:pointer-events-none data-[disabled]:opacity-55",
                className,
            )}
            {...props}
        >
            {Icono ? <Icono aria-hidden className="size-4 shrink-0 text-muted-foreground" /> : null}
            {children}
        </DropdownMenu.Item>
    );
}

export function MenuDesplegableEtiqueta({ className, ...props }: ComponentProps<typeof DropdownMenu.Label>) {
    return <DropdownMenu.Label className={cn("grid min-w-0 gap-0.5 px-2.5 py-2 text-sm", className)} {...props} />;
}

export function MenuDesplegableSeparador({ className, ...props }: ComponentProps<typeof DropdownMenu.Separator>) {
    return <DropdownMenu.Separator className={cn("my-1 h-px bg-border", className)} {...props} />;
}
