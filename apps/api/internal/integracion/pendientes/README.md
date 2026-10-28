# pendientes (integración)

Cola de acciones de Iris hacia Argos (`integracion.pendientes`; contrato `argosisp.md` §0 y §3).

## Qué hace

- **Suscriptor de `orden.cerrada`** (actor sistema, idempotente por la Idempotency-Key única):
  arma las llamadas del §3 — instalación exitosa → `activar-instalacion` + un comodato por equipo
  instalado; retiro exitoso → devolución (o pérdida si `condicion = perdido`) por equipo y
  `retirar` si `retirar_servicio`; cambio de equipo → devoluciones y luego asignaciones
  (`depende_de`); materiales → un consumo con `origen_tipo = iris_orden`; cargo único si la orden
  lo trae. Si falta un dato, la acción nace `fallido_negocio` con `datos_incompletos` (nunca se
  adivina). Sin conector Argos activo no encola y emite `integracion.pendiente_omitido`. Payload
  esperado: `contratos/eventos.md`, «`orden.cerrada`: lo que la cola espera de campo».
- **Job** `integracion.pendientes_procesar` (River, cola `integracion`, cada minuto y al encolar):
  procesa todo lo vencido cuyas dependencias estén hechas, por el cliente anti-SSRF del conector y
  con `Idempotency-Key: iris:<operacion>:<id>`. 2xx → `hecho`; 4xx de negocio → `fallido_negocio`
  (detail de Argos sin credenciales); red, 5xx, 401/403, 408, 409, 425, 429 → reintento a 1 min,
  5 min, 30 min, 2 h y 12 h; el 6.º fallo → `agotado`. Un `en_curso` de más de 15 min (worker
  caído) se retoma. Reloj inyectable (`Config.Reloj`).
- `GET /api/v1/integraciones/pendientes?entidad=&entidadId=&estado=` (`integraciones.ver_pendientes`)
  y `POST /api/v1/integraciones/pendientes/{id}/reintentar { version }` (`integraciones.reintentar`,
  auditado).
- Para campo y el panel: `pendientes.DeEntidad(ctx, q, "orden", id)` y `ConPendienteEnArgos(ps)`
  («cerrada con pendiente en Argos»).

## Eventos

Consume `orden.cerrada`. Emite `integracion.pendiente_hecho`, `_fallido`, `_agotado`,
`_reintentado` y `_omitido` (contratos/eventos.md, «Pendientes hacia Argos»).
