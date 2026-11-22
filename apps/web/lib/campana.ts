import type { Schema } from "@iris/contratos";
import { Bell, BriefcaseBusiness, Gauge, RefreshCw, Server, ShieldAlert, Sparkles, type LucideIcon } from "lucide-react";
import { textoPausado } from "./asistente/presentacion";

/*
 * Avisos personales de la campana del panel (`GET /yo/avisos`, contratos/conversaciones.md §16.9).
 * `asistente_tope`: el asistente de IA llegó al 80 % (`alerta`) o al 100 % (`peligro`, se pausó) del
 * tope del mes. `servidor_ia_caido`, `servidor_ia_incompatible` y `servidor_ia_umbral`
 * (contratos/voz.md §4.12): uno por episodio, a quien administra la instalación. `argos_certificado`
 * (contrato con Argos 0.8.0 §1.5): cambió el certificado de la API de Argos; solo informa. `venta_revisada`
 * (0.10.0 §3.4.8, B20): la instalación cambió el plan o el titular de una venta del vendedor. El texto
 * (`titulo`) lo arma el servidor; aquí solo cómo se pinta.
 */

export type AvisoPanel = Schema<"AvisoPanel">;

/** Tono del aviso (color del icono y del punto). */
export function tonoAvisoPanel(nivel: AvisoPanel["nivel"]): "info" | "warning" | "error" {
    return nivel === "peligro" ? "error" : nivel === "alerta" ? "warning" : "info";
}

const ICONO_TIPO: Record<AvisoPanel["tipo"], LucideIcon> = { asistente_tope: Sparkles, servidor_ia_caido: Server, servidor_ia_incompatible: RefreshCw, servidor_ia_umbral: Gauge, argos_certificado: ShieldAlert, venta_revisada: BriefcaseBusiness };

/** El icono del tipo; uno que este panel aún no conoce (o un nombre heredado como «constructor») lleva la campana. */
export function iconoAvisoPanel(tipo: AvisoPanel["tipo"]): LucideIcon {
    return Object.hasOwn(ICONO_TIPO, tipo) ? ICONO_TIPO[tipo] : Bell;
}

const numero = (v: unknown): number | null => (typeof v === "number" && Number.isFinite(v) ? Math.round(v) : null);

/** Segunda línea de un aviso de umbral del Servidor de IA, con `datos: { tipo, valor, umbral }`. */
function detalleUmbral(datos: AvisoPanel["datos"]): string {
    const valor = numero(datos["valor"]);
    const umbral = numero(datos["umbral"]);
    switch (datos["tipo"]) {
        case "vram":
            return `${valor === null ? "La memoria de video pasó del umbral" : `La memoria de video está al ${valor} %`}${umbral === null ? "" : ` (avisa al ${umbral} %)`}. Si se llena, las respuestas fallan.`;
        case "disco":
            return `${valor === null ? "El disco de los modelos está casi lleno" : `El disco de los modelos está al ${valor} %`}. Quita en el servidor los modelos que ya no se usan.`;
        case "temperatura":
            return `${valor === null ? "La tarjeta gráfica se calentó de más" : `La tarjeta gráfica llegó a ${valor} °C`}. Revisa la ventilación del servidor.`;
        case "saturacion":
            return `${valor === null ? "Se rechazaron peticiones" : `Se rechazaron ${valor} ${valor === 1 ? "petición" : "peticiones"}`} porque el servidor estaba lleno. No hay cola: pasan a una persona.`;
        default:
            return "Mira el tablero del servidor de IA.";
    }
}

/** Mes `YYYY-MM` de `datos.mes`, o `null` si no vino bien. */
function mesDe(datos: AvisoPanel["datos"]): string | null {
    const mes = datos["mes"];
    return typeof mes === "string" && /^\d{4}-\d{2}$/.test(mes) ? mes : null;
}

