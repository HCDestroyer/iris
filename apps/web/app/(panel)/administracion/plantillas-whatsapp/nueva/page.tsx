import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISO_CREAR } from "@/lib/plantillas-whatsapp/tipos";
import { CargarNueva } from "../cargar";

export const metadata: Metadata = { title: "Nueva plantilla de WhatsApp" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_CREAR]}>
            <Suspense>
                <CargarNueva />
            </Suspense>
        </RequierePermiso>
    );
}
