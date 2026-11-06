# mensajeria

Servicio de mensajería de Iris (contrato Iris ↔ ArgosISP 0.6.0 §6, formas de 0.6.1). Argos lo usa
como su canal WhatsApp: decide **cuándo** avisar e Iris decide **cómo y si** sale (plantilla
aprobada, permiso de contacto, límites, horario).

## Proceso y datos

- Corre aparte: `cmd/mensajeria` (mismo binario de la imagen de la API, servicio `mensajeria` del
  compose, puerto `IRIS_MENSAJERIA_PUERTO`). La API reenvía `/api/v1/mensajeria/*` tal cual a
  `IRIS_MENSAJERIA_URL` (`servidor.ProxyHacia`, antes de identidad e idempotencia); sin ella, 503
  `servicio_no_disponible`.
- Schema propio `mensajeria` en la base de Iris: así comparte en la misma transacción la identidad
  (tokens `iris_pat_`, sesiones, ACL), la auditoría y el outbox, sin duplicarlos.
- Sus jobs de River van en la cola `mensajeria` (solo la trabaja este proceso). Los periódicos los
  encola su propio reloj (`Reloj`, únicos por periodo), no el líder de River.

## Endpoints (`/api/v1/mensajeria`)

| Ruta | Permiso | Qué hace |
| --- | --- | --- |
| `POST /avisos` | `mensajeria.enviar` | §6.2. `Idempotency-Key` obligatoria; repetirla devuelve el mismo `aviso_id` con su estado **actual**. `202 { aviso_id, estado, motivo? }`. |
| `GET /avisos/{id}` | `mensajeria.enviar` | Estado del aviso; una integración solo ve los suyos (si no, 404). |
| `POST /consentimientos` | `mensajeria.registrar_consentimiento` | §6.4. 201 nuevo, 200 repetición; `{ consentimiento_id, estado_vigente }`. |
| `GET /plantillas` | `mensajeria.ver` | §6.3 con variables y medios aprobados. |
| `GET/POST /canales`, `POST /canales/{id}/actualizar\|activar\|desactivar` | `mensajeria.configurar` | WhatsApp Cloud API: **varios números por empresa** (credenciales cifradas, nunca se devuelven). |
| `POST /canales/{id}/configurar` | `canales.configurar` | Usos (avisos, soporte, cobros, ventas), cola y número de avisos de la empresa (uno activo). |
| `GET/POST /canales/{id}/webhook` | público | Verificación y webhook de Meta (`X-Hub-Signature-256`). |

## Reglas (§6.5)

Una integración (Argos) solo pide las plantillas de Argos de §6.3 (`pide: argos`); las de Iris
(`boleta_*`, `reconexion_temporal`, `promesa_aceptada`, `corte_programado`, con texto libre) y
`prioridad: urgente` le dan 422 (`plantilla_no_permitida` / `validacion`).

Al pedir el aviso, en este orden: permiso registrado (`sin_permiso`), baja vigente (`baja`), teléfono
de la instantánea de Argos, refrescada si tiene más de 24 h (`sin_contacto`), canal de la empresa que
admite avisos con la plantilla aprobada (`canal_no_disponible`) y 3 avisos automáticos por cliente y
día (`limite_diario`; `urgente` lo salta, solo `corte_programado`). Al enviar: horario de silencio
21:00–07:00 Guatemala (espera; al terminar, si venció su variable de caducidad, `fallido`/`vencido`),
20 por minuto por número remitente y pausa del número si Meta baja su calidad o corta por spam.
Fallas del canal: `numero_invalido`, `rechazado_por_canal`; temporales se reintentan y al final
`agotado`. Un aviso en cola sin acuse en 72 h también termina `agotado`.

Canales: `Adaptador` (WhatsApp Cloud API y el simulado). El simulado solo con
`IRIS_MENSAJERIA_SIMULADO=true` (dev y CI; el proceso no arranca con él si `IRIS_BASE_URL` es https en
un host no local); sin él, una empresa sin canal da `canal_no_disponible`. Del webhook de Meta solo
cuentan los cambios con el `phone_number_id` del canal (una app puede servir a varias empresas); los
demás se descartan y quedan anotados en `mensajeria.entrantes.error`.
WhatsApp por QR y Telegram son de fases siguientes: QR declarará `PermiteAvisos() = false`.

## Eventos

