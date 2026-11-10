# Contrato de la bandeja de conversaciones con clientes (fase 1, sin IA)

Lo implementa I1 en el servicio de mensajería (`apps/api/cmd/mensajeria`, schema `mensajeria`,
código en `apps/api/internal/mensajeria/conv_*.go`). Lo usa el panel (I3, «Conversaciones» y
«Administración / Números y canales»). Los tipos están en `packages/contratos` (operaciones
`*Conversacion*`, `*Cola*`, `*Etiqueta*`, `*RespuestaRapida*`, `*ContactoIris*`, `*Horario*`,
`*Manual*`): no los copies a mano.

Base: `/api/v1/mensajeria`. La API la reenvía tal cual al proceso de mensajería. Auth: la bandeja
es de **personas con sesión del panel** (cookie `HttpOnly` + CSRF) y `conversaciones.ver`: toda
lectura y acción sobre una conversación (también las que piden otro permiso, como `crear-ticket`
con `tickets.crear`, boletas, promesas o adjuntos de la entidad `conversacion`) responde **404** a
un token de API (`Authorization: Bearer`, aunque sea de un supervisor), a un usuario de
integración (`integracion_argos`, `integracion_horus`) y a quien no tiene `conversaciones.ver`. Multiempresa: la empresa activa (`X-Iris-Empresa`
si el usuario tiene varias); todo lo de aquí lleva `empresa_id` y se filtra por ella. Errores RFC
9457 (`type: urn:iris:problema:<codigo>`). Listas `{ data, nextCursor }` con `?cursor=&limit=`.
Las acciones son `POST …/<verbo>` (nunca PATCH de estado) y aceptan `Idempotency-Key`; las que
editan algo llevan `version` (409 `conflicto_version` si cambió).

Entrega en dos PR:

- **PR A** (§1–§9): varios números por empresa, conversaciones, colas, asignación, etiquetas, notas,
  menciones, contactos, acciones (ticket, orden, prospecto, boleta, promesa), respuestas rápidas y
  tiempo real.
- **PR B** (§10–§11): horarios y feriados (entidad genérica), fuera de horario, guardia y biblioteca
  de manuales.

La opción «que atienda un asistente» y todo lo de IA son de la **fase 2**; aquí solo quedan los
puntos de extensión (§10.4). El asistente que **sugiere** respuestas (fase 2, aprobado) está en §16.

## 1. Números de WhatsApp (canales)

Una empresa tiene **varios números** (`mensajeria.canales`), cada uno con:

| Campo | Qué es |
| --- | --- |
| `usos[]` | Uno o varios de `avisos`, `soporte`, `cobros`, `ventas`. Es una etiqueta para la bandeja (color del número) y para el filtro; no restringe lo que se puede contestar. |
| `avisosEmpresa` | `true` en **uno solo por empresa** (activo): es el número por el que salen los avisos de §6 del contrato con Argos (`POST /avisos`). Exige `avisos` en `usos`. Al crear el primer número con `avisos` de la empresa queda marcado solo. Marcar otro lo quita del anterior en la misma transacción. Sin número de avisos activo: los avisos quedan `bloqueado` / `canal_no_disponible`. |
| `colaId` | Cola a la que llegan las conversaciones nuevas de ese número (null = sin cola). |
| `horarioId`, `fueraDeHorario` | PR B (§10). |

- El número que existía antes de esta versión quedó migrado como `usos = [avisos, soporte]` y
  `avisosEmpresa = true`. Los avisos no cambian.
- Si un cliente responde a un aviso, la conversación queda en **ese** número (la de
  `(empresa, número, contacto)`) y lleva el evento de sistema «Respondió al aviso «…»» (§3.4).
- Credenciales y datos de Meta: igual que antes (`POST /canales`, `POST /canales/{id}/actualizar`,
  permiso `mensajeria.configurar`). `POST /canales` acepta además `usos`, `avisosEmpresa` y `colaId`.
- Uso, cola, número de avisos y (PR B) horario: `POST /canales/{id}/configurar`
  `{ version, usos, avisosEmpresa, colaId, horarioId?, fueraDeHorario? }` con `canales.configurar`
  (solo personas con sesión: un token o una integración recibe 404).
- 409 `canal_existente` solo si el `phoneNumberId` ya está en otro número **activo** (al crear, al
  actualizar o al activar). Hay varios números por empresa: no hay límite de uno activo. (Caso raro:
  activar un número que conserva `avisosEmpresa: true` cuando la empresa ya tiene otro número de
  avisos activo también responde `canal_existente`; márcalo de avisos con `configurar` después.)
  Crear con `avisosEmpresa: true` lo quita del número de avisos anterior (antes respondía 409).
- **Varios números en la misma app de Meta.** Meta manda los webhooks de todos los números de una
  app a una sola URL, salvo que cada número tenga la suya: configura en Meta, por número,
  `override_callback_uri` = `…/api/v1/mensajeria/canales/{id}/webhook` (Graph API
  `POST /{waba_id}/subscribed_apps` con `override_callback_uri` y `verify_token`, o por número con
  `POST /{phone_number_id}` `webhook_configuration.override_callback_uri`). Si aun así llega a un
  canal un cambio de otro `phone_number_id`, Iris lo pasa al número activo de ese
  `phone_number_id` **solo** si es de la misma empresa y tiene el mismo `app_secret` con el que se
  verificó la firma; si no, lo descarta (queda anotado en el entrante).

### 1.1 Alta, credenciales y conexión de un número (pantalla «Números de WhatsApp»)

Todo con `mensajeria.configurar`. Las credenciales (token, app secret, verify token) se guardan
cifradas y **nunca** se devuelven ni salen en logs, auditoría o eventos.

**Campos nuevos de `Canal`** (los devuelven `GET /canales`, `GET /canales/{id}` y las respuestas de
crear, actualizar, configurar, activar, desactivar):

| Campo | Qué es |
| --- | --- |
| `estado` | `activo` \| `inactivo` (lo mismo que `activo`, para la columna de la lista) |
| `conexion` | Lo que muestra la columna «Conexión», calculado en el servidor: `desactivado` (inactivo), `sin_probar` (nunca se probó o cambiaron token, `phoneNumberId` o `numero` después de la última prueba), `token_invalido`, `numero_no_coincide`, `sin_permiso`, `error_meta` (la última prueba falló así), `webhook_sin_verificar` (la prueba dio `conectado` pero Meta aún no verificó el webhook) o `conectado` |
| `ultimoEstado` | Resultado de la última prueba (`conectado`, `token_invalido`, `numero_no_coincide`, `sin_permiso`, `error_meta`) o null (sin probar) |
| `ultimoDetalle` | Texto corto en español de la última prueba (null si fue `conectado`); nunca lleva el token |
| `ultimaPruebaEn` | Cuándo se probó por última vez (null = nunca) |
| `webhookVerificadoEn` | Última verificación correcta de Meta (`GET …/webhook` con `hub.challenge` y el verify token del número). Null = Meta no la ha hecho, o se cambió el verify token después |
| `ultimoMensajeEn` | Último webhook firmado que llegó de Meta a este número (cualquier cambio: mensaje, estado, calidad). Precisión de 1 minuto |
| `tokenGuardadoEn`, `appSecretGuardadoEn`, `verifyTokenGuardadoEn` | Cuándo se guardó por última vez cada credencial (para «•••• guardado el …»). Nunca el valor |
| `webhookUrl` | URL completa para pegar en Meta (`IRIS_BASE_URL` + `webhookRuta`). Sin `IRIS_BASE_URL` es solo la ruta y `webhookUrlCompleta` es `false` (el panel antepone su propio origen) |
| `webhookUrlCompleta` | `true` si `webhookUrl` es absoluta |
| `verifyTokenGenerado` | **Solo** en la respuesta de crear o actualizar cuando Iris generó el verify token (ver abajo). Ausente en cualquier otra respuesta |

`webhookRuta` se mantiene. Probar, la verificación del webhook y la llegada de webhooks **no**
cambian `version` (no le dan 409 a un formulario abierto).

**Crear** (`POST /canales`): `verifyToken` pasa a ser opcional. Vacío o ausente = Iris genera uno
(`vt_` + 32 caracteres aleatorios) y lo devuelve **una sola vez** en `verifyTokenGenerado`. Si el
cliente manda el suyo (16–256), se usa y la respuesta no trae `verifyTokenGenerado`. Con
`Idempotency-Key`, una respuesta con `verifyTokenGenerado` no se guarda: repetir la clave responde 409
`idempotencia_no_repetible` (el número ya quedó creado; se ve en la lista).

**Actualizar** (`POST /canales/{id}/actualizar`): suma `generarVerifyToken: bool` («Generar otro»):
Iris genera uno nuevo y lo devuelve una vez en `verifyTokenGenerado` (no se puede mandar junto con
`verifyToken`: 422 `validacion`). Cambiar el verify token (generado o enviado) borra
`webhookVerificadoEn`: hay que pegarlo en Meta y verificar otra vez. Cambiar `token`,
`phoneNumberId` o `numero` borra el resultado de la última prueba (`conexion: sin_probar`).

**Detalle**: `GET /canales/{id}` → `Canal`. Solo personas con sesión del panel (un token de API o una
integración reciben 404; `GET /canales` no cambia). 404 si no existe, es de otra empresa o no tiene
empresa cuando ya hay empresas registradas (lo mismo en probar, actualizar, activar y desactivar).

**Probar conexión**: `POST /canales/{id}/probar`, sin cuerpo (acepta `Idempotency-Key`). Solo
personas con sesión del panel: un token de API o una integración recibe 404. Iris llama a la Graph
API `GET /{phone_number_id}?fields=display_phone_number,verified_name,quality_rating` con el token
guardado (mismo host y tiempos que los envíos; 15 s) y responde 200 `PruebaCanal`:

```
{ canalId, estado, detalle, probadoEn, numeroMeta, nombreVerificado, calidad,
  webhookVerificado, webhookVerificadoEn, ultimoMensajeEn, conexion }
```

| `estado` | Cuándo |
| --- | --- |
| `conectado` | 2xx, el `id` es el `phoneNumberId` del número y `display_phone_number` es `numero` (se comparan solo los dígitos) |
| `token_invalido` | HTTP 401 o error de Meta con código 190 (token vencido, revocado o mal pegado) |
| `numero_no_coincide` | El `display_phone_number` de Meta no es `numero`, el `id` devuelto es otro, o Meta no encuentra ese `phone_number_id` con este token (código 100, subcódigo 33). `numeroMeta` trae lo que Meta dice cuando lo sabe |
| `sin_permiso` | HTTP 403 o códigos 10 o 200–299 (al token le falta `whatsapp_business_management` / `whatsapp_business_messaging` o acceso a la WABA) |
| `error_meta` | Cualquier otra cosa: Meta no respondió, 5xx, 429, cuerpo ilegible. `detalle` dice qué pasó sin el mensaje de Meta (que puede repetir el token) |

- `calidad`: `alta` (GREEN), `media` (YELLOW), `baja` (RED), `desconocida` (otro), o null si no se
  leyó. `nombreVerificado`: el `verified_name` de Meta (o null).
- `webhookVerificado` = `webhookVerificadoEn` no es null.
- El resultado queda en el número (`ultimoEstado`, `ultimoDetalle`, `ultimaPruebaEn`), con su fila de
  auditoría (`mensajeria.probar_canal`, sin credenciales) y el evento `canal_mensajeria.probado`
  `{ canal_id, empresa_id, estado }`. Un número inactivo también se puede probar.
- 404 si el número no existe o es de otra empresa; 422 `cifrado_no_disponible` sin `IRIS_CLAVE_CIFRADO`.

## 2. Colas, etiquetas y respuestas rápidas

**Cola** (`mensajeria.colas`): `nombre` (único por empresa), `descripcion`, `slaRespuestaMin`
(minutos para contestar un mensaje del cliente; null = sin SLA), `slaPorVencerMin` (cuánto antes
de vencer pasa a «por vencer»; por defecto 25 % del SLA), `miembros[]` (agentes), `activa`,
`version`. Una cola define **el alcance**: un agente ve las conversaciones de las colas de las que
es miembro (§4).

| Método y ruta | Permiso | Qué hace |
| --- | --- | --- |
| `GET /colas` | `conversaciones.ver` | Colas de la empresa (con miembros) |
| `POST /colas` | `canales.configurar` | Crear |
| `POST /colas/{id}/actualizar` | `canales.configurar` | Nombre, descripción y SLA (`version`) |
| `POST /colas/{id}/miembros` | `canales.configurar` | Fija la lista de miembros `{ version, usuarioIds[] }` (usuarios activos de la empresa con `conversaciones.ver`; si no, 422 `usuario_no_disponible`) |
| `POST /colas/{id}/activar` · `/desactivar` | `canales.configurar` | Una cola inactiva no recibe transferencias ni conversaciones nuevas (las de un número con esa cola llegan sin cola) |

**Etiqueta** (`mensajeria.etiquetas`): `nombre` único por empresa, `color` (`neutro`, `marca`,
`info`, `exito`, `alerta`, `peligro`: tokens de la identidad Horus), `activa`.
`GET /etiquetas` (`conversaciones.ver`); `POST /etiquetas`, `/etiquetas/{id}/actualizar`,
`/activar`, `/desactivar` (`conversaciones.configurar`).

**Respuesta rápida** (`mensajeria.respuestas_rapidas`): `atajo` (`/visita`, único por empresa),
`titulo`, `texto` (texto plano, ≤ 4000, con `{{nombre}}` opcional que el panel reemplaza),
`colaId` (null = todas las colas), `activa`. `GET /respuestas-rapidas?colaId=&q=`
(`conversaciones.responder`: las de la cola y las generales); `POST /respuestas-rapidas`,
`/{id}/actualizar`, `/activar`, `/desactivar` (`conversaciones.configurar`). Usarla es mandar su
texto como un mensaje normal (opcional `respuestaRapidaId` en el envío, solo para métricas).

## 3. Conversación y mensajes

### 3.1 Conversación

Una por **(empresa, número, contacto)**: el contacto es el teléfono de WhatsApp (E.164). No se
cierra nunca: se resuelve y se reabre.

```json
{ "id": 41, "empresaId": 1, "version": 7,
  "canal": { "id": 3, "nombre": "Soporte", "numero": "+50222000010", "usos": ["soporte"] },
  "cola": { "id": 2, "nombre": "Soporte" },                         // o null
  "numero": "+50255120041", "perfilNombre": "Carlos R.",            // nombre de WhatsApp
  "titulo": "Carlos Ruiz",            // cliente > contacto de Iris > perfil de WhatsApp > número
  "contacto": { "tipo": "cliente", "clienteId": 4101, "servicioId": 41010,
                "contactoIrisId": null, "prospectoId": null, "asociacion": "automatica" },
  "estado": "abierta", "reabiertaEn": null,
  "asignado": { "id": 7, "nombre": "Andrea Castillo" },             // o null
  "etiquetas": [{ "id": 3, "nombre": "Sin servicio", "color": "peligro" }],
  "ventana": { "abierta": true, "cierraEn": "…" },                  // 24 h desde el último mensaje del cliente
  "sla": { "estado": "por_vencer", "venceEn": "…", "esperaDesde": "…" },  // estado: sin | en_tiempo | por_vencer | vencido
  "ultimoMensaje": { "id": "…", "direccion": "entrante", "tipo": "texto", "texto": "Ya lo reinicié…", "creadoEn": "…" },
  "noLeidos": 2, "ultimaActividad": "…", "creadaEn": "…" }
```

Los objetos opcionales (`cola`, `asignado`, `ultimoMensaje`, `clienteResumen` y, en los mensajes,
`autor`, `adjunto`, `ubicacion`, `plantilla`, `evento`) van **ausentes** cuando no aplican (no
`null`): huma no publica objetos anulables. Los escalares opcionales sí van `null`. Las **listas**
van siempre `[]` cuando están vacías (nunca `null`), y el contrato las declara no anulables.

`contacto.tipo`: `cliente` (cliente de Argos: solo su id; nombre, servicio, saldo y zona salen de la
instantánea de lectura o de `GET /clientes/{id}` de la API, nunca de una copia), `contacto_iris`
(§5.2), `prospecto` o `desconocido`. `asociacion`: `automatica` (una sola coincidencia exacta del
número en la instantánea de Argos de la empresa al llegar el primer mensaje) o `manual`.
`verificado`: el número de la conversación es un teléfono del cliente en la instantánea
(`telefono`, `movil` o `contacto_telefono`), o un supervisor confirmó la asociación (§5.1). Sin
verificar no hay plantillas fuera de la ventana, boletas ni promesas a su nombre (422
`cliente_sin_confirmar`).

**Detalle** (`GET /conversaciones/{id}`): lo anterior más `vinculos[]` (tickets, órdenes,
prospectos, boletas y promesas creados desde la conversación: `{ tipo, id, referencia, creadoEn }`,
p. ej. `{ tipo: "ticket", id: 4812, referencia: "TK-004812" }`), `participantes[]` (mencionados) y
`clienteResumen` (de la instantánea: `nombre`, `codigo`, `servicios[] { id, codigo, plan, estado,
zona }`, `sincronizadoEn`; null si no hay cliente o no hay instantánea).

### 3.2 Estados

| Estado | Cuándo |
| --- | --- |
| `abierta` | Llegó un mensaje del cliente que nadie contestó todavía (o se reabrió a mano) |
| `esperando_cliente` | Un agente contestó (mensaje saliente aceptado); el SLA se detiene |
| `resuelta` | `POST …/resolver`. Si el cliente vuelve a escribir, pasa a `abierta` con `reabiertaEn` (evento `conversacion.reabierta`); conserva la asignación si el agente sigue activo y en la cola, si no queda sin asignar |

Las notas internas y los mensajes de sistema no cambian el estado.

### 3.3 Mensajes

