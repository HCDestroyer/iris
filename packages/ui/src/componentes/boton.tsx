import { buttonVariants } from "@heroui/styles";
import type { ComponentProps } from "react";
import { cn } from "../utilidades";
import { Ranura } from "./ranura";

export type VarianteBoton = "primario" | "secundario" | "contorno" | "fantasma" | "peligro" | "enlace";
export type TamanoBoton = "md" | "sm" | "lg" | "icono" | "icono-sm";

/** Variante de Iris → variante de HeroUI (`buttonVariants`). */
const VARIANTE_HEROUI = {
    primario: "primary",
    secundario: "tertiary",
    contorno: "outline",
    fantasma: "ghost",
    peligro: "outline",
    enlace: "ghost",
} as const satisfies Record<VarianteBoton, string>;

/**
 * Ajustes de Horus sobre la variante de HeroUI. Las medidas son las de siempre (40 / 32 / 44 px)
 * para que ninguna pantalla cambie de alto; el radio es el de Horus (6 px), no la píldora.
 */
const AJUSTE_VARIANTE: Record<VarianteBoton, string> = {
    // La acción primaria de la vista: única con halo (solo en oscuro, por token).
    primario: "shadow-glow hover:shadow-glow-fuerte",
    secundario: "text-foreground shadow-[0_0_0_1px_var(--hx-border)]",
    contorno: "",
    fantasma: "",
    // Acción destructiva (desactivar, quitar, descartar): borde y texto en tono de error.
    peligro: "border-error/60 text-error [--button-fg:var(--hx-error)] [--button-bg-hover:color-mix(in_oklab,var(--hx-error)_10%,transparent)]",
    enlace: "h-auto bg-transparent p-0 text-primary underline-offset-4 [--button-fg:var(--hx-primary)] [--button-bg-hover:transparent] hover:underline active:scale-100",
};

const AJUSTE_TAMANO: Record<TamanoBoton, string> = {
    md: "h-10 px-4 text-sm md:h-10",
    sm: "h-8 px-3 text-sm md:h-8",
    lg: "h-11 px-5 text-base md:h-11",
    icono: "size-10 p-0 md:size-10",
    "icono-sm": "size-8 p-0 md:size-8",
};

export type OpcionesBoton = { variante?: VarianteBoton | null | undefined; tamano?: TamanoBoton | null | undefined };

/** Clases del botón (HeroUI `buttonVariants` + ajustes de Horus), p. ej. para un `<label>` o `<a>`. */
export function variantesBoton({ variante, tamano }: OpcionesBoton = {}): string {
    const v = variante ?? "primario";
    const t = tamano ?? "md";
    const soloIcono = t === "icono" || t === "icono-sm";
    const tamanoHeroui = t === "sm" || t === "icono-sm" ? "sm" : t === "lg" ? "lg" : "md";
    return cn(
        buttonVariants({ variant: VARIANTE_HEROUI[v], size: tamanoHeroui, isIconOnly: soloIcono }),
        "rounded-md [&_svg]:size-4",
        AJUSTE_TAMANO[t],
        AJUSTE_VARIANTE[v],
    );
}

export type PropsBoton = ComponentProps<"button"> &
    OpcionesBoton & {
        /** Renderiza el hijo (p. ej. un `<Link>`) con el estilo del botón. */
        asChild?: boolean;
    };

/**
 * Botón de Horus con el estilo de HeroUI (`button`, `button--primary`…). Es un `<button>` nativo:
 * `onClick`, `title`, `type="submit"` y la propagación de eventos se comportan como siempre, y
 * dentro de un disparador de React Aria (menú, popover) recibe sus props por `Pressable`.
 * Los botones solo-icono llevan `aria-label` (y `title` como tooltip).
 */
export function Boton({ className, variante, tamano, asChild = false, type, ...props }: PropsBoton) {
    const clases = cn(variantesBoton({ variante, tamano }), className);
    if (asChild) {
        const { ref, ...resto } = props;
        return <Ranura data-slot="boton" className={clases} {...resto} ref={ref as ComponentProps<typeof Ranura>["ref"]} />;
    }
    return <button data-slot="boton" className={clases} type={type ?? "button"} {...props} />;
}
