import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";
import { JORGE, LUCIA, ROSA } from "./datos-agenda";

/*
 * Reportes (Fase 5) en la API falsa (e2e/api-falsa.ts), tipados con packages/contratos y fieles a
 * internal/reportes: permiso `reportes.ver`, `desde`/`hasta` obligatorios (422 `validacion`),
 * como mucho 366 días (422 `rango_excedido`), porcentajes `null` sin base, reincidencias con
 * cursor y `?formato=csv` → `text/csv` con BOM (422 `exportacion_excedida` si se pide).
 */

type Problema = Schema<"Problema">;
type Permiso = Schema<"Permiso">;

const json = (route: Route, cuerpo: unknown) => route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });

const periodo = (desde: string, hasta: string): Schema<"Periodo"> => ({
    desde,
    hasta,
    inicio: `${desde}T06:00:00Z`,
    fin: `${hasta}T06:00:00Z`,
    zonaHoraria: "America/Guatemala",
});

const pct = (a: number, b: number) => (b === 0 ? null : Math.round((a / b) * 10000) / 100);

function filaSla(d: Partial<Schema<"FilaSLA">> & { resueltos: number; respuesta: [number, number]; resolucion: [number, number] }): Schema<"FilaSLA"> {
    const { respuesta, resolucion, ...resto } = d;
    return {
        ...resto,
        respuestaEnTiempo: respuesta[0],
        conSlaRespuesta: respuesta[1],
        porcentajeRespuesta: pct(respuesta[0], respuesta[1]),
        resolucionEnTiempo: resolucion[0],
        conSlaResolucion: resolucion[1],
        porcentajeResolucion: pct(resolucion[0], resolucion[1]),
    };
}

export const REPORTE_SLA = (desde: string, hasta: string): Schema<"ReporteSLA"> => ({
    periodo: periodo(desde, hasta),
    total: filaSla({ resueltos: 120, respuesta: [102, 110], resolucion: [87, 100] }),
    porPrioridad: [
        filaSla({ prioridad: "baja", resueltos: 20, respuesta: [0, 0], resolucion: [0, 0] }),
        filaSla({ prioridad: "critica", resueltos: 10, respuesta: [9, 10], resolucion: [7, 10] }),
        filaSla({ prioridad: "alta", resueltos: 40, respuesta: [36, 40], resolucion: [30, 40] }),
        filaSla({ prioridad: "media", resueltos: 50, respuesta: [47, 50], resolucion: [43, 50] }),
    ],
    porCategoria: [
        filaSla({ categoria: "Sin servicio", categoriaId: 1, resueltos: 70, respuesta: [62, 66], resolucion: [50, 60] }),
        filaSla({ categoria: "Lentitud", categoriaId: 3, resueltos: 45, respuesta: [40, 44], resolucion: [37, 40] }),
        filaSla({ resueltos: 5, respuesta: [0, 0], resolucion: [0, 0] }),
    ],
});

export const REPORTE_RESOLUCION = (desde: string, hasta: string): Schema<"ReporteResolucion"> => ({
    periodo: periodo(desde, hasta),
    total: { resueltos: 120, promedioMin: 410.5, medianaMin: 185 },
    porPrioridad: [
        { prioridad: "critica", resueltos: 10, promedioMin: 95, medianaMin: 80 },
        { prioridad: "alta", resueltos: 40, promedioMin: 240, medianaMin: 150 },
        { prioridad: "media", resueltos: 50, promedioMin: 520, medianaMin: 300 },
        { prioridad: "baja", resueltos: 20, promedioMin: 1600, medianaMin: 1500 },
    ],
    porCategoria: [
        { categoria: "Sin servicio", categoriaId: 1, resueltos: 70, promedioMin: 300, medianaMin: 160 },
        { categoria: "Lentitud", categoriaId: 3, resueltos: 50, promedioMin: 565, medianaMin: 240 },
    ],
});