```json
{ "id": "0193…", "conversacionId": 41, "seq": 18,
  "direccion": "entrante",            // entrante | saliente | nota | sistema
  "tipo": "texto",                    // texto | imagen | documento | audio | video | ubicacion | plantilla | contacto | sistema
  "texto": "Ya lo reinicié y sigue igual.",   // texto plano (nunca HTML); null si no tiene
  "adjunto": { "id": "…", "estado": "completo", "mime": "image/jpeg", "tamano": 1258291,
               "nombre": "modem.jpg", "urlDescarga": "/api/v1/adjuntos/…" },   // o ausente
  "ubicacion": { "lat": 14.6, "lon": -90.5 },  // o ausente
  "plantilla": { "codigo": "tecnico_en_camino", "variables": { … } },          // saliente con plantilla
  "autor": { "id": 7, "nombre": "Andrea" },    // saliente y nota; null en entrante y sistema
  "estadoEntrega": "leido",           // saliente: en_cola | enviado | entregado | leido | fallido
  "motivoFalla": null,                // con fallido: numero_invalido | rechazado_por_canal | fuera_de_ventana | agotado | sin_permiso
  "menciones": [{ "id": 9, "nombre": "Laura Méndez" }],   // nota
  "evento": { "tipo": "ticket_creado", "referencia": "TK-004812", "entidadId": 4812, "actorNombre": "Andrea" }, // sistema
  "creadoEn": "…", "enviadoEn": "…", "entregadoEn": "…", "leidoEn": "…" }
```

Eventos de sistema (`evento.tipo`): `conversacion_creada`, `asignada` (`actorNombre`,
`asignadoNombre`), `tomada`, `transferida` (`colaNombre`, `resumen`), `resuelta`, `reabierta`,
`etiquetada`, `cliente_asociado`, `contacto_iris_creado`, `ticket_creado`, `orden_creada`,
`prospecto_creado`, `boleta_registrada`, `boleta_aprobada`, `boleta_rechazada`, `promesa_creada`,
`respondio_aviso` (`plantilla`), `fuera_de_horario` (PR B), `guardia_avisada` (PR B),
`manual_enviado` (PR B). El panel arma el texto («Andrea creó TK-004812»).

### 3.4 Mensajes del cliente (webhook de Meta)

El webhook de Meta que ya existe (`POST /canales/{id}/webhook`) guarda el cuerpo en
`mensajeria.entrantes` (deduplicado) y lo procesa en un job. Por cada mensaje del número del canal:

- conversación de `(empresa del canal, canal, from)`: si no existe se crea en la cola del canal y,
  si el número coincide con **un solo** cliente de la instantánea de Argos de la empresa
  (`telefono`, `movil` o `contacto_telefono` del servicio; solo dígitos, `502` delante de 8
  dígitos), se asocia sola (`asociacion: automatica`);
- deduplicado por el id de Meta (`wamid`): el mismo webhook o el mismo mensaje dos veces no
  duplica nada;
- texto, ubicación, botón/lista (su texto), contacto (nombre) y medios: imagen, documento, audio,
  video o sticker. El medio se descarga de Meta en otro job y queda en el módulo de adjuntos
  (entidad `conversacion`); mientras tanto el mensaje trae `adjunto.estado = pendiente`. Si Meta no
  lo entrega en 5 intentos queda `rechazado`;
- `BAJA` / `ALTO` se procesan **antes** que el mensaje: registran la baja del número en la
  empresa (`mensajeria.consentimientos_numero`, origen `cliente`; vale también para contactos de
  Iris), dan de baja los avisos (§6.4) y luego quedan como mensaje (sin respuesta automática);
- si responde (`context.id`) a un aviso de ese número, o recibió un aviso de ese número en las
  últimas 72 h, se agrega el evento `respondio_aviso`;
- `perfilNombre` se toma de `contacts[].profile.name`.

Los estados de entrega (`sent`, `delivered`, `read`, `failed`) de los mensajes salientes actualizan
`estadoEntrega` (nunca retroceden); los de los avisos siguen igual que antes.

## 4. Quién ve qué (permisos y alcance)

| Permiso | Qué permite |
| --- | --- |
| `conversaciones.ver` | Ver las conversaciones de **sus colas** (miembro o supervisor de la cola, §14.1), las **sin cola mientras nadie las tiene** (§14.2), las asignadas a él y las que lo mencionan; sus mensajes, contadores, cambios y el flujo SSE |
| `conversaciones.supervisar` | Ver todas las de la empresa (todas las colas) |
| `conversaciones.iniciar` | Escribir primero a un cliente o contacto con permiso, con una plantilla aprobada (§14.3). **Sensible** |
| `conversaciones.pasar` | «Pasar a…» la conversación que tienes a un compañero de la cola (§14.4) |
| `conversaciones.responder` | Enviar mensajes y plantillas, notas internas con menciones, marcar leído, usar respuestas rápidas |
| `conversaciones.tomar` | Asignarse una conversación sin asignar que ve |
| `conversaciones.asignar` | Asignar a otro agente o quitar la asignación |
| `conversaciones.transferir` | Pasar a otra cola con un resumen |
| `conversaciones.etiquetar` | Poner y quitar etiquetas |
| `conversaciones.resolver` | Resolver y reabrir |
| `conversaciones.asociar` | Asociar a un cliente de Argos, crear y unir contactos de Iris |
| `conversaciones.registrar_boleta` | Mandar a Argos la boleta que envió el cliente (§6.4) |
| `conversaciones.registrar_promesa` | Simular y pedir a Argos una promesa de pago (§6.5) |
| `conversaciones.configurar` | Etiquetas y respuestas rápidas de la empresa |
| `canales.configurar` | Colas, uso, cola y horario de cada número; horarios y feriados (PR B) |
| `manuales.ver` / `manuales.editar` / `manuales.revocar_enlace` | Biblioteca de manuales; revocar un enlace público enviado (PR B) |

Crear ticket, orden o prospecto desde la conversación exige además el permiso del caso que se
reutiliza (`tickets.crear`, `ordenes.crear`, `prospectos.crear`).

Lo que no se ve responde **404**. Roles base:

| Rol | Permisos |
| --- | --- |
| `soporte` | ver, responder, tomar, pasar, iniciar, transferir, etiquetar, resolver, asociar, registrar_boleta, registrar_promesa, manuales.ver |
| `ventas` | ver, responder, tomar, pasar, iniciar, transferir, etiquetar, resolver, asociar, manuales.ver |
| `despacho` | ver, responder, tomar, pasar, etiquetar, resolver, manuales.ver |
| `supervisor` | todos los de arriba + supervisar, asignar, configurar, canales.configurar, manuales.editar, manuales.revocar_enlace |
| `lectura` | ver, manuales.ver |
| `admin` | todos |

Los usuarios de integración no reciben ninguno en esta fase, y los tokens de API no entran a la
bandeja (404 aunque el usuario tenga los permisos). Son **sensibles** (asignarlos pide
re-autenticación): `conversaciones.supervisar`, `conversaciones.registrar_boleta`,
`conversaciones.registrar_promesa`, `conversaciones.configurar` y (fase 1.1) `conversaciones.iniciar`;
por eso asignar los roles `soporte`, `ventas` o `supervisor` también la pide, y en una sesión abierta
por el autologin desde Argos iniciar una conversación pide `POST /api/v1/sesion/reautenticar`.

## 5. Contactos

### 5.1 Cliente de Argos

- `GET /conversaciones/{id}/coincidencias`: clientes de Argos cuyo teléfono coincide exactamente
  con el número (§7.6 del contrato con Argos, `GET /clientes?telefono=`; si Argos no responde,
  las de la instantánea local con `origen: "instantanea"`), cada uno con `coincideEn[]`
  (`telefono`, `movil`, `contacto_servicio`) y sus servicios en la empresa; más los contactos de
  Iris con ese número. `contacto_servicio` no es el titular: el panel lo muestra como «contacto de
  un servicio».
- `POST /conversaciones/{id}/asociar-cliente` `{ version, clienteId, servicioId? }`
  (`conversaciones.asociar`): Iris asegura la instantánea del cliente (y valida que sea de la
  empresa y que el servicio sea suyo; si no, 422 `cliente_fuera_de_empresa` /
  `servicio_fuera_de_empresa`). Solo guarda el id. Evento `conversacion.cliente_asociado`.
  Queda `verificado` si el número coincide con un teléfono del cliente en la instantánea; si no,
  queda sin verificar.
- `POST /conversaciones/{id}/confirmar-cliente` `{ version }` (`conversaciones.supervisar`):
  confirma una asociación manual sin coincidencia (`verificado: true`, evento de sistema
  `cliente_confirmado`, outbox `conversacion.cliente_confirmado`). 422 `sin_cliente` sin cliente.
- `POST /conversaciones/{id}/desasociar` `{ version }`: quita cliente, contacto de Iris, prospecto y la verificación.

### 5.2 Contacto de Iris

Para quien no es cliente (proveedor, personal, otro) o aún no lo es. Es un dato **propio de
Iris**, no una copia de Argos: `nombre`, `tipo` (`proveedor`, `personal`, `prospecto`, `otro`),
`telefono` (E.164), `nota`, `clienteId` (null hasta unirlo), `version`.

| Método y ruta | Permiso | Qué hace |
| --- | --- | --- |
| `GET /contactos-iris?q=` | `conversaciones.ver` | Buscar por nombre o número |
| `POST /conversaciones/{id}/contacto-iris` | `conversaciones.asociar` | Crea el contacto con el número de la conversación y lo asocia `{ version, nombre, tipo, nota? }` |
| `POST /contactos-iris/{id}/actualizar` | `conversaciones.asociar` | Nombre, tipo, nota (`version`) |
| `POST /contactos-iris/{id}/unir-cliente` | `conversaciones.asociar` | `{ version, clienteId }`: se volvió cliente. Sus conversaciones pasan a `contacto.tipo = cliente` con ese id (evento `contacto_iris.unido`), verificadas solo si el número coincide en la instantánea. El contacto queda como historial (`clienteId`, `unidoEn`) |
| `POST /contactos-iris/{id}/consentimiento` | `conversaciones.asociar` (baja); `acepta` exige además `conversaciones.supervisar` (403) | `{ estado: "acepta" \| "baja", motivo? }` → `{ telefono, estado, ocurridoEn }`: permiso de contacto por WhatsApp de ese número en la empresa (origen `agente`; evento `contacto_iris.consentimiento`). Con `acepta` vigente se le pueden mandar plantillas fuera de la ventana (§8). La baja por chat (§3.4) también cuenta, y si hay una baja del propio cliente sin un `acepta` posterior (aunque después haya bajas de agentes), `acepta` exige `motivo` (5–300 letras, queda guardado y auditado; si no, 422 `baja_del_cliente`). La baja por chat cuenta desde que Iris recibe el mensaje (un webhook tardío no queda detrás de un `acepta`) |

### 5.3 Prospecto

`POST /conversaciones/{id}/crear-prospecto` (`prospectos.crear`): la entrada es la de
`POST /api/v1/prospectos` (teléfono por defecto el de la conversación, origen `whatsapp`) y reutiliza
el caso de comercial en la misma transacción. Asocia el prospecto (`contacto.tipo = prospecto`).

## 6. Acciones desde la conversación

Todas reutilizan el caso de uso del módulo dueño **en la misma transacción** (un savepoint) y
agregan un mensaje de sistema y un vínculo. Exigen ver la conversación y el permiso del caso. La
`Idempotency-Key` se guarda con la respuesta (middleware).

| Método y ruta | Permiso | Reutiliza | Evento de sistema |
| --- | --- | --- | --- |
| `POST /conversaciones/{id}/crear-ticket` | `tickets.crear` | `POST /tickets` (cliente y servicio de la conversación por defecto; `descripcion` opcional) | `ticket_creado` |
| `POST /conversaciones/{id}/crear-orden` | `ordenes.crear` | `POST /ordenes` (cliente, servicio y ticket ligado por defecto) | `orden_creada` |
| `POST /conversaciones/{id}/crear-prospecto` | `prospectos.crear` | `POST /prospectos` | `prospecto_creado` |
| `POST /conversaciones/{id}/boletas` | `conversaciones.registrar_boleta` | §6.4 | `boleta_registrada` |
| `POST /conversaciones/{id}/promesas/simular` | `conversaciones.registrar_promesa` | `POST /promesas/simular` de Argos (lectura, sin auditar) | — |
| `POST /conversaciones/{id}/promesas` | `conversaciones.registrar_promesa` | §6.5 | `promesa_creada` |

### 6.4 Boleta (contrato con Argos §7.2)

`{ adjuntoId, servicioId?, metodo, cuentaBancariaId, bancoOrigenId?, referencia, monto,
fechaDeposito, nota? }`. Para el formulario: `GET /conversaciones/cuentas-bancarias` →
`{ data: [{ id, nombre, banco, numero, moneda }] }` (las activas en GTQ de la empresa activa) y
`GET /conversaciones/bancos` → `{ data: [{ id, nombre }] }`, las dos con
`conversaciones.registrar_boleta`, leídas de Argos en el momento (no se guardan; Argos caído: 503).
El adjunto se elige de los mensajes de la conversación (`MensajeConversacion.adjunto.id`); no hay
lista aparte. `adjuntoId` es una imagen o PDF de **esta** conversación (la que mandó el
cliente o una que subió el agente), y sus **bytes** deben ser JPEG, PNG, WebP o PDF (se revisan
con la firma del archivo, no con el tipo que declara Meta o el navegador; si no, 422
`adjunto_no_listo`). Exige cliente asociado (422 `sin_cliente`) y verificado (422
`cliente_sin_confirmar`). Iris guarda la
boleta (`mensajeria.boletas`, uuid) y la manda a Argos en un job (`POST /comprobantes` multipart,
`Idempotency-Key = iris:boleta:<id>`, `X-Iris-Actor: usuario:<id>`, `X-Iris-Conversacion:
<conversacion id>`, `canal_contacto = whatsapp`, `leido_por = persona`), con reintentos a 1 min,
5 min, 30 min, 2 h y 12 h. Estados: `por_enviar` → `registrada` (Argos la aceptó, con
`comprobanteId`) | `fallida` (rechazo de negocio: `referencia_duplicada`, validación…, con el
`detalle` de Argos). La aprobación o el rechazo de caja (webhooks `comprobante.*`) se reflejan en
la conversación cuando lleguen (ver «Pendiente» en §12).

### 6.5 Promesa (contrato con Argos §7.4)

Simular y pedir exigen cliente verificado (422 `cliente_sin_confirmar`) y que `servicioId` sea del
cliente en la empresa (como al asociar: 422 `servicio_fuera_de_empresa`).
`{ servicioId, venceEl: "AAAA-MM-DD" }` (día prometido; Iris manda a Argos el final de ese día en
hora de Guatemala). Sincrónica: Iris llama a Argos fuera de su transacción
(`Idempotency-Key = iris:promesa:<id>`) y guarda el resultado: `activa`, `rechazada` (con `regla` y
`motivo`) o `pendiente_aprobacion`. Argos caído: 503 `argos_no_disponible` y no queda nada a medias
(repetir con la misma `Idempotency-Key`). 409 `promesa_abierta` de Argos pasa tal cual.

## 7. Bandeja

### 7.1 Lista

`GET /conversaciones` (`conversaciones.ver`):

| Parámetro | Valores |
| --- | --- |
| `vista` | `mias` (asignadas a mí), `sin_asignar`, `todas` (por defecto; todas las que veo) |
| `canalId`, `colaId`, `etiquetaId` | filtros (uno de cada) |
| `estado` | `abierta`, `esperando_cliente`, `resuelta`, `activas` (las dos primeras; por defecto) |
| `sla` | `por_vencer`, `vencido` |
| `q` | nombre (cliente de la instantánea, contacto de Iris, perfil de WhatsApp), dígitos del número, o referencia de un vínculo (`TK-004812`) |
| `cursor`, `limit` | paginación (más reciente primero por `ultimaActividad`) |

### 7.2 Contadores

`GET /conversaciones/contadores` → `{ mias, sinAsignar, todas, noLeidosMias, porVencer,
vencidas, porCanal[{ id, total }], porCola[{ id, total }], porEtiqueta[{ id, total }],
porEstado{ abierta, esperando_cliente } }`, sobre las activas que ve el usuario. `noLeidosMias` es
el número de mensajes del cliente sin leer en las asignadas a él.

### 7.2.1 Catálogos para la bandeja (`conversaciones.ver`)

- `GET /conversaciones/canales` → `{ data: [{ id, nombre, numero, usos[], avisosEmpresa, colaId,
  activo }] }`: los números de la empresa sin credenciales ni ids de Meta (para el filtro «Número»
  y el color de cada uno). `contadores.porCanal[]` trae solo `{ id, total }`: se cruza con esta
  lista.
- `GET /conversaciones/agentes?colaId=` → `{ data: [{ id, nombre, supervisa, colas[{ id, nombre }]
  }] }`: usuarios persona activos de la empresa con `conversaciones.ver` (con `colaId`, los miembros
  de esa cola más los que supervisan). Sirve para asignar, mencionar y «pedir ayuda al supervisor».
  **Quién supervisa** es quien tiene `conversaciones.supervisar` (rol supervisor o admin): ve y
  supervisa todas las colas de la empresa. Desde la fase 1.1 una cola tiene además sus
  **supervisores** (§14.1): con `colaId`, la lista trae miembros, supervisores de la cola y
  supervisores globales. Cada agente trae `colasSupervisadas[]` y `disponibilidad` (§14.5).
- `GET /conversaciones/{id}/plantillas` (`conversaciones.responder`) → `{ data: [{ codigo, nombre,
  variables[{ nombre, formato, obligatoria }], vistaPrevia }] }`: las plantillas aprobadas en el
  número de esa conversación (`nombre` = la de Meta; `vistaPrevia` = el texto de referencia con
  `{{variable}}`). Fuera de la ventana de 24 h es lo único que se puede mandar.
- `GET /colas`, `GET /etiquetas` (§2) y `GET /respuestas-rapidas` (`conversaciones.responder`).

**Adjuntos del agente:** `POST /api/v1/adjuntos` acepta `entidad: "conversacion"`, `entidadId` = id
de la conversación, para quien tiene `adjuntos.subir` y `conversaciones.responder` y **ve** la
conversación (si no, 404). Ver o descargar exige `adjuntos.ver` y `conversaciones.ver`. Los roles
soporte, ventas, despacho y supervisor tienen `adjuntos.subir`. Los medios que manda el cliente
también quedan con esa entidad (los registra el sistema).

