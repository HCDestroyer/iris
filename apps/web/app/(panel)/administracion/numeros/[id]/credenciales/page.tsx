import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { CredencialesNumero } from "./credenciales";

export const metadata: Metadata = { title: "Credenciales del número" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["mensajeria.configurar"]}>
            <CredencialesNumero />
        </RequierePermiso>
    );
}
