import type { Metadata } from "next";
import { cache } from "react";
import { leerDatosPublicos } from "@/lib/legal/leer";
import { TITULOS, type ClavePagina, type Idioma } from "@/lib/legal/paginas";
import { titularDe } from "@/lib/legal/titular";
import { PaginaLegal } from "./pagina-legal";

/* Lo que comparten las seis rutas públicas: leer el titular una vez por petición, el título y la página. */

const leer = cache(leerDatosPublicos);

const DESCRIPCION: Record<ClavePagina, Record<Idioma, string>> = {
    privacidad: { es: "Qué datos personales tratamos al atenderte, para qué y cómo ejercer tus derechos.", en: "What personal data we process when assisting you, why, and how to exercise your rights." },
    terminos: { es: "Condiciones de uso de nuestros canales de atención al cliente.", en: "Terms of use of our customer service channels." },
    eliminacion: { es: "Cómo pedir que eliminemos tus datos personales y qué pasa después.", en: "How to ask us to delete your personal data and what happens next." },
};

export async function metadatosLegales(clave: ClavePagina, idioma: Idioma): Promise<Metadata> {
    const titular = titularDe(await leer(), idioma);
    return {
        // El título nombra al titular (Meta pide que la página diga de quién es); sin datos, a Iris.
        title: { absolute: `${TITULOS[clave][idioma]} · ${titular.conNombre ? titular.nombre : "Iris"}` },
        description: DESCRIPCION[clave][idioma],
        robots: { index: true, follow: true },
    };
}

export async function PaginaLegalServidor({ clave, idioma }: { clave: ClavePagina; idioma: Idioma }) {
    return <PaginaLegal clave={clave} idioma={idioma} titular={titularDe(await leer(), idioma)} />;
}
