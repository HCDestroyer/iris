import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { NuevaOrden } from "./nueva-orden";

export const metadata: Metadata = { title: "Nueva orden" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["ordenes.crear"]}>
            <Suspense>
                <NuevaOrden />
            </Suspense>
        </RequierePermiso>
    );
}
