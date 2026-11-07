# Contrato del chat interno del equipo (fase 1)

Lo implementa I1 en el servicio de mensajería (`apps/api/cmd/mensajeria`, schema `mensajeria`,
código en `apps/api/internal/mensajeria/chat_*.go`). Lo usan el panel (I3, «Conversaciones /
Equipo») y la app de campo (I4, tablero `C-Chat`). Los tipos están en `packages/contratos`
(operaciones `*Chat`): no los copies a mano.

Base: `/api/v1/mensajeria/chat`. La API la reenvía tal cual al proceso de mensajería. Auth: la
cookie `HttpOnly` del panel o `Authorization: Bearer` de la app. Multiempresa: la empresa activa
(`X-Iris-Empresa` si el usuario tiene varias), como en el resto de Iris. Errores RFC 9457
(`type: urn:iris:problema:<codigo>`).

## 1. Modelo

| Tipo | Una por | Miembros |
| --- | --- | --- |
| `directa` | par de usuarios y empresa | los dos (fijos) |
| `cuadrilla` | cuadrilla y empresa | técnicos activos de la cuadrilla (`agenda.tecnicos`) + quien tiene `chat.supervisar` global, con la cuadrilla o con una zona que la cuadrilla atiende |
| `orden` | orden | su técnico asignado (`campo.ordenes.tecnico_id`) + `chat.supervisar` global, con la zona o con la cuadrilla de la orden + despacho que se unió, **mientras** su `ordenes.ver` (con alcance) cubra la orden + quien agregó la supervisión (§2.2) |

- Los miembros de cuadrilla y de orden **se derivan al leer**: si la orden se reasigna, el técnico
  nuevo entra y el anterior sale; si un técnico cambia de cuadrilla, cambia de chat. Solo quien se
  une a un hilo de orden desde despacho queda guardado como miembro, y aun así deja de verlo si la
  orden sale de su alcance de `ordenes.ver` (otra zona o cuadrilla); vuelve a verlo si regresa.
- Quien **agrega la supervisión** (`POST /conversaciones/{id}/miembros`, §2.2) queda guardado como
  miembro `agregado` y ve el hilo mientras opere en la empresa y tenga `chat.ver`, sin depender de
  su `ordenes.ver` (lo invitó alguien con alcance sobre la orden).
- **Solo lectura:** el hilo de una orden `cerrada`, `fallida` o `cancelada` es de solo lectura
  (`solo_lectura: true`): enviar y agregar miembros responden **422** `conversacion_cerrada`. Leer,
  marcar leído, listar miembros y eliminar un mensaje propio dentro de sus 15 min siguen permitidos
  (retirar un error no agrega nada). Se deriva del estado de la orden al leer: si la orden se
  reabre, el hilo vuelve a aceptar mensajes.
- Las directas nunca se ven por alcance: ninguna supervisión lee mensajes directos.
- Lo que no ves responde **404** (no se revela que existe).
- Permisos: `chat.ver` (leer, marcar leído, flujo), `chat.enviar` (abrir, enviar, eliminar),
  `chat.supervisar` (alcance; agregar miembros a un hilo de orden). Roles base: técnico, despacho, soporte y supervisor tienen ver y
  enviar; supervisor y admin, supervisar; lectura, solo ver.

## 2. Endpoints

