# Payloads de eventos de Iris

Todo evento sale por `integracion.outbox` con esta forma: `{ id (uuid v7), evento, ocurrido_en, empresa_id, payload }`. Cada agente agrega aquí el detalle de sus eventos, en la misma PR que el código que los emite. El catálogo resumido está en `.claude/CLAUDE.md`.

## Identidad (núcleo, I1)

Emite el módulo `identidad`, con `empresa_id = null` (los usuarios de Iris no pertenecen a una empresa), salvo los de empresas y membresía, que llevan la empresa afectada.

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `usuario.creado` | `usuario_id`, `tipo` | Alta de un usuario (`usuarios.crear`, también desde `iris admin crear`). |
| `usuario.actualizado` | `usuario_id`, `campos[]` (nombres de los campos que cambiaron) | `usuarios.editar`. También `yo.cambiar_contrasena` (`POST /yo/contrasena`, el propio usuario; `campos` = `["contrasena_hash", "cambio_requerido"]` o solo el primero). |
| `usuario.desactivado` | `usuario_id`, `motivo` | `usuarios.desactivar`. Revoca sus sesiones; sus tokens dejan de autenticar mientras esté inactivo, sin revocarse ni emitir `token.revocado`. |
| `usuario.mfa_reiniciado` | `usuario_id`, `motivo`, `sesiones_cerradas` (número), `tokens_revocados` (número) | `usuarios.reiniciar_mfa` (un administrador, nunca el propio usuario): borra su secreto TOTP, reemplaza su contraseña por una temporal (nunca va en el evento) que debe cambiar al entrar, revoca sus sesiones y, salvo `revocarTokens: false`, sus tokens (un `token.revocado` con `motivo = mfa_reiniciado` por cada uno). Un segundo reinicio sin MFA configurado es 422. |
| `usuario.sso_vinculado` | `usuario_id`, `vinculo_id`, `conector_id`, `sistema` (`argosisp`), `usuario_externo` (id del usuario en Argos, `u-<id>`), `origen` (`usuario` \| `admin`) | `sso.vincular` (`origen = usuario`): el usuario, con una sesión abierta después de canjear un ticket de autologin sin vínculo, confirma la cuenta de Argos; solo si Argos manda el correo verificado (`correo_verificado`, contrato 0.3.1) y es el de su cuenta. `usuarios.vincular_sso` (`origen = admin`): un administrador vincula cuando el correo no coincide. Nunca solo por correo ni creando usuarios. Pendiente: aviso por correo al usuario (sin correo saliente del sistema todavía). |
| `usuario.sso_desvinculado` | `usuario_id`, `vinculo_id`, `conector_id`, `usuario_externo`, `motivo` (texto del usuario o del admin, puede ser vacío; o `usuario_desactivado`, `mfa_reiniciado`, `contrasena_reiniciada`, `correo_cambiado` cuando se borra por eso) | `usuarios.borrar_vinculo_sso` (el propio usuario o un administrador), o al desactivar al usuario, reiniciar su MFA o cambiarle un administrador la contraseña o el correo (un evento por vínculo). El siguiente «Ver en Iris» vuelve a pedir iniciar sesión y vincular. |
| `usuario.sesiones_cerradas` | `usuario_id`, `sesiones[]` (ids), `todas` (bool), `motivo` (o null) | `usuarios.cerrar_sesion` (`POST /usuarios/{id}/sesiones/{sesionId}/cerrar`, `todas = false`) y `usuarios.cerrar_sesiones` (`POST /usuarios/{id}/cerrar-sesiones`, `todas = true`; para el propio usuario, todas menos la de la petición). Sin sesiones abiertas no se emite. |
| `usuario.rol_asignado` | `usuario_id`, `rol` (código), `zona_id` (o null), `cuadrilla_id` (o null) | `usuarios.asignar_rol` (`POST /usuarios/{id}/asignar-rol`). Los permisos cambian en la siguiente petición del usuario. |
| `usuario.rol_quitado` | `usuario_id`, `rol`, `zona_id` (o null), `cuadrilla_id` (o null) | `usuarios.quitar_rol` (la asignación exacta, con su alcance). |
| `rol.creado` | `rol_id`, `codigo`, `permisos[]` | `roles.crear` (rol propio; los base los mantiene la semilla y no emiten). |
| `rol.actualizado` | `rol_id`, `campos[]` (`nombre`, `descripcion`, `permisos`), `permisos_agregados[]`, `permisos_quitados[]` | `roles.editar` (solo roles propios). Cambia los permisos de todos los usuarios con el rol. |
| `rol.desactivado` / `rol.activado` | `rol_id`, `codigo`, `usuarios` (cuántos lo tienen asignado), `motivo` (o null) | `roles.desactivar` / `roles.activar`. Un rol desactivado no da permisos ni se asigna; las asignaciones se conservan y vuelven a valer al activarlo. Los roles nunca se borran. |
| `token.creado` | `token_id`, `usuario_id` | Alta de un token `iris_pat_`. |
| `token.revocado` | `token_id`, `usuario_id`, `motivo` | `tokens.revocar`, `usuarios.reiniciar_mfa` (uno por token, `motivo = mfa_reiniciado`) y `usuarios.cerrar_sesiones` con `revocarTokens: true` (`motivo = sesiones_cerradas`). |
| `empresa.creada` | `empresa_id`, `origen` (`manual`) | `empresas.crear` (un administrador registra una empresa de Argos). Lleva `empresa_id` = la empresa. Las que registra el canje del conector no emiten este evento (van en `conector.creado`). |
| `empresa.actualizada` | `empresa_id`, `campos[]` | `empresas.editar` (nombre, NIT). |
| `usuario.empresa_asignada` | `usuario_id`, `empresa_id` | `usuarios.asignar_empresa`, y `usuarios.crear` con `empresas` (uno por empresa). Lleva `empresa_id` = la empresa asignada. |
| `usuario.empresa_quitada` | `usuario_id`, `empresa_id` | `usuarios.quitar_empresa`. |
| `empresa.datos_adoptados` | `empresa_id`, `filas` (objeto `{"esquema.tabla": n}` con cada tabla operativa registrada, también las de 0) | `empresas.adoptar` (`POST /empresas/{id}/adoptar-sin-empresa`, solo admin global): los datos guardados con `empresa_id` null en modo sin conector pasan a la empresa. Lleva `empresa_id` = la empresa. Los consumidores que cachean por empresa deben releer esas tablas. |

