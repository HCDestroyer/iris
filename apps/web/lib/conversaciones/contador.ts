"use client";

import { useEffect, useState } from "react";
import { leerContadores } from "./api";

/** Cada cuánto se relee el contador de «Conversaciones» en la barra lateral. */
const INTERVALO_MS = 60_000;

/**
 * Mensajes de clientes sin leer en tus conversaciones (`noLeidosMias` de
 * `GET /conversaciones/contadores`), para el contador de la barra lateral. Sin permiso, o si la
 * lectura falló, no hay número (nunca un cero inventado). Se relee cada minuto con la pestaña a la
 * vista y al cambiar de empresa (`clave`).
 */
export function useNoLeidosConversaciones(activo: boolean, clave: string): number | undefined {
    // Lo leído lleva su clave: al cambiar de empresa no se muestra el número de la anterior.
    const [leido, setN] = useState<{ clave: string; n: number | undefined }>({ clave: "", n: undefined });
    useEffect(() => {
        if (!activo) return;
        const control = new AbortController();
        const leer = () =>
            leerContadores(control.signal).then(
                (c) => !control.signal.aborted && setN({ clave, n: c.noLeidosMias }),
                () => !control.signal.aborted && setN({ clave, n: undefined }),
            );
        void leer();
        const id = window.setInterval(() => document.visibilityState === "visible" && void leer(), INTERVALO_MS);
        return () => {
            control.abort();
            window.clearInterval(id);
        };
    }, [activo, clave]);
    return (activo && leido.clave === clave && leido.n) || undefined;
}
