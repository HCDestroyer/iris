import type { LucideIcon } from "lucide-react";
import type { ComponentProps } from "react";
import { cn } from "../utilidades";
import { ChipEstado } from "./chip-estado";
import { colorChipHeroui, fondoInsigniaPorTono, iconoPorTono, textoPorTono, type Tono } from "./tonos";

export type PropsInsigniaEstado = Omit<ComponentProps<"span">, "children"> & {
    tono: Tono;
    /** Texto del estado: obligatorio, el color nunca va solo. */
    texto: string;
    /** Icono de Lucide que identifica el estado; por defecto, el del tono. */
    icono?: LucideIcon;
};

/**
 * Estado = icono + texto + color (nunca solo color): el `Chip` de HeroUI, variante suave. Sirve
 * también desde un componente de servidor: el icono llega ya dibujado.
 */
export function InsigniaEstado({ tono, texto, icono, className, ...props }: PropsInsigniaEstado) {
    const Icono = icono ?? iconoPorTono[tono];
    return (
        <ChipEstado
            {...props}
            color={colorChipHeroui[tono]}
            tono={tono}
            className={cn(
                "inline-flex min-h-6 items-center gap-1 rounded-full border px-2.5 py-0.5 text-xs font-semibold whitespace-nowrap",
                textoPorTono[tono],
                fondoInsigniaPorTono[tono],
                className,
            )}
            texto={texto}
        >
            <Icono aria-hidden className="size-3.5 shrink-0" />
        </ChipEstado>
    );
}
