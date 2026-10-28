"use client";

import { X } from "lucide-react";
import { Dialog } from "radix-ui";
import type { ComponentProps, ReactNode } from "react";
import { cn } from "../utilidades";
import { Boton } from "./boton";

/** Panel lateral (shadcn `Sheet`) en vidrio fuerte. Para el menú en teléfono. */
export const Hoja = Dialog.Root;
export const HojaDisparador = Dialog.Trigger;
export const HojaCerrar = Dialog.Close;

export type PropsHojaContenido = ComponentProps<typeof Dialog.Content> & {
    titulo: ReactNode;
    /** Texto del botón de cerrar para lectores de pantalla. */
    etiquetaCerrar?: string;
    lado?: "izquierda" | "derecha";
};

export function HojaContenido({
    titulo,
    etiquetaCerrar = "Cerrar",
    lado = "izquierda",
    className,
    children,
    ...props
}: PropsHojaContenido) {
    return (
        <Dialog.Portal>
            <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40" />
            <Dialog.Content
                aria-describedby={undefined}
                className={cn(
                    "glass-strong fixed inset-y-0 z-50 flex w-[min(20rem,85vw)] flex-col gap-2 p-4 shadow-xl",
                    lado === "izquierda" ? "left-0 rounded-l-none" : "right-0 rounded-r-none",
                    className,
                )}
                {...props}
            >
                <div className="flex items-center justify-between gap-2">
                    <Dialog.Title className="text-base font-semibold">{titulo}</Dialog.Title>
                    <Dialog.Close asChild>
                        <Boton variante="fantasma" tamano="icono-sm" aria-label={etiquetaCerrar} title={etiquetaCerrar}>
                            <X />
                        </Boton>
                    </Dialog.Close>
                </div>
                {children}
            </Dialog.Content>
        </Dialog.Portal>
    );
}
