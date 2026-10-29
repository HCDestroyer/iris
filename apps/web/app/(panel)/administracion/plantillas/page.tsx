import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { ListaPlantillas } from "./lista-plantillas";

export const metadata: Metadata = { title: "Plantillas de formulario" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["plantillas.ver"]}>
            <ListaPlantillas />
        </RequierePermiso>
    );
}
