import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { NuevoTicket } from "./nuevo-ticket";

export const metadata: Metadata = { title: "Nuevo ticket" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["tickets.crear"]}>
            <NuevoTicket />
        </RequierePermiso>
    );
}
