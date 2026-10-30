import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { ListaRoles } from "./lista-roles";

export const metadata: Metadata = { title: "Roles y permisos" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["roles.ver"]}>
            <ListaRoles />
        </RequierePermiso>
    );
}
