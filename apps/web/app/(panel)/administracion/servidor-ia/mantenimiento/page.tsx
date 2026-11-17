import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { MantenimientoServidorIa } from "@/componentes/servidor-ia/mantenimiento";
import { PERMISO_ADMINISTRAR } from "@/lib/servidor-ia/tipos";

export const metadata: Metadata = { title: "Mantenimiento del servidor de IA" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_ADMINISTRAR]}>
            <MantenimientoServidorIa />
        </RequierePermiso>
    );
}
