"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import type { ErrorApi } from "./api/errores";

export type EstadoLectura<T> =
    | { estado: "cargando" }
    | { estado: "error"; error: ErrorApi | Error }
    | { estado: "listo"; datos: T };

/**
 * Lee un recurso al montar y expone `reintentar`. Mientras carga o si falló no hay datos: la
 * pantalla nunca calcula nada sobre una lectura fallida.
 */
export function useLectura<T>(leer: (senal: AbortSignal) => Promise<T>): EstadoLectura<T> & { reintentar: () => void } {
    const [estado, setEstado] = useState<EstadoLectura<T>>({ estado: "cargando" });
    const [intento, setIntento] = useState(0);
    const leerRef = useRef(leer);

    useEffect(() => {
        leerRef.current = leer;
    });

    useEffect(() => {
        const control = new AbortController();
        leerRef.current(control.signal).then(
            (datos) => {
                if (!control.signal.aborted) {
                    setEstado({ estado: "listo", datos });
                }
            },
            (error: unknown) => {
                if (!control.signal.aborted) {
                    setEstado({ estado: "error", error: error instanceof Error ? error : new Error(String(error)) });
                }
            },
        );
        return () => control.abort();
    }, [intento]);

    const reintentar = useCallback(() => {
        setEstado({ estado: "cargando" });
        setIntento((n) => n + 1);
    }, []);

    return { ...estado, reintentar };
}
