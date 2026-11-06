// Tareas propias del técnico y órdenes libres (botón «+», diseño aprobado por el PM en nov 2026).
// Formulario «Nueva tarea» (zod, espejo de `agenda.DatosTarea` en la API), textos de las tarjetas
// y de la pantalla «Órdenes libres». Sin DOM ni React: compartido por la PWA y la app nativa.
import { z } from "zod";
import type { DatosTarea, OrdenLibre, Tarea } from "../contrato";
import { horaCorta, hoyEnGuatemala, ZONA } from "./fechas";

/** Límites de la API (`agenda.TareaQueMax`, `TareaDuracionMin/Max`). */
export const TAREA_QUE_MAX = 200;
export const TAREA_DURACION_MIN = 5;
export const TAREA_DURACION_MAX = 480;

/** Chips de duración de «Nueva tarea». */
export const DURACIONES_TAREA = [
    { minutos: 15, texto: "15 min" },
    { minutos: 30, texto: "30 min" },
    { minutos: 60, texto: "1 h" },
    { minutos: 120, texto: "2 h" },
] as const;

/** «30 min», «1 h», «1 h 30 min». */
export function duracionTexto(minutos: number): string {
    const h = Math.floor(minutos / 60);
    const m = minutos % 60;
    if (h === 0) {
        return `${m} min`;
    }
    return m === 0 ? `${h} h` : `${h} h ${m} min`;
}

const sinNul = (s: string) => !s.includes("\u0000");

/** Valores del formulario «Nueva tarea»: día y hora de Guatemala; `ligada` = `orden:<id>` o vacío. */
export const esquemaTarea = z.object({
    que: z
        .string()
        .trim()
        .min(1, "Escribe qué vas a hacer")
        .max(TAREA_QUE_MAX, `Máximo ${TAREA_QUE_MAX} caracteres`)
        .refine(sinNul, "Texto no válido"),
    dia: z.string().regex(/^\d{4}-\d{2}-\d{2}$/, "Elige el día"),
    hora: z.string().regex(/^([01]\d|2[0-3]):[0-5]\d$/, "Elige la hora"),
    duracionMin: z
        .number({ message: "Elige la duración" })
        .int()
        .min(TAREA_DURACION_MIN, `Al menos ${TAREA_DURACION_MIN} minutos`)
        .max(TAREA_DURACION_MAX, `Hasta ${TAREA_DURACION_MAX / 60} horas`),
    ligada: z.string().regex(/^(orden:\d+)?$/, "Elige una orden"),
});
export type ValoresTarea = z.infer<typeof esquemaTarea>;

/** Guatemala no tiene horario de verano: UTC−6 todo el año. */
const DESFASE_GT = "-06:00";

/** Instante UTC (ISO) de un día y una hora de Guatemala. */
export function instanteDeGuatemala(dia: string, hora: string): string {
    return new Date(`${dia}T${hora}:00${DESFASE_GT}`).toISOString();
}

const horaGt = new Intl.DateTimeFormat("es-GT", {
    timeZone: ZONA,
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
});

/** Valores iniciales: hoy, la siguiente media hora (hora de Guatemala) y 30 minutos. */
export function valoresInicialesTarea(ms = Date.now()): ValoresTarea {
    const siguiente = Math.ceil((ms + 60_000) / (30 * 60_000)) * 30 * 60_000;
    return {
        que: "",
        dia: hoyEnGuatemala(siguiente),
        hora: horaGt.format(siguiente),
        duracionMin: 30,
        ligada: "",
    };
}

/** Los `datos` de `tarea.crear` a partir del formulario ya validado. */
export function datosTarea(v: ValoresTarea): DatosTarea {
    const datos: DatosTarea = {
        que: v.que.trim(),
        inicio: instanteDeGuatemala(v.dia, v.hora),
        duracionMin: v.duracionMin,
    };
    const orden = /^orden:(\d+)$/.exec(v.ligada);
    if (orden) {
        datos.ordenId = Number(orden[1]);
    }
    return datos;
}

function sumarUnDia(fecha: string): string {
    const d = new Date(`${fecha}T12:00:00Z`);
    d.setUTCDate(d.getUTCDate() + 1);
    return d.toISOString().slice(0, 10);
}

const diaCorto = new Intl.DateTimeFormat("es-GT", {
    timeZone: "UTC",
    weekday: "short",
    day: "numeric",
    month: "short",
});

/** «Hoy · 14:00», «Mañana · 09:00» o «Vie 6 nov · 08:00». */
export function textoCuando(
    dia: string,
    hora: string,
    hoy = hoyEnGuatemala(),
): string {
    if (dia === hoy) {
        return `Hoy · ${hora}`;
    }
    if (dia === sumarUnDia(hoy)) {
        return `Mañana · ${hora}`;
    }
    const d = diaCorto.format(new Date(`${dia}T12:00:00Z`)).replace(/\./g, "");
    return `${d.charAt(0).toUpperCase()}${d.slice(1)} · ${hora}`;
}