### 7.3 Acciones de la bandeja

Todas exigen ver la conversación (si no, 404) y llevan `version` (409 `conflicto_version`). Dos
agentes que toman la misma a la vez: gana uno; el otro recibe 409 `ya_asignada` (el `detail` dice
quién la tiene; el panel recarga la conversación).

| Método y ruta | Permiso | Cuerpo | Notas |
| --- | --- | --- | --- |
| `POST /conversaciones/{id}/tomar` | `tomar` | `{ version }` | Solo si está sin asignar (409 `ya_asignada`) y eres miembro de su cola, supervisas o no tiene cola (si no, 422 `usuario_no_disponible`, igual que asignar). Tomar la propia: 200 sin cambios |
| `POST /conversaciones/{id}/asignar` | `asignar` | `{ version, usuarioId \| null }` | El agente debe tener `conversaciones.ver`, ser de la empresa y miembro de la cola (o supervisar); si no, 422 `usuario_no_disponible`. Le llega `asignada` por el flujo |
| `POST /conversaciones/{id}/transferir` | `transferir` | `{ version, colaId, resumen, conservarAsignacion? }` | `resumen` obligatorio (1–1000), queda como nota interna. Por defecto queda sin asignar |
| `POST /conversaciones/{id}/etiquetar` | `etiquetar` | `{ version, etiquetaIds[] }` | Fija el conjunto (máx. 10; solo etiquetas activas de la empresa) |
| `POST /conversaciones/{id}/resolver` | `resolver` | `{ version, nota? }` | |
| `POST /conversaciones/{id}/reabrir` | `resolver` | `{ version }` | Solo desde `resuelta` |
| `POST /conversaciones/{id}/leer` | `ver` | `{ hastaMensajeId }` | Avanza tu lectura (nunca retrocede) |

## 8. Enviar y anotar

`POST /conversaciones/{id}/mensajes` (`conversaciones.responder`):

```json
{ "id": "<uuid v7 del cliente>",
  "texto": "Te enviamos un técnico hoy entre 13:30 y 15:00.",   // o
  "plantilla": { "codigo": "tecnico_en_camino", "variables": { … } },  // o
  "adjuntoId": "<uuid>", "texto": "pie de foto opcional",
  "respuestaRapidaId": 4 }
```

- Idempotente por `id` (mismo id y contenido → 200 con el mensaje actual, mismo cuerpo que el 201; otro contenido → 409
  `mensaje_id_reutilizado`). 201 al crearlo. La respuesta no se guarda en el middleware (trae
  texto); la `Idempotency-Key` la aplica el caso.
- **Ventana de 24 h:** texto y adjuntos solo si el cliente escribió en las últimas 24 h; fuera de
  ella, 422 `fuera_de_ventana` y solo se puede mandar una **plantilla aprobada en ese número**
  (`mensajeria.canales.plantillas`; si no, 422 `plantilla_no_aprobada`).
- **Baja por número:** una plantilla nunca sale a un número con baja vigente en
  `consentimientos_numero` (BAJA por chat o registrada a mano), haya o no cliente asociado, dentro
  o fuera de la ventana: 422 `baja`.
- **Permiso de contacto:** una plantilla fuera de ventana exige (a) cliente de Argos asociado y
  **verificado** (§3.1; si no, 422 `cliente_sin_confirmar`) con permiso vigente `acepta` en
  WhatsApp (§6.4 del contrato con Argos), o (b) contacto de Iris con permiso `acepta` registrado
  por número (§5.2). Si no, 422 `sin_permiso` o `baja`. Dentro de la ventana el cliente inició la
  conversación y no hace falta (salvo baja del número).
- Un mensaje con `baja` vigente nunca sale como plantilla. Un número pausado por calidad (§6.5)
  retiene el envío hasta que se libera; al salir se revisa otra vez el permiso y, si ya no lo
  hay, queda `fallido` con motivo `baja`, `sin_permiso` o `cliente_sin_confirmar`.
- **Responder asigna** (decisión del líder, pendiente del PM): si nadie tiene la conversación y
  quien responde puede atenderla (miembro de la cola, supervisa o sin cola), queda asignada a él
  (evento `conversacion.asignada` con `origen: responder`).
- Sale por un job (`en_cola` → `enviado` → `entregado` → `leido` | `fallido`). Fallas temporales se
  reintentan (8 veces) y al final `agotado`.
- Adjuntos: imagen, documento, audio o video del módulo de adjuntos, entidad `conversacion`, ya
  `completo` (si no, 422 `adjunto_no_listo`); hasta 16 MB (documento 100 MB según Meta).
- Texto plano ≤ 4096 (límite de WhatsApp). Pasar a `esperando_cliente` y detener el SLA ocurre al
  crearlo.

`POST /conversaciones/{id}/notas` (`conversaciones.responder`) `{ id, texto, menciones[] }`: nota
interna (nunca sale a WhatsApp). Cada mencionado (usuario activo de la empresa con
`conversaciones.ver`; si no, 422 `usuario_no_disponible`) pasa a ser **participante** (ve la
conversación aunque no sea de su cola), recibe el evento `mencion` por el flujo y el evento
`conversacion.mencion` sale por el outbox. «Pedir ayuda al supervisor» es una nota que lo menciona:
la conversación sigue con quien la tiene.

`GET /conversaciones/{id}/mensajes?direccion=anteriores|siguientes&cursor=&limit=` igual que el
chat (`seq` ascendente).

## 9. Tiempo real

Mismo patrón que el chat interno (`contratos/chat.md` §4): feed `mensajeria.conv_eventos`,
`NOTIFY iris_conversaciones '<empresa>'`, `GET /conversaciones/cambios?cursor=` (sondeo) y
`GET /conversaciones/eventos` (SSE con `Last-Event-ID`, latido 25 s, cierre a 30 min, 5 flujos por
usuario, revalidación de la sesión o del token en cada latido, 410 `cursor_vencido` tras 7 días).
Tipos: `mensaje_nuevo` (con el mensaje), `mensaje_actualizado` (estado de entrega o adjunto listo),
`conversacion_actualizada` (con la conversación como la ve el usuario), `mencion` (solo al
mencionado). Cada usuario recibe solo lo de las conversaciones que ve.

## 10. Horarios, feriados y fuera de horario (PR B)

### 10.1 Horario (entidad genérica)

`mensajeria.horarios` es reutilizable: se asigna a un **número**, a una **cola** y, en la fase 2, a
un **asistente**. No está pegado a ningún canal.

```json
{ "id": 1, "nombre": "Soporte · extendido", "zona": "America/Guatemala", "version": 3,
  "franjas": [ { "dias": [1,2,3,4,5], "inicio": "06:00", "fin": "22:00", "perfil": null },
               { "dias": [6], "inicio": "08:00", "fin": "12:00", "perfil": null } ],
  "feriados": [ { "fecha": "2026-12-25", "nombre": "Navidad", "franjas": [] } ] }
```

- `dias` 1 = lunes … 7 = domingo. `inicio` < `fin` en la misma fecha local (una franja que cruza la
  medianoche se escribe como dos). En la base todo se calcula en UTC; la zona solo convierte.
- Un feriado sin `franjas` cierra todo el día; con `franjas`, reemplaza las del día.
- `perfil` (texto, opcional) es el **punto de extensión de la fase 2**: el conjunto de permisos que
  tendrá un asistente en esa franja (p. ej. `solo_sugerir` de día; `responder_ticket_agenda` de
  noche, sin promesas ni reconexiones). En la fase 1 se guarda y se devuelve, pero nada lo usa.
- `GET /horarios`, `GET /horarios/{id}`, `POST /horarios`, `POST /horarios/{id}/actualizar`
  (`canales.configurar`; los de ver, `conversaciones.ver`). `GET /horarios/{id}/estado?en=` →
  `{ abierto, franja, proximoCambio }`.
- Se asigna con `horarioId` en `POST /canales/{id}/configurar` y en la cola
  (`POST /colas/{id}/actualizar`). Vale el de la cola si lo tiene; si no, el del número; sin
  ninguno, siempre abierto.

### 10.2 Fuera de horario

`fueraDeHorario` del número:

```json
{ "responder": true, "mensaje": "Gracias por escribir. Atendemos de 6:00 a 22:00; te respondemos al abrir.",
  "guardia": { "activa": true, "usuarioId": 9, "etiquetaIds": [5], "fallaMasiva": true },
  "asistente": { "activo": false } }
```

- **Respuesta automática:** al llegar un mensaje del cliente fuera de horario, Iris contesta
  `mensaje` **una sola vez por ventana cerrada** (hasta que vuelva a abrir el horario): nunca dos
  veces en la misma noche aunque el cliente escriba diez veces. Va como texto libre (el cliente
  acaba de escribir: está dentro de la ventana de 24 h). No sale si el cliente tiene `baja`
  vigente. Queda como mensaje saliente con `autor: null` y evento `fuera_de_horario`.
- **Guardia:** si `guardia.activa` y la conversación tiene una de `etiquetaIds`, o hay una **falla
  masiva** en la zona del servicio del cliente (un `horus.router_estado_cambiado` `caido` abierto en
  el router del servicio, de la instantánea), Iris avisa al usuario de guardia: lo agrega como
  participante, le llega el evento `guardia` por el flujo y sale `conversacion.guardia_avisada` por
  el outbox (para un futuro push). Una vez por conversación y ventana cerrada.
- `asistente.activo` está **reservado**: en la fase 1 solo admite `false` (422
  `disponible_en_fase_2`).
- `guardia` y `asistente` son opcionales en el cuerpo. `POST /canales/{id}/configurar` sin
  `fueraDeHorario` lo deja como estaba; `horarioId` ausente quita el horario del número. Se lee en
  `GET /conversaciones/canales` (`horarioId`, `fueraDeHorario`) y en `GET /canales`.
- La respuesta automática queda como mensaje saliente sin `autor` y no cambia el estado de la
  conversación (sigue `abierta` esperando a una persona) ni el SLA.

### 10.3 SLA y horario

El SLA de la cola empieza a contar al abrir el horario de la cola (o del número) si el mensaje llegó
cerrado: un mensaje de las 22:30 con SLA de 60 min y apertura a las 06:00 vence a las 07:00. Una vez
que empezó, no se pausa si el horario cierra antes de vencer (fase 1).

### 10.4 Puntos de extensión para la fase 2 (asistentes)

- `horarios.franjas[].perfil` → conjunto de permisos del asistente en esa franja.
- `fueraDeHorario.asistente` → que atienda un asistente fuera de horario.
- Manuales con `visibilidad` y la búsqueda por texto (§11) son la base de conocimiento del asistente.
- Los mensajes ya distinguen `autor` (persona) de `autor: null` (sistema); el asistente será un
  autor más (`asistente_ia`), como `X-Iris-Actor` del contrato con Argos.

## 11. Biblioteca de manuales (PR B)

`mensajeria.manuales` por empresa: `titulo`, `cuerpo` (Markdown seguro: se guarda tal cual y el
panel lo muestra sin HTML crudo), `resumen`, `etiquetas[]` (texto libre, en minúsculas), `equipo`
(modelo opcional, p. ej. `ZTE F670L`), `visibilidad` (`interna` | `cliente`), `activo`, `version`.

| Método y ruta | Permiso | Qué hace |
| --- | --- | --- |
| `GET /manuales?q=&etiqueta=&visibilidad=` | `manuales.ver` | Búsqueda de texto completo en español (Postgres `websearch_to_tsquery('spanish', q)`) por título, resumen, cuerpo, etiquetas y equipo, ordenada por relevancia |
| `GET /manuales/{id}` | `manuales.ver` | El artículo |
| `POST /manuales`, `/manuales/{id}/actualizar`, `/activar`, `/desactivar` | `manuales.editar` | |
| `GET /conversaciones/{id}/manuales-sugeridos` | `manuales.ver` | Sugerencias por reglas, cada una con su `motivo`: `etiqueta` (comparte una etiqueta con la conversación, por nombre), `equipo` (el equipo del servicio coincide), `ticket_abierto` (comparte una palabra con el asunto de un ticket abierto del cliente). Hasta 5 |
| `POST /conversaciones/{id}/enviar-manual` | `conversaciones.responder` | `{ id, manualId, modo: "enlace" \| "texto" }`. Solo artículos `cliente` (si no, 422 `manual_interno`). `enlace`: URL pública firmada (`/api/v1/mensajeria/publico/manuales/{token}`, vence a los 30 días) en un mensaje; `texto`: el cuerpo en texto plano. Respeta la ventana de 24 h. Idempotente por `id`: 201 al crearlo, 200 con el mismo cuerpo si se repite |

| `GET /conversaciones/{id}/manual-enlaces` | `conversaciones.ver` | `{ data: [{ id, manualId, titulo, enviadoPor?, venceEn, revocadoEn, creadoEn }] }`: enlaces enviados en la conversación (nunca el token) |
| `POST /manual-enlaces/{id}/revocar` | `manuales.revocar_enlace` (supervisor) | Revoca ese enlace (desde ya responde 410). Solo de conversaciones que ves (si no, 404). Revocar uno ya revocado: 200 sin cambios. Evento `manual.enlace_revocado` |

`GET /api/v1/mensajeria/publico/manuales/{token}` (público): el artículo para el cliente en HTML
mínimo y seguro, o 410 si el enlace venció, se revocó o el artículo se desactivó.

**Operación:** el token va en la ruta. Excluye `/api/v1/mensajeria/publico/manuales/*` de los
access logs del proxy (o enmascara el último segmento) y de cualquier herramienta que guarde URLs
(APM, analítica). La API no registra rutas; Iris guarda solo el SHA-256 del token.

## 12. Eventos (outbox)

Detalle en `contratos/eventos.md` («Conversaciones con clientes»). Ninguno lleva el texto de un
mensaje.

`conversacion.creada`, `conversacion.mensaje_recibido`, `conversacion.mensaje_enviado`,
`conversacion.mensaje_fallido`, `conversacion.nota_agregada`, `conversacion.mencion`,
`conversacion.asignada`, `conversacion.transferida`, `conversacion.etiquetada`,
`conversacion.resuelta`, `conversacion.reabierta`, `conversacion.cliente_asociado`,
`conversacion.vinculo_creado`, `conversacion.sla_vencido`, `conversacion.cliente_confirmado`,
`contacto_iris.creado`, `contacto_iris.unido`, `contacto_iris.consentimiento`, `cola.creada` / `cola.actualizada`, `etiqueta.creada` / `.actualizada`,
`respuesta_rapida.creada` / `.actualizada`, `boleta.registrada`, `boleta.registrada_en_argos`,
`boleta.fallida`, `promesa.solicitada`; PR B: `horario.creado` / `.actualizado`, `conversacion.fuera_de_horario`,
`conversacion.guardia_avisada`, `manual.creado` / `.actualizado` / `.enviado` / `.enlace_revocado`.
Números (§1.1): `canal_mensajeria.probado`.

**Pendiente (fase 1.1, decisión abierta):** consumir los webhooks de Argos `comprobante.aprobado` /
`.rechazado` y `promesa.*` para mover la boleta y la promesa en la conversación y avisar al cliente
con `boleta_confirmada` / `boleta_rechazada` / `boleta_falta_dato`; reconexión temporal (§7.3);
verificación de identidad (§7.5); autologin a una conversación (pedido B2).

## 13. Errores propios

| Código | Status | Cuándo |
| --- | --- | --- |
| `fuera_de_ventana` | 422 | Texto o adjunto con la ventana de 24 h cerrada |
| `plantilla_no_aprobada` | 422 | La plantilla no está aprobada en ese número |
| `sin_permiso` / `baja` | 422 | Plantilla fuera de ventana sin permiso de contacto vigente |
| `ya_asignada` | 409 | Tomar una conversación que otro ya tiene |
| `conflicto_version` | 409 | `version` vieja |
| `mensaje_id_reutilizado` | 409 | Mismo id de mensaje con otro contenido |
| `usuario_no_disponible` | 422 | Asignar o mencionar a quien no puede ver conversaciones |
| `cola_inactiva` | 422 | Transferir a una cola desactivada |
| `adjunto_no_listo` | 422 | El adjunto no está completo o no es de la conversación |
| `sin_cliente` | 422 | Boleta o promesa sin cliente asociado |
| `baja_del_cliente` | 422 | `acepta` sin `motivo` sobre una baja que pidió el propio cliente |
| `cliente_cambiado` | 409 | La conversación cambió de cliente o perdió la verificación mientras Argos creaba la promesa (revisar en Argos) |
| `cliente_sin_confirmar` | 422 | Asociación manual sin coincidencia de número, sin confirmar por un supervisor: plantilla fuera de ventana, boleta o promesa |
| `numero_avisos_requiere_uso` | 422 | `avisosEmpresa` sin `avisos` en `usos` |
| `manual_interno` | 422 | Enviar al cliente un manual interno (PR B) |
| `disponible_en_fase_2` | 422 | Activar el asistente (PR B) |
| `cursor_vencido` | 410 | Feed purgado |
| `no_disponible_aun` | 422 | Fase 1.1: `bot.atiende = agente_autonomo` o un asistente de IA como supervisor de cola (fase 3) |
| `conversacion_de_otro` | 409 | Fase 1.1: iniciar con un número cuya conversación activa tiene otro agente (el `detail` dice quién) |
| `conversacion_de_otra_cola` | 409 | Fase 1.1: iniciar con un número cuya conversación no ves o es de una cola que no atiendes |
| `sin_contacto` | 422 | Fase 1.1: el cliente no tiene teléfono en la instantánea de Argos (ni `movil` ni `telefono`) |
| `numero_no_permitido` | 422 | Fase 1.1: iniciar desde un número que no es de tus colas (o inactivo) |
| `no_es_tuya` | 403 | Fase 1.1: pasar una conversación que no tienes asignada sin ser supervisor (global o de su cola) |
| `mismo_agente` | 422 | Fase 1.1: pasar la conversación a quien ya la tiene |
| `asociacion_requiere_permiso` | 403 | Fase 1.1: iniciar sobre una conversación asociada a otro contacto sin `conversaciones.asociar` |

