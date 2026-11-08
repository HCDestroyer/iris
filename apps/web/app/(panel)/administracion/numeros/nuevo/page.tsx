import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { ElegirConexion } from "./elegir";

export const metadata: Metadata = { title: "Agregar número" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["mensajeria.configurar"]}>
            <ElegirConexion />
        </RequierePermiso>
    );
}