Emite (outbox; detalle en `.claude/integration/contratos/eventos.md`):
`aviso.creado`, `aviso.enviado`, `aviso.entregado`, `aviso.leido`, `aviso.fallido`,
`consentimiento.registrado`, `contacto.baja`, `canal_mensajeria.creado`, `canal_mensajeria.actualizado`,
`canal_mensajeria.activado`, `canal_mensajeria.desactivado`, `canal_mensajeria.calidad`.

Consume: `aviso.entregado`, `aviso.fallido` (solo los de origen `argos`) y `contacto.baja` (suscriptor
`mensajeria.webhooks` en el relay de `cmd/worker`) para los webhooks hacia Argos (§6.6): cuerpo
`{ id, evento, ocurrido_en, payload }`, `X-Webhook-Id` = `id` (el del evento del outbox), firma v1 con
el secreto que Argos entregó en el canje, reintentos a 1 min, 5 min, 30 min, 2 h y 12 h, y solo al
conector Argos activo con contrato ≥ 0.6.0 (si no, `omitido`). Cada aviso emite un solo webhook final.

## Chat interno del equipo (fase 1)

Contrato completo (endpoints, payloads, SSE, reglas de la cola sin conexión):
`.claude/integration/contratos/chat.md`. Código en `chat_*.go` y `chat.sql`; rutas bajo
`/api/v1/mensajeria/chat`, permisos `chat.ver`, `chat.enviar` y `chat.supervisar`.

- **Conversaciones** (`mensajeria.chat_conversaciones`, con `empresa_id`): `directa` (una por par y
  empresa), `cuadrilla` (una por cuadrilla y empresa) y `orden` (una por orden). Los miembros de
  cuadrilla y orden **no se copian**: la función `mensajeria.chat_visibles` los deriva al leer de
  `agenda.tecnicos`, `agenda.cuadrilla_zonas` y `campo.ordenes` (técnico asignado), más el alcance de
  `chat.supervisar` del actor (global, zona o cuadrilla). `chat_miembros` guarda solo los explícitos:
  los dos de una directa y el despacho que se une a un hilo de orden, que solo lo sigue viendo
  mientras su `ordenes.ver` (con alcance) cubra la orden. Las directas nunca se ven por alcance. Lo
  que no se ve es 404.
- **Mensajes** (`chat_mensajes`): id uuid v7 del cliente (mismo id y contenido → 200, otro contenido
  → 409). La `Idempotency-Key` la aplica el caso (queda en el mensaje; `IdempotenciaPropia` saca del
  middleware el envío y el abrir, para no guardar respuestas con texto). Texto plano ≤ 4000, una foto
  (`nucleo/adjuntos`, entidad `chat_conversacion`, registrada con `RegistrarAdjuntos` en cmd/iris y
  cmd/worker) y/o ubicación. `seq` por conversación en orden de llegada. Nunca se editan; el autor
  los elimina en 15 min: lápida sin contenido ni `hash_contenido`. 30 por minuto por usuario (429),
  con el reloj del módulo.
- **Fotos:** un filtro de adjuntos (`adjuntos.RegistrarFiltro`) solo deja ver la foto mientras un
  mensaje sin eliminar la muestra. Al eliminar el mensaje, `adjuntos.Retirar` la deja rechazada
  (`retirado`) y, tras el COMMIT, se borra del almacén (`Config.Almacen`, el mismo de la API). El
  mantenimiento retira las fotos nunca enviadas sin actividad en `ChatFotosSinEnviar` (48 h).
- **Lecturas** (`chat_lecturas`): puntero por usuario que solo avanza; no leídos = mensajes de otros,
  sin eliminar, después del puntero.
- **Tiempo real:** `chat_eventos` es el feed (`mensaje_nuevo`, `mensaje_eliminado`, `leido`,
  `conversacion_actualizada`). Cada escritura toma el candado de su empresa hasta el COMMIT y hace
  `NOTIFY iris_chat '<empresa>'`: dentro de una empresa los ids quedan en orden de COMMIT y el cursor
  nunca salta un evento. `EscucharChat` (cmd/mensajeria) despierta los flujos SSE de esa empresa;
  cada flujo relee el feed con la misma consulta que `GET /chat/cambios` (sondeo), filtrada por lo
  que el usuario ve. Latido cada 25 s, cierre a los 30 min (reconexión con Last-Event-ID) y purga a
  los 7 días en el mantenimiento (cursor más viejo → 410). `leerFeed` lee la cabeza de la empresa
  antes que los eventos (un evento que confirma en medio no se salta).
