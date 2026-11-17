import type { Metadata } from "next";
import { PuertaServidorIa } from "@/componentes/servidor-ia/comun";
import { MantenimientoServidorIa } from "@/componentes/servidor-ia/mantenimiento";

export const metadata: Metadata = { title: "Mantenimiento del servidor de IA" };

export default function Pagina() {
    return (
        <PuertaServidorIa nivel="administrar">
            <MantenimientoServidorIa />
        </PuertaServidorIa>
    );
}
