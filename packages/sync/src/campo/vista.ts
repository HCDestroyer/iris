// Vista de la app de campo: qué pantalla se ve y a qué botón de la isla de menú pertenece.
// En la PWA cada vista va en la URL (`vistaDeUrl` / `urlDeVista`: se puede volver con «Atrás» y
// abrir un enlace directo); la app nativa usa las mismas vistas con su propia pila de «Atrás».
// Compartido con la app nativa.
import { PASOS_ORDEN, type PasoOrden } from "./pasos";

/**
 * Botones de la isla de menú (diseño «combinada», opción elegida por el PM en nov 2026):
 * Hoy · Chat · «+» · Agenda · Yo (con las iniciales). «Por recibir» es el mosaico de Hoy (con su
 * contador) y «Por enviar» se abre desde la hoja de estado y desde «Yo». Sin chat (sin `chat.ver`)
 * la isla no lleva «Chat».
 */
export type Pestana = "hoy" | "chat" | "agenda" | "yo";

/** Chips del encabezado: Hoy · Mañana · Semana. */
export type ChipDia = "hoy" | "manana" | "semana";

/** Pantallas propias dentro de «Yo» (con su dirección y su «Atrás»). */
export type SeccionYo = "contrasena" | "dos_pasos";

const SECCIONES_YO: readonly SeccionYo[] = ["contrasena", "dos_pasos"];

export type Vista =
    | { tipo: "lista" }
    /** Sin `paso`: el resumen de la orden. */
    | { tipo: "orden"; id: number; paso?: PasoOrden }
    | { tipo: "pendientes" }
    /** «Por recibir»: traslados de Argos hacia la bodega del técnico. */
    | { tipo: "recibir" }
    | { tipo: "traslado"; id: number }
    /** «Yo»; con `seccion`, su pantalla de «Cambiar contraseña» o «Verificación en dos pasos». */
    | { tipo: "yo"; seccion?: SeccionYo }
    /** «Mi bodega» (dentro de «Yo»). */
    | { tipo: "bodega" }
    /** Agenda de un día con la tira de la semana; sin `dia`, mañana. */
    | { tipo: "agenda"; dia?: string }
    /** Los 7 días desde hoy, agrupados. */
    | { tipo: "semana" }
    /** Chat interno: la lista de conversaciones. */
    | { tipo: "chat" }
    /** «Nuevo mensaje»: elegir con quién abrir una directa. */
    | { tipo: "chat_nuevo" }
    /** Una conversación del chat. */
    | { tipo: "conversacion"; id: number };

/** Botones de la isla en orden (el «+» va al centro, entre «Chat» y «Agenda»). */
export const PESTANAS: readonly { id: Pestana; texto: string; vista: Vista }[] = [
    { id: "hoy", texto: "Hoy", vista: { tipo: "lista" } },
    { id: "chat", texto: "Chat", vista: { tipo: "chat" } },
    { id: "agenda", texto: "Agenda", vista: { tipo: "agenda" } },
    { id: "yo", texto: "Yo", vista: { tipo: "yo" } },
];

/** Los botones de la isla para esta sesión: sin chat, sin «Chat». */
export function pestanasIsla(conChat: boolean): readonly { id: Pestana; texto: string; vista: Vista }[] {
    return conChat ? PESTANAS : PESTANAS.filter((p) => p.id !== "chat");
}

/** Chips del encabezado, con la vista que abren. */
export const CHIPS_DIA: readonly { id: ChipDia; texto: string; vista: Vista }[] = [
    { id: "hoy", texto: "Hoy", vista: { tipo: "lista" } },
    { id: "manana", texto: "Mañana", vista: { tipo: "agenda" } },
    { id: "semana", texto: "Semana", vista: { tipo: "semana" } },
];

const FECHA = /^\d{4}-\d{2}-\d{2}$/;

function idDe(valor: string | null): number | null {
    const n = Number(valor);
    return valor !== null && Number.isSafeInteger(n) && n > 0 ? n : null;
}

function esPaso(valor: string | null): valor is PasoOrden {
    return (PASOS_ORDEN as readonly string[]).includes(valor ?? "");
}

