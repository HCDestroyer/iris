import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { HistorialAccesos } from "./historial-accesos";

export const metadata: Metadata = { title: "Accesos" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["accesos.ver"]}>
            <Suspense>
                <HistorialAccesos />
            </Suspense>
        </RequierePermiso>
    );
}
