import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISO_VER } from "@/lib/plantillas-whatsapp/tipos";
import { ListaPlantillasWA } from "./lista";

export const metadata: Metadata = { title: "Plantillas de WhatsApp" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_VER]}>
            <Suspense>
                <ListaPlantillasWA />
            </Suspense>
        </RequierePermiso>
    );
}
