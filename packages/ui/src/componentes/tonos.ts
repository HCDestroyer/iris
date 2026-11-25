import { CircleAlert, CircleCheck, CircleX, Info, type LucideIcon } from "lucide-react";

/**
 * Tonos semánticos de estado (skill horus-identity, references/estados.md). La urgencia decide el
 * color; el icono distingue y el texto siempre está. `primary` nunca es un estado.
 */
export type Tono = "neutral" | "info" | "success" | "warning" | "error";

export const iconoPorTono: Record<Tono, LucideIcon> = {
    neutral: Info,
    info: Info,
    success: CircleCheck,
    warning: CircleAlert,
    error: CircleX,
};

/** Texto e icono del tono. */
export const textoPorTono: Record<Tono, string> = {
    neutral: "text-muted-foreground",
    info: "text-info",
    success: "text-success",
    warning: "text-warning",
    error: "text-error",
};

/**
 * Fondo tintado y SÓLIDO (10 % del tono sobre el lienzo del tema, sin transparencia: lo de atrás no
 * se ve a través) y borde al 30 % del tono. Lo usan `Aviso` e `InsigniaEstado`.
 */
export const superficiePorTono: Record<Tono, string> = {
    neutral: "bg-elevated border-border",
    info: "bg-[color-mix(in_oklab,var(--hx-info)_10%,var(--hx-bg))] border-info/30",
    success: "bg-[color-mix(in_oklab,var(--hx-success)_10%,var(--hx-bg))] border-success/30",
    warning: "bg-[color-mix(in_oklab,var(--hx-warning)_10%,var(--hx-bg))] border-warning/30",
    error: "bg-[color-mix(in_oklab,var(--hx-error)_10%,var(--hx-bg))] border-error/30",
};

/**
 * Fondo de la insignia (píldora del rediseño): el `-fondo` del estado, sin borde. Los textos pasan
 * 4,5:1 sobre su fondo en los dos temas (`docs/ui/guia.md`, «Contraste»).
 */
export const fondoInsigniaPorTono: Record<Tono, string> = {
    neutral: "border-transparent bg-neutro-fondo text-neutro",
    info: "border-transparent bg-info-fondo",
    success: "border-transparent bg-exito-fondo",
    warning: "border-transparent bg-atencion-fondo",
    error: "border-transparent bg-peligro-fondo",
};

/**
 * Tono de Iris → estado de HeroUI (`Alert`, `Toast`). HeroUI no tiene «info» ni «neutral» y su
 * `accent` es el índigo de la marca (nunca un estado): los dos van a `default` con el color del tono.
 */
export const estadoHeroui = {
    neutral: "default",
    info: "default",
    success: "success",
    warning: "warning",
    error: "danger",
} as const satisfies Record<Tono, string>;

/** Tono de Iris → color de HeroUI `Chip`. */
export const colorChipHeroui = estadoHeroui;
