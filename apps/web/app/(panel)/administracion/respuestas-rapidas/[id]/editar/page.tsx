import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { EditarRespuesta } from "./editar";

export const metadata: Metadata = { title: "Editar respuesta rápida" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["conversaciones.configurar"]}>
            <EditarRespuesta />
        </RequierePermiso>
    );
}
