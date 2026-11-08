// Formas del chat interno del equipo (`contratos/chat.md`): las del cable vienen de
// `@iris/contratos` (operaciones `*Chat`); aquí solo lo local del teléfono (cola de salida, caché y
// las interfaces que se inyectan: almacén, transporte y flujo SSE).
import type { Schema } from "@iris/contratos";
import type { CambiosTabla } from "../almacen";
import type { ErrorTransporte } from "../errores";
import type { RespuestaApi } from "../transporte";

export type ConversacionChat = Schema<"ConversacionChat">;
export type ConversacionChatDetalle = Schema<"ConversacionChatDetalle">;
export type MensajeChat = Schema<"MensajeChat">;
export type EventoChat = Schema<"EventoChat">;
export type CambiosChat = Schema<"CambiosChat">;
export type CuerpoEnviarMensaje = Schema<"CuerpoEnviarMensaje">;
export type LecturaMiembroChat = Schema<"LecturaMiembroChat">;
export type LecturaChat = Schema<"LecturaChat">;
export type UbicacionChat = Schema<"UbicacionChat">;
export type VistaPreviaChat = Schema<"VistaPreviaChat">;
export type PaginaMensajeChat = Schema<"PaginaMensajeChat">;
export type PaginaConversacionChat = Schema<"PaginaConversacionChat">;
/** Alguien con quien abrir una directa (`GET /chat/personas`). */
export type PersonaChat = Schema<"PersonaChat">;
export type PaginaPersonaChat = Schema<"PaginaPersonaChat">;
export type TipoConversacion = ConversacionChat["tipo"];

/** Error que se muestra (nunca con el texto del mensaje). */
export interface ErrorChat {
    mensaje: string;
    status: number | null;
    codigo: string | null;
}

/**
 * Foto de un mensaje en la cola: alta (`POST /adjuntos`, entidad `chat_conversacion`), luego el
 * mensaje con su `adjunto_id` y después la subida tus (el mensaje puede salir antes, §3).
 */
export interface FotoSalida {
    id: string;
    mime: string;
    tamano: number;
    sha256: string;
    /** `por_crear` → `creada` (alta hecha) → `subida` (bytes completos en el servidor). */
    fase: "por_crear" | "creada" | "subida";
    /** Bytes confirmados por el servidor (`Upload-Offset`). */
    offset: number;
    tomadoEn: string | null;
}

/**
 * Lo que el técnico hizo y falta mandar: un mensaje (`enviar`, la clave es el id del mensaje) o
 * eliminar uno ya enviado (`eliminar`, clave `eliminar:<id>`). Sale en orden de `secuencia`.
 */
export interface SalidaChat {
    id: string;
    tipo: "enviar" | "eliminar";
    conversacionId: number;
    /** Orden de creación en el teléfono. */
    secuencia: number;
    mensajeId: string;
    texto?: string;
    ubicacion?: UbicacionChat;
    /** Ids de los mencionados (§3.2): hasta 20, sin repetir; solo con texto. */
    menciones?: number[];
    foto?: FotoSalida;
    /** Hora del teléfono al escribirlo (ISO). */
    creadoEn: string;
    /** `fallido`: el servidor no lo acepta (403, 404, 409, 422) o el técnico debe decidir. */
    estado: "pendiente" | "fallido";
    /** El servidor ya tiene el mensaje: solo falta terminar de subir la foto. */
    enviado: boolean;
    intentos: number;
    error: ErrorChat | null;
}

export interface MetaChat {
    /** Cursor del feed (`nextCursor` de `/cambios` o el id del último evento SSE). */
    cursor: string | null;
    /** Conversaciones cuyo último tramo de mensajes está en la caché (sin hueco hasta el final). */
    cargadas: number[];
    /** Siguiente `secuencia` de la cola de salida. */
    secuencia: number;
}

export const META_CHAT_INICIAL: MetaChat = { cursor: null, cargadas: [], secuencia: 1 };

