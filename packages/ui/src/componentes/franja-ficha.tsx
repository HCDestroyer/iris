"use client";

import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "../utilidades";

export type ContadorFicha = {
    /** «Tickets abiertos». */
    etiqueta: string;
    /** «3», «Q 250.00». */
    valor: ReactNode;
};

export type PropsFranjaFicha = {
    /** Nombre de lo que se ve («José López», «OT-000042»). Es el `<h1>` de la ficha (hasta 2 líneas; completo en `title`). */
    titulo: ReactNode;
    /** Bajo el título: código, tipo, cliente. Una línea: si se corta, el texto completo va en `title`. */
    detalle?: ReactNode;
    /** Ícono de Lucide en su ficha de color índigo tenue. */
    icono?: LucideIcon;
    /** O un avatar propio (`AvatarUsuario`). */
    avatar?: ReactNode;
    /** Estados (`InsigniaEstado`), junto al título. */
    insignias?: ReactNode;
    /** Cifras de la ficha (máximo 4 a la vista). */
    contadores?: readonly ContadorFicha[];
    /** El «?» del manual (`BotonManual`) junto al título. */
    manual?: ReactNode;
    /** Una sola acción principal (`Boton`). */
    accion?: ReactNode;
    /** «Más» con el resto de las acciones (`MenuAcciones`). */
    mas?: ReactNode;
    className?: string;
};

/**
 * Franja de una ficha (rediseño, regla de fichas): la identidad, sus contadores, una sola acción
 * principal y «Más» con menú, todo en una fila dentro de una tarjeta. En el teléfono los contadores
 * bajan a su propia fila y la acción principal va fija abajo (`AccionFija`) si la pantalla lo pide.
 * Un dato aparece una sola vez: lo que dice la franja no se repite en el resumen.
 */
export function FranjaFicha({ titulo, detalle, icono: Icono, avatar, insignias, contadores = [], manual, accion, mas, className }: PropsFranjaFicha) {
    return (
        <section data-slot="franja-ficha" className={cn("surface-card flex min-w-0 flex-wrap items-center gap-x-5 gap-y-3 px-4 py-3.5 sm:px-5", className)}>
            <div className="flex min-w-0 flex-[1_1_16rem] items-center gap-3">
                {avatar ??
                    (Icono ? (
                        <span aria-hidden className="grid size-11 flex-none place-content-center rounded-xl bg-primario-suave text-primario-texto">
                            <Icono className="size-5" />
                        </span>
                    ) : null)}
                <div className="grid min-w-0 gap-0.5">
                    <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
                        <h1 className="line-clamp-2 min-w-0 text-xl leading-tight font-bold tracking-[-0.015em] break-words" {...(typeof titulo === "string" ? { title: titulo } : {})}>
                            {titulo}
                        </h1>
                        {manual}
                        {insignias}
                    </div>
                    {detalle ? (
                        <p className="truncate text-sm text-muted-foreground" {...(typeof detalle === "string" ? { title: detalle } : {})}>
                            {detalle}
                        </p>
                    ) : null}
                </div>
            </div>
            {contadores.length > 0 ? (
                <dl className="flex min-w-0 flex-wrap items-center gap-x-6 gap-y-2" data-slot="contadores-ficha">
                    {contadores.slice(0, 4).map((c) => (
                        <div key={c.etiqueta} className="grid gap-0.5">
                            <dt className="text-xs text-[var(--iris-texto-apagado)]">{c.etiqueta}</dt>
                            <dd className="text-base font-semibold tabular-nums">{c.valor}</dd>
                        </div>
                    ))}
                </dl>
            ) : null}
            {accion || mas ? (
                <div data-slot="acciones-ficha" className="ml-auto flex max-w-full min-w-0 flex-wrap items-center gap-2">
                    {accion}
                    {mas}
                </div>
            ) : null}
        </section>
    );
}
