import { Tarjeta } from "@iris/ui";
import type { ReactNode } from "react";

/** Sección de la ficha del técnico: título, acción a la derecha y contenido. */
export function Seccion({ titulo, accion, className, children }: { titulo: string; accion?: ReactNode; className?: string; children: ReactNode }) {
    return (
        <Tarjeta className={`grid content-start gap-3 p-4 [&>*]:min-w-0 ${className ?? ""}`}>
            <div className="flex flex-wrap items-center justify-between gap-2">
                <h2 className="text-base font-semibold tracking-[-0.015em]">{titulo}</h2>
                {accion}
            </div>
            {children}
        </Tarjeta>
    );
}
