import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISO_CONFIGURAR_CENTRALITA } from "@/lib/centralita/tipos";
import { ConexionCentralita } from "./formulario";

export const metadata: Metadata = { title: "Conexión con Issabel" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_CONFIGURAR_CENTRALITA]}>
            <ConexionCentralita />
        </RequierePermiso>
    );
}
