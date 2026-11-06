"use client";

import { Label, SearchField } from "@heroui/react";
import { Search, X } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "../utilidades";

export type PropsCampoBusqueda = {
    /** Nombre del campo; con `etiquetaVisible={false}` solo lo lee el lector de pantalla. */
    etiqueta: ReactNode;
    etiquetaVisible?: boolean;
    valor: string;
    alCambiar: (texto: string) => void;
    placeholder?: string;
    maxLength?: number;
    tamano?: "md" | "sm";
    id?: string;
    className?: string;
    autoFocus?: boolean;
};

/**
 * Caja de búsqueda: el `SearchField` de HeroUI (React Aria), un `<input type="search">` con la
 * lupa de Lucide y la «X» para borrar (también Escape). Para filtrar listas mientras se escribe.
 */
export function CampoBusqueda({ etiqueta, etiquetaVisible = true, valor, alCambiar, placeholder, maxLength, tamano = "md", id, className, autoFocus }: PropsCampoBusqueda) {
    const chico = tamano === "sm";
    return (
        <SearchField
            data-slot="campo-busqueda"
            className={cn("grid min-w-0 gap-1.5", className)}
            value={valor}
            onChange={alCambiar}
            {...(id ? { id } : {})}
            {...(autoFocus ? { autoFocus } : {})}
            fullWidth
        >
            <Label className={cn("text-sm font-medium", !etiquetaVisible && "sr-only")}>{etiqueta}</Label>
            <SearchField.Group className={cn("shadow-none", chico ? "h-8 rounded-lg" : "h-10")}>
                <SearchField.SearchIcon>
                    <Search aria-hidden className="size-4 text-muted-foreground" />
                </SearchField.SearchIcon>
                <SearchField.Input placeholder={placeholder} autoComplete="off" {...(maxLength ? { maxLength } : {})} className={chico ? "text-[0.8125rem]" : "text-base sm:text-sm"} />
                <SearchField.ClearButton aria-label="Borrar la búsqueda">
                    <X aria-hidden className="size-4" />
                </SearchField.ClearButton>
            </SearchField.Group>
        </SearchField>
    );
}
