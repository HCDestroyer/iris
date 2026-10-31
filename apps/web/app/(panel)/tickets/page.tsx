import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PantallaTickets } from "./bandeja";

export const metadata: Metadata = { title: "Tickets" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["tickets.ver"]}>
            <Suspense>
                <PantallaTickets />
            </Suspense>
        </RequierePermiso>
    );
}
