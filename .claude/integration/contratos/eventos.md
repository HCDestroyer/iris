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

## Adjuntos (núcleo, I1)

Emite `nucleo/adjuntos`, con `empresa_id` = la de la entidad dueña (null sin conector). Base común
del payload: `adjunto_id` (uuid del cliente), `entidad` (`orden`, `ticket`…), `entidad_id` (texto),
`tipo` (`foto` | `firma` | `documento`), `clave_checklist` (o null).

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `adjunto.registrado` | base | Alta nueva (`POST /adjuntos`); repetir el alta con el mismo id no lo vuelve a emitir. |
| `adjunto.completado` | base + `mime`, `tamano`, `sha256` | Llegó el último byte y se verificaron tamaño, SHA-256 y que el contenido es del MIME declarado; ya está en el almacenamiento. Campo (I2) puede reintentar el cierre de la orden que esperaba sus adjuntos. |
| `adjunto.rechazado` | base + `motivo` (`sha256_distinto` \| `mime_distinto` \| `tamano_excedido` \| `abandonado`) | La subida no coincidió con lo declarado, o quedó sin actividad más de `IRIS_ADJUNTOS_ABANDONO` (48 h; job del worker, borra sus partes); el teléfono debe registrar el adjunto de nuevo con otro id. |

## Pendientes hacia Argos (núcleo, I1)

Emite `integracion/pendientes` (cola `integracion.pendientes`, contrato `argosisp.md` §0 y §3), con
`empresa_id` = la de la orden. Base: `pendiente_id`, `operacion` (`activar_instalacion`,
`asignar_comodato`, `consumo`, `devolucion`, `perdida`, `retiro`, `cargo`), `entidad` (`orden`),
`entidad_id`, `idempotency_key`.

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `integracion.pendiente_hecho` | base | Argos respondió 2xx. |
| `integracion.pendiente_fallido` | base + `intentos`, `status` (o null), `detalle` | 4xx de negocio de Argos (no se reintenta: la orden queda «cerrada con pendiente en Argos»; aviso al supervisor con el `detail` de Argos, sin credenciales), o la orden no traía un dato obligatorio (`detalle` = `datos incompletos: …`, `status` null, `intentos` 0). |
| `integracion.pendiente_agotado` | base + `intentos` (6), `status` (o null), `detalle` | Falló el 6.º intento (inicial + reintentos a 1 min, 5 min, 30 min, 2 h y 12 h): alerta al supervisor. |
| `integracion.pendiente_reintentado` | base sin `idempotency_key` + `de` (estado anterior) | `integraciones.reintentar` (manual). |
| `integracion.pendiente_omitido` | `orden_id`, `motivo` (`sin_conector`), `operaciones[]` (las Idempotency-Key que se habrían encolado) | Se cerró una orden con acciones para Argos y **nunca hubo** conector Argos (modo sin conector): no se encola nada (Iris trabaja con datos locales; además, log warn). Con un conector registrado pero apagado sí se encola y el worker reintenta. Puede repetirse si el relay entrega dos veces. |
| `integracion.pendiente_corregido` | base | La orden se volvió a cerrar con los datos que le faltaban a un pendiente `fallido_negocio` con `datos_incompletos`: payload y ruta nuevos, vuelve a `pendiente` (auditado). |
| `integracion.pendiente_conflicto` | base + `estado`, `motivo` (`cierre_repetido_con_otros_datos`), `payload_anterior`, `payload_nuevo` | La orden se volvió a cerrar con otros datos para una acción que no se puede pisar (hecha, en curso, encolada, agotada o rechazada por Argos: la Idempotency-Key pudo viajar ya con los datos anteriores). Queda marcado (`conflicto`, `payloadConflicto` en la lista del panel) para corregir a mano en Argos. Una vez por payload nuevo. |
| `integracion.pendiente_bloqueado` | base + `depende_de`, `motivo` (`depende_de_<id>_fallido`, o `_bloqueado` en cadena) | Una dependencia quedó `fallido_negocio` o `agotado` (o nació incompleta): el dependiente pasa a `bloqueado`. |
| `integracion.pendiente_desbloqueado` | base + `depende_de` | La dependencia terminó `hecho` (tras reintentarla) y todas las del dependiente están hechas: vuelve a `pendiente`. |

### `orden.cerrada`: lo que la cola espera de campo (I2)

El suscriptor `integracion.pendientes_orden_cerrada` lee estos campos (ids de Argos como número o
texto numérico; `empresa_id` del sobre manda sobre el del payload). Si falta un dato que una
acción necesita, esa acción nace `fallido_negocio` con `datos_incompletos` y el motivo en
`ultimo_error` (nunca se adivina):

