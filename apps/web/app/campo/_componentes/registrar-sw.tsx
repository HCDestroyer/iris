"use client";

import { useEffect } from "react";

/**
 * Registra el service worker (Serwist, generado en el build en /sw.js) con alcance /campo: el
 * shell queda en caché para abrir sin señal. En desarrollo no hay /sw.js y no se registra.
 */
export function RegistrarSw() {
    useEffect(() => {
        if (process.env.NODE_ENV !== "production" || !("serviceWorker" in navigator)) {
            return;
        }
        navigator.serviceWorker.register("/sw.js", { scope: "/campo" }).catch(() => {
            // Sin service worker la app funciona igual con señal; sin señal no abriría de cero.
        });
    }, []);
    return null;
}
