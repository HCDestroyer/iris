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
| `GET /canales/{id}`, `POST /canales/{id}/probar` | `mensajeria.configurar` | Detalle y prueba de conexión con la Graph API (`probar`: solo personas con sesión). `Canal` trae `conexion`, la última prueba, `webhookVerificadoEn`, `ultimoMensajeEn`, `*GuardadoEn` y `webhookUrl` (contratos/conversaciones.md §1.1). |

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
`agotado`. Si Meta rechaza el token (401/190) o los permisos (códigos 10 y 200–299; un 403 con otro
código no) al enviar, el número queda marcado (`ultimo_estado`, `rechazado_por_envio`) y sale
`canal_mensajeria.probado` con `origen: envio`; el siguiente envío aceptado lo devuelve a conectado; hacia Argos
el motivo sigue siendo `rechazado_por_canal`, y en la bandeja el mensaje lleva `token_invalido` /
`sin_permiso_meta` y, para quien administra los números, `codigoFalla` (solo números). Un aviso en cola sin acuse en 72 h también termina `agotado`.

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
`canal_mensajeria.activado`, `canal_mensajeria.desactivado`, `canal_mensajeria.calidad`, `canal_mensajeria.probado`.

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
- **Auditoría:** abrir una conversación nueva, enviar, eliminar y agregar un miembro, con los ids y
  nunca el texto. Listar, ver, leer, cambios y el flujo no dejan fila. Los logs no llevan cuerpos.
- `Config`: `ChatPorMinuto` (30), `ChatPlazoEliminar` (15 min), `ChatLatido` (25 s),
  `ChatDuracionMax` (30 min), `ChatRetencion` (7 días), `ChatSSEMax`, `ChatSSEPorUsuario`,
  `ChatLecturas`, `ChatFotosSinEnviar` y `Almacen`.

- **Panel (chat.md §2.1–§2.3, §3.2; `chat_panel.go`):**
  - *Menciones* (`chat_menciones`, con `empresa_id` y `seq`): `menciones[]` al enviar (hasta 20),
    validadas contra `MiembrosChat` (quienes ven la conversación; nunca el autor). Entran en el hash
    del contenido (otro conjunto con el mismo id → 409) y se borran al eliminar el mensaje. Cuentan
    en `menciones` de la conversación, del `EventoChat` y de `GET /chat/resumen` hasta leerlas.
  - *Miembros:* `MiembrosChat` (consulta única) da quién ve la conversación y por qué (`directa`,
    `tecnico`, `despacho`, `agregado`, `supervision`); la usan `GET …/miembros`, los
    `destinatarios[]` del push y la validación de menciones. `POST …/miembros` (`chat.supervisar` con
    alcance sobre la orden) guarda el miembro `agregado` (con `agregado_por`), que `chat_visibles` deja
    ver sin depender de `ordenes.ver`.
  - *Solo lectura:* el hilo de una orden `cerrada`, `fallida` o `cancelada` no acepta mensajes nuevos
    ni miembros (422 `conversacion_cerrada`); se deriva del estado al leer. Un reenvío de un id que ya
    llegó sigue respondiendo 200 y eliminar lo propio dentro de los 15 min sigue permitido.
  - *Listas:* `q` en `GET /conversaciones` (título), `GET /chat/ordenes/{id}/conversacion` (sin crear
    ni unirse), `GET /chat/personas` (paginada) y `GET /chat/cuadrillas` para «Nuevo mensaje».
  - *Resumen:* `GET /chat/resumen` reutiliza el resultado de hace menos de 2 s por usuario y empresa
    (`cacheResumen`, en memoria del proceso); `POST /leer` olvida el del usuario al confirmar.
  - *Cierre a la vez:* el estado de la orden se lee sin bloquearla; un mensaje o un miembro que llega
    justo con el cierre puede quedar (aceptado, chat.md §1).
  - *Deshacer:* no hay «restaurar» en el servidor; el panel retrasa la eliminación 5 s (chat.md §3.1).

Emite `chat.conversacion_creada`, `chat.mensaje_enviado` (con `destinatarios[]` para el emisor de
push), `chat.mensaje_eliminado`, `chat.mencion` (uno por mencionado; lo consume
`internal/notificaciones`) y `chat.miembro_agregado`. No emite `chat.leido` (ver
`contratos/eventos.md`). No consume eventos.

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
  (todas), asignada a mí, de mis colas (miembro o supervisor de la cola), sin cola mientras nadie la
  tiene, o participante (mencionado). Lo demás es 404.
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
  sincrónicas contra Argos fuera de transacción (`iris:promesa:<id>`); los rechazos de Argos salen en llano (contratos/conversaciones.md §6.5). Cabeceras `X-Iris-Actor` y
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
Consume `argos.comprobante_resuelto` (webhooks `comprobante.*` de Argos; ver «Aprobar y rechazar boletas»). Los
webhooks `promesa.*` quedan para la fase 1.1.

### Aprobar y rechazar boletas (`boletas_decidir.go`, `boletas_rutas.go`, `boletas.sql`)

Contrato con Argos 0.7.0 §7.7 (pedido B3); la API está en `contratos/conversaciones.md` §6.4.1.

- **Acciones:** `POST /boletas/{id}/aprobar` (`boletas.aprobar`) y `POST /boletas/{id}/rechazar`
  (`boletas.rechazar`); `GET /boletas/{id}` y `GET /conversaciones/{id}/boletas` con `puedeDecidir`.
  Solo una persona con sesión del panel que vea la conversación (token, integración o el sistema —así
  actúa el asistente de IA—: 403 `solo_personas`).
