"use client";

import { ToggleButton, ToggleButtonGroup } from "@heroui/react";
import type { LucideIcon } from "lucide-react";
import { useRef, type ComponentProps } from "react";
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
    /** Solo lectura (p. ej. una orden en pausa): se ve la elegida, nada se puede cambiar. */
    deshabilitado?: boolean;
    className?: string;
};

const FLECHAS = new Set(["ArrowRight", "ArrowLeft", "ArrowUp", "ArrowDown", "Home", "End"]);

/**
 * Grupo de opciones excluyentes, como el selector de tema: el `ToggleButtonGroup` de HeroUI con
 * selección única (`role="radiogroup"`, cada opción `role="radio"`). Como un grupo de radios, las
 * flechas mueven y eligen; siempre queda una elegida.
 */
export function GrupoSegmentado<V extends string>({ etiqueta, opciones, valor, alCambiar, deshabilitado = false, className }: PropsGrupoSegmentado<V>) {
    // React Aria mueve el foco con las flechas; la opción que lo recibe así queda elegida.
    const conFlechas = useRef(false);
    return (
        <ToggleButtonGroup
            aria-label={etiqueta}
            data-slot="grupo-segmentado"
            selectionMode="single"
            disallowEmptySelection
            isDisabled={deshabilitado}
            selectedKeys={valor !== undefined ? [valor] : []}
            onSelectionChange={(claves) => {
                const [elegida] = [...claves];
                if (elegida !== undefined) alCambiar(String(elegida) as V);
            }}
            className={cn("inline-flex max-w-full flex-wrap items-center justify-start gap-0.5 rounded-md bg-elevated p-0.5", className)}
        >
            {opciones.map((opcion) => {
                const Icono = opcion.icono;
                return (
                    <ToggleButton
                        key={opcion.valor}
                        id={opcion.valor}
                        size="sm"
                        variant="ghost"
                        {...(opcion.soloIcono ? { "aria-label": opcion.etiqueta } : {})}
                        onKeyDown={(evento) => {
                            conFlechas.current = FLECHAS.has(evento.key);
                            evento.continuePropagation();
                        }}
                        onFocus={() => {
                            if (conFlechas.current && !deshabilitado && opcion.valor !== valor) alCambiar(opcion.valor);
                            conFlechas.current = false;
                        }}
                        render={(dom) => <button {...(dom as ComponentProps<"button">)} title={opcion.soloIcono ? opcion.etiqueta : undefined} />}
                        className={cn(
                            "inline-flex h-7 min-w-0 items-center justify-center gap-1.5 rounded-sm px-2 text-xs font-medium",
                            "text-muted-foreground transition-colors duration-150 hover:text-foreground",
                            // Opción elegida inconfundible en claro y oscuro: color de marca (selección) en texto y borde.
                            "data-selected:bg-background data-selected:font-semibold data-selected:text-primary data-selected:shadow-[0_0_0_1px_var(--hx-primary)]",
                            "[&_svg]:size-4",
                        )}
                    >
                        {Icono ? <Icono aria-hidden /> : null}
                        {opcion.soloIcono ? null : opcion.etiqueta}
                    </ToggleButton>
                );
            })}
        </ToggleButtonGroup>
    );
}
