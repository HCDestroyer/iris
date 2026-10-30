import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISOS_ZONAS } from "@/lib/agenda/permisos";
import { PantallaZonas } from "./zonas";

export const metadata: Metadata = { title: "Zonas" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={PERMISOS_ZONAS}>
            <PantallaZonas />
        </RequierePermiso>
    );
}
