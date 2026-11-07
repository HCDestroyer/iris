# Chat del equipo (panel)

Chat interno del equipo en el panel (`contratos/chat.md`): ícono con insignia en la barra superior,
panel lateral sin telón (`PanelLateral`), página `/equipo` y pestaña «Chat del equipo» de la orden.
Diseño y reglas: `docs/ui/guia.md`, «Chat del equipo en el panel».

- `proveedor.tsx`: un solo `MotorChat` (`@iris/sync/chat`) por usuario y empresa, con `chat.ver`;
  contadores de `GET /resumen`, «En vivo» / «Reconectando…», eliminar con «Deshacer» (5 s) y el
  estado del panel lateral.
- `lista.tsx`, `conversacion.tsx`, `hilo.tsx`, `compositor.tsx`: lo mismo en los tres lugares.
- `dialogos.tsx`: «Nuevo mensaje» (persona o cuadrilla) y «Agregar persona» (`chat.supervisar`).
- Lógica sin React en `lib/chat/` (presentación, eliminaciones diferidas, llamadas fuera del motor).

**Eventos:** el panel no emite eventos de negocio; los emite la API (`chat.mensaje_enviado`,
`chat.mensaje_eliminado`, `chat.mencion`, `chat.miembro_agregado`, `chat.conversacion_creada`).
**Consume** el feed del chat (`EventoChat` por SSE o `/cambios`).

Pruebas: `lib/chat/*.test.ts`, `e2e/chat-equipo.spec.ts` (API falsa en `e2e/api-chat-panel.ts`) y
capturas en `e2e/capturas-chat.spec.ts`.