- **Seguridad del flujo:** en cada latido y antes de entregar, `identidad.Revalidar` vuelve a leer la
  sesión o el token (vigente, usuario activo, en la empresa) y el alcance sale de los permisos de
  ese momento; si falla, se cierra. Topes: `ChatSSEPorUsuario` (5; el sexto, 429
  `demasiados_flujos`), `ChatSSEMax` (`IRIS_CHAT_SSE_MAX`, 1000; 503 `flujos_agotados`) y un
  semáforo de `ChatLecturas` (16) lecturas del feed a la vez. El flujo quita el ReadTimeout del
  servidor y pone un plazo por escritura; el proxy de la API (`servidor.ProxyHacia`) solo en
  `GET /api/v1/mensajeria/chat/eventos` usa un plazo de escritura móvil (60 s) y un máximo de 31 min,
  y ReverseProxy copia cada evento sin búfer.
- **Auditoría:** abrir una conversación nueva, enviar y eliminar, con el id del mensaje y nunca el
  texto. Listar, ver, leer, cambios y el flujo no dejan fila. Los logs no llevan cuerpos.
- `Config`: `ChatPorMinuto` (30), `ChatPlazoEliminar` (15 min), `ChatLatido` (25 s),
  `ChatDuracionMax` (30 min), `ChatRetencion` (7 días), `ChatSSEMax`, `ChatSSEPorUsuario`,
  `ChatLecturas`, `ChatFotosSinEnviar` y `Almacen`.

Emite `chat.conversacion_creada`, `chat.mensaje_enviado` (con `destinatarios[]` para el futuro emisor
de push) y `chat.mensaje_eliminado`. No emite `chat.leido` (ver `contratos/eventos.md`). No consume
eventos.

## Bandeja de conversaciones con clientes (fase 1, sin IA)

Contrato completo: `.claude/integration/contratos/conversaciones.md`. Código en `conv_*.go` y
`conversaciones.sql`; rutas bajo `/api/v1/mensajeria` (`/conversaciones`, `/colas`, `/etiquetas`,
`/respuestas-rapidas`, `/contactos-iris`). Permisos `conversaciones.*` y `canales.configurar`.

- **Números:** varios por empresa (`usos[]`, `cola_id`); `avisos_empresa` marca el único por el que
  salen los avisos de Argos (`CanalAvisosDeEmpresa`). El número que existía quedó como avisos+soporte.
- **Entrantes:** el webhook de Meta ya guardado en `mensajeria.entrantes` se procesa en el job: cada
  mensaje del número entra a la conversación de (empresa, número, contacto) (`recibirMensaje`),
  deduplicado por wamid; se asocia sola si el número coincide con un solo cliente de la instantánea.
  Los medios se descargan de Meta en `mensajeria.descargar_medio` y quedan en el módulo de adjuntos
  (entidad `conversacion`, `adjuntos.GuardarCompleto`). «Respondió al aviso …» si el cliente contesta
  un aviso de ese número.
- **Salientes:** `POST /conversaciones/{id}/mensajes` (idempotente por id del cliente) deja el mensaje
  `en_cola`; `mensajeria.enviar_mensaje` lo manda (texto, medio subido a `/media`, o plantilla).
  Ventana de 24 h, plantillas aprobadas en el número y permiso de contacto (§8 del contrato). Los
  estados de Meta (`sent`, `delivered`, `read`, `failed`) nunca retroceden.
- **Alcance:** solo personas con sesión del panel y `conversaciones.ver` (`alcanceConvDe`): tokens
  de API e integraciones reciben 404 en toda lectura y acción. Luego `conv_visible` — supervisión
  (todas), asignada a mí, de mis colas, sin cola, o participante (mencionado). Lo demás es 404.
- **Cliente verificado:** la asociación automática o la manual cuyo número es un teléfono del
  cliente en la instantánea quedan verificadas; la manual sin coincidencia la confirma un
  supervisor (`confirmar-cliente`). Sin verificar: ni plantillas fuera de ventana, ni boletas, ni
  promesas. Contactos de Iris: permiso por número (`mensajeria.consentimientos_numero`); `BAJA` por
  chat también lo registra (antes de guardar el mensaje).
- **Medios:** el tipo de un medio entrante sale de sus bytes (`http.DetectContentType`); las boletas
  solo aceptan JPEG, PNG, WebP o PDF por firma.
- **Varios números en una app de Meta:** configura `override_callback_uri` por número en Meta
  (`…/canales/{id}/webhook`). Un cambio de otro `phone_number_id` que llegue a un canal se pasa al
  canal activo de ese número solo si es de la misma empresa y comparte el `app_secret` que verificó
  la firma; si no, se descarta.
