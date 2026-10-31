import { chipVariants } from "@heroui/styles";
import type { LucideIcon } from "lucide-react";
import type { ComponentProps } from "react";
import { cn } from "../utilidades";
import { colorChipHeroui, iconoPorTono, superficiePorTono, textoPorTono, type Tono } from "./tonos";

export type PropsInsigniaEstado = Omit<ComponentProps<"span">, "children"> & {
    tono: Tono;
    /** Texto del estado: obligatorio, el color nunca va solo. */
    texto: string;
    /** Icono de Lucide que identifica el estado; por defecto, el del tono. */
    icono?: LucideIcon;
};

/** Estado = icono + texto + color (nunca solo color), sobre HeroUI `Chip` (variante suave). */
export function InsigniaEstado({ tono, texto, icono, className, ...props }: PropsInsigniaEstado) {
    const Icono = icono ?? iconoPorTono[tono];
    const piezas = chipVariants({ color: colorChipHeroui[tono], variant: "soft", size: "sm" });
    return (
        <span
            data-slot="insignia-estado"
            data-tono={tono}
            className={piezas.base({
                className: cn(
                    "inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-xs font-medium whitespace-nowrap",
                    superficiePorTono[tono],
                    textoPorTono[tono],
                    className,
                ),
            })}
            {...props}
        >
            <Icono aria-hidden className="size-3.5 shrink-0" />
            {texto}
        </span>
    );
}
