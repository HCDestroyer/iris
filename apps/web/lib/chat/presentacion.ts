// Lógica de pantalla del chat del equipo en el panel (sin React): filtros, insignias, títulos,
// menciones con @ y roles. Lo que comparte con la app de campo viene de `@iris/sync/chat`.
import { segmentosTexto, tituloConversacion, type ConversacionVista } from "@iris/sync/chat";
import { mencionesDe, ordenTipoDe, type MiembroChat, type MotivoMiembro, type ResumenChat } from "./api";

export type FiltroChat = "todas" | "directas" | "cuadrillas" | "ordenes";
export const FILTROS_CHAT: readonly FiltroChat[] = ["todas", "directas", "cuadrillas", "ordenes"];
const TIPO_DE_FILTRO = { directas: "directa", cuadrillas: "cuadrilla", ordenes: "orden" } as const;

export const filtroDeParametro = (v: string | null | undefined): FiltroChat => FILTROS_CHAT.find((f) => f === v) ?? "todas";

const normalizar = (t: string) =>
    t
        .normalize("NFD")
        .replace(/\p{Diacritic}/gu, "")
        .toLowerCase()
        .trim();

/** Título de la fila y la cabecera: la persona, la cuadrilla o «OT-000214 · Instalación». */
export function tituloChat(c: ConversacionVista): string {
    const base = tituloConversacion(c);
    const tipo = c.tipo === "orden" ? ordenTipoDe(c) : null;
    return tipo ? `${base} · ${tipo}` : base;
}

/** Conversaciones del filtro que coinciden con la búsqueda (título o último mensaje). */
export function filtrarConversaciones(lista: readonly ConversacionVista[], filtro: FiltroChat, q: string): ConversacionVista[] {
    const buscado = normalizar(q);
    return lista.filter((c) => {
        if (filtro !== "todas" && c.tipo !== TIPO_DE_FILTRO[filtro]) return false;
        if (!buscado) return true;
        return normalizar(tituloChat(c)).includes(buscado) || normalizar(c.ultimo_mensaje?.autor_nombre ?? "").includes(buscado);
    });
}

/**
 * Insignia de una conversación (§10: crece solo cuando hace falta actuar; contrato §2.1):
 * - directa: el número de no leídos;
 * - cuadrilla u orden: el número de menciones sin leer; si solo hay mensajes nuevos, un punto.
 */
export function insigniaDe(c: ConversacionVista): { numero: number; punto: boolean } {
    const noLeidos = Math.max(0, c.no_leidos);
    if (c.tipo === "directa") return { numero: noLeidos, punto: false };
    // El motor baja `no_leidos` al leer y con cada evento; las menciones nunca pasan de ahí.
    const menciones = Math.min(mencionesDe(c), noLeidos);
    return { numero: menciones, punto: menciones === 0 && noLeidos > 0 };
}

/** «Requiere acción» de lo cargado: directas sin leer + menciones (lo mismo que `GET /resumen`). */
export function requiereAccionLocal(lista: readonly ConversacionVista[]): number {
    return lista.reduce((n, c) => n + insigniaDe(c).numero, 0);
}

/** Contadores del ícono y de las pestañas: los del servidor (todas) o, sin él, los de lo cargado. */
export function contadoresFiltros(lista: readonly ConversacionVista[], resumen: ResumenChat | null): Record<FiltroChat, number> {
    if (resumen) {
        return {
            todas: resumen.requiere_accion,
            directas: resumen.por_tipo.directa.requiere_accion,
            cuadrillas: resumen.por_tipo.cuadrilla.requiere_accion,
            ordenes: resumen.por_tipo.orden.requiere_accion,
        };
    }
    const de = (tipo: ConversacionVista["tipo"]) => requiereAccionLocal(lista.filter((c) => c.tipo === tipo));
    return { todas: requiereAccionLocal(lista), directas: de("directa"), cuadrillas: de("cuadrilla"), ordenes: de("orden") };
}

/** ¿Hay algo nuevo sin número (un punto en el ícono)? */
export function hayPunto(lista: readonly ConversacionVista[]): boolean {
    return lista.some((c) => insigniaDe(c).punto);
}

/* --- Roles ---------------------------------------------------------------------------------- */

/** Rol que se muestra junto al nombre de quien escribe («Mario Ixcot · Técnico»). */
export function textoMotivo(motivo: MotivoMiembro, tipo: ConversacionVista["tipo"]): string | null {
    switch (motivo) {
        case "tecnico":
            return tipo === "orden" ? "Técnico asignado" : "Técnico";
        case "despacho":
            return "Despacho";
        case "supervision":
            return "Supervisión";
        case "agregado":
            return "Agregado por supervisión";
        case "directa":
            return null;
    }
}

