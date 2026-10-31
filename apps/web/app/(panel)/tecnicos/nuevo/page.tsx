import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { NuevoTecnico } from "./nuevo-tecnico";

export const metadata: Metadata = { title: "Nuevo técnico" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["agenda.crear_tecnico"]}>
            <Suspense>
                <NuevoTecnico />
            </Suspense>
        </RequierePermiso>
    );
}
