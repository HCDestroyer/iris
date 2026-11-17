import type { Metadata } from "next";
import { PuertaServidorIa } from "@/componentes/servidor-ia/comun";
import { EnlazarServidorIa } from "@/componentes/servidor-ia/enlazar";

export const metadata: Metadata = { title: "Enlazar el servidor de IA" };

export default function Pagina() {
    return (
        <PuertaServidorIa nivel="administrar">
            <EnlazarServidorIa />
        </PuertaServidorIa>
    );
}
