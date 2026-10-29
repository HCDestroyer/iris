import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { Reportes } from "./reportes";

export const metadata: Metadata = { title: "Reportes" };

export default function PaginaReportes() {
    return (
        <RequierePermiso permisos={["reportes.ver"]}>
            <Suspense>
                <Reportes />
            </Suspense>
        </RequierePermiso>
    );
}
