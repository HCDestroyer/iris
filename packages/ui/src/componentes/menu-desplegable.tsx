"use client";

import { Dropdown, Menu, Separator } from "@heroui/react";
import type { LucideIcon } from "lucide-react";
import { Children, Fragment, isValidElement, type ComponentProps, type ReactElement, type ReactNode } from "react";
import { Header, Pressable } from "react-aria-components";
import { cn } from "../utilidades";
import { esBotonAria } from "./boton";

/*
 * Menú desplegable: el `Dropdown` de HeroUI (`Dropdown`, `Dropdown.Popover`, `Dropdown.Menu`,
 * `Dropdown.Section`, `Dropdown.Item` y `Separator`, sobre React Aria) en la capa flotante sólida
 * del tema, con borde y sombra: menú del usuario y acciones de una fila. Teclado y foco los
 * resuelve React Aria (flechas, letras, Esc, vuelve al disparador).
 */

export type PropsMenuDesplegable = {
    open?: boolean;
    defaultOpen?: boolean;
    onOpenChange?: (abierto: boolean) => void;
    children?: ReactNode;
};

export function MenuDesplegable({ open, defaultOpen, onOpenChange, children }: PropsMenuDesplegable) {
    return (
        <Dropdown {...(open !== undefined ? { isOpen: open } : {})} {...(defaultOpen !== undefined ? { defaultOpen } : {})} {...(onOpenChange ? { onOpenChange } : {})}>
            {children}
        </Dropdown>
    );
}

/**
 * Disparador: el hijo recibe las props del menú (`aria-expanded`, `aria-haspopup`, teclado). Un
 * `Boton` (Button de React Aria) se conecta solo; otro elemento, por `Pressable`. `asChild` se
 * acepta por compatibilidad; siempre usa al hijo.
 */
export function MenuDesplegableDisparador({ children }: { asChild?: boolean; children: ReactElement }) {
    if (esBotonAria(children)) return children;
    return <Pressable>{children as ComponentProps<typeof Pressable>["children"]}</Pressable>;
}

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
        <Dropdown.Popover
            offset={sideOffset}
            placement={ubicacion}
            containerPadding={8}
            className={cn("grid max-w-[calc(100vw-1rem)] min-w-56 gap-0.5 border border-border p-1 outline-none", className)}
        >
            <Dropdown.Menu className="grid gap-0.5 p-0 outline-none">
                {etiquetas.length > 0 ? (
                    <Dropdown.Section className="grid gap-0.5">
                        <Header>{etiquetas}</Header>
                        {opciones}
                    </Dropdown.Section>
                ) : (
                    opciones
                )}
            </Dropdown.Menu>
        </Dropdown.Popover>
    );
}

export type PropsMenuEnLinea = {
    /** Nombre accesible del menú («Tu cuenta»). */
    etiqueta: string;
    className?: string;
    children?: ReactNode;
};

/**
 * El mismo menú, siempre a la vista (sin disparador): p. ej. la cuenta dentro de «Más» en el
 * teléfono. Mismos `MenuDesplegableEtiqueta`, `…Elemento` y `…Separador`; flechas para moverse.
 */
export function MenuEnLinea({ etiqueta, className, children }: PropsMenuEnLinea) {
    const nodos = aplanar(children);
    const etiquetas = nodos.filter((n) => isValidElement(n) && n.type === MenuDesplegableEtiqueta);
    const opciones = nodos.filter((n) => !(isValidElement(n) && n.type === MenuDesplegableEtiqueta));
    return (
        <Menu aria-label={etiqueta} className={cn("grid gap-0.5 p-0 outline-none", className)}>
            {etiquetas.length > 0 ? (
                <Dropdown.Section className="grid gap-0.5">
                    <Header>{etiquetas}</Header>
                    {opciones}
                </Dropdown.Section>
            ) : (
                opciones
            )}
        </Menu>
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

export function MenuDesplegableElemento({ icono: Icono, onSelect, disabled, className, textValue, children }: PropsMenuDesplegableElemento) {
    const texto = textValue ?? (typeof children === "string" ? children : undefined);
    return (
        <Dropdown.Item
            {...(onSelect ? { onAction: onSelect } : {})}
            isDisabled={disabled ?? false}
            {...(texto ? { textValue: texto } : {})}
            className={cn(
                "flex min-h-9 cursor-pointer items-center gap-2.5 rounded-md px-2.5 text-sm outline-none select-none",
                "data-focused:bg-[var(--overlay-hover)] data-hovered:bg-[var(--overlay-hover)] data-disabled:pointer-events-none data-disabled:opacity-55",
                className,
            )}
        >
            {Icono ? <Icono aria-hidden className="size-4 shrink-0 text-muted-foreground" /> : null}
            {children}
        </Dropdown.Item>
    );
}

export function MenuDesplegableEtiqueta({ className, ...props }: ComponentProps<"div">) {
    return <div className={cn("grid min-w-0 gap-0.5 px-2.5 py-2 text-sm", className)} {...props} />;
}

export function MenuDesplegableSeparador({ className }: { className?: string }) {
    return <Separator className={cn("my-1", className)} />;
}
