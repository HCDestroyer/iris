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
    /**
     * Lee de nuevo sin pasar por «cargando»: los datos actuales siguen a la vista hasta que llega la
     * respuesta. `silencioso` (relecturas de fondo: al volver a la pestaña, por un aviso): si falla,
     * lo que hay en pantalla se queda; no lanza otra si ya hay una en curso; y su resultado se
     * descarta si mientras tanto hubo un `reintentar`, otro `refrescar` o un `reemplazar` (lo que
     * trajo una acción es más nuevo).
     */
    refrescar: (opciones?: { silencioso?: boolean }) => Promise<void>;
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
    const deFondo = useRef<AbortController | null>(null);
    const soltarDeFondo = useCallback(() => {
        deFondo.current?.abort();
        deFondo.current = null;
    }, []);

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

    useEffect(
        () => () => {
            refresco.current?.abort();
            deFondo.current?.abort();
        },
        [],
    );

    const reintentar = useCallback(() => {
        refresco.current?.abort();
        soltarDeFondo();
        setEstado({ estado: "cargando" });
        setIntento((n) => n + 1);
    }, [soltarDeFondo]);

    const refrescar = useCallback(async (opciones?: { silencioso?: boolean }) => {
        if (opciones?.silencioso) {
            if (deFondo.current) return;
            const control = new AbortController();
            deFondo.current = control;
            try {
                const datos = await leerRef.current(control.signal);
                // Solo sobre una lectura buena: nunca convierte un error o una carga en datos por detrás.
                if (!control.signal.aborted) setEstado((actual) => (actual.estado === "listo" ? { estado: "listo", datos } : actual));
            } catch {
                // De fondo: lo que hay en pantalla se queda.
            } finally {
                if (deFondo.current === control) deFondo.current = null;
            }
            return;
        }
        refresco.current?.abort();
        soltarDeFondo();
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
    }, [soltarDeFondo]);

    const reemplazar = useCallback((datos: T) => {
        // Lo que trajo una acción es más nuevo que una relectura de fondo que salió antes.
        soltarDeFondo();
        setEstado({ estado: "listo", datos });
    }, [soltarDeFondo]);

    return { ...estado, reintentar, refrescar, reemplazar };
}
