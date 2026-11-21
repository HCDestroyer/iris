import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISO_VER } from "@/lib/plantillas-whatsapp/tipos";
import { SugeridasPWA } from "../sugeridas";

export const metadata: Metadata = { title: "Plantillas sugeridas" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_VER]}>
            <Suspense>
                <SugeridasPWA />
            </Suspense>
        </RequierePermiso>
    );
}