- **Llamadas sincrónicas contra Argos fuera de transacción** (como las promesas, y nunca por
  `integracion.pendientes`): un caso previo revisa permiso, visibilidad, estado y contrato ≥ 0.7.0;
  se llama a Argos (`X-Iris-Actor: usuario:<id>`, `X-Iris-Conversacion`, `iris:aprobar:<comprobante>` /
  `iris:rechazar:<comprobante>`) y un caso final guarda lo que Argos decidió. Los errores de Argos se
  traducen en `alFallar` (nunca su `detail`). Argos caído: 502 `argos_no_disponible`.
- **Idempotencia propia** (`rutaBoletaPropia`): el middleware no guarda la respuesta; la clave hacia
  Argos sale del comprobante y repetir la misma decisión devuelve la boleta como quedó.
- **Solo Argos cambia el estado** (`aplicarResolucion`, con la boleta en `FOR UPDATE`): la respuesta
  200 de Argos, o lo que diga `GET /comprobantes/:id` al confirmar un webhook o en una relectura
  (`comprobante_revisado`, `otra_persona_decidio`). De `registrada` a resuelta, una vez: mensaje de
  sistema, aviso al cliente, auditoría y `boleta.aprobada` / `boleta.rechazada`. Mismo estado: solo
  completa quién decidió. Otro estado: gana Argos (`boleta.corregida`, sin otro aviso). Orden de
  candados: boleta y después conversación (igual que el despacho de boletas).
- **Aviso al cliente** (`avisarBoleta`, `motivoParaCliente`): texto con la ventana abierta; plantilla
  aprobada y permiso con la ventana cerrada; si no, `sin_aviso`. En un rechazo va un texto fijo
  según la causa (`textosCausa`); el motivo libre (de Iris o de la caja de Argos) solo se agrega si
  pasa `motivoSeguro`: lista blanca sobre el texto normalizado con NFKC y sin caracteres `Cf` (letras
  del español, espacios y `,;:¿?¡!()`; ningún dígito ni punto).
- **Auditoría:** el intento queda auditado antes de llamar a Argos (`fase: intento`). Si el guardado
  falla después del 200 (la persona ya no puede), lo aplica el sistema (`respuesta_sin_sesion`).
- **Quién decidió:** la persona de la sesión en la respuesta directa; en webhook y relectura, el
  `verificado_iris_actor` del comprobante (0.7.1, opcional) si es un usuario persona de esta
  instalación. Nunca `iris_actor` (es quien registró la boleta).
- **`montoEsperado`** (`exigirMonto`): se compara con la boleta y con el comprobante releído antes de
  aprobar; si no coincide, 409 `monto_cambio` con `montoActual`.
- **Webhook:** es solo un aviso. El worker (`SuscribirComprobantes`) exige `empresa_id` y marca la
  boleta de ese comprobante en esa empresa (`boletas.resolucion`). El despacho de este proceso
  (`despacharResoluciones`, cada 10 s) la confirma pasada `GraciaResolucion` (20 s): relee
  `GET /comprobantes/:id`, valida comprobante, canal `iris` y empresa, y aplica lo que diga Argos. Si
  no se confirma, cuenta el intento y espera (`EsperasConfirmacion`); agotados, queda marcada
  (`confirmacionArgos: agotada`, `boleta.confirmacion_agotada`). Un error en una boleta, o en otro
  despacho del mismo job, no corta la corrida. El worker no encola envíos de WhatsApp: por eso
  aplica este proceso.
- **Un comprobante es de una sola boleta por empresa** (índice único parcial
  `boletas_comprobante_uidx`, migración `20261116095604`). Si una instalación probó la rama de B3
  antes de esa migración, revisa primero que no haya repetidos (si la consulta devuelve filas, la
  migración falla hasta corregirlas a mano; ver `docs/runbooks/boletas-comprobante-unico.md`):

  ```sql
  SELECT empresa_id, comprobante_id FROM mensajeria.boletas
  WHERE comprobante_id IS NOT NULL GROUP BY 1,2 HAVING count(*)>1;
  ```

Emite `boleta.aprobada`, `boleta.rechazada`, `boleta.corregida` y `boleta.confirmacion_agotada`;
consume `argos.comprobante_resuelto`.

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

### Bandeja 1.1 (`conv_equipo.go`, `conv_bot.go`, `conv_iniciar.go`, `conv_rutas11.go`, `bandeja.sql`)

Contrato: `contratos/conversaciones.md` §14. Permisos nuevos `conversaciones.iniciar` (sensible:
escribe a clientes por iniciativa propia; soporte, ventas, supervisor) y `conversaciones.pasar`
(todos los roles que atienden).

- **Supervisores por cola** (`mensajeria.cola_supervisores`, con `tipo` persona | asistente para la
  fase 3): ven, asignan (sin `conversaciones.asignar`) y pasan todo lo de su cola. `conv_visible`
  los incluye y deja de mostrar a todos las conversaciones **sin cola ya asignadas**.
- **Iniciar** (`POST /conversaciones/iniciar`): idempotente por el id del mensaje (como responder; la
  clave la aplica el caso, `IdempotenciaPropia`). El número sale de la instantánea de Argos
  (`Contactos`, refrescada fuera de la transacción) o del contacto de Iris; permiso de contacto con
  `permisoPlantilla` (como fuera de la ventana). Reutiliza la conversación de (empresa, número,
  contacto). `GET /conversaciones/destinatarios` busca clientes (instantánea) y contactos de Iris con
  `puede`/`motivo` en una llamada; `GET /conversaciones/canales/{id}/plantillas` las del número.
