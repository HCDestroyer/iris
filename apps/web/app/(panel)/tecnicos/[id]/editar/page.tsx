import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { EditarTecnico } from "./editar-tecnico";

export const metadata: Metadata = { title: "Editar técnico" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["agenda.editar_tecnico"]}>
            <Suspense>
                <EditarTecnico />
            </Suspense>
        </RequierePermiso>
    );
}
