"use client";

import { Aviso, Boton } from "@iris/ui";
import { RotateCw } from "lucide-react";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";

/** Error visible de una lectura, con «Reintentar». Nunca se muestran datos parciales debajo. */
export function ErrorLectura({ error, reintentar, que }: { error: unknown; reintentar: () => void; que: string }) {
    const { titulo, detalle } = mensajeDeError(error);
    const sinConexion = error instanceof ErrorApi && (error.codigo === "sin_conexion" || error.codigo === "api_no_disponible");
    return (
        <Aviso
            tono="error"
            titulo={`No se pudo cargar ${que}`}
            accion={
                <Boton variante="secundario" tamano="sm" onClick={reintentar}>
                    <RotateCw />
                    Reintentar
                </Boton>
            }
        >
            {sinConexion ? "No hay conexión con Iris. Revisa tu red e inténtalo de nuevo." : [titulo, detalle].filter(Boolean).join(". ")}
        </Aviso>
    );
}
