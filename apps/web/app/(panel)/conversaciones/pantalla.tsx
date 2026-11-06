"use client";

import { RequierePermiso } from "@/componentes/requiere-permiso";
import { Bandeja } from "@/componentes/conversaciones/bandeja";
import { useSesion } from "@/lib/sesion";

/** «Conversaciones» usa todo el ancho; sin permiso, el aviso de siempre con su margen. */
export function PantallaConversaciones() {
    const { puede } = useSesion();
    if (!puede(["conversaciones.ver"])) {
        return (
            <div className="p-4 sm:p-6">
                <RequierePermiso permisos={["conversaciones.ver"]}>{null}</RequierePermiso>
            </div>
        );
    }
    return <Bandeja />;
}
