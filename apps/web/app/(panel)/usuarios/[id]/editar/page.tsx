import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { EditarUsuario } from "./editar-usuario";

export const metadata: Metadata = { title: "Editar usuario" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["usuarios.editar"]}>
            <EditarUsuario />
        </RequierePermiso>
    );
}
