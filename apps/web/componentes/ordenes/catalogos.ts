"use client";

import type { Schema } from "@iris/contratos";
import type { Usuario } from "@/componentes/tickets/catalogos";
import { api, resolver } from "@/lib/api/cliente";
import { useLectura } from "@/lib/lectura";
import { ESTADOS_PENDIENTE_ABIERTOS, type Pendiente } from "@/lib/ordenes/presentacion";
import { useSesion } from "@/lib/sesion";

/** Tipos de orden con sus plantillas (`GET /tipos-orden`). */
export function useTiposOrden() {
    return useLectura<Schema<"TipoOrden">[]>(async (signal) => (await resolver(api().GET("/api/v1/tipos-orden", { signal }))).data ?? []);
}

/**
 * Técnicos para asignar y programar: usuarios activos con el rol `tecnico` (de `GET /usuarios`,
 * ya leídos por `useUsuarios`). La API valida que el técnico vea la orden y opere en la empresa.
 */
export function tecnicosDe(usuarios: readonly Usuario[] | null | undefined): Usuario[] {
    return (usuarios ?? []).filter((u) => u.tipo === "persona" && u.activo && (u.roles ?? []).some((r) => r.rol === "tecnico"));
}

const LIMITE_PENDIENTES = 200;

export type PendientesPorOrden = {
    /** Pendientes no `hecho` (o con conflicto) por id de orden (texto, como `entidadId`). */
    porOrden: ReadonlyMap<string, Pendiente[]>;
    /** Algún estado tenía más de 200: la marca puede faltar en órdenes antiguas. */
    parcial: boolean;
};

/**
 * Pendientes hacia Argos que no están `hecho` (una lectura por estado: la API filtra por uno) y
 * los que tienen un conflicto de cierre repetido (aunque estén hechos), agrupados por orden, para
 * la marca «Pendiente en Argos» del tablero. Sin `integraciones.ver_pendientes` no se pide (`null`).
 */
export function usePendientesAbiertos() {
    const { puede } = useSesion();
    const permitido = puede(["integraciones.ver_pendientes"]);
    return useLectura<PendientesPorOrden | null>(async (signal) => {
        if (!permitido) {
            return null;
        }
        const consultas = [...ESTADOS_PENDIENTE_ABIERTOS.map((estado) => ({ estado })), { conflicto: true }];
        const paginas = await Promise.all(
            consultas.map((filtro) =>
                resolver(api().GET("/api/v1/integraciones/pendientes", { params: { query: { entidad: "orden", limit: LIMITE_PENDIENTES, ...filtro } }, signal })),
            ),
        );
        const porId = new Map<number, Pendiente>();
        for (const p of paginas.flatMap((x) => x.data ?? [])) {
            if (p.entidad === "orden" && (p.estado !== "hecho" || p.conflicto !== null)) porId.set(p.id, p);
        }
        const porOrden = new Map<string, Pendiente[]>();
        for (const p of porId.values()) {
            porOrden.set(p.entidadId, [...(porOrden.get(p.entidadId) ?? []), p]);
        }
        return { porOrden, parcial: paginas.some((x) => x.nextCursor !== null) };
    });
}
