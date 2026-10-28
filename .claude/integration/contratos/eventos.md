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

Emiten `integracion/conector` (los `conector.*`, de Argos y de Horus) e `integracion/argos`. Los de
conectores van con `empresa_id = null`; `argos.servicio_retirado` lleva la empresa del servicio
según Argos.

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `conector.creado` | `conector_id`, `sistema` (`argosisp` \| `horus`), `origen` (`enlace_remoto` \| `enlace_local`) | Se canjeó un enlace de conexión. El conector nace apagado. |
| `conector.activado` | `conector_id`, `sistema` | `integraciones.activar`. |
| `conector.desactivado` | `conector_id`, `sistema`, `motivo` | `integraciones.desactivar`. |
| `conector.actualizado` | `conector_id`, `sistema`, `campos[]` (hoy `url_publica`) | `integraciones.configurar_url` (`POST /integraciones/conectores/{id}/url-publica`), solo si cambió algo. |
| `argos.servicio_retirado` | `servicio_id`, `cliente_id` (o null), `de`, `a` (`retirado`), `entrante_id` | Argos avisó `servicio.estado_cambiado` con `a = retirado` **y** Iris lo confirmó releyendo `GET /servicios/:id` en Argos (ids y empresa salen de esa lectura, no del payload). Gancho para que I2 proponga la orden de retiro (Fase 2) si el servicio tiene equipos en comodato. Una vez por webhook (deduplicado por el `id` firmado del cuerpo). |

## Horus (núcleo, I1)

Emite `integracion/horus` al procesar los webhooks de Horus (contrato `horus.md` §2): el job valida
y normaliza el payload y lo publica con **el mismo nombre** que el webhook. **No crea tickets**:
soporte (I2) se suscribe (`bus.Suscribir("soporte.horus", "horus.router_estado_cambiado", fn)`) y
decide. Uno por webhook (deduplicado por su `id` firmado; el job es idempotente), pero un
suscriptor debe tolerar recibirlo dos veces (relay). `empresa_id` va en el sobre y en el payload
(null si no se supo). Fechas en UTC (RFC 3339). Todos llevan `entrante_id`
(`integracion.entrantes.id`, para rastrear).

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `horus.router_estado_cambiado` | `router_id` (id del router **en Argos**; supuesto), `nombre`, `estado` (`caido` \| `arriba`), `ocurrido_en`, `empresa_id` (o null), `entrante_id` | Un router cae o vuelve. `empresa_id`: la del payload; si no viene, la del router en la instantánea `integracion.argos_routers`; si tampoco, null. |
| `horus.servicio_infra_cambiado` | `infra_id` (texto, id en Horus), `nombre`, `tipo` (minúsculas: `olt`, `enlace`…), `estado` (`caido` \| `arriba`), `router_id` (o null), `ocurrido_en`, `empresa_id` (o null), `entrante_id` | Igual, para servicios de infraestructura (OLT, enlaces). |
| `horus.hallazgo_abierto` | `hallazgo_id` (texto), `categoria` (minúsculas), `severidad` (minúsculas o `""`), `ip` (o null), `router_id` (o null), `resumen`, `url_horus` (o null), `cliente_id`, `servicio_id`, `empresa_id` (o null), `fuente_cliente` (`argos` \| `instantanea` \| null), `motivo_sin_cliente` (null si se resolvió; si no: `sin_ip`, `ip_invalida`, `conector_argos_apagado`, `ip_sin_cliente`, `argos_no_disponible`, `argos_rechazo`, `otra_empresa`), `ocurrido_en`, `entrante_id` | Horus abre un hallazgo de seguridad. El cliente sale de Argos (IP → cliente, A8 con respaldo a `GET /ip/{ip}` y a la instantánea si Argos no responde); con `empresa_id` en el payload se busca solo en esa empresa (`otra_empresa` si la IP es de otra). `url_horus` solo si es del mismo origen que el conector (su URL base o la pública). |
| `horus.hallazgo_resuelto` | `hallazgo_id`, `categoria`, `resumen`, `ocurrido_en`, `empresa_id` (o null), `entrante_id` (sin IP ni cliente: se vincula por `hallazgo_id`) | Horus cierra el hallazgo (soporte agrega la nota al ticket vinculado por `hallazgo_id` y propone cerrarlo). |

Un payload inválido (sin `router_id`, `estado` que no es caído/arriba, hallazgo sin `hallazgo_id`
o sin `categoria`) o un evento desconocido no emite nada: el entrante queda «ignorado» con el
motivo en `integracion.entrantes.error`.

## Soporte (I2)

Emite el módulo `soporte`, con `empresa_id` = la empresa del ticket (null sin conector). Las fechas van en UTC (RFC 3339).

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `ticket.creado` | `ticket_id`, `numero`, `empresa_id`, `cliente_id` (o null), `servicio_id` (o null), `categoria_id` (o null), `categoria` (código o null), `prioridad`, `origen`, `ticket_padre_id` (o null) | `tickets.crear`. |
| `ticket.estado_cambiado` | `ticket_id`, `de`, `a`, `motivo` (o null) | Toda transición: asignar o responder (`nuevo → abierto`), poner en espera, reanudar, resolver, reabrir, cerrar y fusionar (el duplicado pasa a `cerrado`). |
| `ticket.asignado` | `ticket_id`, `usuario_id` (o null), `cuadrilla_id` (o null) | `tickets.asignar` (ambos null = sin asignar). |
| `ticket.respondido` | `ticket_id`, `mensaje_id` | `tickets.responder`: mensaje público al cliente (para avisarle por portal, correo o Argos). Las notas internas no emiten evento. |
| `ticket.escalado` | `ticket_id`, `de`, `a` (prioridades; iguales si solo sube el nivel), `nivel`, `motivo` | `tickets.escalar`. |
| `ticket.fusionado` | `ticket_id` (el duplicado), `destino_id`, `motivo` (o null) | `tickets.fusionar`. |
| `ticket.sla_en_riesgo` | `ticket_id`, `sla` (`respuesta` \| `resolucion`), `vence_en` | Cron: el reloj pasó el 80 % de su objetivo. Una vez por ticket, reloj y `vence_en` (si el vencimiento se corre, el nuevo plazo vuelve a alertar); no se emite si ya venció. |
| `ticket.sla_vencido` | `ticket_id`, `sla` (`respuesta` \| `resolucion`), `vence_en` | Cron: el reloj venció sin cumplirse. Una vez por ticket, reloj y `vence_en`. |