## 14. Fase 1.1 (aprobada por el PM)

Supervisores por cola, visibilidad de las conversaciones sin cola, iniciar una conversación,
«Pasar a…», disponibilidad de los agentes, «¿Alguien la atiende?» y el bot «Cuando no hay
agentes». Todo con la misma base, auth (persona con sesión del panel; tokens e integraciones
reciben 404), empresa activa, errores RFC 9457, `Idempotency-Key` y `version` que el resto del
contrato. Los objetos opcionales nuevos van **ausentes** (no `null`) y las listas nuevas son `[]`.

### 14.1 Supervisores por cola

Una cola tiene **miembros** (reciben y atienden sus conversaciones) y **supervisores** (una o
varias personas). Un supervisor de cola **ve, asigna y pasa todo lo de esa cola** sin ser miembro y
sin el permiso global `conversaciones.supervisar`; puede tomarlas y recibirlas como un miembro. No
ve otras colas ni confirma clientes (eso sigue siendo `conversaciones.supervisar`).

- `ColaConversaciones` suma: `supervisores[]` (`UsuarioRef`), `disponibles[]` (ids de los miembros y
  supervisores de la cola que están `disponible` ahora, §14.5), `botSinConfirmacionMin` y
  `botSinDisponiblesMin` (enteros o `null` = vale el del número, §14.6).
- `POST /colas/{id}/supervisores` (`canales.configurar`) `{ version, usuarioIds[], asistenteIa? }`:
  fija la lista (máx. 20; personas activas de la empresa con `conversaciones.ver`; si no, 422
  `usuario_no_disponible`). `asistenteIa: true` responde 422 `no_disponible_aun` (fase 3: el modelo
  ya guarda el tipo de supervisor, `persona` o `asistente`). Evento `cola.actualizada` con
  `campos: ["supervisores"]`.
- `POST /colas` y `POST /colas/{id}/actualizar` aceptan `botSinConfirmacionMin` y
  `botSinDisponiblesMin` (1–1440; al actualizar, ausente = `null`, igual que `horarioId`).
- `GET /conversaciones/agentes?colaId=`: miembros, supervisores de la cola y supervisores globales.
  Cada `AgenteConversaciones` suma:
  - `colasSupervisadas[]` (`ColaRef`);
  - `abiertas` (entero): sus conversaciones activas (no resueltas) asignadas en la empresa activa;
  - `disponibilidad`: `{ estado: "disponible" | "no_disponible", origen: "manual" | "cierre_sesion" |
    "inactividad" | "horario", desde: fecha-hora | null }` (§14.5). Siempre presente: sin fila,
    `{ estado: "no_disponible", origen: "manual", desde: null }`.
- `POST /conversaciones/{id}/asignar` lo puede usar quien tiene `conversaciones.asignar` **o** es
  supervisor de la cola de la conversación (si no, 403). El asignado puede ser miembro o supervisor
  de la cola (o supervisor global).
- Detalle de la conversación: `supervisores[]` (de su cola) y `equipo[]`
  `{ id, nombre, rol: miembro | supervisor, disponible }` (miembros y supervisores de su cola; sin
  cola, `[]`).

### 14.2 Conversaciones sin cola

Una conversación **sin cola** la ve todo soporte (`conversaciones.ver`) **solo mientras nadie la
tiene**. En cuanto queda asignada, solo la ven quien la tiene, los supervisores globales y los
mencionados (participantes). Antes la seguían viendo todos.

Regla completa (`conv_visible`; también en la lista, los contadores, `/cambios` y el SSE):
supervisor global · asignada a mí · de una cola de la que soy miembro · de una cola que superviso ·
sin cola y sin asignar · participante (mencionado o de guardia).

### 14.3 Iniciar una conversación (saliente)

`POST /conversaciones/iniciar` (`conversaciones.iniciar`, **sensible**: escribe a clientes por
iniciativa propia):

```json
{ "id": "<uuid v7 del cliente>",            // id del mensaje: el servidor es idempotente por él
  "canalId": 3,                              // número de salida (de una de tus colas)
  "clienteId": 4101, "servicioId": 41010,    // cliente de Argos (servicioId opcional), o
  "contactoIrisId": 12,                      // contacto de Iris (uno de los dos)
  "plantilla": { "codigo": "tecnico_en_camino", "variables": { … } } }
```

→ `201 { conversacion: ConversacionClienteDetalle, mensaje: MensajeConversacion, nueva: bool }`;
el mismo `id` con el mismo contenido → `200` con el mismo cuerpo e `Idempotent-Replayed: true`;
mismo `id` con otro contenido → 409 `mensaje_id_reutilizado`. Igual que los mensajes: el panel
**repite el mismo id** si recibe un 5xx o se corta la red, y **usa un id nuevo** después de un 4xx.
La respuesta no se guarda en el middleware (la clave la aplica el caso).

- **Número de salida:** activo, de la empresa y cuya cola es una de la que eres miembro o
  supervisor; un supervisor global puede usar cualquier número activo de la empresa. Si no, 422
  `numero_no_permitido`. `GET /conversaciones/canales` trae `iniciar: bool` por número (si tú
  puedes iniciar desde él).
- **Plantillas del número:** `GET /conversaciones/canales/{id}/plantillas` (`conversaciones.iniciar`,
  solo personas; lectura sin auditar) → `{ data: PlantillaDisponible[], nextCursor: null }`: las
  plantillas aprobadas en ese número, con la misma forma que `GET /conversaciones/{id}/plantillas`
  (`codigo`, `nombre`, `categoria`, `variables[{ nombre, formato, obligatoria }]`, `vistaPrevia`);
  `data` es `[]` si no hay. El número debe ser uno desde el que puedes iniciar (la misma regla de
  arriba: de una de tus colas; un supervisor global, cualquiera activo de la empresa); si no, 404.
  `categoria` es la de Meta: `servicio` (utility) | `marketing` | `autenticacion`; todas las del
  catálogo de Iris son `servicio`. Desde 1.1, `GET /conversaciones/{id}/plantillas` también la trae.
- **Destinatario:** el número lo pone Iris, nunca el panel. Cliente de Argos: `movil` (si no,
  `telefono`) de la instantánea de lectura (refrescada si tiene más de 24 h), del cliente en la
  empresa (si no, 422 `cliente_fuera_de_empresa`; sin teléfono, 422 `sin_contacto`); el servicio,
  si viene, debe ser suyo (`servicio_fuera_de_empresa`). Contacto de Iris: su `telefono`. El cliente
  queda **verificado** (el número es un teléfono suyo en Argos).
- **Permiso de contacto:** las mismas reglas que una plantilla fuera de la ventana (§8): nunca a un
  número con baja vigente (`baja`); cliente con permiso `acepta` vigente en WhatsApp (si no,
  `sin_permiso` / `baja`) o contacto de Iris con `acepta` por número. La plantilla debe estar
  aprobada en ese número (`plantilla_no_aprobada`) y sus variables son las del catálogo.
- **Conversación:** se crea en la cola del número, **asignada a quien envía**, en
  `esperando_cliente`, con el evento de sistema `iniciada` (`actorNombre`, `plantilla`). Si ya hay
  una conversación de ese número en ese canal (hay una sola por número, canal y empresa) se
  **reutiliza** y la plantilla va ahí, solo si la **ves** y es **sin cola o de una cola que
  atiendes** (miembro o supervisor de la cola; un supervisor global, cualquiera). Si no (p. ej. el
  menú del bot la pasó a Ventas, o está resuelta en otra cola): 409 `conversacion_de_otra_cola`
  (sin decir quién la tiene; un supervisor la asigna). Si se puede: resuelta → se reabre en la cola
  del número; sin asignar o tuya → queda tuya; de otro agente y activa → 409
  `conversacion_de_otro` (el `detail` dice quién; el panel ofrece abrirla o pedir «Pasar»). Con la
  ventana de 24 h abierta en la existente solo se revisa la baja.
- **Asociación de una conversación que ya existe:** se llena con el destinatario solo si estaba
  vacía (sin cliente ni contacto de Iris); si es el mismo, queda igual (y verificado). Si estaba
  asociada a otro, cambiarla exige además `conversaciones.asociar` (si no, 403 `asociacion_requiere_permiso`). El prospecto se
  conserva.
- **Orden de las revisiones:** permiso, re-autenticación (sesión de autologin), número de salida y
  reenvío del id van **antes** de consultar a Argos el contacto del cliente. Si Argos no lo puede
  dar, 422 `cliente_fuera_de_empresa` (sin el detalle de Argos).
- **Buscar a quién escribir** (pantalla «Nueva conversación»; `conversaciones.iniciar`, solo
  personas con sesión, lecturas sin auditar):
  - `GET /conversaciones/destinatarios?q=&canalId=&limit=` (`q` 2–80 letras; `limit` 1–20, 10 por
    defecto; sin cursor) → `{ data: Destinatario[], nextCursor: null }`: busca **a la vez** clientes
    de Argos de la empresa activa en la instantánea de lectura (nombre, código o dígitos del
    teléfono) y contactos de Iris de la empresa (nombre o dígitos), clientes primero. `data` es `[]`
    si no hay nada (nunca `null`). Sin N+1: una llamada trae `puede` y `motivo` de cada resultado.
  - `GET /conversaciones/destinatario?canalId=&clienteId=|contactoIrisId=` → un `Destinatario`
    (la misma forma y las mismas reglas; 404 si el cliente o el contacto no existe).
  - `Destinatario`:
    ```json
    { "tipo": "cliente",            // cliente | contacto_iris
      "id": 4101,                   // id del cliente en Argos o del contacto de Iris
      "nombre": "Carlos Ruiz",
      "codigo": "C-4101",           // código del cliente en Argos; null en un contacto de Iris
      "detalle": null,              // contacto de Iris: su tipo (proveedor, personal, prospecto, otro); cliente: null
      "numero": "+50255120041",     // el número al que saldría (E.164); null si no tiene
      "puede": true,
      "motivo": null,               // null | sin_contacto | sin_permiso | baja | cliente_fuera_de_empresa | numero_no_permitido | conversacion_de_otra_cola | conversacion_de_otro
      "conversacionId": null }      // con canalId: la conversación que ya existe de ese número en ese canal
    ```
    El número va completo (como en la conversación): quien tiene `conversaciones.iniciar` lo ve al
    escribirle. `motivo` usa los mismos códigos que los 422/409 de `POST /conversaciones/iniciar`
    (el panel muestra «Sin número de WhatsApp en Argos» con `sin_contacto`, «No acepta mensajes»
    con `sin_permiso` o `baja`). `cliente_sin_confirmar` no aplica: el número sale de Argos. Sin
    `canalId` solo se evalúan el número y el permiso de contacto (`numero_no_permitido`,
    `conversacion_de_otra_cola` y `conversacion_de_otro` necesitan el número de salida). Con
    `canalId` se revisa primero el número de salida; `conversacionId` solo viene si ves esa
    conversación (con `conversacion_de_otra_cola`, `null`). Un `canalId` que no existe o es de otra
    empresa da el mismo 422 (`canalId`). `puede` = `motivo` es `null`.
- Outbox: `conversacion.creada` (si es nueva, con `origen: agente`), `conversacion.iniciada`,
  `conversacion.asignada` (`origen: iniciar`, si cambió quién la tiene) y
  `conversacion.mensaje_enviado`.

### 14.4 «Pasar a…»

`POST /conversaciones/{id}/pasar` (`conversaciones.pasar`) `{ version, usuarioId, nota? }`:

- La pasa **quien la tiene** a un compañero de la misma cola (miembro o supervisor de la cola; sin
  cola, cualquiera que atiende conversaciones en la empresa). Un supervisor global o de la cola
  puede pasar **cualquier** conversación de la cola (también una sin asignar). Si no: 403
  `no_es_tuya`. Al mismo que la tiene: 422 `mismo_agente`. A quien no es de la cola: 422
  `usuario_no_disponible`.
- Se puede pasar a alguien **no disponible**: la respuesta y el evento lo dicen y el panel avisa.
- `nota` (1–1000, opcional) queda como nota interna; evento de sistema `pasada` (`actorNombre`,
  `asignadoNombre`, `detalle: "no_disponible"` si lo estaba).
- → `200 { conversacion: ConversacionClienteDetalle, destino: UsuarioRef, destinoDisponible: bool }`.
- Outbox `conversacion.asignada` con `origen: pasar`, `disponible` (del destino) y `nota_id`
  (o `null`).
- «Asignar» (`conversaciones.asignar`) queda igual para supervisores (§7.3, §14.1).

### 14.5 Disponibilidad de los agentes

Por usuario y empresa: `disponible` | `no_disponible`. La persona la cambia a mano y pasa sola a
`no_disponible`:

| `origen` | Cuándo |
| --- | --- |
| `manual` | `POST /conversaciones/disponibilidad` |
| `cierre_sesion` | La sesión con que se puso disponible (o la del último latido) se cerró o venció |
| `inactividad` | Sin actividad en `minutosInactividad` minutos (ajuste de la empresa, 15 por defecto) |
| `horario` | Terminó su horario de trabajo (abajo) |

**Horario de trabajo del agente** (decisión de I1): el de sus colas activas (miembro o supervisor):
el horario de la cola o, si la cola no tiene, el de los números que llegan a ella; sin ninguno,
siempre abierto. Con varias colas vale la unión (en horario si alguna está abierta). Al ponerse
disponible y en cada latido, Iris anota `horarioHasta` = el cierre más tardío de las franjas
abiertas en ese momento; al llegar esa hora pasa a `no_disponible` (`horario`). Quien se pone
disponible a mano **fuera** de su horario queda disponible (es una decisión explícita) hasta la
inactividad, el cierre de sesión o el cierre de la próxima franja que anote un latido.

- **Actividad:** `POST /conversaciones/disponibilidad/latido` (sin cuerpo, `conversaciones.ver`):
  el panel lo llama cada 60 s mientras la persona está activa (teclado, ratón, pestaña visible).
  Cualquier acción de la bandeja (responder, nota, tomar, asignar, pasar, transferir, etiquetar,
  resolver, iniciar…) también cuenta. El latido **no** deja fila de auditoría ni evento y no cambia
  el estado: responde la disponibilidad actual (si quedaste `no_disponible`, sigues así hasta
  cambiarla).
- `GET /conversaciones/disponibilidad` (`conversaciones.ver`) → la tuya
  `{ estado, origen, desde, horarioHasta, minutosInactividad, version }` (sin fila: `no_disponible`,
  `origen: manual`, `desde: null`, `version: 0`). El panel la muestra junto a tu nombre; `/yo` no
  cambia (la disponibilidad vive en el servicio de mensajería).
- `POST /conversaciones/disponibilidad` `{ estado, version }` (`conversaciones.ver`): auditado; si
  cambia, evento `agente.disponibilidad_cambiada` (`origen: manual`). `version` es la del GET (0 la
  primera vez); 409 `conflicto_version` si cambió en otra pestaña o la cambió el mantenimiento.
- `GET /conversaciones/ajustes` (`conversaciones.ver`) → `{ minutosInactividad, version }`;
  `POST /conversaciones/ajustes` (`canales.configurar`) `{ version, minutosInactividad }` (1–480;
  `version` 0 la primera vez; evento `conversaciones.ajustes_cambiados`).
- Se ve en: `GET /conversaciones/agentes` (`disponibilidad` por agente),
  `ColaConversaciones.disponibles[]`, el detalle (`equipo[].disponible`) y `espera` de las
  conversaciones que esperan (§14.6). No hay evento del flujo por cambio de disponibilidad: el panel
  relee esas listas al abrir la conversación o el diálogo.
- Los cambios automáticos los hace el mantenimiento (cada minuto; un caso por agente con la fila
  bloqueada, así dos barridos a la vez no repiten la transición), dejan **una fila de auditoría por
  transición** (actor sistema, `mensajeria.vencer_disponibilidad`) y emiten el mismo evento con su
  `origen`; los manuales también se auditan. Los latidos nunca.

### 14.6 «¿Alguien la atiende?» y el bot «Cuando no hay agentes»

**Configuración por número**: campo `bot` de `POST /canales/{id}/configurar` (ausente = no cambia),
que devuelven `GET /conversaciones/canales` y `GET /canales`:

```json
"bot": { "activo": true,
         "atiende": "bot",                 // agente_autonomo: fase 3 (422 no_disponible_aun)
         "minutosSinConfirmacion": 30,     // 1–1440
         "minutosSinDisponibles": 3,       // 1–1440
         "saludo": "Gracias por escribir. En este momento todos nuestros agentes están ocupados; te respondemos en cuanto se libere uno.",
         "menu": [ { "texto": "Ventas", "colaId": 4, "etiquetaIds": [2] },          // hasta 9
                   { "texto": "Soporte técnico", "colaId": 2, "etiquetaIds": [] } ] }
```

- `saludo` 1–1024 caracteres de texto plano; `menu[].texto` 1–60; `colaId` una cola activa de la
  empresa; `etiquetaIds` hasta 5 etiquetas activas. La cola puede cambiar los minutos
  (`botSinConfirmacionMin`, `botSinDisponiblesMin`, §14.1): **vale el de la cola**, como el horario.
- `activo: false` (los números que ya existían y los nuevos hasta que alguien lo active): ni aviso ni
  bot en horario; fuera de horario sigue la respuesta de siempre (§10.2), sin menú.
- Migración: el `mensaje` de fuera de horario de cada número quedó también como `saludo` del bot (si
  no había, el texto de arriba). `fueraDeHorario` no cambia y sigue mandando fuera de horario.

**Con el horario abierto** (de la cola o, si no tiene, del número), al llegar un mensaje del cliente
a una conversación **sin asignar** sin aviso ni bot ya en curso en esta espera:

1. **Hay disponibles en la cola** (miembros o supervisores de la cola en `disponible`; sin cola, los
   disponibles de la empresa): se les avisa — evento de sistema `aviso_sin_atender` (`colaNombre`,
   `disponibles`), evento `aviso_sin_atender` del flujo solo para cada avisado (el panel muestra
   «¿Alguien la atiende?» con la cuenta regresiva; «La atiendo» = `POST …/tomar`) y outbox
   `conversacion.aviso_sin_atender` (para un futuro push). Si nadie la toma en
   `minutosSinConfirmacion`, contesta el bot (`saludo` y menú) con el evento de sistema
   `bot_atendio` (`detalle: "sin_confirmacion"`, `minutos`): «Nadie la tomó en 30 min: contestó el
   bot».