| Método y ruta | Permiso | Qué hace |
| --- | --- | --- |
| `GET /conversaciones?cursor=&limit=&tipo=&q=` | ver | Mis conversaciones, la más reciente primero. `{ data: ConversacionChat[], nextCursor }`. `q` busca en el título (persona, cuadrilla o número de orden) |
| `GET /resumen` | ver | Contadores del ícono del encabezado y de las pestañas (§2.1) |
| `GET /conversaciones/{id}` | ver | `ConversacionChatDetalle`: la conversación + `lecturas[]` (hasta dónde leyó cada uno: «visto») |
| `GET /conversaciones/{id}/miembros` | ver | Quiénes están en el chat y por qué (§2.2). `{ data: MiembroChat[] }` |
| `POST /conversaciones/{id}/miembros` `{ usuario_id }` | supervisar | Agrega a alguien al hilo de una orden (§2.2). 201 agregado, 200 ya estaba |
| `GET /ordenes/{orden_id}/conversacion` | ver | El hilo de la orden **sin crearlo ni unirse** (pestaña «Chat» de la orden). 404 si no existe o no lo ves: entonces `POST /ordenes/{orden_id}/abrir` |
| `GET /personas?q=&cursor=&limit=` | enviar | «Nuevo mensaje» → Persona: con quién puedes abrir una directa (§2.3) |
| `GET /cuadrillas?q=` | enviar | «Nuevo mensaje» → Cuadrilla: las cuadrillas cuyo chat puedes abrir (§2.3) |
| `POST /directas` `{ usuario_id }` | enviar | Abre o trae la directa. 201 nueva, 200 existente. 422 `usuario_no_disponible` |
| `POST /cuadrillas/{cuadrilla_id}/abrir` | enviar | Abre o trae el chat de la cuadrilla (técnico de ella o supervisión). 201/200; 404 |
| `POST /ordenes/{orden_id}/abrir` | enviar | Abre o trae el hilo de la orden. Despacho queda como miembro (201 al unirse). 404 |
| `GET /conversaciones/{id}/mensajes?direccion=&cursor=&limit=&desde_seq=` | ver | Mensajes, siempre en `seq` ascendente. `anteriores` (defecto): del más reciente hacia atrás; `siguientes`: hacia adelante |
| `POST /conversaciones/{id}/mensajes` | enviar | Envía (ver §3). 201 nuevo, 200 reenvío |
| `POST /mensajes/{mensaje_id}/eliminar` | enviar | Lápida. Solo el autor, hasta 15 min (403 `no_es_autor`, 422 `plazo_vencido`). Repetir: 200. «Deshacer» del panel: §3.1 |
| `POST /conversaciones/{id}/leer` `{ hasta_mensaje_id }` | ver | Avanza tu lectura (nunca retrocede). `{ conversacion_id, leido_hasta_mensaje_id, no_leidos }` |
| `GET /cambios?cursor=&limit=` | ver | Sondeo del feed (§4). `{ data: EventoChat[], nextCursor, hayMas }` |
| `GET /eventos` | ver | Server-Sent Events (§4) |

Abrir una conversación es idempotente por su naturaleza (una por par, cuadrilla u orden): la
`Idempotency-Key` se acepta pero no se guarda la respuesta (trae la vista previa del último mensaje,
que debe desaparecer si se elimina). Repetir el POST da lo mismo.

### `ConversacionChat`

```json
{ "id": 12, "tipo": "orden", "empresa_id": 1, "titulo": "OT-000102",
  "cuadrilla_id": null, "orden_id": 102, "orden_estado": "en_sitio",
  "otro_usuario": { "id": 7, "nombre": "Andrea Castillo" },          // solo directa
  "ultimo_mensaje": { "id": "0193…", "autor_id": 5, "autor_nombre": "Tomás Herrera",
                      "contenido": "texto", "texto": "Listo, −21 dBm…", "enviado_en": "…" },
  "no_leidos": 2, "menciones": 1, "leido_hasta_mensaje_id": "0193…",
  "orden_tipo": "Instalación",                                       // null fuera de un hilo de orden
  "solo_lectura": false,
  "ultima_actividad": "…", "creada_en": "…" }
```

`titulo`: el otro usuario (directa), el nombre de la cuadrilla o el número de la orden (el cliente
lo arma el panel con la orden). `contenido`: `texto | foto | ubicacion | eliminado`; `texto` son los
primeros 140 caracteres. `ultimo_mensaje` y `otro_usuario` faltan si no aplican. `no_leidos`:
mensajes de otros, sin eliminar, después de tu lectura. `menciones`: de esos, los que te mencionan
(«hace falta actuar»: el indicador crece; §2.1). `orden_tipo`: el nombre del tipo de la orden (para
«OT-000214 · Instalación»). `solo_lectura`: §1.

### 2.1 Contadores: `GET /resumen`

```json
{ "no_leidos": 5, "menciones": 1, "conversaciones_no_leidas": 3, "requiere_accion": 2,
  "por_tipo": {
    "directa":   { "no_leidos": 1, "menciones": 0, "conversaciones_no_leidas": 1, "requiere_accion": 1 },
    "cuadrilla": { "no_leidos": 3, "menciones": 0, "conversaciones_no_leidas": 1, "requiere_accion": 0 },
    "orden":     { "no_leidos": 1, "menciones": 1, "conversaciones_no_leidas": 1, "requiere_accion": 1 } } }
```

