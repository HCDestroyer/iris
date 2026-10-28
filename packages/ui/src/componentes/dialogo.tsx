"use client";

import { X } from "lucide-react";
import { Dialog } from "radix-ui";
import type { ComponentProps, ReactNode } from "react";
import { cn } from "../utilidades";
import { Boton } from "./boton";

/** Diálogo modal (shadcn `Dialog`) en vidrio fuerte. En el teléfono ocupa el ancho y sube desde abajo. */
export const Dialogo = Dialog.Root;
export const DialogoDisparador = Dialog.Trigger;
export const DialogoCerrar = Dialog.Close;

export type PropsDialogoContenido = Omit<ComponentProps<typeof Dialog.Content>, "title"> & {
    titulo: ReactNode;
    descripcion?: ReactNode;
    etiquetaCerrar?: string;
};

export function DialogoContenido({ titulo, descripcion, etiquetaCerrar = "Cerrar", className, children, ...props }: PropsDialogoContenido) {
    return (
        <Dialog.Portal>
            <Dialog.Overlay className="fixed inset-0 z-40 bg-black/45" />
            <Dialog.Content
                {...(descripcion ? {} : { "aria-describedby": undefined })}
                className={cn(
                    "glass-strong fixed z-50 flex max-h-[92dvh] w-full flex-col gap-4 overflow-y-auto p-5 shadow-xl",
                    "inset-x-0 bottom-0 rounded-t-[var(--hx-radius-card)]",
                    "sm:inset-auto sm:top-1/2 sm:left-1/2 sm:w-[min(36rem,calc(100vw-2rem))] sm:-translate-x-1/2 sm:-translate-y-1/2 sm:rounded-[var(--hx-radius-card)]",
                    className,
                )}
                {...props}
            >
                <div className="flex items-start justify-between gap-3">
                    <div className="grid gap-1">
                        <Dialog.Title className="text-lg font-semibold tracking-[-0.015em]">{titulo}</Dialog.Title>
                        {descripcion ? (
                            <Dialog.Description className="text-sm text-muted-foreground">{descripcion}</Dialog.Description>
                        ) : null}
                    </div>
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

/** Botones al pie del diálogo: en el teléfono, apilados con la acción principal arriba. */
export function DialogoPie({ className, ...props }: ComponentProps<"div">) {
    return <div className={cn("flex flex-col-reverse gap-2 sm:flex-row sm:justify-end", className)} {...props} />;
}
