// Notificaciones push de la app (`contratos/push.md`): el mensaje de FCM (solo datos) leído y
// validado, los canales de Android, los enlaces `iris://` y cómo se arma cada notificación. Sin
// módulos nativos: lo usan la tarea en segundo plano, la app al frente y las pruebas.
import type { Vista } from "@iris/sync/campo";

export const TIPOS_PUSH = ["chat", "mencion", "orden", "bandeja"] as const;
export type TipoPush = (typeof TIPOS_PUSH)[number];

/** Canales de Android (el id es el `canal` del mensaje). El nombre lo ve el técnico en Ajustes. */
export const CANALES: readonly { id: TipoPush; nombre: string; descripcion: string; alta: boolean }[] = [
    { id: "chat", nombre: "Mensajes del equipo", descripcion: "Directos, de tu cuadrilla y de tus órdenes", alta: true },
    { id: "mencion", nombre: "Menciones", descripcion: "Cuando alguien te menciona con @", alta: true },
    { id: "orden", nombre: "Órdenes", descripcion: "Asignadas a ti o reprogramadas", alta: true },
    { id: "bandeja", nombre: "Bandeja de clientes", descripcion: "Clientes esperando que alguien los atienda", alta: false },
];

/** Acciones del contrato (§5) y su botón. */
export const ACCIONES = {
    responder: "Responder",
    leido: "Marcar como leído",
    ver_orden: "Ver orden",
    como_llegar: "Cómo llegar",
    ver: "Ver",
} as const;
export type AccionPush = keyof typeof ACCIONES;

export interface Remitente {
    id: number;
    nombre: string;
    iniciales: string;
}

/** Lo propio de cada tipo (`datos`), solo lo que la app usa. */
export type DatosPush =
    | { tipo: "chat"; conversacionId: number; mensajeId: string; remitente: Remitente | null }
    | { tipo: "mencion"; origen: "bandeja" | "chat"; conversacionId: number; mensajeId: string; remitente: Remitente | null }
    | { tipo: "orden"; ordenId: number; hoy: boolean }
    | { tipo: "bandeja"; conversacionId: number };

export interface MensajePush {
    id: string;
    tipo: TipoPush;
    usuarioId: number;
    /** `null` = sin conector (sin empresa). */
    empresaId: number | null;
    titulo: string;
    cuerpo: string;
    publicoTitulo: string;
    grupo: string;
    etiqueta: string;
    enlace: string;
    acciones: AccionPush[];
    ocurridoEn: string;
    datos: DatosPush;
}

/** Destino al tocar: una pantalla de la app o, la bandeja de clientes, el panel web. */
export type DestinoPush = { tipo: "vista"; vista: Vista } | { tipo: "panel"; ruta: string };

const ID = /^[1-9]\d{0,15}$/;
const ENLACE = /^iris:\/\/(chat|orden|conversaciones)\/([1-9]\d{0,15})$/;
/** Lo que se acepta de un texto del mensaje (título, cuerpo): sin controles y acotado. */
const MAX_TEXTO = 600;

function idValido(v: unknown): number | null {
    const s = typeof v === "number" ? String(v) : typeof v === "string" ? v.trim() : "";
    if (!ID.test(s)) {
        return null;
    }
    const n = Number(s);
    return Number.isSafeInteger(n) ? n : null;
}

function texto(v: unknown, maximo = MAX_TEXTO): string {
    if (typeof v !== "string") {
        return "";
    }
    // eslint-disable-next-line no-control-regex
    const limpio = v.replace(/[\u0000-\u0009\u000b-\u001f\u007f]/g, "").trim();
    return limpio.length > maximo ? `${limpio.slice(0, maximo - 1)}…` : limpio;
}

/**
 * `iris://chat/{id}`, `iris://orden/{id}` y `iris://conversaciones/{id}` → destino. Cualquier otra
 * cosa (otro esquema, otra ruta, un id que no es un entero positivo, algo de más) → `null`: nunca se
 * navega a una ruta arbitraria.
 */
export function destinoDeEnlace(enlace: unknown): DestinoPush | null {
    if (typeof enlace !== "string") {
        return null;
    }
    const m = ENLACE.exec(enlace.trim());
    const id = m ? idValido(m[2]) : null;
    if (!m || id === null) {
        return null;
    }
    switch (m[1]) {
        case "chat":
            return { tipo: "vista", vista: { tipo: "conversacion", id } };
        case "orden":
            return { tipo: "vista", vista: { tipo: "orden", id } };
        default:
            return { tipo: "panel", ruta: `/conversaciones/${id}` };
    }
}

function remitenteDe(v: unknown): Remitente | null {
    const r = v as Partial<Remitente> | null;
    const id = idValido(r?.id);
    return r && id !== null ? { id, nombre: texto(r.nombre, 80), iniciales: texto(r.iniciales, 3).toLocaleUpperCase("es") } : null;
}

