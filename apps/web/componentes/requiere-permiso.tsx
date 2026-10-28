"use client";

import { EstadoVacio, Tarjeta } from "@iris/ui";
import { Lock } from "lucide-react";
import type { ReactNode } from "react";
import type { Permiso } from "@/lib/menu";
import { useSesion } from "@/lib/sesion";

/** Muestra `children` solo con alguno de los permisos; si no, un estado «Sin permiso». */
export function RequierePermiso({ permisos, children }: { permisos: readonly Permiso[] | null; children: ReactNode }) {
    const { puede } = useSesion();
    if (puede(permisos)) {
        return children;
    }
    return (
        <Tarjeta>
            <EstadoVacio
                icono={Lock}
                titulo="No tienes permiso para ver esta sección"
                descripcion="Pide a un administrador de Iris el rol que la incluye."
            />
        </Tarjeta>
    );
}
