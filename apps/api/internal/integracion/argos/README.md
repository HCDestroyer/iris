# integracion/argos

Conector de **lectura** con ArgosISP (`.claude/integration/contratos/argosisp.md` §1–§2).
Escrituras en Argos y cola de `pendientes`: Fase 2.

## Qué hace

- **Enlace de conexión (§1.3), en los dos sentidos.** `POST /integraciones/argosisp/conectar
  { enlace }` canjea un enlace de Argos (Iris llama a `POST /api/v1/integraciones/iris/conexion/canjear`
  de Argos con su URL, la URL del webhook y el secreto que genera). `POST
  /integraciones/argosisp/conexion/enlaces` genera un enlace de Iris (15 min, un solo uso) que Argos
  canjea en `POST /integraciones/argosisp/conexion/canjear` (sin credenciales, autoriza el código).
  El token `atk_…` y el secreto del webhook se guardan cifrados (AES-256-GCM con
  `IRIS_CLAVE_CIFRADO`, atados a la fila) y nunca salen por la API ni en la auditoría (`***`).
- **Activar / desactivar** (`POST /integraciones/conectores/{id}/activar|desactivar`, con
  `version`): nace apagado; uno activo por sistema. Activar encola la carga de zonas y routers.
- **Cliente HTTP** (`cliente.go`): timeout por llamada (`IRIS_ARGOS_TIMEOUT`), 3 intentos con
  espera exponencial en lecturas ante red, 429 y 5xx, sin seguir redirecciones, problem+json
  mapeado (`Problema`): 404 → 404, 401/403 → 502 `argos_sin_permiso`, caída → 503
  `argos_no_disponible`, otro 4xx → 502 `argos_rechazo`. Lee JSON en snake_case o camelCase,
  envuelto o no en `{ data }`.
- **Lecturas** (`GET /clientes?q=`, `GET /clientes/{id}`, `GET /clientes/resolver-ip?ip=`),
  permiso `clientes.ver` (+ `clientes.estado_cuenta` para el estado de cuenta). Auditadas
  (`clientes.buscar`, `clientes.ver`, `clientes.resolver_ip`). Sin conector activo: 503
  `conector_apagado`. Sin transacción abierta durante la llamada HTTP (dos casos de uso cortos).
- **Instantáneas** (`argos_clientes`, `argos_servicios`, `argos_zonas`, `argos_routers`): solo lo
  que se muestra, con `empresa_id` y `sincronizado_en`. La ficha se refresca si tiene más de 24 h;
  si Argos no responde se devuelve la última con `stale: true` (y la búsqueda cae a las
  instantáneas locales). El estado de cuenta nunca se guarda.
- **Webhooks** (`POST /integraciones/argosisp/webhook/{conector_id}`): firma HMAC-SHA256
  (`X-Webhook-Signature: v1=<hex>` sobre `"<timestamp>.<cuerpo>"`), ventana
  `IRIS_WEBHOOK_VENTANA`, guardado en `integracion.entrantes` deduplicado por `X-Webhook-Id` y job
  de River en la misma transacción. El job (`ProcesarEntrante`) es idempotente.

## Eventos

- **Emite:** `conector.creado`, `conector.activado`, `conector.desactivado` y
  `argos.servicio_retirado` (gancho para la orden de retiro de I2). Detalle en
  `.claude/integration/contratos/eventos.md`.
- **Consume (webhooks de Argos, §2.3):** `cliente.actualizado`, `servicio.creado`,
  `servicio.config_cambiada`, `asignacion.cambiada` y `servicio.estado_cambiado` → refrescan la
  instantánea del cliente si ya está en ella (si no, «ignorado»). Los demás se guardan como
  «ignorado» (Fase 2).

## Jobs (cmd/worker)

`integracion.argos_procesar_entrante` (uno por entrante, hasta 12 intentos) e
`integracion.argos_sincronizar_catalogos` (cada 6 h y al activar).
