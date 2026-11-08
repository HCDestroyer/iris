import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { CompartirNumero } from "./compartir";

export const metadata: Metadata = { title: "Compartir número" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["canales.configurar"]}>
            <CompartirNumero />
        </RequierePermiso>
    );
}
