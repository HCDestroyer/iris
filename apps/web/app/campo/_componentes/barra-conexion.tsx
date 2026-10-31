"use client";

import type { Conexion, EstadoSync } from "@iris/sync";
import { Boton, cn, type Tono } from "@iris/ui";
import { CloudUpload, KeyRound, Building2, RefreshCw, TriangleAlert, Wifi, WifiOff, type LucideIcon } from "lucide-react";
import { horaCorta } from "@/lib/campo/fechas";
import { useCampo } from "./contexto";

const CONEXION: Record<Conexion, { tono: Tono; icono: LucideIcon; texto: string }> = {
    en_linea: { tono: "success", icono: Wifi, texto: "En línea" },
    desconocida: { tono: "neutral", icono: Wifi, texto: "Conectando…" },
    sin_conexion: { tono: "warning", icono: WifiOff, texto: "Sin conexión" },
    sin_sesion: { tono: "error", icono: KeyRound, texto: "Sesión vencida" },
    sin_empresa: { tono: "error", icono: Building2, texto: "Empresa no válida" },
};

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

export interface ResumenEnvio {
    /** Operaciones por enviar (incluye las que esperan archivos). */
    porEnviar: number;
    /** Archivos por subir que no fueron rechazados. */
    archivos: number;
    /** Operaciones y archivos rechazados: piden decidir. */
    rechazos: number;
    /** La barra se abre: hay algo por enviar, un rechazo, la sesión venció o la empresa no vale. */
    abierta: boolean;
    /** Texto del estado (también en el punto, para lectores de pantalla y pruebas). */
    texto: string;
}

/** Qué mostrar: un punto si todo va bien; la barra con texto y acción solo cuando hace falta. */
export function resumenEnvio(estado: EstadoSync): ResumenEnvio {
    const rechazos = estado.rechazos.length + estado.subidas.rechazadas;
    const porEnviar = estado.pendientes + estado.esperandoAdjuntos.length;
    const archivos = estado.subidas.pendientes - estado.subidas.rechazadas;
    const bloqueo = estado.conexion === "sin_sesion" || estado.conexion === "sin_empresa";
    const abierta = bloqueo || rechazos > 0 || porEnviar > 0 || archivos > 0;
    const enviando = estado.sincronizando && estado.conexion !== "sin_conexion" && !bloqueo && (porEnviar > 0 || archivos > 0);
    return { porEnviar, archivos, rechazos, abierta, texto: enviando ? "Sincronizando…" : CONEXION[estado.conexion].texto };
}

/**
 * Punto de estado junto al título: con conexión y todo enviado es solo esto («En línea, todo
 * enviado»). Cuando la barra está abierta, el punto acompaña y la barra explica.
 */
export function PuntoConexion() {
    const { estado } = useCampo();
    const r = resumenEnvio(estado);
    const c = CONEXION[estado.conexion];
    const etiqueta = r.abierta ? r.texto : estado.conexion === "en_linea" ? "En línea, todo enviado" : `${c.texto}, todo enviado`;
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
    const c = CONEXION[estado.conexion];
    const Icono = c.icono;
    const partes = [
        r.porEnviar > 0 ? `${r.porEnviar} ${r.porEnviar === 1 ? "pendiente" : "pendientes"} por enviar` : null,
        r.archivos > 0 ? `${r.archivos} ${r.archivos === 1 ? "archivo" : "archivos"} por subir` : null,
    ].filter(Boolean);
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
