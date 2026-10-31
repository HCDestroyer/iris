"use client";

import { Boton, ErrorFormulario } from "@iris/ui";
import { RotateCw } from "lucide-react";
import { mensajeDeError } from "@/lib/api/errores";
import { errorDeCliente } from "@/lib/clientes/errores";

/**
 * Error del servidor al crear un ticket u orden con cliente de Argos: los de la verificación del
 * cliente (otra empresa, Argos caído, cuota) en palabras claras y con «Reintentar» (vuelve a
 * enviar el formulario) cuando sirve; el resto, título y detalle del problem+json.
 */
export function ErrorAlta({ error, que }: { error: unknown; que: string }) {
    const cliente = errorDeCliente(error);
    if (cliente) {
        return (
            <ErrorFormulario
                titulo={cliente.titulo}
                detalle={cliente.detalle}
                accion={
                    cliente.reintentable ? (
                        <Boton type="submit" variante="secundario" tamano="sm">
                            <RotateCw />
                            Reintentar
                        </Boton>
                    ) : undefined
                }
            />
        );
    }
    const m = mensajeDeError(error);
    return <ErrorFormulario titulo={`No se pudo crear ${que}`} detalle={[m.titulo, m.detalle].filter(Boolean).join(". ")} />;
}

/** Mensaje junto al selector de cliente cuando el error es del cliente elegido. */
export function errorEnCliente(error: unknown): string | undefined {
    return errorDeCliente(error)?.enCliente ? "Este cliente o servicio no es de la empresa activa: elige otro." : undefined;
}