| Campo | Uso |
| --- | --- |
| `orden_id` (texto o número) | Obligatorio (sin él no se encola nada; queda en el log). Va en las Idempotency-Key y en `origen_id`. |
| `tipo` | `instalacion`, `retiro`, `cambio_equipo` disparan acciones de equipo; `visita` y `mantenimiento` solo materiales y cargo. |
| `resultado` | Solo `exitosa` dispara activar, comodatos, devoluciones y retiro. Materiales y cargo se encolan con cualquier resultado. |
| `servicio_id` | Obligatorio para activar, comodato, devolución, pérdida, retiro y consumo. |
| `cliente_id` | Obligatorio para el cargo. |
| `bodega_id` | Bodega del técnico: consumo y devoluciones (una línea o un equipo pueden traer la suya). Un consumo por orden: todas las líneas de una misma bodega. |
| `tecnico_id` | Informativo. |
| `retirar_servicio` (bool) | En un `retiro` exitoso, `true` = el cliente pidió retirar el servicio → `POST /servicios/:id/retirar`. Ausente = false. |
| `materiales[]` | `{ producto_id, cantidad (número o texto, > 0; si no, `cantidad_no_positiva`), bodega_id? }` → `POST /inventario/consumos`. |
| `equipos[]` | `{ accion: instalar \| retirar, unidad_id?, serie?, mac?, condicion?, bodega_id? }`. Instalar: `unidad_id` o `serie`. Retirar: `unidad_id` obligatorio; `condicion: "perdido"` → pérdida; otra condición (`bueno`, `danado`…) → devolución con esa condición. En `cambio_equipo`, cada asignación espera a todas las devoluciones/pérdidas de la orden. |
| `cargo` (o null) | `{ producto_id?, monto? (> 0; si no, `monto_no_positivo`), descripcion? }` (producto o monto) → `POST /facturas/cargo-unico`. |

## Agenda (I2)

Emite el módulo `agenda`, con `empresa_id` en el sobre = la del dato (la de la zona o cuadrilla; en
técnicos y ausencias, la empresa activa de quien actúa; null sin conector o si el actor es el
sistema). Fechas en UTC. `orden.programada` también es de agenda (arriba, en Campo).

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `zona.creada` | `zona_id`, `origen` (`local`), `nombre` | `agenda.crear_zona`. Las de Argos llegan por el espejo (sin evento). |
| `zona.actualizada` | `zona_id`, `campos[]` (`nombre`, `poligono`, `activa`), `activa` | `agenda.editar_zona`, `agenda.activar_zona`, `agenda.desactivar_zona`. |
| `cuadrilla.creada` | `cuadrilla_id`, `nombre`, `zonas[]` | `agenda.crear_cuadrilla`. |
| `cuadrilla.actualizada` | `cuadrilla_id`, `campos[]` (`nombre`, `zonas`, `activa`, `capacidad`), `activa` | Editar, activar, desactivar o fijar la capacidad. |
| `tecnico.creado` | `tecnico_id` (ficha), `usuario_id`, `cuadrilla_id` (o null) | `agenda.crear_tecnico`. |
| `tecnico.actualizado` | `tecnico_id`, `usuario_id`, `campos[]` (`cuadrilla_id`, `bodega_id`, `habilidades`, `base`, `activo`, `turnos`, `capacidad`), `activo` | Editar, activar, desactivar, fijar turnos o capacidad. |
| `tecnico.ausencia_registrada` | `tecnico_id`, `usuario_id`, `ausencia_id`, `inicio`, `fin`, `motivo`, `citas_en_conflicto` (cuántas citas vivas caen dentro) | `agenda.registrar_ausencia`. Las citas no se mueven solas. |
| `tecnico.ausencia_anulada` | `tecnico_id`, `usuario_id`, `ausencia_id`, `motivo` (o null) | `agenda.anular_ausencia`. |

Consume (campo): `argos.servicio_retirado` → orden de retiro automática (`origen = argos`) si el
servicio tiene equipos en comodato según Argos y no hay otra orden de retiro abierta (no
cerrada ni cancelada) del servicio. Idempotente (candado por servicio y la Idempotency-Key
`argos.servicio_retirado:<entrante_id>`). Sin conector o servicio de otra empresa: no crea nada
(log); Argos caído: el relay reintenta.

