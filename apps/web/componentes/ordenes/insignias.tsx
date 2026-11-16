"use client";

import { InsigniaEstado } from "@iris/ui";
import { Sparkles } from "lucide-react";
import {
    ESTADOS_ORDEN,
    ESTADOS_PENDIENTE,
    PRESENTACION_CONFLICTO,
    TIPOS_ORDEN,
    textoMarcaPendiente,
    type EstadoOrden,
    type EstadoPendiente,
    type ResumenPendientes,
    type TipoOrden,
} from "@/lib/ordenes/presentacion";

export function InsigniaEstadoOrden({ estado }: { estado: EstadoOrden }) {
    const p = ESTADOS_ORDEN[estado];
    return <InsigniaEstado tono={p.tono} icono={p.icono} texto={p.texto} data-estado-orden={estado} />;
}

/** Tipo de orden: clasifica, no urge (neutral). */
export function InsigniaTipoOrden({ tipo }: { tipo: TipoOrden }) {
    const p = TIPOS_ORDEN[tipo];
    return <InsigniaEstado tono="neutral" icono={p.icono} texto={p.texto} data-tipo-orden={tipo} />;
}

/**
 * De dónde salió la orden, solo cuando hace falta decirlo: la que creó el asistente de IA al agendar
 * una visita con el cliente (`origen: "asistente"`, contratos/conversaciones.md §17.10). Las demás
 * (panel, API, Argos, sistema) no llevan marca.
 */
export function InsigniaOrigenOrden({ origen }: { origen?: string | undefined }) {
    if (origen !== "asistente") return null;
    return <InsigniaEstado tono="neutral" icono={Sparkles} texto="Agendada por el asistente" data-origen-orden={origen} />;
}

export function InsigniaPendiente({ estado }: { estado: EstadoPendiente }) {
    const p = ESTADOS_PENDIENTE[estado];
    return <InsigniaEstado tono={p.tono} icono={p.icono} texto={p.texto} data-estado-pendiente={estado} />;
}

/** «Pendiente en Argos · <estado>»: el peor de los pendientes no hechos de la orden. */
export function MarcaPendienteArgos({ resumen }: { resumen: ResumenPendientes }) {
    const p = resumen.cuantos === 0 ? PRESENTACION_CONFLICTO : ESTADOS_PENDIENTE[resumen.peor];
    return (
        <InsigniaEstado
            tono={p.tono}
            icono={p.icono}
            texto={textoMarcaPendiente(resumen)}
            data-testid="pendiente-argos"
            className="max-w-full whitespace-normal"
            data-peor={resumen.peor}
            title={resumen.conflicto ? "Hay un cierre repetido con otros datos: corrígelo a mano en Argos" : undefined}
        />
    );
}
