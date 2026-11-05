import { cn } from "@iris/ui";
import type { ComponentProps, ReactNode } from "react";

/**
 * Bloque del panel (nuevo diseño): superficie de tarjeta con borde fino de 1 px y radio de 12 px,
 * con su cabecera (título de 15 px, contador opcional y una acción a la derecha). Lo usan el
 * Inicio, la agenda del mes y el mapa.
 */
export function Bloque({
    titulo,
    contador,
    accion,
    children,
    className,
    cuerpo,
    ...props
}: Omit<ComponentProps<"section">, "title"> & {
    titulo?: ReactNode;
    contador?: number | string | undefined;
    accion?: ReactNode;
    /** Clases del contenido (por defecto sin relleno: cada bloque pone el suyo). */
    cuerpo?: string;
}) {
    return (
        <section className={cn("min-w-0 rounded-xl border border-border bg-card", className)} {...props}>
            {titulo ? (
                <div className="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
                    <h2 className="text-[0.9375rem] font-semibold">
                        {titulo}
                        {contador !== undefined ? <span className="ml-1.5 text-xs font-semibold text-muted-foreground tabular-nums">{contador}</span> : null}
                    </h2>
                    {accion}
                </div>
            ) : null}
            <div className={cuerpo}>{children}</div>
        </section>
    );
}

/** Enlace de texto de una cabecera de bloque («Ver todo», «Ver en el mapa»). */
export const claseEnlaceBloque = "text-[0.8125rem] font-medium text-primary underline-offset-4 hover:underline";
