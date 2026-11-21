import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISO_EDITAR } from "@/lib/plantillas-whatsapp/tipos";
import { CargarEdicion } from "../../cargar";

export const metadata: Metadata = { title: "Editar plantilla de WhatsApp" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_EDITAR]}>
            <Suspense>
                <CargarEdicion />
            </Suspense>
        </RequierePermiso>
    );
}
