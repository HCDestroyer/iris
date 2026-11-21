import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { VistaPaginaManual } from "@/componentes/manual/pagina";
import { PAGINAS_CAMPO, paginaManual, rutaDeClave, SECCION_CAMPO } from "@/lib/manual/contenido";

type Props = { params: Promise<{ pantalla: string }> };

export function generateStaticParams() {
    return PAGINAS_CAMPO.map((p) => ({ pantalla: p.clave.slice(SECCION_CAMPO.length + 1) }));
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
    const pagina = paginaManual(`${SECCION_CAMPO}/${decodeURIComponent((await params).pantalla)}`);
    return { title: { absolute: pagina ? `${pagina.titulo} · Manual · Iris Campo` : "Manual · Iris Campo" } };
}

/** /campo/manual/<pantalla>: la página de la clave `campo/<pantalla>`, también sin señal. */
export default async function PaginaManualCampo({ params }: Props) {
    const pagina = paginaManual(`${SECCION_CAMPO}/${decodeURIComponent((await params).pantalla)}`);
    if (!pagina) notFound();
    return (
        <VistaPaginaManual
            fuente={pagina.fuente}
            ruta={rutaDeClave}
            indice="/campo/manual"
            migas={[{ etiqueta: "Manual de la app", href: "/campo/manual" }, { etiqueta: pagina.titulo }]}
        />
    );
}
