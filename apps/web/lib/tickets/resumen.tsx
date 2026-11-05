"use client";

import type { ResumenTickets } from "./contadores";
import { createContext, useContext, useEffect, useRef, type ReactNode } from "react";
import { api, resolver } from "../api/cliente";
import { useLectura, type Lectura } from "../lectura";

export type { DiaTickets, ResumenTickets } from "./contadores";
export { contadorDe, slaCumplido, textoContador } from "./contadores";

/** Cada cuánto se vuelve a leer el resumen (contadores de la barra lateral, Inicio y vistas). */
export const INTERVALO_RESUMEN_MS = 60_000;

/** Sin `tickets.ver` no hay resumen: los contadores no aparecen (no son cero). */
export type ContextoResumen = Lectura<ResumenTickets> | null;

const Contexto = createContext<ContextoResumen>(null);

/**
 * Lee `GET /tickets/resumen` una vez para todo el panel (barra lateral, avisos, Inicio y las
 * pestañas de la bandeja) y lo refresca cada minuto y al cambiar de empresa (`clave`). Mientras
 * carga o si falló no hay números: nunca se muestra un contador de una lectura fallida.
 */
export function ProveedorResumenTickets({ activo, clave, children }: { activo: boolean; clave: string; children: ReactNode }) {
    return activo ? <ConResumen clave={clave}>{children}</ConResumen> : <Contexto.Provider value={null}>{children}</Contexto.Provider>;
}

function ConResumen({ clave, children }: { clave: string; children: ReactNode }) {
    const lectura = useLectura((signal) => resolver(api().GET("/api/v1/tickets/resumen", { signal })));
    const { reintentar, refrescar } = lectura;
    const primera = useRef(true);
    useEffect(() => {
        if (primera.current) {
            primera.current = false;
            return;
        }
        reintentar();
    }, [clave, reintentar]);
    useEffect(() => {
        const id = window.setInterval(() => {
            if (document.visibilityState === "visible") void refrescar();
        }, INTERVALO_RESUMEN_MS);
        return () => window.clearInterval(id);
    }, [refrescar]);
    return <Contexto.Provider value={lectura}>{children}</Contexto.Provider>;
}

/** El resumen del panel; `null` si el usuario no ve tickets (o fuera del panel). */
export function useResumenTickets(): ContextoResumen {
    return useContext(Contexto);
}
