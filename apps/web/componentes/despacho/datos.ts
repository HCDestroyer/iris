"use client";

import { useEffect, useState } from "react";
import { api, resolver } from "@/lib/api/cliente";
import type { ErrorApi } from "@/lib/api/errores";
import type { Calendario, Cuadrilla, Tecnico, Zona } from "@/lib/despacho/calendario";
import { rangoUtc, type Dia } from "@/lib/despacho/fechas";
import type { RespuestaAsignables } from "@/lib/despacho/mover";
import type { UltimasPosiciones } from "@/lib/despacho/posiciones";
import { useLectura, type Lectura } from "@/lib/lectura";
import type { Orden, TipoOrden } from "@/lib/ordenes/presentacion";
import { useSesion } from "@/lib/sesion";

/**
 * Filtros del despacho (ids del catálogo de agenda; `null` = todas). `tecnicoId` (usuario) solo lo
 * usa la agenda: filtra las filas y las citas, no las órdenes sin programar (son para cualquiera).
 */
export type FiltrosDespacho = { cuadrillaId: number | null; zonaId: number | null; tecnicoId?: number | null };

const query = (f: FiltrosDespacho) => ({
    ...(f.cuadrillaId !== null ? { cuadrillaId: f.cuadrillaId } : {}),
    ...(f.zonaId !== null ? { zonaId: f.zonaId } : {}),
});

/** Zonas y cuadrillas activas para los filtros y los nombres («Zona 4» solo si no está en el catálogo). */
export function useCatalogosAgenda() {
    const zonas = useLectura<Zona[]>(async (signal) => (await resolver(api().GET("/api/v1/zonas", { signal }))).data ?? []);
    const cuadrillas = useLectura<Cuadrilla[]>(async (signal) => (await resolver(api().GET("/api/v1/cuadrillas", { signal }))).data ?? []);
    return { zonas, cuadrillas };
}

export const LIMITE_TECNICOS = 200;
export const LIMITE_SIN_PROGRAMAR = 50;

export type Tecnicos = { lista: Tecnico[]; cortada: boolean };

export type DatosAgenda = {
    tecnicos: Lectura<Tecnicos>;
    agenda: Lectura<Calendario>;
    /** Órdenes pendientes sin programar (`null` sin `ordenes.ver`). */
    sinProgramar: Lectura<{ ordenes: Orden[]; cortada: boolean } | null>;
    /** Relee todo sin pasar por «cargando» (tras mover una orden o un 409). */
    refrescar: () => Promise<void>;
};

/**
 * Lo que necesita el calendario: técnicos activos (filas), `GET /agenda` del rango (hasta 31
 * días) y las órdenes pendientes. El componente que lo usa se monta con `key` por rango y filtros.
 */
export function useDatosAgenda(desde: Dia, dias: number, filtros: FiltrosDespacho): DatosAgenda {
    const { puede } = useSesion();
    const veOrdenes = puede(["ordenes.ver"]);
    const tecnicos = useLectura<Tecnicos>(async (signal) => {
        const p = await resolver(
            api().GET("/api/v1/tecnicos", {
                params: { query: { activo: "true", limit: LIMITE_TECNICOS, ...query(filtros), ...(filtros.tecnicoId ? { usuarioId: filtros.tecnicoId } : {}) } },
                signal,
            }),
        );
        return { lista: p.data ?? [], cortada: p.nextCursor !== null };
    });
    const agenda = useLectura<Calendario>((signal) =>
        resolver(
            api().GET("/api/v1/agenda", {
                params: { query: { ...rangoUtc(desde, dias), ...query(filtros), ...(filtros.tecnicoId ? { tecnicoId: filtros.tecnicoId } : {}) } },
                signal,
            }),
        ),
    );
    const sinProgramar = useLectura(async (signal) => {
        if (!veOrdenes) return null;
        const p = await resolver(api().GET("/api/v1/ordenes", { params: { query: { estado: ["pendiente"], limit: LIMITE_SIN_PROGRAMAR, ...query(filtros) } }, signal }));
        return { ordenes: p.data ?? [], cortada: p.nextCursor !== null };
    });
    const refrescar = async () => {
        await Promise.all([tecnicos.refrescar(), agenda.refrescar(), sinProgramar.refrescar()]);
    };
    return { tecnicos, agenda, sinProgramar, refrescar };
}

