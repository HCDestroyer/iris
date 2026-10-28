# integracion/horus

Conector con Horus Flow (`.claude/integration/contratos/horus.md`), sobre la base común de
`integracion/conector` (mismo mecanismo que Argos: enlace en los dos sentidos bajo
`/api/v1/integraciones/horus`, `sistema = "horus"`, token `hf_pat_…` y secreto del webhook
cifrados, apagado hasta activar, uno activo, permisos `integraciones.*`, SSRF con
`IRIS_HORUS_REDES_PERMITIDAS`).

## Qué hace

- **Webhooks de §2** (`POST /api/v1/integraciones/horus/webhook/{conector_id}`): mismas garantías
  que los de Argos. El job (`integracion.horus_procesar_entrante`) **no crea tickets**: valida y
  normaliza el payload (`eventos.go`) y lo publica por el outbox como evento interno con el mismo
  nombre. Para un hallazgo con IP pregunta a Argos de quién es (`argos.ResolverIPInterno`: A8 con
  respaldo a `GET /ip/{ip}` y a la instantánea) y agrega `cliente_id`/`servicio_id`/`empresa_id`
  o `motivo_sin_cliente`. Sin `empresa_id`, un router toma la de la instantánea de routers de
  Argos. Payload inválido o evento desconocido: entrante «ignorado» con la nota en `error`.
- **Lecturas** (`lecturas.go`, sin ruta todavía): `ConsultarSaludServicio` (consumo y salud del
  servicio por `servicio_id` de Argos) y `ConsultarRouter`, permiso `clientes.ver`, solo de la
  empresa activa según la instantánea de Argos, auditadas, en dos fases sin transacción abierta
  durante la llamada.

## Eventos

- **Emite** (outbox, para soporte): `horus.router_estado_cambiado`, `horus.servicio_infra_cambiado`,
  `horus.hallazgo_abierto`, `horus.hallazgo_resuelto` (payloads en
  `.claude/integration/contratos/eventos.md`) y, por el núcleo, `conector.*` con `sistema = horus`.
- **Consume** (webhooks de Horus, §2): los cuatro de arriba; los demás quedan «ignorado».
