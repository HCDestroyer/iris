import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { Comercial } from "./comercial";

export const metadata: Metadata = { title: "Comercial" };

export default function PaginaComercial() {
    return (
        <RequierePermiso permisos={["prospectos.ver"]}>
            <Suspense>
                <Comercial />
            </Suspense>
        </RequierePermiso>
    );
}