function datosDe(tipo: TipoPush, crudo: unknown): DatosPush | null {
    let d: Record<string, unknown>;
    try {
        d = typeof crudo === "string" ? (JSON.parse(crudo) as Record<string, unknown>) : ((crudo ?? {}) as Record<string, unknown>);
    } catch {
        return null;
    }
    if (typeof d !== "object" || d === null) {
        return null;
    }
    switch (tipo) {
        case "chat": {
            const conversacionId = idValido(d.conversacionId);
            return conversacionId === null ? null : { tipo, conversacionId, mensajeId: texto(d.mensajeId, 64), remitente: remitenteDe(d.remitente) };
        }
        case "mencion": {
            const conversacionId = idValido(d.conversacionId);
            const origen = d.origen === "bandeja" ? "bandeja" : d.origen === "chat" ? "chat" : null;
            return conversacionId === null || origen === null ? null : { tipo, origen, conversacionId, mensajeId: texto(d.mensajeId, 64), remitente: remitenteDe(d.remitente) };
        }
        case "orden": {
            const ordenId = idValido(d.ordenId);
            return ordenId === null ? null : { tipo, ordenId, hoy: d.hoy === true };
        }
        case "bandeja": {
            const conversacionId = idValido(d.conversacionId);
            return conversacionId === null ? null : { tipo, conversacionId };
        }
    }
}

/**
 * El `data` de FCM (todas las claves son texto) → `MensajePush`. `null` si no es de este contrato
 * (otra `v`, otro tipo, sin destinatario o con un enlace que no es de Iris).
 */
export function leerMensajePush(data: Record<string, unknown> | null | undefined): MensajePush | null {
    if (!data || String(data.v ?? "") !== "1") {
        return null;
    }
    const tipo = TIPOS_PUSH.find((t) => t === data.tipo);
    const usuarioId = idValido(data.usuarioId);
    const id = texto(data.id, 64);
    if (!tipo || usuarioId === null || !id || !destinoDeEnlace(data.enlace)) {
        return null;
    }
    const datos = datosDe(tipo, data.datos);
    if (!datos) {
        return null;
    }
    const empresa = data.empresaId === undefined || data.empresaId === "" || data.empresaId === null ? null : idValido(data.empresaId);
    if (empresa === null && data.empresaId !== undefined && data.empresaId !== "" && data.empresaId !== null) {
        return null;
    }
    const acciones = String(data.acciones ?? "")
        .split(",")
        .map((a) => a.trim())
        .filter((a): a is AccionPush => a in ACCIONES);
    return {
        id,
        tipo,
        usuarioId,
        empresaId: empresa,
        titulo: texto(data.titulo, 120),
        cuerpo: texto(data.cuerpo),
        publicoTitulo: texto(data.publicoTitulo, 80),
        grupo: texto(data.grupo, 80) || `${tipo}-${id}`,
        etiqueta: texto(data.etiqueta, 120) || id,
        enlace: String(data.enlace).trim(),
        acciones,
        ocurridoEn: texto(data.ocurridoEn, 40),
        datos,
    };
}

/** Categoría de botones de Android (`setNotificationCategoryAsync`) para las acciones del mensaje. */
export function categoriaDe(m: Pick<MensajePush, "acciones">): string | null {
    const clave = [...m.acciones].sort().join("+");
    return clave ? `iris-${clave}` : null;
}

/** Categorías posibles: las combinaciones que manda el servidor (§4). */
export const CATEGORIAS: readonly AccionPush[][] = [["responder", "leido"], ["ver_orden", "como_llegar"], ["ver"]];

/** Líneas que se guardan por grupo (las últimas): una conversación muestra hasta estas. */
export const LINEAS_GRUPO = 6;

export interface NotificacionArmada {
    /** Identificador de la notificación: el `grupo` (una por conversación u orden). */
    identificador: string;
    canal: TipoPush;
    titulo: string;
    cuerpo: string;
    categoria: string | null;
    /** Las líneas que quedan guardadas para el grupo. */
    lineas: string[];
}

/**
 * Cómo se ve: una notificación por `grupo` (conversación u orden). En el chat y las menciones, los
 * mensajes del grupo se apilan («Cuadrilla Norte · 2 mensajes», una línea por mensaje): es lo más
 * cercano a `MessagingStyle` que permite `expo-notifications`. Una orden reemplaza a la anterior.
 */
export function armarNotificacion(m: MensajePush, lineasPrevias: readonly string[]): NotificacionArmada {
    const apila = m.tipo === "chat" || m.tipo === "mencion";
    const lineas = apila ? [...lineasPrevias, m.cuerpo].filter(Boolean).slice(-LINEAS_GRUPO) : [m.cuerpo];
    const n = apila ? lineasPrevias.length + 1 : 1;
    return {
        identificador: m.grupo,
        canal: m.tipo,
        titulo: n > 1 ? `${m.titulo} · ${n} mensajes` : m.titulo,
        cuerpo: lineas.join("\n"),
        categoria: categoriaDe(m),
        lineas: apila ? lineas : [],
    };
}

/**
 * Con la app al frente no se avisa el chat que se está viendo (el mensaje ya aparece en el hilo).
 * Lo demás (una orden reprogramada, la bandeja) sí se avisa.
 */
export function seEstaViendo(m: MensajePush, vista: Vista | null): boolean {
    if (!vista || vista.tipo !== "conversacion") {
        return false;
    }
    const delChat = m.datos.tipo === "chat" || (m.datos.tipo === "mencion" && m.datos.origen === "chat");
    return delChat && "conversacionId" in m.datos && vista.id === m.datos.conversacionId;
}
