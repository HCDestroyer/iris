import { useId, type ComponentProps } from "react";
import { cn } from "../utilidades";

/**
 * Marca de Iris (arcos) en blanco sobre el degradado índigo #4F46E5 → cian #06B6D4 (135°), radio
 * 9/32. Mismos trazos que `marca/logo-mark.svg` (copia de docs/marca/logo-mark.svg). Es el mismo en
 * claro y en oscuro: no se cambia de color, no se rota ni se le ponen sombras. Cada logo lleva su
 * propio id de degradado (puede haber varios en la página).
 */
export function LogoIris({ className, titulo, ...props }: ComponentProps<"svg"> & { titulo?: string }) {
    const id = `iris-degradado-${useId().replace(/[^a-zA-Z0-9_-]/g, "")}`;
    return (
        <svg
            viewBox="0 0 32 32"
            role={titulo ? "img" : undefined}
            aria-label={titulo}
            aria-hidden={titulo ? undefined : true}
            className={cn("size-8 shrink-0", className)}
            {...props}
        >
            <defs>
                <linearGradient id={id} x1="0" y1="0" x2="1" y2="1">
                    <stop offset="0" stopColor="#4f46e5" />
                    <stop offset="1" stopColor="#06b6d4" />
                </linearGradient>
            </defs>
            <rect width="32" height="32" rx="9" fill={`url(#${id})`} />
            <g fill="none" stroke="#fff" strokeLinecap="round" strokeWidth="2.6">
                <path d="M6 22a10 10 0 0 1 20 0" />
                <path d="M9.5 22a6.5 6.5 0 0 1 13 0" strokeOpacity=".7" />
                <path d="M13 22a3 3 0 0 1 6 0" strokeOpacity=".45" />
            </g>
        </svg>
    );
}

/** Logotipo: marca + «Iris» en la fuente del sistema, negrita, tracking −0,015 em. */
export function MarcaIris({ className, ...props }: ComponentProps<"span">) {
    return (
        <span data-slot="marca" className={cn("inline-flex items-center gap-2.5", className)} {...props}>
            <LogoIris className="size-7" />
            <span className="text-[1.0625rem] leading-none font-bold tracking-[-0.015em]">Iris</span>
        </span>
    );
}
