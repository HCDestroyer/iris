import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { DetalleTicket } from "./detalle";

export const metadata: Metadata = { title: "Ticket" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["tickets.ver"]}>
            {/* La pestaña elegida va en la URL (useSearchParams). */}
            <Suspense>
                <DetalleTicket />
            </Suspense>
        </RequierePermiso>
    );
}
