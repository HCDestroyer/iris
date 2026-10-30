import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { DetalleUsuario } from "./detalle-usuario";

export const metadata: Metadata = { title: "Usuario" };

export default function PaginaUsuario() {
    return (
        <RequierePermiso permisos={["usuarios.ver"]}>
            <DetalleUsuario />
        </RequierePermiso>
    );
}
