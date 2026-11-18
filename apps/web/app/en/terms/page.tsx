import { metadatosLegales, PaginaLegalServidor } from "@/componentes/legal/servidor";

// Pública y sin sesión (contratos/legal.md). Se arma en cada visita: los datos del titular se leen
// de la API en el servidor (con caché de un minuto), nunca en el build.
export const dynamic = "force-dynamic";

export const generateMetadata = () => metadatosLegales("terminos", "en");

export default function Pagina() {
    return <PaginaLegalServidor clave="terminos" idioma="en" />;
}