export const REPORTE_TECNICOS = (desde: string, hasta: string): Schema<"ReporteTecnicos"> => ({
    periodo: periodo(desde, hasta),
    total: { tecnicoId: 0, nombre: "Total", finalizadas: 47, exitosas: 40, parciales: 4, fallidas: 3, porcentajeExito: 85.11, conTiempoEnSitio: 45, duracionMediaSitioMin: 52.4, tiempoPausadoMin: 180, tiempoPausadoMedioMin: 30, conPausa: 6 },
    data: [
        { tecnicoId: JORGE, nombre: "Jorge Méndez", finalizadas: 20, exitosas: 18, parciales: 1, fallidas: 1, porcentajeExito: 90, conTiempoEnSitio: 20, duracionMediaSitioMin: 48, tiempoPausadoMin: 60, tiempoPausadoMedioMin: 30, conPausa: 2 },
        { tecnicoId: LUCIA, nombre: "Lucía Ramírez", finalizadas: 27, exitosas: 22, parciales: 3, fallidas: 2, porcentajeExito: 81.48, conTiempoEnSitio: 25, duracionMediaSitioMin: 55.5, tiempoPausadoMin: 120, tiempoPausadoMedioMin: 30, conPausa: 4 },
        { tecnicoId: ROSA, nombre: "Rosa Ajú", finalizadas: 0, exitosas: 0, parciales: 0, fallidas: 0, porcentajeExito: null, conTiempoEnSitio: 0, duracionMediaSitioMin: null, tiempoPausadoMin: 0, tiempoPausadoMedioMin: null, conPausa: 0 },
    ],
});

export const REPORTE_PRIMERA_VISITA = (desde: string, hasta: string, dias: number): Schema<"ReportePrimeraVisita"> => ({
    periodo: periodo(desde, hasta),
    dias,
    total: { cerradas: 40, evaluadas: 30, resueltas: 27, conRetorno: 3, porcentaje: 90, enObservacion: 8, sinServicio: 2 },
    porTipo: [
        { tipo: "instalacion", cerradas: 25, evaluadas: 20, resueltas: 19, conRetorno: 1, porcentaje: 95, enObservacion: 5, sinServicio: 0 },
        { tipo: "visita", cerradas: 15, evaluadas: 10, resueltas: 8, conRetorno: 2, porcentaje: 80, enObservacion: 3, sinServicio: 2 },
    ],
    porTecnico: [
        { tecnico: "Jorge Méndez", tecnicoId: JORGE, cerradas: 22, evaluadas: 18, resueltas: 17, conRetorno: 1, porcentaje: 94.44, enObservacion: 4, sinServicio: 0 },
        { tecnico: "Lucía Ramírez", tecnicoId: LUCIA, cerradas: 18, evaluadas: 12, resueltas: 10, conRetorno: 2, porcentaje: 83.33, enObservacion: 4, sinServicio: 2 },
        { tecnico: "Rosa Ajú", tecnicoId: ROSA, cerradas: 0, evaluadas: 0, resueltas: 0, conRetorno: 0, porcentaje: null, enObservacion: 0, sinServicio: 0 },
    ],
});

/** 60 reincidencias, de la más reciente a la más antigua. */
export const REINCIDENCIAS: Schema<"Reincidencia">[] = Array.from({ length: 60 }, (_, i) => {
    const resuelto = Date.UTC(2026, 9, 28, 18) - i * 3 * 3600_000;
    const horas = 5 + (i % 7) * 11;
    return {
        ticketId: 7000 + i,
        numero: `TK-${String(7000 + i).padStart(6, "0")}`,
        asunto: i % 2 ? "Sin internet desde la mañana" : "Lentitud en horas pico",
        prioridad: (["alta", "media", "critica", "baja"] as const)[i % 4] ?? "media",
        categoria: i % 2 ? "Sin servicio" : "Lentitud",
        categoriaId: i % 2 ? 1 : 3,
        clienteId: 501,
        servicioId: 9001 + i,
        resueltoEn: new Date(resuelto).toISOString(),
        siguienteId: 8000 + i,
        siguienteNumero: `TK-${String(8000 + i).padStart(6, "0")}`,
        siguienteCreadoEn: new Date(resuelto + horas * 3600_000).toISOString(),
        horasHastaVolver: horas,
        nuevosEnVentana: 1 + (i % 3),
    };
});

