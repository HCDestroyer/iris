import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { FormularioBuzon } from "../formulario-buzon";

export const metadata: Metadata = { title: "Nuevo buzón" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["buzones.crear"]}>
            <FormularioBuzon />
        </RequierePermiso>
    );
}