- Sobre **todas** las conversaciones que ves en la empresa activa (no solo la página cargada).
- `requiere_accion` = no leídos de las **directas** + **menciones** sin leer en cuadrillas y
  órdenes. Es el número del ícono del encabezado y de las pestañas; lo demás no leído es un punto
  (CLAUDE.md §10: el indicador solo crece cuando hace falta actuar).
- Cada `EventoChat` trae `no_leidos` y `menciones` de su conversación; para los totales, vuelve a
  pedir `GET /resumen` con un respiro (p. ej. 1 s tras el último evento), no por cada evento.

### 2.2 Miembros: `GET` y `POST /conversaciones/{id}/miembros`

```json
{ "data": [
  { "usuario_id": 5, "nombre": "Mario Ixcot", "motivo": "tecnico" },
  { "usuario_id": 9, "nombre": "Ana López", "motivo": "despacho" },
  { "usuario_id": 3, "nombre": "Laura Méndez", "motivo": "supervision" },
  { "usuario_id": 12, "nombre": "Pedro Gómez", "motivo": "agregado" } ] }
```

- Los usuarios activos de la empresa que ven la conversación, tú incluido, por nombre. `motivo`:
  `directa` (los dos de una directa), `tecnico` (técnico de la cuadrilla o asignado a la orden),
  `despacho` (se unió al hilo), `agregado` (lo agregó la supervisión) o `supervision`
  (`chat.supervisar` que la cubre). Con varios motivos, el primero de esta lista.
- No paginada: es acotada (los miembros de un chat). Sirve también para el autocompletado de
  `@menciones`.
- **Agregar** (`chat.supervisar`; sin él, **403**): solo en hilos de **orden** (otro tipo: 422
  `tipo_no_admite_miembros`); tu `chat.supervisar` debe cubrir la orden (global, su zona o su
  cuadrilla; si ves el hilo por otra vía, **403** `fuera_de_alcance`); orden cerrada, fallida o
  cancelada: 422 `conversacion_cerrada`. El agregado debe ser una persona activa, de la empresa y
  con `chat.ver` (422 `usuario_no_disponible`). Si ya lo ve (por cualquier motivo): **200** sin
  cambios; si no: **201**, queda `agregado`, se audita, emite `chat.miembro_agregado` y un
  `conversacion_actualizada` en el feed (al agregado le aparece el hilo). Responde la lista de
  miembros. Acepta `Idempotency-Key`.
- Quitar miembros queda fuera de esta fase.

### 2.3 «Nuevo mensaje»

- `GET /personas?q=&cursor=&limit=` → `{ data: PersonaChat[], nextCursor }`:
  `{ "id": 7, "nombre": "Andrea Castillo", "conversacion_id": 31 }`. Personas activas de la empresa
  con `chat.ver`, sin ti, por nombre. `conversacion_id`: la directa que ya tienen (o null). Elegir
  una → `POST /directas`.
- `GET /cuadrillas?q=` → `{ data: CuadrillaChat[] }` (no paginada: acotada):
  `{ "id": 3, "nombre": "Cuadrilla Sur", "tecnicos": 4, "zonas": ["San Lucas", "Amatitlán"],
  "conversacion_id": 18 }`. Las activas cuyo chat puedes abrir: la tuya (técnico) y las que cubre tu
  `chat.supervisar`. `conversacion_id` no nulo = «Ya existe». Elegir una →
  `POST /cuadrillas/{id}/abrir`.
- Los hilos de orden se abren desde la orden (`GET /ordenes/{id}/conversacion` y, si no existe o
  no estás, `POST /ordenes/{id}/abrir`).

### `MensajeChat`

```json
{ "id": "0193…", "conversacion_id": 12, "seq": 41, "autor_id": 5, "autor_nombre": "Tomás Herrera",
  "texto": "La ONU tiene LOS en rojo.",                          // null si no tiene o se eliminó
  "adjunto": { "id": "…", "estado": "subiendo", "url_descarga": "/api/v1/adjuntos/…" },  // opcional
  "ubicacion": { "lat": 14.6, "lon": -90.5 },                     // opcional
  "menciones": [ { "id": 7, "nombre": "Mario Ixcot" } ],           // siempre ([] sin menciones)
  "creado_en": "…",   // hora del teléfono al escribirlo (o null)
  "enviado_en": "…",  // hora del servidor al recibirlo («enviado sin señal, llegó 11:34»)
  "eliminado": false, "eliminado_en": null }
```

`menciones`: a quiénes menciona (§3.2), por id; `[]` sin menciones o eliminado.