- **Pasar** (`POST …/pasar`): quien la tiene o un supervisor (global o de la cola); se puede pasar a
  alguien no disponible (`destinoDisponible`).
- **Disponibilidad** (`mensajeria.disponibilidad`, por usuario y empresa): manual (auditada) y
  automática en el mantenimiento (`vencerDisp`: un caso por agente con la fila bloqueada y el motivo
  recalculado; una fila de auditoría por transición, actor sistema): sesión cerrada o vencida
  (`sesion_id` del último cambio o latido), `horario_hasta` (cierre de las franjas abiertas de los
  horarios de sus colas, anotado al ponerse disponible y en cada latido) e inactividad
  (`mensajeria.ajustes_bandeja.minutos_inactividad`, 15). El latido y las acciones de la bandeja solo
  tocan `ultima_actividad`: nunca auditan ni emiten.
- **Bot** (`canales.bot`, minutos por cola en `colas.bot_sin_*_min`): `alRecibirBandeja` corre
  después de guardar el mensaje del cliente: respuesta al menú → fuera de horario (con el menú) → en
  horario, aviso a los disponibles (temporizador `sin_confirmacion`) o saludo inmediato
  (`sin_disponibles`). El estado de la espera vive en la conversación (`bot_espera_en`,
  `bot_vence_en`, `bot_motivo`, `bot_atendio_en`, `bot_menu`); el mantenimiento dispara lo vencido
  (`dispararBot`, un caso por conversación con la fila bloqueada) y tomar/asignar/pasar/transferir/
  resolver/iniciar lo cancelan (`limpiarEspera`). Cada respuesta del bot se audita como actor sistema (`mensajeria.bot_conversacion` o
  `mensajeria.bot_respondio`). Un error en una fila del barrido queda en el log y sigue con la
  siguiente. `iniciar` solo reutiliza la conversación del número si el agente la ve y es de su cola
  (si no, 409 `conversacion_de_otra_cola`) y no reescribe una asociación sin `conversaciones.asociar`.
  Los mensajes automáticos llevan `automatico`
  (`bot`, `fuera_de_horario`). Nunca a una baja ni fuera de la ventana de 24 h.

Emite además `conversacion.iniciada`, `conversacion.aviso_sin_atender`, `conversacion.bot_atendio`,
`conversacion.menu_elegido`, `agente.disponibilidad_cambiada` y `conversaciones.ajustes_cambiados`
(`conversacion.asignada` suma los orígenes `pasar` e `iniciar`). No consume eventos nuevos.

## Conexión con Meta, WhatsApp por QR y préstamo de números (`contratos/conversaciones.md` §15)

- **Conexión con Meta** (`conexion_meta.go`): una por instalación en `mensajeria.conexion_meta`
  (modo `propia` con app ID, app secret, config ID y versión de la Graph API; `compartida` con el
  servicio central «Iris Cloud», que todavía no existe), o la del entorno (`IRIS_META_MODO=saas`,
  solo lectura). Permiso `mensajeria.conexion_meta` (sensible, solo admin). Los secretos se cifran
  con `IRIS_CLAVE_CIFRADO` y nunca se devuelven. Webhook de la app: `GET/POST /meta/webhook`
  (verify token y firma de la conexión); cada `entry` se reparte al número QR de su
  `phone_number_id` (o de su WABA en `account_update`).
- **Registro con QR** (`canal_qr.go`, `registro_meta.go`): `POST /canales/qr` canjea el `code` de
  Embedded Signup una sola vez (`registros_qr` guarda solo su SHA-256), lee el número, suscribe la
  app a la WABA (con `override_callback_uri` cuando hay varios números QR) y nunca llama
  `/register`. La interfaz `RegistroMeta` tiene la implementación de la app propia (Graph API) y la
  del servicio central (hoy `servicio_central_no_disponible`). `POST /canales/{id}/sincronizar`
  pide contactos e historial (`smb_app_data`, una vez y dentro de 24 h).
- **Coexistencia** (`coexistencia.go`): `smb_message_echoes` entra como saliente `origen: telefono`
  (sin asignar ni bot); `history` entra resuelto, sin eventos de conversación ni feed;
  `smb_app_state_sync` crea o renombra contactos de Iris (los clientes de Argos solo se cuentan);
  `account_update` desconecta o reconecta el número.
- **Préstamo de números** (`prestamos.go`): la dueña comparte un número con otras empresas
  (`mensajeria.canal_prestamos`: usos, cola y número de avisos de quien lo usa). Un mensaje que
  llega a un número compartido va a la empresa de la regla de §15.3 (conversación abierta → quien
  la empezó en 24 h → cliente de una sola empresa → la dueña). Una conversación es de una sola
  empresa: listas, contadores, detalle y feed filtran por ella. Revocar resuelve lo abierto de esa
  empresa (queda de solo lectura) y cancela sus avisos en cola.

Emite: `meta.conexion_cambiada`, `meta.conexion_probada`, `canal_mensajeria.desconectado`,
`canal_mensajeria.reconectado`, `canal_mensajeria.prestado`, `canal_mensajeria.prestamo_revocado` y
`conversacion.empresa_asignada`; suma `tipo_conexion` a `canal_mensajeria.creado` y `origen` a
`conversacion.creada`, `conversacion.mensaje_enviado` y `contacto_iris.creado`. No consume eventos.

## Asistente de IA que sugiere respuestas (fase 2, `contratos/conversaciones.md` §16)

Código en `asistente*.go` y `asistente.sql`. **Nunca envía mensajes:** guarda una sugerencia que una
persona copia a la caja y envía con `POST /conversaciones/{id}/mensajes` (con `sugerenciaId` para medir
si se envió igual o editada). No registra pagos, promesas ni reconexiones.

