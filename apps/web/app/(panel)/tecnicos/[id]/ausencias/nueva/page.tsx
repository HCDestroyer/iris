import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { NuevaAusencia } from "./nueva-ausencia";

export const metadata: Metadata = { title: "Nueva ausencia" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["agenda.registrar_ausencia"]}>
            <NuevaAusencia />
        </RequierePermiso>
    );
}
