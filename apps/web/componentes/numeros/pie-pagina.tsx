import { cn } from "@iris/ui";
import type { ReactNode } from "react";

/** Pie fijo de una pantalla sin formulario («Terminar después»), con el mismo aspecto que `BarraGuardar`. */
export function PiePagina({ nota, children }: { nota?: ReactNode; children: ReactNode }) {
    return (
        <div
            data-slot="barra-guardar"
            className={cn(
                "glass-material sticky bottom-0 z-20 mt-2 flex min-w-0 flex-col gap-2 rounded-t-[var(--hx-radius-card)] border-t px-4 pt-3",
                "pb-[max(0.75rem,env(safe-area-inset-bottom))] sm:flex-row sm:items-center sm:justify-end",
            )}
        >
            {nota ? <p className="min-w-0 text-sm text-muted-foreground sm:mr-auto">{nota}</p> : null}
            <div className="flex flex-col-reverse gap-2 sm:flex-row">{children}</div>
        </div>
    );
}
