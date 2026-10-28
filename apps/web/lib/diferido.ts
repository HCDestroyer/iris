"use client";

import { useEffect, useState } from "react";

/** Valor que se actualiza `ms` después del último cambio (búsquedas: no pedir en cada tecla). */
export function useDiferido<T>(valor: T, ms: number): T {
    const [diferido, setDiferido] = useState(valor);
    useEffect(() => {
        const t = setTimeout(() => setDiferido(valor), ms);
        return () => clearTimeout(t);
    }, [valor, ms]);
    return diferido;
}
