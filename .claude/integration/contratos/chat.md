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
| `orden` | orden | su técnico asignado (`campo.ordenes.tecnico_id`) + `chat.supervisar` global, con la zona o con la cuadrilla de la orden + despacho que se unió (`ordenes.ver` con alcance sobre la orden) |

- Los miembros de cuadrilla y de orden **se derivan al leer**: si la orden se reasigna, el técnico
  nuevo entra y el anterior sale; si un técnico cambia de cuadrilla, cambia de chat. Solo quien se
  une a un hilo de orden desde despacho queda guardado como miembro.
- Las directas nunca se ven por alcance: ninguna supervisión lee mensajes directos.
- Lo que no ves responde **404** (no se revela que existe).
- Permisos: `chat.ver` (leer, marcar leído, flujo), `chat.enviar` (abrir, enviar, eliminar),
  `chat.supervisar` (alcance). Roles base: técnico, despacho, soporte y supervisor tienen ver y
  enviar; supervisor y admin, supervisar; lectura, solo ver.

## 2. Endpoints

| Método y ruta | Permiso | Qué hace |
| --- | --- | --- |
| `GET /conversaciones?cursor=&limit=&tipo=` | ver | Mis conversaciones, la más reciente primero. `{ data: ConversacionChat[], nextCursor }` |
| `GET /conversaciones/{id}` | ver | `ConversacionChatDetalle`: la conversación + `lecturas[]` (hasta dónde leyó cada uno: «visto») |
| `POST /directas` `{ usuario_id }` | enviar | Abre o trae la directa. 201 nueva, 200 existente. 422 `usuario_no_disponible` |
| `POST /cuadrillas/{cuadrilla_id}/abrir` | enviar | Abre o trae el chat de la cuadrilla (técnico de ella o supervisión). 201/200; 404 |
| `POST /ordenes/{orden_id}/abrir` | enviar | Abre o trae el hilo de la orden. Despacho queda como miembro (201 al unirse). 404 |
| `GET /conversaciones/{id}/mensajes?direccion=&cursor=&limit=&desde_seq=` | ver | Mensajes, siempre en `seq` ascendente. `anteriores` (defecto): del más reciente hacia atrás; `siguientes`: hacia adelante |
| `POST /conversaciones/{id}/mensajes` | enviar | Envía (ver §3). 201 nuevo, 200 reenvío |
| `POST /mensajes/{mensaje_id}/eliminar` | enviar | Lápida. Solo el autor, hasta 15 min (403 `no_es_autor`, 422 `plazo_vencido`). Repetir: 200 |
| `POST /conversaciones/{id}/leer` `{ hasta_mensaje_id }` | ver | Avanza tu lectura (nunca retrocede). `{ conversacion_id, leido_hasta_mensaje_id, no_leidos }` |
| `GET /cambios?cursor=&limit=` | ver | Sondeo del feed (§4). `{ data: EventoChat[], nextCursor, hayMas }` |
| `GET /eventos` | ver | Server-Sent Events (§4) |

Abrir una conversación es idempotente por su naturaleza (una por par, cuadrilla u orden); acepta
`Idempotency-Key` como cualquier POST.

### `ConversacionChat`

```json
{ "id": 12, "tipo": "orden", "empresa_id": 1, "titulo": "OT-000102",
  "cuadrilla_id": null, "orden_id": 102, "orden_estado": "en_sitio",
  "otro_usuario": { "id": 7, "nombre": "Andrea Castillo" },          // solo directa
  "ultimo_mensaje": { "id": "0193…", "autor_id": 5, "autor_nombre": "Tomás Herrera",
                      "contenido": "texto", "texto": "Listo, −21 dBm…", "enviado_en": "…" },
  "no_leidos": 2, "leido_hasta_mensaje_id": "0193…",
  "ultima_actividad": "…", "creada_en": "…" }
```

`titulo`: el otro usuario (directa), el nombre de la cuadrilla o el número de la orden (el cliente
lo arma el panel con la orden). `contenido`: `texto | foto | ubicacion | eliminado`; `texto` son los
primeros 140 caracteres. `ultimo_mensaje` y `otro_usuario` faltan si no aplican. `no_leidos`:
mensajes de otros, sin eliminar, después de tu lectura.

### `MensajeChat`

```json
{ "id": "0193…", "conversacion_id": 12, "seq": 41, "autor_id": 5, "autor_nombre": "Tomás Herrera",
  "texto": "La ONU tiene LOS en rojo.",                          // null si no tiene o se eliminó
  "adjunto": { "id": "…", "estado": "subiendo", "url_descarga": "/api/v1/adjuntos/…" },  // opcional
  "ubicacion": { "lat": 14.6, "lon": -90.5 },                     // opcional
  "creado_en": "…",   // hora del teléfono al escribirlo (o null)
  "enviado_en": "…",  // hora del servidor al recibirlo («enviado sin señal, llegó 11:34»)
  "eliminado": false, "eliminado_en": null }
```

