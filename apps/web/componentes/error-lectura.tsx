"use client";

import { Aviso, Boton, EstadoVacio, Tarjeta } from "@iris/ui";
import { Building2, RotateCw } from "lucide-react";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { useSesionOpcional } from "@/lib/sesion";

/** Sin empresas asignadas (403 `sin_empresa`): no es un fallo que se arregle reintentando. */
export function SinEmpresa() {
    return (
        <Tarjeta>
            <EstadoVacio
                icono={Building2}
                titulo="No tienes empresas asignadas"
                descripcion="Para ver y atender datos operativos necesitas al menos una empresa. Pide a un administrador de Iris que te asigne la empresa en que trabajas."
            />
        </Tarjeta>
    );
}

/** Error visible de una lectura, con «Reintentar». Nunca se muestran datos parciales debajo. */
export function ErrorLectura({ error, reintentar, que }: { error: unknown; reintentar: () => void; que: string }) {
    const sesion = useSesionOpcional();
    const codigo = error instanceof ErrorApi ? error.codigo : undefined;
    if (codigo === "sin_empresa") {
        return <SinEmpresa />;
    }
    if (codigo === "empresa_requerida" && sesion) {
        return (
            <Aviso
                tono="warning"
                titulo="Elige una empresa"
                icono={Building2}
                accion={
                    <Boton variante="secundario" tamano="sm" onClick={sesion.pedirEmpresa}>
                        <Building2 />
                        Elegir empresa
                    </Boton>
                }
            >
                Trabajas en varias empresas: elige con cuál vas a operar para ver {que}.
            </Aviso>
        );
    }
    const { titulo, detalle } = mensajeDeError(error);
    const sinConexion = codigo === "sin_conexion" || codigo === "api_no_disponible";
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
