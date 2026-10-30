import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { NuevoRol } from "./nuevo-rol";

export const metadata: Metadata = { title: "Nuevo rol" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["roles.crear"]}>
            <NuevoRol />
        </RequierePermiso>
    );
}
