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
   **Supuesto 1, resuelto el 2026-11-17:** `limite_mensajeria` sale de
   `GET /{phone_number_id}?fields=whatsapp_business_manager_messaging_limit` (límite del portafolio
   de negocio; `messaging_limit_tier` está deprecado y devolvía vacío, por eso Iris decía «Falta»
   con un límite real de 2 000). Si Meta no lo devuelve o Iris no reconoce el valor, el requisito
   sale «sin comprobar» y no impide encender. `calling_habilitado` sale de
   `GET /{phone_number_id}/settings`; `webhook_calls` no se puede leer (se confirma con la primera
   llamada). Detalle en `voz.md` §10.1-1.
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

**Lo primero a mirar: la oferta SDP real de Meta.** Iris valida la oferta con una lista cerrada de
líneas, atributos y códecs de audio (Opus obligatorio; además `telephone-event`, `red`, G722, PCMU,
PCMA y CN, que trae la respuesta real de Meta a una saliente) antes de entregarla al navegador. Si la
oferta real trae un atributo o un códec fuera de la lista, **ninguna llamada suena**: falla cerrado,
la llamada queda `perdida` y en el log del servidor aparece `voz: la oferta SDP de Meta no pasa la
validación` con el motivo. Si en el paso 2 no suena nada, mirar ese log y el SDP del webhook crudo
(antes de que termine la llamada: después queda tapado) y ampliar la lista (`atributosSDP` en
`voz_modelo.go`). La respuesta del navegador no se rechaza por traer direcciones de la red local:
el servidor las quita antes de mandarla a Meta; comprobar que aun así el audio conecta.

| # | Supuesto | Cómo probarlo | Si no se cumple |
| --- | --- | --- | --- |
| 1 | Se puede hacer `pre_accept` y luego **no** aceptar sin que el cliente lo note | Reclamar, contestar y no confirmar el medio (cerrar la pestaña): ¿el teléfono sigue sonando? | Quien reclama se queda con la llamada (§3.3, paso 7) |
| 2 | `reject` corta el timbre en el teléfono | Dejar vencer `segundosSonando` | Ajustar qué se manda al perderse |
| 3 | `terminate` es idempotente por `call_id` | Colgar dos veces seguidas (segunda vía API) | El mantenimiento no debe repetirlo |
| 4 | Cuánto deja sonar Meta de verdad | Cronometrar sin contestar | Ajustar el rango de `segundosSonando` (15–45) |
| 5 | Un `connect` repetido trae el mismo `id` | Mirar reintentos de Meta en `entrantes` | Revisar la deduplicación |
| 6 | Qué trae `terminate` si nadie contestó | Colgar desde el teléfono antes de contestar | Ajustar `terminarPorMeta` |
| 7 | Una llamada **no** abre la ventana de 24 h | Llamar desde un número que no ha escrito y ver si sale el mensaje de llamada perdida | Si la abre, quitar la condición |
| 8 | Cómo se pide la grabación y qué trae `call_recording_available` | Con una página de prueba aparte (Iris no la pide: la grabación es E1c) | Define E1c |
| 9 | La forma de `call_hours` en `POST /{phone_number_id}/settings` | Configurar un horario que quepa y mirar WhatsApp Manager | Ajustar `graphHTTP.FijarAjustes` |
| 11 | `accept` no necesita el SDP otra vez (Iris lo manda solo en `pre_accept`; `confirmar-medio` manda `accept` sin `session`) | **Validado el 2026-11-19 con Meta real (0.29.0, Chrome de escritorio):** la entrante se contestó y se oyó en los dos sentidos | Guardar el SDP de la respuesta hasta el `accept` y mandarlo también ahí |
| 12 | Qué hace Meta con un **segundo `pre_accept`** de la misma llamada (después de soltar, otra persona contesta con otro SDP) | Reclamar y contestar con A, soltar, reclamar y contestar con B: ¿B oye al cliente? | Quien hace `pre_accept` se queda con la llamada: `soltar` después de `contestar` pasa a terminar la llamada |
| 10 | Qué distingue el recado del buzón de un audio normal | Dejar un recado con `fueraDeHorario: buzon` y mirar el webhook `messages` | Cambiar la regla de los 5 minutos por la marca de Meta |

## 4. Fuera de horario, perdida y límites

1. Con el horario cerrado y `fueraDeHorario: rechazar_con_mensaje`: la llamada se rechaza y sale el
   mensaje de fuera de horario (una vez por ventana cerrada, compartida con el chat).
