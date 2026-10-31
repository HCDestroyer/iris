import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISOS_TECNICOS } from "@/lib/agenda/permisos";
import { FichaTecnico } from "./ficha-tecnico";

export const metadata: Metadata = { title: "Técnico" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={PERMISOS_TECNICOS}>
            {/* La pestaña elegida va en la URL (useSearchParams). */}
            <Suspense>
                <FichaTecnico />
            </Suspense>
        </RequierePermiso>
    );
}
