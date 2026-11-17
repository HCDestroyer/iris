import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { AvisosServidorIa } from "@/componentes/servidor-ia/avisos";
import { PERMISO_ADMINISTRAR } from "@/lib/servidor-ia/tipos";

export const metadata: Metadata = { title: "Avisos del servidor de IA" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_ADMINISTRAR]}>
            <AvisosServidorIa />
        </RequierePermiso>
    );
}