2. **No hay nadie disponible:** el bot manda el `saludo` **en el momento** y, si en
   `minutosSinDisponibles` nadie la tomó, toma la conversación: manda el menú (si hay) y queda el
   evento de sistema `bot_atendio` (`detalle: "sin_disponibles"`): «Sin agentes disponibles:
   contestó el bot».

**Con el horario cerrado:** como hoy (§10.2: `fueraDeHorario.mensaje` una vez por ventana cerrada y
la guardia), pero lo manda el bot: con el bot activo sale `conversacion.bot_atendio` con
`motivo: fuera_de_horario` (además de `conversacion.fuera_de_horario`) y, si tiene menú, el menú va
en un segundo mensaje después del de fuera de horario. Esto no abre una espera: el primer mensaje
del cliente con el horario abierto (si sigue sin asignar) avisa a los disponibles como arriba.

**Menú:** si el cliente contesta con el número de una opción (`1`…`9`; se ignoran espacios y
puntuación), la conversación pasa a la cola de la opción con sus etiquetas (se suman a las que
tenía, máx. 10), evento de sistema `menu_elegido` (`colaNombre`, `detalle` = texto de la opción),
outbox `conversacion.menu_elegido`, y se avisa a los disponibles de esa cola (sin otro
temporizador: el bot ya atendió). Una respuesta que no es una opción repite el menú **una vez**; a
la segunda, el bot deja de insistir.

**Reglas comunes:**
- El bot nunca contesta a `BAJA`/`ALTO` ni a un número o cliente con baja vigente (las reglas de §8),
  ni con la ventana de 24 h cerrada. Sus mensajes son salientes sin `autor` con
  `automatico: "bot"` (`"fuera_de_horario"` la respuesta de §10.2), y no cambian el estado ni el SLA.
- Cada respuesta del bot deja una fila de auditoría como actor sistema: `mensajeria.bot_conversacion`
  (temporizador) o `mensajeria.bot_respondio` (saludo inmediato, fuera de horario, menú repetido o
  elegido), con la conversación y el id del mensaje (nunca el texto).
- Tomar, asignar, pasar, transferir, resolver, iniciar o responder (si asigna) **cancelan** el
  temporizador y el menú. El mantenimiento procesa
  cada minuto todo lo vencido hasta ahora y vuelve a revisar con la fila bloqueada: un temporizador
  cancelado nunca dispara y uno que ya disparó no se repite. Una vez que el bot contestó, no vuelve a
  hacerlo en la misma espera; resolver, tomar o asignar empiezan una espera nueva.
- `ConversacionCliente.espera` (ausente si está asignada o resuelta):
  `{ disponibles: UsuarioRef[] (hasta 5), totalDisponibles, botContestaEn (o null),
  botMotivo (sin_confirmacion | sin_disponibles | null), botAtendio }` para «2 disponibles en
  Soporte», «Nadie disponible en Cobros» y «Bot en 12 min».
- `MensajeConversacion.automatico`: `"bot"` | `"fuera_de_horario"` | `null`; el mismo valor en
  `ConversacionCliente.ultimoMensaje.automatico` (la lista muestra «Bot: …»).
- `Variable` (en las plantillas de la conversación, del número y de `GET /plantillas`) trae
  `obligatoria: bool`. Hoy todas son `true` (Meta exige todos los parámetros de una plantilla
  aprobada); el panel debe pedir solo las obligatorias.
- Eventos de sistema nuevos (`evento.tipo`): `aviso_sin_atender`, `bot_atendio`, `menu_elegido`,
  `pasada`, `iniciada`. `EventoSistema` suma `minutos` y `disponibles` (enteros, opcionales).
- Tipo nuevo del flujo (SSE y `/cambios`): `aviso_sin_atender` (solo a los avisados).

### 14.7 Eventos nuevos (outbox)

`conversacion.iniciada`, `conversacion.aviso_sin_atender`, `conversacion.bot_atendio`,
`conversacion.menu_elegido`, `agente.disponibilidad_cambiada` y
`conversaciones.ajustes_cambiados`; `conversacion.asignada` suma los orígenes `pasar` e `iniciar`
(con `pasar`, `disponible` y `nota_id`); `conversacion.creada` suma `origen` (`cliente` | `agente`).
Detalle en `contratos/eventos.md`.

## 15. Conexión con Meta, WhatsApp por QR (coexistencia) y préstamo de números (aprobado por el PM)

Misma base, auth (persona con sesión del panel: un token de API o una integración reciben 404),
errores RFC 9457, `Idempotency-Key` y `version` que el resto del contrato. Las credenciales (app
secret, clave de enlace, token del negocio, verify token) se guardan cifradas con
`IRIS_CLAVE_CIFRADO`, son de **solo escritura** y nunca salen en respuestas, logs, auditoría (`***`)
ni eventos. El `code` de Meta tampoco.

Documentación de Meta en que se apoya (revisada el 8 nov. 2026):

- Embedded Signup, implementación (evento `WA_EMBEDDED_SIGNUP`, `FB.login` con `config_id`,
  `response_type: code`, vida del código de **30 s**):
  https://developers.facebook.com/docs/whatsapp/embedded-signup/implementation/
- Coexistencia («Onboarding WhatsApp Business app users»: `extras.featureType:
  "whatsapp_business_app_onboarding"`, evento `FINISH_WHATSAPP_BUSINESS_APP_ONBOARDING`, **no**
  llamar `/register`, `smb_app_data` con `smb_app_state_sync` e `history` una vez cada uno y dentro
  de 24 h, historial de 180 días, 20 mensajes por segundo, campos `smb_message_echoes`, `history`,
  `smb_app_state_sync` y `account_update`):
  https://developers.facebook.com/docs/whatsapp/embedded-signup/custom-flows/onboarding-business-app-users/
- Canje del código y suscripción a la WABA (Tech Provider: `GET /oauth/access_token?client_id&client_secret&code`
  → `{ access_token, token_type }`; `POST /<WABA_ID>/subscribed_apps`):
  https://developers.facebook.com/docs/whatsapp/embedded-signup/onboarding-customers-as-a-tech-provider
- URL de webhook alterna por WABA (`override_callback_uri` + `verify_token`, ≤ 200 caracteres):
  https://developers.facebook.com/docs/whatsapp/embedded-signup/webhooks/override
- `account_update` (`PARTNER_REMOVED` con `disconnection_info.reason`, `ACCOUNT_OFFBOARDED`,
  `ACCOUNT_RECONNECTED`; `waba_info.waba_id`):
  https://developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/reference/account_update/
- Token de la app como `access_token={app_id}|{app_secret}`:
  https://developers.facebook.com/docs/facebook-login/guides/access-tokens/

### 15.1 Conexión con Meta (una por instalación)

Una sola para todo el Iris (todas las empresas), en `mensajeria.conexion_meta`. La usan los números
que se conectan con QR; los números por «datos de la API» (§1.1) no la necesitan. Permiso nuevo
**`mensajeria.conexion_meta`** (sensible; solo el rol `admin`).

| `modo` | Qué es |
| --- | --- |
| `sin_configurar` | Nadie la configuró todavía (el QR no está disponible) |
| `propia` | La app de Meta del ISP: `appId`, `appSecret` (solo escritura), `configId` (la configuración de Facebook Login para empresas de tipo WhatsApp Embedded Signup) y `graphVersion` (vacío = la de Iris). Para el QR la app debe ser Tech Provider aprobado; Iris no lo puede comprobar |
| `compartida` | Enlazada al servicio central «Iris Cloud» (`servicioUrl`, por defecto `https://cloud.iris.kns.gt`) con una `claveEnlace` (solo escritura). **El servicio central aún no existe**: se guarda la configuración, `estado` es `sin_servicio_central` y el QR responde 422 `servicio_central_no_disponible` |
| `saas` | Iris en la nube: la fija el entorno (`IRIS_META_MODO=saas`, con `IRIS_META_APP_ID`, `IRIS_META_APP_SECRET`, `IRIS_META_CONFIG_ID`, `IRIS_META_GRAPH_VERSION`, `IRIS_META_VERIFY_TOKEN`). Solo lectura en el panel (`gestionada: true`) |

`GET /conexion-meta` (`mensajeria.conexion_meta`) → `ConexionMeta`:

```json
{ "modo": "propia", "gestionada": false,
  "appId": "1298475561023344", "configId": "889900112233",
  "graphVersion": "v21.0",               // la efectiva
  "graphVersionPropia": null,            // la guardada; null = la de Iris
  "appSecretGuardadoEn": "…",            // null = no hay; nunca el valor
  "servicioUrl": "https://cloud.iris.kns.gt", "claveEnlaceGuardadaEn": null,
  "webhookUrl": "https://iris.ejemplo.gt/api/v1/mensajeria/meta/webhook", "webhookUrlCompleta": true,
  "verifyTokenGuardadoEn": "…", "webhookVerificadoEn": "…", "ultimoWebhookEn": "…",
  "camposWebhook": ["messages", "smb_message_echoes", "history", "smb_app_state_sync", "account_update"],
  "estado": "lista",                     // sin_configurar | sin_probar | lista | error | sin_servicio_central | gestionada
  "qrDisponible": true,
  "motivoQr": null,                      // sin_configurar | servicio_central_no_disponible | null
  "ultimaPruebaEn": "…",
  "comprobaciones": [ { "clave": "app", "estado": "ok", "detalle": "Meta aceptó el app ID y el app secret: app «Red Sur Iris»" },
                      { "clave": "configuracion", "estado": "sin_verificar", "detalle": "…" },
                      { "clave": "webhook", "estado": "ok", "detalle": "…" },
                      { "clave": "tech_provider", "estado": "sin_verificar", "detalle": "…" } ],
  "version": 3, "actualizadoEn": "…",
  "verifyTokenGenerado": "vt_…" }        // SOLO en la respuesta de actualizar cuando Iris lo generó
```

- `comprobaciones[].clave`: `app`, `configuracion`, `webhook`, `tech_provider`; `estado`: `ok`,
  `error`, `advertencia`, `sin_verificar`. Vacía (`[]`) sin prueba.
- `qrDisponible`: modo `propia` con app ID, app secret y config ID guardados (o `saas` con los del
  entorno). Probar no es requisito (el panel muestra «sin probar»).

`POST /conexion-meta/actualizar` (`mensajeria.conexion_meta`, `Idempotency-Key`):

```json
{ "version": 0,                // 0 la primera vez; 409 conflicto_version si cambió
  "modo": "propia",            // propia | compartida
  "appId": "1298475561023344", // 1–30 dígitos (obligatorio en propia)
  "appSecret": "…",            // solo escritura; vacío = no cambia (obligatorio la primera vez en propia)
  "configId": "889900112233",  // 1–30 dígitos (obligatorio en propia)
  "graphVersion": null,        // "v21.0" (^v[0-9]{1,2}\.[0-9]$) o null = la de Iris
  "servicioUrl": null,         // https; null = la por defecto (solo compartida)
  "claveEnlace": "…",          // solo escritura; vacío = no cambia (obligatoria la primera vez en compartida)
  "generarVerifyToken": false } // «Generar otro» del verify token del webhook de la app
```

- Iris genera el verify token del webhook de la app la primera vez que se guarda la conexión (o con
  `generarVerifyToken`) y lo devuelve **una vez** en `verifyTokenGenerado` (esa respuesta no se guarda
  con la `Idempotency-Key`: repetirla da 409 `idempotencia_no_repetible`). Uno nuevo borra
  `webhookVerificadoEn`.
- Cambiar de modo **no** desconecta los números ya conectados (su token del negocio es suyo). Cambiar
  `appId`, `configId` o el secreto borra la última prueba.
- En `saas`: 422 `conexion_gestionada`.
- Auditada (`mensajeria.conexion_meta`, secretos como `***`); evento `meta.conexion_cambiada`
  `{ modo, anterior, campos[] }` (nunca valores de secretos).

`POST /conexion-meta/probar` (sin cuerpo; `mensajeria.conexion_meta`, `Idempotency-Key`) →
`ConexionMeta` con `comprobaciones` y `ultimaPruebaEn` nuevas:

