import type * as MapLibreGL from "maplibre-gl";
import type { Map as MapaLibre } from "maplibre-gl";

/*
 * Lo común de los mapas del panel (despacho y comercial): MapLibre GL con teselas raster de
 * OpenStreetMap, sin estilo remoto ni fuentes (los marcadores son HTML de React).
 */

export const TESELAS_OSM = "https://tile.openstreetmap.org/{z}/{x}/{y}.png";
export const URL_WORKER = new URL("maplibre-gl/dist/maplibre-gl-worker.mjs", import.meta.url).href;
export const CENTRO_GUATEMALA: [number, number] = [-90.5133, 14.6349];

/** Color de un token de la identidad Horus (MapLibre no lee variables CSS). */
export function token(nombre: string, porDefecto: string): string {
    if (typeof document === "undefined") return porDefecto;
    return getComputedStyle(document.documentElement).getPropertyValue(nombre).trim() || porDefecto;
}

/** Pintura de las teselas: atenuadas en modo oscuro. */
export function pinturaTeselas(oscuro: boolean): Record<string, number> {
    return oscuro ? { "raster-brightness-max": 0.62, "raster-saturation": -0.55, "raster-contrast": 0.1 } : {};
}

/** Aplica la pintura del tema a la capa `osm` sin recrear el mapa. */
export function aplicarTemaTeselas(m: MapaLibre, oscuro: boolean): void {
    const paint = pinturaTeselas(oscuro);
    for (const p of ["raster-brightness-max", "raster-saturation", "raster-contrast"] as const) {
        m.setPaintProperty("osm", p, paint[p] ?? (p === "raster-brightness-max" ? 1 : 0));
    }
}

export function encuadrar(m: MapaLibre, ml: typeof MapLibreGL, puntos: [number, number][]) {
    if (puntos.length === 0) return;
    if (puntos.length === 1 && puntos[0]) {
        m.jumpTo({ center: puntos[0], zoom: 14 });
        return;
    }
    const caja = new ml.LngLatBounds(puntos[0], puntos[0]);
    for (const p of puntos) caja.extend(p);
    m.fitBounds(caja, { padding: 48, maxZoom: 15, duration: 0 });
}

export const FONDO_TONO = {
    neutral: "bg-muted-foreground",
    info: "bg-info",
    success: "bg-success",
    warning: "bg-warning",
    error: "bg-error",
} as const;

export const TEXTO_TONO = {
    neutral: "text-muted-foreground",
    info: "text-info",
    success: "text-success",
    warning: "text-warning",
    error: "text-error",
} as const;
