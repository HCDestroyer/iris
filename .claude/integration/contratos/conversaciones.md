# Contrato de la bandeja de conversaciones con clientes (fase 1, sin IA)

Lo implementa I1 en el servicio de mensajería (`apps/api/cmd/mensajeria`, schema `mensajeria`,
código en `apps/api/internal/mensajeria/conv_*.go`). Lo usa el panel (I3, «Conversaciones» y
«Administración / Números y canales»). Los tipos están en `packages/contratos` (operaciones
`*Conversacion*`, `*Cola*`, `*Etiqueta*`, `*RespuestaRapida*`, `*ContactoIris*`, `*Horario*`,
`*Manual*`): no los copies a mano.

Base: `/api/v1/mensajeria`. La API la reenvía tal cual al proceso de mensajería. Auth: cookie
`HttpOnly` del panel o `Authorization: Bearer`. Multiempresa: la empresa activa (`X-Iris-Empresa`
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
  `{ version, usos, avisosEmpresa, colaId, horarioId?, fueraDeHorario? }` con `canales.configurar`.
- 409 `canal_existente` solo si el `phoneNumberId` ya está en otro número activo.

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
`null`): huma no publica objetos anulables. Los escalares opcionales sí van `null`.

`contacto.tipo`: `cliente` (cliente de Argos: solo su id; nombre, servicio, saldo y zona salen de la
instantánea de lectura o de `GET /clientes/{id}` de la API, nunca de una copia), `contacto_iris`
(§5.2), `prospecto` o `desconocido`. `asociacion`: `automatica` (una sola coincidencia exacta del
número en la instantánea de Argos de la empresa al llegar el primer mensaje) o `manual`.

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
- `BAJA` / `ALTO` siguen dando de baja los avisos (§6.4) y además quedan como mensaje;
- si responde (`context.id`) a un aviso de ese número, o recibió un aviso de ese número en las
  últimas 72 h, se agrega el evento `respondio_aviso`;
- `perfilNombre` se toma de `contacts[].profile.name`.

Los estados de entrega (`sent`, `delivered`, `read`, `failed`) de los mensajes salientes actualizan
`estadoEntrega` (nunca retroceden); los de los avisos siguen igual que antes.

## 4. Quién ve qué (permisos y alcance)

| Permiso | Qué permite |
| --- | --- |
| `conversaciones.ver` | Ver las conversaciones de **sus colas**, las sin cola, las asignadas a él y las que lo mencionan; sus mensajes, contadores, cambios y el flujo SSE |
| `conversaciones.supervisar` | Ver todas las de la empresa (todas las colas) |
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
| `manuales.ver` / `manuales.editar` | Biblioteca de manuales (PR B) |

Crear ticket, orden o prospecto desde la conversación exige además el permiso del caso que se
reutiliza (`tickets.crear`, `ordenes.crear`, `prospectos.crear`).

Lo que no se ve responde **404**. Roles base:

| Rol | Permisos |
| --- | --- |
| `soporte` | ver, responder, tomar, transferir, etiquetar, resolver, asociar, registrar_boleta, registrar_promesa, manuales.ver |
| `ventas` | ver, responder, tomar, transferir, etiquetar, resolver, asociar, manuales.ver |
| `despacho` | ver, responder, tomar, etiquetar, resolver, manuales.ver |
| `supervisor` | todos los de arriba + supervisar, asignar, configurar, canales.configurar, manuales.editar |
| `lectura` | ver, manuales.ver |
| `admin` | todos |

Los usuarios de integración no reciben ninguno en esta fase.

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
- `POST /conversaciones/{id}/desasociar` `{ version }`: quita cliente, contacto de Iris y prospecto.

### 5.2 Contacto de Iris

Para quien no es cliente (proveedor, personal, otro) o aún no lo es. Es un dato **propio de
Iris**, no una copia de Argos: `nombre`, `tipo` (`proveedor`, `personal`, `prospecto`, `otro`),
`telefono` (E.164), `nota`, `clienteId` (null hasta unirlo), `version`.

