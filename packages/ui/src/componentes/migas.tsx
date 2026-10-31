"use client";

import { Breadcrumbs } from "@heroui/react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import type { ReactNode } from "react";
import { Link, RouterProvider } from "react-aria-components";
import { cn } from "../utilidades";

export type Miga = {
    etiqueta: string;
    /** Sin `href` es la pantalla actual (`aria-current="page"`): siempre la última. */
    href?: string;
};

export type PropsMigas = {
    migas: readonly Miga[];
    /** Nombre del landmark de navegación. */
    etiqueta?: string;
    className?: string;
};

/**
 * Ruta de migas («Órdenes › ORD-1042 › Editar») sobre HeroUI `Breadcrumbs` (React Aria). Va en
 * cada pantalla del panel y reemplaza a los enlaces «← Volver». En el teléfono queda solo el
 * enlace a la pantalla anterior («‹ Órdenes»); la actual sigue para lectores de pantalla.
 */
export function Migas({ migas, etiqueta = "Ruta de navegación", className }: PropsMigas) {
    const anterior = [...migas.slice(0, -1)].reverse().find((m) => m.href);
    const actual = migas.at(-1);
    return (
        <>
            <nav aria-label={etiqueta} data-slot="migas" className={cn("min-w-0 max-sm:hidden", className)}>
                <ListaMigas migas={migas} />
            </nav>
            {anterior?.href ? (
                <nav aria-label={etiqueta} data-slot="migas-telefono" className={cn("min-w-0 sm:hidden", className)}>
                    <Link
                        href={anterior.href}
                        className="inline-flex max-w-full items-center gap-0.5 rounded-sm py-1 text-sm text-muted-foreground outline-none hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary"
                    >
                        <ChevronLeft aria-hidden className="size-4 shrink-0" />
                        <span className="truncate">{anterior.etiqueta}</span>
                    </Link>
                    {actual ? (
                        <span className="sr-only" aria-current="page">
                            {actual.etiqueta}
                        </span>
                    ) : null}
                </nav>
            ) : null}
        </>
    );
}

function ListaMigas({ migas }: { migas: readonly Miga[] }) {
    return (
        <>
            <Breadcrumbs aria-label="Migas" separator={<ChevronRight aria-hidden className="size-3.5 text-dimmed" />} className="flex-wrap text-sm">
                {migas.map((m, i) => (
                    <Breadcrumbs.Item
                        key={`${i}:${m.etiqueta}`}
                        {...(m.href && i < migas.length - 1 ? { href: m.href } : {})}
                        className="max-w-[14rem] truncate"
                    >
                        {m.etiqueta}
                    </Breadcrumbs.Item>
                ))}
            </Breadcrumbs>
        </>
    );
}

export type PropsProveedorRutas = {
    /** Navegación del cliente (en Next: `router.push`). */
    navegar: (ruta: string) => void;
    children: ReactNode;
};

/**
 * Enlaces de React Aria (migas, pestañas con `href`, opciones de menú) con la navegación de la
 * app en lugar de recargar la página. Va una vez en el layout: `<ProveedorRutas navegar={router.push}>`.
 */
export function ProveedorRutas({ navegar, children }: PropsProveedorRutas) {
    return <RouterProvider navigate={navegar}>{children}</RouterProvider>;
}
