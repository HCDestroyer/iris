import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISO_CONEXION_META } from "@/lib/meta/tipos";
import { ConexionConMeta } from "./conexion";

export const metadata: Metadata = { title: "Conexión con Meta" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_CONEXION_META]}>
            <ConexionConMeta />
        </RequierePermiso>
    );
}