## Integraciones (núcleo, I1)

Emiten `integracion/conector` (los `conector.*`, de Argos y de Horus) e `integracion/argos`. Los de
conectores van con `empresa_id = null`; `argos.servicio_retirado` lleva la empresa del servicio
según Argos; `argos.servicio_eliminado` y `argos.router_eliminado`, la del webhook (o la de la
instantánea si el webhook no la trae).

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `conector.creado` | `conector_id`, `sistema` (`argosisp` \| `horus`), `origen` (`enlace_remoto` \| `enlace_local`) | Se canjeó un enlace de conexión. El conector nace apagado. |
| `conector.activado` | `conector_id`, `sistema` | `integraciones.activar`. |
| `conector.desactivado` | `conector_id`, `sistema`, `motivo` | `integraciones.desactivar`. |
| `conector.actualizado` | `conector_id`, `sistema`, `campos[]` (`url_publica`; `version_remota`, `contrato_remoto`, `contrato_informado`) | `integraciones.configurar_url` (`POST /integraciones/conectores/{id}/url-publica`), solo si cambió algo. También `integraciones.informar_contrato` (job del worker, contrato `argosisp.md` 0.5.0 §3.3): Iris informó su contrato a Argos (`PUT /integraciones/iris/contrato`) o leyó otra versión en `GET /integraciones/iris/estado`. |
| `argos.servicio_retirado` | `servicio_id`, `cliente_id` (o null), `de`, `a` (`retirado`), `entrante_id` | Argos avisó `servicio.estado_cambiado` con `a = retirado` **y** Iris lo confirmó releyendo `GET /servicios/:id` en Argos (ids y empresa salen de esa lectura, no del payload). Gancho para que I2 proponga la orden de retiro (Fase 2) si el servicio tiene equipos en comodato. Una vez por webhook (deduplicado por el `id` firmado del cuerpo). |
| `argos.servicio_eliminado` | `servicio_id`, `cliente_id` (o null), `empresa_id` (o null), `router_id` (o null), `entrante_id` | Contrato 0.3.1: Argos avisó `servicio.eliminado` (solo servicios que nunca se usaron), Iris lo confirmó con el 404 problem+json `urn:argosisp:problema:no_encontrado` de `GET /servicios/:id` (un 403 `empresa_no_permitida` u otro 404 no confirman). Si la instantánea tenía el servicio, su empresa y su cliente mandan sobre el payload (una discrepancia se anota en la auditoría). La instantánea ya no lo tiene; queda la lápida `integracion.argos_eliminados` con su última copia (código y plan) y la ficha del cliente lo lista en `serviciosEliminados`. Órdenes y tickets que lo referencian conservan sus datos y lo muestran «eliminado en Argos» (`argos.ServiciosEliminados`). Lleva la empresa del servicio. Una vez por servicio (un segundo aviso encuentra la lápida y no emite). |
| `argos.router_eliminado` | `router_id`, `empresa_id` (o null = router compartido), `entrante_id` | Contrato 0.3.1: Argos avisó `router.eliminado` (solo routers sin historial), `GET /routers/:id` respondió 404 problem+json `urn:argosisp:problema:no_encontrado`; la empresa de la instantánea (null = compartido) manda sobre el payload. Sale de `integracion.argos_routers` (factibilidad ya no lo usa; una factibilidad evaluada conserva el id) con lápida: una carga de catálogos no lo recrea salvo que `GET /routers/:id` responda 200 después de la confirmación (`argos.router_restaurado`). Una vez por router. |
| `argos.servicio_restaurado` | `servicio_id`, `cliente_id`, `empresa_id` (o null), `eliminado_entrante_id` | Un servicio con lápida volvió en una lista de Argos (ficha del cliente) y `GET /servicios/:id` respondió 200 pedido después de la confirmación (reloj de Postgres): la lápida estaba mal o Argos lo restauró. Se borra la lápida y vuelve a la instantánea (auditado, log de advertencia). Lleva la empresa del servicio. |
| `argos.datos_cambiados` | `entidad` (`servicio` \| `cliente`), `cliente_id` (o null), `servicio_id` (o null), `empresa_id` (o null), `campos[]` (con prefijo: `servicio.direccion`, `servicio.coordenadas`, `servicio.referencia`, `servicio.contacto_nombre`, `servicio.contacto_telefono`, `cliente.direccion`, `cliente.referencia`, `cliente.telefono`…), `entrante_id` | Contrato 0.5.0: `servicio.datos_cambiados`, o `cliente.actualizado` con algún campo que copian las órdenes (dirección, coordenadas, referencia, contacto, teléfono, móvil, nombre; `email` no). La instantánea ya se refrescó (si Iris tenía al cliente). Campo marca sus órdenes abiertas «dato de Argos cambió» (`orden.datos_argos_cambiados`) sin pisar lo copiado. Lleva la empresa del webhook. |
| `argos.traslado_cambiado` | `traslado_id`, `estado` (`enviado` \| `recibido` \| `cancelado`, el guardado: un estado final no vuelve a enviado), `empresa_id` (o null), `usuario_id` (técnico de Iris por el vínculo de `agenda.tecnicos`, o null si nadie lo tiene), `entrante_id` | Contrato 0.5.0: `inventario.traslado_enviado` / `_recibido` / `_cancelado`. Se guarda en `integracion.argos_traslados` (lista «Por recibir» de la app de campo). Aviso para el técnico. |
| `argos.router_restaurado` | `router_id`, `empresa_id` (o null), `eliminado_entrante_id` | Igual, para un router que vuelve en la carga de catálogos y cuyo `GET /routers/:id` respondió 200. |

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
`entidad_id`, `idempotency_key`. Desde 0.5.0 la cola también tiene `recepcion` (entidad `traslado`).

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `integracion.pendiente_hecho` | base | Argos respondió 2xx. |
| `integracion.pendiente_fallido` | base + `intentos`, `status` (o null), `detalle` | 4xx de negocio de Argos (no se reintenta: la orden queda «cerrada con pendiente en Argos»; aviso al supervisor con el `detail` de Argos, sin credenciales), o la orden no traía un dato obligatorio (`detalle` = `datos incompletos: …`, `status` null, `intentos` 0). |
| `integracion.pendiente_agotado` | base + `intentos` (6), `status` (o null), `detalle` | Falló el 6.º intento (inicial + reintentos a 1 min, 5 min, 30 min, 2 h y 12 h): alerta al supervisor. |
| `integracion.pendiente_reintentado` | base sin `idempotency_key` + `de` (estado anterior) | `integraciones.reintentar` (manual). |
| `integracion.pendiente_omitido` | `orden_id`, `motivo` (`sin_conector`, `material_comodato`), `operaciones[]` (las Idempotency-Key que se habrían encolado; con `material_comodato`, `producto:<id>`) | 0.5.0: `material_comodato` = un material de modalidad comodato no va en el consumo (Argos respondería `422 producto_es_comodato`; se registra como equipo con serie). `sin_conector`: se cerró una orden con acciones para Argos y **nunca hubo** conector Argos (modo sin conector): no se encola nada (Iris trabaja con datos locales; además, log warn). Con un conector registrado pero apagado sí se encola y el worker reintenta. Puede repetirse si el relay entrega dos veces. |
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
| `tecnico_id` | El usuario técnico: con `tecnico`, queda en el pendiente (`tecnico_usuario_id`) para corregir el vínculo al reintentar a mano. |
| `tecnico` (0.5.0) | `{ id_externo }` del vínculo de la ficha de agenda con Argos, congelado al cerrar, o null. Con Argos ≥ 1.6.0 (`version_remota` del conector) consumo, comodato y devolución mandan `tecnico` en vez de `bodega_id` (una bodega propia de la línea o el equipo distinta de la de la orden sigue mandando `bodega_id`, que en Argos manda sobre `tecnico`). Sin vínculo o con Argos < 1.6.0, `bodega_id` como antes. Un cierre repetido que solo cambia `bodega_id` ↔ `tecnico` no pisa lo encolado ni marca conflicto. |
| `retirar_servicio` (bool) | En un `retiro` exitoso, `true` = el cliente pidió retirar el servicio → `POST /servicios/:id/retirar`. Ausente = false. |
| `materiales[]` | `{ producto_id, cantidad (número o texto, > 0; si no, `cantidad_no_positiva`), bodega_id?, modalidad? }` → `POST /inventario/consumos` con `lineas[]`. 0.5.0: `modalidad` (`comodato` \| `consumible_gasto` \| `consumible_cobro` o null si no se sabía): las de `comodato` no van en el consumo (`integracion.pendiente_omitido` con `material_comodato`). |
| `equipos[]` | `{ accion: instalar \| retirar, unidad_id?, serie?, mac?, condicion?, bodega_id? }`. Instalar: `unidad_id` o `serie`. Retirar: `unidad_id` obligatorio; `condicion: "perdido"` → pérdida; otra condición (`bueno`, `danado`…) → devolución con esa condición. En `cambio_equipo`, cada asignación espera a todas las devoluciones/pérdidas de la orden. |
| `cargo` (o null) | `{ producto_id?, monto? (> 0; si no, `monto_no_positivo`), descripcion? }` (producto o monto) → `POST /facturas/cargo-unico`. 0.5.0: nunca se descarta. Si `producto_id` está en el consumo de la orden, el cargo espera al consumo y un `409 cargo_duplicado` (Argos ya lo cobró por modalidad `consumible_cobro`) cuenta como hecho (conciliado). |

