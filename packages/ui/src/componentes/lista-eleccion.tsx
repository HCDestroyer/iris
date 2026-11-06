"use client";

import { Label, Radio, RadioGroup } from "@heroui/react";
import { useId, type ReactNode } from "react";
import { cn } from "../utilidades";
import { PieCampo } from "./campo";

export type OpcionEleccion = {
    valor: string;
    /** Nombre accesible de la opción (lo que lee el lector de pantalla). */
    texto: string;
    /** Lo que se ve en la fila: avatar, nombre, detalle… */
    contenido: ReactNode;
    /** A la derecha de la fila (estado, insignia). */
    fin?: ReactNode;
    deshabilitada?: boolean;
    /** Se ve más tenue sin dejar de poder elegirse (p. ej. alguien «No disponible»). */
    atenuada?: boolean;
};

export type PropsListaEleccion = {
    /** Pregunta del grupo (nombre del `radiogroup`). */
    leyenda: ReactNode;
    /** Oculta la leyenda (queda para el lector de pantalla). */
    leyendaOculta?: boolean;
    nombre: string;
    opciones: readonly OpcionEleccion[];
    valor: string | undefined;
    alCambiar: (valor: string) => void;
    error?: string | undefined;
    ayuda?: ReactNode;
    className?: string;
    /** Filas pegadas en una sola caja (lista de búsqueda) en vez de tarjetas separadas. */
    pegadas?: boolean;
};

/**
 * Elegir una entre varias filas con detalle: el `RadioGroup` de HeroUI (React Aria) con radios
 * reales (flechas para moverse y elegir) y contenido propio por fila: un compañero con su
 * disponibilidad, un contacto con su permiso de mensajes. La elegida lleva el radio marcado, borde
 * y fondo de marca (nunca solo color); una deshabilitada no se puede elegir y dice por qué en su
 * contenido.
 */
export function ListaEleccion({ leyenda, leyendaOculta = false, nombre, opciones, valor, alCambiar, error, ayuda, className, pegadas = false }: PropsListaEleccion) {
    const id = useId();
    const idAyuda = `${id}-ayuda`;
    const idError = `${id}-error`;
    const describe = [ayuda ? idAyuda : null, error ? idError : null].filter(Boolean).join(" ") || undefined;
    return (
        <RadioGroup
            data-slot="lista-eleccion"
            name={nombre}
            value={valor ?? null}
            onChange={alCambiar}
            isInvalid={Boolean(error)}
            {...(describe ? { "aria-describedby": describe } : {})}
            className={cn("grid min-w-0 gap-2", className)}
        >
            <Label className={cn("text-sm font-medium", leyendaOculta && "sr-only")}>{leyenda}</Label>
            <div className={cn("grid min-w-0", pegadas ? "overflow-hidden rounded-lg border border-border" : "gap-2")}>
                {opciones.map((o) => {
                    const marcado = valor === o.valor;
                    return (
                        <Radio key={o.valor} value={o.valor} isDisabled={o.deshabilitada ?? false} aria-label={o.texto} className="contents">
                            <Radio.Content
                                data-opcion={o.valor}
                                data-elegida={marcado || undefined}
                                className={cn(
                                    "flex min-h-12 w-full min-w-0 cursor-pointer items-center gap-3 bg-background px-3 py-2.5 text-left text-sm",
                                    "transition-[border-color,box-shadow,background-color] duration-150 hover:bg-elevated",
                                    "data-focus-visible:outline-2 data-focus-visible:-outline-offset-2 data-focus-visible:outline-primary",
                                    "data-disabled:cursor-not-allowed data-disabled:hover:bg-background",
                                    pegadas ? "border-b border-border last:border-b-0" : "rounded-lg border border-border",
                                    marcado && "bg-primary/5 hover:bg-primary/5",
                                    marcado && !pegadas && "border-primary shadow-[0_0_0_1px_var(--hx-primary)]",
                                    error && !marcado && !pegadas && "border-error",
                                )}
                            >
                                <Radio.Indicator
                                    className={cn(
                                        "relative inset-auto m-0 flex size-5 shrink-0 items-center justify-center rounded-full border-2 bg-background p-0 shadow-none",
                                        marcado ? "border-primary" : "border-border",
                                        o.deshabilitada && "opacity-40",
                                    )}
                                >
                                    <span aria-hidden className={cn("size-2.5 rounded-full bg-primary", marcado ? "opacity-100" : "opacity-0")} />
                                </Radio.Indicator>
                                <span className={cn("flex min-w-0 flex-1 items-center gap-3", (o.atenuada || o.deshabilitada) && "opacity-70")}>{o.contenido}</span>
                                {o.fin ? <span className="shrink-0">{o.fin}</span> : null}
                            </Radio.Content>
                        </Radio>
                    );
                })}
            </div>
            <PieCampo idAyuda={idAyuda} idError={idError} ayuda={ayuda} error={error} />
        </RadioGroup>
    );
}
