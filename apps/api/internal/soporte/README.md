# soporte

Tickets de soporte con SLA por categoría y prioridad, calendario laboral y feriados de Guatemala,
conversación (mensajes públicos y notas internas), historial y cron de SLA. Schema `soporte`.

## Qué hace

- **Acciones** (`POST /api/v1/tickets/{id}/<acción>`, permiso `tickets.<acción>`; nunca PATCH):
  `crear` (`POST /tickets`), `asignar`, `responder`, `nota-interna`, `escalar`,
  `poner-en-espera`, `reanudar`, `resolver`, `reabrir`, `cerrar`, `fusionar`. Lecturas con
  `tickets.ver`: `GET /tickets` (cursor y filtros), `/tickets/{id}`, `/tickets/{id}/mensajes`,
  `/tickets/{id}/eventos`, `/categorias`, `/slas`. Las notas internas (y el motivo de una fusión
  en el historial) solo salen con `tickets.ver_notas` y nunca por el canal `portal`.
- **Estados:** `nuevo → abierto` (asignar o responder) `⇄ en_espera` (poner en espera / reanudar)
  `→ resuelto` (desde nuevo, abierto o en espera) `→ abierto` (reabrir) `→ cerrado` (desde
  resuelto; desde nuevo, abierto o en espera con motivo; fusionar). Cerrado es final. Una acción
  fuera de su estado responde 422 `transicion_invalida`; con `version` vieja, 409.
- **SLA:** el ticket copia al crearse los objetivos (minutos de respuesta y de resolución) del SLA
  de su categoría, o del SLA por defecto, para su prioridad. Con calendario cuentan solo los
  tramos laborales fuera de feriados (hora de `America/Guatemala`); sin calendario, 24/7.
  `en_espera` pausa la resolución (no la respuesta); el tiempo en `resuelto` también es pausa si se
  reabre. Escalar la prioridad recalcula desde el inicio los relojes que no se cumplieron (una
  primera respuesta ya dada conserva su objetivo y vencimiento). Riesgo = 80 %.
- **Cron** (`TrabajadorSLA`, River, cada minuto, único por minuto): procesa todo lo pendiente
  hasta ahora y emite `ticket.sla_en_riesgo` y `ticket.sla_vencido` una sola vez por ticket,
  reloj (`respuesta`/`resolucion`), umbral y vencimiento (`soporte.ticket_sla_alertas`): si el
  vencimiento se corre (reanudar, reabrir, escalar), el nuevo plazo vuelve a alertar.
- **Alcance:** las listas y acciones respetan el alcance del permiso (zona o cuadrilla del ticket,
  o asignado al actor) y la empresa activa. Fuera del alcance de lectura, 404; visible pero sin
  alcance para la acción, 403 `fuera_de_alcance`.
- **Empresa activa** (middleware de I1): con empresa activa, solo sus tickets (otro id = 404) y
  se crea en ella (otra en el cuerpo: 422 `empresa_no_coincide`). **Sin conector** (sin empresas
  registradas) solo los de `empresa_id` null, y se crea con null. El actor sistema ve todas y,
  con empresas registradas, indica una registrada al crear. Al asignar, el responsable debe
  operar en la empresa del ticket (422 `asignado_fuera_de_empresa`). `soporte.tickets`,
  `ticket_mensajes` y `ticket_eventos` se registran para la adopción
  (`POST /empresas/{id}/adoptar-sin-empresa`).
- **Origen:** `correo`, `telegram`, `horus` y `argos` solo los declara una integración o el
  sistema; `portal`, solo el canal portal (422 `origen_no_permitido`).
- **Asignar:** el responsable es una persona activa con `tickets.ver` y alcance sobre el ticket
  (global, su zona o la cuadrilla asignada); si no, 422 en `usuarioId`.
- **Idempotencia:** `crear`, `responder` y `nota-interna` guardan la `Idempotency-Key` (por
  credencial, índice único). Si la clave se reclama y la petición se repite, devuelven lo que
  ya se creó en vez de duplicarlo; las demás acciones lo toleran con `version` y su estado.
- **Argos:** guarda `cliente_id`, `servicio_id` y `zona_id` (opcionales) y, sin conector, los
  datos de contacto capturados a mano. No llama a Argos.

## Entrada por correo (IMAP) y respuesta por SMTP

Paquete de protocolo en `correo/` (IMAP con go-imap v2, MIME con go-message, SMTP con
`net/smtp`); la lógica, en `buzones.go` y `correo_*.go`.