- `app`: `GET /{app_id}?fields=id,name&access_token={app_id}|{app_secret}` (Graph, 15 s; el token de
  la app como lo documenta Meta en https://developers.facebook.com/docs/facebook-login/guides/access-tokens/).
  2xx con el
  mismo `id` → `ok`; código 190/101 o HTTP 400–401 → `error` («Meta no aceptó el app ID o el app
  secret»); otra cosa → `error` con el HTTP y el código (nunca el mensaje de Meta).
- `configuracion`: Iris solo revisa el formato (Meta no publica una lectura de la configuración):
  `sin_verificar`.
- `webhook`: `ok` si Meta ya verificó el webhook de la app (`webhookVerificadoEn`), si no
  `advertencia`.
- `tech_provider`: siempre `sin_verificar` («si la app no es Tech Provider, el QR falla al final»).
- `estado` queda `lista` (sin `error`) o `error`. Auditada; evento `meta.conexion_probada
  { modo, estado }`. En `compartida`: `estado: sin_servicio_central` sin llamar a nadie; en `saas`
  prueba la app del entorno. Sin conexión: 422 `conexion_meta_sin_configurar`.

**Webhook de la app** (lo que Meta llama para los números conectados con QR):
`GET /meta/webhook` (verificación con `hub.verify_token` = el verify token de la conexión; 403
`verificacion_invalida`) y `POST /meta/webhook` (firmado `X-Hub-Signature-256` con el app secret de
la conexión; 401 `firma_invalida`). Cada `entry` se reparte al número QR de su `phone_number_id`
(o de su WABA en `account_update`); un `entry` con cambios de varios números se separa por número.
Sigue igual que el webhook de un número (`entrantes`, job).
Entradas de números que Iris no tiene se descartan. Suscribe en Meta los campos de
`camposWebhook`. El webhook propio de un número QR (`/canales/{id}/webhook`) también vale y se
verifica con el app secret de la conexión.

**Secretos en la URL:** el canje y la prueba de la app llevan el secreto (y el código) en la consulta
porque es lo que Meta documenta. Son llamadas de servidor a servidor por TLS; Iris nunca registra esas
URLs: el error de red de la llamada (que incluye la URL) se descarta y la respuesta, la auditoría y
los logs solo llevan un texto fijo, el HTTP y el código de Meta.

**Servicio central (modo `compartida`)**: el código define la interfaz `RegistroMeta`
(`CanjearCodigo`, `ConsultarNumero`, `SuscribirWABA`, `PedirSincronizacion`) con dos implementaciones:
la de la app propia (Graph API) y la del servicio central, que hoy responde siempre
`servicio_central_no_disponible`. Cuando exista Iris Cloud se cambia solo esa implementación.

### 15.2 Conectar un número con QR (coexistencia)

Para un número que ya se usa en la app **WhatsApp Business**: sigue funcionando en el teléfono y
también llega a Iris. Solo con la conexión en `propia` (o `saas`).

**1. Datos para abrir la ventana de Meta** — `GET /conexion-meta/registro`
(`mensajeria.configurar`; lectura sin auditar) →

```json
{ "disponible": true, "motivo": null,          // motivo: sin_configurar | servicio_central_no_disponible
  "modo": "propia", "appId": "1298475561023344", "configId": "889900112233", "graphVersion": "v21.0",
  "featureType": "whatsapp_business_app_onboarding", "sessionInfoVersion": "3" }
```

Nunca el secreto (`appId` y `configId` van null si no está disponible). El panel carga el SDK de
Facebook con `appId` y `graphVersion` y llama `FB.login({ config_id, response_type: "code",
override_default_response_type: true, extras: { setup: {}, featureType, sessionInfoVersion } })`.
Escucha `message` de un origen que termine en `facebook.com` con `type: "WA_EMBEDDED_SIGNUP"`:
`FINISH_WHATSAPP_BUSINESS_APP_ONBOARDING` trae `data.waba_id` (y `phone_number_id`, como `FINISH`),
`CANCEL` (`current_step`) y `ERROR` (`error_message`, `error_code`). El dominio del panel debe estar
en «Dominios permitidos para el SDK de JavaScript» de Facebook Login para empresas.

**2. Crear el número** — `POST /canales/qr` (`mensajeria.configurar`, `Idempotency-Key`). El panel
lo llama **en cuanto** recibe el `code` (vive ~30 s):

```json
{ "code": "AQB…",                    // el código del callback de FB.login (1–2048); nunca se guarda ni se registra
  "wabaId": "980198427658004",       // 1–30 dígitos
  "phoneNumberId": "106540352242922",// 1–30 dígitos
  "nombre": "Ventas tienda",         // 1–120
  "usos": ["ventas"],                // opcional (por defecto ["soporte"])
  "colaId": 4,                       // opcional
  "empresaId": 1 }                   // opcional: por defecto la empresa activa
```

→ `201 Canal`. Iris, en este orden y dentro de la vida del código:

1. Valida la entrada, el modo de la conexión y que el `phoneNumberId` no esté en otro número activo
   (409 `canal_existente`). Si ya hay un número QR **de la misma empresa** con ese
   `phoneNumberId` (p. ej. desconectado por Meta), lo **reconecta** (mismo id; 200).
2. Canjea el código **una sola vez** (`GET /oauth/access_token?client_id&client_secret&code`, el
   método que documenta Meta en
   https://developers.facebook.com/docs/whatsapp/embedded-signup/onboarding-customers-as-a-tech-provider). El
   SHA-256 del código queda registrado: el mismo código otra vez (aunque sea con otra clave) da 422
   `codigo_vencido` sin volver a llamar a Meta. Repetir la misma `Idempotency-Key` devuelve la misma
   respuesta.
3. Comprueba que el número es de la WABA (`GET /{waba_id}/phone_numbers?fields=id`; si no está,
   422 `numero_no_elegible`) y lo lee (`GET /{phone_number_id}?fields=display_phone_number,verified_name,is_on_biz_app,platform_type`)
   con el token del negocio. `is_on_biz_app: false` → 422 `numero_no_elegible`.
4. Suscribe la app a la WABA (`POST /{waba_id}/subscribed_apps`) con `override_callback_uri` = el
   webhook de este número (`…/canales/{id}/webhook`) y su `verify_token`: **siempre** en modo
   `saas` y `compartida` (la app no es de esta instalación; sin `IRIS_BASE_URL` absoluta, 422
   `base_url_requerida` antes de canjear el código), y en `propia` cuando ya hay otro número QR
   activo y `IRIS_BASE_URL` es absoluta (si no, los webhooks llegan al de la app). **Nunca** llama
   `/register`.
5. Guarda el número con `tipoConexion: "qr"`, `coexistencia: true`, el token del negocio cifrado y un
   verify token propio; `conectadoEn` = ahora y el plazo de sincronización = `conectadoEn + 24 h`.
   Evento `canal_mensajeria.creado` (con `tipo_conexion: qr`), o `canal_mensajeria.reconectado`.

Errores (`detail` en español; nunca el código, el token ni el `message` de Meta):

| Código | Status | Cuándo |
| --- | --- | --- |
| `codigo_vencido` | 422 | Meta rechazó el canje (código vencido o ya usado: error 100 o 190 del canje) o el código ya se usó en Iris |
| `app_no_aprobada` | 422 | La app no tiene permiso: error 3, 10, 101 (secreto inválido) o 200–299, o HTTP 403 |
| `numero_no_elegible` | 422 | `is_on_biz_app: false`, o Meta no da acceso a ese número (100/33) |
| `error_meta` | 502 | Cualquier otra respuesta, 5xx, 429 o sin respuesta. `detail` lleva «Código N · Tipo · fbtrace X» cuando Meta los da |
| `servicio_central_no_disponible` | 422 | Conexión en modo `compartida` |
| `conexion_meta_sin_configurar` | 422 | Sin conexión `propia`/`saas` completa |
| `cifrado_no_disponible` | 422 | Sin `IRIS_CLAVE_CIFRADO` |

**3. Traer contactos e historial** — `POST /canales/{id}/sincronizar` `{ contactos: bool, mensajes:
bool }` (`mensajeria.configurar`, `Idempotency-Key`; al menos uno en `true`, si no 422 `validacion`)
→ `Canal`. Solo números QR (si no, 422 `no_es_coexistencia`). Por cada tipo pedido Iris llama
`POST /{phone_number_id}/smb_app_data` (`messaging_product: whatsapp`, `sync_type`
`smb_app_state_sync` o `history`) con el token del número. Meta lo permite **una vez** y **dentro de
24 h** desde `conectadoEn`:

- ya pedido (y Meta lo aceptó): 422 `sincronizacion_ya_pedida`;
- pasado el plazo: 422 `sincronizacion_vencida`;
- si la llamada a Meta falló, queda `fallido` y se puede pedir otra vez dentro del plazo; un
  `pedido` sin respuesta de Meta (la llamada no terminó) se puede repetir pasados 10 minutos.

`Canal.sincronizacion` (solo en números QR; ausente en los demás):

```json
{ "plazo": "2026-11-09T16:42:00Z",
  "contactos": { "estado": "listo", "pedidoEn": "…", "total": 312, "asociados": 184, "detalle": null },
  "mensajes":  { "estado": "recibiendo", "pedidoEn": "…", "progreso": 62, "fase": 1, "mensajes": 5210, "detalle": null } }
```

`estado`: `sin_pedir`, `pedido`, `recibiendo` (llegó algo), `listo` (contactos: llegó un lote;
mensajes: `progress` 100), `rechazado` (el dueño no aceptó compartir el historial, error 2593109) o
`fallido` (la llamada a Meta falló; `detalle`).

**4. Webhooks de coexistencia** (por el webhook del número o el de la app):

- `smb_message_echoes` (lo que se manda **desde el teléfono**): entra como mensaje `saliente` en la
  conversación del cliente (`to`) con `origen: "telefono"`, sin `autor` (el panel lo rotula «Desde el
  teléfono»), `estadoEntrega: enviado`, deduplicado por su id. Pasa la conversación a
  `esperando_cliente` y detiene el SLA como una respuesta. **No** asigna, **no** dispara el bot ni
  avisos, **no** abre espera. Si no había conversación se crea (`conversacion.creada` con
  `origen: telefono`) sin asignar. Outbox `conversacion.mensaje_enviado` con `origen: telefono` y
  `autor_id: null`. Actualiza `Canal.ultimoEcoEn`.
- `history`: cada hilo (`threads[].context.wa_id` o `threads[].id`) entra a la conversación de ese
  número en la empresa **dueña** del número; si no existe se crea **resuelta**. Mensajes con
  `origen: "historial"` (entrante si `from` es el cliente, saliente si no), con su fecha original
  (`creadoEn`), deduplicados por id. **Sin** eventos de outbox de conversación (ni creada, ni
  recibido), sin bot, sin avisos, sin encuestas, sin SLA y sin cambiar el estado de una conversación
  que ya existía. El historial se agrega al final (`seq`) con su fecha original; un mensaje más viejo
  que el último de la conversación no cambia `ultimoMensaje` ni `ultimaActividad`. Actualiza
  `sincronizacion.mensajes` (`progreso`, `fase`, `mensajes`). `errors[].code = 2593109` →
  `rechazado`.
- `smb_app_state_sync` (contactos del teléfono, `action: add`): si el teléfono es de **un** cliente
  de la instantánea de Argos de la empresa dueña, cuenta como `asociados` y no se crea nada (los datos
  de Argos nunca se tocan); si no, crea un contacto de Iris (`tipo: otro`, nombre = `full_name`,
  origen teléfono) o, si ya existe con ese teléfono **y vino del teléfono**, le actualiza el nombre
  (sube `version`). Un contacto que creó una persona no se toca. Evento `contacto_iris.creado` con
  `origen: telefono` al crear. `remove` no borra nada.
- `account_update` (por `waba_info.waba_id` o `entry.id`; vale para **todos** los números QR de la
  WABA): `PARTNER_REMOVED` (con
  `disconnection_info.reason`, p. ej. `PRIMARY_INACTIVITY` = ~14 días sin abrir la app) y
  `ACCOUNT_OFFBOARDED` dejan el número **desconectado** (`desconectadoEn`, `desconectadoMotivo` =
  la razón en minúsculas, o `partner_removed` / `account_offboarded`); `ACCOUNT_RECONNECTED` lo
  reconecta. Eventos `canal_mensajeria.desconectado { canal_id, empresa_id, motivo }` y
  `canal_mensajeria.reconectado { canal_id, empresa_id, origen }`. Un número desconectado no envía
  (avisos `fallido` / `rechazado_por_canal`; mensajes `fallido` / `rechazado_por_canal`) y su
  `conexion` es `desconectado`; «Reconectar» = repetir el QR (paso 2).
- `MensajeConversacion.origen`: `"telefono"` | `"historial"`; **ausente** = salió de Iris (opcional
  para no romper los tipos que ya existen).

**Campos nuevos de `Canal`**: `tipoConexion` (`api` | `qr`), `coexistencia` (bool), `ultimoEcoEn`
(último mensaje mandado desde el teléfono, precisión de 1 minuto; el panel muestra «Teléfono sin
actividad N días» y Meta desconecta a los ~14 días), `conectadoEn`, `desconectadoEn`,
`desconectadoMotivo`, `sincronizacion` (arriba); `conexion` suma `desconectado`. En un número QR,
`tokenGuardadoEn` es cuándo se canjeó el token y `appSecretGuardadoEn` es null (usa el de la
conexión). Actualizar (`/actualizar`) un número QR: token, app secret, `phoneNumberId` y `numero` no
se editan (422 `numero_qr`).

### 15.3 Prestar un número a otras empresas

La empresa **dueña** de un número lo comparte con otras empresas de la misma instalación. Cada
**préstamo** (`mensajeria.canal_prestamos`, uno por número y empresa) tiene:

| Campo | Qué es |
| --- | --- |
| `empresaId`, `empresaNombre` | La empresa que lo usa |
| `usos[]` | Los que puede usar: `avisos`, `soporte`, `cobros`, `ventas` (1–4). **Para quien lo usa sí limitan** (para el dueño siguen siendo etiquetas) |
| `colaId`, `colaNombre` | Cola de esa empresa donde caen sus conversaciones (null = sin cola) |
| `avisosEmpresa` | La empresa que lo usa lo marcó como su número de avisos (solo con `avisos`) |
| `estado` | `activo` \| `revocado` |
| `prestadoEn`, `revocadoEn`, `motivoRevocacion` | |
| `conversacionesAbiertas`, `avisosProgramados` | De esa empresa por este número (para el diálogo de revocar) |
| `version` | |

Endpoints (todos con `canales.configurar` **en la empresa dueña** y solo personas; de otra empresa,
404):

| Método y ruta | Qué hace |
| --- | --- |
| `GET /canales/{id}/prestamos` | `{ data: Prestamo[], nextCursor: null }` (activos y revocados) |
| `GET /canales/{id}/prestamos/opciones` | `{ empresas: [{ id, nombre, colas: [{ id, nombre }] }] }`: las otras empresas registradas de la instalación (no tienen estado inactivo) con sus colas activas (para «Agregar empresa» y «Cola en …») |
| `POST /canales/{id}/prestamos` | `{ empresaId, usos[], colaId, version? }` crea o cambia el préstamo (`version` obligatoria si ya existe; 0/ausente para uno nuevo; uno revocado se vuelve a activar) → `Prestamo`. Evento `canal_mensajeria.prestado` `{ canal_id, empresa_id (dueña), prestatario_id, usos, cola_id, nuevo }` |
| `POST /canales/{id}/prestamos/{empresa}/revocar` | `{ version, motivo }` (motivo 5–300, la confirmación) → `Prestamo`. Evento `canal_mensajeria.prestamo_revocado` `{ canal_id, empresa_id, prestatario_id, motivo, conversaciones_cerradas, avisos_cancelados }` |

Validaciones: la empresa existe (las empresas no tienen estado activo/inactivo en Iris: vale cualquiera
registrada) y no es la dueña (422 `empresa_no_valida`); la cola es de **esa**
empresa y está activa (422 `colaId`); el número está activo (422 `canal_inactivo`). Quitar
`avisos` de los usos quita `avisosEmpresa`.

**Quién usa el número** (la empresa que lo recibe, con `canales.configurar` / `mensajeria.configurar`):

- `GET /canales` y `GET /canales/{id}` lo muestran con `prestadoPor` (`{ empresaId, nombre }`) y
  `prestamo` (`{ usos, colaId, avisosEmpresa, version, prestadoEn }`), **sin** datos de Meta ni
  conexión: `phoneNumberId`, `wabaId`, `webhookUrl`, `plantillas` y las fechas de credenciales van
  vacíos (`""`, `{}` o null) y `empresaId` es la empresa que lo usa. Quien ve todas las empresas
  (`empresas.todas`) recibe en `GET /canales` el número como de su dueña **y además**, para la
  empresa activa, la entrada prestada (mismo `id`, `empresaId` = la empresa activa, con
  `prestadoPor`): el panel filtra por `empresaId`. `GET /canales/{id}` con la empresa activa que lo
  usa devuelve la vista prestada. `GET /conversaciones/canales` lo trae con `prestadoPor`, y los
  `usos` y la `colaId` **del préstamo**. Editar, probar, activar, desactivar o prestar: 404 (salvo
  quien ve todas las empresas, que lo administra como dueña).
- `POST /canales/{id}/prestamo/configurar` `{ version, avisosEmpresa }` (`canales.configurar`, en la
  empresa que lo usa; `version` la del préstamo) → `Canal`: lo marca como su **número de avisos de la
  empresa** (exige `avisos` en el préstamo: 422 `uso_no_permitido`); quita la marca de sus números
  propios y de otros préstamos (y al revés: marcar uno propio la quita del préstamo). Evento
  `canal_mensajeria.actualizado` con su `empresa_id` y `campos: ["avisos_empresa"]`.
- Avisos de su Argos (`POST /avisos`): salen por el número prestado solo si `avisos` está permitido y
  lo marcó como su número de avisos; usan las plantillas y la WABA del dueño.
- Conversaciones: contestar dentro de la ventana en sus conversaciones; **iniciar** (§14.3) y mandar
  **plantillas** solo si el préstamo tiene `soporte`, `cobros` o `ventas` (si no, 422
  `uso_no_permitido`). Para iniciar, el número cuenta como de la cola del préstamo.
- Horario, bot y fuera de horario son del dueño y **no** se aplican a las conversaciones de otra
  empresa (no les contesta el bot del dueño).

**Calidad y límites compartidos:** la calificación de calidad, el límite de mensajes y las pausas
de Meta son **del número**: si los avisos de una empresa se marcan como spam, el número baja (o se
pausa) para todas. El tope por minuto de Iris (§6.5 de Argos) también es por número.

**A qué empresa va un mensaje que llega** a un número compartido (con algún préstamo, activo o
revocado). «Empresas del número» = la dueña + las que lo usan con préstamo activo:

1. Si hay una conversación **no resuelta** de ese teléfono en ese número en una empresa del
   número, sigue ahí: la de la dueña si la hay; si no, la de actividad más reciente del cliente
   (su último mensaje). Motivo `conversacion_abierta`.
2. Si no, si una empresa del número **empezó** la conversación en las últimas 24 h (un aviso
   enviado a ese teléfono por ese número, o una plantilla mandada por una persona), va a esa
   empresa (la más reciente). Motivo `iniciada_por_empresa`.
3. Si no, se busca el teléfono en los clientes de cada empresa del número que lo usa para
   conversaciones (dueña + préstamos con `soporte`, `cobros` o `ventas`): instantánea de Argos
   (`telefono`, `movil`, `contacto_telefono`) o contactos de Iris. **Una sola** empresa coincide →
   esa empresa, en la cola del préstamo (o la del número si es la dueña). Motivo
   `cliente_de_empresa`. **Ninguna** o **varias** → la dueña (motivos `sin_coincidencia`,
   `varias_coincidencias`).

**Lo mandado desde el teléfono** (ecos de un número QR compartido) solo sigue al paso 1; sin
conversación abierta es de la dueña (el teléfono es suyo), nunca por los pasos 2 y 3.

**Iniciar sobre una conversación ajena:** en un número compartido, `POST /conversaciones/iniciar`,
`POST /conversaciones/{id}/reabrir` y `POST /conversaciones/{id}/mensajes` en una conversación
resuelta o con plantilla responden 409 `conversacion_en_otra_empresa` si otra empresa del número
tiene una conversación no resuelta con ese teléfono (y `Destinatario.motivo` lo dice). Así nadie le
quita a otra empresa una conversación abierta. El mensaje rechazado no queda guardado ni cuenta como
inicio de la conversación.

**Adjuntos:** al responder con `adjuntoId` y al registrar una boleta, el adjunto debe ser de la
empresa de la conversación (si no, 422 `adjunto_no_listo`): los de las notas que se quedaron con la
dueña al pasar una conversación no se pueden usar.

Cuando la regla elige empresa (pasos 2 y 3) se emite `conversacion.empresa_asignada`
`{ conversacion_id, empresa_id, canal_id, motivo, anterior_empresa_id (o null) }`. El paso 1 no
emite nada.

**A qué empresas se puede pasar** — `GET /conversaciones/{id}/empresas-destino`
(`conversaciones.transferir`, el mismo permiso que pasar; lectura sin auditar; quien no ve la
conversación, 404) → `{ data: [{ empresaId, nombre }], nextCursor: null }`: las empresas con
préstamo activo del número y un uso de conversación (`soporte`, `cobros` o `ventas`). `[]` si la
conversación no es de la dueña del número o no hay ninguna. El panel no necesita
`canales.configurar` para mostrar «Pasar a otra empresa».

**Pasar a otra empresa** — `POST /conversaciones/{id}/pasar-empresa` `{ version, empresaId, nota? }`
(`conversaciones.transferir`; solo en la empresa **dueña** del número y solo a una empresa con
préstamo activo de ese número que tenga un uso de conversación; si no, 422 `empresa_no_valida`).
La conversación **pasa** a esa empresa: queda en la cola del préstamo, sin asignar, sin etiquetas,
sin participantes y sin contacto (cliente, contacto de Iris y prospecto son de la dueña). Pasan
**solo** los mensajes del cliente y los salientes, con sus adjuntos (su `empresa_id` pasa a la otra
empresa: solo los de esos mensajes). Las notas internas previas de la dueña, sus menciones y sus
eventos de sistema **no** pasan: siguen guardados con la dueña y la otra empresa no los ve (los
mensajes se listan por la empresa de la conversación). `nota` (1–1000), escrita para quien la
recibe, queda como su primera nota interna, y el evento de sistema `pasada_a_empresa` (`detalle` =
nombre de la empresa dueña, sin `actorNombre`) dice «Pasada por <empresa dueña>». Desde ahí la ve
**solo** la otra empresa → `200
ConversacionCliente` (como la ve ahora quien la pasó: `soloLectura`, ya no la puede abrir). 409
`conversacion_existente` si esa empresa ya tiene una conversación de ese teléfono en ese número;
422 `conversacion_con_vinculos` si tiene tickets, órdenes, prospectos, boletas o promesas. Evento
`conversacion.empresa_asignada` (motivo `pasada`, `anterior_empresa_id`).

**Visibilidad:** una conversación es de **una** empresa (`empresa_id`). Quien usa un número
prestado nunca ve las conversaciones de la dueña por ese número, ni al revés, salvo las pasadas
(que cambian de empresa). Lista, búsqueda, contadores, detalle, mensajes, `/cambios` y el SSE
filtran por la empresa de la conversación.

**Revocar** — efectos, en la misma transacción:

- el préstamo queda `revocado` (sin `avisosEmpresa`);
- las conversaciones **no resueltas** de esa empresa por ese número se **resuelven** (evento de
  sistema `prestamo_revocado`, outbox `conversacion.resuelta` con `actor_id: null`) y quedan de
  **solo lectura** para ella: `ConversacionCliente.soloLectura: true` (también en el detalle), y responder, anotar, tomar,
  asignar, pasar, transferir, etiquetar, resolver, reabrir o asociar responden 422
  `conversacion_solo_lectura`. Su historial se sigue viendo;
- si esos clientes vuelven a escribir, llegan a la **dueña** (la empresa ya no es del número);
- sus avisos en cola por ese número que no salieron fallan (`aviso.fallido`, `rechazado_por_canal`,
  detalle `prestamo_revocado`); lo ya enviado no cambia;
- se puede volver a prestar después (`POST /canales/{id}/prestamos`).

**Campos nuevos**: `Canal.prestadoPor`, `Canal.prestamo`, `Canal.compartidoCon` (cuántas empresas
lo usan con préstamo activo; 0 si no está compartido), `NumeroConversaciones.prestadoPor`,
`ConversacionCliente.soloLectura` (solo viene, en `true`, cuando aplica; ausente = `false`).

### 15.4 Errores nuevos

| Código | Status | Cuándo |
| --- | --- | --- |
| `conexion_gestionada` | 422 | Cambiar la conexión en modo `saas` |
| `conexion_meta_sin_configurar` | 422 | QR o probar sin conexión `propia`/`saas` completa |
| `servicio_central_no_disponible` | 422 | QR con la conexión en modo `compartida` |
| `codigo_vencido` | 422 | Canje del código rechazado o código ya usado |
| `app_no_aprobada` | 422 | La app de Meta no tiene permiso para el QR |
| `numero_no_elegible` | 422 | Meta no deja conectar ese número con QR |
| `error_meta` | 502 | Otra respuesta de Meta en el QR o al sincronizar |
| `no_es_coexistencia` | 422 | Sincronizar un número que no es QR |
| `sincronizacion_ya_pedida` / `sincronizacion_vencida` | 422 | Sincronizar dos veces o después de 24 h |
| `numero_qr` | 422 | Editar token, app secret, `phoneNumberId` o `numero` de un número QR |
| `empresa_no_valida` | 422 | Prestar o pasar a una empresa que no puede |
| `canal_inactivo` | 422 | Prestar un número inactivo |
| `uso_no_permitido` | 422 | El préstamo no permite ese uso (plantilla, iniciar, número de avisos) |
| `conversacion_solo_lectura` | 422 | Acción sobre una conversación de un préstamo revocado |
| `conversacion_existente` | 409 | Pasar a una empresa que ya tiene esa conversación |
| `conversacion_en_otra_empresa` | 409 | Iniciar, reabrir, o responder en una resuelta o con plantilla, en un número compartido con un teléfono que tiene una conversación abierta en otra empresa |
| `base_url_requerida` | 422 | QR en modo `saas` o `compartida` sin `IRIS_BASE_URL` absoluta |
| `conversacion_con_vinculos` | 422 | Pasar una conversación con vínculos |

### 15.5 Eventos nuevos (outbox)

`meta.conexion_cambiada`, `meta.conexion_probada`, `canal_mensajeria.desconectado`,
`canal_mensajeria.reconectado`, `canal_mensajeria.prestado`, `canal_mensajeria.prestamo_revocado`,
`conversacion.empresa_asignada`. Cambian: `canal_mensajeria.creado` suma `tipo_conexion`;
`conversacion.creada` suma `origen: telefono`; `conversacion.mensaje_enviado` suma `origen`
(`telefono` | null); `contacto_iris.creado` suma `origen` (`agente` | `telefono`). Detalle en
`contratos/eventos.md`.

## 16. Asistente de IA que sugiere respuestas (fase 2, aprobado por el PM)

Lo implementa I1 en el servicio de mensajería (`apps/api/internal/mensajeria/asistente*.go`,
schema `mensajeria`). Lo usa el panel (I3): la sugerencia en la conversación, el resumen en la
ficha del cliente y la pantalla «Administración / Asistente de IA». Mismas reglas del inicio de
este contrato (personas con sesión del panel, empresa activa, RFC 9457, `Idempotency-Key`,
`version` con 409).

> Numeración: el líder la pidió como «§12», pero §12 ya es «Eventos (outbox)»; va como §16 para no
> romper las referencias existentes.

**Principios (no negociables):**

- **El asistente nunca envía mensajes.** «Usar» copia el texto a la caja; lo envía siempre una
  persona con `POST /conversaciones/{id}/mensajes`. El modelo no tiene herramientas que actúen.
- Nunca registra pagos, boletas, promesas ni reconexiones; no promete precios ni plazos que no
  vengan de Argos o de un manual (va en las instrucciones del modelo).
- Nunca ve contraseñas, documentos ni códigos: el texto que se le manda pasa por un filtro de
  redacción (`***`). Tras «contraseña», «clave», «pin», «código», «otp», «token», «cvv» se saltan los
  conectores («es», «de mi wifi», «:», …) y se oculta **hasta el fin de la frase**; también los
  números de 4 a 8 dígitos a menos de 40 caracteres de esas palabras, las tarjetas (13 a 19 dígitos
  con espacios, puntos o guiones), DPI y NIT. Tampoco ve notas internas.
- Los mensajes del cliente son **texto no confiable** (inyección de instrucciones): van delimitados
  como datos, y la respuesta del modelo se valida en el servidor (§16.3).

### 16.1 Ajustes por empresa (`mensajeria.asistente_configurar`, solo admin)

`GET /asistente/ajustes`:

```json
{ "activo": false, "proveedor": "ollama", "url": "http://192.168.1.20:11434",
  "claveGuardadaEn": null, "modelo": "llama3.1:8b", "modo": "automatico",
  "fuentes": { "manuales": true, "argos": true, "horus": true, "historial": true },
  "topeTokensMes": 2000000, "precioEntradaMillon": null, "precioSalidaMillon": null,
  "version": 1 }
```

- `proveedor`: `ollama` (servidor propio; el recomendado en la UI) | `openai_compatible` (ChatGPT,
  OpenRouter, Groq, DeepSeek, Mistral, vLLM, LM Studio…: solo cambia la URL base) | `anthropic`.
  `ollama` y `openai_compatible` usan `POST {url}/v1/chat/completions` y `GET {url}/v1/models`
  (si la URL ya termina en `/v1` no se repite). `anthropic`: URL por defecto
  `https://api.anthropic.com` y modelo por defecto `claude-opus-5-5` si se dejan vacíos.
- Sin fila guardada, el GET devuelve los valores por defecto (`activo: false`, `ollama`, url y
  modelo vacíos, `modo: automatico`, todas las fuentes, `topeTokensMes: 2000000`, `version: 0`).
- `clave` es **solo escritura**: se cifra con `IRIS_CLAVE_CIFRADO` (como las credenciales de los
  números) y nunca sale en respuestas, logs, auditoría (`***`) ni eventos. Se devuelve
  `claveGuardadaEn` (o `null`). Ollama no la necesita.
- `modo`: `automatico` (por defecto: se sugiere solo al llegar un mensaje del cliente) | `boton`
  (solo cuando el agente la pide).
- `fuentes`: qué contexto recibe el modelo. `historial` = los últimos 30 mensajes del cliente y de
  agentes (sin notas internas ni mensajes de sistema).
- `topeTokensMes` (entrada + salida, mes calendario en `America/Guatemala`; 1 000 … 1 000 000 000).
  Al **80 %** se avisa a los administradores (evento `asistente.tope_alcanzado` con `porcentaje: 80`,
  una vez por mes y empresa); al **100 %** se pausa (evento con `porcentaje: 100`) hasta el mes
  siguiente o hasta que suban el tope. En pausa no se generan sugerencias ni resúmenes
  (`tope_alcanzado`). **2.1:** los dos umbrales avisan a quienes tienen
  `mensajeria.asistente_configurar` en esa empresa por la **campana del panel** y por **correo**
  (§16.9), una sola vez por umbral, mes y valor del tope.
- `precioEntradaMillon` / `precioSalidaMillon` (opcionales, por millón de tokens, en la moneda que
  el admin quiera; decimal como texto, p. ej. `"3.00"`, o `null`): con los dos, el uso se muestra
  también en dinero.

`POST /asistente/ajustes/actualizar` con los mismos campos (sin `claveGuardadaEn`) más `clave?`
(solo escritura; `""` la borra; ausente la deja) y `version` (0 la primera vez). → los ajustes.
Activar exige `url` (salvo `anthropic`) y `modelo` (salvo `anthropic`), y clave guardada si el
proveedor es `anthropic` (422 `sin_configurar`). Evento `asistente.ajustes_cambiados` (campos[]; la
clave aparece como `clave_guardada`, nunca su valor). Cambiar el tope recalcula los avisos del 80 % y
100 % del mes con el tope nuevo.

**La clave no viaja a otro servidor:** si hay clave guardada y cambia el `proveedor` o la URL
normalizada (sin barra final; en OpenAI/Ollama sin `/v1`; Anthropic vacía = la oficial) y el cuerpo
no trae `clave`, responde **422 `clave_requerida`** (campo `clave`): hay que escribirla otra vez o
mandar `""` para quitarla. Vale para todos los proveedores, Ollama incluido.

**URL (la pone un admin):** solo `http`/`https`, sin usuario/contraseña ni parámetros. Se permiten
IP privadas y de la LAN (Ollama suele estar ahí). Se bloquean siempre link-local y metadata
(`169.254.0.0/16`, `fe80::/10`, `fd00:ec2::254`, `100.100.100.200`, `168.63.129.16`), NAT64
(`64:ff9b::/96`), Teredo (`2001::/32`), IPv4-compatible (`::/96`), 6to4 (`2002::/16`) con una IPv4
embebida no permitida, `0.0.0.0`/`::` y multicast. **Loopback** (`127.0.0.0/8`, `::1`, `localhost`)
está bloqueado salvo con `IRIS_ASISTENTE_PERMITIR_LOOPBACK=true` (Ollama en el mismo host). Se revisa
al guardar y **en cada conexión** (después de resolver el DNS). **No se sigue ninguna redirección**:
un 3xx es el error `proveedor_redirige`. **Con clave, la URL debe ser `https://`**, salvo una IP
privada (o loopback permitido). Error: 422 `url_no_permitida` (campo `url`).

**Tiempos:** conexión 10 s; respuesta completa hasta 90 s (Ollama puede tardar 20–40 s); `probar` y
`modelos` 30 s.

`POST /asistente/probar` `{ proveedor, url, clave?, modelo }` (sin `clave` usa la guardada **solo si
el proveedor y la URL son los guardados**: la clave nunca viaja a otro servidor) → 200
`{ "estado": "ok" | "error", "latenciaMs": 1840, "tokensPorSegundo": 23.5 | null, "detalle": "…" }`.
Manda un pedido corto real (no cuenta para el tope). No guarda nada; audita sin la clave. **Cualquier
falla** (conexión rechazada, tiempo agotado, DNS, 4xx/5xx, redirección, respuesta que no es de IA)
responde lo mismo: `estado: "error"`, `latenciaMs: 0`, `tokensPorSegundo: null` y `detalle`:
«No se pudo usar ese servidor como proveedor de IA: revisa la URL, la clave y el modelo» (así no
sirve para escanear puertos). La URL inválida sí es 422 `url_no_permitida` antes de conectar.

`GET /asistente/modelos` (sin parámetros: **solo lo guardado**) y
`POST /asistente/modelos` `{ proveedor, url, clave? }` (otra URL o clave; con sesión y **CSRF**, la
clave guardada con la misma regla que `probar`) → `{ data: [{ id, nombre }] }`. Si el proveedor
falla: 502 `proveedor_error` con el mismo `detail` único de `probar`.

`GET /asistente/uso?mes=YYYY-MM` (por defecto el mes actual) →
`{ mes, tokensEntrada, tokensSalida, sugerencias, usadas, utiles, noUtiles, resumenes,
costo: "12.40" | null, topeTokensMes, porcentaje, pausado }`. `costo` solo con los dos precios.

### 16.2 Sugerencias (`conversaciones.sugerencias`)

Roles: los que hoy responden conversaciones (`despacho`, `soporte`, `ventas`, `supervisor`, `admin`). Solo
sobre conversaciones que ves (si no, 404).

```json
{ "id": "0193…", "conversacionId": 41, "paraMensajeId": "0193…",
  "estado": "lista",            // generando | lista | error | descartada | usada | vencida
  "texto": "Hola Carlos, vemos una falla en tu zona…",   // null mientras genera o con error
  "fuentes": [ { "tipo": "horus", "etiqueta": "Falla en Zona 4 · Horus", "entidadId": "router:1:77" },
               { "tipo": "argos", "etiqueta": "Servicio activo, al día · Argos", "entidadId": "41010" },
               { "tipo": "manual", "etiqueta": "Manual «Sin internet (FTTH)»", "entidadId": "12" } ],
  "accion": { "tipo": "etiquetar", "etiquetaId": 3, "etiqueta": "Sin servicio" },  // o ausente
  "error": { "codigo": "proveedor_no_responde", "detalle": "El servidor de IA no respondió a tiempo" }, // o ausente
  "calificacion": "util",       // util | no_util | null
  "origen": "automatico",       // automatico | boton | otra
  "creadaEn": "…", "actualizadaEn": "…" }
```

- `accion` y `error` van **ausentes** cuando no aplican (huma no publica objetos anulables).
  `fuentes` siempre es lista (`[]`).
- `tipo` de fuente: `manual` | `argos` | `horus` | `historial`. `entidadId` es texto (id del
  manual, del servicio, clave del estado de Horus) o `null`.
- **2.1, saldo del cliente (§16.10):** con la fuente `argos` y estado de cuenta leído en vivo, la
  fuente del primer servicio (el de la conversación) dice «Servicio activo, al día · Argos» (o el
  estado del servicio: «Servicio cortado, al día · Argos») o «Con saldo vencido (2 facturas) · Argos»
  («(1 factura)»; sin número de facturas: «Con saldo vencido · Argos»). **Nunca lleva el monto**, y el
  texto sugerido tampoco (el monto no se le da al modelo; lo escribe el agente desde la ficha). Si
  Argos no responde, la fuente queda como antes («Servicio activo, plan 20 Mbps · Argos»).
- `accion` por ahora solo `etiquetar` con una **etiqueta activa existente de la empresa**. El panel
  la ofrece como botón; aplicarla es la acción normal de etiquetar de la persona (el asistente no la
  aplica).
- Códigos de `error`: `proveedor_no_responde` (sin conexión, tiempo agotado, 5xx o atascada más de
  90 s + 2 min), `proveedor_error` (respuesta inesperada, 404, 429, otro 4xx), `clave_invalida`
  (401/403), `proveedor_redirige` (3xx: no se siguen), `tope_alcanzado`, `sin_configurar`,
  `rechazo_modelo` (el modelo, y su respaldo en Anthropic, se negó), `respuesta_invalida` (no pasó la
  validación de §16.3), `url_no_permitida` (el servidor resolvió a una dirección bloqueada).
- Los manuales del equipo (`interna`) pueden salir como fuente para el agente con la etiqueta
  «Manual del equipo «…»»; el modelo tiene la instrucción de no mencionarlos al cliente.

| Método y ruta | Qué hace |
| --- | --- |
| `GET /asistente/estado` | `{ activo: boolean, modo: "automatico" \| "boton", pausado: boolean }` de la empresa activa, para decidir si se muestra «Sugerir». Nada de proveedor, URL, clave ni uso. Sin ajustes guardados: `{ activo: false, modo: "automatico", pausado: false }` |
| `POST /conversaciones/{id}/sugerencias` `{ otra?: boolean }` | Pide una (también «Otra»). → **202** con la `Sugerencia` en `generando`. La vigente (generando/lista/error) queda `descartada` y **su generación en curso se corta**; `otra: true` marca el origen `otra` (si no, `boton`). Funciona en modo `automatico` y `boton`. 422 `asistente_inactivo`, `tope_alcanzado`, `sin_mensaje` (no hay mensaje del cliente), `conversacion_resuelta`; **429** `demasiados_pedidos` / `demasiadas_sugerencias` con `Retry-After` (§16.2.1). Con `Idempotency-Key` la repetición devuelve la misma sugerencia |
| `GET /conversaciones/{id}/sugerencias/actual` | La vigente (`generando`, `lista` o `error`) o 404 `sin_sugerencia` |
| `POST /sugerencias/{id}/usar` | Marca `usada` (el panel copia `texto` a la caja). Solo desde `lista` (si no, 422 `sugerencia_no_vigente`; repetir sobre `usada` es 200 sin cambios). Evento `asistente.sugerencia_usada` con `editada: null` (todavía no se envió) |
| `POST /sugerencias/{id}/descartar` | `descartada` (ocultar). Desde `generando` (corta la llamada al proveedor), `lista` o `error`; repetir: 200 sin cambios |
| `POST /sugerencias/{id}/calificar` `{ util: boolean }` | 👍/👎; se puede cambiar. Evento `asistente.sugerencia_calificada` |

Las tres acciones sobre `/sugerencias/{id}` devuelven la `Sugerencia` y dan 404 si no ves su
conversación o si la sugerencia es de otra empresa (la conversación se pasó con un préstamo, §15.3).

**Préstamo de números:** pasar la conversación a otra empresa (`pasar-empresa`) deja `vencida` la
sugerencia vigente en la misma transacción. `GET …/sugerencias/actual` y el feed solo muestran
sugerencias de la empresa actual de la conversación. Si al preparar o al guardar una generación la
conversación ya es de otra empresa, la sugerencia queda `vencida` sin llamar al proveedor y sin
cobrar uso a ninguna empresa.

#### 16.2.1 Límites de costo y frecuencia

| Límite | Valor | Respuesta |
| --- | --- | --- |
| Pedidos por persona (sugerencia con botón, «Otra» y resumen) | 10 por minuto | 429 `demasiados_pedidos` con `Retry-After` |
| Sugerencias por conversación (de cualquier origen) | 20 por hora | 429 `demasiadas_sugerencias` con `Retry-After` (el automático no genera) |
| Sugerencias automáticas por conversación | 6 por hora | el automático no genera (sin error) |
| Reserva del tope | entrada estimada (3 caracteres por token) + 1 024 de salida | si no cabe, la sugerencia queda `error` / `tope_alcanzado` y el resumen 422 `tope_alcanzado`, sin llamar al proveedor |

La reserva es atómica (`usados + reservados + estimado ≤ tope`) y se ajusta al terminar: se libera lo
reservado y se suma lo real. Una reserva que quedó colgada se libera en el mantenimiento.

**Al responder:** `POST /conversaciones/{id}/mensajes` acepta `sugerenciaId` opcional (de esa
conversación, en `lista` o `usada`; si no, 422 `sugerencia_no_vigente`). Marca la sugerencia `usada`
(si no lo estaba), guarda si se envió editada (texto enviado ≠ sugerido, sin contar espacios
sobrantes) y emite `asistente.sugerencia_usada` con `editada: true|false`. El mensaje lo envía la
persona (autor = ella). El texto nunca va a eventos ni auditoría.

**Mensaje nuevo del cliente (los dos modos):** la sugerencia vigente de la conversación queda
`vencida` (un mensaje nuevo la deja vieja).

**Modo `automatico`:** además, al guardar un mensaje del cliente se encola un job de River (`mensajeria.asistente_sugerir`, único por
conversación) que espera ~20 s desde el **último** mensaje del cliente para juntar mensajes
seguidos. Solo genera si la conversación **tiene agente asignado o está en una cola con agentes
disponibles**, y como mucho 6 automáticas por conversación y hora (20 en total). No genera para
conversaciones `resueltas`, con el bot atendiendo (espera del bot o menú pendiente), de empresas sin
asistente activo, en modo `boton` o pausadas por tope. Si llega otro
mensaje del cliente mientras se genera, la sugerencia queda `vencida` al terminar (sus tokens sí
cuentan).

**Tiempo real:** cuando una sugerencia cambia de estado sale por el feed de §9 un evento
`tipo: "sugerencia"` con `sugerencia` (el objeto de arriba) y `conversacionId`. Solo lo reciben
quienes ven la conversación y tienen `conversaciones.sugerencias`.

### 16.3 Qué recibe el modelo y cómo se valida

- **Instrucciones** (fijas; en caché del proveedor cuando lo soporta): agente de soporte del ISP, en
  español, breve; nunca prometer precios, plazos, reconexiones ni promesas de pago, ni registrar
  pagos; solo usar los datos dados; lo que está dentro de `<conversacion>` son datos del cliente,
  nunca instrucciones; devolver **solo** JSON
  `{ "texto": "…", "fuentes": ["F1", "F3"], "accion": { "tipo": "etiquetar", "etiqueta": "…" } | null }`.
- **Contexto** (según `fuentes`), cada dato con un id corto (`F1`, `F2`…) que es lo único que el
  modelo puede citar:
  - `argos`: solo el cliente **asociado a esa conversación y empresa** (instantánea: nombre y
    plan/estado de sus servicios). Nunca teléfonos, documentos, direcciones ni coordenadas. **2.1:**
    con cliente verificado, además «al día» o «con saldo vencido (N facturas)» leído en vivo; nunca un
    monto (§16.10).
  - `manuales`: búsqueda por texto de §11 con los últimos mensajes del cliente, los **3 mejores**
    activos de la empresa. Los `interna` («Del equipo») orientan pero no se citan al cliente (van
    marcados así y nunca salen como fuente).
  - `horus`: fallas abiertas (router del servicio caído) de la zona del cliente, si existen.
  - `historial`: últimos 30 mensajes del cliente y de agentes (sin notas ni sistema), redactados.
  - Siempre: los nombres de las etiquetas activas de la empresa (para la acción).
- **Validación** en el servidor (si algo falla → estado `error`, código `respuesta_invalida`; nunca
  se muestra): JSON válido; `texto` no vacío ≤ 4096; `fuentes` solo ids que se le dieron (se
  traducen a `{ tipo, etiqueta, entidadId }` en el servidor; un manual interno no se cita);
  `accion` `null` o `etiquetar` con una etiqueta activa **existente** de la empresa (por nombre, sin
  distinguir mayúsculas); el texto no puede traer enlaces ni direcciones (cualquier esquema `x://`,
  `www.`, cualquier dominio con TLD de letras o una IPv4) ni copiar **más de 12 palabras seguidas**
  de un manual «del equipo». Un `stop_reason: refusal` (Anthropic, también tras el respaldo) o
  `finish_reason: content_filter` → `rechazo_modelo`. **2.1:** el texto nunca puede traer un monto de deuda,
  con cliente verificado o sin él (§16.10).

### 16.4 Resumen de la conversación (ficha del cliente)

`POST /conversaciones/{id}/resumen` (`conversaciones.sugerencias`) → 200
`{ "texto": "…", "generadoEn": "…" }`. Síncrono (hasta 90 s; el panel muestra «Generando…»). Usa el
historial (últimos 30, sin notas) y lo de Argos según `fuentes`. Reserva y cuenta para el tope; el
uso se registra aunque quien lo pidió cierre la pantalla. No se guarda el texto. 422
`asistente_inactivo`, `tope_alcanzado`, `sin_mensaje`; 429 `demasiados_pedidos`; 502
`proveedor_no_responde` / `proveedor_error` / `clave_invalida` / `proveedor_redirige` / `rechazo_modelo`.

### 16.5 Permisos nuevos

- `mensajeria.asistente_configurar` (sensible: solo `admin`): ajustes, probar, modelos y uso.
- `conversaciones.sugerencias`: `despacho`, `soporte`, `ventas`, `supervisor`, `admin` (los que responden). También da `GET /asistente/estado`.

### 16.6 Errores nuevos

| Código | Status | Cuándo |
| --- | --- | --- |
| `asistente_inactivo` | 422 | La empresa no tiene el asistente activo |
| `sin_configurar` | 422 | Activar sin URL, modelo o clave necesaria |
| `url_no_permitida` | 422 | URL con esquema distinto de http/https, credenciales, parámetros, dirección bloqueada (§16.1) o http con clave a un host que no es IP privada |
| `clave_requerida` | 422 | Cambiar proveedor o URL con clave guardada sin mandar `clave` |
| `demasiados_pedidos` / `demasiadas_sugerencias` | 429 | Límites de §16.2.1 (con `Retry-After`) |
| `tope_alcanzado` | 422 | Se llegó al tope de tokens del mes |
| `sin_mensaje` | 422 | Pedir sugerencia o resumen sin mensaje del cliente |
| `conversacion_resuelta` | 422 | Pedir sugerencia en una conversación resuelta |
| `sin_sugerencia` | 404 | No hay sugerencia vigente |
| `sugerencia_no_vigente` | 422 | Usar una sugerencia que no está `lista`, o `sugerenciaId` de otra conversación |
| `proveedor_no_responde` / `proveedor_error` / `clave_invalida` / `proveedor_redirige` / `rechazo_modelo` | 502 | Resumen cuando el proveedor falla (modelos: siempre `proveedor_error`) |

### 16.7 Notas de implementación

- Tablas: `mensajeria.asistente_ajustes` (una por empresa; `clave_cifrada` rota con `iris clave
  rotar`), `mensajeria.asistente_uso` (por empresa y mes, con `tokens_reservados`),
  `mensajeria.asistente_reservas` (reservas en curso), `mensajeria.asistente_pedidos` (límite por
  persona) y `mensajeria.sugerencias` (el texto solo vive aquí). El feed suma
  `conv_eventos.sugerencia_id` y el tipo `sugerencia`.
- Mantenimiento (cada minuto): una sugerencia en `generando` más de 90 s + 2 min pasa a `error`
  `proveedor_no_responde`; las reservas colgadas se liberan; los pedidos de más de un día se borran.
- Cancelación: mientras el proveedor responde, la generación mira cada 2 s si la sugerencia sigue en
  `generando`; si la descartaron («Otra», descartar, vencida) corta la llamada.
- Jobs de River (cola `mensajeria`): `mensajeria.asistente_generar` (botón/«Otra», único por
  sugerencia) y `mensajeria.asistente_sugerir` (automático, único por conversación mientras espera o
  corre; se pospone con `JobSnooze` hasta 20 s después del último mensaje del cliente).
- Las respuestas de `…/resumen` y de `/sugerencias/{id}/usar|descartar|calificar` no se guardan en
  la tabla de idempotencia (traen texto del modelo); repetirlas responde 200 sin cambios.
- Aviso a administradores al 80 %/100 % (2.1): el worker consume `asistente.tope_alcanzado` y crea
  los avisos de la campana y los correos (§16.9, `internal/notificaciones`). `pausado` sigue en
  `GET /asistente/estado` y `/asistente/uso`.
- Anthropic: SDK oficial de Go (`github.com/anthropics/anthropic-sdk-go` v1.84.0; la v2 exige Go 1.25),
  endpoint beta con el **respaldo del lado del servidor activado por defecto** (beta
  `server-side-fallback-2026-07-01`, `fallbacks: "default"`: si el modelo declina, la API reintenta
  en el modelo de respaldo dentro de la misma llamada), sin `temperature`/`top_p`, instrucciones
  fijas en `system` con `cache_control`; si el respaldo también declina (`stop_reason: refusal`) →
  `rechazo_modelo`. El cliente no lee variables `ANTHROPIC_*` del servidor.

### 16.8 Eventos nuevos (outbox, nunca el texto ni la clave)

`asistente.ajustes_cambiados` (empresa_id, campos[]), `asistente.sugerencia_creada` (sugerencia_id,
conversacion_id, empresa_id, origen `automatico|boton|otra`, proveedor, modelo, tokens_entrada,
tokens_salida, estado `lista|error|vencida|descartada`, error (código o null); se emite al terminar
de generar), `asistente.sugerencia_usada` (sugerencia_id, conversacion_id, empresa_id, editada
`true|false|null`), `asistente.sugerencia_calificada` (sugerencia_id, conversacion_id, empresa_id,
util), `asistente.tope_alcanzado` (empresa_id, porcentaje `80|100`, mes, tokens, tope). Detalle en
`contratos/eventos.md`.

**2.1 (notificaciones):** `notificaciones.avisos_creados` (tipo `asistente_tope`, clave, empresa_id,
usuarios[], correo `enviar|omitido`), `notificaciones.correo_enviado` / `notificaciones.correo_fallido`
(correo_id, usuario_id, tipo, clave, motivo o null).

### 16.9 Avisos del panel y correo del tope (entrega 2.1, decisión del PM: «pueden ser ambos»)

Lo implementa I1 en `internal/notificaciones` (API principal, no el servicio de mensajería: van sin
el prefijo `/mensajeria`). Lo usa el panel (I3) en la campana de la barra superior.

**Cuándo:** el worker consume `asistente.tope_alcanzado` (80 y 100). Destinatarios: personas activas
con `mensajeria.asistente_configurar` (por un rol activo) que operan en esa empresa (la tienen
asignada o tienen `empresas.todas` sin límite). **Una sola vez por umbral, mes y valor del tope** y por usuario
(clave `asistente_tope:<empresa>:<YYYY-MM>:<80|100>:<tope>`, única por usuario): un evento repetido
no vuelve a avisar. Si suben el tope dentro del mes y lo vuelven a cruzar, la clave es otra y se
avisa de nuevo (campana y correo). `enlace` es siempre una ruta interna del panel (la base lo exige:
`^/[A-Za-z0-9_\-/?=&#.]*$` y nunca `//…`).

**Textos (decisión del PM):**

- 80 %: «El asistente de IA de <empresa> usó el 80 % de su tope del mes» (`nivel: "alerta"`).
- 100 %: «El asistente de IA de <empresa> se pausó: llegó a su tope del mes» (`nivel: "peligro"`).
- Los dos con `enlace: "/administracion/asistente"`. `<empresa>` = nombre de la empresa en Iris
  («tu empresa» sin conector).

**Campana** (persona con sesión del panel o token propio; solo los suyos **de la empresa activa**):

| Método y ruta | Qué hace |
| --- | --- |
| `GET /api/v1/yo/avisos?cursor=&limit=&soloNoLeidos=` | `{ data: [AvisoPanel], nextCursor }`, del más nuevo al más viejo |
| `GET /api/v1/yo/avisos/resumen` | `{ "noLeidos": 2 }` (el punto de la campana) |
| `POST /api/v1/yo/avisos/{id}/leer` | → `AvisoPanel` con `leido: true` (repetir no cambia `leidoEn`; 404 si no es tuyo) |
| `POST /api/v1/yo/avisos/leer-todos` | → `{ "leidos": 3 }` |

```json
{ "id": 12, "tipo": "asistente_tope", "nivel": "alerta",
  "titulo": "El asistente de IA de Cable Norte usó el 80 % de su tope del mes",
  "enlace": "/administracion/asistente", "datos": { "porcentaje": 80, "mes": "2026-11", "tope": 2000000 },
  "leido": false, "leidoEn": null, "creadoEn": "…", "empresaId": 7 }
```

Marcar leído es el estado personal de la campana: no emite evento ni deja auditoría. Los avisos y los
correos se purgan a los 180 días.

**Correo:** el asunto es el mismo texto; el cuerpo (texto plano) suma el uso del mes («1,600,000 de
2,000,000 tokens»), qué pasa al 100 % y el enlace `<IRIS_BASE_URL>/administracion/asistente`. Sale por
el **correo del sistema** (`IRIS_SMTP_HOST`, `IRIS_SMTP_PORT`, `IRIS_SMTP_USUARIO`,
`IRIS_SMTP_CONTRASENA`, `IRIS_SMTP_TLS` `starttls|tls`, `IRIS_SMTP_REMITENTE`; TLS obligatorio y la
protección de red de los buzones, `IRIS_CORREO_REDES_PERMITIDAS` / `IRIS_CORREO_CA`) con un job por
correo (hasta 6 intentos con espera exponencial). **Sin `IRIS_SMTP_HOST` o sin
`IRIS_SMTP_REMITENTE` se omite sin error y queda registrado** (`notificaciones.correos` con
`estado: omitido`, `motivo: sin_correo_sistema`; `sin_correo` si el usuario no tiene correo); la
campana sale igual.

### 16.10 Saldo del cliente en las sugerencias (entrega 2.1, decisión del PM y del líder)

**Regla (revisión de seguridad de la #155): el monto nunca llega al modelo ni aparece en una
sugerencia, con cliente verificado o sin él.** El agente lo ve en la ficha del cliente y lo escribe
él si quiere.

- Con la fuente `argos` activa y un cliente asociado a la conversación, al preparar cada sugerencia
  Iris lee **en vivo** `GET /api/v1/clientes/:id/estado-cuenta` de Argos (§2.1 del contrato con
  Argos; la empresa de la conversación en `?empresa_id=` y `X-Empresa-Id`; `X-Iris-Actor:
  asistente_ia`), fuera de la transacción y con 5 s de plazo. El servicio no trae saldo propio en lo
  que Iris lee de Argos; su `estado` (instantánea) se combina con el estado de cuenta de la empresa.
- **Qué se guarda y qué no.** De la respuesta de Argos Iris se queda solo con «al día» o «con saldo
  vencido» y la cantidad de facturas vencidas. Eso **sí se guarda** como etiqueta de la fuente de la
  sugerencia («Servicio activo, al día · Argos», «Con saldo vencido (2 facturas) · Argos»), que es
  interna: la ve el agente. **El monto y el saldo no se guardan en ningún lado ni se le pasan al
  modelo**: ni en la sugerencia (texto, fuentes, error), ni en eventos, auditoría o logs.
- **Si Argos falla** (sin conector, caído, sin permiso `clientes.estado_cuenta`), la sugerencia sale
  sin esa fuente (las de la instantánea quedan como antes); no se cae.
- **Cliente verificado** = la conversación tiene cliente y `contacto.verificado: true` (el número
  coincide con Argos o lo confirmó un supervisor, §5), la misma regla de boletas y promesas. La
  verificación de identidad de §7.5 del contrato con Argos aún no está en Iris (§12, pendiente):
  cuando exista, esta regla la exigirá.
- **Al modelo, con cliente verificado:** solo «al día» o «con saldo vencido (N facturas vencidas)».
  Puede decirle al cliente que tiene un saldo pendiente; si pide el monto, que un agente se lo confirma.
- **Al modelo, con cliente sin verificar:** nada del estado de cuenta; solo la instrucción de no
  hablar de saldos, deudas ni pagos pendientes y de ofrecer verificar su identidad. La etiqueta de la
  fuente (para el agente) sí dice el estado.
- **Validador de montos (siempre, verificado o no).** El texto sugerido se normaliza (NFKC, espacios
  Unicode e invisibles, sin `*`, `_`, `~` ni comillas invertidas, minúsculas) y la sugerencia queda
  `error` / `respuesta_invalida` («La sugerencia mencionaba un monto de deuda; el monto lo da el
  agente desde la ficha del cliente», sin el número) si habla de deuda (saldo, debe, deuda, adeuda,
  pendiente de pago, factura vencida, total a pagar, en mora…) y trae:
    - una cifra con moneda en cualquier parte del texto, con la moneda antes o después (`Q`, `GTQ`,
      `qtz`, `USD`, `$`, quetzal(es), dólar(es), peso(s); `Q350`, `350 Q`, `Q: 350`, «cien
      quetzales»), o
    - un número de 2 dígitos o más, un decimal o un numeral en letras de diez en adelante a menos de
      60 caracteres de la palabra de deuda («debe 1,250», «su saldo es de 150», «tu deuda es de
      mil»), salvo que cuente otra cosa (facturas, días, meses, Mbps, una fecha).
  Un precio de plan tomado de un manual («el plan de 100 Mb cuesta Q 299 al mes») no habla de deuda
  y pasa. Un texto que mezcla un precio con una palabra de deuda se rechaza (el agente pide otra).
- **La cuenta es del cliente que se leyó.** Si la conversación cambió de cliente mientras Argos
  respondía, el estado de cuenta leído no se usa. Si cambió mientras el modelo generaba, la
  sugerencia queda `vencida` (sin texto ni fuentes; sus tokens cuentan). Asociar otro cliente,
  desasociarlo, unir un contacto de Iris a un cliente o iniciar la conversación con otro cliente
  dejan `vencida` la sugerencia vigente en la misma transacción.
