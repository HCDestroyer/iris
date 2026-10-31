import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { DetalleUsuario } from "./detalle-usuario";

export const metadata: Metadata = { title: "Usuario" };

export default function PaginaUsuario() {
    return (
        <RequierePermiso permisos={["usuarios.ver"]}>
            {/* La pestaña elegida va en la URL (useSearchParams). */}
            <Suspense>
                <DetalleUsuario />
            </Suspense>
        </RequierePermiso>
    );
}