2. Con `buzon`: Iris no toca la llamada; anotar **cómo llega el audio del buzón** por el webhook
   `messages` (qué lo distingue de un audio normal).
3. Sin nadie disponible: a los `segundosSonando` queda perdida, aparece en «Por devolver» y, si el
   cliente escribió en las últimas 24 h, sale el mensaje automático (una vez por hora).
4. Como máximo 3 llamadas que suenan por número cada 10 minutos (ventana deslizante, sin bloqueo
   adicional): la cuarta se rechaza. Cuentan solo las **entrantes** que sonaron (ni las que ya
   rechazó la ráfaga ni las salientes a ese número). Pasados 10 minutos desde la primera de las tres,
   vuelve a sonar aunque se haya seguido intentando (antes de 0.29.1 cada reintento renovaba el
   bloqueo).

## 4b. Salientes y permiso de llamada (E1b)

Con un usuario `supervisor` (tiene `llamadas.llamar`) y el número de pruebas de la empresa dueña.

1. `GET …/conversaciones/{id}/permiso-llamada`: `sin_pedir`. `POST …/solicitar-permiso-llamada`
   dentro de la ventana de 24 h: al teléfono llega el mensaje con los botones de permiso. Guardar
   el **cuerpo que Iris manda** y el **webhook de la respuesta** al aceptar (temporal) y al rechazar.
2. Repetir la solicitud: 422 `limite_solicitudes`. Comprobar contra WhatsApp Manager los límites
   (1 por 24 h, 2 por 7 días).
3. Fuera de la ventana: indicar la plantilla en los ajustes de llamadas del número
   (`plantillaPermiso`) y solicitar. Guardar el cuerpo y la respuesta de Meta.
4. `POST …/conversaciones/{id}/llamar` desde el panel. Esperado: el teléfono suena; al contestar,
   audio en los dos sentidos. Guardar los webhooks en orden (respuesta SDP, RINGING, ACCEPTED,
   terminate) y la respuesta de `connect` y de `call_permissions`.
5. No contestar: a los 60 s Iris la deja perdida y manda `terminate`. Rechazar desde el teléfono.
6. Colgar desde el panel mientras suena.

Supuestos de E1b por confirmar (si alguno no se cumple, las salientes no funcionan o cuentan mal):

| # | Supuesto | Dónde está en el código |
| --- | --- | --- |
| 13 | `connect` responde `{ "calls": [{ "id": "wacid…" }] }` | `graphHTTP.Llamar` |
| 14 | La respuesta SDP llega en `calls[]` con `event: connect`, `direction: BUSINESS_INITIATED` y `session.sdp_type: answer` | `eventoSaliente` |
| 15 | RINGING, ACCEPTED y REJECTED llegan en `statuses[]` del campo `calls`, con `id` = call_id y `recipient_id` | `procesarMeta` (entrantes.go) |
| 16 | `GET /{phone_number_id}/call_permissions?user_wa_id=` responde `permission.status` (`temporary`, `permanent`, otro = sin permiso), `expiration_time` y `actions[]` con `start_call` | `graphHTTP.Permiso` |
| 17 | La solicitud es `interactive.type: call_permission_request` y la respuesta `interactive.call_permission_reply { response, is_permanent, expiration_timestamp }` | `whatsapp_cloud.go`, `recibirPermisoLlamada` |
| 18 | La plantilla de permiso se manda sin componentes (`template { name, language }`) | `whatsapp_cloud.go` |
| 19 | La respuesta SDP real de Meta a una saliente pasa la lista cerrada (si no, la llamada queda `fallida` y se ve en `llamada.fallida`) | `ValidarSDP` |
| 20 | ¿Meta manda `ACCEPTED` siempre? Si no, la llamada se ve `marcando` mientras hay audio; al llegar el `terminate` con duración queda `terminada` y cuenta para el tope, pero a los 60 s el mantenimiento la habría cortado antes: habría que pasar a `en_curso` al llegar la respuesta SDP | `eventoSaliente`, `terminarPorMeta` |

Además, con un número real:

- Abrir el panel en dos navegadores con el mismo usuario, llamar desde uno y cerrar ese: la llamada
  debe cortarse a los 55 s aunque el otro siga abierto.
- Colgar desde el panel justo al pulsar «Llamar» (mientras Meta responde al `connect`): el teléfono
  no debe quedarse sonando (mirar `mensajeria.llamadas_huerfanas`).
