import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISO_CONFIGURAR_CENTRALITA } from "@/lib/centralita/tipos";
import { TelefonoCentralita } from "./formulario";

export const metadata: Metadata = { title: "Teléfono en Iris" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_CONFIGURAR_CENTRALITA]}>
            <TelefonoCentralita />
        </RequierePermiso>
    );
}
