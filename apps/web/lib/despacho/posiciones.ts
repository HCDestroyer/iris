import type { Schema } from "@iris/contratos";
import type { Tono } from "@iris/ui";
import { Clock, Radio, WifiOff, type LucideIcon } from "lucide-react";
import { textoAntiguedad } from "./fechas";

export type PosicionTecnico = Schema<"PosicionTecnico">;
export type UltimasPosiciones = Schema<"UltimasPosiciones">;

/** Cada cuánto se vuelve a pedir `GET /campo/posiciones/ultimas` (la app manda cada 60 s). */
export const INTERVALO_POSICIONES_MS = 30_000;

/**
 * Antigüedad de la posición ahora mismo: la que dio la API (`antiguedadSeg`, medida contra su
 * reloj) más lo que pasó en este navegador desde que se leyó. No depende del reloj del equipo.
 */
export function antiguedadAhora(p: Pick<PosicionTecnico, "antiguedadSeg">, leidaEnMs: number, ahoraMs: number): number {
    return p.antiguedadSeg + Math.max(0, (ahoraMs - leidaEnMs) / 1000);
}

export type Frescura = "en_vivo" | "retrasada" | "sin_senal";

/** Hasta 3 min: en vivo (la app manda cada 60 s); hasta 15 min: retrasada; más: sin señal reciente. */
export function frescura(segundos: number): Frescura {
    if (segundos <= 180) return "en_vivo";
    if (segundos <= 900) return "retrasada";
    return "sin_senal";
}

export const FRESCURAS: Record<Frescura, { tono: Tono; icono: LucideIcon; texto: string }> = {
    en_vivo: { tono: "success", icono: Radio, texto: "En vivo" },
    retrasada: { tono: "warning", icono: Clock, texto: "Retrasada" },
    sin_senal: { tono: "neutral", icono: WifiOff, texto: "Sin señal reciente" },
};

/** «En vivo · hace 40 s», «Retrasada · hace 7 min». */
export function textoPosicion(segundos: number): string {
    return `${FRESCURAS[frescura(segundos)].texto} · ${textoAntiguedad(segundos)}`;
}

/** Iniciales para el marcador del técnico en el mapa. */
export function iniciales(nombre: string): string {
    const partes = nombre.trim().split(/\s+/).filter(Boolean);
    const letras = partes.length >= 2 ? [partes[0], partes[partes.length - 1]] : partes;
    return letras.map((p) => p?.[0]?.toUpperCase() ?? "").join("") || "?";
}
