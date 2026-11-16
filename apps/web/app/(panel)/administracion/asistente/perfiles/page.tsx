import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISO_CONFIGURAR_ASISTENTE } from "@/lib/asistente/permisos";
import { ListaPerfiles } from "./lista";

export const metadata: Metadata = { title: "Perfiles por horario" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={[PERMISO_CONFIGURAR_ASISTENTE]}>
            <ListaPerfiles />
        </RequierePermiso>
    );
}
