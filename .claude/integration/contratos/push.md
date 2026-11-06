# Contrato de notificaciones push (app de campo, Android)

Lo implementa I5 en el módulo `apps/api/internal/notificaciones` (API: dispositivos y preferencias;
worker: emisor). Lo usa la app nativa (`apps/movil`, Expo, paquete `gt.horus.iris.campo`). Los tipos
de los endpoints están en `packages/contratos` (operaciones `registrarDispositivo`,
`quitarDispositivo`, `listarDispositivos`, `verPreferenciasNotificaciones`,
`guardarPreferenciasNotificaciones`): no los copies a mano. El mensaje push no pasa por el OpenAPI:
su forma es la de §4 de este documento.

El servidor manda **directo a Firebase Cloud Messaging HTTP v1** (sin el servicio de push de Expo: el
ISP se aloja solo). La app obtiene el token **nativo** de FCM (`getDevicePushTokenAsync()` de
`expo-notifications`, no `getExpoPushTokenAsync()`).

## 1. Registro del dispositivo

Auth: la sesión de la app (cookie `iris_sesion` + `X-CSRF-Token`) o un token `iris_pat_` de una
persona. Un usuario de integración recibe 403. Multiempresa: el dispositivo queda en la **empresa
activa** de la petición (`X-Iris-Empresa`); con varias empresas se registra una vez por empresa (el
mismo token puede estar en varias). Sin conector (sin empresas registradas), en «sin empresa».

| Método y ruta | Qué hace |
| --- | --- |
| `POST /api/v1/dispositivos` `{ token, plataforma, appVersion }` | Registra o renueva. **Idempotente por token:** 201 la primera vez, 200 después (actualiza versión de la app y la sesión). Si el token estaba registrado por **otro usuario**, se le quita a ese usuario (`dispositivo.quitado`, motivo `otro_usuario`) y queda tuyo. Responde `Dispositivo`. |
| `GET /api/v1/dispositivos` | Tus dispositivos en la empresa activa (nunca los de otros). `{ data: Dispositivo[], nextCursor }` |
| `POST /api/v1/dispositivos/{id}/quitar` | Lo quita (deja de recibir push). Repetir o quitar uno ajeno: 404. |

- `token`: el token FCM (100–4096 caracteres de `A-Z a-z 0-9 _ : -`; si no, 422). Hasta **10
  dispositivos por usuario y empresa**: al registrar el undécimo se quita el más viejo
  (`dispositivo.quitado`, motivo `usuario`). `plataforma`: `android` (`ios` queda para
  más adelante: hoy 422). `appVersion`: la versión de la app (`1.4.0`, hasta 40 caracteres).
- `Dispositivo`: `{ id, plataforma, appVersion, tokenFin, empresaId, registradoEn, actualizadoEn,
  ultimoEnvioEn }`. **Nunca devuelve el token**; `tokenFin` son sus últimos 6 caracteres (para
  reconocerlo en «Yo»).
- Acepta `Idempotency-Key` (regla 6).
- **Cuándo registrar:** al entrar, al cambiar de empresa, cuando FCM rota el token
  (`addPushTokenListener`) y una vez al día con la app abierta. Es barato e idempotente.
- **Se quita solo:**
  - al cerrar sesión (`DELETE /api/v1/sesion`): los dispositivos de esa sesión, en la misma
    transacción;
  - cuando la sesión vence o un administrador la cierra, el token se revoca, el usuario se desactiva
    o sale de la empresa: en el siguiente envío, o en la purga de cada hora;
  - cuando FCM responde que el token no vale: `UNREGISTERED`, o `INVALID_ARGUMENT` /
    `SENDER_ID_MISMATCH` con el detalle en `message.token`. Sin ese detalle el envío queda fallido y
    el dispositivo sigue (puede ser un error de configuración del servidor).
  - La app, al iniciar sesión otra vez, vuelve a registrar: no hace falta guardar el `id`.

## 2. Preferencias (de la cuenta: valen en cualquier teléfono)

| Método y ruta | Qué hace |
| --- | --- |
| `GET /api/v1/yo/notificaciones` | `PreferenciasNotificaciones` (sin fila guardada: los valores por defecto con `version: 0`) |
| `POST /api/v1/yo/notificaciones` `{ version, …todo… }` | Guarda (reemplaza todo). `version` la que leíste: distinta → **409** `conflicto_version`. Responde las preferencias con la versión nueva. |

