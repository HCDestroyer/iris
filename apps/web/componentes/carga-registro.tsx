"use client";

import { Esqueleto, EstadoVacio, Tarjeta } from "@iris/ui";
import { SearchX } from "lucide-react";
import type { ReactNode } from "react";
import { ErrorLectura } from "@/componentes/error-lectura";
import { ErrorApi } from "@/lib/api/errores";
import type { EstadoLectura } from "@/lib/lectura";

/**
 * Lectura del registro de una pantalla de formulario (editar, cotización…): esqueleto mientras
 * carga, «no encontrado» con un 404 o un id inválido (`null`), error con «Reintentar», y el
 * formulario cuando ya está.
 */
export function CargaRegistro<T>({
    lectura,
    que,
    noEncontrado,
    children,
}: {
    lectura: EstadoLectura<T | null> & { reintentar: () => void };
    /** «el usuario», «la ficha del técnico»… */
    que: string;
    noEncontrado: string;
    children: (datos: T) => ReactNode;
}) {
    if (lectura.estado === "cargando") {
        return <CargandoRegistro que={que} />;
    }
    if ((lectura.estado === "error" && lectura.error instanceof ErrorApi && lectura.error.estado === 404) || (lectura.estado === "listo" && lectura.datos === null)) {
        return (
            <Tarjeta>
                <EstadoVacio icono={SearchX} titulo={noEncontrado} descripcion="No existe o no está en tu alcance." />
            </Tarjeta>
        );
    }
    if (lectura.estado === "error") {
        return <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que={que} />;
    }
    return lectura.datos === null ? null : children(lectura.datos);
}

/** Esqueleto de una pantalla de formulario mientras se lee lo que necesita. */
export function CargandoRegistro({ que }: { que: string }) {
    return (
        <div className="mx-auto grid w-full max-w-3xl gap-4" aria-busy="true">
            <span className="sr-only" role="status">
                Cargando {que}…
            </span>
            <Esqueleto className="h-8 w-64" />
            <Esqueleto className="h-48" />
        </div>
    );
}
