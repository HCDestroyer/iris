"use client";

import { InsigniaEstado } from "@iris/ui";
import { Ban, Building2, CircleCheck, Globe, NotebookPen, Star } from "lucide-react";
import { TIPOS_ORDEN } from "@/lib/ordenes/presentacion";
import type { Plantilla } from "@/lib/plantillas/esquemas";

export function textoTipoPlantilla(tipo: Plantilla["tipo"]): string {
    return tipo ? TIPOS_ORDEN[tipo].texto : "Cualquiera";
}

/** Activa / inactiva / borrador, por defecto y común o de la empresa (icono + texto + color). */
export function InsigniasPlantilla({ p }: { p: Pick<Plantilla, "activa" | "borrador" | "porDefecto" | "empresaId"> }) {
    return (
        <span className="flex flex-wrap gap-1.5">
            {p.borrador ? (
                <InsigniaEstado tono="neutral" icono={NotebookPen} texto="Borrador" />
            ) : p.activa ? (
                <InsigniaEstado tono="success" icono={CircleCheck} texto="Activa" />
            ) : (
                <InsigniaEstado tono="neutral" icono={Ban} texto="Inactiva" />
            )}
            {p.porDefecto ? <InsigniaEstado tono="info" icono={Star} texto="Por defecto del tipo" /> : null}
            {p.empresaId === null ? <InsigniaEstado tono="neutral" icono={Globe} texto="Común" /> : <InsigniaEstado tono="neutral" icono={Building2} texto="De la empresa" />}
        </span>
    );
}
