import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { EnlazarServidorIa } from "@/componentes/servidor-ia/enlazar";
import { PERMISO_ADMINISTRAR } from "@/lib/servidor-ia/tipos";

export const metadata: Metadata = { title: "Enlazar el servidor de IA" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_ADMINISTRAR]}>
            <EnlazarServidorIa />
        </RequierePermiso>
    );
}
