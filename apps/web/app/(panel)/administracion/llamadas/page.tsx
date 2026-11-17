import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { AjustesDeLlamadas } from "./ajustes";

export const metadata: Metadata = { title: "Ajustes de llamadas" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["voz.configurar"]}>
            <AjustesDeLlamadas />
        </RequierePermiso>
    );
}