- **Estados:** abierta → esperando_cliente (al responder) → resuelta; el cliente reabre. SLA por cola
  (`espera_desde`, `sla_vence_en`; por vencer = 25 % o lo configurado); el mantenimiento emite
  `conversacion.sla_vencido` una vez por espera.
- **Acciones:** crear ticket, orden y prospecto reutilizan `soporte.CrearTicket`, `campo.CrearOrden`
  y `comercial.CrearProspecto` en la misma transacción (cmd/mensajeria los arma; River con
  `SkipUnknownJobCheck` porque sus jobs los trabaja cmd/worker). Boletas: `mensajeria.boletas` y el
  despacho del reloj (`POST /comprobantes` multipart a Argos, reintentos 1 min…12 h). Promesas:
  sincrónicas contra Argos fuera de transacción (`iris:promesa:<id>`). Cabeceras `X-Iris-Actor` y
  `X-Iris-Conversacion` (§7.1).
- **Contactos:** solo el id del cliente de Argos (los datos salen de la instantánea); coincidencias
  por teléfono con `GET /clientes?telefono=` (o la instantánea si Argos no responde); contactos
  propios de Iris (`mensajeria.contactos_iris`) que se unen a la ficha de Argos.
- **Tiempo real:** feed `mensajeria.conv_eventos` + `NOTIFY iris_conversaciones` + SSE
  (`GET /conversaciones/eventos`) y sondeo (`/conversaciones/cambios`), con los mismos topes y
  revalidación que el chat; purga a los 7 días (`chat_estado.conv_purgados_hasta`).
- **Auditoría:** cada acción; el texto de los mensajes y notas nunca (solo id, tipo y hash).

Emite (detalle en `contratos/eventos.md`, «Conversaciones con clientes»): `conversacion.*`,
`contacto_iris.*` (también `contacto_iris.consentimiento`), `cola.*`, `etiqueta.*`, `respuesta_rapida.*`, `boleta.*`, `promesa.solicitada`.
No consume eventos (los webhooks `comprobante.*` / `promesa.*` de Argos quedan para la fase 1.1).

### Horarios, fuera de horario y manuales (`horarios.go`, `fuera_horario.go`, `manuales.go`, `horarios.sql`)

- **Horario** (`mensajeria.horarios`): entidad genérica (número, cola y, en la fase 2, asistente) con
  franjas por día en hora local de su zona y feriados que cierran o reemplazan el día. `calendario`
  evalúa en UTC (`estado`: abierto, franja y próximo cambio). `perfil` por franja es el punto de
  extensión de la fase 2 (permisos del asistente); hoy se guarda y nada lo usa. Vale el de la cola o,
  si no tiene, el del número; sin ninguno, siempre abierto. El SLA empieza a contar al abrir.
- **Fuera de horario** (`canales.fuera_horario`): `alRecibir` (después de guardar el mensaje del
  cliente) contesta una vez por ventana cerrada (`conversaciones.fuera_horario_hasta` = próxima
  apertura), nunca a una baja vigente, y avisa a la guardia (participante + evento `guardia`) por
  etiqueta o por falla masiva (router del servicio caído en `soporte.horus_estados`, solo lectura),
  una vez por conversación y ventana. `asistente.activo` es de la fase 2 (422).
- **Manuales** (`mensajeria.manuales`): búsqueda `websearch_to_tsquery('spanish')` sobre un
  `tsvector` ponderado (título A; resumen, etiquetas y equipo B; cuerpo C). Sugerencias por reglas con
  su motivo: etiqueta de la conversación, equipo del servicio (Argos, `EquiposEnComodato`) y ticket
  abierto del cliente (`soporte.tickets`, solo lectura). Enviar al cliente solo los de visibilidad
  `cliente`: enlace público (`mensajeria.manual_enlaces`, solo el hash del token, 30 días) servido como
  HTML mínimo con todo el texto escapado y CSP cerrada, o el texto. Cada enlace se puede revocar
  (`manuales.revocar_enlace`, `revocado_en`). Excluye `/api/v1/mensajeria/publico/manuales/*` de los
  access logs del proxy: el token va en la ruta. Es la base de conocimiento del asistente de la fase 2.
- **Fuera de horario y bajas:** la palabra BAJA se registra antes de guardar el mensaje, así que
  quien la escribe no recibe la respuesta automática; tampoco un número con baja vigente.

Emite además `horario.*`, `conversacion.fuera_de_horario`, `conversacion.guardia_avisada`,
`manual.creado`, `manual.actualizado`, `manual.enviado` y `manual.enlace_revocado`.
