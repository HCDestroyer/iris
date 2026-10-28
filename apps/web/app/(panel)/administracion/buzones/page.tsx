import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { ListaBuzones } from "./lista-buzones";

export const metadata: Metadata = { title: "Buzones de correo" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["buzones.ver"]}>
            <ListaBuzones />
        </RequierePermiso>
    );
}
