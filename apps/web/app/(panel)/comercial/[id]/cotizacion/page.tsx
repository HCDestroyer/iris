import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { PantallaCotizacion } from "./pantalla-cotizacion";

export const metadata: Metadata = { title: "Cotización" };

export default function Pagina() {
    return (
        <RequierePermiso permisos={["cotizaciones.crear"]}>
            <PantallaCotizacion />
        </RequierePermiso>
    );
}
