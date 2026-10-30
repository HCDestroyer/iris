import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { ActividadApp } from "./actividad-app";

export const metadata: Metadata = { title: "Actividad de la app" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["campo.ver_actividad"]}>
            <ActividadApp />
        </RequierePermiso>
    );
}