/** Segunda línea del aviso: al 100 %, hasta cuándo queda pausado; al 80 %, qué pasa si sigue. */
export function detalleAvisoPanel(a: Pick<AvisoPanel, "tipo" | "nivel" | "datos">): string | null {
    const tipo = a.tipo;
    // Los del Servidor de IA se reactivan si el problema se repite: `datos.veces` dice cuántas en la hora.
    const veces = numero(a.datos["veces"]);
    const repetido = veces !== null && veces > 1 ? ` Ya van ${veces} veces en esta hora.` : "";
    if (tipo === "servidor_ia_caido") return `Lo que el asistente atendía solo pasa a personas hasta que vuelva. Iris lo sigue intentando sola.${repetido}`;
    if (tipo === "servidor_ia_incompatible") return `El asistente no lo usa hasta actualizar. El tablero dice qué hay que actualizar.${repetido}`;
    if (tipo === "servidor_ia_umbral") return `${detalleUmbral(a.datos)}${repetido}`;
    if (tipo === "argos_certificado") return "Lo normal es la renovación automática del certificado. Iris no detuvo nada; si no la esperabas, revisa el servidor de Argos.";
    if (tipo !== "asistente_tope") return null;
    if (a.nivel === "peligro") {
        const mes = mesDe(a.datos);
        return mes ? textoPausado(mes) : "Pausado hasta el mes siguiente o hasta que subas el tope.";
    }
    return "Al llegar al 100 % deja de sugerir. Puedes subir el tope.";
}

/** Caracteres de control y barra invertida: el navegador los quita o los lee como «/» al resolver. */
function conNoPermitidos(texto: string): boolean {
    for (let i = 0; i < texto.length; i++) {
        const c = texto.charCodeAt(i);
        if (c <= 0x1f || c === 0x5c) return true;
    }
    return false;
}

/** Origen con el que se resuelve el enlace fuera del navegador (pruebas, servidor). */
const ORIGEN_LOCAL = "http://iris.invalid";

/**
 * Solo rutas internas del panel («/administracion/asistente»): el enlace se resuelve como lo haría
 * el navegador y debe quedar en el mismo origen. Se devuelve la ruta ya resuelta (ruta, parámetros
 * y ancla), nunca el texto que llegó. Otro origen, `//host`, `/\host` o un carácter de control: `null`.
 */
export function enlaceAvisoPanel(enlace: string | null, origen: string = typeof location === "undefined" ? ORIGEN_LOCAL : location.origin): string | null {
    if (!enlace || !enlace.startsWith("/") || conNoPermitidos(enlace)) return null;
    try {
        const base = new URL(origen);
        const url = new URL(enlace, base);
        if (url.origin !== base.origin) return null;
        return `${url.pathname}${url.search}${url.hash}`;
    } catch {
        return null;
    }
}

/**
 * La campana sondea solo con una lectura buena a la vista: mientras carga o está en error no se
 * insiste (con la sesión vencida cada intento sería otro 401); «Reintentar» lo reanuda.
 */
export function sondeoActivo(estado: "cargando" | "error" | "listo"): boolean {
    return estado === "listo";
}

/** La lectura falló porque la sesión venció (401): hay que entrar otra vez. */
export function sesionVencida(error: unknown): boolean {
    return typeof error === "object" && error !== null && "estado" in error && error.estado === 401;
}

/** El punto de la campana crece solo cuando hay que actuar: un aviso sin leer de nivel `peligro`. */
export function hayQueActuar(avisos: readonly Pick<AvisoPanel, "leido" | "nivel">[]): boolean {
    return avisos.some((a) => !a.leido && a.nivel === "peligro");
}

/** Marca un aviso como leído en la lista local (la respuesta del servidor puede tardar). */
export function marcarLeido(avisos: readonly AvisoPanel[], id: number, ahora: string): AvisoPanel[] {
    return avisos.map((a) => (a.id === id && !a.leido ? { ...a, leido: true, leidoEn: ahora } : a));
}
