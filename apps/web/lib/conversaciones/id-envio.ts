"use client";

import { useMemo, useRef } from "react";
import { ErrorApi } from "../api/errores";
import { nuevoIdMensaje } from "./api";
import { esSesionCambiada } from "./sesion";

/**
 * Id de un envío a la bandeja (mensaje, nota, plantilla o manual). Es a la vez el `id` del cuerpo y
 * la `Idempotency-Key`, así que los dos cambian juntos.
 *
 * - Sin respuesta (`estado === 0`) o con la sesión cambiada antes de enviar: se repite el mismo id.
 *   Si la primera petición sí llegó, el servidor no duplica nada.
 * - Con respuesta (4xx o 5xx): el servidor guardó ese resultado con esa clave, y otro cuerpo con la
 *   misma clave daría `idempotencia_reutilizada`. El siguiente intento lleva un id nuevo.
 */
export function idTrasError(error: unknown, actual: string, nuevo: () => string = nuevoIdMensaje): string {
    if (esSesionCambiada(error)) return actual;
    return error instanceof ErrorApi && error.estado !== 0 ? nuevo() : actual;
}

export type IdEnvio = {
    /** Id del intento actual. */
    actual: () => string;
    /** El envío terminó bien: el próximo es otro mensaje. */
    renovar: () => void;
    /** El envío falló: ver `idTrasError`. */
    trasError: (error: unknown) => void;
};

/** Id de envío de un formulario, para usar desde manejadores (nunca durante el render). */
export function useIdEnvio(): IdEnvio {
    const id = useRef<string | null>(null);
    return useMemo(
        () => ({
            actual: () => (id.current ??= nuevoIdMensaje()),
            renovar: () => {
                id.current = nuevoIdMensaje();
            },
            trasError: (error: unknown) => {
                id.current = idTrasError(error, id.current ?? nuevoIdMensaje());
            },
        }),
        [],
    );
}
