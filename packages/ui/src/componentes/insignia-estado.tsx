import type { LucideIcon } from "lucide-react";
import type { ComponentProps } from "react";
import { cn } from "../utilidades";
import { iconoPorTono, superficiePorTono, textoPorTono, type Tono } from "./tonos";

export type PropsInsigniaEstado = Omit<ComponentProps<"span">, "children"> & {
    tono: Tono;
    /** Texto del estado: obligatorio, el color nunca va solo. */
    texto: string;
    /** Icono de Lucide que identifica el estado; por defecto, el del tono. */
    icono?: LucideIcon;
};

/** Estado = icono + texto + color (nunca solo color). */
export function InsigniaEstado({ tono, texto, icono, className, ...props }: PropsInsigniaEstado) {
    const Icono = icono ?? iconoPorTono[tono];
    return (
        <span
            data-slot="insignia-estado"
            data-tono={tono}
            className={cn(
                "inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-xs font-medium whitespace-nowrap",
                superficiePorTono[tono],
                textoPorTono[tono],
                className,
            )}
            {...props}
        >
            <Icono aria-hidden className="size-3.5 shrink-0" />
            {texto}
        </span>
    );
}
