import type { Schema } from "@iris/contratos";
import { formatearFecha } from "../fechas";
import { ESTADOS_ORDEN, type EstadoOrden } from "./presentacion";

export type EventoOrden = Schema<"EventoOrden">;

const estadoTexto = (v: string | null) => (v && v in ESTADOS_ORDEN ? ESTADOS_ORDEN[v as EstadoOrden].texto.toLowerCase() : (v ?? "—"));

const numero = (v: unknown): number | undefined => (typeof v === "number" && Number.isFinite(v) ? v : undefined);

/** Frase del historial de una orden. */
export function describirEventoOrden(e: EventoOrden): string {
    switch (e.tipo) {
        case "creada":
            return "Creó la orden";
        case "estado":
            return `Cambió el estado de ${estadoTexto(e.de)} a ${estadoTexto(e.a)}`;
        case "asignada":
            return numero(e.datos.tecnico_id) === undefined ? "Dejó la orden sin técnico" : "Cambió el técnico";
        case "programada": {
            const inicio = typeof e.datos.inicio === "string" ? formatearFecha(e.datos.inicio) : undefined;
            const verbo = e.datos.reprogramada === true ? "Reprogramó" : "Programó";
            return inicio ? `${verbo} para el ${inicio}` : `${verbo} la visita`;
        }
        case "formulario": {
            const nombre = typeof e.datos.nombre === "string" ? ` «${e.datos.nombre}»` : "";
            return e.datos.accion === "quitado" ? `Quitó el formulario${nombre}` : `Agregó el formulario${nombre}`;
        }
    }
}

/** Posición GPS registrada con un cambio de estado (salir, llegar, cerrar, fallar). */
export type PuntoGps = { eventoId: number; lat: number; lon: number; precision: number | undefined; tomadoEn: string; que: string };

const QUE_GPS: Partial<Record<EstadoOrden, string>> = {
    en_camino: "Salió hacia el sitio",
    en_sitio: "Llegó al sitio",
    cerrada: "Cerró la orden",
    fallida: "Marcó la orden como fallida",
};

/** Puntos GPS del historial, en el orden en que ocurrieron. */
export function puntosGps(eventos: readonly EventoOrden[]): PuntoGps[] {
    const puntos: PuntoGps[] = [];
    for (const e of eventos) {
        const lat = numero(e.datos.lat);
        const lon = numero(e.datos.lon);
        if (lat === undefined || lon === undefined || Math.abs(lat) > 90 || Math.abs(lon) > 180) continue;
        const tomado = typeof e.datos.tomado_en === "string" ? e.datos.tomado_en : e.ocurridoEn;
        const que = (e.a && QUE_GPS[e.a as EstadoOrden]) || describirEventoOrden(e);
        puntos.push({ eventoId: e.id, lat, lon, precision: numero(e.datos.precision), tomadoEn: tomado, que });
    }
    return puntos.sort((a, b) => Date.parse(a.tomadoEn) - Date.parse(b.tomadoEn));
}
