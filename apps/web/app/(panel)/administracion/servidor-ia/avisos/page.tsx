import type { Metadata } from "next";
import { PuertaServidorIa } from "@/componentes/servidor-ia/comun";
import { AvisosServidorIa } from "@/componentes/servidor-ia/avisos";

export const metadata: Metadata = { title: "Avisos del servidor de IA" };

export default function Pagina() {
    return (
        <PuertaServidorIa nivel="administrar">
            <AvisosServidorIa />
        </PuertaServidorIa>
    );
}
