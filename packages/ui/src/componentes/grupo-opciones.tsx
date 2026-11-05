"use client";

import { Label, Radio, RadioGroup } from "@heroui/react";
import { CircleCheck, type LucideIcon } from "lucide-react";
import { useId, type ReactNode } from "react";
import { cn } from "../utilidades";
import { PieCampo } from "./campo";

export type OpcionGrupo<V extends string | number> = {
    valor: V;
    /** Texto visible de la opción: obligatorio (nunca solo un icono o una estrella). */
    etiqueta: string;
    icono?: LucideIcon;
    /** Texto pequeño bajo la etiqueta. */
    detalle?: string;
};

export type PropsGrupoOpciones<V extends string | number> = {
    /** Pregunta del grupo (nombre del `radiogroup`). */
    leyenda: ReactNode;
    /** `name` de los radios. */
    nombre: string;
    opciones: readonly OpcionGrupo<V>[];
    valor: V | undefined;
    alCambiar: (valor: V) => void;
    error?: string | undefined;
    ayuda?: ReactNode;
    disabled?: boolean;
    className?: string;
};

/**
 * Opciones excluyentes como tarjetas grandes: el `RadioGroup` de HeroUI (`RadioGroup`, `Label`,
 * `Radio`, `Radio.Content`, `Radio.Indicator`) con radios reales de React Aria: flechas para
 * moverse y elegir, como cualquier radio del sistema. Pensado para formularios públicos y táctiles
 * (p. ej. la calificación 1–5 de la encuesta), con área táctil ≥ 44 px. La elegida se marca con
 * borde, peso y una marca (nunca solo color).
 */
export function GrupoOpciones<V extends string | number>({ leyenda, nombre, opciones, valor, alCambiar, error, ayuda, disabled, className }: PropsGrupoOpciones<V>) {
    const id = useId();
    const idAyuda = `${id}-ayuda`;
    const idError = `${id}-error`;
    const describe = [ayuda ? idAyuda : null, error ? idError : null].filter(Boolean).join(" ") || undefined;
    return (
        <RadioGroup
            data-slot="grupo-opciones"
            name={nombre}
            value={valor === undefined ? null : String(valor)}
            onChange={(texto) => {
                const elegida = opciones.find((o) => String(o.valor) === texto);
                if (elegida) alCambiar(elegida.valor);
            }}
            isInvalid={Boolean(error)}
            isDisabled={disabled ?? false}
            {...(describe ? { "aria-describedby": describe } : {})}
            className={cn("grid gap-2", className)}
        >
            <Label className="mb-1 text-sm font-medium">{leyenda}</Label>
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-[repeat(auto-fit,minmax(6.5rem,1fr))]">
                {opciones.map((o) => {
                    const Icono = o.icono;
                    const marcado = valor === o.valor;
                    return (
                        <Radio key={String(o.valor)} value={String(o.valor)} className="contents">
                            <Radio.Content
                                className={cn(
                                    "iris-entrada-cubierta relative flex min-h-11 w-full cursor-pointer items-center gap-2.5 rounded-lg border border-input bg-background px-3 py-2.5 text-sm",
                                    "transition-[border-color,box-shadow,background-color] duration-150 hover:bg-elevated",
                                    "data-focus-visible:outline-2 data-focus-visible:outline-offset-2 data-focus-visible:outline-primary",
                                    "data-disabled:cursor-not-allowed data-disabled:opacity-55",
                                    "sm:flex-col sm:justify-center sm:gap-1.5 sm:py-3 sm:text-center",
                                    marcado && "border-primary bg-primary/5 font-semibold shadow-[0_0_0_1px_var(--hx-primary)]",
                                    error && !marcado && "border-error",
                                )}
                            >
                                <Radio.Indicator className="absolute top-1.5 right-1.5 m-0 size-4 border-0 bg-transparent p-0 shadow-none">
                                    <CircleCheck aria-hidden className={cn("size-4 text-primary", marcado ? "opacity-100" : "opacity-0")} />
                                </Radio.Indicator>
                                {Icono ? <Icono aria-hidden className={cn("size-5 shrink-0 sm:size-6", marcado ? "text-primary" : "text-muted-foreground")} /> : null}
                                <span className="grid gap-0.5">
                                    <span>{o.etiqueta}</span>
                                    {o.detalle ? <span className="text-xs font-normal text-muted-foreground">{o.detalle}</span> : null}
                                </span>
                            </Radio.Content>
                        </Radio>
                    );
                })}
            </div>
            <PieCampo idAyuda={idAyuda} idError={idError} ayuda={ayuda} error={error} />
        </RadioGroup>
    );
}
