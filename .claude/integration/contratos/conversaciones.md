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
puntos de extensión (§10.4).

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
- 409 `canal_existente` solo si el `phoneNumberId` ya está en otro número activo.
- **Varios números en la misma app de Meta.** Meta manda los webhooks de todos los números de una
  app a una sola URL, salvo que cada número tenga la suya: configura en Meta, por número,
  `override_callback_uri` = `…/api/v1/mensajeria/canales/{id}/webhook` (Graph API
  `POST /{waba_id}/subscribed_apps` con `override_callback_uri` y `verify_token`, o por número con
  `POST /{phone_number_id}` `webhook_configuration.override_callback_uri`). Si aun así llega a un
  canal un cambio de otro `phone_number_id`, Iris lo pasa al número activo de ese
  `phone_number_id` **solo** si es de la misma empresa y tiene el mismo `app_secret` con el que se
  verificó la firma; si no, lo descarta (queda anotado en el entrante).

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
| `sin_contacto` | 422 | Fase 1.1: el cliente no tiene teléfono en la instantánea de Argos (ni `movil` ni `telefono`) |
| `numero_no_permitido` | 422 | Fase 1.1: iniciar desde un número que no es de tus colas (o inactivo) |
| `no_es_tuya` | 403 | Fase 1.1: pasar una conversación que no tienes asignada sin ser supervisor (global o de su cola) |
| `mismo_agente` | 422 | Fase 1.1: pasar la conversación a quien ya la tiene |

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
- `GET /conversaciones/agentes?colaId=`: miembros, supervisores de la cola y supervisores globales;
  cada agente trae `colasSupervisadas[]` (`ColaRef`) y `disponibilidad` (§14.5).
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
  **reutiliza** y la plantilla va ahí: resuelta → se reabre en la cola del número; sin asignar o
  tuya → queda tuya; de otro agente y activa → 409 `conversacion_de_otro` (el `detail` dice quién;
  el panel ofrece abrirla o pedir «Pasar»). Con la ventana de 24 h abierta en la existente solo se
  revisa la baja.
- `GET /conversaciones/destinatario?canalId=&clienteId=|contactoIrisId=` (`conversaciones.iniciar`,
  lectura sin auditar) → `{ numero, puede, motivo, conversacionId }` para la pantalla «Nueva
  conversación» («Acepta mensajes», «No acepta mensajes», «Sin número de WhatsApp en Argos»).
  `motivo`: `null` | `sin_contacto` | `sin_permiso` | `baja` | `cliente_sin_confirmar` |
  `cliente_fuera_de_empresa` | `conversacion_de_otro` | `numero_no_permitido`; `numero` `null` si no
  hay; `conversacionId` si ya hay una de ese número en ese canal, si no `null`.
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
- Los cambios automáticos los hace el mantenimiento (cada minuto), emiten el mismo evento con su
  `origen` y no dejan fila de auditoría por agente; los manuales se auditan.

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
la guardia), pero lo manda el bot: si el bot está activo y tiene menú, el menú va después del
mensaje y sale `conversacion.bot_atendio` con `motivo: fuera_de_horario` (además de
`conversacion.fuera_de_horario`).

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
- Tomar, asignar, pasar o iniciar **cancelan** el temporizador y el menú. El mantenimiento procesa
  cada minuto todo lo vencido hasta ahora y vuelve a revisar con la fila bloqueada: un temporizador
  cancelado nunca dispara y uno que ya disparó no se repite. Una vez que el bot contestó, no vuelve a
  hacerlo en la misma espera; resolver, tomar o asignar empiezan una espera nueva.
- `ConversacionCliente.espera` (ausente si está asignada o resuelta):
  `{ disponibles: UsuarioRef[] (hasta 5), totalDisponibles, botContestaEn (o null),
  botMotivo (sin_confirmacion | sin_disponibles | null), botAtendio }` para «2 disponibles en
  Soporte», «Nadie disponible en Cobros» y «Bot en 12 min».
- `MensajeConversacion.automatico`: `"bot"` | `"fuera_de_horario"` | `null`.
- Eventos de sistema nuevos (`evento.tipo`): `aviso_sin_atender`, `bot_atendio`, `menu_elegido`,
  `pasada`, `iniciada`. `EventoSistema` suma `minutos` y `disponibles` (enteros, opcionales).
- Tipo nuevo del flujo (SSE y `/cambios`): `aviso_sin_atender` (solo a los avisados).

### 14.7 Eventos nuevos (outbox)

`conversacion.iniciada`, `conversacion.aviso_sin_atender`, `conversacion.bot_atendio`,
`conversacion.menu_elegido`, `agente.disponibilidad_cambiada` y
`conversaciones.ajustes_cambiados`; `conversacion.asignada` suma los orígenes `pasar` e `iniciar`
(con `pasar`, `disponible` y `nota_id`); `conversacion.creada` suma `origen` (`cliente` | `agente`).
Detalle en `contratos/eventos.md`.