```json
{ "version": 3,
  "tipos": { "chat": true, "mencion": true, "orden": true, "bandeja": true },
  "contenido": "completo",                 // completo | remitente
  "noMolestar": { "activo": true, "desde": "21:00", "hasta": "06:00", "urgenteIgual": true },
  "zonaHoraria": "America/Guatemala",
  "push": { "estado": "configurado", "texto": "Notificaciones push: activas" },   // solo lectura
  "bandejaDisponible": true }                                                    // solo lectura
```

- Por defecto: todo encendido, `contenido: completo`, No molestar **apagado** con 21:00–06:00 y
  `urgenteIgual: true`, `America/Guatemala`.
- `desde`/`hasta`: `HH:MM` (24 h) en `zonaHoraria` (zona IANA válida; si no, 422). `desde > hasta`
  cruza la medianoche; iguales: 422 `horario_invalido`.
- `push.estado`: `configurado` | `sin_configurar` | `error` (las credenciales del servidor no se
  pueden leer). Con `sin_configurar` la app muestra «Notificaciones push: sin configurar» y no pide
  el permiso de Android. Las preferencias se guardan igual.
- `bandejaDisponible`: el usuario tiene `conversaciones.ver` (muestra el interruptor «Bandeja de
  clientes»).
- Guardar emite `notificaciones.preferencias_cambiadas` y queda en la auditoría.

## 3. Qué se avisa

| Tipo y canal Android | Evento que lo dispara | A quién | Urgente |
| --- | --- | --- | --- |
| `chat` · «Mensajes del equipo» | `chat.mensaje_enviado` | `destinatarios[]` del evento menos el autor | no |
| `mencion` · «Menciones» | `conversacion.mencion` (nota interna de la bandeja) y `chat.mencion` (cuando el chat tenga menciones; mismo payload) | el mencionado (`usuario_id`), nunca el autor | **sí** |
| `orden` · «Órdenes» | `orden.asignada` con `tecnico_id` nuevo (distinto del anterior); `orden.programada` con `reprogramada: true` (no la de `origen: asignar`, que ya avisa `orden.asignada`) | el técnico de la orden | **sí** si la orden es para **hoy** (en la zona del usuario) |
| `bandeja` · «Bandeja de clientes» | `conversacion.aviso_sin_atender` («¿Alguien la atiende?») | los `usuarios[]` del evento (disponibles en la cola) | no |

**Al enviar** (no al recibir el evento) el servidor vuelve a comprobar todo, y si algo no se cumple
no manda nada:

1. La sesión (o el token) del dispositivo sigue vigente, el usuario está activo y sigue en la empresa
   del dispositivo. Si no, **quita el dispositivo**.
2. La empresa del evento es la del dispositivo.
3. El tipo está encendido en sus preferencias.
4. Sigue viendo lo que se avisa, con sus permisos de ahora:
   - `chat`/`mencion` de chat: `chat.ver` y es miembro (o lo cubre su alcance) según `contratos/chat.md`
     §1 — quien salió de la cuadrilla deja de recibir sus push. Un mensaje eliminado no se avisa.
   - `mencion` de bandeja y `bandeja`: `conversaciones.ver` y ve la conversación (sesión de persona,
     no token). `bandeja` además: la conversación sigue **sin asignar** y no resuelta.
   - `orden`: `ordenes.ver_propias` u `ordenes.ver`, la orden sigue asignada a él y no está cerrada,
     fallida ni cancelada.
5. **No molestar:** dentro del horario solo sale lo urgente y solo con `urgenteIgual`. Lo demás **no
   se guarda para después** (la app lo muestra al abrirse).

Un evento repetido nunca manda dos veces (un envío por evento y dispositivo). Si una misma acción
asigna y reprograma la orden, sale **un** push (`cambio: asignada`).

## 4. El mensaje (FCM HTTP v1)

**Solo datos** (`data`, sin `notification`): la app arma la notificación (canal, versión pública,
agrupación y acciones), también con la app cerrada (tarea en segundo plano de
`expo-notifications`, `Notifications.registerTaskAsync`). Con la app en primer plano, la app decide
(no la muestra si ya está en esa pantalla).

```json
{ "message": {
    "token": "<token FCM>",
    "data": { …claves de abajo, todas texto… },
    "android": { "priority": "HIGH", "ttl": "3600s", "collapse_key": "orden-102" } } }
```

`ttl`: `chat`, `mencion` y `bandeja` 1 h; `orden` 12 h. `collapse_key` solo en `orden`
(`orden-<id>`): sin señal, el teléfono recibe solo el último cambio de esa orden.

### Claves comunes de `data`