| Método y ruta | Permiso | Qué hace |
| --- | --- | --- |
| `GET /contactos-iris?q=` | `conversaciones.ver` | Buscar por nombre o número |
| `POST /conversaciones/{id}/contacto-iris` | `conversaciones.asociar` | Crea el contacto con el número de la conversación y lo asocia `{ version, nombre, tipo, nota? }` |
| `POST /contactos-iris/{id}/actualizar` | `conversaciones.asociar` | Nombre, tipo, nota (`version`) |
| `POST /contactos-iris/{id}/unir-cliente` | `conversaciones.asociar` | `{ version, clienteId }`: se volvió cliente. Sus conversaciones pasan a `contacto.tipo = cliente` con ese id (evento `contacto_iris.unido`). El contacto queda como historial (`clienteId`, `unidoEn`) |

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
cliente o una que subió el agente). Exige cliente asociado (422 `sin_cliente`). Iris guarda la
boleta (`mensajeria.boletas`, uuid) y la manda a Argos en un job (`POST /comprobantes` multipart,
`Idempotency-Key = iris:boleta:<id>`, `X-Iris-Actor: usuario:<id>`, `X-Iris-Conversacion:
<conversacion id>`, `canal_contacto = whatsapp`, `leido_por = persona`), con reintentos a 1 min,
5 min, 30 min, 2 h y 12 h. Estados: `por_enviar` → `registrada` (Argos la aceptó, con
`comprobanteId`) | `fallida` (rechazo de negocio: `referencia_duplicada`, validación…, con el
`detalle` de Argos). La aprobación o el rechazo de caja (webhooks `comprobante.*`) se reflejan en
la conversación cuando lleguen (ver «Pendiente» en §12).

### 6.5 Promesa (contrato con Argos §7.4)

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
  supervisa todas las colas de la empresa; en la fase 1 no hay supervisor por cola.
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
| `POST /conversaciones/{id}/tomar` | `tomar` | `{ version }` | Solo si está sin asignar (409 `ya_asignada`). Tomar la propia: 200 sin cambios |
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

- Idempotente por `id` (mismo id y contenido → 200 con el mensaje actual; otro contenido → 409
  `mensaje_id_reutilizado`). 201 al crearlo. La respuesta no se guarda en el middleware (trae
  texto); la `Idempotency-Key` la aplica el caso.
- **Ventana de 24 h:** texto y adjuntos solo si el cliente escribió en las últimas 24 h; fuera de
  ella, 422 `fuera_de_ventana` y solo se puede mandar una **plantilla aprobada en ese número**
  (`mensajeria.canales.plantillas`; si no, 422 `plantilla_no_aprobada`).
- **Permiso de contacto:** una plantilla fuera de ventana exige cliente de Argos asociado con
  permiso vigente `acepta` en WhatsApp (§6.4 del contrato con Argos): si no, 422 `sin_permiso` o
  `baja`. Dentro de la ventana el cliente inició la conversación y no hace falta.
- Un mensaje con `baja` vigente del cliente nunca sale como plantilla. Un número pausado por
  calidad (§6.5) retiene el envío hasta que se libera.
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

### 10.3 SLA y horario

El SLA de la cola corre solo dentro del horario de la cola (o del número): un mensaje que llega de
noche empieza a contar al abrir.

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
| `POST /conversaciones/{id}/enviar-manual` | `conversaciones.responder` | `{ id, manualId, modo: "enlace" \| "texto" }`. Solo artículos `cliente` (si no, 422 `manual_interno`). `enlace`: URL pública firmada (`/api/v1/mensajeria/publico/manuales/{token}`, vence a los 30 días) en un mensaje; `texto`: el cuerpo en texto plano. Respeta la ventana de 24 h |

`GET /api/v1/mensajeria/publico/manuales/{token}` (público): el artículo para el cliente en HTML
mínimo y seguro, o 410 si el enlace venció o el artículo se desactivó.

## 12. Eventos (outbox)

Detalle en `contratos/eventos.md` («Conversaciones con clientes»). Ninguno lleva el texto de un
mensaje.

`conversacion.creada`, `conversacion.mensaje_recibido`, `conversacion.mensaje_enviado`,
`conversacion.mensaje_fallido`, `conversacion.nota_agregada`, `conversacion.mencion`,
`conversacion.asignada`, `conversacion.transferida`, `conversacion.etiquetada`,
`conversacion.resuelta`, `conversacion.reabierta`, `conversacion.cliente_asociado`,
`conversacion.vinculo_creado`, `conversacion.sla_vencido`, `contacto_iris.creado`,
`contacto_iris.unido`, `cola.creada` / `cola.actualizada`, `etiqueta.creada` / `.actualizada`,
`respuesta_rapida.creada` / `.actualizada`, `boleta.registrada`, `boleta.registrada_en_argos`,
`boleta.fallida`, `promesa.solicitada`; PR B: `horario.creado` / `.actualizado`, `conversacion.fuera_de_horario`,
`conversacion.guardia_avisada`, `manual.creado` / `.actualizado` / `.enviado`.

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
| `numero_avisos_requiere_uso` | 422 | `avisosEmpresa` sin `avisos` en `usos` |
| `manual_interno` | 422 | Enviar al cliente un manual interno (PR B) |
| `disponible_en_fase_2` | 422 | Activar el asistente (PR B) |
| `cursor_vencido` | 410 | Feed purgado |
