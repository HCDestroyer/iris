import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PERMISOS_TECNICOS } from "@/lib/agenda/permisos";
import { PantallaTecnicos } from "./tecnicos";

export const metadata: Metadata = { title: "Técnicos" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={PERMISOS_TECNICOS}>
            <PantallaTecnicos />
        </RequierePermiso>
    );
}
