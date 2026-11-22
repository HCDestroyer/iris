// @iris/telefono: el teléfono de Iris (E4c, `.claude/integration/contratos/voz.md` §3.12). El motor
// SIP sobre WebRTC con Iris como puente, el WebSocket con ticket, una sola pestaña y los textos en
// llano, compartidos por el panel y la app de campo (`/campo`). JsSIP se carga aparte, con import
// dinámico, desde `@iris/telefono/jssip`.
export * from "./errores";
export * from "./motor";
export * from "./pestanas";
export * from "./presentacion";
export * from "./socket";
export * from "./tipos";
