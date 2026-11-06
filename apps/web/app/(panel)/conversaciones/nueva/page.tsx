import type { Metadata } from "next";
import { NuevaConversacion } from "@/componentes/conversaciones/nueva-conversacion";
import { RequierePermiso } from "@/componentes/requiere-permiso";

export const metadata: Metadata = { title: "Nueva conversación" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["conversaciones.iniciar"]}>
            <NuevaConversacion />
        </RequierePermiso>
    );
}
