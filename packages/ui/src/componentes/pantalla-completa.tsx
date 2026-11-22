"use client";

import { Modal } from "@heroui/react";
import type { ComponentProps } from "react";
import { cn } from "../utilidades";

export type PropsPantallaCompleta = Omit<ComponentProps<"div">, "role" | "aria-label" | "aria-modal"> & {
    /** Nombre accesible del diálogo (p. ej. «Llamada con María Castillo»). */
    etiqueta: string;
};

/**
 * Una pantalla que tapa toda la app mientras dura algo que no se cierra tocando fuera (p. ej. una
 * llamada en curso): el `Modal` de HeroUI (React Aria) a pantalla completa, con foco contenido y el
 * resto de la página inerte. Ni Escape ni tocar fuera la cierran: se sale por sus propias acciones
 * (colgar), y se desmonta cuando quien la dibuja deja de dibujarla. Fondo sólido del tema y los
 * márgenes seguros del teléfono los pone quien la usa en `className`.
 */
export function PantallaCompleta({ etiqueta, className, children, ...props }: PropsPantallaCompleta) {
    return (
        <Modal.Backdrop isOpen isDismissable={false} isKeyboardDismissDisabled variant="transparent" className="z-[60] data-exiting:animate-none data-exiting:transition-none">
            <Modal.Container placement="center" className="size-full p-0 data-exiting:animate-none motion-reduce:animate-none">
                <Modal.Dialog
                    aria-label={etiqueta}
                    className={cn("fixed inset-0 m-0 h-dvh max-h-none w-full max-w-none rounded-none border-0 bg-background p-0 shadow-none", className)}
                    {...props}
                >
                    {children}
                </Modal.Dialog>
            </Modal.Container>
        </Modal.Backdrop>
    );
}
