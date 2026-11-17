import type { Schema } from "@iris/contratos";
import { z } from "zod";
import { api, resolver } from "@/lib/api/cliente";
import type { Permiso } from "@/lib/menu";
import { CLAVES, IDIOMAS, RUTAS, TITULOS, type ClavePagina, type Idioma } from "./paginas";

/*
 * Datos legales del titular en el panel (contratos/legal.md): tipos de `packages/contratos`, el
 * esquema zod espejo del validador del servidor y la lógica pura de la pantalla.
 */

/** `GET /datos-legales`. */
export type DatosLegales = Schema<"DatosLegales">;
/** Cuerpo de `POST /datos-legales/guardar`. */
export type EntradaLegal = Schema<"CuerpoGuardar">;

export const PERMISO_VER_LEGAL = "datos_legales.ver" satisfies Permiso;
export const PERMISO_CONFIGURAR_LEGAL = "datos_legales.configurar" satisfies Permiso;

// --- Llamadas -----------------------------------------------------------------------------------

export function leerDatosLegales(signal?: AbortSignal) {
    return resolver(api().GET("/api/v1/datos-legales", { signal }));
}

export function guardarDatosLegales(entrada: EntradaLegal, clave: string) {
    return resolver(api().POST("/api/v1/datos-legales/guardar", { params: { header: { "Idempotency-Key": clave } }, body: entrada }));
}

// --- Formulario ---------------------------------------------------------------------------------

/** Una sola línea: sin saltos ni caracteres de control. */
const unaLinea = (v: string) => ![...v].some((c) => c.charCodeAt(0) < 32 || c.charCodeAt(0) === 127);
const CORREO = /^[^\s@<>,;"]+@[^\s@<>,;"]+\.[^\s@<>,;"]+$/;
const TELEFONO = /^\+?[0-9][0-9 ().-]{5,38}$/;

const texto = (max: number) => z.string().trim().max(max, `Hasta ${max} caracteres`).refine(unaLinea, "Escríbelo en una sola línea");

/** Todo puede ir vacío (se guarda lo que haya); lo que falta para estar listo lo dice `faltan`. */
export const esquemaLegal = z.object({
    razonSocial: texto(200),
    nombreComercial: texto(120),
    correoPrivacidad: texto(254).refine((v) => v === "" || CORREO.test(v), "Escribe un solo correo, como privacidad@tuempresa.com"),
    telefono: texto(40).refine((v) => v === "" || TELEFONO.test(v), "Solo dígitos, espacios, +, guiones y paréntesis"),
    direccion: texto(300),
    pais: texto(80),
});
export type ValoresLegal = z.infer<typeof esquemaLegal>;

export const CAMPOS_LEGAL = ["razonSocial", "nombreComercial", "correoPrivacidad", "telefono", "direccion", "pais"] as const;
export type CampoLegal = (typeof CAMPOS_LEGAL)[number];

export function valoresLegal(d: DatosLegales): ValoresLegal {
    return {
        razonSocial: d.razonSocial,
        nombreComercial: d.nombreComercial,
        correoPrivacidad: d.correoPrivacidad,
        telefono: d.telefono,
        direccion: d.direccion,
        pais: d.pais,
    };
}

export function entradaLegal(d: Pick<DatosLegales, "version">, v: ValoresLegal): EntradaLegal {
    return { version: d.version, ...v };
}

// --- Lo que falta -------------------------------------------------------------------------------

/** Cómo se le nombra a la persona cada dato obligatorio. */
const NOMBRE_FALTANTE: Record<string, string> = {
    razonSocial: "La razón social",
    nombreComercial: "El nombre comercial",
    correoPrivacidad: "El correo de privacidad",
    direccion: "La dirección",
    pais: "El país",
};

/** Los datos que faltan, con las palabras del formulario. Un código que el panel no conoce se muestra tal cual. */
export function faltantesEnLlano(faltan: readonly string[] | null | undefined): string[] {
    return (faltan ?? []).map((f) => NOMBRE_FALTANTE[f] ?? f);
}

// --- URLs para Meta -----------------------------------------------------------------------------

export type EnlaceMeta = { clave: ClavePagina; idioma: Idioma; titulo: string; ruta: string; url: string };

/** Dónde se pega cada URL en el panel de Meta (developers.facebook.com › la app › Configuración de la app › Básica). */
export const CAMPO_META: Record<ClavePagina, string> = {
    privacidad: "URL de la Política de privacidad",
    terminos: "URL de las Condiciones del servicio",
    eliminacion: "Eliminación de datos de usuarios › URL de instrucciones para la eliminación de datos",
};

const NOMBRE_IDIOMA: Record<Idioma, string> = { es: "Español", en: "Inglés" };
export const nombreIdioma = (i: Idioma): string => NOMBRE_IDIOMA[i];

/** Las seis URL públicas de esta instalación, con el origen que ve el navegador (el dominio del panel). */
export function enlacesMeta(origen: string): EnlaceMeta[] {
    const base = origen.replace(/\/+$/, "");
    return CLAVES.flatMap((clave) => IDIOMAS.map((idioma) => ({ clave, idioma, titulo: TITULOS[clave][idioma], ruta: RUTAS[clave][idioma], url: `${base}${RUTAS[clave][idioma]}` })));
}

/** Meta solo acepta HTTPS; en una instalación local se avisa en vez de dar por buena la URL. */
export const esHttps = (origen: string): boolean => origen.startsWith("https://");

export type EstadoEnlace = "comprobando" | "ok" | "error";

/** La página respondió 200 y es la página legal esperada (no el inicio de sesión ni otra cosa). */
export function paginaResponde(estado: number, html: string, clave: ClavePagina): boolean {
    return estado === 200 && html.includes(`data-pagina-legal="${clave}"`);
}