- **Supuesto 21, categoría de la plantilla de permiso (sin confirmar).** Iris supone que
  `GET /{waba_id}/message_templates?name=` devuelve `data[] { name, status, category, language }`
  y que una plantilla de permiso de llamada aprobada es de categoría `UTILITY` o `MARKETING`
  (`graphHTTP.Plantilla` y `ConfigurarLlamadas`). Cómo probarlo: crear la plantilla en WhatsApp
  Manager, esperar la aprobación y guardarla en los ajustes de llamadas del número. Si la categoría
  real es otra, `llamadas/configurar` responde 422 `plantilla_permiso_invalida` con la plantilla
  buena: anotar la categoría que devuelve Meta y ampliar la lista en `ConfigurarLlamadas`.
  La plantilla **solo se valida al guardarla** y Iris no puede comprobar que sea de permiso de
  llamada: probar a guardar una plantilla aprobada cualquiera y solicitar el permiso fuera de la
  ventana. Esperado: Meta la rechaza, el mensaje queda fallido en el hilo (`rechazado_por_canal`), la
  solicitud no cuenta para el límite y se puede volver a pedir. Guardar la respuesta de Meta.
- **Supuesto 22 (duda a validar con Meta):** ¿una saliente que **el negocio cuelga mientras timbra**
  cuenta para la revocación del permiso (las cuatro seguidas sin contestar)? Hoy Iris **no** la suma
  a `sin_contestar` (sí cuenta para el límite de «colgadas marcando»). Cómo probarlo: con un permiso
  temporal, llamar y colgar desde el panel antes de que conteste, cuatro veces seguidas, y leer
  `call_permissions`. Si Meta la cuenta, hay que sumarla también en Iris (`trasSaliente`), o
  `confirmarRiesgo` irá por detrás de la cuenta de Meta.
- **Supuesto 24 (comprobar con una llamada real):** cuando el agente deja una entrante antes de
  que haya audio, Iris manda **`reject`** si nunca mandó `accept` (solo reservada o pre-aceptada) y
  **`terminate`** si el `accept` ya salió o estaba en vuelo; si Meta responde 400 o 404 a una, prueba
  la otra, y alterna. Probar las tres: cerrar la pestaña justo al pulsar «Contestar», después del
  `pre_accept` y durante el `accept`. En las tres el teléfono del cliente debe dejar de timbrar; si
  en alguna sigue sonando, anotar qué respondió Meta a cada acción.
- **Supuesto 23:** `call_permissions` trae, en la acción `send_call_permission_request`,
  `limits[] { time_period: "PT24H", current_usage }`. Iris lo usa para saber si una solicitud cuyo
  envío se quedó sin respuesta llegó a salir (`graphHTTP.Permiso`, `resolverSolicitudes`). Si Meta
  no trae ese dato, la solicitud dudosa sigue contando para el límite hasta que pase un día: mirar
  la respuesta real y ajustar.
- Si el número también hace salientes desde otro sistema: comprobar que Iris no las corta (quedan
  en `mensajeria.llamadas_huerfanas` con estado `ajena`).

## Si no conecta el audio

Lo que ve la persona y qué significa:

- **«No se pudo preparar el audio de la llamada»** (al llamar) o **«No se pudo contestar — Iris no
  aceptó el audio de este navegador»**: no es la red. Iris no aceptó la descripción del audio que dio
  el navegador y ni siquiera se llamó a WhatsApp. Pasó en 0.29.0 con toda llamada saliente desde
  Chrome (Iris solo admitía Opus y Chrome ofrece también otros códecs); se corrigió en 0.29.1. Si
  vuelve a pasar, el registro del servidor dice el motivo exacto (ver abajo) y hay que avisar a
  desarrollo con esa línea.
- **«No se pudo conectar el audio»**: la llamada llegó a WhatsApp pero el audio no pasó. Ahí sí puede
  ser la red: probar desde otra red (por ejemplo, los datos del teléfono compartidos a la
  computadora). Meta no exige servidores STUN ni TURN (su FAQ: usa ICE-lite y acepta al navegador
  aunque no anuncie su dirección), pero una red que bloquea UDP no deja pasar el audio.

Qué mirar, sin tocar nada:

1. En la computadora, en Chrome, abrir la consola (F12 → Consola) durante la llamada: las líneas
   `[voz] ICE:` dicen cómo va la conexión del audio (`checking`, `connected`, `failed`) y qué tipo de
   candidatos tiene el navegador (`host`, `srflx`, `relay`), sin direcciones. Si no aparece ninguna
   línea `[voz] ICE:`, no se llegó a intentar el audio (es el caso de «no se pudo preparar»).
