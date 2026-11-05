"use client";

import { Button } from "@heroui/react";
import { buttonVariants } from "@heroui/styles";
import { isValidElement, type ComponentProps, type MouseEvent, type ReactElement, type ReactNode } from "react";
import { cn } from "../utilidades";
import { Ranura } from "./ranura";

export type VarianteBoton = "primario" | "tinta" | "secundario" | "contorno" | "fantasma" | "peligro" | "enlace";
export type TamanoBoton = "md" | "sm" | "lg" | "icono" | "icono-sm";

/** Variante de Iris → variante de HeroUI (`Button variant`). */
const VARIANTE_HEROUI = {
    primario: "primary",
    tinta: "primary",
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
    // La acción principal de una pantalla del técnico («Empezar trabajo»): neutra, sin halo.
    tinta: "bg-tinta text-sobre-tinta shadow-none [--button-bg:var(--iris-tinta)] [--button-fg:var(--iris-sobre-tinta)] [--button-bg-hover:color-mix(in_oklab,var(--iris-tinta)_88%,var(--iris-sobre-tinta))] hover:bg-[color-mix(in_oklab,var(--iris-tinta)_88%,var(--iris-sobre-tinta))]",
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

function opcionesHeroui({ variante, tamano }: OpcionesBoton) {
    const v = variante ?? "primario";
    const t = tamano ?? "md";
    return {
        v,
        t,
        variant: VARIANTE_HEROUI[v],
        size: t === "sm" || t === "icono-sm" ? ("sm" as const) : t === "lg" ? ("lg" as const) : ("md" as const),
        isIconOnly: t === "icono" || t === "icono-sm",
    };
}

/** Ajustes de Horus que van encima de las clases de HeroUI. */
function ajustesHorus(v: VarianteBoton, t: TamanoBoton): string {
    return cn("rounded-md [&_svg]:size-4", AJUSTE_TAMANO[t], AJUSTE_VARIANTE[v]);
}

/**
 * Clases del botón (HeroUI `buttonVariants` + ajustes de Horus) para lo que no es un `Boton`: un
 * `<label>` (p. ej. `Camara`) o un `<a>` con `asChild`.
 */
export function variantesBoton(opciones: OpcionesBoton = {}): string {
    const { v, t, variant, size, isIconOnly } = opcionesHeroui(opciones);
    return cn(buttonVariants({ variant, size, isIconOnly }), ajustesHorus(v, t));
}

export type PropsBoton = ComponentProps<"button"> &
    OpcionesBoton & {
        /** Renderiza el hijo (p. ej. un `<Link>`) con el estilo del botón. */
        asChild?: boolean;
    };

/*
 * Atributos que el `Button` de React Aria no deja pasar (filtra lo que no conoce): van directo al
 * `<button>` por `render`. `title` (globo del navegador), `role`, `tabIndex`, `aria-invalid`,
 * `aria-keyshortcuts` y el arrastre nativo (`draggable`, `onDrag*`, `onDrop`), que usa el
 * calendario de despacho; React Aria no impide el arrastre de un elemento `draggable`.
 */
const DIRECTO = /^(title|role|tabIndex|draggable|onDrag[A-Za-z]*|onDrop|aria-(invalid|keyshortcuts|busy|checked|selected|owns|live|atomic|errormessage|roledescription|hidden))$/;

/**
 * Botón de Horus: el `Button` de HeroUI (React Aria) con las variantes de Iris. `disabled`,
 * `onClick`, `type="submit"`, `title` y los `data-*` funcionan como en un `<button>`; React Aria
 * agrega el estado de presión, el foco visible (`data-focus-visible`) y el teclado. Como todo
 * `Button` de React Aria, el clic no sigue subiendo a los padres. Dentro de un disparador de
 * React Aria (menú, popover, tooltip) se conecta solo. Los botones solo-icono llevan `aria-label`
 * (y `title` como tooltip). Con `asChild`, el hijo (un enlace) toma el estilo.
 */
export function Boton({ className, variante, tamano, asChild = false, type, disabled, onClick, children, ...props }: PropsBoton) {
    const { v, t, variant, size, isIconOnly } = opcionesHeroui({ variante, tamano });
    if (asChild) {
        const { ref, ...resto } = props;
        return (
            <Ranura
                data-slot="boton"
                className={cn(variantesBoton({ variante, tamano }), className)}
                {...(onClick ? { onClick: onClick as ComponentProps<typeof Ranura>["onClick"] } : {})}
                {...resto}
                ref={ref as ComponentProps<typeof Ranura>["ref"]}
            >
                {children}
            </Ranura>
        );
    }
    const directos: Record<string, unknown> = {};
    const paraAria: Record<string, unknown> = {};
    for (const [clave, valor] of Object.entries(props)) {
        if (DIRECTO.test(clave)) directos[clave] = valor;
        else paraAria[clave] = valor;
    }
    return (
        <Button
            data-slot="boton"
            variant={variant}
            size={size}
            isIconOnly={isIconOnly}
            className={cn(ajustesHorus(v, t), className)}
            type={type ?? "button"}
            isDisabled={disabled ?? false}
            {...(onClick ? { onClick: onClick as (evento: MouseEvent<Element>) => void } : {})}
            {...(paraAria as ComponentProps<typeof Button>)}
            render={(dom) => <button {...(dom as ComponentProps<"button">)} {...directos} />}
        >
            {children as ReactNode}
        </Button>
    );
}

/**
 * ¿El elemento es un `Boton` de React Aria? Esos se conectan solos con los disparadores (menú,
 * popover, globo); el resto (enlaces, `asChild`) se envuelve con `Pressable` o `Focusable`.
 */
export function esBotonAria(elemento: ReactElement): boolean {
    return isValidElement<PropsBoton>(elemento) && elemento.type === Boton && !elemento.props.asChild;
}