| Clave | Ejemplo | Para qué |
| --- | --- | --- |
| `v` | `1` | Versión de este contrato |
| `id` | `8812` | Id del envío: ignora uno repetido |
| `tipo` | `chat` | `chat` \| `mencion` \| `orden` \| `bandeja` |
| `canal` | `chat` | **Id del canal de Android** (igual que `tipo`). La app crea los cuatro canales al iniciar: `chat` «Mensajes del equipo», `mencion` «Menciones», `orden` «Órdenes», `bandeja` «Bandeja de clientes» |
| `usuarioId` | `7` | Para quién es: si en el teléfono hay otra sesión, se descarta |
| `empresaId` | `1` (o vacío sin conector) | Empresa con que se abre (`X-Iris-Empresa`) |
| `titulo` | `Cuadrilla Norte` | Título de la notificación |
| `cuerpo` | `Carlos Ruiz: ya llegué a la casa` | Texto (ya respeta `contenido`) |
| `publicoTitulo` | `Nuevo mensaje` | **Versión pública** (pantalla bloqueada): `setPublicVersion` con este título, sin cuerpo y sin acciones |
| `grupo` | `chat-12` | Agrupa (`setGroup`): una conversación, una orden |
| `etiqueta` | `chat-0193…` | `tag` de la notificación: la misma etiqueta reemplaza a la anterior |
| `enlace` | `iris://chat/12` | Al tocar. **Formato interno del mensaje**: la app no registra el esquema `iris` en Android (solo `iris-campo`), así que no se abre desde afuera; la app lo valida (`iris://chat|orden|conversaciones/<id entero>`) y nunca navega a otra cosa |
| `acciones` | `responder,leido` | Botones, en orden (§5) |
| `ocurridoEn` | `2026-11-06T20:32:10Z` | Hora del hecho (`setWhen`) |
| `datos` | `{"conversacionId":12,…}` | JSON con lo propio del tipo (abajo) |

Textos de la versión pública: `chat` «Nuevo mensaje», `mencion` «Te mencionaron», `orden` «Orden
actualizada», `bandeja` «Cliente esperando».

### `datos` por tipo

**`chat`** (`grupo` `chat-<conversacionId>`, `etiqueta` `chat-<mensajeId>`, `enlace`
`iris://chat/<conversacionId>`, acciones `responder,leido`):

```json
{ "conversacionId": 12, "conversacionTipo": "cuadrilla", "conversacionTitulo": "Cuadrilla Norte",
  "mensajeId": "0193…", "seq": 41,
  "remitente": { "id": 5, "nombre": "Carlos Ruiz", "iniciales": "CR" },
  "contenido": "texto",                // texto | foto | ubicacion
  "texto": "ya llegué a la casa",      // solo con contenido = completo; null si no
  "ordenId": null, "cuadrillaId": 3 }
```

- `titulo`: la conversación (directa: el nombre del otro; cuadrilla: su nombre; orden: el número,
  `OT-000102`). `cuerpo`: con `completo`, `Carlos Ruiz: <texto>` (en una directa solo el texto); con
  `remitente`, `Nuevo mensaje de Carlos Ruiz`.
- `texto` se recorta a **200 caracteres** (con «…»). Una foto: `📷 Foto`; una ubicación:
  `📍 Ubicación` (con `contenido` el tipo real). Con texto y foto, el texto.
- La app agrupa por conversación con `MessagingStyle` (avatar con `iniciales`, nombre y texto de
  cada mensaje) y el resumen «Cuadrilla Norte · 2 mensajes».

**`mencion`** (`grupo` `mencion-<conversacionId>`, `etiqueta` `mencion-<mensajeId>`):

```json
{ "origen": "bandeja",                 // bandeja | chat
  "conversacionId": 55, "conversacionTitulo": "Juan Pérez", "mensajeId": "0193…",
  "remitente": { "id": 9, "nombre": "Ana López", "iniciales": "AL" },
  "texto": "@Juan ¿traes el conector SC/APC?" }   // solo con completo
```

- `titulo` y `conversacionTitulo`: el contacto de la conversación (bandeja) o la conversación
  (chat). Con `remitente`, en la bandeja: `Conversación con un cliente` (sin el nombre del cliente).
- `cuerpo`: `Ana López te mencionó: <texto>` o, con `remitente`, `Ana López te mencionó`.
- `enlace`: `iris://conversaciones/<id>` (bandeja) o `iris://chat/<id>` (chat). Acciones
  `responder,leido`.

**`orden`** (`grupo` y `etiqueta` `orden-<ordenId>`, `enlace` `iris://orden/<ordenId>`, acciones
`ver_orden,como_llegar`):

