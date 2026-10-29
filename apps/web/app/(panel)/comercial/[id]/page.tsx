import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { DetalleProspecto } from "./detalle";

export const metadata: Metadata = { title: "Prospecto" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["prospectos.ver"]}>
            <DetalleProspecto />
        </RequierePermiso>
    );
}
