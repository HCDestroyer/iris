import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISO_CONFIGURAR_ASISTENTE } from "@/lib/asistente/permisos";
import { ListaCategorias } from "./lista";

export const metadata: Metadata = { title: "Categorías de mensajes" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_CONFIGURAR_ASISTENTE]}>
            <ListaCategorias />
        </RequierePermiso>
    );
}
