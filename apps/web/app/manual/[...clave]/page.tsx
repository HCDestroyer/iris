import type { Metadata } from "next";
import { notFound, redirect } from "next/navigation";
import { VistaPaginaManual } from "@/componentes/manual/pagina";
import { esClaveCampo, nombreSeccion, PAGINAS_MANUAL, paginaManual, rutaCampo, rutaDeClave } from "@/lib/manual/contenido";

type Props = { params: Promise<{ clave: string[] }> };

function claveDe(partes: readonly string[]): string {
    return partes.map((p) => decodeURIComponent(p)).join("/");
}

/** Todas las páginas se arman en el build (son estáticas); una clave desconocida responde 404. */
export function generateStaticParams() {
    return PAGINAS_MANUAL.filter((p) => !esClaveCampo(p.clave)).map((p) => ({ clave: p.clave.split("/") }));
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
    const pagina = paginaManual(claveDe((await params).clave));
    return { title: pagina ? `${pagina.titulo} · Manual` : "Manual" };
}

/** /manual/<modulo>/<pantalla>: la página que abre el botón «?» de esa pantalla. */
export default async function PaginaManual({ params }: Props) {
    const clave = claveDe((await params).clave);
    const pagina = paginaManual(clave);
    if (!pagina) notFound();
    // La app del técnico abre su manual en /campo/manual, que el service worker guarda para leerlo sin señal.
    if (esClaveCampo(clave)) redirect(rutaCampo(clave));
    return (
        <VistaPaginaManual
            fuente={pagina.fuente}
            ruta={rutaDeClave}
            indice="/manual"
            migas={[{ etiqueta: "Manual", href: "/manual" }, { etiqueta: nombreSeccion(clave), href: "/manual" }, { etiqueta: pagina.titulo }]}
        />
    );
}
