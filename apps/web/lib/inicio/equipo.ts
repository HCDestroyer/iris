import type { Tono } from "@iris/ui";
import { CalendarOff, CircleCheck, CirclePause, Clock, MapPin, Navigation, WifiOff, type LucideIcon } from "lucide-react";
import { ausenciasDelDia, citasDelDia, type AusenciaAgenda, type CitaAgenda, type Tecnico } from "../despacho/calendario";
import { diaDesdeIso, type Dia } from "../despacho/fechas";
import { frescura, type PosicionTecnico } from "../despacho/posiciones";

/*
 * «Equipo hoy» (Inicio) y la tarjeta del técnico en el Mapa: con lo que ya dan `GET /agenda`
 * (citas y ausencias del día), `GET /tecnicos` y `GET /campo/posiciones/ultimas`, el avance y
 * el estado de cada técnico. Nada se inventa: sin posiciones no hay «sin señal»; sin citas, «Sin
 * órdenes hoy».
 */

export type EstadoTecnicoHoy = "en_sitio" | "en_camino" | "en_pausa" | "sin_senal" | "ausente" | "termino" | "sin_iniciar" | "sin_ordenes";

export const ESTADOS_TECNICO: Record<EstadoTecnicoHoy, { tono: Tono; icono: LucideIcon; texto: string }> = {
    en_sitio: { tono: "success", icono: MapPin, texto: "En sitio" },
    en_camino: { tono: "info", icono: Navigation, texto: "En camino" },
    en_pausa: { tono: "warning", icono: CirclePause, texto: "En pausa" },
    sin_senal: { tono: "warning", icono: WifiOff, texto: "Sin señal" },
    ausente: { tono: "neutral", icono: CalendarOff, texto: "Ausente" },
    termino: { tono: "success", icono: CircleCheck, texto: "Terminó" },
    sin_iniciar: { tono: "neutral", icono: Clock, texto: "Sin iniciar" },
    sin_ordenes: { tono: "neutral", icono: Clock, texto: "Sin órdenes hoy" },
};

/** Estados de una cita que cuentan como trabajo terminado del día. */
const TERMINADAS: readonly CitaAgenda["estado"][] = ["cerrada", "fallida"];

export type TecnicoHoy = {
    usuarioId: number;
    nombre: string;
    estado: EstadoTecnicoHoy;
    /** Citas del día sin las canceladas. */
    total: number;
    terminadas: number;
    /** La cita en curso (en sitio, en camino o en pausa). */
    actual: CitaAgenda | undefined;
    /** La próxima cita sin empezar (programada), por hora. */
    siguiente: CitaAgenda | undefined;
    /** Segundos desde la última posición (si se pudo leer y hay una). */
    antiguedadSeg: number | undefined;
};

/**
 * Estado de cada técnico hoy. `posiciones` en `null` = no se leyeron (sin permiso): entonces no se
 * dice «sin señal» de nadie. El orden: los que necesitan atención primero (sin señal, en pausa),
 * luego los que trabajan, y al final los ausentes y los que no tienen órdenes.
 */
export function equipoDeHoy(
    tecnicos: readonly Tecnico[],
    citas: readonly CitaAgenda[],
    ausencias: readonly AusenciaAgenda[],
    posiciones: readonly (PosicionTecnico & { antiguedad?: number })[] | null,
    dia: Dia,
    ahoraMs: number,
): TecnicoHoy[] {
    const filas: TecnicoHoy[] = [];
    const usuarios = new Map<number, string>();
    for (const t of tecnicos) if (t.activo) usuarios.set(t.usuarioId, t.nombre);
    // Quien tiene citas hoy y no vino en la lista (otra cuadrilla, sin ficha) también cuenta.
    for (const c of citas) if (!usuarios.has(c.tecnicoId) && diaDesdeIso(c.inicio) === dia) usuarios.set(c.tecnicoId, `Técnico ${c.tecnicoId}`);
    for (const [usuarioId, nombre] of usuarios) {
        const delDia = citasDelDia(citas, usuarioId, dia).filter((c) => c.estado !== "cancelada");
        const terminadas = delDia.filter((c) => TERMINADAS.includes(c.estado)).length;
        const actual =
            delDia.find((c) => c.estado === "en_sitio") ?? delDia.find((c) => c.estado === "en_camino") ?? delDia.find((c) => c.estado === "pausada");
        const siguiente = delDia.find((c) => c.estado === "programada" && c !== actual);
        const ausente = ausenciasDelDia(ausencias, usuarioId, dia).some((a) => Date.parse(a.inicio) <= ahoraMs && ahoraMs < Date.parse(a.fin));
        const pos = posiciones?.find((p) => p.tecnicoId === usuarioId);
        const antiguedad = pos ? (pos.antiguedad ?? pos.antiguedadSeg) : undefined;
        // Sin señal: tiene trabajo pendiente hoy y su última posición es vieja (o nunca mandó una).
        const sinSenal = posiciones !== null && delDia.length > terminadas && (antiguedad === undefined || frescura(antiguedad) === "sin_senal");
        let estado: EstadoTecnicoHoy;
        if (ausente) estado = "ausente";
        else if (actual?.estado === "en_sitio") estado = "en_sitio";
        else if (actual?.estado === "en_camino") estado = sinSenal ? "sin_senal" : "en_camino";
        else if (actual?.estado === "pausada") estado = "en_pausa";
        else if (delDia.length === 0) estado = "sin_ordenes";
        else if (terminadas === delDia.length) estado = "termino";
        else if (sinSenal) estado = "sin_senal";
        else estado = "sin_iniciar";
        filas.push({ usuarioId, nombre, estado, total: delDia.length, terminadas, actual, siguiente, antiguedadSeg: antiguedad });
    }
    const peso: Record<EstadoTecnicoHoy, number> = {
        sin_senal: 0,
        en_pausa: 1,
        en_sitio: 2,
        en_camino: 3,
        sin_iniciar: 4,
        termino: 5,
        ausente: 6,
        sin_ordenes: 7,
    };
    return filas.sort((a, b) => peso[a.estado] - peso[b.estado] || a.nombre.localeCompare(b.nombre, "es"));
}

/** Citas por día (sin canceladas) de `dias`: la tendencia de «Órdenes de hoy». */
export function citasPorDia(citas: readonly CitaAgenda[], dias: readonly Dia[]): number[] {
    return dias.map((d) => citas.filter((c) => c.estado !== "cancelada" && diaDesdeIso(c.inicio) === d).length);
}

/**
 * Puntos de una línea de tendencia (`<polyline points>`) en una caja de `ancho` × `alto`, con 2 px
 * de margen arriba y abajo. Una serie plana queda a media altura.
 */
export function puntosTendencia(valores: readonly number[], ancho = 96, alto = 32): string {
    if (valores.length === 0) return "";
    const min = Math.min(...valores);
    const max = Math.max(...valores);
    const paso = valores.length > 1 ? ancho / (valores.length - 1) : 0;
    const util = alto - 4;
    return valores
        .map((v, i) => {
            const y = max === min ? alto / 2 : 2 + util - ((v - min) / (max - min)) * util;
            return `${Math.round(i * paso * 10) / 10},${Math.round(y * 10) / 10}`;
        })
        .join(" ");
}
