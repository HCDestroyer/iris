/*
 * Marcadores de datos por completar en un manual: texto entre corchetes como «[Nombre del ISP]» o
 * «[número de soporte]» (los manuales de inicio los traen). Un enlace de Markdown `[texto](url)` no
 * cuenta. Mientras quede alguno, el manual no se envía al cliente desde la bandeja y la biblioteca
 * lo marca «Falta completar datos de tu empresa».
 */
const MARCADOR = /\[([^\]\n]{2,})\](?!\()/g;

/** Los marcadores del texto, sin repetir y en el orden en que aparecen: «[Nombre del ISP]»… */
export function marcadoresPendientes(texto: string): string[] {
    return [...new Set([...texto.matchAll(MARCADOR)].map((m) => `[${(m[1] ?? "").trim()}]`))];
}

export function tieneMarcadores(texto: string): boolean {
    return marcadoresPendientes(texto).length > 0;
}

/** «[Nombre del ISP], [número de soporte] y 3 más». */
export function textoMarcadores(marcadores: readonly string[], maximo = 2): string {
    const primeros = marcadores.slice(0, maximo);
    if (marcadores.length > maximo) return `${primeros.join(", ")} y ${marcadores.length - maximo} más`;
    return primeros.length === 2 ? primeros.join(" y ") : primeros.join("");
}

export const TEXTO_FALTA_COMPLETAR = "Falta completar datos de tu empresa";
