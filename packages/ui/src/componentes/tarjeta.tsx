import { cardVariants } from "@heroui/styles";
import type { ComponentProps } from "react";
import { cn } from "../utilidades";

/*
 * Tarjeta de contenido (material casi sólido) con las piezas de HeroUI `Card` (`card`,
 * `card__header`, `card__title`…). El relleno y el radio son los de Horus (`surface-card`, 14 px);
 * nunca dentro de vidrio sobre vidrio.
 */
const piezas = cardVariants();

/*
 * `.card` de HeroUI trae su propio acomodo (flex en columna, separación y relleno). Se anula para que
 * cada pantalla siga mandando con sus clases (`grid`, `flex flex-wrap`, `p-4`…) como antes.
 */
const NEUTRO = "block flex-row flex-nowrap items-stretch gap-0 overflow-visible p-0";

export function Tarjeta({ className, ...props }: ComponentProps<"div">) {
    return (
        <div
            data-slot="tarjeta"
            className={piezas.base({ className: cn(NEUTRO, "surface-card text-card-foreground", className) })}
            {...props}
        />
    );
}

export function TarjetaEncabezado({ className, ...props }: ComponentProps<"div">) {
    return <div data-slot="tarjeta-encabezado" className={piezas.header({ className: cn("grid gap-1 p-5 pb-3", className) })} {...props} />;
}

export function TarjetaTitulo({ className, ...props }: ComponentProps<"h2">) {
    return (
        <h2
            data-slot="tarjeta-titulo"
            className={piezas.title({ className: cn("text-base leading-tight font-semibold tracking-[-0.015em]", className) })}
            {...props}
        />
    );
}

export function TarjetaDescripcion({ className, ...props }: ComponentProps<"p">) {
    return <p data-slot="tarjeta-descripcion" className={piezas.description({ className: cn("text-sm text-muted-foreground", className) })} {...props} />;
}

export function TarjetaContenido({ className, ...props }: ComponentProps<"div">) {
    return <div data-slot="tarjeta-contenido" className={piezas.content({ className: cn("block flex-initial flex-row gap-0 p-5 pt-0", className) })} {...props} />;
}

export function TarjetaPie({ className, ...props }: ComponentProps<"div">) {
    return <div data-slot="tarjeta-pie" className={piezas.footer({ className: cn("flex items-center gap-2 p-5 pt-0", className) })} {...props} />;
}
