import { cva, type VariantProps } from "class-variance-authority";
import { Slot } from "radix-ui";
import type { ComponentProps } from "react";
import { cn } from "../utilidades";

export const variantesBoton = cva(
    [
        "inline-flex shrink-0 items-center justify-center gap-2 rounded-md font-medium whitespace-nowrap",
        "transition-[background-color,box-shadow,color] duration-150 select-none",
        "disabled:pointer-events-none disabled:opacity-55",
        "[&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0",
    ],
    {
        variants: {
            variante: {
                // La acción primaria de la vista: única con halo (solo en oscuro, por token).
                primario: "bg-primary text-primary-foreground shadow-glow hover:bg-primary-hover hover:shadow-glow-fuerte",
                secundario: "bg-elevated text-foreground shadow-[0_0_0_1px_var(--hx-border)] hover:bg-muted",
                contorno: "border border-border bg-transparent text-foreground hover:bg-elevated",
                fantasma: "bg-transparent text-foreground hover:bg-elevated",
                // Acción destructiva (desactivar, quitar, descartar): borde y texto en tono de error.
                peligro: "border border-error/60 bg-transparent text-error hover:bg-error/10",
                enlace: "h-auto bg-transparent p-0 text-primary underline-offset-4 hover:underline",
            },
            tamano: {
                md: "h-10 px-4 text-sm",
                sm: "h-8 px-3 text-sm",
                lg: "h-11 px-5 text-base",
                icono: "size-10",
                "icono-sm": "size-8",
            },
        },
        defaultVariants: { variante: "primario", tamano: "md" },
    },
);

export type PropsBoton = ComponentProps<"button"> &
    VariantProps<typeof variantesBoton> & {
        /** Renderiza el hijo (p. ej. un `<Link>`) con el estilo del botón. */
        asChild?: boolean;
    };

/** Botón de Horus. Los botones solo-icono llevan `aria-label` (y `title` como tooltip). */
export function Boton({ className, variante, tamano, asChild = false, type, ...props }: PropsBoton) {
    const Componente = asChild ? Slot.Root : "button";
    return (
        <Componente
            data-slot="boton"
            className={cn(variantesBoton({ variante, tamano }), className)}
            {...(asChild ? {} : { type: type ?? "button" })}
            {...props}
        />
    );
}
