"use client";

import { Aviso, Boton } from "@iris/ui";
import { RotateCw } from "lucide-react";
import type { Cuadrilla, Zona } from "@/lib/despacho/calendario";
import type { Lectura } from "@/lib/lectura";

/** Catálogo de zonas o cuadrillas sin leer: se avisa con «Reintentar» (los nombres quedan como «Zona N»). */
export function AvisoCatalogos({ zonas, cuadrillas }: { zonas: Lectura<Zona[]>; cuadrillas: Lectura<Cuadrilla[]> }) {
    const fallidos = [zonas.estado === "error" ? "las zonas" : null, cuadrillas.estado === "error" ? "las cuadrillas" : null].filter(Boolean);
    if (fallidos.length === 0) return null;
    return (
        <Aviso
            tono="warning"
            className="mb-4"
            titulo={`No se pudo leer el catálogo de ${fallidos.join(" y ")}`}
            accion={
                <Boton
                    variante="secundario"
                    tamano="sm"
                    onClick={() => {
                        if (zonas.estado === "error") zonas.reintentar();
                        if (cuadrillas.estado === "error") cuadrillas.reintentar();
                    }}
                >
                    <RotateCw />
                    Reintentar
                </Boton>
            }
        >
            El filtro queda desactivado y se muestran los números en lugar de los nombres.
        </Aviso>
    );
}
