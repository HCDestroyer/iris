import type { PruebaAsistente, Sugerencia, UsoAsistente } from "./tipos";

/*
 * Lógica de pantalla del asistente de IA (contratos/conversaciones.md §16), sin React: qué
 * sugerencia queda a la vista cuando llegan cambios, cómo se explica cada error y cómo se escriben
 * las cifras de uso y de la prueba de conexión.
 */

/** La sugerencia se muestra mientras genera, está lista o falló (las demás ya no son vigentes). */
export function esVigente(s: Pick<Sugerencia, "estado">): boolean {
    return s.estado === "generando" || s.estado === "lista" || s.estado === "error";
}

const posterior = (a: string, b: string) => Date.parse(a) > Date.parse(b);

/**
 * Aplica un cambio (respuesta de una acción, evento `sugerencia` del feed o la lectura de
 * `…/actual`) sobre la sugerencia a la vista. El feed puede repetir eventos o traerlos en otro
 * orden: un estado viejo de la misma sugerencia (`actualizadaEn` anterior) no pisa al nuevo, y una
 * sugerencia vieja que deja de ser vigente no oculta la nueva.
 */
export function aplicarSugerencia(actual: Sugerencia | null, nueva: Sugerencia): Sugerencia | null {
    if (actual && actual.conversacionId !== nueva.conversacionId) return actual;
    if (actual && actual.id === nueva.id) {
        if (posterior(actual.actualizadaEn, nueva.actualizadaEn)) return actual;
        return esVigente(nueva) ? nueva : null;
    }
    if (!esVigente(nueva)) return actual;
    if (actual && esVigente(actual) && posterior(actual.creadaEn, nueva.creadaEn)) return actual;
    return nueva;
}

export type ExplicacionError = { titulo: string; texto: string; reintentar: boolean };

/**
 * Error de la sugerencia (o de pedirla) en palabras del agente. `tope_alcanzado` y
 * `sin_configurar` no se arreglan reintentando: lo explican y no ofrecen «Reintentar».
 */
export function explicarError(codigo: string, detalle?: string | null): ExplicacionError {
    switch (codigo) {
        case "tope_alcanzado":
            return {
                titulo: "El asistente está en pausa este mes",
                texto: "Se llegó al tope de uso del mes. Vuelve el mes que viene o cuando un administrador suba el tope.",
                reintentar: false,
            };
        case "sin_configurar":
            return { titulo: "El asistente no está configurado", texto: "Un administrador debe revisarlo en Administración › Asistente de IA.", reintentar: false };
        case "asistente_inactivo":
            return { titulo: "El asistente está apagado", texto: "Un administrador lo puede activar en Administración › Asistente de IA.", reintentar: false };
        case "sin_mensaje":
            return { titulo: "Todavía no hay qué responder", texto: "El asistente sugiere cuando hay un mensaje del cliente.", reintentar: false };
        case "conversacion_resuelta":
            return { titulo: "La conversación está resuelta", texto: "Reábrela para pedir una sugerencia.", reintentar: false };
        case "rechazo_modelo":
            return { titulo: "El asistente no quiso sugerir esta vez", texto: detalle || "Escribe la respuesta tú o pide otra.", reintentar: true };
        case "respuesta_invalida":
            return { titulo: "La sugerencia no pasó la revisión", texto: detalle || "Iris la descartó por no cumplir las reglas. Puedes pedir otra.", reintentar: true };
        case "clave_invalida":
            return { titulo: "No se pudo preparar la sugerencia", texto: detalle || "La clave del proveedor de IA no es válida.", reintentar: true };
        default:
            return { titulo: "No se pudo preparar la sugerencia", texto: detalle || "El servidor de IA no respondió.", reintentar: true };
    }
}

const ENTERO = new Intl.NumberFormat("es-GT", { maximumFractionDigits: 0 });
const UN_DECIMAL = new Intl.NumberFormat("es-GT", { maximumFractionDigits: 1 });

/** Tokens en palabras: «2.1 millones», «850 mil», «1,200». */
export function formatearTokens(n: number): string {
    if (n >= 1_000_000) {
        const m = n / 1_000_000;
        return `${UN_DECIMAL.format(m)} ${m === 1 ? "millón" : "millones"}`;
    }
    if (n >= 10_000) return `${ENTERO.format(Math.round(n / 1000))} mil`;
    return ENTERO.format(n);
}

/** Por cada token salen unas 0.75 palabras: la velocidad se dice en palabras, que se entienden. */
const PALABRAS_POR_TOKEN = 0.75;

/** «Respondió en 1.8 s · 28 palabras por segundo», o el detalle del error. */
export function textoPrueba(p: PruebaAsistente): string {
    if (p.estado !== "ok") return p.detalle || "El proveedor no respondió.";
    const segundos = UN_DECIMAL.format(Math.max(p.latenciaMs, 0) / 1000);
    const velocidad = p.tokensPorSegundo && p.tokensPorSegundo > 0 ? ` · ${ENTERO.format(Math.max(1, Math.round(p.tokensPorSegundo * PALABRAS_POR_TOKEN)))} palabras por segundo` : "";
    return `Respondió en ${segundos} s${velocidad}`;
}

/** % de sugerencias que se usaron; `null` sin sugerencias (no se inventa un 0 %). */
export function porcentajeUsadas(uso: Pick<UsoAsistente, "sugerencias" | "usadas">): number | null {
    if (uso.sugerencias <= 0) return null;
    return Math.round((uso.usadas / uso.sugerencias) * 100);
}

/** «noviembre de 2026» desde `2026-11`. */
export function nombreMes(mes: string): string {
    const [a, m] = mes.split("-").map(Number);
    if (!a || !m) return mes;
    return new Intl.DateTimeFormat("es-GT", { month: "long", year: "numeric", timeZone: "UTC" }).format(Date.UTC(a, m - 1, 1));
}
