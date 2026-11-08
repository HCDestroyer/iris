import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { ConectarNumero } from "./conectar";

export const metadata: Metadata = { title: "Conectar número" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["mensajeria.configurar"]}>
            <ConectarNumero />
        </RequierePermiso>
    );
}