Las posiciones GPS (`POST /campo/posiciones`) **no** emiten eventos ni dejan auditoría por fila.

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
| `horus.hallazgo_abierto` | `hallazgo_id` (texto), `categoria` (minúsculas), `severidad` (minúsculas o `""`), `ip` (o null), `router_id` (o null), `resumen`, `url_horus` (o null), `cliente_id`, `servicio_id`, `empresa_id` (o null), `fuente_cliente` (`argos` \| `instantanea` \| null), `motivo_sin_cliente` (null si se resolvió; si no: `sin_ip`, `ip_invalida`, `conector_argos_apagado`, `ip_sin_cliente`, `argos_no_disponible`, `argos_rechazo`, `otra_empresa`, `empresa_desconocida`), `ocurrido_en`, `entrante_id` | Horus abre un hallazgo de seguridad. El cliente sale de Argos (IP → cliente, A8 con respaldo a `GET /ip/{ip}` y a la instantánea si Argos no responde), buscado solo en la empresa del hallazgo: la de `empresa_id` del payload o, si no viene, la de su `router_id` en la instantánea de routers de Argos (`otra_empresa` si la IP es de otra); sin empresa no se busca (`empresa_desconocida`): la misma IP puede estar en dos empresas. `url_horus` solo si es del mismo origen que el conector (su URL base o la pública). |
| `horus.hallazgo_resuelto` | `hallazgo_id`, `categoria`, `resumen`, `ocurrido_en`, `empresa_id` (la del payload o la de su router; o null), `entrante_id` (sin IP ni cliente: se vincula por `hallazgo_id`) | Horus cierra el hallazgo (soporte agrega la nota al ticket vinculado por `hallazgo_id` y propone cerrarlo). |

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
| `ticket.resolucion_propuesta` | `ticket_id`, `accion` (`resolver` \| `cerrar`), `motivo` (texto de la nota), `origen` (`horus`), `entrante_id` | Horus avisó que el problema terminó: `horus.router_estado_cambiado` / `horus.servicio_infra_cambiado` con `arriba` sobre un maestro abierto (`resolver`), o `horus.hallazgo_resuelto` (`resolver` si el ticket está abierto, `cerrar` si ya está resuelto). Iris no cambia el estado: un agente confirma. Una vez por propuesta (si ya estaba la misma, no se repite). |
| `ticket.mensaje_cliente` | `ticket_id`, `mensaje_id`, `canal` (`portal`) | El cliente escribió en su ticket desde el portal (`POST /portal/tickets/{id}/responder`); si reanudó o reabrió, sale además `ticket.estado_cambiado`. |

## Correo de soporte (I2)

Emite el módulo `soporte` (entrada por IMAP y salida por SMTP), con `empresa_id` = la del buzón
(null sin conector). Un correo que abre un ticket o responde a uno emite además los eventos de
ticket de siempre (`ticket.creado`, `ticket.estado_cambiado` al reanudar o reabrir).

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `correo.recibido` | `correo_id`, `buzon_id`, `estado` (`procesado` \| `ignorado` \| `error`), `resultado` (`ticket_creado` \| `respuesta` \| `nota_interna` \| null), `ticket_id` (o null), `mensaje_id` (o null), `motivo` (o null: `automatico: …`, `remitente_propio`, `remitente_no_es_contacto` / `remitente_no_verificado` con `nota_interna`, o el código de la regla que impidió crear) | El worker procesó un correo leído del buzón (una vez por correo: deduplicado por `Message-ID` y buzón). |
| `buzon.creado` | `buzon_id`, `correo` | `buzones.crear` (nace inactivo). Nunca lleva contraseñas. |
| `buzon.actualizado` | `buzon_id` | `buzones.editar`. |
| `buzon.activado` / `buzon.desactivado` | `buzon_id` | `buzones.activar` / `buzones.desactivar`. |

Consume `ticket.respondido` (suscriptor `soporte.correo_saliente`, idempotente por `mensaje_id`):
si el ticket es de origen `correo` y el mensaje es público de un agente (o un aviso de Iris, como el
de un incidente maestro resuelto), encola el envío por el SMTP del buzón, siempre al contacto del
ticket.

`ticket.respondido` también sale, uno por hijo, cuando se resuelve un incidente maestro
(`tickets.resolver` sobre un ticket con `maestro`) o Horus lo reabre: el mensaje público de Iris que
avisa al cliente, emitido por el job `soporte.avisar_hijos` (en tandas, una vez por hijo y aviso).

## Tickets desde Horus (I2)