- **Proveedor por empresa** (`mensajeria.asistente_ajustes`, `mensajeria.asistente_configurar`):
  `ollama` y `openai_compatible` con un adaptador (`POST {url}/v1/chat/completions`,
  `GET {url}/v1/models`) y `anthropic` con el SDK oficial de Go (sin `temperature`, system con
  `cache_control`, respaldo del lado del servidor `fallbacks: "default"`, `refusal` → `rechazo_modelo`,
  sin leer `ANTHROPIC_*` del entorno). Interfaz
  `Proveedor` (`Generar`, `Modelos`); los adaptadores no tienen reglas de negocio. La clave se cifra
  con `IRIS_CLAVE_CIFRADO` (`DatosAsociadosAsistente`, rota con `iris clave rotar`) y nunca sale.
- **Red (SSRF):** solo http/https sin credenciales ni parámetros; IP privadas sí; loopback solo con
  `IRIS_ASISTENTE_PERMITIR_LOOPBACK`; link-local, metadata (AWS, GCP, Azure, Alibaba), NAT64, Teredo,
  IPv4-compatible, 6to4 con IPv4 bloqueada, sin especificar y multicast nunca. Se revisa al guardar y
  en cada conexión (después del DNS, `controlIA`). Ninguna redirección se sigue (`sinRedirecciones`:
  un 3xx es `proveedor_redirige`, también en el SDK). Con clave, https salvo IP privada. La clave
  guardada no viaja a otro proveedor o URL (422 `clave_requerida`). Probar y listar modelos dan una
  sola respuesta para cualquier falla (no sirven para escanear puertos); listar con otra URL es POST
  (CSRF). Sin proxy. Plazos: 10 s de conexión, 90 s por generación (`PlazoIA`), 30 s probar/modelos.
- **Costo y abuso** (`asistente_limites.go`): reserva atómica de tokens estimados antes de llamar
  (sugerencias y resúmenes) y ajuste al terminar; 10 pedidos por minuto por persona, 20 sugerencias
  por hora por conversación (429 con `Retry-After`), 6 automáticas por hora; «Otra» y descartar
  cortan la llamada en curso (sondeo cada 2 s); el automático solo con agente asignado o disponibles
  en la cola; el mantenimiento vence las atascadas y libera reservas huérfanas.
- **Préstamo de números:** pasar la conversación a otra empresa vence la sugerencia; las consultas
  filtran por la empresa actual y una generación que cambió de empresa queda vencida sin cobrar.
- **Contexto** (según `fuentes`): instantánea de Argos del cliente asociado (solo servicios de la
  empresa de la conversación), fallas de Horus (router caído, `soporte.horus_estados`), los 3 mejores
  manuales de la empresa (`to_tsquery` con OR) y los últimos 30 mensajes del cliente y de agentes (sin
  notas ni sistema). Todo pasa por `RedactarParaIA` (tras «contraseña», «clave», «pin», «código»,
  «cvv»… salta conectores y oculta hasta el fin de la frase; números de 4 a 8 dígitos cerca de esas
  palabras, tarjetas con espacios/puntos/guiones, DPI y NIT → `***`) y se neutralizan `<`/`>` del texto no confiable. Cada dato lleva un id `F#`.
- **Validación** (`validarSalidaModelo`): JSON `{ texto, fuentes[], accion }`; texto ≤ 4096 sin
  enlaces (cualquier esquema, dominio o IPv4) ni marcas internas, sin copiar más de 12 palabras
  seguidas de un manual del equipo; solo fuentes dadas; `accion` solo `etiquetar` con una etiqueta activa
  existente. Si no, `error` / `respuesta_invalida`.
- **Flujo:** `POST …/sugerencias` (botón/«Otra») crea la fila `generando` y encola
  `mensajeria.asistente_generar`; modo automático: cada mensaje del cliente vence la vigente y encola
  `mensajeria.asistente_sugerir` (único por conversación; `JobSnooze` hasta 20 s después del último
  mensaje; no genera en resueltas, con bot atendiendo, inactivo, modo botón o en pausa). La llamada al
  proveedor va entre dos transacciones; si llegó otro mensaje mientras tanto queda `vencida`.
  Cada cambio sale por el feed de la bandeja como `sugerencia` (solo a quien tiene
  `conversaciones.sugerencias`).
- **Tope:** `mensajeria.asistente_uso` por empresa y mes (Guatemala). Al 80 % y 100 % emite
  `asistente.tope_alcanzado`; al 100 % se pausa hasta el mes siguiente o hasta subir el tope. Costo
  con `precioEntradaMillon`/`precioSalidaMillon`. El aviso a los administradores (campana y correo)
  lo hace `internal/notificaciones` al consumir el evento (§16.9).
- **Saldo del cliente (2.1, `asistente_cuenta.go`, §16.10):** con la fuente `argos` y cliente asociado,
  antes de preparar cada sugerencia se lee en vivo el estado de cuenta (`ArgosAtencion.EstadoCuentaCliente`,
  `X-Iris-Actor: asistente_ia`, 5 s), fuera de la transacción y sin guardarlo (ni fuentes, ni eventos,
  ni auditoría, ni logs) el monto. Solo queda «al día» o «con saldo vencido (N facturas)», que sí se
  guarda como etiqueta de la fuente («Servicio activo, al día · Argos», «Con saldo vencido (N facturas)
  · Argos»). **El monto nunca llega al modelo ni sale en una sugerencia:** con cliente verificado el
  modelo ve solo ese estado; sin verificar, nada del estado de cuenta y la instrucción de ofrecer
  verificar. `validarSalidaModelo` rechaza siempre un monto de deuda (`ContieneMonto`: normaliza con
  NFKC, moneda antes o después, enteros y numerales en letras cerca de «saldo», «debe», «deuda»…; los
  precios de plan de un manual pasan). La cuenta va atada al cliente (`cuentaIA.clienteID`): si la
  conversación cambia de cliente no se usa, y la sugerencia queda `vencida`; asociar o desasociar
  vence la vigente. Si Argos falla, la sugerencia sale sin esa fuente. El resumen no lo lee.
