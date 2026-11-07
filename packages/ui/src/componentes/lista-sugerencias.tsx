"use client";

import type { KeyboardEvent, ReactNode } from "react";
import { cn } from "../utilidades";

/*
 * Sugerencias bajo (o sobre) un área de texto que sigue con el foco: las `@menciones` del chat del
 * equipo. Patrón combobox de ARIA con foco virtual: el `AreaTexto` lleva `role="combobox"`,
 * `aria-controls` = el `id` de esta lista y `aria-activedescendant` = la opción activa
 * (`idOpcion(id, i)`); `teclasSugerencias` mueve, elige y cierra con el teclado. El `Autocompletar`
 * (ComboBox de React Aria) no sirve aquí: es un campo de una línea que reemplaza todo el valor.
 */

export type SugerenciaLista = { valor: string; etiqueta: string; detalle?: ReactNode; inicio?: ReactNode };

export type PropsListaSugerencias = {
    id: string;
    /** Nombre de la lista para el lector de pantalla («Mencionar a…»). */
    etiqueta: string;
    opciones: readonly SugerenciaLista[];
    /** Índice de la opción resaltada. */
    activa: number;
    alElegir: (valor: string) => void;
    alResaltar?: (indice: number) => void;
    className?: string;
};

export const idOpcion = (id: string, indice: number) => `${id}-op-${indice}`;

export function ListaSugerencias({ id, etiqueta, opciones, activa, alElegir, alResaltar, className }: PropsListaSugerencias) {
    if (opciones.length === 0) return null;
    return (
        <ul
            id={id}
            role="listbox"
            aria-label={etiqueta}
            data-slot="lista-sugerencias"
            className={cn("grid max-h-60 gap-0.5 overflow-y-auto rounded-xl border border-border bg-[var(--overlay)] p-1 shadow-[var(--overlay-shadow)]", className)}
        >
            {opciones.map((o, i) => (
                <li
                    key={o.valor}
                    id={idOpcion(id, i)}
                    role="option"
                    aria-selected={i === activa}
                    data-activa={i === activa || undefined}
                    className={cn(
                        "flex min-w-0 cursor-pointer items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-sm",
                        i === activa ? "bg-primary/10 text-foreground" : "hover:bg-[var(--overlay-hover)]",
                    )}
                    // El foco se queda en el área de texto: el clic no se lo quita.
                    onMouseDown={(e) => e.preventDefault()}
                    onMouseEnter={() => alResaltar?.(i)}
                    onClick={() => alElegir(o.valor)}
                >
                    {o.inicio}
                    <span className="min-w-0 flex-1 truncate font-medium">{o.etiqueta}</span>
                    {o.detalle ? <span className="shrink-0 text-xs text-muted-foreground">{o.detalle}</span> : null}
                </li>
            ))}
        </ul>
    );
}

/**
 * Teclado del área de texto mientras hay sugerencias: flechas mueven, Enter o Tab eligen y Escape
 * cierra. Devuelve `true` si usó la tecla (y ya hizo `preventDefault`).
 */
export function teclasSugerencias(
    evento: KeyboardEvent<HTMLElement>,
    estado: { total: number; activa: number; alResaltar: (i: number) => void; alElegir: (i: number) => void; alCerrar: () => void },
): boolean {
    if (estado.total === 0) return false;
    switch (evento.key) {
        case "ArrowDown":
            estado.alResaltar((estado.activa + 1) % estado.total);
            break;
        case "ArrowUp":
            estado.alResaltar((estado.activa - 1 + estado.total) % estado.total);
            break;
        case "Enter":
        case "Tab":
            if (evento.shiftKey) return false;
            estado.alElegir(estado.activa);
            break;
        case "Escape":
            estado.alCerrar();
            break;
        default:
            return false;
    }
    evento.preventDefault();
    evento.stopPropagation();
    return true;
}