/** Escritura atómica de la caché y la cola del chat. */
export interface LoteChat {
    salida?: CambiosTabla<SalidaChat>;
    conversaciones?: CambiosTabla<ConversacionChat, number>;
    mensajes?: CambiosTabla<MensajeChat>;
    bytes?: CambiosTabla<{ id: string; datos: Uint8Array }>;
    meta?: Partial<MetaChat>;
}

export interface ContenidoChat {
    salida: SalidaChat[];
    conversaciones: ConversacionChat[];
    mensajes: MensajeChat[];
    meta: MetaChat;
}

/**
 * Persistencia del chat, en la misma base del usuario y la empresa que la cola de sync (IndexedDB en
 * la PWA, SQLite en la app nativa). `borrarCacheChat` deja la cola de salida (y sus fotos).
 */
export interface AlmacenChat {
    cargarChat(): Promise<ContenidoChat>;
    escribirChat(lote: LoteChat): Promise<void>;
    leerBytesChat(id: string): Promise<Uint8Array | null>;
    /** Borra conversaciones, mensajes y el cursor (cerrar sesión, «¿No eres tú?», 410). */
    borrarCacheChat(): Promise<void>;
}

/** Página de mensajes pedida: `anteriores` hacia atrás (defecto) o `siguientes` desde un `seq`. */
export interface ConsultaMensajes {
    direccion?: "anteriores" | "siguientes";
    cursor?: string;
    desdeSeq?: number;
    limit?: number;
}

/** Respuesta de `POST /mensajes`: el mensaje del servidor y si era un reenvío (200). */
export interface MensajeEnviado {
    mensaje: MensajeChat;
    reenvio: boolean;
}

/** Llamadas a `/api/v1/mensajeria/chat` y al alta de fotos. Toda falla es `ErrorTransporte`. */
export interface TransporteChat {
    listarConversaciones(cursor?: string): Promise<PaginaConversacionChat>;
    verConversacion(id: number): Promise<ConversacionChatDetalle>;
    /** «Nuevo mensaje» → Persona: personas activas de la empresa con `chat.ver` (sin ti), por nombre. */
    personas(q: string, cursor?: string): Promise<PaginaPersonaChat>;
    abrirDirecta(usuarioId: number): Promise<ConversacionChatDetalle>;
    abrirCuadrilla(cuadrillaId: number): Promise<ConversacionChatDetalle>;
    abrirOrden(ordenId: number): Promise<ConversacionChatDetalle>;
    listarMensajes(conversacionId: number, consulta: ConsultaMensajes): Promise<PaginaMensajeChat>;
    enviarMensaje(conversacionId: number, cuerpo: CuerpoEnviarMensaje): Promise<MensajeEnviado>;
    eliminarMensaje(mensajeId: string): Promise<MensajeChat>;
    leer(conversacionId: number, hastaMensajeId: string): Promise<{ no_leidos: number; leido_hasta_mensaje_id: string | null }>;
    cambios(cursor: string | null): Promise<RespuestaApi<CambiosChat>>;
}

/** Lo que el flujo SSE avisa a quien lo abrió. */
export interface OyentesFlujo {
    alAbrir(): void;
    alEvento(evento: EventoChat): void;
    /**
     * El flujo terminó (el servidor lo cerró, se cortó la red o respondió un error). Quien ve el
     * estado HTTP (el flujo con `fetch` de la app nativa) pasa el error: 401/403 detienen el chat y
     * 429/503 esperan; `EventSource` no lo sabe y no pasa nada (el sondeo que sigue lo descubre).
     */
    alCerrar(error?: ErrorTransporte): void;
}

export interface FlujoChat {
    cerrar(): void;
}

/**
 * Abre `GET /chat/eventos` desde `desde` (o desde ahora). La PWA usa `EventSource`; la app nativa,
 * `crearFlujoFetch` sobre `expo/fetch` (lee el cuerpo por partes). Sin flujo se sondea.
 */
export type AbrirFlujoChat = (desde: string | null, oyentes: OyentesFlujo) => FlujoChat;
