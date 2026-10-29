import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { DetallePlantilla } from "./detalle-plantilla";

export const metadata: Metadata = { title: "Plantilla" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["plantillas.ver"]}>
            <Suspense>
                <DetallePlantilla />
            </Suspense>
        </RequierePermiso>
    );
}
