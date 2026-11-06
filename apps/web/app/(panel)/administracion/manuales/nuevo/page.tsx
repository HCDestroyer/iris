import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { FormularioManual } from "../formulario";

export const metadata: Metadata = { title: "Nuevo manual" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["manuales.editar"]}>
            <FormularioManual />
        </RequierePermiso>
    );
}
