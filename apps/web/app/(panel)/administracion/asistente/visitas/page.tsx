import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISO_CONFIGURAR_ASISTENTE } from "@/lib/asistente/permisos";
import { VisitasAsistente } from "./formulario";

export const metadata: Metadata = { title: "Cómo agenda visitas" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_CONFIGURAR_ASISTENTE]}>
            <VisitasAsistente />
        </RequierePermiso>
    );
}
