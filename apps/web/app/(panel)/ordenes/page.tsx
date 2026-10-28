import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { Tablero } from "./tablero";

export const metadata: Metadata = { title: "Órdenes" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["ordenes.ver", "ordenes.ver_propias"]}>
            <Tablero />
        </RequierePermiso>
    );
}
