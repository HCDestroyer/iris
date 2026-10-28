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
  alcance para la acción, 403 `fuera_de_alcance`. El ticket es de la empresa activa (otra en el
  cuerpo: 422 `empresa_no_coincide`; una persona sin empresa activa: 422 `empresa_requerida`).
  El aislamiento depende del middleware de empresa activa de I1.
- **Origen:** `correo`, `telegram`, `horus` y `argos` solo los declara una integración o el
  sistema; `portal`, solo el canal portal (422 `origen_no_permitido`).
- **Asignar:** el responsable es una persona activa con `tickets.ver` y alcance sobre el ticket
  (global, su zona o la cuadrilla asignada); si no, 422 en `usuarioId`.
- **Idempotencia:** `crear`, `responder` y `nota-interna` guardan la `Idempotency-Key` (por
  credencial, índice único). Si la clave se reclama y la petición se repite, devuelven lo que
  ya se creó en vez de duplicarlo; las demás acciones lo toleran con `version` y su estado.
- **Argos:** guarda `cliente_id`, `servicio_id` y `zona_id` (opcionales) y, sin conector, los
  datos de contacto capturados a mano. No llama a Argos.

## Eventos que emite

`ticket.creado`, `ticket.asignado`, `ticket.estado_cambiado`, `ticket.respondido`,
`ticket.escalado`, `ticket.fusionado`, `ticket.sla_en_riesgo`, `ticket.sla_vencido`. Payloads en
`.claude/integration/contratos/eventos.md`.

## Eventos que consume

Ninguno todavía (el webhook de Horus y la entrada por correo y portal van en otra PR).

## Pruebas

`IRIS_DB=iris_test_i2 go test ./internal/soporte/` (Postgres real): calendario (horario, fin de
semana, feriados y medio día), transiciones válidas e inválidas, permisos y alcance, auditoría y
outbox, `version` → 409, idempotencia por HTTP, pausa del SLA y cron idempotente.
`revision_test.go` cubre los hallazgos de la revisión de la PR #5.
