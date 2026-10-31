"use client";

import { Tabs } from "@heroui/react";
import type { LucideIcon } from "lucide-react";
import type { Key, ReactNode } from "react";
import { cn } from "../utilidades";

export type Pestana<I extends string = string> = {
    id: I;
    /** Texto visible de la pestaña (y su nombre accesible). */
    etiqueta: string;
    icono?: LucideIcon;
    /** Contenido del panel. Si la pestaña navega (`href`), puede omitirse. */
    contenido?: ReactNode;
    /** Pestaña que cambia la URL (p. ej. `/tickets/42/historial`): las fichas con pestañas de la fase 3. */
    href?: string;
    deshabilitada?: boolean;
    /** Número junto a la etiqueta (p. ej. órdenes abiertas). */
    contador?: number;
};

export type PropsPestanas<I extends string = string> = {
    /** Nombre accesible de la lista de pestañas («Secciones del ticket»). */
    etiqueta: string;
    pestanas: readonly Pestana<I>[];
    /** Pestaña elegida (controlada). Sin ella, empieza en `inicial` o en la primera. */
    seleccionada?: I;
    inicial?: I;
    alCambiar?: (id: I) => void;
    className?: string;
};

/**
 * Pestañas (HeroUI `Tabs`, React Aria): flechas para moverse, Inicio/Fin, y en el teléfono la lista
 * se desplaza en horizontal. Para las fichas (Ticket: Resumen · Conversación · Órdenes · Historial).
 */
export function Pestanas<I extends string = string>({ etiqueta, pestanas, seleccionada, inicial, alCambiar, className }: PropsPestanas<I>) {
    const deshabilitadas = pestanas.filter((p) => p.deshabilitada).map((p) => p.id);
    // Una pestaña elegida que no está (p. ej. sin permiso para verla) cae en la primera.
    const elegida = seleccionada !== undefined && !pestanas.some((p) => p.id === seleccionada && !p.deshabilitada) ? pestanas.find((p) => !p.deshabilitada)?.id : seleccionada;
    return (
        <Tabs
            data-slot="pestanas"
            className={cn("w-full min-w-0 max-w-full", className)}
            {...(elegida !== undefined ? { selectedKey: elegida } : {})}
            {...(inicial !== undefined ? { defaultSelectedKey: inicial } : {})}
            disabledKeys={deshabilitadas}
            {...(alCambiar ? { onSelectionChange: (clave: Key) => alCambiar(String(clave) as I) } : {})}
        >
            <Tabs.ListContainer>
                <Tabs.List aria-label={etiqueta}>
                    {pestanas.map((p) => {
                        const Icono = p.icono;
                        return (
                            <Tabs.Tab key={p.id} id={p.id} {...(p.href ? { href: p.href } : {})} className="gap-1.5">
                                {Icono ? <Icono aria-hidden className="size-4 shrink-0" /> : null}
                                {p.etiqueta}
                                {p.contador !== undefined ? <span className="text-xs text-muted-foreground tabular-nums">{p.contador}</span> : null}
                                <Tabs.Indicator />
                            </Tabs.Tab>
                        );
                    })}
                </Tabs.List>
            </Tabs.ListContainer>
            {pestanas.map((p) =>
                p.contenido !== undefined ? (
                    <Tabs.Panel key={p.id} id={p.id} className="pt-4">
                        {p.contenido}
                    </Tabs.Panel>
                ) : null,
            )}
        </Tabs>
    );
}
