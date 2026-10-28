import type { ComponentProps } from "react";
import { cn } from "../utilidades";

/**
 * Marca de Iris (arco) en blanco sobre teal 700, radio 7/32. Mismos trazos que
 * `marca/logo-mark.svg` (copia de docs/marca/logo-mark.svg). En oscuro el cuadrado sigue en
 * teal 700: no se cambia de color, no se rota ni se le ponen sombras.
 */
export function LogoIris({ className, titulo, ...props }: ComponentProps<"svg"> & { titulo?: string }) {
    return (
        <svg
            viewBox="0 0 32 32"
            role={titulo ? "img" : undefined}
            aria-label={titulo}
            aria-hidden={titulo ? undefined : true}
            className={cn("size-8 shrink-0", className)}
            {...props}
        >
            <rect width="32" height="32" rx="7" fill="#0f766e" />
            <g fill="none" stroke="#fff" strokeLinecap="round" strokeWidth="2.6">
                <path d="M6 22a10 10 0 0 1 20 0" />
                <path d="M9.5 22a6.5 6.5 0 0 1 13 0" strokeOpacity=".7" />
                <path d="M13 22a3 3 0 0 1 6 0" strokeOpacity=".45" />
            </g>
        </svg>
    );
}

/** Logotipo: marca + «Iris» en la fuente del sistema, semibold, tracking −0,015 em. */
export function MarcaIris({ className, ...props }: ComponentProps<"span">) {
    return (
        <span data-slot="marca" className={cn("inline-flex items-center gap-2.5", className)} {...props}>
            <LogoIris className="size-7" />
            <span className="text-lg leading-none font-semibold tracking-[-0.015em]">Iris</span>
        </span>
    );
}