- **Buzones** (`soporte.buzones`, `/api/v1/buzones`): `GET` lista y detalle (`buzones.ver`), `POST`
  crear (`buzones.crear`, nace inactivo), `POST /{id}/editar` (`buzones.editar`, con `version`),
  `/activar` y `/desactivar` (`buzones.activar`/`desactivar`) y `/probar` (`buzones.editar`).
  Empresa: la activa (null sin conector), como los tickets. Solo TLS (`tls` implícito o `starttls`
  obligatorio; si el servidor no ofrece STARTTLS se corta antes del LOGIN) con el certificado
  verificado. Contraseñas IMAP y SMTP cifradas con `IRIS_CLAVE_CIFRADO` (AES-GCM, atadas al
  buzón); nunca salen en respuestas, logs, errores ni auditoría (`***`). Editar host, puerto,
  TLS o usuario exige volver a escribir la contraseña. SSRF: IP interna escrita → 422
  `destino_no_permitido`; al conectar, `argos.DestinoPermitido` sobre la IP resuelta, salvo
  `IRIS_CORREO_REDES_PERMITIDAS`.
- **Lectura** (`soporte.programar_buzones` cada minuto → `soporte.leer_buzon`, único por buzón):
  UID mayores que el guardado con la misma UIDVALIDITY (la primera vez, o si cambió, solo los no
  leídos). Cada correo se guarda en `correos_entrantes` (único por buzón y `Message-ID`) y avanza
  `ultimo_uid` en la misma transacción; solo después se marca leído (o se mueve a la carpeta de
  procesados). Más grande que `IRIS_CORREO_TAMANO_MAX`: solo cabeceras. Errores resumidos en
  `buzones.ultimo_error`.
- **Proceso** (`soporte.procesar_correos`, único por buzón, en orden): autorespuestas y rebotes
  (`Auto-Submitted`, `Precedence`, `X-Autoreply`, MAILER-DAEMON/postmaster, `multipart/report`,
  `Return-Path: <>`) y correos del propio buzón se ignoran. Respuesta si `In-Reply-To`/`References`
  apuntan a una respuesta enviada o a un correo ya ligado (mismo buzón), o si el asunto trae
  `[TK-…]` **y** el remitente es el contacto del ticket; el ticket debe ser de la empresa del
  buzón. Se agrega como mensaje público del cliente (`autor_tipo = cliente`, canal `correo`, sin
  la cita): `en_espera → abierto` (reanuda) y `resuelto → abierto` (reabre); si estaba fusionado,
  va al destino; si está cerrado, abre otro que lo menciona. Si no, ticket nuevo (origen `correo`,
  categoría y prioridad del buzón, remitente como contacto). HTML → texto (sin scripts ni
  estilos), adjuntos solo como metadatos (se listan en el mensaje).
- **Salida** (suscriptor de `ticket.respondido` → `soporte.enviar_correo`): mensaje público de un
  agente en un ticket de origen correo → un `correos_salientes` por mensaje (idempotente), con
  `Re: [TK-…] asunto`, `In-Reply-To`/`References` al último correo del cliente, `Auto-Submitted:
  auto-replied` y `Message-ID` fijo (un reintento sale igual). Sin SMTP en el buzón, no sale.

## Eventos que emite

`ticket.creado`, `ticket.asignado`, `ticket.estado_cambiado`, `ticket.respondido`,
`ticket.escalado`, `ticket.fusionado`, `ticket.sla_en_riesgo`, `ticket.sla_vencido`,
`correo.recibido`, `buzon.creado`, `buzon.actualizado`, `buzon.activado`, `buzon.desactivado`.
Payloads en `.claude/integration/contratos/eventos.md`.

## Eventos que consume

`ticket.respondido` (salida por correo; idempotente por mensaje). El webhook de Horus y el
portal van en otra PR.

## Pruebas

`IRIS_DB=iris_test_i2 go test ./internal/soporte/` (Postgres real): calendario (horario, fin de
semana, feriados y medio día), transiciones válidas e inválidas, permisos y alcance, auditoría y
outbox, `version` → 409, idempotencia por HTTP, pausa del SLA y cron idempotente.
`revision_test.go` cubre los hallazgos de la revisión de la PR #5 y `empresa_test.go` la empresa
activa (aislamiento A/B por HTTP, modo sin conector, asignación y adopción). `correo_test.go`, con
un IMAP en memoria (`imapmemserver`) y un SMTP falso con TLS de una CA de prueba
(`correo_falso_test.go`): ticket nuevo, respuesta por número y por `In-Reply-To`, duplicado por
`Message-ID`, autorespuesta y rebote, HTML, reanudación por UID, otra empresa, contraseña cifrada y
nunca expuesta, TLS exigido (sin STARTTLS no hay LOGIN; CA desconocida), SSRF, envío idempotente y
las rutas HTTP. `correo/mensaje_test.go`: HTML, cita, charsets y cabeceras sin inyección.
