import type { Metadata } from "next";
import { Suspense } from "react";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISO_CONFIGURAR_CORREO, PERMISO_VER_CORREO } from "@/lib/correo/tipos";
import { CorreoDelSistema } from "./correo";

export const metadata: Metadata = { title: "Correo" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_VER_CORREO, PERMISO_CONFIGURAR_CORREO]}>
            <Suspense>
                <CorreoDelSistema />
            </Suspense>
        </RequierePermiso>
    );
}
