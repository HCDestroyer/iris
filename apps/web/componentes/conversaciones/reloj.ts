"use client";

import { useEffect, useState } from "react";

/** La hora actual, que se renueva cada `ms` (horas de la lista y tiempo de la ventana de 24 h). */
export function useAhora(ms: number): number {
    const [ahora, setAhora] = useState(() => Date.now());
    useEffect(() => {
        const t = setInterval(() => setAhora(Date.now()), ms);
        return () => clearInterval(t);
    }, [ms]);
    return ahora;
}

/** Ancho de un elemento, al día con `ResizeObserver` (0 hasta medirlo). */
export function useAncho<T extends HTMLElement>(): [(el: T | null) => void, number] {
    const [ancho, setAncho] = useState(0);
    const [el, setEl] = useState<T | null>(null);
    useEffect(() => {
        if (!el) return;
        const obs = new ResizeObserver(([e]) => setAncho(Math.round(e?.contentRect.width ?? 0)));
        obs.observe(el);
        return () => obs.disconnect();
    }, [el]);
    return [setEl, ancho];
}