/** Día de Guatemala (YYYY-MM-DD) en que empieza la tarea. */
export function diaDeTarea(t: Pick<Tarea, "inicio">): string {
    return hoyEnGuatemala(Date.parse(t.inicio));
}

/** Las tareas (no anuladas) de ese día, por hora. */
export function tareasDelDia<T extends Pick<Tarea, "inicio" | "estado">>(
    tareas: readonly T[],
    fecha: string,
): T[] {
    return tareas
        .filter((t) => t.estado !== "anulada" && diaDeTarea(t) === fecha)
        .sort((a, b) => a.inicio.localeCompare(b.inicio));
}

/** Detalle de la tarjeta: «Tarea propia · 30 min» (o «Hecha · 30 min»). */
export function detalleTarea(t: Pick<Tarea, "estado" | "duracionMin">): string {
    return `${t.estado === "completada" ? "Hecha" : "Tarea propia"} · ${duracionTexto(t.duracionMin)}`;
}

/** Hora de inicio de la tarea («14:00», Guatemala). */
export function horaTarea(t: Pick<Tarea, "inicio">): string {
    return horaCorta(t.inicio) ?? "";
}

/**
 * Entrada de la lista del día: una orden o una tarea propia. Las órdenes conservan EXACTAMENTE el
 * orden en que llegan (el de las paradas con ruta publicada; el de la cita sin ruta): nunca se
 * mueven entre sí. Cada tarea va antes de la primera orden cuya hora (`horaDe`: la llegada
 * estimada con ruta o el inicio de la cita) sea posterior a su inicio; si ninguna, al final. Las
 * tareas entre sí, por inicio.
 */
export type EntradaDia<O, T = Tarea> =
    | { tipo: "orden"; clave: string; orden: O }
    | { tipo: "tarea"; clave: string; tarea: T };

export function intercalarTareas<
    O extends { id: number },
    T extends Pick<Tarea, "id" | "inicio">,
>(
    ordenes: readonly O[],
    horaDe: (o: O) => string | null | undefined,
    tareas: readonly T[],
): EntradaDia<O, T>[] {
    const pendientes = [...tareas].sort(
        (a, b) =>
            Date.parse(a.inicio) - Date.parse(b.inicio) ||
            a.id.localeCompare(b.id),
    );
    const salida: EntradaDia<O, T>[] = [];
    const tarea = (t: T): EntradaDia<O, T> => ({
        tipo: "tarea",
        clave: `tarea-${t.id}`,
        tarea: t,
    });
    for (const orden of ordenes) {
        const hora = horaDe(orden);
        const ms = hora ? Date.parse(hora) : Number.NaN;
        if (!Number.isNaN(ms)) {
            while (
                pendientes.length > 0 &&
                Date.parse(pendientes[0]?.inicio ?? "") < ms
            ) {
                const t = pendientes.shift();
                if (t) {
                    salida.push(tarea(t));
                }
            }
        }
        salida.push({ tipo: "orden", clave: `orden-${orden.id}`, orden });
    }
    return [...salida, ...pendientes.map(tarea)];
}

// ---------------------------------------------------------------------------------------------
// Órdenes libres
// ---------------------------------------------------------------------------------------------

/** «1,2 km» o «350 m». */
export function distanciaTexto(
    metros: number | null | undefined,
): string | null {
    if (metros === null || metros === undefined) {
        return null;
    }
    if (metros < 1000) {
        return `${Math.round(metros / 10) * 10} m`;
    }
    // Con coma decimal, como el diseño (es-GT usa punto y Hermes no siempre trae Intl completo).
    return `${(Math.round(metros / 100) / 10).toFixed(1).replace(".", ",")} km`;
}

/** «Hoy 16:00», «Mañana 09:00», «Vie 6 nov 08:00» o «Sin fecha». */
export function cuandoLibre(
    o: Pick<OrdenLibre, "programadaInicio">,
    hoy = hoyEnGuatemala(),
): string {
    if (!o.programadaInicio) {
        return "Sin fecha";
    }
    const ms = Date.parse(o.programadaInicio);
    return textoCuando(hoyEnGuatemala(ms), horaGt.format(ms), hoy).replace(
        " · ",
        " ",
    );
}

/** «Zona Norte · sin técnico asignado · más cercanas primero». */
export function subtituloLibres(
    zonas: readonly { nombre: string }[],
    orden: "distancia" | "fecha",
): string {
    const donde =
        zonas.length === 0
            ? "Tu cuadrilla"
            : zonas.map((z) => z.nombre).join(", ");
    return `${donde} · sin técnico asignado · ${orden === "distancia" ? "más cercanas primero" : "por fecha"}`;
}

/** Aviso cuando otro técnico la tomó primero (409 `orden_ya_tomada` con `tomadaPor`). */
export function avisoYaTomada(
    numero: string,
    nombre: string | null | undefined,
): string {
    return `${numero} ya la tomó ${nombre?.trim() || "otro técnico"}. La quitamos de la lista.`;
}
