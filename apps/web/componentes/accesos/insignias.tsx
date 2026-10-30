import { InsigniaEstado } from "@iris/ui";
import { CANALES, ESTADOS_ACTIVIDAD, RESULTADOS, type CanalAcceso, type EstadoActividad, type ResultadoAcceso } from "@/lib/accesos/presentacion";

export function InsigniaResultado({ resultado }: { resultado: ResultadoAcceso }) {
    const r = RESULTADOS[resultado];
    return <InsigniaEstado tono={r.tono} icono={r.icono} texto={r.etiqueta} />;
}

export function Canal({ canal }: { canal: CanalAcceso }) {
    const c = CANALES[canal];
    return (
        <span className="inline-flex items-center gap-1.5 text-sm">
            <c.icono aria-hidden className="size-4 shrink-0 text-muted-foreground" />
            {c.etiqueta}
        </span>
    );
}

export function InsigniaActividad({ estado, texto }: { estado: EstadoActividad; texto?: string }) {
    const e = ESTADOS_ACTIVIDAD[estado];
    return <InsigniaEstado tono={e.tono} icono={e.icono} texto={texto ?? e.etiqueta} />;
}
