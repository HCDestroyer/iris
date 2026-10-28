import type { Schema } from "@iris/contratos";
import type { Tono } from "@iris/ui";
import { ArrowDownRight, ArrowUpRight, CircleCheck, type LucideIcon } from "lucide-react";
import type { CitaAgenda } from "./calendario";

/*
 * Ruta del día de un técnico (API de rutas de I2): la propuesta (`RutaDetalle.paradas`, con la
 * llegada estimada) frente a la real (`llegadaReal`, `desvioSeg` y el recorrido GPS en `real`).
 * Nada se inventa: sin hora estimada no hay desvío, una parada oculta solo muestra su posición y
 * sin `geometria` no hay trazo.
 */

export type Ruta = Schema<"Ruta">;
export type RutaDetalle = Schema<"RutaDetalle">;
export type ParadaRuta = Schema<"ParadaRuta">;

/** Borrador y publicada del día (a lo sumo una de cada una; las reemplazadas no se piden). */
export function rutasDelDia(rutas: readonly Ruta[]): { borrador: Ruta | null; publicada: Ruta | null } {
    return {
        borrador: rutas.find((r) => r.estado === "borrador") ?? null,
        publicada: rutas.find((r) => r.estado === "publicada") ?? null,
    };
}

/** Desvío de una parada visitada: hasta 5 min de diferencia es «a tiempo». */
export const TOLERANCIA_DESVIO_SEG = 300;

export function presentacionDesvio(desvioSeg: number | null): { tono: Tono; icono: LucideIcon; texto: string } | null {
    if (desvioSeg === null) return null;
    const min = Math.round(Math.abs(desvioSeg) / 60);
    if (Math.abs(desvioSeg) <= TOLERANCIA_DESVIO_SEG) return { tono: "success", icono: CircleCheck, texto: "A tiempo" };
    if (desvioSeg > 0) return { tono: "warning", icono: ArrowDownRight, texto: `${min} min tarde` };
    return { tono: "info", icono: ArrowUpRight, texto: `${min} min antes` };
}

/** ¿Se puede cambiar de lugar? Solo en un borrador, sin paradas ocultas, y nunca las fijas (ya en curso o hechas). */
export function sePuedeReordenar(ruta: Pick<RutaDetalle, "estado" | "paradas">): boolean {
    const paradas = ruta.paradas ?? [];
    return ruta.estado === "borrador" && !paradas.some((p) => p.oculta) && paradas.filter((p) => !p.fija).length > 1;
}

/**
 * `ordenIds` para `POST /rutas/proponer` tras mover la parada `indice` un lugar (`delta` −1 o +1).
 * Las fijas no se mueven ni se saltan; `null` si el movimiento no es posible.
 */
export function ordenTrasMover(paradas: readonly ParadaRuta[], indice: number, delta: -1 | 1): number[] | null {
    const destino = indice + delta;
    const a = paradas[indice];
    const b = paradas[destino];
    if (!a || !b || a.fija || b.fija || a.oculta || b.oculta) return null;
    const copia = [...paradas];
    copia[indice] = b;
    copia[destino] = a;
    return copia.map((p) => p.ordenId);
}

/** Coordenadas de un GeoJSON LineString ([lon, lat]); `null` si no viene o no tiene esa forma. */
export function coordenadasDeGeometria(geometria: unknown): [number, number][] | null {
    if (typeof geometria !== "object" || geometria === null) return null;
    const g = geometria as { type?: unknown; coordinates?: unknown };
    if (g.type !== "LineString" || !Array.isArray(g.coordinates)) return null;
    const puntos: [number, number][] = [];
    for (const c of g.coordinates) {
        if (!Array.isArray(c) || typeof c[0] !== "number" || typeof c[1] !== "number" || !Number.isFinite(c[0]) || !Number.isFinite(c[1])) return null;
        puntos.push([c[0], c[1]]);
    }
    return puntos.length >= 2 ? puntos : null;
}

/** Recorrido GPS real como línea ([lon, lat]); `null` si no está disponible (nunca «no se movió»). */
export function coordenadasReales(real: RutaDetalle["real"]): [number, number][] | null {
    if (!real.disponible) return null;
    const puntos = (real.puntos ?? []).map((p): [number, number] => [p.lon, p.lat]);
    return puntos.length >= 2 ? puntos : null;
}

export const MOTIVOS_SIN_RECORRIDO: Record<NonNullable<RutaDetalle["real"]["motivo"]>, string> = {
    sin_permiso: "No tienes permiso para ver ubicaciones (campo.ver_posiciones): el recorrido real no se muestra.",
    sin_posiciones: "La app de campo no envió ubicación ese día: no hay recorrido real que mostrar.",
};

export const MOTIVOS_MOTOR: Record<NonNullable<Ruta["motivoMotor"]>, string> = {
    sin_osrm: "no hay servicio de rutas configurado",
    osrm_no_disponible: "el servicio de rutas no respondió",
    datos_cambiaron: "las órdenes cambiaron mientras se calculaba",
};

/** Llegadas reales del técnico en el día desde la agenda (`enSitioEn`), en orden. */
export function llegadasDeCitas(citas: readonly CitaAgenda[], tecnicoId: number): (CitaAgenda & { enSitioEn: string })[] {
    return citas
        .filter((c): c is CitaAgenda & { enSitioEn: string } => c.tecnicoId === tecnicoId && typeof c.enSitioEn === "string")
        .sort((a, b) => a.enSitioEn.localeCompare(b.enSitioEn));
}
