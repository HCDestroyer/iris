import type { Metadata } from "next";
import { NuevaConversacion } from "@/componentes/conversaciones/nueva-conversacion";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISO_INICIAR } from "@/lib/conversaciones/contrato-11";

export const metadata: Metadata = { title: "Nueva conversación" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_INICIAR]}>
            <NuevaConversacion />
        </RequierePermiso>
    );
}
