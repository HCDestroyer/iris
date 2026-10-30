import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PantallaAgenda } from "./agenda";

export const metadata: Metadata = { title: "Agenda" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["agenda.ver"]}>
            <Suspense>
                <PantallaAgenda />
            </Suspense>
        </RequierePermiso>
    );
}