- **Resumen** (`POST …/resumen`): síncrono, cuenta para el tope, no se guarda el texto.
- **Auditoría:** ajustes (clave `***`), pruebas, pedir/usar/descartar/calificar, generación (ids,
  códigos y tokens) y resumen; nunca el texto de la sugerencia ni del resumen. Las respuestas con
  texto del modelo no pasan por la tabla de idempotencia (`rutaAsistentePropia`).

Emite `asistente.ajustes_cambiados`, `asistente.sugerencia_creada`, `asistente.sugerencia_usada`,
`asistente.sugerencia_calificada` y `asistente.tope_alcanzado`. No consume eventos.

## Asistente autónomo y categorías (fase 3a, `contratos/conversaciones.md` §17)

Código en `asistente3*.go` y `asistente3.sql`. **En 3a el asistente todavía no actúa solo:**
`AutonomoDisponible` es `false`, `modoAutonomo` solo admite `nunca` (422 `autonomo_no_disponible`) y
el principio de la fase 2 («nunca envía») sigue vigente. El bucle de herramientas es de 3b (§17.8).

- **Ajustes ampliados** (`asistente_ajustes`): `modo_autonomo`, `espera_min`, `max_turnos`, `perfiles`
  (jsonb: acciones y límites con nombre; el primero es `defecto`) y `por_cola` (acciones apagadas por
  cola). Todos opcionales al guardar: el panel de la fase 2 no los pisa. Los perfiles extra se
  usarán por franja con `horarios.franjas[].perfil` (3b).
- **Categorías** (`mensajeria.asistente_categorias`, por empresa): seis iniciales sembradas al
  guardar los ajustes (`sembrarCategorias`, con candado; el GET no escribe, y sin
  `mensajeria.asistente_configurar` solo devuelve id, nombre, tono, activa y esOtro). `nombre`, `descripcion` (dato
  para clasificar), `tono`, cola, SLA de primera respuesta, `modo`, `condicion` y manuales base.
  «Otro» no se desactiva. No se borran.
- **Clasificación:** con el asistente activo, cada mensaje del cliente encola
  `mensajeria.asistente_sugerir` (también en modo botón). Si toca sugerir, la categoría sale de la
  **misma llamada** (`categoria` y `confianza` en el JSON; `clasificarTrasSugerencia`, en otra
  transacción que bloquea solo la conversación). Si no (modo botón, nadie que la lea, límite), una
  llamada corta aparte (`ClasificarAparte`: reserva de tokens, 6 por conversación y hora). Nombre que
  no existe o confianza < 0.5 → «Otro». Un mensaje no se clasifica dos veces
  (`conversaciones.clasificado_mensaje_id`). En pausa por tope o sin asistente no se clasifica.
- **Efecto** (`aplicarCategoria`): guarda `categoria_id`, confianza y origen; sin asignar y con cola
  activa en la categoría, transfiere (`conversacion.transferida` con `origen`); el SLA de la categoría
  gana sobre el de la cola (`venceSLA`), también sin cola. La corrección del agente
  (`POST …/categorizar`, `conversaciones.categorizar`) no la pisa el asistente hasta que la
  conversación se reabra.
- **Transferir con cuidado:** el asistente solo mueve la cola si la categoría cambió, con confianza
  ≥ 0.75, una vez por periodo abierto (`asistente_transfirio_en`), si la cola actual es la inicial o
  la puso él (`conversaciones.cola_origen`, que un trigger pasa a `otro` en cualquier otro cambio de
  cola) y si el bot no está atendiendo. Un agente sin `conversaciones.transferir` cambia la categoría
  sin mover la cola. Pasar la conversación a otra empresa borra la categoría; un trigger impide una
  categoría de otra empresa.
- **Costo:** clasificar aparte se detiene al 80 % del tope del mes y tiene un máximo de 300 por
  empresa y hora (además de 6 por conversación y hora).
- **Lista:** filtro `categoriaId`, `categoria`, `actividadAsistente` (una línea armada por el
  servidor: `clasifico`, `transfirio`, `sugirio`) y `porCategoria` en los contadores. Los mensajes
  llevan `autorTipo` (`cliente`, `persona`, `asistente_ia`, `sistema`).
- **Probar mensaje** (`asistente3_probar.go`): con los ajustes del formulario sin guardar; contexto =
  manuales base de las categorías y los 3 de la búsqueda (sin cliente: nada de Argos ni Horus). El
  modelo devuelve categoría, texto, fuentes, acciones y si pasaría a una persona; **la decisión
  (`haria`) y el filtro de acciones (lista cerrada y encendidas en el perfil) son del servidor** y
  nada se ejecuta. La respuesta pasa `validarSalidaModelo`. Cuenta para el tope (reserva) y para los
  10 pedidos por minuto; no pasa por la tabla de idempotencia.
- **Efecto** (`GET /asistente/efecto`): primera respuesta promedio y resueltas sin persona de las
  conversaciones del mes que el asistente clasificó; «antes» = los 30 días previos a
  `asistente_ajustes.activado_en`.

