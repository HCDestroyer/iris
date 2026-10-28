import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { AdoptarDatos } from "./adoptar-datos";

export const metadata: Metadata = { title: "Datos sin empresa" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["empresas.adoptar"]}>
            <AdoptarDatos />
        </RequierePermiso>
    );
}
