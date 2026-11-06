// Chat interno del equipo en la app de campo (`contratos/chat.md`), compartido por la PWA y la app
// nativa: `@iris/sync/chat`.
export * from "./tipos";
export { AlmacenChatMemoria } from "./almacen-memoria";
export { crearTransporteChatApi } from "./http";
export { crearFlujoFetch, crearLectorSSE, RUTA_FLUJO_CHAT, SIN_LATIDO_MS, type EventoSSE, type FetchFlujo, type OpcionesFlujoFetch, type RespuestaFlujo } from "./flujo-fetch";
export {
    ErrorChatLocal,
    FLUJO_MINIMO_MS,
    MotorChat,
    vistaPreviaDe,
    type AvisoChat,
    type ConversacionVista,
    type EntradaMensaje,
    type EnvioMensaje,
    type EstadoCarga,
    type EstadoChat,
    type FotoVista,
    type MensajeVista,
    type ModoTiempoReal,
    type OpcionesMotorChat,
    type VistaConversacion,
} from "./motor";
export * from "./vista";