**Texto plano.** El servidor no limpia nada y nunca interpreta HTML: el cliente lo muestra como
texto (React lo escapa; nada de `dangerouslySetInnerHTML` ni Markdown). Con `eliminado: true` se
muestra «Mensaje eliminado».

## 3. Enviar, idempotencia y orden

```
POST /conversaciones/{id}/mensajes
{ "id": "<uuid v7>", "texto": "…", "adjunto_id": "<uuid>", "ubicacion": { "lat": …, "lon": … }, "creado_en": "…" }
```

- `id` lo genera el cliente **al escribir** (uuid v7; otro formato: 422) y se guarda en la cola
  local. Reenviar el mismo `id` con el mismo contenido responde **200** con el mensaje actual y
  `Idempotent-Replayed: true` (aunque ya se haya eliminado: llega la lápida). El mismo `id` con otro
  contenido (o de otro autor): **409** `mensaje_id_reutilizado` — es un error del cliente; no se
  reintenta.
- `Idempotency-Key` (regla 6) también se acepta; el `id` basta.
- Al menos uno de `texto` (1–4000 caracteres tras recortar los extremos), `adjunto_id` o
  `ubicacion`; si no, 422 `mensaje_vacio`. `creado_en` no puede ser futuro (más de 5 min).
- **Foto:** primero `POST /api/v1/adjuntos` con `entidad: "chat_conversacion"`, `entidadId:
  "<id de la conversación>"`, `tipo: "foto"`, y el mensaje con su `adjunto_id`. El mensaje puede
  salir antes de terminar la subida (`adjunto.estado` = `pendiente`/`subiendo`; se muestra al estar
  `completo`). La foto debe ser tuya, de esa conversación y no rechazada (422 `adjunto_invalido`,
  `adjunto_no_es_foto`, `adjunto_rechazado`); una foto va en un solo mensaje (409).
- **Límite:** 30 mensajes por minuto por usuario → 429 `demasiados_mensajes` con `Retry-After`. Los
  reenvíos no cuentan. La cola espera `Retry-After` y sigue con el mismo `id`.
- **Orden:** `seq` lo asigna el servidor al recibir (orden de llegada, por conversación). Se ordena
  por `seq`; `creado_en` solo se muestra. Mientras un mensaje está en la cola local no tiene `seq`:
  va al final, marcado «se envía al tener señal».
- Errores que la cola **no** reintenta: 403, 404 (ya no eres miembro), 409, 422. Reintenta 429 (con
  `Retry-After`), 5xx y sin red, con espera exponencial.

## 4. Tiempo real y sondeo

Un solo feed por usuario y empresa: los cambios de las conversaciones que ves, con un cursor
creciente. SSE y sondeo dan el **mismo** `EventoChat` con el **mismo** cursor: se puede pasar de uno a
otro sin perder nada.

```json
{ "id": "1543", "tipo": "mensaje_nuevo", "conversacion_id": 12, "ocurrido_en": "…", "no_leidos": 3,
  "mensaje": { …MensajeChat… } }               // mensaje_nuevo, mensaje_eliminado
{ "id": "1544", "tipo": "leido", "conversacion_id": 12, "ocurrido_en": "…", "no_leidos": 0,
  "lectura": { "usuario_id": 7, "hasta_mensaje_id": "0193…", "leido_en": "…" } }
{ "id": "1545", "tipo": "conversacion_actualizada", "conversacion_id": 13, "ocurrido_en": "…", "no_leidos": 0,
  "conversacion": { …ConversacionChat… } }       // nueva, o te uniste
```

- `no_leidos` es el tuyo en esa conversación al leer el evento: úsalo para el contador.
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

- `Accept: text/event-stream` (EventSource lo manda; un cliente `fetch` debe mandarlo: es lo que
  quita los plazos del proxy de la API).
- Reanudar: `Last-Event-ID: <id>` (EventSource lo hace solo) o `?desde=<id>`. Sin los dos: desde
  ahora.
- `: latido` cada 25 s. El servidor cierra cada flujo a los 30 min: reconecta con el último `id`
  (así se vuelven a verificar sesión y permisos).
- 410 `cursor_vencido` (el cursor es anterior a la retención de 7 días): EventSource no reintenta;
  recarga las conversaciones y abre el flujo sin cursor. 401/403: no reintentes.
- El panel usa `EventSource` con la cookie (`withCredentials` si el origen difiere). La app usa un
  cliente SSE con cabeceras (Bearer), o el sondeo.

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

`chat.conversacion_creada`, `chat.mensaje_enviado` y `chat.mensaje_eliminado`
(`contratos/eventos.md`, «Chat»). Nunca llevan el texto: el emisor de push (fase siguiente) lee el
mensaje por `mensaje_id` y avisa a `destinatarios[]`. No hay `chat.leido` (ver eventos.md).

## 7. Fuera de esta fase

Push a teléfonos (Expo/FCM; el evento ya trae `destinatarios`), grupos libres («Despacho»,
«Soporte nivel 2»), menciones de orden o cliente, mensajes de sistema en el hilo de la orden
(«Tomás llegó al sitio»), indicador «escribiendo», presencia y moderación (eliminar mensajes de
otros).