export type ApiReportes = {
    /** El próximo CSV responde 422 `exportacion_excedida`. */
    exportacionExcedida: { n: number };
    manejar: (c: { route: Route; metodo: string; ruta: string; query: URLSearchParams; puede: (p: Permiso) => boolean }) => Promise<boolean>;
};

const DIA = /^\d{4}-\d{2}-\d{2}$/;

export function apiReportes(): ApiReportes {
    const exportacionExcedida = { n: 0 };
    return {
        exportacionExcedida,
        async manejar({ route, metodo, ruta, query, puede }) {
            if (!ruta.startsWith("/api/v1/reportes/") || metodo !== "GET") return false;
            const reporte = ruta.slice("/api/v1/reportes/".length);
            if (!["sla", "resolucion", "tecnicos", "primera-visita", "reincidencias"].includes(reporte)) return false;
            if (!puede("reportes.ver")) {
                await problema(route, { type: "urn:iris:problema:prohibido", title: "Sin permiso", status: 403 });
                return true;
            }
            const desde = query.get("desde") ?? "";
            const hasta = query.get("hasta") ?? "";
            if (!DIA.test(desde) || !DIA.test(hasta) || desde > hasta) {
                await problema(route, { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, detail: "desde y hasta son obligatorios (YYYY-MM-DD)" });
                return true;
            }
            const dias = (Date.parse(hasta) - Date.parse(desde)) / 86_400_000 + 1;
            if (dias > 366) {
                await problema(route, { type: "urn:iris:problema:rango_excedido", title: "Período demasiado largo", status: 422, detail: "El período admite como mucho 366 días" });
                return true;
            }
            // La API lee los arreglos separados por coma (explode=false): `prioridad=alta,critica`.
            if (query.getAll("prioridad").length > 1) {
                await problema(route, { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, detail: "prioridad repetida" });
                return true;
            }
            const ventana = Number(query.get("dias") ?? 30);

            if (query.get("formato") === "csv") {
                if (exportacionExcedida.n > 0) {
                    exportacionExcedida.n--;
                    await problema(route, {
                        type: "urn:iris:problema:exportacion_excedida",
                        title: "Demasiadas filas para exportar",
                        status: 422,
                        detail: "El CSV admite hasta 10 000 filas: acota el período o los filtros",
                    });
                    return true;
                }
                const filas = reporte === "reincidencias" ? REINCIDENCIAS.map((r) => `${r.numero};${r.siguienteNumero};${r.horasHastaVolver}`) : ["Total;120"];
                await route.fulfill({
                    status: 200,
                    contentType: "text/csv; charset=utf-8",
                    headers: { "Content-Disposition": `attachment; filename="${reporte}.csv"` },
                    body: `\uFEFFReporte;${reporte}\nDesde;${desde}\nHasta;${hasta}\n${filas.join("\n")}\n`,
                });
                return true;
            }

            switch (reporte) {
                case "sla":
                    await json(route, REPORTE_SLA(desde, hasta));
                    return true;
                case "resolucion":
                    await json(route, REPORTE_RESOLUCION(desde, hasta));
                    return true;
                case "tecnicos":
                    await json(route, REPORTE_TECNICOS(desde, hasta));
                    return true;
                case "primera-visita":
                    await json(route, REPORTE_PRIMERA_VISITA(desde, hasta, ventana));
                    return true;
                default: {
                    const limite = Math.min(200, Math.max(1, Number(query.get("limit") ?? 50)));
                    const inicio = Number(query.get("cursor") ?? 0);
                    const data = REINCIDENCIAS.slice(inicio, inicio + limite);
                    const siguiente = inicio + limite < REINCIDENCIAS.length ? String(inicio + limite) : null;
                    await json(route, {
                        periodo: periodo(desde, hasta),
                        dias: ventana,
                        resueltos: 400,
                        evaluables: 380,
                        reincidentes: REINCIDENCIAS.length,
                        porcentaje: pct(REINCIDENCIAS.length, 380),
                        data,
                        nextCursor: siguiente,
                    } satisfies Schema<"ReporteReincidencias">);
                    return true;
                }
            }
        },
    };
}
