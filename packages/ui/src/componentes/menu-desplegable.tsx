"use client";

import { dropdownVariants, menuItemVariants } from "@heroui/styles";
import type { LucideIcon } from "lucide-react";
import { Children, Fragment, isValidElement, type ComponentProps, type ReactElement, type ReactNode } from "react";
import { Header, Menu, MenuItem, MenuSection, MenuTrigger, Popover, Pressable, Separator } from "react-aria-components";
import { cn } from "../utilidades";

/*
 * Menú desplegable sobre HeroUI `Dropdown` (React Aria `MenuTrigger` + `Popover` + `Menu`) en
 * vidrio fuerte: menú del usuario y acciones de una fila. Teclado y foco los resuelve React Aria
 * (flechas, letras, Esc, vuelve al disparador).
 */

export type PropsMenuDesplegable = {
    open?: boolean;
    defaultOpen?: boolean;
    onOpenChange?: (abierto: boolean) => void;
    children?: ReactNode;
};

export function MenuDesplegable({ open, defaultOpen, onOpenChange, children }: PropsMenuDesplegable) {
    return (
        <MenuTrigger {...(open !== undefined ? { isOpen: open } : {})} {...(defaultOpen !== undefined ? { defaultOpen } : {})} {...(onOpenChange ? { onOpenChange } : {})}>
            {children}
        </MenuTrigger>
    );
}

/**
 * Disparador: el hijo (un `Boton`) recibe por `Pressable` las props del menú (`aria-expanded`,
 * `aria-haspopup`, teclado). `asChild` se acepta por compatibilidad; siempre usa al hijo.
 */
export function MenuDesplegableDisparador({ children }: { asChild?: boolean; children: ReactElement }) {
    return <Pressable>{children as ComponentProps<typeof Pressable>["children"]}</Pressable>;
}

const piezasMenu = dropdownVariants();

export type PropsMenuDesplegableContenido = {
    className?: string;
    /** Distancia al disparador en px. */
    sideOffset?: number;
    align?: "start" | "end" | "center";
    children?: ReactNode;
};

function aplanar(nodos: ReactNode): ReactNode[] {
    return Children.toArray(nodos).flatMap((nodo) =>
        isValidElement<{ children?: ReactNode }>(nodo) && nodo.type === Fragment ? aplanar(nodo.props.children) : [nodo],
    );
}

/**
 * Lista del menú. Las `MenuDesplegableEtiqueta` (p. ej. nombre y correo) van arriba, como
 * encabezado de la sección (dentro del `role="menu"`, sin ser opciones); el resto (elementos y
 * separadores) son las opciones.
 */
export function MenuDesplegableContenido({ className, sideOffset = 6, align = "end", children }: PropsMenuDesplegableContenido) {
    const nodos = aplanar(children);
    const etiquetas = nodos.filter((n) => isValidElement(n) && n.type === MenuDesplegableEtiqueta);
    const opciones = nodos.filter((n) => !(isValidElement(n) && n.type === MenuDesplegableEtiqueta));
    const ubicacion = align === "start" ? "bottom start" : align === "center" ? "bottom" : "bottom end";
    return (
        <Popover
            offset={sideOffset}
            placement={ubicacion}
            containerPadding={8}
            className={piezasMenu.popover({
                className: cn("glass-strong grid max-w-[calc(100vw-1rem)] min-w-56 gap-0.5 rounded-lg border p-1 shadow-xl outline-none", className),
            })}
        >
            <Menu className="grid gap-0.5 outline-none">
                {etiquetas.length > 0 ? (
                    <MenuSection className="grid gap-0.5">
                        <Header>{etiquetas}</Header>
                        {opciones}
                    </MenuSection>
                ) : (
                    opciones
                )}
            </Menu>
        </Popover>
    );
}

export type PropsMenuDesplegableElemento = {
    icono?: LucideIcon;
    /** Al elegir la opción (clic, Enter o espacio); el menú se cierra solo. */
    onSelect?: () => void;
    disabled?: boolean;
    className?: string;
    /** Texto para buscar con el teclado si el contenido no es texto. */
    textValue?: string;
    children?: ReactNode;
};

const piezasElemento = menuItemVariants();

export function MenuDesplegableElemento({ icono: Icono, onSelect, disabled, className, textValue, children }: PropsMenuDesplegableElemento) {
    const texto = textValue ?? (typeof children === "string" ? children : undefined);
    return (
        <MenuItem
            {...(onSelect ? { onAction: onSelect } : {})}
            isDisabled={disabled ?? false}
            {...(texto ? { textValue: texto } : {})}
            className={piezasElemento.item({
                className: cn(
                    "flex min-h-9 cursor-pointer items-center gap-2.5 rounded-md px-2.5 text-sm outline-none select-none",
                    "data-focused:bg-elevated data-hovered:bg-elevated data-disabled:pointer-events-none data-disabled:opacity-55",
                    className,
                ),
            })}
        >
            {Icono ? <Icono aria-hidden className="size-4 shrink-0 text-muted-foreground" /> : null}
            {children}
        </MenuItem>
    );
}

export function MenuDesplegableEtiqueta({ className, ...props }: ComponentProps<"div">) {
    return <div className={cn("grid min-w-0 gap-0.5 px-2.5 py-2 text-sm", className)} {...props} />;
}

export function MenuDesplegableSeparador({ className }: { className?: string }) {
    return <Separator className={cn("my-1 h-px border-0 bg-border", className)} />;
}
