# Voz E1: guion de la prueba manual con un número de Meta

Para cuando haya un **número de prueba de Meta con Calling habilitado** (contrato `voz.md` §9). Las
pruebas automáticas usan un doble de la Graph API fiel a los supuestos de §10.1; este guion valida
esos supuestos contra Meta de verdad. Anota en cada paso lo que difiera: el cliente real está en un
solo sitio (`graphHTTP`, `apps/api/internal/mensajeria/voz_modelo.go`) y la forma del webhook en
`metaLlamada`.

## Antes de empezar

1. Número conectado **por API** en Iris (no por QR), con su webhook verificado.
2. En la app de Meta, suscribir el campo **`calls`** del webhook de WhatsApp.
3. Límite de mensajería del número ≥ 2 000 y Calling habilitado en WhatsApp Manager.
4. Un usuario con rol `supervisor` (para configurar) y otro con rol `soporte`, miembro de la cola del
   número.

## 1. Requisitos y encendido

1. `POST /api/v1/mensajeria/canales/{id}/llamadas/comprobar`. Esperado: `requisitos.cumple: true`.
   **Supuesto a validar:** de dónde salen de verdad `calling_habilitado`, `limite_mensajeria` y
   `webhook_calls` (hoy `GET /{phone_number_id}/settings` y `?fields=messaging_limit_tier`).
2. `POST …/llamadas/configurar` con `llamadasActivas: true`. Comprobar en WhatsApp Manager que el
   icono de llamada aparece en el chat del número.

## 2. Contestar una entrante

1. El agente abre el panel, se pone disponible y enciende «Recibir llamadas».
2. Desde un teléfono, llamar al número por WhatsApp.
3. Esperado en menos de 2 s: la llamada suena en el panel. Guardar el **cuerpo crudo del webhook**
   (`mensajeria.entrantes`) y comparar con `metaLlamada`: `id`, `from`, `event: connect`,
   `timestamp`, `direction`, `session.sdp`. ¿Trae el nombre de perfil?
4. Contestar. Esperado: audio en los dos sentidos en menos de 2 s desde `confirmar-medio`.
   Probar en Chrome, Firefox, Safari y Chrome en Android. Anotar si hizo falta TURN.
5. Colgar desde el panel: el teléfono corta. Repetir colgando desde el teléfono: llega `terminate`
   con `duration` y el panel muestra la duración de Meta.

## 3. Lo que el diseño supone y hay que confirmar

| # | Supuesto | Cómo probarlo | Si no se cumple |
| --- | --- | --- | --- |
| 1 | Se puede hacer `pre_accept` y luego **no** aceptar sin que el cliente lo note | Reclamar, contestar y no confirmar el medio (cerrar la pestaña): ¿el teléfono sigue sonando? | Quien reclama se queda con la llamada (§3.3, paso 7) |
| 2 | `reject` corta el timbre en el teléfono | Dejar vencer `segundosSonando` | Ajustar qué se manda al perderse |
| 3 | `terminate` es idempotente por `call_id` | Colgar dos veces seguidas (segunda vía API) | El mantenimiento no debe repetirlo |
| 4 | Cuánto deja sonar Meta de verdad | Cronometrar sin contestar | Ajustar el rango de `segundosSonando` (15–45) |
| 5 | Un `connect` repetido trae el mismo `id` | Mirar reintentos de Meta en `entrantes` | Revisar la deduplicación |
| 6 | Qué trae `terminate` si nadie contestó | Colgar desde el teléfono antes de contestar | Ajustar `terminarPorMeta` |
| 7 | Una llamada **no** abre la ventana de 24 h | Llamar desde un número que no ha escrito y ver si sale el mensaje de llamada perdida | Si la abre, quitar la condición |
| 8 | La grabación se pide al aceptar (`recording`) y llega `call_recording_available` | Encender `grabar: todas` y contestar | Define la descarga (pendiente de E1) |

## 4. Fuera de horario, perdida y límites

1. Con el horario cerrado y `fueraDeHorario: rechazar_con_mensaje`: la llamada se rechaza y sale el
   mensaje de fuera de horario (una vez por ventana cerrada, compartida con el chat).
2. Con `buzon`: Iris no toca la llamada; anotar **cómo llega el audio del buzón** por el webhook
   `messages` (qué lo distingue de un audio normal).
3. Sin nadie disponible: a los `segundosSonando` queda perdida, aparece en «Por devolver» y, si el
   cliente escribió en las últimas 24 h, sale el mensaje automático (una vez por hora).
4. Cuatro llamadas seguidas del mismo teléfono en 10 minutos: la cuarta se rechaza.

## 5. Qué entregar

Un informe corto con: lo que difiere de cada supuesto, los cuerpos reales de `connect` y `terminate`
(sin el número del cliente), los tiempos medidos y si hace falta TURN.
