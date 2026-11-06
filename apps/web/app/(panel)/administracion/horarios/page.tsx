import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { ListaHorarios } from "./lista";

export const metadata: Metadata = { title: "Horarios" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["canales.configurar"]}>
            <ListaHorarios />
        </RequierePermiso>
    );
}
