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
    const [n, setN] = useState<number | undefined>(undefined);
    useEffect(() => {
        setN(undefined);
        if (!activo) return;
        const control = new AbortController();
        const leer = () =>
            leerContadores(control.signal).then(
                (c) => !control.signal.aborted && setN(c.noLeidosMias),
                () => !control.signal.aborted && setN(undefined),
            );
        void leer();
        const id = window.setInterval(() => document.visibilityState === "visible" && void leer(), INTERVALO_MS);
        return () => {
            control.abort();
            window.clearInterval(id);
        };
    }, [activo, clave]);
    return n || undefined;
}
