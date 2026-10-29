"use client";

import { Aviso, Boton, Entrada, Esqueleto, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { Check, Copy, Mail, RotateCw } from "lucide-react";
import { useState } from "react";
import { CalificacionEncuesta, InsigniaAlerta, InsigniaEstadoEncuesta } from "@/componentes/encuestas/calificacion";
import { ErrorLectura } from "@/componentes/error-lectura";
import { mensajeDeError } from "@/lib/api/errores";
import { encuestaDe, enlaceDe } from "@/lib/encuestas/api-panel";
import type { Encuesta } from "@/lib/encuestas/presentacion";
import { useLectura } from "@/lib/lectura";
import { textoFechaHora } from "@/lib/reportes/presentacion";

/**
 * Tarjeta «Encuesta» del detalle del ticket o de la orden (`encuestas.ver`): estado, calificación,
 * comentario y, mientras está pendiente, «Copiar enlace» para compartirla por otro medio.
 */
export function TarjetaEncuesta({ filtro }: { filtro: { ticketId: number } | { ordenId: number } }) {
    const lectura = useLectura((signal) => encuestaDe(filtro, signal));
    return (
        <Tarjeta data-testid="tarjeta-encuesta">
            <TarjetaEncabezado>
                <TarjetaTitulo>Encuesta</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                {lectura.estado === "cargando" ? (
                    <Esqueleto className="h-16" />
                ) : lectura.estado === "error" ? (
                    <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="la encuesta" />
                ) : lectura.datos === null ? (
                    <p className="text-sm text-muted-foreground">Sin encuesta. Se envía al resolver el ticket o al cerrar con éxito una instalación o visita.</p>
                ) : (
                    <DetalleEncuesta encuesta={lectura.datos} />
                )}
            </TarjetaContenido>
        </Tarjeta>
    );
}

function DetalleEncuesta({ encuesta: e }: { encuesta: Encuesta }) {
    return (
        <>
            <div className="flex flex-wrap items-center gap-2">
                <InsigniaEstadoEncuesta estado={e.estado} />
                <CalificacionEncuesta calificacion={e.calificacion} />
                {e.alerta ? <InsigniaAlerta /> : null}
            </div>
            {e.comentario ? <p className="text-sm break-words whitespace-pre-line" data-testid="comentario-encuesta">«{e.comentario}»</p> : null}
            <p className="text-xs text-muted-foreground tabular-nums">
                {e.canal === "correo" ? "Enviada por correo" : "Por enlace"} el {textoFechaHora(e.creadaEn)}
                {e.respondidaEn ? ` · respondida ${textoFechaHora(e.respondidaEn)}` : e.estado === "pendiente" ? ` · vence ${textoFechaHora(e.expiraEn)}` : ""}
            </p>
            {e.estado === "pendiente" ? <CompartirEnlace encuesta={e} /> : null}
        </>
    );
}

function CompartirEnlace({ encuesta }: { encuesta: Encuesta }) {
    const [estado, setEstado] = useState<"listo" | "pidiendo" | "copiado">("listo");
    const [error, setError] = useState<unknown>(null);
    const [aMano, setAMano] = useState<string | null>(null);

    if (!encuesta.enlaceDisponible) {
        return <p className="text-xs text-muted-foreground">El enlace no se puede volver a mostrar.</p>;
    }
    if (encuesta.canal === "correo" && !encuesta.enlace) {
        return (
            <p className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
                <Mail aria-hidden className="size-4" />
                Enviada por correo al cliente.
            </p>
        );
    }

    async function copiar() {
        setEstado("pidiendo");
        setError(null);
        setAMano(null);
        let enlace: string;
        try {
            enlace = await enlaceDe(encuesta);
        } catch (e) {
            setError(e);
            setEstado("listo");
            return;
        }
        try {
            if (!navigator.clipboard) throw new Error("sin portapapeles");
            await navigator.clipboard.writeText(enlace);
            setEstado("copiado");
            window.setTimeout(() => setEstado("listo"), 2500);
        } catch {
            // El navegador no dejó copiar: se muestra para copiarlo a mano (nunca se da por copiado).
            setAMano(enlace);
            setEstado("listo");
        }
    }

    return (
        <div className="grid gap-2">
            <Boton variante="secundario" tamano="sm" className="justify-self-start" onClick={() => void copiar()} disabled={estado === "pidiendo"}>
                {estado === "copiado" ? <Check /> : <Copy />}
                {estado === "copiado" ? "Enlace copiado" : estado === "pidiendo" ? "Preparando…" : "Copiar enlace"}
            </Boton>
            <span role="status" className="text-xs text-muted-foreground empty:hidden">
                {estado === "copiado" ? "Compártelo solo con el cliente: es personal y de un solo uso." : ""}
            </span>
            {aMano ? (
                <div className="grid gap-1">
                    <p className="text-xs text-muted-foreground">No se pudo copiar: selecciónalo y cópialo a mano.</p>
                    <Entrada readOnly value={aMano} aria-label="Enlace de la encuesta" className="font-mono text-xs" onFocus={(ev) => ev.currentTarget.select()} />
                </div>
            ) : null}
            {error ? (
                <Aviso
                    tono="error"
                    titulo="No se pudo obtener el enlace"
                    accion={
                        <Boton variante="secundario" tamano="sm" onClick={() => void copiar()}>
                            <RotateCw />
                            Reintentar
                        </Boton>
                    }
                >
                    {mensajeDeError(error).detalle ?? (error instanceof Error ? error.message : undefined)}
                </Aviso>
            ) : null}
        </div>
    );
}