## Plantillas de formularios (I2)

Emite el módulo `campo` con `empresa_id` = la de la plantilla en el sobre (null = común a todas las
empresas).

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `plantilla.creada` | `plantilla_id`, `codigo`, `empresa_id` (o null), `version_id`, `numero_version` (1) | `plantillas.crear`. |
| `plantilla.actualizada` | `plantilla_id`, `version_id`, `numero_version`, `version_nueva` (bool: la actual ya estaba en alguna orden y se creó otra), `activa` | `plantillas.editar`. |
| `plantilla.activada` / `plantilla.desactivada` | `plantilla_id` | `plantillas.activar` / `plantillas.desactivar`. |

## Agenda (I2)

Emite el módulo `agenda`, con `empresa_id` en el sobre = la del dato (la de la zona o cuadrilla; en
técnicos y ausencias, la empresa activa de quien actúa; null sin conector o si el actor es el
sistema). Fechas en UTC. `orden.programada` también es de agenda (arriba, en Campo).

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `zona.creada` | `zona_id`, `origen` (`local`), `nombre` | `agenda.crear_zona`. Las de Argos llegan por el espejo (sin evento). |
| `zona.actualizada` | `zona_id`, `campos[]` (`nombre`, `poligono`, `activa`), `activa` | `agenda.editar_zona`, `agenda.activar_zona`, `agenda.desactivar_zona`. También `catalogo.cambiado` de Argos (0.5.0, I1) sobre una zona espejo (`nombre`, `activa`). |
| `cuadrilla.creada` | `cuadrilla_id`, `nombre`, `zonas[]` | `agenda.crear_cuadrilla`. |
| `cuadrilla.actualizada` | `cuadrilla_id`, `campos[]` (`nombre`, `zonas`, `activa`, `capacidad`), `activa` | Editar, activar, desactivar o fijar la capacidad. |
| `tecnico.creado` | `tecnico_id` (ficha), `usuario_id`, `cuadrilla_id` (o null) | `agenda.crear_tecnico`. |
| `tecnico.actualizado` | `tecnico_id`, `usuario_id`, `campos[]` (`cuadrilla_id`, `bodega_id`, `habilidades`, `base`, `activo`, `turnos`, `capacidad`), `activo` | Editar, activar, desactivar, fijar turnos o capacidad. |
| `tecnico.ausencia_registrada` | `tecnico_id`, `usuario_id`, `ausencia_id`, `inicio`, `fin`, `motivo`, `citas_en_conflicto` (cuántas citas vivas caen dentro) | `agenda.registrar_ausencia`. Las citas no se mueven solas. |
| `tecnico.ausencia_anulada` | `tecnico_id`, `usuario_id`, `ausencia_id`, `motivo` (o null) | `agenda.anular_ausencia`. |
| `tecnico.argos_vinculado` | `tecnico_id`, `usuario_id`, `id_externo` (usuario de Argos), `anterior_id_externo` (o null) | `agenda.vincular_argos` (I1, contrato `argosisp.md` 0.5.0 §3.3): un administrador vincula la ficha con un usuario de `GET /inventario/tecnicos` que tiene bodega técnico activa en la empresa activa. Desde entonces, `orden.cerrada` lleva `tecnico { id_externo }`. Sin el correo (dato personal). |
| `tecnico.argos_desvinculado` | `tecnico_id`, `usuario_id`, `id_externo` (el que tenía), `motivo` (o null) | `agenda.desvincular_argos`. Sus órdenes vuelven a `bodega_id`; lo ya encolado no cambia. |
| `ruta.propuesta` | `ruta_id`, `tecnico_id` (usuario), `fecha` (YYYY-MM-DD, Guatemala), `ordenes[]` (en el orden de visita), `modo` (`optimizada` \| `manual`), `motor` (`osrm` \| `haversine`), `degradada` (bool), `motivo_motor` (`sin_osrm` \| `osrm_no_disponible` \| `datos_cambiaron`, o null) | `rutas.proponer`: crea o rehace el borrador del técnico ese día. |
| `ruta.publicada` | `ruta_id`, `tecnico_id`, `fecha`, `ordenes[]` (orden de visita), `reemplaza_id` (la publicada anterior, que queda `reemplazada`, o null), `motor`, `degradada`, `version` | `rutas.publicar`. El técnico la recibe en `GET /campo/paquete` y `GET /campo/cambios`. |
| `ruta.desactualizada` | `ruta_id`, `tecnico_id`, `fecha`, `estado` (`borrador` \| `publicada`), `orden_id`, `motivo` (texto: «La orden OT-… cambió de técnico/hora», «se canceló», «se reabrió», «quedó sin técnico», «se programó») | En la transacción de la acción de campo que mueve una cita (programar, reprogramar, asignar, cancelar, reabrir, quitar técnico): marca la ruta del día de antes y la de después. Solo al pasar de vigente a desactualizada (una vez por ruta). Nunca se borra una ruta. |

