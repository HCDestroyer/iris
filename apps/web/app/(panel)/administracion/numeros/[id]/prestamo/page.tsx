import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { NumeroPrestado } from "./prestamo";

export const metadata: Metadata = { title: "Número prestado" };

export default function Pagina() {
    return (
        // GET /canales/{id} pide mensajeria.configurar.
        <RequierePermiso permisos={["mensajeria.configurar"]}>
            <NumeroPrestado />
        </RequierePermiso>
    );
}
