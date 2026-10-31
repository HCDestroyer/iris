import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { DetalleProspecto } from "./detalle";

export const metadata: Metadata = { title: "Prospecto" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["prospectos.ver"]}>
            {/* La pestaña elegida va en la URL (useSearchParams). */}
            <Suspense>
                <DetalleProspecto />
            </Suspense>
        </RequierePermiso>
    );
}
