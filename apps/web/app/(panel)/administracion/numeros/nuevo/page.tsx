import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { NuevoNumero } from "./nuevo";

export const metadata: Metadata = { title: "Agregar número" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["mensajeria.configurar"]}>
            <NuevoNumero />
        </RequierePermiso>
    );
}
