"use client";

import type { LucideIcon } from "lucide-react";
import { useRef, type KeyboardEvent } from "react";
import { cn } from "../utilidades";

export type OpcionSegmentada<V extends string> = {
    valor: V;
    etiqueta: string;
    icono?: LucideIcon;
    /** Solo icono visible; la etiqueta queda para lectores de pantalla y como tooltip. */
    soloIcono?: boolean;
};

export type PropsGrupoSegmentado<V extends string> = {
    etiqueta: string;
    opciones: readonly OpcionSegmentada<V>[];
    valor: V | undefined;
    alCambiar: (valor: V) => void;
    className?: string;
};

/** Grupo de opciones excluyentes (radiogroup) con flechas para moverse, como el selector de tema. */
export function GrupoSegmentado<V extends string>({
    etiqueta,
    opciones,
    valor,
    alCambiar,
    className,
}: PropsGrupoSegmentado<V>) {
    const refs = useRef<(HTMLButtonElement | null)[]>([]);
    const indiceActual = Math.max(
        0,
        opciones.findIndex((o) => o.valor === valor),
    );

    function alTeclear(evento: KeyboardEvent<HTMLDivElement>) {
        const paso = evento.key === "ArrowRight" || evento.key === "ArrowDown" ? 1 : evento.key === "ArrowLeft" || evento.key === "ArrowUp" ? -1 : 0;
        if (paso === 0) {
            return;
        }
        evento.preventDefault();
        const siguiente = (indiceActual + paso + opciones.length) % opciones.length;
        const opcion = opciones[siguiente];
        if (opcion) {
            alCambiar(opcion.valor);
            refs.current[siguiente]?.focus();
        }
    }

    return (
        <div
            role="radiogroup"
            aria-label={etiqueta}
            data-slot="grupo-segmentado"
            onKeyDown={alTeclear}
            className={cn("inline-flex items-center gap-0.5 rounded-md bg-elevated p-0.5", className)}
        >
            {opciones.map((opcion, i) => {
                const marcado = opcion.valor === valor;
                const Icono = opcion.icono;
                return (
                    <button
                        key={opcion.valor}
                        ref={(el) => {
                            refs.current[i] = el;
                        }}
                        type="button"
                        role="radio"
                        aria-checked={marcado}
                        aria-label={opcion.soloIcono ? opcion.etiqueta : undefined}
                        title={opcion.soloIcono ? opcion.etiqueta : undefined}
                        tabIndex={i === indiceActual ? 0 : -1}
                        onClick={() => alCambiar(opcion.valor)}
                        className={cn(
                            "inline-flex h-7 items-center justify-center gap-1.5 rounded-sm px-2 text-xs font-medium",
                            "text-muted-foreground transition-colors duration-150 hover:text-foreground",
                            "aria-checked:bg-background aria-checked:text-foreground aria-checked:shadow-[0_0_0_1px_var(--hx-border)]",
                            "[&_svg]:size-4",
                        )}
                    >
                        {Icono ? <Icono aria-hidden /> : null}
                        {opcion.soloIcono ? null : opcion.etiqueta}
                    </button>
                );
            })}
        </div>
    );
}
