import type { ReactNode } from "react";

export function EncabezadoPagina({ titulo, descripcion, acciones }: { titulo: string; descripcion?: ReactNode; acciones?: ReactNode }) {
    return (
        <div className="mb-5 flex flex-wrap items-end justify-between gap-3">
            <div className="grid gap-1">
                <h1 className="text-2xl font-semibold tracking-[-0.015em]">{titulo}</h1>
                {descripcion ? <p className="text-sm text-muted-foreground">{descripcion}</p> : null}
            </div>
            {acciones}
        </div>
    );
}