Emite `conversacion.categorizada`, `categoria.creada`, `categoria.actualizada` y
`asistente.mensaje_probado`; `conversacion.transferida` suma `origen`. No consume eventos.

## El asistente atiende solo (fase 3b, `contratos/conversaciones.md` §17.8)

`asistente_auto.go` (turno, decisión, validación del mensaje, pasar a persona), `asistente_herramientas.go`
(herramientas), `asistente_auto.sql`, y las herramientas en los dos adaptadores de `asistente_proveedor.go`.

- **Actor.** `contexto.ActorAsistenteIA` con canal `ia` y solo los permisos de `nucleo.PermisosAsistenteIA()`
  (rol interno `asistente_ia`: no está en `nucleo.roles`, no se asigna y su código está reservado). Los casos
  de uso de la bandeja que puede usar (`Responder`, `CrearTicket`, `CrearOrden`, `Boleta`, `Promesas`) pasan
  por `alcanceAccionDe`; el resto sigue con `alcanceConvDe` (solo personas). `convDelAsistente` solo le deja
  tocar una conversación de su empresa, sin asignar, sin resolver y que él atiende.
- **Turno.** `SugerirAutomatico` llama primero a `AtenderSolo`: `prepararTurno` (actor sistema) decide
  (modo, horario, agentes, categoría y condición, perfil de la franja menos `accionesApagadas` de la cola,
  límites, tope con reserva), toma el turno (`mensajeria.asistente_turnos`, único por conversación y
  mensaje) y arma la lista cerrada de herramientas. `correrTurno` es el bucle (máximo 4 herramientas y 6
  pasos; respaldo en JSON de un paso si el modelo no acepta herramientas). `terminarTurno` (actor
  asistente_ia, una transacción) cuenta el uso, valida el texto y lo envía con `Responder`, o pasa a una
  persona. Si una persona tomó la conversación o escribió, no envía nada.
- **Proveedores.** OpenAI compatible: `tools` estrictas y `tool_choice: "auto"`. Anthropic: SDK oficial,
  herramientas `strict`, `tool_choice` auto y la respuesta anterior devuelta tal cual en cada paso.
- **Estado en la conversación.** `asistente_estado` (`atendiendo`, `espera_persona`, `detenido`),
  `asistente_turnos`, `asistente_presento_en`. Un disparador de la base lo deja `detenido` al asignarse
  una persona y lo pone en cero al resolverse o al pasar de empresa.
- **Bot.** `asistenteAtiende` en `alRecibirBandeja`, `dispararBot` y `alRecibirFueraDeHorario`.
- **Mantenimiento.** Un turno cuyo job murió se vuelve a encolar; los turnos de más de 90 días se purgan.
- **Eventos que emite:** `asistente.accion_ejecutada` y `asistente.paso_a_persona`; además los de los casos
  de uso que corre (`conversacion.mensaje_enviado` con `automatico: "asistente"`, `conversacion.vinculo_creado`,
  `promesa.solicitada`, `boleta.registrada`, `conversacion.transferida` con `origen: "asistente"`).
  **No consume** eventos nuevos.
- **Pendiente:** `agendar_visita` (necesita los espacios libres de agenda) y la llamada de reconexión
  temporal en `internal/integracion/argos` (`ArgosReconexion`).

### Revisión de seguridad de 3b (§17.8.2 y §17.9)

- **Verificación en dos niveles** (`contacto.verificacion`): `numero` (asociación automática) y `fuerte`
  (confirmada por una persona o por `verificar_identidad` contra Argos, §7.5; el dato nunca se guarda: el
  mensaje queda con `***`; 3 intentos por cliente y empresa cada día entre sus conversaciones de número verificado; desde un número asociado a mano van aparte —3 por conversación, 3 por cliente y 2 por media hora— y no gastan los del titular; cada intento se reserva antes de llamar a Argos). Saldo, promesa y reconexión exigen `fuerte`
  (`perfiles[].exigeVerificacionFuerte.dar_saldo` puede bajar solo el saldo a `numero`).
- **Reconexión temporal:** caso de uso `PedirReconexion` (`conv_reconexion.go`, permiso
  `conversaciones.pedir_reconexion`), tabla `mensajeria.reconexiones`, evento `reconexion.solicitada`. Solo
  fuera de horario (el único motivo que acepta Argos), con una boleta registrada en Argos. El intento se
  registra antes de llamar (`pendiente`) y el mantenimiento repite con la misma clave si la respuesta se pierde.
- **Boletas del asistente:** adjunto nuevo de este periodo, uno por boleta, 3 por conversación y día;
  `por_asistente = true`.
- **Texto:** precios, plazos y enlaces válidos salen de los datos, manuales «Para el cliente» y resultados de
  herramientas, nunca del cliente; el historial va en una línea por mensaje y sin prefijos de otro papel; no se
  envía nada que diga ser una persona, dé un pago por aplicado o una reconexión por hecha sin herramienta.
- **Bot y fuera de horario:** con modo autónomo esperan a la clasificación (`bot_diferido_mensaje_id`, caso
  `mensajeria.bot_diferido`), sin doble saludo.
- **Costo:** `presupuestoAutonomoPct` (70 % del tope por defecto, `asistente_uso.tokens_autonomo`), 10 turnos
  por día a números sin cliente, 3 tickets por conversación y día, tokens contados aunque el turno se descarte.
- **Respaldo JSON:** solo herramientas de lectura y `pasar_a_persona`.
- **Eventos nuevos:** `conversacion.identidad_verificada`, `reconexion.solicitada`.

### El asistente agenda visitas (§17.10, `asistente_visitas.go`)

