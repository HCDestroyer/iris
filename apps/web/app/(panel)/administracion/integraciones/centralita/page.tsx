import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISO_ASIGNAR_EXTENSION, PERMISO_CONFIGURAR_CENTRALITA } from "@/lib/centralita/tipos";
import { PaginaCentralita } from "./centralita";

export const metadata: Metadata = { title: "Centralita" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_CONFIGURAR_CENTRALITA, PERMISO_ASIGNAR_EXTENSION]}>
            <PaginaCentralita />
        </RequierePermiso>
    );
}
