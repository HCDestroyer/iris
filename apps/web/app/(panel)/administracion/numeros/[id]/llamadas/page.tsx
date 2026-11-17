import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { LlamadasDelNumero } from "./llamadas";

export const metadata: Metadata = { title: "Llamadas del número" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["llamadas.configurar"]}>
            <LlamadasDelNumero />
        </RequierePermiso>
    );
}
