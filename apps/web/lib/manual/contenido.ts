import { textoPlano } from "@/componentes/manual/markdown";
import type { EntradaBusqueda } from "./busqueda";
import datos from "./contenido.generado.json";

/*
 * Manual de Iris: las páginas de docs/manual, generadas como JSON por `pnpm --filter @iris/web
 * manual` (scripts/manual.mjs). La clave de cada página es `modulo/pantalla` (docs/manual/claves.md),
 * la misma que recibe el botón «?» de cada pantalla.
 */

export type PaginaManual = { clave: string; titulo: string; fuente: string };

export const INDICE_MANUAL: string = datos.indice;
export const PAGINAS_MANUAL: readonly PaginaManual[] = datos.paginas;

const POR_CLAVE = new Map(PAGINAS_MANUAL.map((p) => [p.clave, p]));

/** La página de una clave (`tickets/lista`), o null si esa pantalla aún no tiene manual. */
export function paginaManual(clave: string): PaginaManual | null {
    return POR_CLAVE.get(clave) ?? null;
}

/** La sección de la app del técnico: se lee en el teléfono y abre sin señal (/campo/manual). */
export const SECCION_CAMPO = "campo";

export function esClaveCampo(clave: string): boolean {
    return clave.startsWith(`${SECCION_CAMPO}/`);
}

export const PAGINAS_CAMPO: readonly PaginaManual[] = PAGINAS_MANUAL.filter((p) => esClaveCampo(p.clave));

/** Ruta sin señal de una página de campo (`campo/hoy` → `/campo/manual/hoy`). */
export function rutaCampo(clave: string): string {
    return `/campo/manual/${clave.slice(SECCION_CAMPO.length + 1)}`;
}

/** Nombre de cada sección (la carpeta de docs/manual) para las migas y los resultados. */
const SECCIONES: Record<string, string> = {
    administracion: "Administración",
    agenda: "Agenda",
    campo: "App del técnico",
    comercial: "Ventas",
    conversaciones: "Conversaciones",
    cuenta: "Tu cuenta",
    despacho: "Despacho",
    encuestas: "Encuestas",
    equipo: "Chat del equipo",
    inicio: "Inicio",
    mapa: "Mapa",
    ordenes: "Órdenes",
    reportes: "Reportes",
    soporte: "Portal de soporte",
    tecnicos: "Técnicos",
    tickets: "Tickets",
    usuarios: "Usuarios",
};

export function nombreSeccion(clave: string): string {
    const seccion = clave.split("/")[0] ?? "";
    return SECCIONES[seccion] ?? seccion;
}

export function entradasBusqueda(paginas: readonly PaginaManual[] = PAGINAS_MANUAL): EntradaBusqueda[] {
    return paginas.map((p) => ({
        clave: p.clave,
        titulo: p.titulo,
        seccion: nombreSeccion(p.clave),
        // Sin el título, que ya va aparte.
        texto: textoPlano(p.fuente.replace(/^# .*\n?/, "")),
    }));
}

/** Dónde se abre cada clave: las de la app del técnico, en /campo/manual (abren sin señal). */
export function rutaDeClave(clave: string | null): string {
    if (clave === null) return "/manual";
    return esClaveCampo(clave) ? rutaCampo(clave) : `/manual/${clave}`;
}

/** Rutas de todas las páginas, para los resultados del buscador. */
export function rutasDe(paginas: readonly PaginaManual[] = PAGINAS_MANUAL): Record<string, string> {
    return Object.fromEntries(paginas.map((p) => [p.clave, rutaDeClave(p.clave)]));
}
