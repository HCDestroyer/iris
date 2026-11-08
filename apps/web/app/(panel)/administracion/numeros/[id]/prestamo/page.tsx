import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { NumeroPrestado } from "./prestamo";

export const metadata: Metadata = { title: "Número prestado" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["canales.configurar", "mensajeria.configurar"]}>
            <NumeroPrestado />
        </RequierePermiso>
    );
}
