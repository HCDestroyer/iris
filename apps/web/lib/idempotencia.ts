"use client";

import { useMemo, useRef } from "react";
import { claveTrasError, nuevaClaveIdempotencia } from "./api/cliente";

export type ClaveIdempotencia = {
    /** Clave del intento actual (se reusa al reintentar el mismo envío sin respuesta). */
    actual: () => string;
    /** El envío terminó bien: el próximo es otro intento lógico. */
    renovar: () => void;
    /** El envío falló: nueva clave si la API respondió (guardó el error), la misma si no hubo respuesta. */
    trasError: (error: unknown) => void;
};

/** `Idempotency-Key` de un formulario o acción, para usar desde manejadores (nunca durante el render). */
export function useClaveIdempotencia(): ClaveIdempotencia {
    const clave = useRef<string | null>(null);
    return useMemo(
        () => ({
            actual: () => (clave.current ??= nuevaClaveIdempotencia()),
            renovar: () => {
                clave.current = nuevaClaveIdempotencia();
            },
            trasError: (error: unknown) => {
                clave.current = claveTrasError(error, clave.current ?? nuevaClaveIdempotencia());
            },
        }),
        [],
    );
}
