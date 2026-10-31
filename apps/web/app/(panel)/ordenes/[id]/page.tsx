import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { DetalleOrden } from "./detalle";

export const metadata: Metadata = { title: "Orden" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["ordenes.ver", "ordenes.ver_propias"]}>
            {/* La pestaña elegida va en la URL (useSearchParams). */}
            <Suspense>
                <DetalleOrden />
            </Suspense>
        </RequierePermiso>
    );
}
