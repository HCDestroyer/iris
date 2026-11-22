# @iris/telefono

El teléfono de Iris (E4c, `.claude/integration/contratos/voz.md` §3.12): SIP sobre WebRTC contra la
Issabel con **Iris como puente**. Lo comparten el panel (`apps/web`, `componentes/telefono`) y la app
de campo (`/campo`).

## Qué hace

- `MotorTelefono` (`motor.ts`): la única fuente de verdad del teléfono de una pestaña.
  - Conecta, registra y llama.
  - Contesta, rechaza, silencia, pone en espera, manda tonos de a uno por INFO, transfiere y cuelga.
  - Detiene el agente con `4001` (otra pestaña) y `4003` (sin acceso o contraseña rechazada, que nunca se reintenta sola por fail2ban).
  - Pide credenciales TURN nuevas si vencen pronto.
  - El agente SIP real (JsSIP 3.13) se carga aparte, con import dinámico, desde `@iris/telefono/jssip`.
  - Las dependencias (`abrir`, `crearUa`, `pedirMicrofono`, `revisar`…) las pone cada app.
- `SocketIris` (`socket.ts`): el WebSocket SIP con el ticket de un uso como subprotocolo, uno nuevo por conexión.
- `coordinarPestanas` (`pestanas.ts`): una sola pestaña con `navigator.locks` y `BroadcastChannel`.
- `presentacion.ts`: el punto, los avisos, los motivos y los errores en llano; el teclado; el fin de la llamada.
- `tipos.ts`: los tipos de la API (de `@iris/contratos`) y los del teléfono en la pestaña.
- `errores.ts`: reconoce los errores de la API y del micrófono por su forma, así no depende del cliente HTTP de cada app.

No emite ni consume eventos (los emite la API).

## Pruebas

`pnpm --filter @iris/telefono test`: el motor con un agente y un WebSocket falsos, y los textos.
