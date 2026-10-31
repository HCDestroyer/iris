"use client";

import type { Conexion } from "@iris/sync";
import { CONEXION_TEXTO, etiquetaPunto, partesEnvio, resumenEnvio } from "@iris/sync/campo";
import { Boton, cn, type Tono } from "@iris/ui";
import { CloudUpload, KeyRound, Building2, RefreshCw, TriangleAlert, Wifi, WifiOff, type LucideIcon } from "lucide-react";
import { horaCorta } from "@/lib/campo/fechas";
import { useCampo } from "./contexto";

const ICONOS_CONEXION: Record<Conexion, LucideIcon> = {
    en_linea: Wifi,
    desconocida: Wifi,
    sin_conexion: WifiOff,
    sin_sesion: KeyRound,
    sin_empresa: Building2,
};

const CONEXION = (c: Conexion): { tono: Tono; icono: LucideIcon; texto: string } => ({ ...CONEXION_TEXTO[c], icono: ICONOS_CONEXION[c] });

const TEXTO_TONO: Record<Tono, string> = {
    neutral: "text-muted-foreground",
    info: "text-info",
    success: "text-success",
    warning: "text-warning",
    error: "text-error",
};

const FONDO_TONO: Record<Tono, string> = {
    neutral: "bg-muted-foreground",
    info: "bg-info",
    success: "bg-success",
    warning: "bg-warning",
    error: "bg-error",
};

/**
 * Punto de estado junto al título: con conexión y todo enviado es solo esto («En línea, todo
 * enviado»). Cuando la barra está abierta, el punto acompaña y la barra explica.
 */
export function PuntoConexion() {
    const { estado } = useCampo();
    const r = resumenEnvio(estado);
    const c = CONEXION(estado.conexion);
    const etiqueta = etiquetaPunto(estado.conexion, r);
    return (
        <span role="status" aria-label={etiqueta} title={etiqueta} data-slot="punto-conexion" data-conexion={estado.conexion} className="inline-flex items-center">
            <span aria-hidden className={cn("size-2.5 rounded-full", FONDO_TONO[c.tono], estado.conexion === "desconocida" && "animate-pulse")} />
            {r.abierta ? null : (
                <span data-testid="estado-conexion" aria-hidden className="sr-only">
                    {r.texto}
                </span>
            )}
        </span>
    );
}

/**
 * Barra que crece solo cuando hace falta: sin señal con cosas por enviar, enviando, sesión vencida,
 * empresa no válida o un rechazo que pide decidir. Con todo enviado no se muestra (queda el punto).
 */
export function BarraConexion() {
    const { estado, motor, navegar } = useCampo();
    const r = resumenEnvio(estado);
    if (!r.abierta) {
        return null;
    }
    const c = CONEXION(estado.conexion);
    const Icono = c.icono;
    const partes = partesEnvio(r).pendientes;
    const hayAlgo = partes.length > 0 || r.rechazos > 0;

    return (
        <section
            aria-label="Estado de la sincronización"
            data-conexion={estado.conexion}
            data-slot="barra-conexion"
            className="border-b border-border bg-background/95 px-4 py-2 backdrop-blur"
        >
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                <p className={cn("flex min-w-0 items-center gap-1.5 text-sm font-semibold", TEXTO_TONO[c.tono])}>
                    <Icono aria-hidden className="size-4 shrink-0" />
                    <span data-testid="estado-conexion">{r.texto}</span>
                </p>
                {hayAlgo ? (
                    <Boton
                        variante={r.rechazos > 0 ? "secundario" : "fantasma"}
                        className="ml-auto h-auto min-h-11 py-1 whitespace-normal text-left"
                        onClick={() => navegar({ tipo: "pendientes" })}
                        aria-label={`${[...partes, r.rechazos > 0 ? `${r.rechazos} rechazados` : null].filter(Boolean).join(", ")}. Ver detalle`}
                    >
                        {r.rechazos > 0 ? <TriangleAlert aria-hidden className="text-error" /> : <CloudUpload aria-hidden className="text-warning" />}
                        <span data-testid="pendientes-por-enviar">
                            {partes.length > 0 ? partes.join(" · ") : null}
                            {r.rechazos > 0 ? `${partes.length > 0 ? " · " : ""}${r.rechazos} ${r.rechazos === 1 ? "rechazado" : "rechazados"}` : null}
                        </span>
                    </Boton>
                ) : null}
            </div>
            {estado.proximoIntento !== null && estado.conexion !== "sin_sesion" && hayAlgo ? (
                <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                    <span>Reintento a las {horaCorta(new Date(estado.proximoIntento).toISOString())}</span>
                    <Boton variante="fantasma" tamano="sm" className="min-h-11 text-xs" onClick={() => motor.reintentarAhora()}>
                        <RefreshCw aria-hidden />
                        Reintentar ahora
                    </Boton>
                </div>
            ) : null}
        </section>
    );
}