**Texto plano.** El servidor no limpia nada y nunca interpreta HTML: el cliente lo muestra como
texto (React lo escapa; nada de `dangerouslySetInnerHTML` ni Markdown). Con `eliminado: true` se
muestra «Mensaje eliminado».

## 3. Enviar, idempotencia y orden

```
POST /conversaciones/{id}/mensajes
{ "id": "<uuid v7>", "texto": "…", "adjunto_id": "<uuid>", "ubicacion": { "lat": …, "lon": … }, "creado_en": "…",
  "menciones": [7, 12] }
```

- Hilo de orden cerrada, fallida o cancelada: **422** `conversacion_cerrada` (§1).

- `id` lo genera el cliente **al escribir** (uuid v7; otro formato: 422) y se guarda en la cola
  local. Reenviar el mismo `id` con el mismo contenido responde **200** con el mensaje actual y
  `Idempotent-Replayed: true` (aunque ya se haya eliminado: llega la lápida). El mismo `id` con otro
  contenido (o de otro autor): **409** `mensaje_id_reutilizado` — es un error del cliente; no se
  reintenta.
- `Idempotency-Key` (regla 6) también se acepta; la aplica el servidor en el mensaje (no guarda la
  respuesta con el texto): la misma clave con el mismo `id` responde el mensaje actual (200); con
  otro `id`, 422 `idempotencia_reutilizada`. El `id` basta.
- **Eliminar** deja una lápida sin nada del contenido (ni texto, ni foto, ni ubicación, ni su
  huella): un reenvío del mismo `id` por su autor responde la lápida (200), sin 409.
- Al menos uno de `texto` (1–4000 caracteres tras recortar los extremos), `adjunto_id` o
  `ubicacion`; si no, 422 `mensaje_vacio`. `creado_en` no puede ser futuro (más de 5 min).
- **Foto:** primero `POST /api/v1/adjuntos` con `entidad: "chat_conversacion"`, `entidadId:
  "<id de la conversación>"`, `tipo: "foto"`, y el mensaje con su `adjunto_id`. El mensaje puede
  salir antes de terminar la subida (`adjunto.estado` = `pendiente`/`subiendo`; se muestra al estar
  `completo`). La foto debe ser tuya, de esa conversación y no rechazada (422 `adjunto_invalido`,
  `adjunto_no_es_foto`, `adjunto_rechazado`); una foto va en un solo mensaje (409).
  **Una foto solo se ve mientras un mensaje sin eliminar la muestra**: `GET /api/v1/adjuntos?entidad=
  chat_conversacion&entidadId=…` no la lista y `GET /api/v1/adjuntos/{id}` da 404 antes de enviar el
  mensaje (tampoco para su autor: muestra la copia local) y después de eliminarlo. Al eliminar el
  mensaje la foto queda `rechazado` (motivo `retirado`) y se borra del almacén. Una foto subida y
  nunca enviada se retira a las 48 h sin actividad.
- **Límite:** 30 mensajes por minuto por usuario → 429 `demasiados_mensajes` con `Retry-After`. Los
  reenvíos no cuentan. La cola espera `Retry-After` y sigue con el mismo `id`.
- **Orden:** `seq` lo asigna el servidor al recibir (orden de llegada, por conversación). Se ordena
  por `seq`; `creado_en` solo se muestra. Mientras un mensaje está en la cola local no tiene `seq`:
  va al final, marcado «se envía al tener señal».
- Errores que la cola **no** reintenta: 403, 404 (ya no eres miembro), 409, 422. Reintenta 429 (con
  `Retry-After`), 5xx y sin red, con espera exponencial.

### 3.1 Eliminar con «Deshacer» (panel)

El servidor **no** tiene «restaurar»: eliminar borra el texto, la foto (también del almacén), la
ubicación y las menciones, y no guarda nada para devolverlos (§3). El «Deshacer» es del cliente:

1. Al pulsar «Eliminar», el panel muestra el mensaje como «Mensaje eliminado» (local) y el aviso
   con «Deshacer» durante **5 s**. Todavía **no** llama al servidor.
2. «Deshacer» dentro de esos 5 s: cancela el temporizador y vuelve a mostrar el mensaje. Nada salió.
3. Pasados los 5 s (o al cerrar el aviso): `POST /mensajes/{id}/eliminar`. Si responde 422
   `plazo_vencido` (se pasó de los 15 min) u otro error, vuelve a mostrar el mensaje con el error y
   «Reintentar».
