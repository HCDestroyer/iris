import type { operations } from "@iris/contratos";
import { esDia } from "../despacho/fechas";
import { CALIFICACIONES, type Calificacion, type EntidadEncuesta, type EstadoEncuesta } from "./presentacion";

/*
 * Filtros de `/encuestas` en la URL: días de creación (Guatemala), calificaciones, tipo y estado.
 * `consultaDe` arma la query de `GET /encuestas` (cursor aparte).
 */

export type FiltrosEncuestas = {
    desde: string;
    hasta: string;
    calificaciones: Calificacion[];
    entidad: EntidadEncuesta | "";
    estado: EstadoEncuesta | "";
};

export const ESTADOS_FILTRO: readonly EstadoEncuesta[] = ["pendiente", "respondida", "vencida", "anulada"];

export const FILTROS_ENCUESTAS_VACIOS: FiltrosEncuestas = { desde: "", hasta: "", calificaciones: [], entidad: "", estado: "" };

export function filtrosEncuestasDesdeUrl(p: URLSearchParams): FiltrosEncuestas {
    const desde = p.get("desde") ?? "";
    const hasta = p.get("hasta") ?? "";
    const calif = (p.get("calificacion") ?? "").split(",").map(Number);
    const entidad = p.get("entidad");
    const estado = p.get("estado") ?? "";
    return {
        desde: esDia(desde) ? desde : "",
        hasta: esDia(hasta) ? hasta : "",
        calificaciones: CALIFICACIONES.filter((c) => calif.includes(c)),
        entidad: entidad === "ticket" || entidad === "orden" ? entidad : "",
        estado: (ESTADOS_FILTRO as readonly string[]).includes(estado) ? (estado as EstadoEncuesta) : "",
    };
}

export function urlEncuestas(f: FiltrosEncuestas): string {
    const p = new URLSearchParams();
    if (f.desde) p.set("desde", f.desde);
    if (f.hasta) p.set("hasta", f.hasta);
    if (f.calificaciones.length) p.set("calificacion", f.calificaciones.join(","));
    if (f.entidad) p.set("entidad", f.entidad);
    if (f.estado) p.set("estado", f.estado);
    const q = p.toString();
    return q ? `/encuestas?${q}` : "/encuestas";
}

export function hayFiltrosEncuestas(f: FiltrosEncuestas): boolean {
    return Boolean(f.desde || f.hasta || f.calificaciones.length || f.entidad || f.estado);
}

/** «Desde» después de «hasta»: no se pide. */
export function errorFechasEncuestas(f: Pick<FiltrosEncuestas, "desde" | "hasta">): string | undefined {
    return f.desde && f.hasta && f.desde > f.hasta ? "«Desde» va antes de «hasta»" : undefined;
}

export type ConsultaEncuestas = NonNullable<operations["listarEncuestas"]["parameters"]["query"]>;

export const LIMITE_ENCUESTAS = 25;

export function consultaEncuestas(f: FiltrosEncuestas): ConsultaEncuestas {
    const q: ConsultaEncuestas = { limit: LIMITE_ENCUESTAS };
    if (f.desde) q.desde = f.desde;
    if (f.hasta) q.hasta = f.hasta;
    if (f.calificaciones.length) q.calificacion = [...f.calificaciones];
    if (f.entidad) q.entidad = f.entidad;
    if (f.estado) q.estado = f.estado;
    return q;
}
