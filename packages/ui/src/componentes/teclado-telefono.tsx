"use client";

import type { ComponentType, ReactNode, SVGProps } from "react";
import { cn } from "../utilidades";
import { Boton } from "./boton";

/*
 * Piezas del teléfono de Iris (E4c, maqueta aprobada el 21 nov 2026): el teclado de 12 teclas
 * (marcar y tonos durante la llamada) y los controles redondos de la llamada (silenciar, en espera,
 * teclado, transferir, colgar). Botones grandes (56 px) para tocarlos sin mirar, con texto debajo:
 * el ícono nunca va solo. Cada tecla y cada control es un `Boton` (el `Button` de HeroUI): React
 * Aria da el teclado, el foco visible y el estado de presión.
 */

const TECLAS: readonly (readonly [string, string])[] = [
    ["1", ""],
    ["2", "ABC"],
    ["3", "DEF"],
    ["4", "GHI"],
    ["5", "JKL"],
    ["6", "MNO"],
    ["7", "PQRS"],
    ["8", "TUV"],
    ["9", "WXYZ"],
    ["*", ""],
    ["0", "+"],
    ["#", ""],
];

export type PropsTecladoTelefono = {
    /** Cada tecla tocada (0–9, `*`, `#`). */
    alTocar: (tecla: string) => void;
    /** Nombre del grupo para lectores de pantalla. */
    etiqueta?: string;
    deshabilitado?: boolean;
    className?: string;
};

export function TecladoTelefono({ alTocar, etiqueta = "Teclado", deshabilitado, className }: PropsTecladoTelefono) {
    return (
        <div role="group" aria-label={etiqueta} data-slot="teclado-telefono" className={cn("mx-auto grid w-full max-w-[17rem] grid-cols-3 gap-2.5", className)}>
            {TECLAS.map(([tecla, letras]) => (
                <Boton
                    key={tecla}
                    variante="fantasma"
                    disabled={deshabilitado}
                    aria-label={tecla === "*" ? "Asterisco" : tecla === "#" ? "Numeral" : tecla}
                    onClick={() => alTocar(tecla)}
                    className="flex h-14 w-full flex-col items-center justify-center gap-0 rounded-full border border-border bg-card px-0 text-foreground transition-colors select-none hover:bg-elevated active:scale-95 md:h-14"
                >
                    <span className="text-xl leading-6 font-semibold tabular-nums">{tecla}</span>
                    <span aria-hidden className="h-3 text-[0.625rem] leading-3 tracking-[0.12em] text-muted-foreground">
                        {letras}
                    </span>
                </Boton>
            ))}
        </div>
    );
}

export type PropsControlLlamada = {
    icono: ComponentType<SVGProps<SVGSVGElement>>;
    /** Texto debajo del botón (también su nombre accesible). */
    etiqueta: string;
    onClick?: () => void;
    /** Interruptor (silenciar, en espera): pinta el estado y anuncia `aria-pressed`. */
    activo?: boolean;
    /** Colgar. */
    peligro?: boolean;
    deshabilitado?: boolean;
    /** Lo que se dice además de la etiqueta (p. ej. «Activa el micrófono»). */
    ayuda?: string;
};

export function ControlLlamada({ icono: Icono, etiqueta, onClick, activo, peligro, deshabilitado, ayuda }: PropsControlLlamada) {
    return (
        <Boton
            variante="fantasma"
            {...(onClick ? { onClick } : {})}
            disabled={deshabilitado}
            aria-pressed={activo === undefined ? undefined : activo}
            {...(ayuda ? { title: ayuda } : {})}
            data-slot="control-llamada"
            data-activo={activo || undefined}
            className="group flex h-auto w-auto min-w-0 flex-col items-center gap-1.5 rounded-lg p-1 text-xs font-medium text-foreground hover:bg-transparent md:h-auto [&_svg]:size-5"
        >
            <span
                aria-hidden
                className={cn(
                    "flex size-14 items-center justify-center rounded-full border transition-colors group-active:scale-95 [&_svg]:size-5",
                    peligro
                        ? "border-transparent bg-error text-white group-hover:bg-[color-mix(in_oklab,var(--hx-error)_88%,black)]"
                        : activo
                          ? "border-transparent bg-foreground text-background"
                          : "border-border bg-card group-hover:bg-elevated",
                )}
            >
                <Icono />
            </span>
            <span className="max-w-full truncate">{etiqueta}</span>
        </Boton>
    );
}

/** Fila de controles: 3 por fila en el teléfono, todos en una fila desde `sm`. */
export function ControlesLlamada({ children, etiqueta = "Controles de la llamada" }: { children: ReactNode; etiqueta?: string }) {
    return (
        <div role="group" aria-label={etiqueta} data-slot="controles-llamada" className="grid grid-cols-3 gap-x-2 gap-y-3">
            {children}
        </div>
    );
}
