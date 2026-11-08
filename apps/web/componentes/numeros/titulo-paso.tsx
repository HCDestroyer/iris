import { cn } from "@iris/ui";
import { Check } from "lucide-react";

/** Título de un paso numerado («1 En Meta»); hecho, el número pasa a una marca teal. Va dentro del `<h2>` de la sección. */
export function TituloPaso({ numero, titulo, listo = false }: { numero: number; titulo: string; listo?: boolean }) {
    return (
        <span className="flex items-center gap-2.5">
            <span
                aria-hidden
                className={cn(
                    "grid size-7 shrink-0 place-items-center rounded-full text-sm font-semibold",
                    listo ? "bg-primary text-primary-foreground" : "border border-primary/40 bg-primary/10 text-primary",
                )}
            >
                {listo ? <Check className="size-4" /> : numero}
            </span>
            <span>
                <span className="sr-only">{listo ? `Paso ${numero} (listo): ` : `Paso ${numero}: `}</span>
                {titulo}
            </span>
        </span>
    );
}
