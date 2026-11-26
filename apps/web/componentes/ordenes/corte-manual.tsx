"use client";

import { InsigniaEstado, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { ArrowUpCircle, Tv } from "lucide-react";
import { ESTADOS_CORTE, MOTIVOS_CORTE, type CorteManual, type Orden } from "@/lib/ordenes/presentacion";
import { formatearFecha } from "@/lib/fechas";

/*
 * Corte manual de respaldo del cable (contrato con Argos 0.15.0 §13.2, B34): Argos no pudo cortar o
 * reconectar el CATV por SmartOLT y lo pide en campo. La orden muestra la causa, qué se toca (solo el
 * cable) y lo que Iris le informa a Argos. Cada estado lleva ícono, texto y color.
 */

/** «Prioridad alta» (la reconexión de cable siempre la lleva). Nada con prioridad normal. */
export function InsigniaPrioridad({ prioridad }: { prioridad: Orden["prioridad"] }) {
    if (prioridad !== "alta") return null;
    return <InsigniaEstado tono="warning" icono={ArrowUpCircle} texto="Prioridad alta" data-testid="prioridad-alta" />;
}

export function CorteManualOrden({ corte }: { corte: CorteManual }) {
    const estado = ESTADOS_CORTE[corte.estado];
    const reconexion = corte.accion === "reconexion";
    return (
        <Tarjeta data-testid="corte-manual">
            <TarjetaEncabezado className="flex flex-row flex-wrap items-center justify-between gap-2">
                <TarjetaTitulo className="inline-flex items-center gap-2">
                    <Tv aria-hidden className="size-4 shrink-0" />
                    {reconexion ? "Reconexión de cable pedida por Argos" : "Corte de cable pedido por Argos"}
                </TarjetaTitulo>
                <InsigniaEstado tono={estado.tono} icono={estado.icono} texto={estado.texto} data-testid="corte-estado" />
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-2 text-sm">
                <p className="break-words" data-testid="corte-causa">
                    <span className="text-muted-foreground">Por qué: </span>
                    {corte.causaTexto}
                </p>
                <p className="break-words text-muted-foreground">
                    Solo se toca el {corte.componente === "cable" ? "cable (TV)" : corte.componente}; el internet del servicio sigue igual. Al cerrar, marca en el checklist si
                    se hizo.
                </p>
                {corte.motivoCodigo ? <p data-testid="corte-motivo">Motivo: {MOTIVOS_CORTE[corte.motivoCodigo]}</p> : null}
                {corte.cancelacionPedida ? (
                    <p className="font-medium text-warning" data-testid="corte-cancelacion">
                        Argos canceló la solicitud con el técnico en camino: si no se hizo, queda cancelada al cerrar.
                    </p>
                ) : null}
                <p className="text-xs text-muted-foreground tabular-nums">
                    Solicitud {corte.solicitudId} de Argos{corte.solicitadaEn ? ` · ${formatearFecha(corte.solicitadaEn)}` : ""}
                </p>
            </TarjetaContenido>
        </Tarjeta>
    );
}
