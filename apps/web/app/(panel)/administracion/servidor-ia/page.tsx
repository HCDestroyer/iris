import type { Metadata } from "next";
import { PuertaServidorIa } from "@/componentes/servidor-ia/comun";
import { PaginaServidorIa } from "@/componentes/servidor-ia/pagina";

export const metadata: Metadata = { title: "Servidor de IA" };

export default function Pagina() {
    return (
        <PuertaServidorIa nivel="ver">
            <PaginaServidorIa />
        </PuertaServidorIa>
    );
}
