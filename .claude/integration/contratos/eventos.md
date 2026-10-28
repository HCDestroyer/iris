# Payloads de eventos de Iris

Todo evento sale por `integracion.outbox` con esta forma: `{ id (uuid v7), evento, ocurrido_en, empresa_id, payload }`. Cada agente agrega aquí el detalle de sus eventos, en la misma PR que el código que los emite. El catálogo resumido está en `.claude/CLAUDE.md`.

## Identidad (núcleo, I1)

Emite el módulo `identidad`, con `empresa_id = null` (los usuarios de Iris no pertenecen a una empresa), salvo los de empresas y membresía, que llevan la empresa afectada.

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `usuario.creado` | `usuario_id`, `tipo` | Alta de un usuario (`usuarios.crear`, también desde `iris admin crear`). |
| `usuario.actualizado` | `usuario_id`, `campos[]` (nombres de los campos que cambiaron) | `usuarios.editar`. |
| `usuario.desactivado` | `usuario_id`, `motivo` | `usuarios.desactivar`. Revoca sus sesiones; sus tokens dejan de autenticar mientras esté inactivo, sin revocarse ni emitir `token.revocado`. |
| `token.creado` | `token_id`, `usuario_id` | Alta de un token `iris_pat_`. |
| `token.revocado` | `token_id`, `usuario_id`, `motivo` | `tokens.revocar`. |
| `empresa.creada` | `empresa_id`, `origen` (`manual`) | `empresas.crear` (un administrador registra una empresa de Argos). Lleva `empresa_id` = la empresa. Las que registra el canje del conector no emiten este evento (van en `conector.creado`). |
| `empresa.actualizada` | `empresa_id`, `campos[]` | `empresas.editar` (nombre, NIT). |
| `usuario.empresa_asignada` | `usuario_id`, `empresa_id` | `usuarios.asignar_empresa`, y `usuarios.crear` con `empresas` (uno por empresa). Lleva `empresa_id` = la empresa asignada. |
| `usuario.empresa_quitada` | `usuario_id`, `empresa_id` | `usuarios.quitar_empresa`. |
| `empresa.datos_adoptados` | `empresa_id`, `filas` (objeto `{"esquema.tabla": n}` con cada tabla operativa registrada, también las de 0) | `empresas.adoptar` (`POST /empresas/{id}/adoptar-sin-empresa`, solo admin global): los datos guardados con `empresa_id` null en modo sin conector pasan a la empresa. Lleva `empresa_id` = la empresa. Los consumidores que cachean por empresa deben releer esas tablas. |

## Integraciones (núcleo, I1)

Emite `integracion/argos`. Los de conectores van con `empresa_id = null`; `argos.servicio_retirado`
lleva la empresa del servicio según Argos.

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `conector.creado` | `conector_id`, `sistema` (`argosisp`), `origen` (`enlace_remoto` \| `enlace_local`) | Se canjeó un enlace de conexión. El conector nace apagado. |
| `conector.activado` | `conector_id`, `sistema` | `integraciones.activar`. |
| `conector.desactivado` | `conector_id`, `sistema`, `motivo` | `integraciones.desactivar`. |
| `argos.servicio_retirado` | `servicio_id`, `cliente_id` (o null), `de`, `a` (`retirado`), `entrante_id` | Argos avisó `servicio.estado_cambiado` con `a = retirado` **y** Iris lo confirmó releyendo `GET /servicios/:id` en Argos (ids y empresa salen de esa lectura, no del payload). Gancho para que I2 proponga la orden de retiro (Fase 2) si el servicio tiene equipos en comodato. Una vez por webhook (deduplicado por el `id` firmado del cuerpo). |
