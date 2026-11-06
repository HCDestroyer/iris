import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { FormularioHorario } from "../formulario";

export const metadata: Metadata = { title: "Nuevo horario" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["canales.configurar"]}>
            <FormularioHorario />
        </RequierePermiso>
    );
}
