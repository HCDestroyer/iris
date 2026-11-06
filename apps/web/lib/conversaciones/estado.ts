import type { Conversacion, Mensaje } from "./tipos";

/*
 * Estado local de la bandeja: la lista y el hilo se arman con lo leído y se actualizan con el
 * feed (un evento puede llegar dos veces o desordenado: todo se aplica por `id` y por `version`).
 */

export type Vista = "mias" | "sin_asignar" | "todas";
export type FiltroEstado = "activas" | "abierta" | "esperando_cliente" | "resuelta";

export type Filtros = {
    vista: Vista;
    estado: FiltroEstado;
    canalId: number | null;
    colaId: number | null;
    etiquetaId: number | null;
    sla: "por_vencer" | "vencido" | null;
    q: string;
};

export const FILTROS_INICIALES: Filtros = { vista: "todas", estado: "activas", canalId: null, colaId: null, etiquetaId: null, sla: null, q: "" };

const VISTAS: readonly Vista[] = ["mias", "sin_asignar", "todas"];
const ESTADOS: readonly FiltroEstado[] = ["activas", "abierta", "esperando_cliente", "resuelta"];

const idDe = (v: string | null): number | null => {
    const n = Number(v);
    return v !== null && Number.isSafeInteger(n) && n > 0 ? n : null;
};

/** Filtros desde la URL (`?vista=&estado=&canal=&cola=&etiqueta=&sla=&q=`), validados. */
export function filtrosDeUrl(p: URLSearchParams): Filtros {
    const vista = p.get("vista");
    const estado = p.get("estado");
    const sla = p.get("sla");
    return {
        vista: VISTAS.includes(vista as Vista) ? (vista as Vista) : FILTROS_INICIALES.vista,
        estado: ESTADOS.includes(estado as FiltroEstado) ? (estado as FiltroEstado) : FILTROS_INICIALES.estado,
        canalId: idDe(p.get("canal")),
        colaId: idDe(p.get("cola")),
        etiquetaId: idDe(p.get("etiqueta")),
        sla: sla === "por_vencer" || sla === "vencido" ? sla : null,
        q: (p.get("q") ?? "").slice(0, 100),
    };
}

/** Los filtros (y la conversación abierta) en la URL; lo que está por defecto no se escribe. */
export function urlDeFiltros(f: Filtros, conversacionId: number | null): string {
    const p = new URLSearchParams();
    if (f.vista !== FILTROS_INICIALES.vista) p.set("vista", f.vista);
    if (f.estado !== FILTROS_INICIALES.estado) p.set("estado", f.estado);
    if (f.canalId) p.set("canal", String(f.canalId));
    if (f.colaId) p.set("cola", String(f.colaId));
    if (f.etiquetaId) p.set("etiqueta", String(f.etiquetaId));
    if (f.sla) p.set("sla", f.sla);
    if (f.q.trim()) p.set("q", f.q.trim());
    if (conversacionId) p.set("c", String(conversacionId));
    const q = p.toString();
    return q ? `?${q}` : "";
}

/** Filtros del popover activos (número, estado, SLA, etiqueta, cola): el contador del botón. */
export function cuantosFiltros(f: Filtros): number {
    return [f.canalId, f.colaId, f.etiquetaId, f.sla, f.estado !== "activas" ? f.estado : null].filter((v) => v !== null).length;
}

/** ¿La conversación entra en la lista con estos filtros? (sin `q`: esa la resuelve el servidor) */
export function coincide(c: Conversacion, f: Filtros, usuarioId: number): boolean {
    if (f.vista === "mias" && c.asignado?.id !== usuarioId) return false;
    if (f.vista === "sin_asignar" && c.asignado) return false;
    if (f.estado === "activas" ? c.estado === "resuelta" : c.estado !== f.estado) return false;
    if (f.canalId && c.canal.id !== f.canalId) return false;
    if (f.colaId && c.cola?.id !== f.colaId) return false;
    if (f.etiquetaId && !c.etiquetas.some((e) => e.id === f.etiquetaId)) return false;
    if (f.sla && c.sla.estado !== f.sla) return false;
    return true;
}

const porActividad = (a: Conversacion, b: Conversacion) => Date.parse(b.ultimaActividad) - Date.parse(a.ultimaActividad) || b.id - a.id;

/**
 * Aplica una conversación que llegó por el feed o por una acción: reemplaza la que hay si es más
 * nueva (`version`), la agrega si ahora entra en los filtros, o la quita si ya no entra. Con una
 * búsqueda escrita solo se actualizan las que ya están (la búsqueda la hace el servidor).
 */
export function aplicarConversacion(lista: readonly Conversacion[], c: Conversacion, f: Filtros, usuarioId: number): Conversacion[] {
    const i = lista.findIndex((x) => x.id === c.id);
    const actual = i === -1 ? undefined : lista[i];
    if (actual && actual.version > c.version) return [...lista];
    const entra = coincide(c, f, usuarioId);
    if (!entra) return lista.filter((x) => x.id !== c.id);
    if (!actual && f.q.trim()) return [...lista];
    const sin = lista.filter((x) => x.id !== c.id);
    return [...sin, actual ? { ...c } : c].sort(porActividad);
}

/** Une páginas sin repetir (la más nueva gana), en orden de actividad. */
export function unirConversaciones(...listas: readonly (readonly Conversacion[])[]): Conversacion[] {
    const mapa = new Map<number, Conversacion>();
    for (const l of listas) {
        for (const c of l) {
            const previa = mapa.get(c.id);
            if (!previa || previa.version <= c.version) mapa.set(c.id, c);
        }
    }
    return [...mapa.values()].sort(porActividad);
}

/** Mensaje propio aún sin confirmar por el servidor (se muestra «enviando…» o el error). */
export type Pendiente = {
    id: string;
    modo: "responder" | "nota";
    texto: string;
    creadoEn: string;
    error: string | null;
};

/**
 * Fusiona mensajes por `id` (el más reciente gana: un `mensaje_actualizado` trae el estado de
 * entrega nuevo) y los ordena por `seq`. Nunca retrocede un estado de entrega.
 */
export function fusionarMensajes(actual: readonly Mensaje[], nuevos: readonly Mensaje[]): Mensaje[] {
    const mapa = new Map(actual.map((m) => [m.id, m]));
    for (const m of nuevos) {
        const previo = mapa.get(m.id);
        mapa.set(m.id, previo ? { ...m, estadoEntrega: entregaMayor(previo.estadoEntrega, m.estadoEntrega) } : m);
    }
    return [...mapa.values()].sort((a, b) => a.seq - b.seq);
}

const ORDEN_ENTREGA = ["en_cola", "enviado", "entregado", "leido"];

function entregaMayor(a: Mensaje["estadoEntrega"], b: Mensaje["estadoEntrega"]): Mensaje["estadoEntrega"] {
    if (b === "fallido" || a === "fallido") return b ?? a;
    if (!a) return b;
    if (!b) return a;
    return ORDEN_ENTREGA.indexOf(b) >= ORDEN_ENTREGA.indexOf(a) ? b : a;
}

/** Quita de los pendientes los que el servidor ya confirmó (mismo `id`). */
export function pendientesSinConfirmar(pendientes: readonly Pendiente[], mensajes: readonly Mensaje[]): Pendiente[] {
    const ids = new Set(mensajes.map((m) => m.id));
    return pendientes.filter((p) => !ids.has(p.id));
}
