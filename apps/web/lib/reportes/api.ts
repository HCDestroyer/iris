import type { Schema } from "@iris/contratos";
import { api, resolver } from "../api/cliente";
import type { QueryReporte, Reporte } from "./filtros";

export type ReporteSLA = Schema<"ReporteSLA">;
export type ReporteResolucion = Schema<"ReporteResolucion">;
export type ReporteTecnicos = Schema<"ReporteTecnicos">;
export type ReportePrimeraVisita = Schema<"ReportePrimeraVisita">;
export type ReporteReincidencias = Schema<"ReporteReincidencias">;
export type FilaSLA = Schema<"FilaSLA">;
export type FilaResolucion = Schema<"FilaResolucion">;
export type FilaTecnico = Schema<"FilaTecnico">;
export type FilaPrimeraVisita = Schema<"FilaPrimeraVisita">;
export type Reincidencia = Schema<"Reincidencia">;

/** Página de reincidencias (la lista va por cursor; los totales vienen en cada página). */
export const LIMITE_REINCIDENCIAS = 50;

/**
 * La respuesta 200 declara JSON y CSV; sin `formato=csv` la API responde JSON, así que se toma el
 * esquema del reporte (si llegara texto, se rechaza en lugar de pintar algo que no es el reporte).
 */
async function jsonDe<D>(llamada: Promise<{ data?: D | string; error?: unknown; response: Response }>): Promise<D> {
    const datos = await resolver(llamada);
    if (typeof datos === "string" || datos === undefined || datos === null) throw new Error("El reporte no llegó en JSON");
    return datos;
}

export const leerSLA = (query: QueryReporte, signal?: AbortSignal): Promise<ReporteSLA> =>
    jsonDe(api().GET("/api/v1/reportes/sla", { params: { query }, signal }));

export const leerResolucion = (query: QueryReporte, signal?: AbortSignal): Promise<ReporteResolucion> =>
    jsonDe(api().GET("/api/v1/reportes/resolucion", { params: { query }, signal }));

export const leerTecnicos = (query: QueryReporte, signal?: AbortSignal): Promise<ReporteTecnicos> =>
    jsonDe(api().GET("/api/v1/reportes/tecnicos", { params: { query }, signal }));

export const leerPrimeraVisita = (query: QueryReporte, signal?: AbortSignal): Promise<ReportePrimeraVisita> =>
    jsonDe(api().GET("/api/v1/reportes/primera-visita", { params: { query }, signal }));

export const leerReincidencias = (query: QueryReporte, cursor: string | undefined, signal?: AbortSignal): Promise<ReporteReincidencias> =>
    jsonDe(
        api().GET("/api/v1/reportes/reincidencias", {
            params: { query: { ...query, limit: LIMITE_REINCIDENCIAS, ...(cursor ? { cursor } : {}) } },
            signal,
        }),
    );

/** El CSV del reporte con los mismos filtros (`?formato=csv`), con la sesión del panel. */
export async function descargarCsv(reporte: Reporte, query: QueryReporte): Promise<Blob> {
    const opciones = { params: { query: { ...query, formato: "csv" as const } }, parseAs: "blob" as const };
    const c = api();
    let datos: unknown;
    switch (reporte) {
        case "sla":
            datos = await resolver(c.GET("/api/v1/reportes/sla", opciones));
            break;
        case "resolucion":
            datos = await resolver(c.GET("/api/v1/reportes/resolucion", opciones));
            break;
        case "tecnicos":
            datos = await resolver(c.GET("/api/v1/reportes/tecnicos", opciones));
            break;
        case "primera-visita":
            datos = await resolver(c.GET("/api/v1/reportes/primera-visita", opciones));
            break;
        case "reincidencias":
            datos = await resolver(c.GET("/api/v1/reportes/reincidencias", opciones));
            break;
    }
    if (!(datos instanceof Blob)) throw new Error("La respuesta no trajo el archivo");
    return datos;
}
