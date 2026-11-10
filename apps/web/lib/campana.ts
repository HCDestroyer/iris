import type { Schema } from "@iris/contratos";
import { Bell, Sparkles, type LucideIcon } from "lucide-react";
import { textoPausado } from "./asistente/presentacion";

/*
 * Avisos personales de la campana del panel (`GET /yo/avisos`, contratos/conversaciones.md §16.9).
 * Hoy solo `asistente_tope`: el asistente de IA llegó al 80 % (`alerta`) o al 100 % (`peligro`, se
 * pausó) del tope del mes. El texto (`titulo`) lo arma el servidor; aquí solo cómo se pinta.
 */

export type AvisoPanel = Schema<"AvisoPanel">;

/** Tono del aviso (color del icono y del punto). */
export function tonoAvisoPanel(nivel: AvisoPanel["nivel"]): "info" | "warning" | "error" {
    return nivel === "peligro" ? "error" : nivel === "alerta" ? "warning" : "info";
}

export function iconoAvisoPanel(tipo: AvisoPanel["tipo"]): LucideIcon {
    return tipo === "asistente_tope" ? Sparkles : Bell;
}

/** Mes `YYYY-MM` de `datos.mes`, o `null` si no vino bien. */
function mesDe(datos: AvisoPanel["datos"]): string | null {
    const mes = datos["mes"];
    return typeof mes === "string" && /^\d{4}-\d{2}$/.test(mes) ? mes : null;
}

/** Segunda línea del aviso: al 100 %, hasta cuándo queda pausado; al 80 %, qué pasa si sigue. */
export function detalleAvisoPanel(a: Pick<AvisoPanel, "tipo" | "nivel" | "datos">): string | null {
    if (a.tipo !== "asistente_tope") return null;
    if (a.nivel === "peligro") {
        const mes = mesDe(a.datos);
        return mes ? textoPausado(mes) : "Pausado hasta el mes siguiente o hasta que subas el tope.";
    }
    return "Al llegar al 100 % deja de sugerir. Puedes subir el tope.";
}

/** Solo rutas internas del panel («/administracion/asistente»): nunca otro origen. */
export function enlaceAvisoPanel(enlace: string | null): string | null {
    if (!enlace || !enlace.startsWith("/") || enlace.startsWith("//") || enlace.includes("\\")) return null;
    return enlace;
}

/** El punto de la campana crece solo cuando hay que actuar: un aviso sin leer de nivel `peligro`. */
export function hayQueActuar(avisos: readonly Pick<AvisoPanel, "leido" | "nivel">[]): boolean {
    return avisos.some((a) => !a.leido && a.nivel === "peligro");
}

/** Marca un aviso como leído en la lista local (la respuesta del servidor puede tardar). */
export function marcarLeido(avisos: readonly AvisoPanel[], id: number, ahora: string): AvisoPanel[] {
    return avisos.map((a) => (a.id === id && !a.leido ? { ...a, leido: true, leidoEn: ahora } : a));
}
