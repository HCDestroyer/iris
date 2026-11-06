import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { ListaRespuestasRapidas } from "./lista";

export const metadata: Metadata = { title: "Respuestas rápidas" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["conversaciones.configurar"]}>
            <ListaRespuestasRapidas />
        </RequierePermiso>
    );
}
