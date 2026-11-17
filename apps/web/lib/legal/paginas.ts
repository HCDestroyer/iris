/*
 * Páginas legales públicas (contratos/legal.md): las tres que Meta pide para revisar y publicar la
 * app de WhatsApp (política de privacidad, términos del servicio y eliminación de datos), en español
 * y en inglés. Las rutas son estables: se pegan en el panel de Meta y no deben cambiar.
 */

export type Idioma = "es" | "en";
export type ClavePagina = "privacidad" | "terminos" | "eliminacion";

export const IDIOMAS: readonly Idioma[] = ["es", "en"];
export const CLAVES: readonly ClavePagina[] = ["privacidad", "terminos", "eliminacion"];

export const RUTAS: Record<ClavePagina, Record<Idioma, string>> = {
    privacidad: { es: "/privacidad", en: "/en/privacy" },
    terminos: { es: "/terminos", en: "/en/terms" },
    eliminacion: { es: "/eliminacion-de-datos", en: "/en/data-deletion" },
};

export const TITULOS: Record<ClavePagina, Record<Idioma, string>> = {
    privacidad: { es: "Política de privacidad", en: "Privacy Policy" },
    terminos: { es: "Términos del servicio", en: "Terms of Service" },
    eliminacion: { es: "Eliminación de datos", en: "Data Deletion" },
};

/** Las seis rutas, para el middleware (sin sesión), `robots.txt` y la pantalla del panel. */
export const RUTAS_PUBLICAS: readonly string[] = CLAVES.flatMap((c) => IDIOMAS.map((i) => RUTAS[c][i]));

/** Marca en el HTML de cada página: con ella el panel comprueba que la URL sirve la página y no otra cosa. */
export const MARCA_PAGINA = "data-pagina-legal";