4. Si la página se cierra u oculta (`pagehide`, o `visibilitychange` a `hidden`) con una
   eliminación pendiente, se manda ya (`fetch` con `keepalive: true`, la misma cookie y el CSRF).
5. No ofrezcas «Eliminar» si al mensaje le quedan menos de 10 s del plazo de 15 min.

Mientras dura la espera los demás siguen viendo el mensaje (aún no se eliminó) y su push pudo haber
salido, como con cualquier eliminación.

### 3.2 Menciones

- `menciones`: ids de usuarios (hasta **20**; los repetidos cuentan una vez). Cada uno debe **ver
  la conversación** ahora (los de `GET /conversaciones/{id}/miembros`) y no puede ser el autor: si
  no, **422** `mencion_invalida` con `errors[]` (`menciones[i]`). Una mención necesita texto (422
  `mencion_sin_texto`). El texto lleva `@Nombre` tal cual lo escribió el usuario; el servidor no lo
  interpreta: vale la lista de ids (el panel resalta los `@Nombre` de los mencionados).
- Las menciones son parte del contenido: reenviar el mismo `id` con otras menciones da **409**
  `mensaje_id_reutilizado`. Al eliminar el mensaje se borran (la lápida trae `menciones: []`).
- Cada mencionado (nunca el autor) genera el evento `chat.mencion` → push de tipo `mencion`
  (push.md §3; urgente: sale aun en «No molestar» con `urgenteIgual`). El mencionado no recibe
  además el push `chat` del mismo mensaje: el emisor los une (gana la mención).
- Cuentan en `menciones` de la conversación y en `GET /resumen` hasta que las lees (`POST /leer`
  hasta ese mensaje o uno posterior).

## 4. Tiempo real y sondeo

Un solo feed por usuario y empresa: los cambios de las conversaciones que ves, con un cursor
creciente. SSE y sondeo dan el **mismo** `EventoChat` con el **mismo** cursor: se puede pasar de uno a
otro sin perder nada.

```json
{ "id": "1543", "tipo": "mensaje_nuevo", "conversacion_id": 12, "ocurrido_en": "…", "no_leidos": 3, "menciones": 1,
  "mensaje": { …MensajeChat… } }               // mensaje_nuevo, mensaje_eliminado
{ "id": "1544", "tipo": "leido", "conversacion_id": 12, "ocurrido_en": "…", "no_leidos": 0,
  "lectura": { "usuario_id": 7, "hasta_mensaje_id": "0193…", "leido_en": "…" } }
{ "id": "1545", "tipo": "conversacion_actualizada", "conversacion_id": 13, "ocurrido_en": "…", "no_leidos": 0,
  "conversacion": { …ConversacionChat… } }       // nueva, o te uniste
```

- `no_leidos` y `menciones` son los tuyos en esa conversación al leer el evento: úsalos para los
  contadores.
- `mensaje` es el estado **actual** del mensaje: si se eliminó después, un `mensaje_nuevo` viejo ya
  llega como lápida. Aplica todo por `id` (upsert): un evento puede llegar dos veces.
- `leido` de otro usuario = «visto»; el tuyo (de otro dispositivo) = baja tu contador.

### SSE: `GET /eventos`

```
retry: 3000

id: 1543
event: mensaje_nuevo
data: {"id":"1543","tipo":"mensaje_nuevo",…}

: latido
```

- `Accept: text/event-stream` (EventSource lo manda; con `fetch`, mándalo). La API reenvía esta ruta
  exacta con un plazo de escritura móvil (60 s, renovado con cada evento o latido) y un máximo de
  31 min; las demás rutas conservan los plazos de siempre.
- Reanudar: `Last-Event-ID: <id>` (EventSource lo hace solo) o `?desde=<id>`. Sin los dos: desde
  ahora.
- `: latido` cada 25 s. **En cada latido y antes de entregar eventos** el servidor revalida la
  credencial: sesión cerrada o vencida, token revocado, usuario desactivado, fuera de la empresa o
  sin `chat.ver` → cierra el flujo (al reconectar, 401/403: no reintentes; vuelve a iniciar sesión).
  El alcance se recalcula de la base: quitar `chat.supervisar` deja de entregar lo de ese alcance
  sin cerrar el flujo. Igual, cada flujo se cierra a los 30 min: reconecta con el último `id`.
