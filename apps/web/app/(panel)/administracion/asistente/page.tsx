import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISO_CONFIGURAR_ASISTENTE } from "@/lib/asistente/permisos";
import { AsistentePrincipal } from "./principal";

export const metadata: Metadata = { title: "Asistente de IA" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_CONFIGURAR_ASISTENTE]}>
            <AsistentePrincipal />
        </RequierePermiso>
    );
}
