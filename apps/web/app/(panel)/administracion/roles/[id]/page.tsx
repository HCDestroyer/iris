import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { DetalleRol } from "./detalle-rol";

export const metadata: Metadata = { title: "Rol" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["roles.ver"]}>
            <DetalleRol />
        </RequierePermiso>
    );
}
