// Lógica de pantalla del chat que comparten la PWA y la app nativa (sin React): horas y días en
// America/Guatemala, separadores de fecha, textos de estado de envío, vista previa de la lista,
// quién puede eliminar y enlaces seguros dentro del texto plano.
import type { ConversacionVista, MensajeVista } from "./motor";

export const ZONA_CHAT = "America/Guatemala";
/** Un mensaje propio se puede eliminar hasta 15 min después de `enviado_en` (§2). */
export const PLAZO_ELIMINAR_MS = 15 * 60_000;
/** Texto: 1–4000 caracteres tras recortar los extremos (§3). */
export const MAXIMO_TEXTO = 4000;

const formatoHora = new Intl.DateTimeFormat("es-GT", { timeZone: ZONA_CHAT, hour: "2-digit", minute: "2-digit", hour12: false });
const formatoDia = new Intl.DateTimeFormat("en-CA", { timeZone: ZONA_CHAT, year: "numeric", month: "2-digit", day: "2-digit" });
const formatoFecha = new Intl.DateTimeFormat("es-GT", { timeZone: ZONA_CHAT, weekday: "short", day: "numeric", month: "short" });
const formatoFechaCorta = new Intl.DateTimeFormat("es-GT", { timeZone: ZONA_CHAT, day: "numeric", month: "short" });

const msDe = (iso: string | null | undefined): number | null => {
    const ms = iso ? Date.parse(iso) : Number.NaN;
    return Number.isNaN(ms) ? null : ms;
};

/** «14:05» en Guatemala. */
export function horaChat(iso: string | null | undefined): string {
    const ms = msDe(iso);
    return ms === null ? "" : formatoHora.format(ms);
}

/** Día `YYYY-MM-DD` en Guatemala. */
export function diaChat(ms: number): string {
    return formatoDia.format(ms);
}

const sinPunto = (t: string) => t.replace(/\./g, "");

/** Separador de fecha: «Hoy», «Ayer» o «mar 3 nov». */
export function etiquetaDia(ms: number, ahora: number): string {
    const dia = diaChat(ms);
    if (dia === diaChat(ahora)) {
        return "Hoy";
    }
    if (dia === diaChat(ahora - 24 * 60 * 60_000)) {
        return "Ayer";
    }
    return sinPunto(formatoFecha.format(ms));
}

/** Hora de la fila de la lista: la hora si es de hoy, «Ayer» o la fecha corta. */
export function horaLista(iso: string | null | undefined, ahora: number): string {
    const ms = msDe(iso);
    if (ms === null) {
        return "";
    }
    const dia = diaChat(ms);
    if (dia === diaChat(ahora)) {
        return formatoHora.format(ms);
    }
    if (dia === diaChat(ahora - 24 * 60 * 60_000)) {
        return "Ayer";
    }
    return sinPunto(formatoFechaCorta.format(ms));
}

/** Hora que se muestra de un mensaje: la del teléfono al escribirlo (`creado_en`) o la del servidor. */
export function horaMensaje(m: Pick<MensajeVista, "creadoEn" | "enviadoEn">): string {
    return horaChat(m.creadoEn ?? m.enviadoEn);
}

export type ElementoHilo = { tipo: "dia"; clave: string; texto: string } | { tipo: "mensaje"; clave: string; mensaje: MensajeVista; conAutor: boolean };

/**
 * El hilo para pintar: separadores de día (por la hora que se muestra) y, en grupos, el nombre del
 * autor en el primer mensaje de cada racha de otro.
 */
export function elementosHilo(mensajes: readonly MensajeVista[], grupo: boolean, ahora: number): ElementoHilo[] {
    const salida: ElementoHilo[] = [];
    let diaPrevio: string | null = null;
    let autorPrevio: number | null = null;
    for (const m of mensajes) {
        const ms = msDe(m.creadoEn ?? m.enviadoEn) ?? ahora;
        const dia = diaChat(ms);
        if (dia !== diaPrevio) {
            salida.push({ tipo: "dia", clave: `dia-${dia}`, texto: etiquetaDia(ms, ahora) });
            diaPrevio = dia;
            autorPrevio = null;
        }
        salida.push({ tipo: "mensaje", clave: m.id, mensaje: m, conAutor: grupo && !m.propio && autorPrevio !== m.autorId });
        autorPrevio = m.autorId;
    }
    return salida;
}

/** Estado de un mensaje propio al pie de la burbuja (nada para los de otros). */
export function estadoEnvio(m: MensajeVista): { texto: string; tono: "neutral" | "warning" | "error" } | null {
    if (!m.propio || m.eliminado) {
        return m.envio === "eliminando" ? { texto: "eliminando…", tono: "neutral" } : null;
    }
    switch (m.envio) {
        case "fallido":
            return { texto: "no enviado", tono: "error" };
        case "sin_senal":
            return { texto: "se envía al tener señal", tono: "warning" };
        case "enviando":
            return { texto: "enviando", tono: "neutral" };
        case "eliminando":
            return { texto: "eliminando…", tono: "neutral" };
        case "enviado":
            return { texto: m.visto ? "visto" : "enviado", tono: "neutral" };
    }
}

