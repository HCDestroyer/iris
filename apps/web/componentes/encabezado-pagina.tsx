import { BotonManualPantalla } from "@iris/ui";
import type { ReactNode } from "react";

/** Título de una pantalla del panel con su «?» del manual (la clave sale de la ruta) y sus acciones. */
export function EncabezadoPagina({ titulo, descripcion, acciones }: { titulo: string; descripcion?: ReactNode; acciones?: ReactNode }) {
    return (
        <div className="mb-5 flex flex-wrap items-end justify-between gap-3">
            <div className="grid gap-1">
                <div className="flex min-w-0 items-center gap-1.5">
                    <h1 className="text-[1.625rem] leading-tight font-bold tracking-[-0.015em]">{titulo}</h1>
                    <BotonManualPantalla />
                </div>
                {descripcion ? <p className="text-sm text-muted-foreground">{descripcion}</p> : null}
            </div>
            {acciones}
        </div>
    );
}
