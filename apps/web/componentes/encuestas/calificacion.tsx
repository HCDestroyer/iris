import { InsigniaEstado } from "@iris/ui";
import { TriangleAlert } from "lucide-react";
import { esCalificacion, presentacionEstado, TEXTO_CALIFICACION } from "@/lib/encuestas/presentacion";

/** Calificación con icono y texto («4 · Bien»); sin calificar, nada. */
export function CalificacionEncuesta({ calificacion }: { calificacion: number | null | undefined }) {
    if (!esCalificacion(calificacion)) return null;
    const { texto, icono: Icono } = TEXTO_CALIFICACION[calificacion];
    return (
        <span className="inline-flex items-center gap-1.5 text-sm font-medium tabular-nums" data-calificacion={calificacion}>
            <Icono aria-hidden className="size-4 shrink-0 text-muted-foreground" />
            {calificacion} · {texto}
        </span>
    );
}

/** Alerta (calificación ≤ 2): icono + texto + color. */
export function InsigniaAlerta() {
    return <InsigniaEstado tono="warning" icono={TriangleAlert} texto="Alerta" />;
}

export function InsigniaEstadoEncuesta({ estado }: { estado: string }) {
    const p = presentacionEstado(estado);
    return <InsigniaEstado tono={p.tono} icono={p.icono} texto={p.texto} />;
}