- Herramientas `ofrecer_visita` y `agendar_visita` (acción `agendar_visita`, apagada de fábrica). Las
  ventanas salen de `agenda.HuecosOfrecibles`; la orden, de `campo.CrearOrden` y `campo.ProgramarOrden`
  con el actor `asistente_ia` y un contexto que solo esas herramientas arman (`ctxVisitas`).
- Ajustes por empresa en `asistente_ajustes.visitas` (`VisitasAsistente`); ofertas en
  `mensajeria.asistente_ofertas_visita`.
- Eventos: `orden.creada` (origen `asistente`), `orden.programada`, `conversacion.vinculo_creado` y
  `asistente.accion_ejecutada` (`ofrecer_visita`, `agendar_visita`).

## Voz, etapa E1: llamadas de WhatsApp contestadas en el panel

Contrato: `.claude/integration/contratos/voz.md`. Código en `voz_*.go`, consultas en `voz.sql`.

- **Qué hace.** Una llamada entrante de WhatsApp (webhook `calls` de Meta) suena en el panel a
  quienes atienden la cola, están disponibles y tienen «Recibir llamadas» encendido con el panel
  abierto. Uno la reclama (reserva atómica de 12 s), el navegador negocia el audio **directo con
  Meta** y el servidor solo pasa la señalización: `reclamar` → `contestar` (`pre_accept`) →
  `confirmar-medio` (`accept`) → `colgar`. La llamada queda en la conversación como un mensaje de
  tipo `llamada`, con su nota posterior.
- **Dos fases con Meta.** Ninguna transacción espera a la Graph API: se guarda la intención
  (`accion_meta` pendiente), se llama a Meta sin transacción (4 s) y se guarda el resultado. El
  mantenimiento (job `mensajeria.mantenimiento_voz`, cada 5 s) da por fallida una llamada cuyo
  `pre_accept` o `accept` quedó a medias, pero no llama a Meta: los `reject` y `terminate` que
  decide, sus reintentos y los ajustes de número pendientes los hace el job `mensajeria.voz_meta`
  (`SKIP LOCKED`). El resultado de un `pre_accept` o `accept` solo vale si la llamada sigue en manos
  de quien tenía la reserva (`accion_meta_por`); con uno en vuelo, `soltar` y `reclamar` dan 409
  `accion_en_curso`.
- **Reserva por navegador.** `reclamar` ata la reserva al navegador de la cabecera
  `X-Iris-Navegador`; `contestar`, `confirmar-medio` y `soltar` exigen el mismo usuario y el mismo
  navegador (409 `otro_navegador` al otro). `Llamada.senales` lleva servicio y ticket abierto del
  cliente; «tiene saldo vencido» se pregunta a Argos en vivo solo en `GET /llamadas/{id}`.
- **Salientes (E1b, `voz_salientes.go`).** `POST /conversaciones/{id}/llamar` (solo
  `llamadas.llamar`, sensible; solo la empresa dueña del número; nunca un número libre) y el permiso
  del contacto (`llamada_permisos`, `solicitar-permiso-llamada`, respuesta por webhook). En fases:
  `call_permissions` → llamada `marcando` → `connect` → `call_id`; la respuesta SDP de Meta se valida
  y se lee con `GET /llamadas/{id}/respuesta-sdp`; `ACCEPTED` la pasa a `en_curso`. Límites por día,
  por número y tope de minutos del mes (`voz.tope_alcanzado`), decididos con un candado por empresa;
  el mantenimiento corta al cruzar el tope. Lo que queda vivo en Meta sin llamada en Iris va a
  `llamadas_huerfanas` y el job `mensajeria.voz_meta` lo termina. Emite `llamada.saliente_iniciada`,
  `llamada.permiso_solicitado` y `llamada.permiso_cambiado`.
- **Número prestado.** Una llamada que corresponde a la empresa prestataria no usa nada de la dueña
  (cola, horario, candidatos, textos): no suena y queda `perdida` en la prestataria.
- **El `connect` se procesa en línea** (`RecibirWebhookMeta`): es la excepción documentada a «los
  webhooks se procesan en un job». Es el mismo caso de uso que corre el job, idempotente por el
  `call_id`.
- **Mantenimiento:** perdida al vencer `segundosSonando` (con devolución pendiente y, si la ventana
  de 24 h está abierta, el mensaje automático, una vez por hora), reserva vencida, ampliar de la
  persona asignada a toda la cola, duración máxima y panel caído (15 s sin flujo).
- **Seguridad.** El SDP se valida antes de reenviarlo (`ValidarSDP`), nunca va a auditoría (solo su
  sha256), eventos, feed ni idempotencia, y se borra al terminar. Los eventos de llamada no llevan
  el número del cliente. El `call_id` de Meta y el token no
  salen por la API. Solo un candidato vivo de esa llamada puede reclamarla; después solo quien la
  reclamó sigue (los demás, 404). Llamadas solo en números por API.
- **Graph API detrás de una interfaz** (`GraphLlamadas`): todo lo de la Calling API es un supuesto a
  validar con un número de prueba; las pruebas usan un doble. Guion de la prueba manual:
  `docs/runbooks/voz-prueba-manual.md`.
- **Tablas:** `llamadas`, `llamada_tramos`, `llamada_candidatos`, `llamada_receptores`,
  `llamada_devoluciones`, `llamada_grabaciones`, `voz_ajustes`, `voz_ajustes_canal`.
- **Emite:** `llamada.entrante`, `llamada.contestada`, `llamada.terminada`, `llamada.perdida`,
  `llamada.rechazada`, `llamada.fallida`, `llamada.anotada`, `llamada.devolucion_pedida`,
  `llamada.devolucion_cerrada`, `canal_mensajeria.llamadas_cambiadas`, `voz.ajustes_cambiados` y
  `conversacion.mensaje_recibido` con `tipo: llamada`. No consume eventos.
