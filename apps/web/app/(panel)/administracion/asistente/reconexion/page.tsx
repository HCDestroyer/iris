import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISO_CONFIGURAR_ASISTENTE } from "@/lib/asistente/permisos";
import { ReconexionAsistente } from "./formulario";

export const metadata: Metadata = { title: "Reconexión temporal" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_CONFIGURAR_ASISTENTE]}>
            <ReconexionAsistente />
        </RequierePermiso>
    );
}
