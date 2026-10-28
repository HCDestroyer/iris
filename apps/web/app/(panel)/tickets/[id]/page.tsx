import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { DetalleTicket } from "./detalle";

export const metadata: Metadata = { title: "Ticket" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["tickets.ver"]}>
            <DetalleTicket />
        </RequierePermiso>
    );
}
