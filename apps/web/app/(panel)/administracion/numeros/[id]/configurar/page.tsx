import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { ConfigurarNumero } from "./configurar";

export const metadata: Metadata = { title: "Configurar número" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["canales.configurar"]}>
            <ConfigurarNumero />
        </RequierePermiso>
    );
}
