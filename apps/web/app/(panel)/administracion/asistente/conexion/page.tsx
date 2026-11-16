import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISO_CONFIGURAR_ASISTENTE } from "@/lib/asistente/permisos";
import { ConexionAsistente } from "./formulario";

export const metadata: Metadata = { title: "Conexión con la IA" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_CONFIGURAR_ASISTENTE]}>
            <ConexionAsistente />
        </RequierePermiso>
    );
}
