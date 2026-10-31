// «Por recibir» y «Mi bodega» (contrato Iris ↔ ArgosISP 0.5.0): lecturas de la API que se hacen con
// señal y quedan en memoria mientras la sesión está abierta (sin señal se ve lo último leído). No
// se guardan en el teléfono: al cerrar sesión o cambiar de empresa se olvidan. Como la PWA.
import type { InventarioCampo, MiBodega, Traslado } from "@iris/sync";
import { createContext, useCallback, useContext, useEffect, useEffectEvent, useMemo, useRef, useState, type ReactNode } from "react";
import { useCampo } from "./contexto";

export type Lectura<T> =
    | { estado: "inactiva"; datos: null; error: null; leidoEn: null }
    | { estado: "cargando" | "lista" | "error"; datos: T | null; error: unknown; leidoEn: number | null };

const INACTIVA = { estado: "inactiva", datos: null, error: null, leidoEn: null } as const;

interface ContextoInventario {
    traslados: Lectura<Traslado[]>;
    bodega: Lectura<MiBodega>;
    cargarTraslados: () => Promise<void>;
    cargarBodega: () => Promise<void>;
}

const Contexto = createContext<ContextoInventario | null>(null);

export function useInventario(): ContextoInventario {
    const c = useContext(Contexto);
    if (!c) {
        throw new Error("useInventario fuera de ProveedorInventario");
    }
    return c;
}

/** Una lectura a la vez; si falla, se conserva lo último leído. */
export function useLectura<T>(leer: () => Promise<T>): [Lectura<T>, () => Promise<void>] {
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

export function ProveedorInventario({ inventario, children }: { inventario: InventarioCampo; children: ReactNode }) {
    const leerTraslados = useCallback(() => inventario.porRecibir(), [inventario]);
    const leerBodega = useCallback(() => inventario.miBodega(), [inventario]);
    const [traslados, cargarTraslados] = useLectura(leerTraslados);
    const [bodega, cargarBodega] = useLectura(leerBodega);
    const valor = useMemo(() => ({ traslados, bodega, cargarTraslados, cargarBodega }), [traslados, bodega, cargarTraslados, cargarBodega]);
    return <Contexto.Provider value={valor}>{children}</Contexto.Provider>;
}

/** Al abrir la vista (y al volver la señal) se relee; sin señal queda lo último leído. */
export function useCargarAlAbrir(cargar: () => Promise<void>): void {
    const { estado } = useCampo();
    const sinSenal = estado.conexion === "sin_conexion";
    const releer = useEffectEvent(() => void cargar());
    useEffect(() => {
        if (!sinSenal) {
            releer();
        }
    }, [sinSenal]);
}

/** Cuando una recepción del teléfono sale (o cambia), se relee la lista para ver cómo va en Argos. */
export function useReleerAlEnviar(cargar: () => Promise<void>): void {
    const { estado } = useCampo();
    const ids = estado.recepciones.map((r) => `${r.id}:${r.estado}`).join(",");
    const previos = useRef(ids);
    const sinSenal = estado.conexion === "sin_conexion";
    const releer = useEffectEvent(() => void cargar());
    useEffect(() => {
        if (previos.current !== ids && !sinSenal) {
            releer();
        }
        previos.current = ids;
    }, [ids, sinSenal]);
}