export function vistaDeUrl(busqueda: string): Vista {
    const p = new URLSearchParams(busqueda);
    const orden = idDe(p.get("orden"));
    if (orden !== null) {
        const paso = p.get("paso");
        return esPaso(paso) ? { tipo: "orden", id: orden, paso } : { tipo: "orden", id: orden };
    }
    const conversacion = idDe(p.get("conversacion"));
    if (conversacion !== null) {
        return { tipo: "conversacion", id: conversacion };
    }
    const traslado = idDe(p.get("traslado"));
    if (traslado !== null) {
        return { tipo: "traslado", id: traslado };
    }
    switch (p.get("vista")) {
        case "pendientes":
            return { tipo: "pendientes" };
        case "recibir":
            return { tipo: "recibir" };
        case "yo": {
            const seccion = SECCIONES_YO.find((x) => x === p.get("seccion"));
            return seccion ? { tipo: "yo", seccion } : { tipo: "yo" };
        }
        case "bodega":
            return { tipo: "bodega" };
        case "agenda": {
            const dia = p.get("dia");
            return dia && FECHA.test(dia) ? { tipo: "agenda", dia } : { tipo: "agenda" };
        }
        case "semana":
            return { tipo: "semana" };
        case "chat":
            return { tipo: "chat" };
        case "chat-nuevo":
            return { tipo: "chat_nuevo" };
        default:
            return { tipo: "lista" };
    }
}

export function urlDeVista(v: Vista): string {
    switch (v.tipo) {
        case "orden":
            return v.paso ? `/campo?orden=${v.id}&paso=${v.paso}` : `/campo?orden=${v.id}`;
        case "traslado":
            return `/campo?traslado=${v.id}`;
        case "conversacion":
            return `/campo?conversacion=${v.id}`;
        case "chat_nuevo":
            return "/campo?vista=chat-nuevo";
        case "lista":
            return "/campo";
        case "yo":
            return v.seccion ? `/campo?vista=yo&seccion=${v.seccion}` : "/campo?vista=yo";
        case "agenda":
            return v.dia ? `/campo?vista=agenda&dia=${v.dia}` : "/campo?vista=agenda";
        default:
            return `/campo?vista=${v.tipo}`;
    }
}

/**
 * Botón de la isla al que pertenece una vista (`null`: «Por enviar», que no está en la isla). «Por
 * recibir» y sus traslados se abren desde el mosaico de Hoy.
 */
export function pestanaDe(v: Vista): Pestana | null {
    switch (v.tipo) {
        case "lista":
        case "orden":
            return "hoy";
        case "recibir":
        case "traslado":
            return "hoy";
        case "pendientes":
            return null;
        case "agenda":
        case "semana":
            return "agenda";
        case "yo":
        case "bodega":
            return "yo";
        case "chat":
        case "chat_nuevo":
        case "conversacion":
            return "chat";
    }
}

/**
 * Chip del encabezado que va marcado: «Hoy» en la lista, «Mañana» en la agenda de mañana y
 * «Semana» en la semana. Otro día de la tira no marca ningún chip; fuera de estas vistas, `null`.
 */
export function chipDe(v: Vista, manana: string): ChipDia | null {
    switch (v.tipo) {
        case "lista":
            return "hoy";
        case "agenda":
            return v.dia === undefined || v.dia === manana ? "manana" : null;
        case "semana":
            return "semana";
        default:
            return null;
    }
}

/** ¿Lleva el encabezado completo (saludo y chips)? Hoy, la agenda y la semana. */
export function esVistaDeDia(v: Vista): boolean {
    return v.tipo === "lista" || v.tipo === "agenda" || v.tipo === "semana";
}

/** ¿Es la misma vista? (misma pantalla, misma orden y mismo paso). */
export function mismaVista(a: Vista, b: Vista): boolean {
    return urlDeVista(a) === urlDeVista(b);
}

/** Paso anterior y siguiente de la orden («Anterior» / «Siguiente»); `null` en los extremos. */
export function vecinosPaso(paso: PasoOrden): { anterior: PasoOrden | null; siguiente: PasoOrden | null } {
    const i = PASOS_ORDEN.indexOf(paso);
    return { anterior: PASOS_ORDEN[i - 1] ?? null, siguiente: PASOS_ORDEN[i + 1] ?? null };
}
