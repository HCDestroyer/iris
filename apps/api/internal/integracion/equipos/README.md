# equipos (integración)

Cambios de Wi-Fi del equipo del cliente pedidos a Argos (contrato `argosisp.md` 0.9.0 §12.4, B12 y B14).
Argos es el único que habla con SmartOLT y el ACS: Iris nunca guarda sus credenciales.

## Qué hace

- `Encolar` (en la transacción del caso de uso que lo origina: la orden, el ticket o el formulario del
  enlace del asistente): valida SSID (1 a 32) y clave (8 a 63, ASCII imprimible), aplica el límite de 3
  cambios por servicio al día (hora de Guatemala; 429 `demasiados_cambios_wifi` con `Retry-After`, con
  candado por servicio) y encola `POST /servicios/:id/wifi` con `iris:wifi:<orden|ticket|conversacion>:<id>:<uuid>`,
  `X-Iris-Actor` (`usuario:<id>` o `asistente_ia`; desde una conversación también `X-Iris-Conversacion`) y
  `verificacion { metodo, verificado_en, cliente_id }`.
- **La clave** va solo al depósito de tránsito cifrado de la cola (`integracion.datos_transito`, con la
  entidad en los datos asociados) y se borra al 2xx o al rechazo de Argos. `integracion.cambios_wifi`
  guarda solo si se pidió SSID o clave, nunca sus valores.
- Resultado: el 202 de la cola (`en_proceso`, con `solicitud_id`), su rechazo (`fallido` con el código de
  Argos) y el webhook `equipo.wifi_resuelto` (`argos.wifi_resuelto`: `aplicado` o `fallido` con `causa`).
  Un cambio resuelto no cambia (webhook repetido o tardío).

## Eventos

Emite `wifi.cambio_pedido` y `wifi.cambio_resuelto` (nunca el SSID ni la clave). Consume
`integracion.pendiente_hecho`, `_fallido` y `_resuelto_manual` (operación `cambiar_wifi`) y
`argos.wifi_resuelto`.
