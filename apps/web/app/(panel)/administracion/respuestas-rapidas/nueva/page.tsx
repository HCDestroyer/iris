import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { FormularioRespuesta } from "../formulario";

export const metadata: Metadata = { title: "Nueva respuesta rápida" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["conversaciones.configurar"]}>
            <FormularioRespuesta />
        </RequierePermiso>
    );
}
