import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISOS_CUADRILLAS } from "@/lib/agenda/permisos";
import { PantallaCuadrillas } from "./cuadrillas";

export const metadata: Metadata = { title: "Cuadrillas" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={PERMISOS_CUADRILLAS}>
            <PantallaCuadrillas />
        </RequierePermiso>
    );
}
