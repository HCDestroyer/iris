import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PaginaServidorIa } from "@/componentes/servidor-ia/pagina";
import { PERMISO_VER, PERMISO_VER_USO } from "@/lib/servidor-ia/tipos";

export const metadata: Metadata = { title: "Servidor de IA" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_VER, PERMISO_VER_USO]}>
            <PaginaServidorIa />
        </RequierePermiso>
    );
}
