/*
 * Búsqueda del manual, en el navegador. Sin el contenido: así el buscador (componente de cliente)
 * no carga el JSON entero, solo las entradas que le pasa la pantalla.
 */

/** Lo que el buscador necesita de cada página (sin marcas de Markdown). */
export type EntradaBusqueda = { clave: string; titulo: string; seccion: string; texto: string };

/** Minúsculas y sin acentos: «Órdenes» y «ordenes» se encuentran igual. */
export function normalizar(texto: string): string {
    return texto
        .normalize("NFD")
        .replace(/\p{Diacritic}/gu, "")
        .toLowerCase();
}

export type ResultadoBusqueda = EntradaBusqueda & { extracto: string };

const LARGO_EXTRACTO = 160;

/**
 * Páginas que contienen todas las palabras buscadas (en el título, la sección o el texto). Primero
 * las que las tienen en el título. Cada resultado lleva un extracto alrededor de la primera palabra.
 */
export function buscarEnManual(entradas: readonly EntradaBusqueda[], consulta: string): ResultadoBusqueda[] {
    const palabras = normalizar(consulta)
        .split(/\s+/)
        .filter((p) => p.length > 0);
    if (palabras.length === 0) return [];
    const resultados: { r: ResultadoBusqueda; puntos: number }[] = [];
    for (const e of entradas) {
        const titulo = normalizar(`${e.titulo} ${e.seccion}`);
        const texto = normalizar(e.texto);
        if (!palabras.every((p) => titulo.includes(p) || texto.includes(p))) continue;
        const puntos = palabras.filter((p) => titulo.includes(p)).length;
        resultados.push({ r: { ...e, extracto: extracto(e.texto, texto, palabras) }, puntos });
    }
    return resultados.sort((a, b) => b.puntos - a.puntos || a.r.titulo.localeCompare(b.r.titulo, "es")).map((x) => x.r);
}

function extracto(original: string, normalizado: string, palabras: readonly string[]): string {
    // La normalización NFD puede alargar el texto: se busca la posición en el original por palabras.
    const indice = palabras.map((p) => normalizado.indexOf(p)).find((i) => i >= 0) ?? -1;
    if (indice < 0 || normalizado.length !== original.length) {
        return original.length > LARGO_EXTRACTO ? `${original.slice(0, LARGO_EXTRACTO).trimEnd()}…` : original;
    }
    const desde = Math.max(0, indice - 40);
    const hasta = Math.min(original.length, desde + LARGO_EXTRACTO);
    return `${desde > 0 ? "…" : ""}${original.slice(desde, hasta).trim()}${hasta < original.length ? "…" : ""}`;
}
