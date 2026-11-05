"use client";

import { Card } from "@heroui/react";
import type { ComponentProps, ReactElement } from "react";
import { cn } from "../utilidades";

/*
 * Tarjeta de contenido (material casi sólido): el `Card` de HeroUI (`Card`, `Card.Header`,
 * `Card.Title`…). El relleno y el radio son los de Horus (`surface-card`, 14 px); nunca dentro de
 * vidrio sobre vidrio.
 *
 * `.card` de HeroUI trae su propio acomodo (flex en columna, separación y relleno). Se anula para
 * que cada pantalla siga mandando con sus clases (`grid`, `flex flex-wrap`, `p-4`…) como antes.
 */
const NEUTRO = "block flex-row flex-nowrap items-stretch gap-0 overflow-visible p-0";

export function Tarjeta({ className, children, ...props }: ComponentProps<"div">) {
    return (
        <Card data-slot="tarjeta" className={cn(NEUTRO, "surface-card text-card-foreground", className)} {...props}>
            {children}
        </Card>
    );
}

export function TarjetaEncabezado({ className, ...props }: ComponentProps<"div">) {
    return <Card.Header data-slot="tarjeta-encabezado" className={cn("grid gap-1 p-5 pb-3", className)} {...props} />;
}

/** Título de la tarjeta: un `<h2>` (las fichas lo usan como encabezado de sección). */
/** `Card.Title` con su `render` tipado para un `<h2>` (HeroUI lo declara genérico, `<h3>` por defecto). */
const TituloCard = Card.Title as unknown as (props: ComponentProps<"h2"> & { render: (dom: ComponentProps<"h2">) => ReactElement }) => ReactElement;

export function TarjetaTitulo({ className, children, ...props }: ComponentProps<"h2">) {
    return (
        <TituloCard
            {...props}
            data-slot="tarjeta-titulo"
            className={cn("text-base leading-tight font-semibold tracking-[-0.015em]", className)}
            render={(dom) => <h2 {...dom} />}
        >
            {children}
        </TituloCard>
    );
}

export function TarjetaDescripcion({ className, ...props }: ComponentProps<"p">) {
    return <Card.Description data-slot="tarjeta-descripcion" className={cn("text-sm text-muted-foreground", className)} {...props} />;
}

export function TarjetaContenido({ className, ...props }: ComponentProps<"div">) {
    return <Card.Content data-slot="tarjeta-contenido" className={cn("block flex-initial flex-row gap-0 p-5 pt-0", className)} {...props} />;
}

export function TarjetaPie({ className, ...props }: ComponentProps<"div">) {
    return <Card.Footer data-slot="tarjeta-pie" className={cn("flex items-center gap-2 p-5 pt-0", className)} {...props} />;
}
