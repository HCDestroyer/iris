"use client";

import { Paperclip, type LucideIcon } from "lucide-react";
import { useId, type ChangeEvent, type ReactNode } from "react";
import { cn } from "../utilidades";
import { variantesBoton, type PropsBoton } from "./boton";

export type PropsSelectorArchivo = {
    /** Nombre accesible (y globo): «Adjuntar archivo». */
    etiqueta: string;
    /** Texto visible; sin él, solo el icono. */
    children?: ReactNode;
    alElegir: (archivo: File) => void;
    accept?: string;
    disabled?: boolean;
    icono?: LucideIcon;
    variante?: PropsBoton["variante"];
    tamano?: PropsBoton["tamano"];
    className?: string;
};

/**
 * Botón para elegir un archivo (sin cámara, a diferencia de `Camara`): el `<input type="file">`
 * real queda oculto pero enfocable dentro de la etiqueta, así el teclado y los lectores de pantalla
 * lo usan. Se limpia después de cada elección para poder elegir el mismo archivo otra vez.
 */
export function SelectorArchivo({ etiqueta, children, alElegir, accept, disabled, icono: Icono = Paperclip, variante = "fantasma", tamano = "icono-sm", className }: PropsSelectorArchivo) {
    const id = useId();
    const alCambiar = (e: ChangeEvent<HTMLInputElement>) => {
        const archivo = e.target.files?.[0];
        e.target.value = "";
        if (archivo) alElegir(archivo);
    };
    return (
        <label
            htmlFor={id}
            title={etiqueta}
            data-slot="selector-archivo"
            aria-disabled={disabled || undefined}
            className={cn(
                variantesBoton({ variante, tamano }),
                "cursor-pointer has-[input:focus-visible]:outline-2 has-[input:focus-visible]:outline-offset-2",
                disabled && "pointer-events-none opacity-55",
                className,
            )}
        >
            <Icono aria-hidden />
            {children}
            <input id={id} type="file" accept={accept} disabled={disabled} aria-label={etiqueta} className="sr-only" onChange={alCambiar} />
        </label>
    );
}
