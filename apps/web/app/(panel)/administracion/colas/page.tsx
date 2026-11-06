import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { ListaColas } from "./lista";

export const metadata: Metadata = { title: "Colas" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["canales.configurar"]}>
            <ListaColas />
        </RequierePermiso>
    );
}
