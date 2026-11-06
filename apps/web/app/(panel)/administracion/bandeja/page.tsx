import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { AjustesDeBandeja } from "./ajustes";

export const metadata: Metadata = { title: "Ajustes de la bandeja" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["canales.configurar"]}>
            <AjustesDeBandeja />
        </RequierePermiso>
    );
}
