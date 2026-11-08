import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { ListaNumeros } from "./lista";

export const metadata: Metadata = { title: "Números de WhatsApp" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["canales.configurar", "mensajeria.configurar"]}>
            <ListaNumeros />
        </RequierePermiso>
    );
}
