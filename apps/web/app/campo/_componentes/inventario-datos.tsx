"use client";

import type { InventarioCampo, MiBodega, Traslado } from "@iris/sync";
import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from "react";

/** Lectura de la API guardada en memoria mientras la app está abierta (sin señal se ve la última). */
export type Lectura<T> =
    | { estado: "inactiva"; datos: null; error: null; leidoEn: null }
    | { estado: "cargando" | "lista" | "error"; datos: T | null; error: unknown; leidoEn: number | null };

const INACTIVA = { estado: "inactiva", datos: null, error: null, leidoEn: null } as const;

type ContextoInventario = {
    traslados: Lectura<Traslado[]>;
    bodega: Lectura<MiBodega>;
    cargarTraslados: () => Promise<void>;
    cargarBodega: () => Promise<void>;
};

const Contexto = createContext<ContextoInventario | null>(null);

export function useInventario(): ContextoInventario {
    const c = useContext(Contexto);
    if (!c) {
        throw new Error("useInventario fuera de ProveedorInventario");
    }
    return c;
}

function useLectura<T>(leer: () => Promise<T>): [Lectura<T>, () => Promise<void>] {
    const [lectura, setLectura] = useState<Lectura<T>>(INACTIVA);
    const enCurso = useRef<Promise<void> | null>(null);
    const cargar = useCallback(() => {
        if (enCurso.current) {
            return enCurso.current;
        }
        setLectura((l) => ({ estado: "cargando", datos: l.datos, error: null, leidoEn: l.leidoEn }));
        const p = leer()
            .then((datos) => setLectura({ estado: "lista", datos, error: null, leidoEn: Date.now() }))
            .catch((error: unknown) => setLectura((l) => ({ estado: "error", datos: l.datos, error, leidoEn: l.leidoEn })))
            .finally(() => {
                enCurso.current = null;
            });
        enCurso.current = p;
        return p;
    }, [leer]);
    return [lectura, cargar];
}

/**
 * «Por recibir» y «Mi bodega»: se leen con señal y quedan en memoria del motor abierto (no se
 * guardan en el teléfono: al cerrar sesión o cambiar de empresa se olvidan).
 */
export function ProveedorInventario({ inventario, children }: { inventario: InventarioCampo; children: ReactNode }) {
    const leerTraslados = useCallback(() => inventario.porRecibir(), [inventario]);
    const leerBodega = useCallback(() => inventario.miBodega(), [inventario]);
    const [traslados, cargarTraslados] = useLectura(leerTraslados);
    const [bodega, cargarBodega] = useLectura(leerBodega);
    const valor = useMemo(() => ({ traslados, bodega, cargarTraslados, cargarBodega }), [traslados, bodega, cargarTraslados, cargarBodega]);
    return <Contexto.Provider value={valor}>{children}</Contexto.Provider>;
}
