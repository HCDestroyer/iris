"use client";

import { useEffect, useState } from "react";

/** El valor después de que deja de cambiar `ms` (búsqueda en vivo). Vacío pasa en el acto. */
export function useDemorado<T>(valor: T, ms: number): T {
    const [demorado, setDemorado] = useState(valor);
    useEffect(() => {
        const t = setTimeout(() => setDemorado(valor), valor === "" ? 0 : ms);
        return () => clearTimeout(t);
    }, [valor, ms]);
    return demorado;
}
