"use client";

import { Breadcrumbs } from "@heroui/react";
import { ChevronRight } from "lucide-react";
import type { ReactNode } from "react";
import { RouterProvider } from "react-aria-components";
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
 * cada pantalla del panel y reemplaza a los enlaces «← Volver». En el teléfono las migas
 * intermedias se recortan y queda visible la anterior y la actual.
 */
export function Migas({ migas, etiqueta = "Ruta de navegación", className }: PropsMigas) {
    return (
        <nav aria-label={etiqueta} data-slot="migas" className={cn("min-w-0", className)}>
            <Breadcrumbs aria-label="Migas" separator={<ChevronRight aria-hidden className="size-3.5 text-dimmed" />} className="flex-wrap text-sm">
                {migas.map((m, i) => (
                    <Breadcrumbs.Item
                        key={`${i}:${m.etiqueta}`}
                        {...(m.href && i < migas.length - 1 ? { href: m.href } : {})}
                        className={cn("max-w-[14rem] truncate", i < migas.length - 2 && "max-sm:hidden")}
                    >
                        {m.etiqueta}
                    </Breadcrumbs.Item>
                ))}
            </Breadcrumbs>
        </nav>
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
