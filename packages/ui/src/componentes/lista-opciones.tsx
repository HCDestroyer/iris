"use client";

import { ListBox } from "@heroui/react";
import type { Key, ReactNode } from "react";
import { cn } from "../utilidades";

export type OpcionLista = {
    valor: string;
    /** Texto de la opción para el teclado (búsqueda por letra) y el lector de pantalla. */
    texto: string;
    /** Lo que se ve: puede llevar varias líneas, insignias… */
    contenido: ReactNode;
    deshabilitada?: boolean;
};

export type PropsListaOpciones = {
    /** Nombre de la lista (`aria-label`). */
    etiqueta: string;
    opciones: readonly OpcionLista[];
    /** Al elegir una opción (clic, Enter o espacio). */
    alElegir: (valor: string) => void;
    /** El foco entra en la primera opción al aparecer. */
    autoFocus?: boolean;
    className?: string;
};

/**
 * Lista de opciones grandes para elegir una (HeroUI `ListBox`, React Aria): flechas, Inicio/Fin,
 * búsqueda por letra, Enter o clic eligen. Para escoger entre pocas tarjetas con detalle (el
 * servicio de un cliente); para un campo de formulario, `Selector`.
 */
export function ListaOpciones({ etiqueta, opciones, alElegir, autoFocus, className }: PropsListaOpciones) {
    return (
        <ListBox
            data-slot="lista-opciones"
            aria-label={etiqueta}
            items={opciones.map((o) => ({ ...o, id: o.valor }))}
            disabledKeys={opciones.filter((o) => o.deshabilitada).map((o) => o.valor)}
            onAction={(clave: Key) => alElegir(String(clave))}
            {...(autoFocus ? { autoFocus: "first" as const } : {})}
            className={cn("grid gap-2 p-0", className)}
        >
            {(o) => (
                <ListBox.Item
                    id={o.valor}
                    textValue={o.texto}
                    className={cn(
                        "grid cursor-pointer gap-1 rounded-md border border-border bg-background p-3 text-sm",
                        "data-[hovered=true]:border-primary/60 data-[focused=true]:border-primary data-[focused=true]:bg-primary/10",
                    )}
                >
                    {o.contenido}
                </ListBox.Item>
            )}
        </ListBox>
    );
}