- **Ajustes en WhatsApp:** encendido, icono, devolución y horario de llamadas (`call_hours`, espejo
  del horario de Iris cuando cabe en los límites de Meta) se escriben como un pendiente con
  reintentos (1 min, 5 min, 30 min, 2 h, 12 h; después `error`), con su estado en
  `GET /canales/{id}/llamadas` y `POST …/llamadas/aplicar` para reintentar.
- **Llamada perdida:** push a quien tiene la conversación y a quienes supervisan la cola (lo arma
  `notificaciones` con el tipo `bandeja`). **Buzón de voz:** el audio que llega hasta 5 minutos
  después de una llamada dejada al buzón se marca `buzonDeVoz`.
- **Fuera de E1:** la grabación de Meta es **E1c** (no se ofrece ni se acepta: 422
  `grabacion_no_disponible`; nunca se pide a Meta). Las salientes llegan con E1b.

## Servidor de IA («Iris Voz», `contratos/voz.md` §4)

Un servidor dedicado con GPU, de la instalación, que el asistente de chat usa como proveedor
(`servidor_ia`) por un canal autenticado: mTLS con huellas fijadas y firma por petición
(`internal/voz`). Solo Iris abre conexiones hacia él. Guía: `docs/runbooks/servidor-ia.md`.

- **Tarjeta** (`servidor_ia.ver` / `servidor_ia.administrar`, más `empresas.todas`):
  `GET /voz/motores`, `POST /voz/motores/enlazar` (código de un solo uso, guardado cifrado hasta
  enlazar o 15 min), `…/{id}/conectar` (el intercambio; el secreto inicial se deriva, no viaja),
  `…/activar` (con la `huellaTls` que comparó el admin: 409 `huella_cambiada` si ya no es esa; rota a un token aleatorio sin convivencia), `…/desactivar`, `…/probar`,
  `…/rotar-token` (`revocar: true` = sin convivencia y certificado de cliente nuevo; lo nuevo se
  guarda como «siguiente» antes de entregarlo y una rotación a medias se termina sola; una a la
  vez: 409 `rotacion_en_curso`),
  `…/empresas/actualizar` y `…/modelos/actualizar`.
- **Proveedor del asistente** (`servidor_ia_proveedor.go`): el adaptador compatible con OpenAI con
  otro transporte. Toda llamada al modelo toma una **plaza** (tope del servidor, máximo por empresa y
  una plaza garantizada por empresa activa; sin cola: `servidor_ia_ocupado` al instante), anota el
  uso por minuto, empresa y modelo (`servidor_ia_uso`, sin texto) y cuenta para el tope de tokens
  `max(lo que declara el servidor, la estimación propia)` con techo (la estimación cuenta también
  los pasos del turno y las herramientas; un error después de enviar anota la estimación). Con el servidor caído, degradado,
  incompatible o desactivado falla al instante sin llamar (`servidor_ia_no_disponible`).
- **Latido** (job `mensajeria.servidor_ia`, cada 10 s): `/salud`, compatibilidad en los dos
  sentidos, catálogo revalidado, huella siguiente del servidor (solo se acepta anunciada por el
  canal vigente), rotación automática del token cada 30 días y limpieza de códigos y plazas
  vencidos. Tres fallos seguidos → `no_disponible`. En la misma pasada lee `/metricas`, lo
  revalida entero y guarda la **muestra** (`servidor_ia_muestras`; una lectura que no valida o que
  no llega cuenta como fallida), abre o cierra los **episodios de aviso** (`servidor_ia_avisos`:
  caído, incompatible, VRAM y temperatura sostenidas, disco y saturación) y, cada 5 minutos, agrega
  por hora (`servidor_ia_muestras_hora`) y aplica la retención.
- **Tablero y umbrales**: `GET /servidor-ia/tablero`, `GET /servidor-ia/umbrales`,
  `POST /servidor-ia/umbrales/actualizar`. El tablero trae el estado, la última muestra buena del
  servidor (GPU, CPU, memoria, disco y modelos), las series del rango (GPU y VRAM de las muestras;
  peticiones y tiempos del uso), el uso por empresa y los episodios de aviso. Quien solo tiene
  `servidor_ia.ver_uso` ve el estado sin detalle y el uso de su empresa, nada del servidor.
- **Avisos**: `notificaciones` escucha `voz.motor_estado_cambiado` (caído, incompatible) y
  `servidor_ia.umbral_superado` y avisa por campana y correo a quienes tienen `servidor_ia.ver` y
  todas las empresas, como mucho uno por tipo, servidor y hora. Los episodios tienen histéresis
  (se abren con toda la ventana por encima y se cierran con toda por debajo; caído e incompatible,
  tras 6 lecturas seguidas fuera de ese estado) y la muestra completa solo se guarda de la última
  lectura buena.

Eventos (todos con `empresa_id: null`): `voz.motor_enlazado`, `voz.motor_activado`,
`voz.motor_desactivado`, `voz.motor_estado_cambiado`, `voz.motor_token_rotado`,
`voz.motor_empresas_cambiadas`, `voz.motor_modelos_cambiados`, `servidor_ia.ajustes_cambiados`,
`servidor_ia.umbral_superado` y `servidor_ia.umbral_normalizado`.
No consume eventos. Secretos cifrados con `IRIS_CLAVE_CIFRADO` (los rota `iris clave rotar`): el
token y el token siguiente, la clave del certificado de cliente (la vigente y la siguiente) y el
código de enlace pendiente.