2. Para más detalle, abrir `chrome://webrtc-internals` en otra pestaña **antes** de llamar: en la
   conexión, «ICE connection state» y la tabla de pares de candidatos. Si todos los pares quedan en
   `failed`/`in-progress` sin respuesta, la red no deja pasar el audio hacia WhatsApp.
3. En el servidor, el registro de voz de las últimas 3 horas (`irisctl logs` sigue la salida en vivo y
   solo muestra las últimas 200 líneas, por eso no sirve con `grep`):

   ```
   sudo irisctl compose logs --no-color --since 3h api worker 2>&1 | grep -F 'voz:'
   ```

   Líneas útiles: `voz: la oferta SDP del navegador no pasa la validación; no se llama` (con
   `motivo=`), `voz: la respuesta SDP del navegador no pasa la validación; no se contesta`,
   `voz: la respuesta SDP de Meta a una saliente no pasa la validación; se cuelga`,
   `voz: a la oferta SDP del navegador se le quitó lo que Meta no usa` (informativa: se quitaron
   atributos o códecs desconocidos y la llamada siguió). Nunca se registra el SDP ni el token.
4. Las últimas llamadas que intentó hacer cada persona y cómo terminaron, en la base:

   ```
   sudo irisctl compose exec -T postgres sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -c "select created_at, resultado, error->>'\''type'\'' from auditoria.acciones where accion = '\''llamadas.llamar'\'' order by id desc limit 5"'
   ```

   `error` con `sdp_invalido` = Iris no aceptó el audio del navegador (no es la red).

## Si no aparece «Contestar»

Una llamada entrante solo le suena (con el aviso y el botón «Contestar») a quien cumple **todo** esto
en el momento:

1. Está **Disponible** en Conversaciones (el punto de su foto en verde; «No disponible» es gris).
2. Tiene **«Recibir llamadas»** encendido en ese navegador: es el botón del teléfono de la barra de
   arriba. Al encenderlo, el navegador pide el micrófono: hay que permitirlo.
3. Tiene el panel de Iris abierto (cualquier pantalla) en ese navegador.
4. Es miembro o supervisor de la cola a la que entra la llamada (la del número en Administración →
   Llamadas, o la de la conversación).
5. Tiene el permiso de contestar llamadas (roles `soporte`, `despacho`, `ventas`, `supervisor` y `admin`).

Ahora el panel lo dice solo: en la tarjeta de la llamada (sonando o perdida) aparece «No te
sonó porque…» con el paso que falta y su botón («Encender recibir llamadas», «Ponerme disponible» o
volver a pedir el micrófono); el panel «Llamadas» también ofrece «Ponerme disponible», y la primera vez
que alguien que contesta entra con un número con llamadas encendidas, un aviso le ofrece encender
«Recibir llamadas». Lo que el panel no puede saber (si eres de la cola) no se dice.

Si a nadie le suena, la llamada queda en la conversación como **«Llamada perdida — Nadie
contestó»** y en «Por devolver». Si alguien se pone disponible o enciende «Recibir llamadas» mientras
todavía suena, le empieza a sonar en unos segundos.

**«Llamada rechazada» sin nadie que la rechazara**: es el límite contra abuso. Como máximo suenan 3
llamadas del mismo número cada 10 minutos; la siguiente se rechaza sola (el cliente oye que no se le
contestó). La tarjeta dice «Demasiadas llamadas seguidas de este número; vuelve a sonar a las HH:MM» a
quien configura llamadas, y trae **«Dejar que vuelva a llamar»** (supervisor y admin) para que la
próxima suene ya. Llamar a ese número desde Iris también lo levanta. Al probar: usar ese botón,
esperar a la hora que dice o probar desde otro teléfono. Otras causas que se ven
distinto: con las llamadas apagadas en Iris no queda nada en la conversación (registro: «voz: llegó
una llamada y se rechazó porque las llamadas del número están apagadas en Iris»); fuera del horario
queda «Llamó fuera de horario».

En el registro del servidor (mismo comando de arriba): `voz: llamada entrante sin nadie que la pueda
contestar ahora` (nadie cumplía los cinco puntos), `voz: llamada entrante rechazada por ráfaga`
(el límite) y `voz: la oferta SDP de Meta no pasa la validación; la llamada no suena` (Iris no aceptó
la oferta de Meta: avisar a desarrollo con esa línea).

## 5. Qué entregar

Un informe corto con: lo que difiere de cada supuesto, los cuerpos reales de `connect` y `terminate`
(sin el número del cliente), los tiempos medidos y si hace falta TURN.
