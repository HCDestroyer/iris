import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { Integraciones } from "./integraciones";

export const metadata: Metadata = { title: "Integraciones" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["integraciones.ver"]}>
            <Integraciones />
        </RequierePermiso>
    );
}
