import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { ListaUsuarios } from "./lista-usuarios";

export const metadata: Metadata = { title: "Usuarios" };

export default function PaginaUsuarios() {
    return (
        <RequierePermiso permisos={["usuarios.ver"]}>
            <ListaUsuarios />
        </RequierePermiso>
    );
}