Consume (campo): `argos.servicio_retirado` → orden de retiro automática (`origen = argos`) si el
servicio tiene equipos en comodato según Argos y no hay otra orden de retiro abierta (no
cerrada ni cancelada) del servicio. Idempotente (candado por servicio y la Idempotency-Key
`argos.servicio_retirado:<entrante_id>`). Sin conector o servicio de otra empresa: no crea nada
(log); Argos caído: el relay reintenta.

Las posiciones GPS (`POST /campo/posiciones`) **no** emiten eventos ni dejan auditoría por fila.

En los eventos de rutas el `empresa_id` del sobre es el de la ruta (la empresa activa de quien
propone; null sin conector).

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
| `ticket.asignado` | `ticket_id`, `usuario_id` (o null), `cuadrilla_id` (o null) | `tickets.asignar` (ambos null = sin asignar), también `ordenes.igualar_asignacion` con `desde = orden` (el ticket toma el técnico y la cuadrilla de su orden). |
| `ticket.respondido` | `ticket_id`, `mensaje_id` | `tickets.responder`: mensaje público al cliente (para avisarle por portal, correo o Argos). Las notas internas no emiten evento. |
| `ticket.escalado` | `ticket_id`, `de`, `a` (prioridades; iguales si solo sube el nivel), `nivel`, `motivo` | `tickets.escalar`. |
| `ticket.fusionado` | `ticket_id` (el duplicado), `destino_id`, `motivo` (o null) | `tickets.fusionar`. |
| `ticket.sla_en_riesgo` | `ticket_id`, `sla` (`respuesta` \| `resolucion`), `vence_en` | Cron: el reloj pasó el 80 % de su objetivo. Una vez por ticket, reloj y `vence_en` (si el vencimiento se corre, el nuevo plazo vuelve a alertar); no se emite si ya venció. |
| `ticket.sla_vencido` | `ticket_id`, `sla` (`respuesta` \| `resolucion`), `vence_en` | Cron: el reloj venció sin cumplirse. Una vez por ticket, reloj y `vence_en`. |
| `ticket.resolucion_propuesta` | `ticket_id`, `accion` (`resolver` \| `cerrar`), `motivo` (texto de la nota), `origen` (`horus` \| `orden`), `entrante_id` (o null; solo `horus`), `orden_id` (o null; solo `orden`) | `origen = horus`: Horus avisó que el problema terminó: `horus.router_estado_cambiado` / `horus.servicio_infra_cambiado` con `arriba` sobre un maestro abierto (`resolver`), o `horus.hallazgo_resuelto` (`resolver` si el ticket está abierto, `cerrar` si ya está resuelto). Una vez por propuesta (si ya estaba la misma, no se repite). `origen = orden`: se cerró (`ordenes.cerrar`, panel o lote) la última orden abierta del ticket y el ticket sigue abierto (`nuevo`, `abierto`, `en_espera`): `accion = resolver`, en la transacción del cierre y con la empresa del ticket. No pisa una propuesta de Horus pendiente; una de Horus sí reemplaza a una de orden (y se emite). Una de orden se descarta sin evento si el ticket vuelve a tener una orden abierta (creada o reabierta). En los dos casos Iris no cambia el estado: un agente confirma; la propuesta se descarta en cualquier cambio de estado. |
| `ticket.mensaje_cliente` | `ticket_id`, `mensaje_id`, `canal` (`portal`) | El cliente escribió en su ticket desde el portal (`POST /portal/tickets/{id}/responder`); si reanudó o reabrió, sale además `ticket.estado_cambiado`. |

