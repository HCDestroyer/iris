import { cn } from "@iris/ui";
import { Tag } from "lucide-react";
import { tonoDe, type CategoriaRef } from "@/lib/asistente/fase3";
import { CLASE_ETIQUETA } from "@/lib/conversaciones/presentacion";

/**
 * Chip de la categoría del mensaje (fase 3 del asistente): icono + texto + color, nunca solo
 * color. Los tonos son los de las etiquetas (tokens de Horus); uno desconocido se ve neutro.
 */
export function ChipCategoria({ categoria, className }: { categoria: Pick<CategoriaRef, "nombre" | "tono">; className?: string }) {
    return (
        <span
            data-slot="chip-categoria"
            data-tono={tonoDe(categoria.tono)}
            className={cn("inline-flex h-[1.375rem] max-w-full min-w-0 items-center gap-1 rounded-full px-2 text-xs font-semibold", CLASE_ETIQUETA[tonoDe(categoria.tono)], className)}
        >
            <Tag aria-hidden className="size-3 shrink-0" />
            <span className="truncate">{categoria.nombre}</span>
        </span>
    );
}
