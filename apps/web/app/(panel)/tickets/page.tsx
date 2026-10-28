import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { Bandeja } from "./bandeja";

export const metadata: Metadata = { title: "Tickets" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["tickets.ver"]}>
            <Bandeja />
        </RequierePermiso>
    );
}
