import type { EstadoOrden } from "@iris/sync";
import { InsigniaEstado } from "@iris/ui";
import { ESTADOS } from "@/lib/campo/textos";

export function InsigniaOrden({ estado, className }: { estado: EstadoOrden; className?: string }) {
    const p = ESTADOS[estado];
    return <InsigniaEstado tono={p.tono} icono={p.icono} texto={p.texto} {...(className ? { className } : {})} />;
}
