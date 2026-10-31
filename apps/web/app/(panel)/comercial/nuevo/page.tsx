import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { NuevoProspecto } from "./nuevo-prospecto";

export const metadata: Metadata = { title: "Nuevo prospecto" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["prospectos.crear"]}>
            <NuevoProspecto />
        </RequierePermiso>
    );
}
