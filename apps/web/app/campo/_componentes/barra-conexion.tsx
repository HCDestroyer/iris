"use client";

import type { Conexion } from "@iris/sync";
import { CONEXION_TEXTO, etiquetaPunto, resumenEnvio } from "@iris/sync/campo";
import { cn, type Tono } from "@iris/ui";
import { KeyRound, Building2, Wifi, WifiOff, type LucideIcon } from "lucide-react";
import { useCampo } from "./contexto";

const ICONOS_CONEXION: Record<Conexion, LucideIcon> = {
    en_linea: Wifi,
    desconocida: Wifi,
    sin_conexion: WifiOff,
    sin_sesion: KeyRound,
    sin_empresa: Building2,
};

const CONEXION = (c: Conexion): { tono: Tono; icono: LucideIcon; texto: string } => ({ ...CONEXION_TEXTO[c], icono: ICONOS_CONEXION[c] });

const FONDO_TONO: Record<Tono, string> = {
    neutral: "bg-muted-foreground",
    info: "bg-info",
    success: "bg-success",
    warning: "bg-warning",
    error: "bg-error",
};

/**
 * Punto de estado junto a la fecha del encabezado: con conexión y todo enviado es solo esto («En
 * línea, todo enviado»). Cuando hace falta actuar, el punto acompaña y la píldora encima de la isla
 * explica (`AvisoIsla`).
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
