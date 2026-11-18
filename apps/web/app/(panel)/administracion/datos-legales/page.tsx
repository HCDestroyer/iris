import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISO_CONFIGURAR_LEGAL, PERMISO_VER_LEGAL } from "@/lib/legal/datos";
import { DatosLegalesPantalla } from "./datos-legales";

export const metadata: Metadata = { title: "Datos legales" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_VER_LEGAL, PERMISO_CONFIGURAR_LEGAL]}>
            <DatosLegalesPantalla />
        </RequierePermiso>
    );
}
