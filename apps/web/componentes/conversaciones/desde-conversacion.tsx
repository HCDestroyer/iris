"use client";

import { useParams } from "next/navigation";
import type { ReactNode } from "react";
import { CargaRegistro } from "@/componentes/carga-registro";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { leerDetalle } from "@/lib/conversaciones/api";
import type { ConversacionDetalle } from "@/lib/conversaciones/tipos";
import { useLectura } from "@/lib/lectura";
import type { Permiso } from "@/lib/menu";
import { idDeParametro } from "@/lib/parametros";

/**
 * Pantallas de formulario que salen de una conversación (`/conversaciones/[id]/orden`, `boleta`,
 * `prospecto`): el id de la URL se valida y la conversación se lee de la API (404 si no la ves).
 */
export function DesdeConversacion({ permisos, children }: { permisos: readonly Permiso[]; children: (c: ConversacionDetalle) => ReactNode }) {
    return (
        <RequierePermiso permisos={permisos}>
            <Cargar>{children}</Cargar>
        </RequierePermiso>
    );
}

function Cargar({ children }: { children: (c: ConversacionDetalle) => ReactNode }) {
    const id = idDeParametro(useParams<{ id: string }>().id);
    const lectura = useLectura(async (signal) => (id === null ? null : leerDetalle(id, signal)));
    return (
        <CargaRegistro lectura={lectura} que="la conversación" noEncontrado="Conversación no encontrada">
            {children}
        </CargaRegistro>
    );
}