/* --- Menciones ---------------------------------------------------------------------------- */

export type ConsultaMencion = { inicio: number; fin: number; texto: string };

/**
 * Lo que se está escribiendo tras una `@` antes del cursor (al inicio o después de un espacio), o
 * null. Admite nombres con espacio («@Mario Ix») hasta 30 caracteres.
 */
export function consultaMencion(texto: string, cursor: number): ConsultaMencion | null {
    const antes = texto.slice(0, cursor);
    const m = /(^|\s)@([\p{L}\p{M}.'-]*(?: [\p{L}\p{M}.'-]*)?)$/u.exec(antes);
    if (!m || m[2] === undefined || m[2].length > 30) return null;
    const inicio = antes.length - m[2].length - 1;
    return { inicio, fin: cursor, texto: m[2] };
}

/** Miembros que encajan con lo escrito tras la `@` (sin uno mismo), por nombre, hasta 6. */
export function candidatosMencion(miembros: readonly MiembroChat[], consulta: string, yo: number): MiembroChat[] {
    const q = normalizar(consulta);
    return miembros
        .filter((m) => m.usuario_id !== yo)
        .filter((m) => {
            const nombre = normalizar(m.nombre);
            return !q || nombre.startsWith(q) || nombre.split(/\s+/).some((p) => p.startsWith(q));
        })
        .sort((a, b) => a.nombre.localeCompare(b.nombre, "es"))
        .slice(0, 6);
}

/** Pone `@Nombre ` en lugar de la consulta; devuelve el texto y dónde queda el cursor. */
export function aplicarMencion(texto: string, consulta: ConsultaMencion, nombre: string): { texto: string; cursor: number } {
    const insertado = `@${nombre} `;
    const resto = texto.slice(consulta.fin).replace(/^ /, "");
    return { texto: texto.slice(0, consulta.inicio) + insertado + resto, cursor: consulta.inicio + insertado.length };
}

/** Ids de los elegidos cuyo `@Nombre` sigue en el texto (si lo borró, ya no se menciona). */
export function mencionesVigentes(texto: string, elegidos: readonly { id: number; nombre: string }[]): number[] {
    const ids = elegidos.filter((e) => texto.includes(`@${e.nombre}`)).map((e) => e.id);
    return [...new Set(ids)].slice(0, 20);
}

export type SegmentoChat = { tipo: "texto"; texto: string } | { tipo: "enlace"; texto: string; url: string } | { tipo: "mencion"; texto: string; propia: boolean };

const escaparRe = (t: string) => t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

/**
 * Texto plano en texto, enlaces y `@Nombre` de miembros (el nombre completo o, si es único, el
 * primero). `propia`: te mencionan a ti. Nunca HTML: quien pinta lo escapa.
 */
export function segmentosChat(texto: string, miembros: readonly MiembroChat[], yo: number): SegmentoChat[] {
    const primeros = new Map<string, number>();
    for (const m of miembros) {
        const p = m.nombre.trim().split(/\s+/)[0] ?? "";
        primeros.set(p, primeros.has(p) ? -1 : m.usuario_id);
    }
    const nombres: { texto: string; id: number }[] = [
        ...miembros.map((m) => ({ texto: m.nombre.trim(), id: m.usuario_id })),
        ...[...primeros].filter(([, id]) => id >= 0).map(([texto, id]) => ({ texto, id })),
    ]
        .filter((n) => n.texto.length > 0)
        .sort((a, b) => b.texto.length - a.texto.length);
    const salida: SegmentoChat[] = [];
    for (const s of segmentosTexto(texto)) {
        if (s.tipo === "enlace" || nombres.length === 0) {
            salida.push(s);
            continue;
        }
        const re = new RegExp(`@(${nombres.map((n) => escaparRe(n.texto)).join("|")})(?![\\p{L}\\p{N}])`, "gu");
        let desde = 0;
        for (const m of s.texto.matchAll(re)) {
            if (m.index > desde) salida.push({ tipo: "texto", texto: s.texto.slice(desde, m.index) });
            const id = nombres.find((n) => n.texto === m[1])?.id;
            salida.push({ tipo: "mencion", texto: m[0], propia: id === yo });
            desde = m.index + m[0].length;
        }
        if (desde < s.texto.length) salida.push({ tipo: "texto", texto: s.texto.slice(desde) });
    }
    return salida;
}
