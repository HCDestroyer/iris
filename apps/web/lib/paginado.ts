"use client";

import { useState } from "react";
import { useLectura } from "./lectura";

export type PaginaCursor<T> = { data: T[] | null; nextCursor: string | null };

/**
 * Lista con cursor (`{ data, nextCursor }`): la primera página se lee al montar y «Cargar más»
 * trae las siguientes. Si «Cargar más» falla, lo ya leído sigue a la vista y el error va aparte.
 */
export function usePaginado<T>(leer: (cursor: string | undefined, signal?: AbortSignal) => Promise<PaginaCursor<T>>) {
    const primera = useLectura((signal) => leer(undefined, signal));
    const [extra, setExtra] = useState<{ items: T[]; cursor: string | null } | null>(null);
    const [agregados, setAgregados] = useState<T[]>([]);
    const [cargandoMas, setCargandoMas] = useState(false);
    const [errorMas, setErrorMas] = useState<unknown>(null);
    const siguiente = primera.estado === "listo" ? (extra ? extra.cursor : primera.datos.nextCursor) : null;

    async function cargarMas() {
        if (!siguiente) return;
        setCargandoMas(true);
        setErrorMas(null);
        try {
            const p = await leer(siguiente);
            setExtra((prev) => ({ items: [...(prev?.items ?? []), ...(p.data ?? [])], cursor: p.nextCursor }));
        } catch (e) {
            setErrorMas(e);
        } finally {
            setCargandoMas(false);
        }
    }

    async function refrescar() {
        setExtra(null);
        setAgregados([]);
        setErrorMas(null);
        await primera.refrescar();
    }

    /** Agrega al final un elemento que devolvió una acción (p. ej. el mensaje recién enviado). */
    function agregar(item: T) {
        setAgregados((prev) => [...prev, item]);
    }

    const items = primera.estado === "listo" ? [...(primera.datos.data ?? []), ...(extra?.items ?? []), ...agregados] : [];
    return { primera, items, siguiente, cargarMas, cargandoMas, errorMas, refrescar, agregar };
}

export type Paginado<T> = ReturnType<typeof usePaginado<T>>;