Emite el módulo `soporte` (suscriptor `soporte.horus` de los eventos `horus.*`, actor sistema). Un
maestro nuevo o un ticket por hallazgo emiten los de siempre (`ticket.creado`, con `origen =
horus`); reabrir o cerrar un maestro, `ticket.estado_cambiado`.

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `soporte.horus_descartado` | `evento` (el `horus.*`), `entrante_id`, `empresa_id` (la del evento o null), `motivo` (`sin_empresa` \| `empresa_no_registrada` \| `error`), `detalle` | Un evento de Horus que debía crear o tocar un ticket no se aplicó: sin empresa (o con una no registrada) habiendo empresas registradas, o una regla de negocio lo impidió (`error`, con el código y el detalle; p. ej. la categoría de la regla de hallazgos está inactiva). Alerta interna para el NOC; no se reintenta. Una vez por `entrante_id`. |

Consume `horus.router_estado_cambiado`, `horus.servicio_infra_cambiado`, `horus.hallazgo_abierto` y
`horus.hallazgo_resuelto` (idempotente por `entrante_id`, tabla `soporte.horus_eventos`; un evento
con `ocurrido_en` igual o anterior al último conocido de su router, servicio o hallazgo se ignora
como `obsoleto`); reglas en
`apps/api/internal/soporte/README.md` («Tickets desde Horus»).


## Campo: órdenes de trabajo (I2)

Emite el módulo `campo`, con `empresa_id` = la empresa de la orden (null sin conector) en el sobre y,
en `orden.creada` y `orden.cerrada`, también en el payload. Fechas en UTC (RFC 3339). Un evento por
acción aplicada (desde el panel o desde el lote de la app de campo: una operación duplicada no lo
repite).

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `orden.creada` | `orden_id`, `numero`, `tipo` (`instalacion` \| `visita` \| `retiro` \| `cambio_equipo` \| `mantenimiento`), `empresa_id`, `ticket_id`, `prospecto_id`, `cliente_id`, `servicio_id`, `zona_id` (o null), `origen` (`panel` \| `api` \| `argos` \| `sistema`) | `ordenes.crear` (también desde un ticket con `ticketId`, y la orden de retiro automática con `origen = argos`). |
| `orden.asignada` | `orden_id`, `tecnico_id`, `cuadrilla_id`, `tecnico_anterior_id` (o null) | `ordenes.asignar`, y `programar`/`reprogramar` cuando cambian el técnico. |
| `orden.programada` | `orden_id`, `numero`, `tecnico_id`, `cuadrilla_id` (de la orden, o null), `tecnico_cuadrilla_id` (de la ficha del técnico, o null), `inicio`, `fin`, `ventana_inicio`/`ventana_fin` (ventana de llegada acordada con el cliente, o null), `reprogramada` (bool), `origen` (`programar` \| `reprogramar` \| `asignar`), `motivo` (o null) | Lo emite **agenda** (`agenda.ReservarCita`) en la transacción de `ordenes.programar`, `ordenes.reprogramar` y de `ordenes.asignar` cuando una orden programada cambia de técnico (`origen = asignar`, misma hora). |
| `orden.estado_cambiada` | `orden_id`, `numero`, `de`, `a`, `motivo` (o null), `tecnico_id` | Toda transición (programar, iniciar camino, llegar, cerrar —`motivo` = resultado—, fallar —`motivo` = `<motivo_falla>: <detalle>`—, cancelar, reabrir, reprogramar y la vuelta a pendiente o programada al reasignar). |
| `orden.cerrada` | `orden_id`, `numero`, `tipo`, `resultado` (`exitosa` \| `parcial`), `empresa_id`, `cliente_id`, `servicio_id`, `ticket_id`, `prospecto_id`, `tecnico_id`, `bodega_id` (o null), `cerrada_en`, `retirar_servicio` (bool: retiro exitoso), `equipos_validados` (bool: false si algún equipo retirado no se verificó contra el comodato de Argos —sin conector o Argos caído—; tratarlo como dato no verificado), `materiales[]` (`producto_id`, `cantidad` —sumada por producto y bodega, 3 decimales—, `bodega_id` (o null), `excede_existencia`), `equipos[]` (`id`, `accion` `instalar` \| `retirar`, `unidad_id`, `serie`, `mac` `AA:BB:CC:DD:EE:FF`, `modelo`, `condicion` `buena` \| `danada` \| `perdido` (solo retirar; `perdido` = pérdida del comodato), `bodega_id`, `validado`) | `ordenes.cerrar`. Lo consume el núcleo (I1) para los pendientes en Argos (contrato §3): con `resultado = exitosa`, activar la instalación y el comodato, devoluciones o pérdidas, retiro del servicio; materiales → consumo desde la bodega. `cargo` (o null): `{ producto_id?, monto? (texto/número > 0, 2 decimales), descripcion }` si el tipo de orden o su plantilla dicen cobrar (visita cobrada; ver «Cargo» en `apps/api/internal/campo/README.md`). |
