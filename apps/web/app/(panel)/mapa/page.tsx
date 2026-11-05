import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PantallaMapa } from "./mapa";

export const metadata: Metadata = { title: "Mapa" };

/** Técnicos en vivo (`campo.ver_posiciones`) y órdenes del día (`agenda.ver`): basta uno de los dos. */
export default function Pagina() {
    return (
        <RequierePermiso permisos={["agenda.ver", "campo.ver_posiciones"]}>
            <PantallaMapa />
        </RequierePermiso>
    );
}
