"use client";

import { Aviso, Boton } from "@iris/ui";
import { RotateCw } from "lucide-react";
import { ErrorApi } from "@/lib/api/errores";

/** Error de una lectura del portal, en lenguaje del cliente y con «Reintentar». Nunca con datos a medias. */
export function ErrorPortal({ error, reintentar, que }: { error: unknown; reintentar: () => void; que: string }) {
    const sinConexion = error instanceof ErrorApi && (error.codigo === "sin_conexion" || error.codigo === "api_no_disponible");
    return (
        <Aviso
            tono="error"
            titulo={`No pudimos cargar ${que}`}
            accion={
                <Boton variante="secundario" tamano="sm" onClick={reintentar}>
                    <RotateCw />
                    Reintentar
                </Boton>
            }
        >
            {sinConexion ? "No hay conexión. Revisa tu internet e inténtalo de nuevo." : "Tuvimos un problema. Inténtalo de nuevo en un momento."}
        </Aviso>
    );
}
