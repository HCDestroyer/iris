"use client";

import { avisar, Aviso, DialogoConfirmar } from "@iris/ui";
import { useState, type ReactNode } from "react";
import { mensajeDeError } from "@/lib/api/errores";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { actualizarCanal, cuerpoActualizar } from "@/lib/numeros/api";
import type { Canal } from "@/lib/numeros/tipos";

export type GenerarVerifyToken = {
    /** Abre la confirmación. */
    pedir: () => void;
    generando: boolean;
    /** Error de la última vez (con su aviso listo para la pantalla). */
    error: ReactNode;
    /** Diálogo de confirmación: va una vez en la pantalla. */
    dialogo: ReactNode;
};

/**
 * «Generar otro» token de verificación (`POST /canales/{id}/actualizar` con `generarVerifyToken`):
 * Iris lo devuelve una sola vez en `verifyTokenGenerado`. Cambiarlo borra la verificación del
 * webhook, así que pregunta antes. `alGenerado` recibe el número actualizado (nueva `version`) y el
 * token, que la pantalla muestra y no guarda en ningún otro lado.
 */
export function useGenerarVerifyToken(canal: Canal, alGenerado: (canal: Canal, token: string) => void): GenerarVerifyToken {
    const clave = useClaveIdempotencia();
    const [abierto, setAbierto] = useState(false);
    const [generando, setGenerando] = useState(false);
    const [error, setError] = useState<unknown>(null);

    async function generar() {
        setGenerando(true);
        setError(null);
        try {
            const nuevo = await actualizarCanal(canal.id, cuerpoActualizar(canal, { generarVerifyToken: true }), clave.actual());
            clave.renovar();
            setAbierto(false);
            if (nuevo.verifyTokenGenerado) {
                alGenerado(nuevo, nuevo.verifyTokenGenerado);
                avisar({ titulo: "Token de verificación nuevo: pégalo en Meta" });
            } else {
                alGenerado(nuevo, "");
            }
        } catch (e) {
            clave.trasError(e);
            setAbierto(false);
            setError(e);
        } finally {
            setGenerando(false);
        }
    }

    return {
        pedir: () => setAbierto(true),
        generando,
        error: error ? (
            <Aviso tono="error" titulo="No se pudo generar otro token">
                {mensajeDeError(error).titulo}. El de antes sigue valiendo. Vuelve a intentarlo con «Generar otro».
            </Aviso>
        ) : null,
        dialogo: (
            <DialogoConfirmar
                open={abierto}
                onOpenChange={setAbierto}
                titulo="¿Generar otro token de verificación?"
                descripcion="El de ahora deja de valer y el webhook queda sin verificar hasta que pegues el nuevo en Meta y pulses «Verificar y guardar»."
                textoConfirmar="Generar otro"
                textoOcupado="Generando…"
                alConfirmar={generar}
            />
        ),
    };
}
