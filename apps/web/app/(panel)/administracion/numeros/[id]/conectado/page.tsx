import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { NumeroConectado } from "./conectado";

export const metadata: Metadata = { title: "Número conectado" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["mensajeria.configurar"]}>
            <NumeroConectado />
        </RequierePermiso>
    );
}
