"use client";

import { Aviso } from "@iris/ui";
import { Clock } from "lucide-react";
import { ErrorApi } from "@/lib/api/errores";

/** Error al enviar: el tope de casos (429) se explica con el `detail` de la API. */
export function ErrorEnvio({ error }: { error: unknown }) {
    if (error instanceof ErrorApi && error.estado === 429) {
        return (
            <Aviso tono="warning" icono={Clock} titulo={error.codigo === "demasiados_tickets" ? "Ya tienes varios casos en curso" : "Enviaste muchos mensajes seguidos"}>
                {error.detalle ?? "Espera un momento antes de volver a intentarlo."}
            </Aviso>
        );
    }
    const sinConexion = error instanceof ErrorApi && (error.codigo === "sin_conexion" || error.codigo === "api_no_disponible");
    return (
        <Aviso tono="error" titulo="No pudimos enviar">
            {sinConexion
                ? "No hay conexión. Revisa tu internet y vuelve a intentarlo: no se perdió lo que escribiste."
                : error instanceof ErrorApi && error.detalle
                  ? error.detalle
                  : "Tuvimos un problema. Inténtalo de nuevo en un momento."}
        </Aviso>
    );
}
