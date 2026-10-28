"use client";

import { InsigniaEstado } from "@iris/ui";
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
