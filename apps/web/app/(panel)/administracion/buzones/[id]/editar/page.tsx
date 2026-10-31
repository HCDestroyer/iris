import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { EditarBuzon } from "./editar-buzon";

export const metadata: Metadata = { title: "Editar buzón" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["buzones.editar"]}>
            <EditarBuzon />
        </RequierePermiso>
    );
}
