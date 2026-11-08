"use client";

import { useEffect, useState } from "react";
import { api, resolver } from "@/lib/api/cliente";
import { tieneMarcadores } from "@/lib/manuales/marcadores";

const EN_PARALELO = 4;

/**
 * Qué manuales de la lista todavía tienen datos de la empresa por completar («[Nombre del ISP]»…).
 * El resumen de la lista no trae el cuerpo: se lee cada manual (`GET /manuales/{id}`), de 4 en 4.
 * Devuelve solo los que se pudieron leer y tienen marcadores; uno que no se pudo leer no se marca
 * (la bandeja igual lo revisa antes de enviarlo).
 */
export function useFaltaCompletar(ids: readonly number[] | null): ReadonlySet<number> {
    const [pendientes, setPendientes] = useState<ReadonlySet<number>>(new Set());
    const clave = ids ? ids.join(",") : "";
    useEffect(() => {
        if (!clave) return undefined;
        const lista = clave.split(",").map(Number);
        const control = new AbortController();
        const encontrados = new Set<number>();
        let siguiente = 0;
        const trabajar = async () => {
            while (siguiente < lista.length && !control.signal.aborted) {
                const id = lista[siguiente++];
                if (id === undefined) break;
                try {
                    const m = await resolver(api().GET("/api/v1/mensajeria/manuales/{id}", { params: { path: { id } }, signal: control.signal }));
                    if (tieneMarcadores(m.cuerpo)) {
                        encontrados.add(id);
                        if (!control.signal.aborted) setPendientes(new Set(encontrados));
                    }
                } catch {
                    // Sin leer: no se marca.
                }
            }
        };
        void Promise.all(Array.from({ length: Math.min(EN_PARALELO, lista.length) }, trabajar));
        return () => control.abort();
    }, [clave]);
    return clave ? pendientes : new Set();
}
