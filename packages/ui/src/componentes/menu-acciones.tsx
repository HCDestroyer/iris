"use client";

import { Dropdown, Kbd, Separator } from "@heroui/react";
import { Ellipsis, type LucideIcon } from "lucide-react";
import { Fragment, type ReactElement } from "react";
import { Header } from "react-aria-components";
import { cn } from "../utilidades";
import { Boton } from "./boton";

export type AccionMenu = {
    id: string;
    texto: string;
    icono?: LucideIcon;
    /** Atajo que se muestra a la derecha («Enter», «Ctrl D»); solo informativo. */
    atajo?: string;
    /** Destructiva (cerrar, fusionar, eliminar): va al final, separada y en rojo. */
    peligro?: boolean;
    /** Sección con título (en mayúsculas pequeñas) dentro del menú. */
    seccion?: string;
    deshabilitada?: boolean;
    /** `false` la quita del menú (p. ej. sin el permiso). Por omisión, se muestra. */
    permitida?: boolean;
    alElegir: () => void;
};

export type PropsMenuAcciones = {
    /** Nombre accesible del botón («Acciones de TK-001236»). */
    etiqueta: string;
    acciones: readonly AccionMenu[];
    /** Disparador propio (p. ej. «Más» con texto); por omisión, el `⋯` redondo de 32 px. */
    disparador?: ReactElement;
    /** Lado del menú respecto al disparador. */
    alineacion?: "start" | "end";
    className?: string;
};

type Grupo = { titulo: string | null; acciones: AccionMenu[] };

/**
 * Agrupa las acciones visibles: primero las comunes en el orden dado (por sección), al final las
 * peligrosas. Sin acciones visibles, nada.
 */
export function gruposMenuAcciones(acciones: readonly AccionMenu[]): { comunes: Grupo[]; peligrosas: AccionMenu[] } {
    const visibles = acciones.filter((a) => a.permitida !== false);
    const comunes: Grupo[] = [];
    for (const accion of visibles.filter((a) => !a.peligro)) {
        const titulo = accion.seccion ?? null;
        const grupo = comunes.find((g) => g.titulo === titulo);
        if (grupo) grupo.acciones.push(accion);
        else comunes.push({ titulo, acciones: [accion] });
    }
    return { comunes, peligrosas: visibles.filter((a) => a.peligro) };
}

function Elemento({ accion }: { accion: AccionMenu }) {
    const Icono = accion.icono;
    return (
        <Dropdown.Item
            id={accion.id}
            textValue={accion.texto}
            isDisabled={accion.deshabilitada ?? false}
            onAction={accion.alElegir}
            data-peligro={accion.peligro ? "true" : undefined}
            className={cn(
                "flex min-h-9 cursor-pointer items-center gap-2.5 rounded-[9px] px-2.5 text-sm outline-none select-none",
                "data-focused:bg-[var(--overlay-hover)] data-hovered:bg-[var(--overlay-hover)] data-disabled:pointer-events-none data-disabled:opacity-55",
                accion.peligro && "text-danger",
            )}
        >
            {Icono ? <Icono aria-hidden className={cn("size-4 shrink-0", accion.peligro ? "text-danger" : "text-muted-foreground")} /> : null}
            <span className="min-w-0 flex-1 truncate">{accion.texto}</span>
            {accion.atajo ? <Kbd className="ml-auto text-[0.6875rem] text-muted-foreground">{accion.atajo}</Kbd> : null}
        </Dropdown.Item>
    );
}

/**
 * Menú `⋯` de una fila o de una ficha: el `Dropdown` de HeroUI con secciones. Las acciones
 * frecuentes primero (en el orden dado), las peligrosas al final, separadas y en rojo; las que no
 * están permitidas no aparecen. Sin ninguna acción visible no se dibuja.
 */
export function MenuAcciones({ etiqueta, acciones, disparador, alineacion = "end", className }: PropsMenuAcciones) {
    const { comunes, peligrosas } = gruposMenuAcciones(acciones);
    if (comunes.length === 0 && peligrosas.length === 0) return null;
    return (
        <Dropdown>
            {disparador ?? (
                <Boton variante="fantasma" tamano="icono-sm" aria-label={etiqueta} title={etiqueta} data-slot="menu-acciones" className={className}>
                    <Ellipsis />
                </Boton>
            )}
            <Dropdown.Popover
                offset={6}
                placement={alineacion === "start" ? "bottom start" : "bottom end"}
                containerPadding={8}
                className="min-w-60 max-w-[calc(100vw-1rem)] border border-border p-1.5 outline-none"
            >
                <Dropdown.Menu aria-label={etiqueta} className="grid gap-0.5 p-0 outline-none">
                    {comunes.map((grupo, i) => (
                        <Fragment key={grupo.titulo ?? `grupo-${i}`}>
                            {i > 0 ? <Separator className="my-1" /> : null}
                            <Dropdown.Section className="grid gap-0.5">
                                {grupo.titulo ? (
                                    <Header className="px-2.5 pt-2 pb-1 text-[0.6875rem] font-bold tracking-[0.06em] text-[var(--iris-texto-apagado)] uppercase">{grupo.titulo}</Header>
                                ) : null}
                                {grupo.acciones.map((a) => (
                                    <Elemento key={a.id} accion={a} />
                                ))}
                            </Dropdown.Section>
                        </Fragment>
                    ))}
                    {peligrosas.length > 0 ? (
                        <>
                            {comunes.length > 0 ? <Separator className="my-1" /> : null}
                            <Dropdown.Section aria-label="Acciones peligrosas" className="grid gap-0.5" data-slot="acciones-peligrosas">
                                {peligrosas.map((a) => (
                                    <Elemento key={a.id} accion={a} />
                                ))}
                            </Dropdown.Section>
                        </>
                    ) : null}
                </Dropdown.Menu>
            </Dropdown.Popover>
        </Dropdown>
    );
}
