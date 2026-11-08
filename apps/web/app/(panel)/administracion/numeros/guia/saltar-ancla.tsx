"use client";

import { useEffect } from "react";

/**
 * Lleva a la sección del `#ancla` al abrir la guía. El navegador lo hace solo cuando la sección
 * ya está en el HTML; aquí llega después (la página espera la sesión para ver los permisos).
 */
export function SaltarAncla() {
    useEffect(() => {
        let id: string;
        try {
            id = decodeURIComponent(window.location.hash.slice(1));
        } catch {
            // `#%E0…` mal formado: no hay ancla a la que ir.
            return;
        }
        if (!id) return;
        document.getElementById(id)?.scrollIntoView({ block: "start" });
    }, []);
    return null;
}
