"use client";

import type { LibresCampo, OrdenesLibres } from "@iris/sync";
import {
    createContext,
    useCallback,
    useContext,
    useMemo,
    useRef,
    useState,
    type ReactNode,
} from "react";
import type { Lectura } from "./inventario-datos";

type ContextoLibres = {
    api: LibresCampo;
    /** La primera página leída (con su `total` para el contador del «+»). */
    libres: Lectura<OrdenesLibres>;
    cargarLibres: () => Promise<void>;
    /** Quita una orden de la lista leída (tomada por el técnico o por otro). */
    quitar: (id: number) => void;
};

const Contexto = createContext<ContextoLibres | null>(null);

export function useLibres(): ContextoLibres {
    const c = useContext(Contexto);
    if (!c) {
        throw new Error("useLibres fuera de ProveedorLibres");
    }
    return c;
}

/**
 * Órdenes libres (botón «+»): se leen con señal (`GET /campo/ordenes-libres`) y quedan en memoria;
 * nunca se guardan en el teléfono ni van por la cola. Sin leer no hay contador.
 */
export function ProveedorLibres({
    api,
    children,
}: {
    api: LibresCampo;
    children: ReactNode;
}) {
    const [libres, setLibres] = useState<Lectura<OrdenesLibres>>({
        estado: "inactiva",
        datos: null,
        error: null,
        leidoEn: null,
    });
    const enCurso = useRef<Promise<void> | null>(null);
    const cargarLibres = useCallback(() => {
        if (enCurso.current) {
            return enCurso.current;
        }
        setLibres((l) => ({
            estado: "cargando",
            datos: l.datos,
            error: null,
            leidoEn: l.leidoEn,
        }));
        const p = api
            .listar()
            .then((datos) =>
                setLibres({
                    estado: "lista",
                    datos,
                    error: null,
                    leidoEn: Date.now(),
                }),
            )
            .catch((error: unknown) =>
                setLibres((l) => ({
                    estado: "error",
                    datos: l.datos,
                    error,
                    leidoEn: l.leidoEn,
                })),
            )
            .finally(() => {
                enCurso.current = null;
            });
        enCurso.current = p;
        return p;
    }, [api]);
    const quitar = useCallback((id: number) => {
        setLibres((l) =>
            l.datos
                ? {
                      ...l,
                      datos: {
                          ...l.datos,
                          data: (l.datos.data ?? []).filter((o) => o.id !== id),
                          total: Math.max(0, l.datos.total - 1),
                      },
                  }
                : l,
        );
    }, []);
    const valor = useMemo(
        () => ({ api, libres, cargarLibres, quitar }),
        [api, libres, cargarLibres, quitar],
    );
    return <Contexto.Provider value={valor}>{children}</Contexto.Provider>;
}
