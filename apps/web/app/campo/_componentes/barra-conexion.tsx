"use client";

import type { Conexion } from "@iris/sync";
import { Boton, cn, type Tono } from "@iris/ui";
import { CloudCheck, CloudUpload, KeyRound, Building2, RefreshCw, TriangleAlert, Wifi, WifiOff, type LucideIcon } from "lucide-react";
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

/**
 * Indicador siempre visible: conexión (icono + texto + color) y «N pendientes por enviar» con
 * acceso a lo pendiente y a los rechazos.
 */
export function BarraConexion() {
    const { estado, motor, navegar } = useCampo();
    const c = CONEXION[estado.conexion];
    const Icono = c.icono;
    const rechazos = estado.rechazos.length + estado.subidas.rechazadas;
    const porEnviar = estado.pendientes + estado.esperandoAdjuntos.length;
    const fotos = estado.subidas.pendientes - estado.subidas.rechazadas;
    const nada = porEnviar === 0 && fotos === 0 && rechazos === 0;
    const partes = [
        porEnviar > 0 ? `${porEnviar} ${porEnviar === 1 ? "pendiente" : "pendientes"} por enviar` : null,
        fotos > 0 ? `${fotos} ${fotos === 1 ? "archivo" : "archivos"} por subir` : null,
    ].filter(Boolean);

    return (
        <section
            aria-label="Estado de la sincronización"
            data-conexion={estado.conexion}
            className="sticky top-[3.5rem] z-20 border-b border-border bg-background/95 px-4 py-2 backdrop-blur"
        >
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                <p role="status" className={cn("flex min-w-0 items-center gap-1.5 text-sm font-semibold", TEXTO_TONO[c.tono])}>
                    <Icono aria-hidden className="size-4 shrink-0" />
                    <span data-testid="estado-conexion">{estado.sincronizando && estado.conexion !== "sin_conexion" ? "Sincronizando…" : c.texto}</span>
                </p>
                <Boton
                    variante={rechazos > 0 ? "secundario" : "fantasma"}
                    tamano="sm"
                    className="ml-auto h-auto min-h-8 py-1 whitespace-normal text-left"
                    onClick={() => navegar({ tipo: "pendientes" })}
                    aria-label={nada ? "Todo enviado. Ver detalle" : `${[...partes, rechazos > 0 ? `${rechazos} rechazados` : null].filter(Boolean).join(", ")}. Ver detalle`}
                >
                    {nada ? (
                        <>
                            <CloudCheck aria-hidden className="text-success" />
                            <span>Todo enviado</span>
                        </>
                    ) : (
                        <>
                            {rechazos > 0 ? <TriangleAlert aria-hidden className="text-error" /> : <CloudUpload aria-hidden className="text-warning" />}
                            <span data-testid="pendientes-por-enviar">
                                {partes.length > 0 ? partes.join(" · ") : null}
                                {rechazos > 0 ? `${partes.length > 0 ? " · " : ""}${rechazos} ${rechazos === 1 ? "rechazado" : "rechazados"}` : null}
                            </span>
                        </>
                    )}
                </Boton>
            </div>
            {estado.proximoIntento !== null && estado.conexion !== "sin_sesion" && !nada ? (
                <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
                    <span>Reintento a las {horaCorta(new Date(estado.proximoIntento).toISOString())}</span>
                    <Boton variante="enlace" tamano="sm" className="text-xs" onClick={() => motor.reintentarAhora()}>
                        <RefreshCw aria-hidden />
                        Reintentar ahora
                    </Boton>
                </div>
            ) : null}
        </section>
    );
}
