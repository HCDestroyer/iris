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

Al pedir el aviso, en este orden: permiso registrado (`sin_permiso`), baja vigente (`baja`), teléfono
de la instantánea de Argos, refrescada si tiene más de 24 h (`sin_contacto`), canal de la empresa que
admite avisos con la plantilla aprobada (`canal_no_disponible`) y 3 avisos automáticos por cliente y
día (`limite_diario`; `urgente` lo salta, solo `corte_programado`). Al enviar: horario de silencio
21:00–07:00 Guatemala (espera; al terminar, si venció su variable de caducidad, `fallido`/`vencido`),
20 por minuto por número remitente y pausa del número si Meta baja su calidad o corta por spam.
Fallas del canal: `numero_invalido`, `rechazado_por_canal`; temporales se reintentan y al final
`agotado`. Un aviso en cola sin acuse en 72 h también termina `agotado`.

Canales: `Adaptador` (WhatsApp Cloud API y el simulado). El simulado solo con
`IRIS_MENSAJERIA_SIMULADO=true` (dev y CI); sin él, una empresa sin canal da `canal_no_disponible`.
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
