"use client";

import { cn, InsigniaEstado } from "@iris/ui";
import { useEffect, useState } from "react";
import { ESTADOS, PRIORIDADES, type EstadoTicket, type Prioridad } from "@/lib/tickets/presentacion";
import { describirSla, NOMBRE_RELOJ, type RelojSLA, type TipoReloj } from "@/lib/tickets/sla";

/** Hora actual que avanza cada `cadaMs` (para el tiempo restante del SLA). */
export function useAhora(cadaMs = 30_000): number {
    const [ahora, setAhora] = useState(() => Date.now());
    useEffect(() => {
        const t = setInterval(() => setAhora(Date.now()), cadaMs);
        return () => clearInterval(t);
    }, [cadaMs]);
    return ahora;
}

export function InsigniaEstadoTicket({ estado }: { estado: EstadoTicket }) {
    const p = ESTADOS[estado];
    return <InsigniaEstado tono={p.tono} icono={p.icono} texto={p.texto} data-estado={estado} />;
}

export function InsigniaPrioridad({ prioridad }: { prioridad: Prioridad }) {
    const p = PRIORIDADES[prioridad];
    return <InsigniaEstado tono={p.tono} icono={p.icono} texto={p.texto} data-prioridad={prioridad} />;
}

/**
 * SLA = icono + texto + color, con el reloj («Respuesta»/«Resolución») y el tiempo legible.
 * `compacto`: una línea para la bandeja; si no, además la fecha (America/Guatemala).
 */
export function SlaVisible({
    reloj,
    tipo,
    ahora,
    compacto = false,
    className,
}: {
    reloj: RelojSLA;
    tipo: TipoReloj;
    ahora: number;
    compacto?: boolean;
    className?: string;
}) {
    const d = describirSla(reloj, ahora);
    return (
        <span className={cn("inline-grid gap-0.5", className)} data-sla={d.estado} data-reloj={tipo}>
            <span className="inline-flex flex-wrap items-center gap-1.5">
                <InsigniaEstado tono={d.tono} icono={d.icono} texto={d.texto} />
                {compacto ? <span className="text-xs text-muted-foreground">{NOMBRE_RELOJ[tipo]}</span> : null}
            </span>
            {d.detalle ? (
                <span className="text-xs text-muted-foreground tabular-nums">
                    {d.detalle}
                    {!compacto && d.fecha ? ` · ${d.fecha}` : null}
                </span>
            ) : null}
        </span>
    );
}
