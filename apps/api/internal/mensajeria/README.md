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
| `GET/POST /canales`, `POST /canales/{id}/actualizar\|activar\|desactivar` | `mensajeria.configurar` | WhatsApp Cloud API por empresa (credenciales cifradas, nunca se devuelven). |
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
  los dos de una directa y el despacho que se une a un hilo de orden (`ordenes.ver` con alcance).
  Las directas nunca se ven por alcance. Lo que no se ve es 404.
- **Mensajes** (`chat_mensajes`): id uuid v7 del cliente (mismo id y contenido → 200, otro contenido
  → 409; también `Idempotency-Key`), texto plano ≤ 4000, una foto (`nucleo/adjuntos`, entidad
  `chat_conversacion`, registrada con `RegistrarAdjuntos` en cmd/iris y cmd/worker) y/o ubicación.
  `seq` por conversación en orden de llegada. Nunca se editan; el autor los elimina en 15 min
  (lápida, sin contenido). 30 por minuto por usuario (429), con el reloj del módulo.
- **Lecturas** (`chat_lecturas`): puntero por usuario que solo avanza; no leídos = mensajes de otros,
  sin eliminar, después del puntero.
- **Tiempo real:** `chat_eventos` es el feed (`mensaje_nuevo`, `mensaje_eliminado`, `leido`,
  `conversacion_actualizada`). Cada escritura toma el candado de su empresa hasta el COMMIT y hace
  `NOTIFY iris_chat '<empresa>'`: dentro de una empresa los ids quedan en orden de COMMIT y el cursor
  nunca salta un evento. `EscucharChat` (cmd/mensajeria) despierta los flujos SSE de esa empresa;
  cada flujo relee el feed con la misma consulta que `GET /chat/cambios` (sondeo), filtrada por lo
  que el usuario ve. Latido cada 25 s, cierre a los 30 min (reconexión con Last-Event-ID) y purga a
  los 7 días en el mantenimiento (cursor más viejo → 410). El flujo quita los plazos de escritura y
  lectura del servidor; el proxy de la API (`servidor.ProxyHacia`) también, para GET con
  `Accept: text/event-stream`, y ReverseProxy copia cada evento sin búfer.
- **Auditoría:** abrir una conversación nueva, enviar y eliminar, con el id del mensaje y nunca el
  texto. Listar, ver, leer, cambios y el flujo no dejan fila. Los logs no llevan cuerpos.
- `Config`: `ChatPorMinuto` (30), `ChatPlazoEliminar` (15 min), `ChatLatido` (25 s),
  `ChatDuracionMax` (30 min), `ChatRetencion` (7 días).

Emite `chat.conversacion_creada`, `chat.mensaje_enviado` (con `destinatarios[]` para el futuro emisor
de push) y `chat.mensaje_eliminado`. No emite `chat.leido` (ver `contratos/eventos.md`). No consume
eventos.