export type ConsultaAsignables = { fecha: Dia; tipo: TipoOrden; zonaId: number | null; ordenId: number | null };

export type EstadoAsignables = { estado: "cargando" } | { estado: "error"; error: ErrorApi | Error } | { estado: "listo"; datos: RespuestaAsignables };

/** `GET /tecnicos/asignables` del día y la orden; se vuelve a leer al cambiar el día. */
export function useAsignables(consulta: ConsultaAsignables | null): EstadoAsignables & { reintentar: () => void } {
    const [intento, setIntento] = useState(0);
    const fecha = consulta?.fecha;
    const tipo = consulta?.tipo;
    const zonaId = consulta?.zonaId ?? null;
    const ordenId = consulta?.ordenId ?? null;
    const clave = `${fecha}|${tipo}|${zonaId}|${ordenId}|${intento}`;
    // El resultado queda guardado con la consulta que lo pidió: otra consulta = «cargando».
    const [resultado, setResultado] = useState<{ clave: string; estado: EstadoAsignables } | null>(null);

    useEffect(() => {
        if (fecha === undefined || tipo === undefined) return;
        const control = new AbortController();
        resolver(
            api().GET("/api/v1/tecnicos/asignables", {
                params: {
                    query: {
                        fecha,
                        tipo,
                        ...(zonaId !== null ? { zonaId } : {}),
                        ...(ordenId !== null ? { ordenId } : {}),
                    },
                },
                signal: control.signal,
            }),
        ).then(
            (datos) => {
                if (!control.signal.aborted) setResultado({ clave, estado: { estado: "listo", datos } });
            },
            (error: unknown) => {
                if (!control.signal.aborted) setResultado({ clave, estado: { estado: "error", error: error instanceof Error ? error : new Error(String(error)) } });
            },
        );
        return () => control.abort();
    }, [clave, fecha, tipo, zonaId, ordenId]);

    const estado: EstadoAsignables = resultado?.clave === clave ? resultado.estado : { estado: "cargando" };
    return { ...estado, reintentar: () => setIntento((n) => n + 1) };
}

/**
 * Últimas posiciones de los técnicos con refresco periódico; `leidaEn` es la hora local de la
 * lectura. Sin `activo` (sin `campo.ver_posiciones`) no se pide nada (`null`).
 */
export function usePosiciones(filtros: FiltrosDespacho, activo: boolean, intervaloMs: number) {
    const lectura = useLectura<{ respuesta: UltimasPosiciones; leidaEn: number } | null>(async (signal) => {
        if (!activo) return null;
        const respuesta = await resolver(api().GET("/api/v1/campo/posiciones/ultimas", { params: { query: query(filtros) }, signal }));
        return { respuesta, leidaEn: Date.now() };
    });
    const { refrescar } = lectura;
    const enError = lectura.estado === "error";
    useEffect(() => {
        // Tras un error no se insiste solo: el aviso ofrece «Reintentar».
        if (enError || !activo) return;
        const t = setInterval(() => {
            if (typeof document === "undefined" || document.visibilityState !== "hidden") void refrescar();
        }, intervaloMs);
        return () => clearInterval(t);
    }, [refrescar, intervaloMs, enError, activo]);
    return lectura;
}

/** Hora actual que avanza cada `ms` (para la antigüedad de las posiciones). */
export function useAhora(ms: number): number {
    const [ahora, setAhora] = useState(() => Date.now());
    useEffect(() => {
        const t = setInterval(() => setAhora(Date.now()), ms);
        return () => clearInterval(t);
    }, [ms]);
    return ahora;
}
