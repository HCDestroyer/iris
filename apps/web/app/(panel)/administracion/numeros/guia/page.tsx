import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { GuiaWhatsApp } from "./guia";

export const metadata: Metadata = { title: "Cómo conectar WhatsApp" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["mensajeria.configurar", "canales.configurar"]}>
            <GuiaWhatsApp />
        </RequierePermiso>
    );
}
