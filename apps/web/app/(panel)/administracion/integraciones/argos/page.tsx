import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { EstadoArgos } from "./estado-argos";

export const metadata: Metadata = { title: "Conexión con Argos" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["integraciones.ver"]}>
            <EstadoArgos />
        </RequierePermiso>
    );
}
