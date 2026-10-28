"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import type { ErrorApi } from "./api/errores";

export type EstadoLectura<T> =
    | { estado: "cargando" }
    | { estado: "error"; error: ErrorApi | Error }
    | { estado: "listo"; datos: T };

export type Lectura<T> = EstadoLectura<T> & {
    /** Vuelve a «cargando» y lee de nuevo. */
    reintentar: () => void;
    /** Lee de nuevo sin pasar por «cargando»: los datos actuales siguen a la vista hasta que llega la respuesta. */
    refrescar: () => Promise<void>;
    /** Reemplaza los datos con lo que devolvió una acción (p. ej. el ticket tras escalar). */
    reemplazar: (datos: T) => void;
};

const comoError = (error: unknown) => (error instanceof Error ? error : new Error(String(error)));

/**
 * Lee un recurso al montar y expone `reintentar`. Mientras carga o si falló no hay datos: la
 * pantalla nunca calcula nada sobre una lectura fallida.
 */
export function useLectura<T>(leer: (senal: AbortSignal) => Promise<T>): Lectura<T> {
    const [estado, setEstado] = useState<EstadoLectura<T>>({ estado: "cargando" });
    const [intento, setIntento] = useState(0);
    const leerRef = useRef(leer);
    const refresco = useRef<AbortController | null>(null);

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
                    setEstado({ estado: "error", error: comoError(error) });
                }
            },
        );
        return () => control.abort();
    }, [intento]);

    useEffect(() => () => refresco.current?.abort(), []);

    const reintentar = useCallback(() => {
        refresco.current?.abort();
        setEstado({ estado: "cargando" });
        setIntento((n) => n + 1);
    }, []);

    const refrescar = useCallback(async () => {
        refresco.current?.abort();
        const control = new AbortController();
        refresco.current = control;
        try {
            const datos = await leerRef.current(control.signal);
            if (!control.signal.aborted) {
                setEstado({ estado: "listo", datos });
            }
        } catch (error) {
            if (!control.signal.aborted) {
                setEstado({ estado: "error", error: comoError(error) });
            }
        }
    }, []);

    const reemplazar = useCallback((datos: T) => setEstado({ estado: "listo", datos }), []);

    return { ...estado, reintentar, refrescar, reemplazar };
}
