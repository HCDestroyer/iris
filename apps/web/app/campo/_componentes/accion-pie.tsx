"use client";

import { RegistroAccionPie, type AccionPie, type AccionPieRegistrada } from "@iris/sync/campo";
import { createContext, useContext, useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore, type ReactNode } from "react";

/*
 * La acción principal de las pantallas con firma («Firmar y cerrar orden», «Confirmar recepción»):
 * la pantalla la registra aquí y el marco la dibuja en una isla fija abajo (`IslaAccion`), siempre a
 * la vista, en lugar de la isla de menú o la de pasos (`RegistroAccionPie` de `@iris/sync/campo`).
 */

const Contexto = createContext<RegistroAccionPie | null>(null);

export function ProveedorAccionPie({ children }: { children: ReactNode }) {
    const [registro] = useState(() => new RegistroAccionPie());
    return <Contexto.Provider value={registro}>{children}</Contexto.Provider>;
}

function useRegistro(): RegistroAccionPie {
    const r = useContext(Contexto);
    if (!r) {
        throw new Error("useAccionPie fuera de ProveedorAccionPie");
    }
    return r;
}

/**
 * Pone la acción al pie mientras la pantalla está montada. `presionar` puede cambiar en cada dibujo
 * (se llama la última); la isla solo se vuelve a dibujar cuando cambia lo que muestra.
 */
export function useAccionPie(accion: AccionPie, presionar: () => void): void {
    const registro = useRegistro();
    const ultimo = useRef(presionar);
    useEffect(() => {
        ultimo.current = presionar;
    });
    const { texto, icono, ocupado, prueba } = accion;
    const volver = JSON.stringify(accion.volver);
    // Antes de pintar: la isla de menú no alcanza a verse un instante al abrir la pantalla.
    useLayoutEffect(
        () => registro.registrar({ texto, icono, ocupado, prueba, volver: JSON.parse(volver) as AccionPie["volver"] }, () => ultimo.current()),
        [registro, texto, icono, ocupado, prueba, volver],
    );
}

/** La acción al pie de la pantalla abierta, o `null`. */
export function useAccionPieActual(): AccionPieRegistrada | null {
    const r = useRegistro();
    return useSyncExternalStore(r.suscribir, r.obtener, r.obtener);
}