## Encuestas de satisfacción (I2)

Emite el módulo `soporte`, con `empresa_id` = la empresa del ticket o la orden (null sin conector).

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `encuesta.enviada` | `encuesta_id`, `entidad` (`ticket` \| `orden`), `entidad_id`, `canal` (`correo` \| `enlace`) | Al crear la encuesta, en la misma transacción: un ticket pasa a `resuelto` (`tickets.resolver`; no el maestro de un incidente) o se cierra con éxito una orden de instalación o visita (`ordenes.cerrar`). Una viva por ticket y una por orden (las anuladas no cuentan). `correo`: se encoló el correo al contacto del ticket; `enlace`: soporte lo pide con `POST /encuestas/{id}/enlace` para compartirlo. |
| `encuesta.respondida` | `encuesta_id`, `entidad`, `entidad_id`, `calificacion` (1–5), `alerta` (`true` si la calificación es 1 o 2) | El cliente respondió por el enlace (`POST /encuestas/publica/responder`). Una sola vez (repetir la misma respuesta no emite otro). |
| `encuesta.anulada` | `encuesta_id`, `entidad` (`ticket`), `entidad_id`, `motivo` (`reabierto` \| `fusionado`) | El ticket de una encuesta pendiente se reabrió (`tickets.reabrir`, o el cliente respondió por portal o correo a un resuelto) o se fusionó en otro, en la misma transacción. La siguiente resolución genera otra encuesta. |

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
| `orden.creada` | `orden_id`, `numero`, `tipo` (`instalacion` \| `visita` \| `retiro` \| `cambio_equipo` \| `mantenimiento`), `empresa_id`, `ticket_id`, `prospecto_id`, `cliente_id`, `servicio_id`, `zona_id` (o null), `origen` (`panel` \| `api` \| `argos` \| `sistema`) | `ordenes.crear` (también desde un ticket con `ticketId`, y la orden de retiro automática con `origen = argos`). También `tickets.crear_orden` (`POST /tickets/{id}/crear-orden`), que en la misma transacción emite después `orden.asignada` y, con fecha, `orden.programada`. |
| `orden.asignada` | `orden_id`, `tecnico_id`, `cuadrilla_id`, `tecnico_anterior_id` (o null) | `ordenes.asignar`, y `programar`/`reprogramar` cuando cambian el técnico. También `tickets.crear_orden` (con técnico, vía `ordenes.asignar`; solo con cuadrilla, `tecnico_id = null`) y `ordenes.igualar_asignacion` con `desde = ticket`. |
| `orden.programada` | `orden_id`, `numero`, `tecnico_id`, `cuadrilla_id` (de la orden, o null), `tecnico_cuadrilla_id` (de la ficha del técnico, o null), `inicio`, `fin`, `ventana_inicio`/`ventana_fin` (ventana de llegada acordada con el cliente, o null), `reprogramada` (bool), `origen` (`programar` \| `reprogramar` \| `asignar`), `motivo` (o null) | Lo emite **agenda** (`agenda.ReservarCita`) en la transacción de `ordenes.programar`, `ordenes.reprogramar` y de `ordenes.asignar` cuando una orden programada cambia de técnico (`origen = asignar`, misma hora). |
| `orden.estado_cambiada` | `orden_id`, `numero`, `de`, `a`, `motivo` (o null), `tecnico_id` | Toda transición (programar, iniciar camino, llegar, cerrar —`motivo` = resultado—, fallar —`motivo` = `<motivo_falla>: <detalle>`—, pausar —`a = pausada`, `motivo` = `<motivo>` o `<motivo>: <nota>`—, reanudar —`de = pausada`, `motivo = reanudada`—, cancelar, reabrir, reprogramar y la vuelta a pendiente o programada al reasignar). |
| `orden.pausada` | `orden_id`, `numero`, `pausa_id`, `motivo` (`esperando_material` \| `cliente_ausente` \| `clima` \| `acceso` \| `otro`), `nota` (o null), `desde` (`en_camino` \| `en_sitio`), `inicio`, `tecnico_id` | `ordenes.pausar` (panel u `orden.pausar` del lote), junto con `orden.estado_cambiada`. |
| `orden.reanudada` | `orden_id`, `numero`, `pausa_id`, `a` (el estado de antes de la pausa), `duracion_seg`, `tiempo_pausado_seg` (total de la orden), `tecnico_id` | `ordenes.reanudar` (panel u `orden.reanudar` del lote). Cancelar o reprogramar una pausada cierra la pausa sin este evento (solo `orden.estado_cambiada`). |
| `orden.formulario_agregado` | `orden_id`, `formulario_id` (uuid), `plantilla_id`, `version_id`, `numero_version`, `origen` (`despacho` \| `tecnico`), `tecnico_id` | `ordenes.asignar_formulario` (despacho) u `orden.agregar_formulario` del lote (el técnico). El formulario del tipo (origen `tipo`) va con `orden.creada`, sin este evento. |
| `orden.formulario_quitado` | `orden_id`, `formulario_id`, `plantilla_id`, `origen`, `canal` | Quitar un formulario sin respuestas (despacho, o el técnico uno que agregó él). |
| `orden.datos_argos_cambiados` | `orden_id`, `numero`, `entidad` (`servicio` \| `cliente`), `campos[]` (los acumulados de este aviso, con prefijo: `servicio.referencia`…), `entrante_id` | I1, contrato 0.5.0: `argos.datos_cambiados` sobre una orden abierta (no cerrada, fallida ni cancelada) de ese servicio o cliente. La orden conserva lo copiado y queda marcada (`datosArgosCambiados { en, campos[] }` en la API; no sube la versión) para que despacho lo aplique. |
| `orden.cerrada` | `orden_id`, `numero`, `tipo`, `resultado` (`exitosa` \| `parcial`), `empresa_id`, `cliente_id`, `servicio_id`, `ticket_id`, `prospecto_id`, `tecnico_id`, `tecnico` (0.5.0: `{ id_externo }` del vínculo con Argos de la ficha del técnico, o null), `bodega_id` (o null), `cerrada_en`, `retirar_servicio` (bool: retiro exitoso), `equipos_validados` (bool: false si algún equipo retirado no se verificó contra el comodato de Argos —sin conector o Argos caído—; tratarlo como dato no verificado), `materiales[]` (`producto_id`, `cantidad` —sumada por producto y bodega, 3 decimales—, `bodega_id` (o null), `excede_existencia`, `modalidad` (0.5.0, o null)), `equipos[]` (`id`, `accion` `instalar` \| `retirar`, `unidad_id`, `serie`, `mac` `AA:BB:CC:DD:EE:FF`, `modelo`, `condicion` `buena` \| `danada` \| `perdido` (solo retirar; `perdido` = pérdida del comodato), `bodega_id`, `validado`) | `ordenes.cerrar`. Lo consume el núcleo (I1) para los pendientes en Argos (contrato §3): con `resultado = exitosa`, activar la instalación y el comodato, devoluciones o pérdidas, retiro del servicio; materiales → consumo desde la bodega. `cargo` (o null): `{ producto_id?, monto? (texto/número > 0, 2 decimales), descripcion }` si el tipo de orden o su plantilla dicen cobrar (visita cobrada; ver «Cargo» en `apps/api/internal/campo/README.md`). |

