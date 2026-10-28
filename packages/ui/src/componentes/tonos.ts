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

/** Fondo tintado al 10 % y borde al 30 % del tono. */
export const superficiePorTono: Record<Tono, string> = {
    neutral: "bg-elevated border-border",
    info: "bg-info/10 border-info/30",
    success: "bg-success/10 border-success/30",
    warning: "bg-warning/10 border-warning/30",
    error: "bg-error/10 border-error/30",
};
