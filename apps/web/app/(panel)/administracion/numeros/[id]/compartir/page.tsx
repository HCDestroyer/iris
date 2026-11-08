import type { Metadata } from "next";
import { RequierePermiso } from "@/componentes/requiere-permiso";
import { CompartirNumero } from "./compartir";

export const metadata: Metadata = { title: "Compartir número" };

export default function Pagina() {
    return (
        // Los dos: GET /canales/{id} pide mensajeria.configurar y los préstamos, canales.configurar
        // (RequierePermiso acepta cualquiera de su lista, por eso van anidados).
        <RequierePermiso permisos={["mensajeria.configurar"]}>
            <RequierePermiso permisos={["canales.configurar"]}>
                <CompartirNumero />
            </RequierePermiso>
        </RequierePermiso>
    );
}
