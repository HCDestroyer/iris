import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { NuevaPlantilla } from "./nueva-plantilla";

export const metadata: Metadata = { title: "Nueva plantilla" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["plantillas.crear"]}>
            <NuevaPlantilla />
        </RequierePermiso>
    );
}