```json
{ "ordenId": 123, "numero": "OT-000123", "cambio": "asignada",   // asignada | reprogramada
  "tipo": "instalacion", "tipoNombre": "Instalación",
  "zona": "Villa Nueva", "cliente": "Familia Morales",           // o null
  "inicio": "2026-11-06T20:00:00Z", "fin": "2026-11-06T22:00:00Z",  // o null sin programar
  "ventanaInicio": null, "ventanaFin": null, "hoy": true }
```

- `titulo`: `OT-000123 asignada para hoy` / `OT-000123 asignada para mañana` /
  `OT-000123 asignada para el lun 9 nov` / `OT-000123 asignada` (sin fecha); `reprogramada` igual.
- `cuerpo`: `14:00–16:00 · Villa Nueva · Instalación · Familia Morales` (la ventana acordada si
  la hay; si no, la cita; omite lo que falte). Horas en la zona del usuario.
- `contenido` no cambia este tipo. Las coordenadas no viajan: «Cómo llegar» abre la orden del
  paquete del día (ya en el teléfono).

**`bandeja`** (`grupo` y `etiqueta` `conversacion-<id>`, `enlace` `iris://conversaciones/<id>`,
acción `ver`):

```json
{ "conversacionId": 55, "cola": "Soporte", "contacto": "Juan Pérez",   // contacto solo con completo
  "botContestaEn": "2026-11-06T21:02:00Z" }                             // o null
```

- `titulo`: `¿Alguien la atiende?`; `cuerpo`: `Cliente esperando en Soporte · Juan Pérez`.

## 5. Acciones

| Acción | Botón | Qué hace la app |
| --- | --- | --- |
| `responder` | «Responder» (con campo de texto, `RemoteInput`) | **chat:** encola el mensaje con `packages/sync` como cualquier otro (`POST /api/v1/mensajeria/chat/conversaciones/{conversacionId}/mensajes` `{ id: <uuid v7>, texto }`, contratos/chat.md §3). **mención de bandeja:** nota interna `POST /api/v1/mensajeria/conversaciones/{conversacionId}/notas` `{ id, texto, menciones: [remitente.id] }`. Luego quita la notificación |
| `leido` | «Marcar como leído» | **chat:** `POST /api/v1/mensajeria/chat/conversaciones/{conversacionId}/leer` `{ hasta_mensaje_id: <mensajeId> }`. **bandeja:** `POST /api/v1/mensajeria/conversaciones/{conversacionId}/leer` `{ hastaMensajeId: <mensajeId> }`. Quita las notificaciones del `grupo` |
| `ver_orden` | «Ver orden» | Abre `iris://orden/<ordenId>` |
| `como_llegar` | «Cómo llegar» | Abre la navegación con las coordenadas de la orden que ya están en el teléfono (sin ellas, abre la orden) |
| `ver` | «Ver» | Abre `enlace` (en la bandeja, «La atiendo» se hace en la pantalla: `POST …/tomar` pide la `version`) |

Sin señal, `responder` y `leido` se encolan (la cola de `packages/sync` los manda al volver la señal).

## 6. Eventos (outbox)

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `dispositivo.registrado` | `dispositivo_id`, `usuario_id`, `empresa_id`, `plataforma`, `app_version`, `token_fin`, `nuevo` (bool) | `POST /dispositivos` (también al renovar: `nuevo: false`) |
| `dispositivo.quitado` | `dispositivo_id`, `usuario_id`, `empresa_id`, `token_fin`, `motivo` (`usuario` \| `sesion_cerrada` \| `token_invalido` \| `otro_usuario` \| `sin_acceso`) | `POST /dispositivos/{id}/quitar`, cierre de sesión, purga, respuesta de FCM o el token pasó a otro usuario |
| `notificaciones.preferencias_cambiadas` | `usuario_id`, `campos[]`, `version` | `POST /yo/notificaciones` |

Nunca llevan el token completo ni textos de mensajes.

## 7. Servidor (para operar)

- `IRIS_FCM_PROYECTO` (id del proyecto de Firebase) e `IRIS_FCM_CREDENCIALES` (ruta del JSON de la
  cuenta de servicio, montado de solo lectura). Sin las dos, el push queda apagado: la API funciona,
  las preferencias y los dispositivos se guardan y `GET /api/v1/salud` dice
  `"notificacionesPush": "sin_configurar"`. Runbook: `docs/runbooks/notificaciones-push.md`.
- Reintentos: 429 y 5xx con espera exponencial (hasta 8 intentos); 4xx no se reintenta.
- Los registros nunca llevan el token ni el texto; la auditoría guarda `token_fin`.