- **Topes:** hasta **5 flujos por usuario** por proceso; el sexto responde **429**
  `demasiados_flujos` (no se cierra el más viejo: el cliente debe cerrar el anterior antes de abrir
  otro; un flujo muerto se libera al fallar su siguiente escritura, a más tardar en un latido más el
  plazo). Tope por proceso `IRIS_CHAT_SSE_MAX` (1000): **503** `flujos_agotados` con `Retry-After`;
  la app pasa al sondeo. Las lecturas del feed de los flujos van por un semáforo (16 a la vez).
- 410 `cursor_vencido` (el cursor es anterior a la retención de 7 días): EventSource no reintenta;
  recarga las conversaciones y abre el flujo sin cursor. 401/403: no reintentes.
- El panel usa `EventSource` con la cookie (`withCredentials` si el origen difiere). La app nativa lee
  el flujo con `expo/fetch` (`crearFlujoFetch` de `@iris/sync/chat`): la misma cookie de sesión que el
  resto de sus llamadas (no usa Bearer), `X-Iris-Empresa`, `X-Iris-Canal`, `X-Iris-App` y
  `Last-Event-ID`/`?desde=`; solo en primer plano y con un flujo a la vez. Sin latido en 60 s corta y
  reconecta; un flujo de menos de 5 s es corto y el siguiente espera el doble (3 s a 30 s); tras 3
  cortos, sondeo. 401/403 detienen; 429/503, sondeo con espera.

### Sondeo: `GET /cambios?cursor=<id>&limit=`

- Sin cursor: `{ data: [], nextCursor: "<cabeza>", hayMas: false }`.
- `nextCursor` siempre viene: guárdalo y úsalo en la siguiente consulta (o como `Last-Event-ID`).
  `hayMas: true` = consulta de nuevo enseguida. Intervalo sugerido: 10–15 s con la app abierta.
- 410 `cursor_vencido`: igual que en SSE.

## 5. Cómo concilia la app sin conexión (I4)

1. **Arranque:** `GET /cambios` sin cursor → guarda `nextCursor` → `GET /conversaciones` (y
   `abrir` de su cuadrilla y de las órdenes del día, idempotentes) → abre SSE (o sondea) desde ese
   cursor. Lo que pase entre el cursor y la lista llega dos veces: el upsert por id lo absorbe.
2. **Escribir:** crea el mensaje local con `id` uuid v7 y `creado_en`, lo muestra «pendiente» y lo
   encola (`packages/sync`). La foto va antes por `POST /adjuntos` + subida tus, con el mismo
   tratamiento de la cola de adjuntos de las órdenes.
3. **Enviar la cola:** en orden de creación, uno a uno por conversación. 201/200 → reemplaza el
   local por el del servidor (ya con `seq` y `enviado_en`). Ver §3 para qué no se reintenta.
4. **Al volver la señal:** sigue el feed desde el cursor guardado. Con 410, borra la caché del chat
   (no la cola de salida), recarga y sigue sin cursor.
5. **Hueco de mensajes:** si una conversación quedó atrás (p. ej. se abrió el detalle tras días sin
   señal), `GET /conversaciones/{id}/mensajes?direccion=siguientes&desde_seq=<último seq local>`.
6. **Leído:** al mostrar el último mensaje, `POST /leer` con su `id` (de los recibidos, no de un
   pendiente). Se puede agrupar: solo cuenta el más avanzado.
7. **Eliminar:** solo mensajes ya enviados y dentro de 15 min por `enviado_en`; sin señal se encola
   igual (el servidor responde 422 `plazo_vencido` si llegó tarde: se muestra el error y se deja el
   mensaje).

## 6. Eventos de negocio (outbox)

`chat.conversacion_creada`, `chat.mensaje_enviado`, `chat.mensaje_eliminado`, `chat.mencion` y
`chat.miembro_agregado`
(`contratos/eventos.md`, «Chat»). Nunca llevan el texto: el emisor de push (`internal/notificaciones`) lee el
mensaje por `mensaje_id` y avisa a `destinatarios[]`. No hay `chat.leido` (ver eventos.md).

## 7. Fuera de esta fase

Quitar miembros de un hilo de orden, «restaurar» un mensaje eliminado (§3.1), grupos libres («Despacho»,
«Soporte nivel 2»), menciones de orden o cliente, mensajes de sistema en el hilo de la orden
(«Tomás llegó al sitio»), indicador «escribiendo», presencia y moderación (eliminar mensajes de
otros).
