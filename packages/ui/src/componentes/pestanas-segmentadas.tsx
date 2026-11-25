"use client";

import { Tabs } from "@heroui/react";
import type { Key } from "react";
import { cn } from "../utilidades";
import type { Pestana } from "./pestanas";

export type TonoContador = "marca" | "atencion" | "peligro";

export type PestanaSegmentada<I extends string = string> = Pestana<I> & {
    /** Color del contador: índigo por omisión; ámbar o rojo cuando pide actuar. */
    tonoContador?: TonoContador;
};

export type PropsPestanasSegmentadas<I extends string = string> = {
    /** Nombre accesible de la lista de pestañas («Vistas de tickets»). */
    etiqueta: string;
    pestanas: readonly PestanaSegmentada<I>[];
    seleccionada?: I;
    inicial?: I;
    alCambiar?: (id: I) => void;
    /** A todo el ancho, con las opciones repartidas (Hoy · Mañana · Semana en la app de campo). */
    ancho?: boolean;
    className?: string;
    /** Clases del panel de cada pestaña. */
    clasePanel?: string;
};

const FONDO_CONTADOR: Record<TonoContador, string> = {
    marca: "bg-primario-suave text-primario-texto",
    atencion: "bg-atencion-fondo text-warning",
    peligro: "bg-peligro-fondo text-danger",
};

/**
 * Pestañas segmentadas (rediseño, regla 6): las `Tabs` de HeroUI con aspecto de control
 * segmentado (riel gris de 14 px de radio; la elegida es una píldora de tarjeta que se desliza,
 * el `Tabs.Indicator`), con ícono, texto y contador. Van dentro de la tarjeta de su contenido. Con
 * `href`, cada pestaña tiene su URL (`?pestana=`). En el teléfono la tira se desliza de lado sin
 * barra. Flechas, Inicio y Fin para moverse (React Aria).
 */
export function PestanasSegmentadas<I extends string = string>({ etiqueta, pestanas, seleccionada, inicial, alCambiar, ancho = false, className, clasePanel }: PropsPestanasSegmentadas<I>) {
    const deshabilitadas = pestanas.filter((p) => p.deshabilitada).map((p) => p.id);
    const elegida = seleccionada !== undefined && !pestanas.some((p) => p.id === seleccionada && !p.deshabilitada) ? pestanas.find((p) => !p.deshabilitada)?.id : seleccionada;
    return (
        <Tabs
            data-slot="pestanas-segmentadas"
            className={cn("min-w-0 max-w-full", ancho ? "w-full" : "w-auto", className)}
            {...(elegida !== undefined ? { selectedKey: elegida } : {})}
            {...(inicial !== undefined ? { defaultSelectedKey: inicial } : {})}
            disabledKeys={deshabilitadas}
            {...(alCambiar ? { onSelectionChange: (clave: Key) => alCambiar(String(clave) as I) } : {})}
        >
            <Tabs.ListContainer className={cn("max-w-full overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden", ancho && "w-full")}>
                <Tabs.List
                    aria-label={etiqueta}
                    className={cn("inline-flex gap-0.5 rounded-[14px] border-0 bg-relleno p-1", ancho && "flex w-full [&>*]:flex-1")}
                >
                    {pestanas.map((p) => {
                        const Icono = p.icono;
                        return (
                            <Tabs.Tab
                                key={p.id}
                                id={p.id}
                                {...(p.href ? { href: p.href } : {})}
                                className={cn(
                                    "relative h-[2.125rem] justify-center gap-1.5 rounded-[10px] px-3.5 text-[0.8125rem] font-semibold whitespace-nowrap text-muted-foreground",
                                    "data-selected:text-primario-texto [&_svg]:size-4",
                                )}
                            >
                                {/* La píldora elegida (se desliza); el texto va encima. */}
                                <Tabs.Indicator className="rounded-[10px] bg-[var(--iris-tarjeta)] shadow-[0_1px_3px_rgb(15_23_42/0.12)]" />
                                <span className="relative z-[1] inline-flex items-center gap-1.5">
                                    {Icono ? <Icono aria-hidden /> : null}
                                    {p.etiqueta}
                                    {p.contador !== undefined ? (
                                        <span data-slot="contador" className={cn("rounded-full px-1.5 text-[0.6875rem] leading-[1.125rem] font-bold tabular-nums", FONDO_CONTADOR[p.tonoContador ?? "marca"])}>
                                            {p.contador}
                                        </span>
                                    ) : null}
                                </span>
                            </Tabs.Tab>
                        );
                    })}
                </Tabs.List>
            </Tabs.ListContainer>
            {pestanas.map((p) =>
                p.contenido !== undefined ? (
                    <Tabs.Panel key={p.id} id={p.id} className={cn("pt-4", clasePanel)}>
                        {p.contenido}
                    </Tabs.Panel>
                ) : null,
            )}
        </Tabs>
    );
}