## Comercial (I2)

Emite el módulo `comercial`, con `empresa_id` en el sobre = la del prospecto (null sin conector).
Montos como texto decimal con 2 cifras (`"250.00"`). Fechas en UTC.

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `prospecto.creado` | `prospecto_id`, `numero`, `empresa_id`, `origen` (`panel`, `web`, `referido`, `argos`, `otro`), `zona_id` (o null), `vendedor_id` (o null) | `prospectos.crear`. |
| `prospecto.actualizado` | `prospecto_id`, `campos[]` (`nombre`, `documento`, `telefono`, `correo`, `direccion`, `lat`, `lon`, `zona_id`, `plan_interes_id`, `plan_interes_nombre`, `origen_detalle`, `notas`) | `prospectos.editar` (hasta cotizado). |
| `prospecto.asignado` | `prospecto_id`, `vendedor_id` (o null), `vendedor_anterior_id` (o null) | `prospectos.asignar`. |
| `prospecto.estado_cambiado` | `prospecto_id`, `de`, `a`, `motivo` (o null) | Cada cambio de estado del embudo: `contactar`, factibilidad (nuevo/contactado → factibilidad), cotizar (→ cotizado; también desde contrato si se anula el borrador), contratar (→ contrato), `perder`, `reabrir` (perdido → contactado) y `ganar`. |
| `prospecto.actividad_registrada` | `prospecto_id`, `actividad_id`, `tipo` (`llamada`, `visita`, `nota`, `mensaje`), `proxima_fecha` (o null) | `prospectos.registrar_actividad`. |
| `prospecto.perdido` | `prospecto_id`, `empresa_id`, `motivo`, `de` (estado anterior) | `prospectos.perder`. Anula el contrato vivo (`contrato.anulado`). |
| `prospecto.ganado` | `prospecto_id`, `empresa_id`, `modo` (`local` \| `argos`), `contrato_id`, `cotizacion_id`, `plan_id` (o null), `obra_confirmada`, `router_id` (argos), `orden_id` (local: la orden de instalación; argos: null hasta el alta), `pendientes[]` (argos: el id del pendiente `crear_cliente`) | `prospectos.ganar`. |
| `prospecto.alta_actualizada` | `prospecto_id`, `estado` (`esperando_cliente` \| `esperando_servicio` \| `completa`), `cliente_id`, `servicio_id` (o null), `orden_id` (o null), `error` (solo si el alta quedó para revisión manual) | El alta en Argos avanza (actor sistema): cliente creado → se encola el servicio; servicio creado → orden de instalación (`orden.creada` con `origen = sistema` y `prospecto_id`). |
| `factibilidad.evaluada` | `prospecto_id`, `factibilidad_id`, `resultado` (`viable`, `no_viable`, `requiere_obra`, `indeterminado`), `motor` (`distancia`, `manual`; `ozmap` en la fase 6), `motivo` (indeterminado: `sin_conector`, `conector_apagado`, `sin_coordenadas`, `sin_empresa`, `sin_instantanea`, `sin_routers_con_coordenadas`, `planta_no_disponible`; si no, null), `router_id` (sugerido, o null), `caja` (o null), `distancia_m` (o null), `anula_id` (la automática no_viable que anula una manual, o null) | `factibilidad.evaluar` y `factibilidad.registrar_manual`. |
| `factibilidad.umbrales_cambiados` | `empresa_id`, `viable_m`, `obra_m` | `factibilidad.configurar`. |
| `cotizacion.creada` | `cotizacion_id`, `prospecto_id`, `numero`, `revision`, `plan_id` (o null sin conector), `total_mensual`, `total_unico`, `moneda`, `reemplaza_id` (la anterior, que queda `reemplazada`, o null) | `cotizaciones.crear`. |
| `cotizacion.aceptada` | `cotizacion_id`, `prospecto_id`, `numero` | `cotizaciones.aceptar`. |
| `contrato.creado` | `contrato_id`, `prospecto_id`, `cotizacion_id`, `numero` | `contratos.crear`. |
| `contrato.firmado` | `contrato_id`, `prospecto_id`, `numero`, `adjunto_id` (PDF firmado), `sha256` | `contratos.firmar`. El SVG de la firma nunca va en el evento. |
| `contrato.anulado` | `contrato_id`, `prospecto_id`, `de` (`borrador` \| `firmado`), `motivo` | Una cotización nueva anula el borrador; perder anula el vivo. |

