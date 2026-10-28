import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

/** Une clases de Tailwind resolviendo conflictos (el `cn` de shadcn/ui). */
export function cn(...entradas: ClassValue[]): string {
    return twMerge(clsx(entradas));
}