/** Texto de una burbuja eliminada. */
export const TEXTO_ELIMINADO = "Mensaje eliminado";

/** ¿Puede el técnico eliminar este mensaje? Propio, enviado, sin eliminar y dentro de 15 min. */
export function puedeEliminar(m: MensajeVista, ahora: number): boolean {
    const enviado = msDe(m.enviadoEn);
    return m.propio && !m.eliminado && m.envio === "enviado" && enviado !== null && ahora - enviado <= PLAZO_ELIMINAR_MS;
}

/** Vista previa de la fila: «Tú: …», «Andrea: …» (en grupos), «Foto», «Ubicación» o «Mensaje eliminado». */
export function vistaPrevia(c: ConversacionVista, usuarioId: number): string {
    const u = c.ultimo_mensaje;
    if (!u) {
        return "Sin mensajes todavía";
    }
    const cuerpo = u.contenido === "eliminado" ? TEXTO_ELIMINADO : u.contenido === "foto" ? "Foto" : u.contenido === "ubicacion" ? "Ubicación compartida" : (u.texto ?? "");
    if (u.autor_id === usuarioId) {
        return `Tú: ${cuerpo}`;
    }
    return c.tipo === "directa" ? cuerpo : `${primerNombre(u.autor_nombre)}: ${cuerpo}`;
}

export function primerNombre(nombre: string): string {
    return nombre.trim().split(/\s+/)[0] ?? nombre;
}

/** Título de la fila y del encabezado. */
export function tituloConversacion(c: Pick<ConversacionVista, "tipo" | "titulo" | "otro_usuario">): string {
    return c.tipo === "directa" ? (c.otro_usuario?.nombre ?? c.titulo) : c.titulo;
}

/** Subtítulo del encabezado de la conversación. */
export function subtituloConversacion(c: Pick<ConversacionVista, "tipo">): string {
    switch (c.tipo) {
        case "orden":
            return "Chat de la orden con despacho · el cliente no lo ve";
        case "cuadrilla":
            return "Tu cuadrilla y supervisión";
        case "directa":
            return "Mensaje directo";
    }
}

/** Contador para el punto de la isla y de la fila: «99+» arriba de 99. */
export function textoContador(n: number): string {
    return n > 99 ? "99+" : String(n);
}

export type SegmentoTexto = { tipo: "texto"; texto: string } | { tipo: "enlace"; texto: string; url: string };

const RE_ENLACE = /\bhttps?:\/\/[^\s<>"']+/gi;

/**
 * Parte el texto plano en texto y enlaces `http(s)` (nada de HTML: quien pinta lo escapa). Los
 * signos de puntuación del final no son parte del enlace. Otros esquemas (`javascript:`, `data:`)
 * nunca son enlace.
 */
export function segmentosTexto(texto: string): SegmentoTexto[] {
    const salida: SegmentoTexto[] = [];
    let desde = 0;
    for (const coincidencia of texto.matchAll(RE_ENLACE)) {
        let url = coincidencia[0];
        const recorte = /[.,;:!?)\]]+$/.exec(url);
        if (recorte) {
            url = url.slice(0, -recorte[0].length);
        }
        const inicio = coincidencia.index;
        let valida = false;
        try {
            const u = new URL(url);
            valida = u.protocol === "http:" || u.protocol === "https:";
        } catch {
            valida = false;
        }
        if (!valida) {
            continue;
        }
        if (inicio > desde) {
            salida.push({ tipo: "texto", texto: texto.slice(desde, inicio) });
        }
        salida.push({ tipo: "enlace", texto: url, url });
        desde = inicio + url.length;
    }
    if (desde < texto.length) {
        salida.push({ tipo: "texto", texto: texto.slice(desde) });
    }
    return salida;
}

/** Enlace de mapa para una ubicación compartida. */
export function enlaceUbicacion(lat: number, lon: number): string {
    return `https://www.google.com/maps/search/?api=1&query=${lat},${lon}`;
}

/** Respuestas rápidas del tablero C-Chat (se envían como texto). */
export const RESPUESTAS_RAPIDAS = ["Ya llegué", "Necesito material", "Cliente ausente"] as const;

/** Lo que el compositor deja enviar: texto recortado de 1–4000 caracteres. */
export function textoValido(texto: string): { valido: boolean; motivo: string | null } {
    const limpio = texto.trim();
    if (limpio.length === 0) {
        return { valido: false, motivo: null };
    }
    if (limpio.length > MAXIMO_TEXTO) {
        return { valido: false, motivo: `Máximo ${MAXIMO_TEXTO} caracteres (llevas ${limpio.length}).` };
    }
    return { valido: true, motivo: null };
}
