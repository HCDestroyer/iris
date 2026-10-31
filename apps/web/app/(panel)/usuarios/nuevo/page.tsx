import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { NuevoUsuario } from "./nuevo-usuario";

export const metadata: Metadata = { title: "Nuevo usuario" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["usuarios.crear"]}>
            <Suspense>
                <NuevoUsuario />
            </Suspense>
        </RequierePermiso>
    );
}