Pendientes hacia Argos (contratos/argosisp.md §3, «Prospecto ganado»), `entidad = prospecto`:
`crear_cliente` (`POST /clientes`, `iris:cliente:<prospecto_id>`) y `crear_servicio`
(`POST /servicios`, `iris:servicio:<prospecto_id>`, `depende_de` = el del cliente). Cuerpos según
Argos 1.2 (solo los opcionales con valor; se serializan una vez y se guardan en el pendiente: la
cola y la repetición mandan los mismos bytes, que es la huella de idempotencia de Argos):

- `POST /clientes`: `nombre`, `codigo_externo = "iris:prospecto:<id>"`, y si hay: `nit_cui` (documento), `email`, `telefono`, `direccion`, `coordenadas` (`"lat,lng"`), `zona_id`. Sin `empresa_id` (el cliente es de la instalación).
- `POST /servicios`: `cliente_id`, `router_id` (el sugerido por la factibilidad o el elegido al ganar), `plan_id`, `empresa_id` (la del prospecto), `estado_inicial = "pendiente_instalacion"`, y si hay: `direccion`, `coordenadas`. Sin `origen_tipo`/`origen_id`.
- Respuesta: `2xx` con `{ id }`; repetir la clave con los mismos bytes devuelve el 2xx original (`Idempotent-Replayed: true`). Tras recuperar el id, Iris comprueba con `GET /clientes/{id}` que `codigo_externo` sea el del prospecto (y con `GET /servicios/{id}` que el servicio sea de ese cliente); si no, `alta.error`.

Consume `integracion.pendiente_hecho` con `entidad = prospecto` (avanza el alta; idempotente).
