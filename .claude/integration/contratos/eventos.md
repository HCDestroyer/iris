# Payloads de eventos de Iris

Todo evento sale por `integracion.outbox` con esta forma: `{ id (uuid v7), evento, ocurrido_en, empresa_id, payload }`. Cada agente agrega aquí el detalle de sus eventos, en la misma PR que el código que los emite. El catálogo resumido está en `.claude/CLAUDE.md`.

## Legal (núcleo)

Emite el módulo `nucleo/legal` (`contratos/legal.md`), con `empresa_id = null` (son los datos de la instalación).

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `datos_legales.cambiados` | `campos[]` (los que cambiaron, en snake_case: `razon_social`, `nombre_comercial`, `correo_privacidad`, `telefono`, `direccion`, `pais`), `completo` (están todos los obligatorios: las páginas públicas están listas para Meta), `faltan[]` (obligatorios vacíos, en camelCase como en la API) | `POST /datos-legales/guardar` con algún cambio. Nunca los valores. Guardar lo mismo no emite. |

## Identidad (núcleo, I1)

Emite el módulo `identidad`, con `empresa_id = null` (los usuarios de Iris no pertenecen a una empresa), salvo los de empresas y membresía, que llevan la empresa afectada.

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `usuario.creado` | `usuario_id`, `tipo` | Alta de un usuario (`usuarios.crear`, también desde `iris admin crear`). |
| `usuario.actualizado` | `usuario_id`, `campos[]` (nombres de los campos que cambiaron) | `usuarios.editar`. También `yo.cambiar_contrasena` (`POST /yo/contrasena`, el propio usuario; `campos` = `["contrasena_hash", "cambio_requerido"]` o solo el primero). |
| `usuario.desactivado` | `usuario_id`, `motivo` | `usuarios.desactivar`. Revoca sus sesiones; sus tokens dejan de autenticar mientras esté inactivo, sin revocarse ni emitir `token.revocado`. |
| `usuario.mfa_reiniciado` | `usuario_id`, `motivo`, `sesiones_cerradas` (número), `tokens_revocados` (número), `tokens_conservados` (número: los que usa un conector activo de Argos u Horus, que no se revocan porque cortaría la integración) | `usuarios.reiniciar_mfa` (un administrador, nunca el propio usuario): borra su secreto TOTP, reemplaza su contraseña por una temporal (nunca va en el evento) que debe cambiar al entrar, revoca sus sesiones y, salvo `revocarTokens: false`, sus tokens (un `token.revocado` con `motivo = mfa_reiniciado` por cada uno). Un segundo reinicio sin MFA configurado es 422. |
| `usuario.contrasena_restablecida` | `usuario_id`, `motivo`, `sesiones_cerradas` (número), `tokens_revocados` (número; 0 salvo `revocarTokens: true`), `tokens_conservados` (número: los que usa un conector activo) | `usuarios.restablecer_contrasena` (un administrador sobre otra persona; nunca la propia ni una integración): reemplaza su contraseña por una temporal (nunca va en el evento) que debe cambiar al entrar, revoca todas sus sesiones y borra sus vínculos de autologin (un `usuario.sso_desvinculado` con `motivo = contrasena_reiniciada` por cada uno). No toca el MFA. Con `revocarTokens: true`, también sus tokens (un `token.revocado` con `motivo = contrasena_restablecida` por cada uno). |
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
| `token.eliminado` | `token_id`, `usuario_id`, `motivo` (`manual`, `lote`, `conector`) | `tokens.eliminar` (borrado lógico de un token revocado o vencido; uno vigente es 409 `token_vigente` y uno que usa un conector activo 409 `token_en_uso_por_conector`), `tokens.eliminar_inactivos` (uno por token, `motivo = lote`) e `integraciones.eliminar` (el `iris_pat_` del conector, `motivo = conector`; si aún valía, antes un `token.revocado` con `motivo = conector_eliminado`). Su hash se borra; la fila queda para la auditoría. Eliminar uno ya eliminado responde 200 sin evento. |
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
| `conector.eliminado` | `conector_id`, `sistema`, `motivo` (`manual`, `lote`), `token_id` (o null) | `integraciones.eliminar` y `integraciones.eliminar_apagados` (uno por conector): borrado lógico de un conector apagado; sus credenciales cifradas se borran y su `iris_pat_` (`token_id`) se elimina. Uno activo es 409 `conector_activo`; uno que el activo reemplazó con la reconexión sin confirmar, 409 `conector_en_uso`. Repetirlo responde 200 sin evento. |
| `conector.actualizado` | `conector_id`, `sistema`, `campos[]` (`url_publica`; `version_remota`, `contrato_remoto`, `contrato_informado`, `version_estado`) | `integraciones.configurar_url` (`POST /integraciones/conectores/{id}/url-publica`), solo si cambió algo. También `integraciones.informar_contrato` (job del worker, contrato `argosisp.md` 0.5.0 §3.3; también encolado ya con `POST /integraciones/conectores/{id}/revisar-version`): Iris informó su contrato a Argos (`PUT /integraciones/iris/contrato`), leyó otra versión en `GET /integraciones/iris/estado` o cambió el diagnóstico de la lectura (`version_estado`: `ok`, `ruta_no_existe`, `sin_permiso`, `no_responde`, `error`; el detalle va en el conector, no en el evento). |
| `argos.servicio_retirado` | `servicio_id`, `cliente_id` (o null), `de`, `a` (`retirado`), `entrante_id` | Argos avisó `servicio.estado_cambiado` con `a = retirado` **y** Iris lo confirmó releyendo `GET /servicios/:id` en Argos (ids y empresa salen de esa lectura, no del payload). Gancho para que I2 proponga la orden de retiro (Fase 2) si el servicio tiene equipos en comodato. Una vez por webhook (deduplicado por el `id` firmado del cuerpo). |
| `argos.servicio_eliminado` | `servicio_id`, `cliente_id` (o null), `empresa_id` (o null), `router_id` (o null), `entrante_id` | Contrato 0.3.1: Argos avisó `servicio.eliminado` (solo servicios que nunca se usaron), Iris lo confirmó con el 404 problem+json `urn:argosisp:problema:no_encontrado` de `GET /servicios/:id` (un 403 `empresa_no_permitida` u otro 404 no confirman). Si la instantánea tenía el servicio, su empresa y su cliente mandan sobre el payload (una discrepancia se anota en la auditoría). La instantánea ya no lo tiene; queda la lápida `integracion.argos_eliminados` con su última copia (código y plan) y la ficha del cliente lo lista en `serviciosEliminados`. Órdenes y tickets que lo referencian conservan sus datos y lo muestran «eliminado en Argos» (`argos.ServiciosEliminados`). Lleva la empresa del servicio. Una vez por servicio (un segundo aviso encuentra la lápida y no emite). |
| `argos.router_eliminado` | `router_id`, `empresa_id` (o null = router compartido), `entrante_id` | Contrato 0.3.1: Argos avisó `router.eliminado` (solo routers sin historial), `GET /routers/:id` respondió 404 problem+json `urn:argosisp:problema:no_encontrado`; la empresa de la instantánea (null = compartido) manda sobre el payload. Sale de `integracion.argos_routers` (factibilidad ya no lo usa; una factibilidad evaluada conserva el id) con lápida: una carga de catálogos no lo recrea salvo que `GET /routers/:id` responda 200 después de la confirmación (`argos.router_restaurado`). Una vez por router. |
| `argos.servicio_restaurado` | `servicio_id`, `cliente_id`, `empresa_id` (o null), `eliminado_entrante_id` | Un servicio con lápida volvió en una lista de Argos (ficha del cliente) y `GET /servicios/:id` respondió 200 pedido después de la confirmación (reloj de Postgres): la lápida estaba mal o Argos lo restauró. Se borra la lápida y vuelve a la instantánea (auditado, log de advertencia). Lleva la empresa del servicio. |
| `argos.datos_cambiados` | `entidad` (`servicio` \| `cliente`), `cliente_id` (o null), `servicio_id` (o null), `empresa_id` (o null), `campos[]` (con prefijo: `servicio.direccion`, `servicio.coordenadas`, `servicio.referencia`, `servicio.contacto_nombre`, `servicio.contacto_telefono`, `cliente.direccion`, `cliente.referencia`, `cliente.telefono`…), `entrante_id` | Contrato 0.5.0: `servicio.datos_cambiados`, o `cliente.actualizado` con algún campo que copian las órdenes (dirección, coordenadas, referencia, contacto, teléfono, móvil, nombre; `email` no). La instantánea ya se refrescó (si Iris tenía al cliente). Campo marca sus órdenes abiertas «dato de Argos cambió» (`orden.datos_argos_cambiados`) sin pisar lo copiado. Lleva la empresa del webhook. |
| `argos.traslado_cambiado` | `traslado_id`, `estado` (`enviado` \| `recibido` \| `cancelado`, el guardado: un estado final no vuelve a enviado), `empresa_id` (o null), `usuario_id` (técnico de Iris por el vínculo de `agenda.tecnicos`, o null si nadie lo tiene), `entrante_id` | Contrato 0.5.0: `inventario.traslado_enviado` / `_recibido` / `_cancelado`. Se guarda en `integracion.argos_traslados` (lista «Por recibir» de la app de campo). Aviso para el técnico. |
| `argos.router_restaurado` | `router_id`, `empresa_id` (o null), `eliminado_entrante_id` | Igual, para un router que vuelve en la carga de catálogos y cuyo `GET /routers/:id` respondió 200. |
| `argos.comprobante_resuelto` | `comprobante_id`, `estado` (`aprobado` \| `rechazado`), `empresa_id` (o null), `cliente_id`, `servicio_id` (o null), `canal` (`portal` \| `panel` \| `iris`), `pago_id` (o null), `servicio_estado` (o null), `entrante_id`. Sin `monto`, `motivo` ni `causa`: vienen sin verificar y salen de releer Argos | Contrato 0.6.0 §7.2 (sin cambios en 0.7.0): webhooks `comprobante.aprobado` / `comprobante.rechazado` de Argos, decida caja en su panel o una persona desde Iris (§7.7). Sale del payload firmado, sin releer Argos; uno por webhook (reprocesar el entrante no lo repite). Lo consume mensajería como **aviso**: con `empresa_id`, marca la boleta de ese comprobante en esa empresa y relee `GET /comprobantes/:id` antes de aplicar nada (`boleta.aprobada` / `boleta.rechazada` / `boleta.corregida`); el resto del payload no se usa. |
| `argos.promesa_cambiada` | `promesa_id`, `estado` (`activa` \| `cumplida` \| `incumplida` \| `cancelada`), `tipo` (`promesa` \| `reconexion_temporal`), `empresa_id` (o null), `cliente_id`, `servicio_id`, `comprobante_id` (o null), `canal` (`portal` \| `ia` \| `operador` \| `supervisor` o null), `vence` (RFC 3339 UTC o null), `motivo_reconexion` (`fuera_de_horario` \| `sin_personal` o null: null en una `promesa`, con un Argos < 0.8.0 o con un valor desconocido), `motivo` (solo `cancelada`: `comprobante_rechazado` \| `vencida_sin_revisar` \| `otro`; nunca texto libre de Argos), `entrante_id`. **Nunca** el monto | Contrato 0.6.0 §7.4 y 0.8.0 §7.3: Argos avisó `promesa.creada`, `promesa.cumplida`, `promesa.incumplida` o `promesa.cancelada` (payload firmado, sin releer Argos). Lo consume mensajería (`mensajeria.reconexiones`) para la reconexión temporal de la conversación. |
| `argos.caidas_repetidas` | `servicio_id`, `empresa_id`, `cliente_id` (o null), `router_id` (o null), `sesiones`, `ventana_minutos`, `desde`, `hasta` (RFC 3339 UTC o null), `entrante_id` | Contrato 0.13.0 §3.11 (A49): Argos avisó caídas repetidas de un servicio. Solo con la empresa en el alcance del conector y el servicio de esa empresa (instantánea o `GET /servicios/:id`). Lo consume soporte (B29). |
| `argos.falla_general_abierta` / `argos.falla_general_cerrada` | `incidente_id`, `empresa_id`, `alcance` (`router` \| `olt` \| `pon` \| `zona`), `alcance_id` (texto), `desde`, `hasta` (o null), `entrante_id`; abierta suma `servicios[]` (los que la instantánea tiene en la empresa del evento), `descartados` (de otra empresa según la instantánea), `por_verificar` (ids que Iris no conoce) y `paginando` (hay más afectados por leer) | Contrato 0.13.0 §3.11 (A50): un evento por empresa afectada. La apertura sale sin leer Argos; los ids desconocidos y el resto (`GET /incidentes-red/:id/servicios`) los verifica el job `argos.verificar_incidente`. Nunca nombres ni direcciones. Lo consume soporte (B30). |
| `argos.falla_general_servicios` | `incidente_id`, `empresa_id`, `entrante_id`, `servicios[]` (verificados con `GET /servicios/:id`), `descartados` (404, 403 u otra empresa), `no_verificados` (Argos falló 3 veces con ese id) | Una tanda del job `argos.verificar_incidente` (con presupuesto propio; un error con un id solo afecta a ese id). Lo consume soporte (suma los servicios a la falla abierta). |
| `usuario.argos_abierto` | `usuario_id`, `conector_id`, `con_destino` (bool) | `integraciones.abrir_argos` (`POST /integraciones/argosisp/abrir`, contrato 0.7.0 §1.3): una persona pidió su enlace de autologin Iris → Argos («Abrir en Argos» con destino, «Vincular mi usuario de Argos» sin él) y Argos lo entregó. **Nunca** la URL ni el ticket (en la auditoría, `url: "***"`). No dice si el vínculo se creó: eso solo lo sabe Argos. |
| `argos.sincronizacion_iniciada` | `sincronizacion_id` (UUID v7), `conector_id`, `origen` (`activacion` \| `manual`), `excluir_retirados` (bool: Argos ≥ 0.8.0), `contrato_remoto` (o null) | Contrato 0.8.0 §2.5 (B7): empieza la primera sincronización de un conector de Argos (al activar uno que nunca la tuvo, o con `integraciones.sincronizar`: una nueva o una `con_errores` reabierta con su mismo id). `empresa_id = null`, como los `conector.*`. |
| `argos.sincronizacion_terminada` | `conector_id`, `sincronizacion_id` (UUID v7), `resultado` (`completa` \| `con_errores`), `recursos[]` (`{ recurso, estado, filas }`, con `recurso` = `empresas` \| `zonas` \| `planes` \| `routers` \| `cuentas_bancarias` \| `bancos` \| `clientes` \| `servicios` y `estado` = `completa` \| `con_errores`; `filas` = las guardadas), `iniciada_en`, `terminada_en`. Sin datos de clientes | Contrato 0.8.0 §2.5 (B7): terminó la primera sincronización (o se rindió en el último intento del job). Son los mismos estados y recursos que Iris informa a Argos con `PUT /integraciones/iris/sincronizacion`. Una reabierta que vuelve a terminar lo emite otra vez. `empresa_id = null`. |
| `argos.certificado_cambiado` | `conector_id`, `certificado_id`, `anterior_id`, `host`, `huella_sha256`, `huella_anterior` (SHA-256 del certificado hoja, hex), `emisor` | Contrato 0.8.0 §1.5 («registrar y avisar»): la API de Argos presentó, en el mismo conector y sin reconectar, un certificado distinto del último visto (la cadena ya validó contra las CA públicas). Solo informa: no marca el conector ni detiene nada. Lo consume notificaciones: campana `argos_certificado` a quien tiene `integraciones.conectar` global, uno por certificado nuevo. El primer certificado de un conector no lo emite. `empresa_id = null`. |
| `argos.contactos_cambiados` | `cliente_id`, `empresa_id` (o null), `contacto_id` (o null), `accion` (`creado`, `editado`, `quitado`), `persona_cliente_id` (o null), `entrante_id`. **Nunca** números ni nombres | Contrato 0.10.0 §3.5 y 0.10.1 §3.8 (B22, B25): Argos avisó `cliente.contactos_cambiados` de un cliente que Iris conoce e Iris refrescó su caché de contactos (`integracion.argos_contactos`): con `quitado`, el contacto sale al momento; si no, relee `GET /clientes/:id/contactos` con `X-Iris-Actor: sistema` (0.14.2). Si Argos no acepta esa lectura, la caché del cliente se olvida y la vuelve a leer la próxima persona que atienda. También sale (con `accion: quitado`, `contacto_id` y `entrante_id` null) cuando una relectura de una persona encuentra que Argos ya no devuelve contactos que Iris tenía. Lo consume la mensajería (`mensajeria.contactos_argos`): las conversaciones asociadas por un contacto que ya no está en la caché pierden la verificación del número. |
| `argos.cuenta_bancaria_cambiada` | `cuenta_bancaria_id`, `empresa_id`, `campos[]` (solo nombres: `banco_id`, `numero`, `alias`, `titular`, `tipo`, `moneda`, `activa`), `activa` (el valor después del cambio), `creada` (bool), `entrante_id` (o null cuando el cambio lo trajo una relectura: la primera carga, la relectura periódica de 6 h, la de más de 24 h al usarla o la de la cuenta antes de una boleta). **Nunca** el número de cuenta ni el titular | Contrato 0.8.0 §7.8: Argos avisó `cuenta_bancaria.cambiada` (al crear o editar una cuenta) e Iris releyó `GET /cuentas-bancarias/:id` y refrescó su instantánea, o una relectura encontró la cuenta distinta (nueva: `creada: true` con todos los campos). Con el webhook, `campos[]` y `creada` son los de Argos; en una relectura, los que cambiaron en la instantánea. Una cuenta con `activa: false` deja de ofrecerse al cliente. Sin consumidores internos todavía. |
| `argos.titular_cambiado` | `servicio_id`, `empresa_id` (o null), `de_cliente_id` (o null), `a_cliente_id`, `de_cliente_eliminado`, `origen_tipo` (o null), `origen_id` (o null), `tipo_titular` (`persona` \| `empresa`; persona si Argos < 0.14.1 no lo manda), `entrante_id`. **Nunca** el documento, la identidad ni el texto del motivo | Contrato 0.10.0 §2.3 y §3.4.4 (B17): Argos avisó `servicio.titular_cambiado` de un servicio que Iris tiene en su instantánea; Iris releyó al titular nuevo (`GET /clientes/:id` con sus servicios) y solo emite si Argos confirma que el servicio ya es suyo (la instantánea queda refrescada). La fila del servicio se bloquea y guarda la hora **de Argos** del último cambio aplicado (`titular_cambiado_en`): un aviso con `ocurrido_en` anterior (A→B después de B→C) no retrocede ni emite (MEDIA 2 de #280). Solo se comparan horas de Argos (`integracion.entrantes.ocurrido_origen = argos`, hasta la recepción más 5 minutos); un aviso sin hora de Argos o con una hora futura no se ordena por hora ni la deja: manda la ficha releída. Consumen comercial (el prospecto pasa al cliente nuevo) y campo (`orden.titular_movido`), los dos solo si la instantánea dice que el servicio es de `a_cliente_id`. |

Además, `reconexion.solicitada` (más abajo, en «Asistente autónomo») suma el motivo `sin_personal` (B4, implementado), con `reconexion.actualizada` y `reconexion.ajustes_cambiados`.

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
| `integracion.pendiente_desbloqueado` | base + `depende_de` | La dependencia terminó `hecho` (tras reintentarla) o `resuelto_manual`, y todas las del dependiente están así: vuelve a `pendiente`. |
| `integracion.pendiente_resuelto_manual` | base + `de` (`fallido_negocio`), `motivo` (qué se hizo en Argos, 5–1000), `usuario_id` (o null) | `integraciones.resolver_pendiente` (manual, auditado): un pendiente que Argos rechazó por negocio (o que nació incompleto) se corrigió directo en Argos. Queda `resuelto_manual` (final: nunca vuelve a salir; para sus dependientes cuenta como hecho, así que el cargo que esperaba al consumo sale). No aplica a `agotado` (se reintenta) ni a `bloqueado` (espera a su dependencia): 422 `pendiente_no_resoluble`. Un consumo con un cargo de un producto del consumo esperándolo exige `cargo` (`liberar` o `resolver_tambien`; sin él, 422 `decision_cargo_requerida`); con `resolver_tambien` el cargo sale con su propio `integracion.pendiente_resuelto_manual` (mismo `motivo`, `de` = `bloqueado`) y no se manda. Los dependientes que esperaban su respuesta (marcadores) emiten `integracion.pendiente_fallido` (`detalle` = `dependencia_resuelta_a_mano: …`) y quedan incompletos. |

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
| `bodega_id` | Bodega del técnico: consumo y devoluciones (una línea o un equipo pueden traer la suya). Un consumo por orden: todas las líneas de una misma bodega. Con un Argos que informó contrato ≥ 0.14.0, un consumo por actor y bodega (`iris:consumo:<orden_id>:<usuario_id>:<bodega_id>`). |
| `tecnico_id` | El usuario técnico: con `tecnico`, queda en el pendiente (`tecnico_usuario_id`) para corregir el vínculo al reintentar a mano. |
| `tecnico` (0.5.0) | `{ id_externo }` del vínculo de la ficha de agenda con Argos, congelado al cerrar, o null. Con Argos ≥ 1.6.0 (`version_remota` del conector) consumo, comodato y devolución mandan `tecnico` en vez de `bodega_id` (una bodega propia de la línea o el equipo distinta de la de la orden sigue mandando `bodega_id`, que en Argos manda sobre `tecnico`). Sin vínculo o con Argos < 1.6.0, `bodega_id` como antes. Un cierre repetido que solo cambia `bodega_id` ↔ `tecnico` no pisa lo encolado ni marca conflicto. Con un Argos ≥ 0.14.0, `tecnico` va por línea (material o equipo) y es el I1 del `usuario_id` de esa línea. |
| `retirar_servicio` (bool) | En un `retiro` exitoso, `true` = el cliente pidió retirar el servicio → `POST /servicios/:id/retirar`. Ausente = false. |
| `materiales[]` | `{ producto_id, cantidad (número o texto, > 0; si no, `cantidad_no_positiva`), bodega_id?, modalidad? }` → `POST /inventario/consumos` con `lineas[]`. 0.5.0: `modalidad` (`comodato` \| `consumible_gasto` \| `consumible_cobro` o null si no se sabía): las de `comodato` no van en el consumo (`integracion.pendiente_omitido` con `material_comodato`). 0.14.0: `usuario_id` (quien registró el material), para el consumo por actor; las líneas se suman por producto, bodega y usuario. |
| `equipos[]` | `{ accion: instalar \| retirar, unidad_id?, serie?, mac?, condicion?, bodega_id?, usuario_id (0.14.0: quien lo registró) }`. Instalar: `unidad_id` o `serie`. Retirar: `unidad_id` obligatorio; `condicion: "perdido"` → pérdida; otra condición (`bueno`, `danado`…) → devolución con esa condición. En `cambio_equipo`, cada asignación espera a todas las devoluciones/pérdidas de la orden. **Desde 0.12.0 (contrato con Argos §3.10)** suma `accion: cambio` (`{ sale_unidad_id, entra: { serie \| mac \| unidad_id }, motivo, destino_sale, cobro_propuesto? }` → `POST /servicios/:id/equipos/cambiar`, en lugar de devolución más asignación) y `accion: no_recuperado` (`{ unidad_id, motivo, foto_ref }` → `POST /servicios/:id/equipos/:unidad_id/no-recuperado`; `foto_ref` obligatoria desde 0.14.0); `condicion` de retirar pasa a `funciona` \| `revision`, y `perdido` ya no se usa (la pérdida la declara una persona en Argos). **Desde 0.14.0**, `cambio` y `no_recuperado` vienen de las operaciones `equipo.cambiar` y `equipo.no_recuperado` del lote (`contratos/sync.md` §2), que salen a Argos al aplicarse: en `orden.cerrada` solo los resumen y la cola no los vuelve a encolar. |
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
| `cuadrilla.creada` | `cuadrilla_id`, `empresa_id`, `version`, `nombre`, `zonas[]`, `miembros[]` (`{ usuario_id, id_externo }`, `id_externo` null sin vínculo I1), `jefe_usuario_id` (o null) | `agenda.crear_cuadrilla`. Va también a Argos (contrato 0.11.0 §6.6). |
| `cuadrilla.actualizada` | `cuadrilla_id`, `empresa_id`, `version`, `campos[]` (`nombre`, `zonas`, `activa`, `capacidad`, `miembros`, `jefe`), `activa`, `miembros[]` (`{ usuario_id, id_externo }`), `jefe_usuario_id` (o null) | Editar, activar, desactivar, fijar la capacidad, cambiar los miembros o `cuadrillas.asignar_jefe` (`POST /cuadrillas/{id}/asignar-jefe`), y por cada cuadrilla afectada al cambiar `tecnico.cuadrilla_id`, `tecnico.activo`, el vínculo I1 o al desactivar el usuario (con `miembros[]` completo). Va también a Argos (contrato 0.11.0 §6.6). |
| `tecnico.creado` | `tecnico_id` (ficha), `usuario_id`, `cuadrilla_id` (o null) | `agenda.crear_tecnico`. |
| `tecnico.actualizado` | `tecnico_id`, `usuario_id`, `campos[]` (`cuadrilla_id`, `bodega_id`, `habilidades`, `base`, `activo`, `turnos`, `capacidad`), `activo` | Editar, activar, desactivar, fijar turnos o capacidad. |
| `tecnico.ausencia_registrada` | `tecnico_id`, `usuario_id`, `ausencia_id`, `inicio`, `fin`, `motivo`, `citas_en_conflicto` (cuántas citas vivas caen dentro) | `agenda.registrar_ausencia`. Las citas no se mueven solas. |
| `tecnico.ausencia_anulada` | `tecnico_id`, `usuario_id`, `ausencia_id`, `motivo` (o null) | `agenda.anular_ausencia`. |
| `tecnico.argos_vinculado` | `tecnico_id`, `usuario_id`, `empresa_id`, `version` (entera y creciente por técnico; 0.14.0), `id_externo` (usuario de Argos), `anterior_id_externo` (o null) | `agenda.vincular_argos` (I1, contrato `argosisp.md` 0.5.0 §3.3): un administrador vincula la ficha con un usuario de `GET /inventario/tecnicos` que tiene bodega técnico activa en la empresa activa. Desde entonces, `orden.cerrada` lleva `tecnico { id_externo }`. Sin el correo (dato personal). |
| `tecnico.argos_desvinculado` | `tecnico_id`, `usuario_id`, `empresa_id`, `version` (0.14.0), `id_externo` (el que tenía), `motivo` (o null; `desactivado` cuando se desactiva el técnico o su usuario) | `agenda.desvincular_argos`. Desde 0.14.0, también al desactivar el técnico o su usuario (`motivo: desactivado`). Sus órdenes vuelven a `bodega_id`; lo ya encolado no cambia. |
| `tarea.creada` | `tarea_id` (uuid), `empresa_id` (o null), `tecnico_id` (usuario dueño), `creada_por` (o null), `inicio`, `fin`, `duracion_min`, `orden_id` (o null), `cliente_id` (o null), `origen` (`campo` \| `panel` \| `api`), `solapa_con` (cuántas citas vivas del técnico se cruzan; no bloquea) | `agenda.crear_tarea` (`POST /agenda/tareas` o `tarea.crear` del lote, con el id de la operación como id de la tarea). Un reintento con el mismo id no lo repite. |
| `tarea.completada` | `tarea_id`, `empresa_id`, `tecnico_id`, `completada_por`, `completada_en` | `agenda.completar_tarea` (el dueño o quien lo supervisa; `tarea.completar` del lote). Ya completada: sin evento. |
| `tarea.anulada` | `tarea_id`, `empresa_id`, `tecnico_id`, `anulada_por`, `motivo` (o null) | `agenda.anular_tarea` (borrado lógico; `tarea.anular` del lote). Ya anulada: sin evento. |
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

## Mensajería (I1)

Emite `internal/mensajeria` (proceso `cmd/mensajeria`, contrato Iris ↔ ArgosISP 0.6.0 §6). Llevan
`empresa_id` = la empresa del aviso, del permiso o del canal (null en modo sin conector). Nunca llevan
el teléfono del cliente. `origen` dice quién pidió el aviso (`argos`: un token de integración de
Argos; `iris`: cualquier otro). Fechas en UTC (RFC 3339).

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `aviso.creado` | `aviso_id` (uuid), `empresa_id`, `cliente_id`, `servicio_id` (o null), `plantilla`, `estado` (`en_cola` \| `bloqueado`), `motivo` (o null: `sin_permiso`, `baja`, `limite_diario`, `sin_contacto`, `canal_no_disponible`), `origen`, `prioridad`, `programado_en` (o null), `aviso_padre_id` (solo en una copia a un contacto, §3.5; nunca el número) | `mensajeria.enviar` (`POST /mensajeria/avisos`). Una repetición con la misma `Idempotency-Key` no emite. Un aviso de cobro en cola emite además uno por cada copia a un contacto con `avisos`. |
| `aviso.enviado` | `aviso_id`, `canal`, `canal_id` (o null con el simulado), `origen` | El canal aceptó el envío (Meta devolvió su id). |
| `aviso.entregado` | `aviso_id`, `idempotency_key`, `empresa_id`, `canal`, `entregado_en`, `leido_en` (o null), `origen`, `cliente_id` | El canal confirmó la entrega (o la lectura antes que la entrega). Estado final: uno por aviso. Va a Argos como webhook (§6.6) si `origen = argos`. |
| `aviso.leido` | `aviso_id`, `leido_en`, `origen` | Lectura después de la entrega. Solo interno: no va a Argos. |
| `aviso.fallido` | `aviso_id`, `idempotency_key`, `empresa_id`, `canal` (o null), `motivo` (`vencido`, `numero_invalido`, `rechazado_por_canal`, `agotado`), `detalle` (o null), `origen`, `cliente_id` | Estado final del aviso (uno por aviso): venció al terminar el silencio, el canal lo rechazó, se agotaron los reintentos o no hubo acuse en 72 h. Va a Argos si `origen = argos`. |
| `consentimiento.registrado` | `consentimiento_id`, `empresa_id`, `cliente_id`, `canal`, `estado` (`acepta` \| `baja`), `origen` (`argos_panel`, `argos_portal`, `cliente`, `agente`), `ocurrido_en`, `estado_vigente`, `por_numero` (bool: el permiso es de un número de contacto del cliente, contrato 0.10.0 §6.4; **nunca** el número) | `mensajeria.registrar_consentimiento` o una baja por chat. Una repetición no emite. Con `por_numero`, `estado_vigente` es el de ese número y una baja no emite `contacto.baja`. |
| `contacto.baja` | `cliente_id`, `empresa_id`, `canal`, `ocurrido_en`, `origen` (`cliente` \| `agente`), `consentimiento_id` | Una baja dada en Iris (el cliente escribe `BAJA` o `ALTO`) que queda vigente. Va a Argos (§6.6) sin `consentimiento_id`. |
| `canal_mensajeria.creado` | `canal_id`, `empresa_id`, `tipo` (`whatsapp_cloud`), `canal` (`whatsapp`) | `mensajeria.configurar` (`POST /mensajeria/canales`). |
| `canal_mensajeria.actualizado` | `canal_id`, `empresa_id`, `campos[]` (las credenciales cambiadas van como `credenciales`, nunca el valor) | `POST /mensajeria/canales/{id}/actualizar`. |
| `canal_mensajeria.activado` / `canal_mensajeria.desactivado` | `canal_id`, `empresa_id` | `POST /mensajeria/canales/{id}/activar` / `desactivar`. |
| `canal_mensajeria.calidad` | `canal_id`, `evento` (de Meta: `FLAGGED`, `UNFLAGGED`, `DOWNGRADE`, `UPGRADE`), `pausado_hasta` (o null) | Meta cambia la calidad del número: se pausa 24 h o se libera (§6.5). |
| `canal_mensajeria.probado` | `canal_id`, `empresa_id` (o null), `estado` (`conectado`, `token_invalido`, `numero_no_coincide`, `sin_permiso`, `error_meta`), `origen` (`envio`; ausente = la prueba) | `POST /mensajeria/canales/{id}/probar` (contratos/conversaciones.md §1.1). Con `origen: envio`: Meta rechazó el token o los permisos al enviar y el número quedó marcado, o aceptó un envío después y volvió a `conectado` (una vez por cambio de estado). Nunca credenciales ni el detalle de Meta. |

**Webhooks hacia Argos (§6.6, 0.6.1).** El suscriptor `mensajeria.webhooks` del relay toma
`aviso.entregado`, `aviso.fallido` (solo `origen = argos`) y `contacto.baja`, y los entrega como
`{ id, evento, ocurrido_en, payload }` con `X-Webhook-Id` = `id` = el id del evento del outbox. El
payload lleva solo los campos del contrato: `aviso.entregado` (aviso_id, idempotency_key, empresa_id,
canal, entregado_en, leido_en o null), `aviso.fallido` (aviso_id, idempotency_key, empresa_id, canal
o null, motivo, detalle o null) y `contacto.baja` (cliente_id, empresa_id, canal, ocurrido_en, origen).

## Plantillas de WhatsApp (servicio de mensajería, `contratos/plantillas-whatsapp.md`)

Todos llevan `plantilla_id`, `empresa_id` (la dueña de la cuenta; null sin conector) y `waba_id`, salvo
`plantillas_whatsapp.ajustes_cambiados`. `origen` del cambio: `iris` (una persona), `webhook` (Meta en
vivo) o `sincronizacion` (botón o noche). Nunca el token ni el texto de un mensaje a un cliente.

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `plantilla_whatsapp.creada` | + `nombre`, `idioma`, `categoria`, `origen` (`iris`, `sugerida`, `meta`, `migrada`, `canal`) | Borrador nuevo, una sugerida, una que trajo la sincronización (creada en Meta) o una que reflejó el formulario del número. |
| `plantilla_whatsapp.actualizada` | + `campos[]` (`definicion`, `nombre`, `usos`, `codigos`, `variables`…) | Borrador editado, usos o códigos cambiados (también la que soltó un código). |
| `plantilla_whatsapp.enviada` | + `meta_id`, `tipo` (`creacion`, `edicion`), `estado` | Meta aceptó la plantilla (o su edición) para revisar. |
| `plantilla_whatsapp.estado_cambiado` | + `de`, `a`, `motivo` (o null: reason de Meta, `editada`, `reanudada`), `origen`, `pausada_hasta` (o null) | Cambio de estado en Meta (aprobada, rechazada, pausada…) o en Iris (editar, reanudar). |
| `plantilla_whatsapp.categoria_cambiada` | + `de`, `a`, `origen`, `pendiente` (true = Meta avisó que la reclasificará), `en_uso` | Meta la reclasificó (o avisó que lo hará); el panel lo avisa. |
| `plantilla_whatsapp.calidad_cambiada` | + `de`, `a` (`GREEN`, `YELLOW`, `RED`, `UNKNOWN`), `origen` | Cambio de calidad. |
| `plantilla_whatsapp.borrada` | + `nombre`, `idioma`, `origen` (`iris`, `meta`, `sistema`), `estaba_en_uso`, `codigos[]`, `nombre_libre_en` (o null); `motivo` (`cuenta_de_otra_empresa`) y `usos[]` solo con `sistema` (con `codigos[]`: lo que tenía, para deshacerlo) | Borrada desde Iris, Meta ya no la tiene o el sistema la archivó porque la cuenta pasó a otra empresa. La fila nunca se elimina. |
| `plantilla_whatsapp.sincronizada` | `waba_id`, `empresa_id`, `origen` (`manual`, `nocturna`, `webhook`), `total`, `nuevas`, `cambiadas`, `borradas` | Terminó una lectura completa de la cuenta. |
| `plantillas_whatsapp.ajustes_cambiados` | `empresa_id`, `campos[]`, `marketing_permitido` | Ajustes de la empresa. |

Nadie los consume todavía (el panel lee por la API); quedan para la campana de avisos.

## Chat interno del equipo (I1, servicio de mensajería)

Contrato de la API en `contratos/chat.md`. Ninguno lleva el texto de un mensaje: quien lo necesite
(el emisor de push, fase siguiente) lo lee por `mensaje_id`. No salen hacia Argos ni Horus.

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `chat.conversacion_creada` | `conversacion_id`, `empresa_id` (o null), `tipo` (`directa` \| `cuadrilla` \| `orden`), `cuadrilla_id` (o null), `orden_id` (o null), `creada_por`, `miembros[]` (los explícitos: los dos de la directa, o despacho que abrió el hilo; los derivados no van) | Primera vez que se abre (`POST /chat/directas`, `/chat/cuadrillas/{id}/abrir`, `/chat/ordenes/{id}/abrir`). Abrirla de nuevo no emite. |
| `chat.mensaje_enviado` | `conversacion_id`, `mensaje_id` (uuid v7 del cliente), `empresa_id`, `tipo`, `cuadrilla_id`, `orden_id`, `autor_id`, `seq`, `contenido` `{ texto, foto, ubicacion }` (booleanos), `enviado_en`, `destinatarios[]` (usuarios activos de la empresa que la ven, sin el autor: miembros, técnicos y supervisión por alcance; en una directa, el otro) | `POST /chat/conversaciones/{id}/mensajes`. Un reenvío del mismo id no emite. Para el emisor de push. |
| `chat.mensaje_eliminado` | `conversacion_id`, `mensaje_id`, `empresa_id`, `autor_id`, `eliminado_en` | El autor lo eliminó (15 min). Para retirar un push ya enviado. |
| `chat.mencion` | `conversacion_id`, `mensaje_id`, `empresa_id` (o null), `usuario_id` (el mencionado), `autor_id` — el mismo payload que `conversacion.mencion` | Uno por mencionado al enviar un mensaje con `menciones[]` (`contratos/chat.md` §3.2); nunca para el autor. Un reenvío del mismo id no emite. Lo consume el emisor de push (tipo `mencion`). |
| `chat.miembro_agregado` | `conversacion_id`, `empresa_id` (o null), `orden_id`, `usuario_id` (el agregado), `agregado_por` | La supervisión agregó a alguien al hilo de una orden (`POST /chat/conversaciones/{id}/miembros`, `chat.supervisar`). Si ya lo veía, no emite. |

Sin `chat.mensaje_restaurado`: el «Deshacer» del panel retrasa la eliminación en el cliente
(`contratos/chat.md` §3.1); el servidor no restaura mensajes.

**Sin `chat.leido`.** Las lecturas son muy frecuentes, no cambian nada de negocio y nadie fuera del
chat las consume: el «visto» y los contadores viajan por el feed propio del chat
(`mensajeria.chat_eventos`, SSE y `GET /chat/cambios`) y el emisor de push calcula el contador del
badge al enviar. Llenar el outbox (y el relay) con ellas no aporta. Si un consumidor lo necesita, se
agrega.

**Por qué el tiempo real no usa el outbox.** El relay corre en `cmd/worker` con River (latencia de
un job, y el SSE vive en `cmd/mensajeria`), sus ids no dan un cursor por usuario y no se guardan
para reanudar. El feed del chat es una tabla propia con id creciente en orden de COMMIT por empresa
y `NOTIFY iris_chat` en la misma transacción.

## Notificaciones push (I5, `internal/notificaciones`)

Contrato con la app en `contratos/push.md`. Nunca llevan el token FCM completo (solo `token_fin`, sus
últimos 6 caracteres) ni textos de mensajes. No salen hacia Argos ni Horus.

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `dispositivo.registrado` | `dispositivo_id`, `usuario_id`, `empresa_id` (o null sin conector), `plataforma` (`android`), `app_version`, `token_fin`, `nuevo` (bool) | `POST /dispositivos` (`dispositivos.registrar`); también al renovar uno que ya estaba (`nuevo: false`). |
| `dispositivo.quitado` | `dispositivo_id`, `usuario_id`, `empresa_id`, `token_fin`, `motivo` (`usuario` \| `sesion_cerrada` \| `token_invalido` \| `otro_usuario` \| `sin_acceso`) | `POST /dispositivos/{id}/quitar`; al cerrar sesión (`DELETE /sesion`, en su transacción); en la purga de cada hora o al enviar si la sesión o el token ya no valen (`sesion_cerrada`) o el usuario salió de la empresa (`sin_acceso`); FCM respondió `UNREGISTERED`, o `INVALID_ARGUMENT` / `SENDER_ID_MISMATCH` sobre `message.token` (`token_invalido`); el token pasó a otro usuario (`otro_usuario`); el usuario pasó de 10 dispositivos en la empresa (el más viejo, `usuario`). |
| `notificaciones.preferencias_cambiadas` | `usuario_id`, `campos[]`, `version` | `POST /yo/notificaciones` (`notificaciones.guardar_preferencias`). `empresa_id` null (son de la cuenta). |

**Avisos del panel y correo del sistema (2.1, `contratos/conversaciones.md` §16.9).** No salen hacia
Argos ni Horus; nunca llevan textos de clientes.

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `notificaciones.avisos_creados` | `tipo` (`asistente_tope`), `clave` (`asistente_tope:<empresa>:<YYYY-MM>:<80\|100>:<tope>`), `empresa_id` (o null), `usuarios[]` (los avisados esta vez), `correo` (`enviar` \| `omitido`: sin correo del sistema) | El worker consumió `asistente.tope_alcanzado` y creó al menos un aviso (`avisos.asistente_tope`, actor sistema). Un evento repetido no crea avisos ni emite; con otro valor del tope en el mismo mes sí (clave nueva). |
| `correo_sistema.ajustes_cambiados` | `empresa_id` (o null = el ajuste global), `campos[]` (los que cambiaron, en snake_case; puede traer `contrasena`, nunca su valor), `activo` | Guardar, activar o desactivar el correo del sistema de un ámbito (`contratos/correo-sistema.md`). Sin consumidores hoy: el worker lee el ajuste vigente en cada envío. |
| `correo_sistema.probado` | `empresa_id` (o null), `estado` (`ok` \| `error`), `causa` (o null; lista cerrada de `correo-sistema.md` §3.3), `usuario_id` | Cada «probar» del correo del sistema. Nunca el host, el destinatario ni credenciales. |
| `notificaciones.correo_enviado` / `notificaciones.correo_fallido` | `correo_id`, `usuario_id`, `tipo`, `clave`, `motivo` (o null; en fallido, el resumen del error SMTP sin credenciales) | Job `notificaciones.enviar_correo`: el SMTP del sistema aceptó el correo, o falló en el último intento (6). Los omitidos (`sin_correo_sistema`, `sin_correo`) no emiten: quedan en `notificaciones.correos`. |

**Consume** `asistente.tope_alcanzado` (avisos del panel) y `chat.mensaje_enviado`, `chat.mencion`, `conversacion.mencion`,
`conversacion.aviso_sin_atender`, `orden.asignada` y `orden.programada` (push.md §3). `chat.mencion`
lo emite el chat al enviar un mensaje con menciones, con el payload de `conversacion.mencion`
(`conversacion_id`, `mensaje_id`, `empresa_id`, `usuario_id`, `autor_id`); el emisor lo une al push
del mismo mensaje. Los envíos no emiten eventos (son muchos y no cambian nada de negocio): su
estado queda en `notificaciones.envios`.

## Conversaciones con clientes (I1, servicio de mensajería)

Contrato de la API en `contratos/conversaciones.md`. Ninguno lleva el texto de un mensaje ni el
teléfono del contacto (se leen por `conversacion_id` / `mensaje_id`). No salen hacia Argos ni Horus.
El tiempo real del panel va por el feed propio (`mensajeria.conv_eventos`, SSE y
`GET /conversaciones/cambios`), no por el outbox.

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `conversacion.creada` | `conversacion_id`, `empresa_id` (o null), `canal_id`, `cola_id` (o null), `cliente_id` (o null), `asociacion` (`automatica` o null) | Primer mensaje de un número a un número de la empresa. |
| `conversacion.mensaje_recibido` | `conversacion_id`, `mensaje_id`, `empresa_id`, `canal_id`, `tipo`, `cliente_id`, `asignado_a`, `cola_id` | Cada mensaje nuevo del cliente (el mismo wamid dos veces no emite). |
| `conversacion.mensaje_enviado` | `conversacion_id`, `mensaje_id`, `empresa_id`, `autor_id`, `tipo`, `plantilla` (o null), `respuesta_rapida_id` (o null) | Un agente respondió (queda en cola hacia WhatsApp). Un reenvío del mismo id no emite. |
| `conversacion.mensaje_fallido` | `conversacion_id`, `mensaje_id`, `empresa_id`, `motivo` (`numero_invalido`, `rechazado_por_canal`, `fuera_de_ventana`, `agotado`, `token_invalido`, `sin_permiso_meta`, `cliente_sin_confirmar`), `autor_id` | WhatsApp no lo entregó, o Iris no lo mandó (`cliente_sin_confirmar`: la plantilla lleva datos del cliente y el cliente no está confirmado). Nunca el código ni el texto de Meta. |
| `conversacion.nota_agregada` | `conversacion_id`, `mensaje_id`, `empresa_id`, `autor_id`, `menciones[]` | Nota interna. |
| `conversacion.mencion` | `conversacion_id`, `mensaje_id`, `empresa_id`, `usuario_id`, `autor_id` | Uno por mencionado (para el futuro push). |
| `conversacion.asignada` | `conversacion_id`, `empresa_id`, `usuario_id` (o null), `anterior_id` (o null), `origen` (`tomar`, `asignar`, `responder`), `actor_id` | Tomar, asignar o responder una sin asignar. |
| `conversacion.transferida` | `conversacion_id`, `empresa_id`, `de_cola_id`, `a_cola_id`, `asignado_a`, `actor_id` | Pasar a otra cola (el resumen queda como nota). |
| `conversacion.etiquetada` | `conversacion_id`, `empresa_id`, `etiquetas[]`, `anteriores[]`, `actor_id` | Fijar etiquetas. |
| `conversacion.resuelta` | `conversacion_id`, `empresa_id`, `actor_id`, `asignado_a` | Resolver. |
| `conversacion.reabierta` | `conversacion_id`, `empresa_id`, `origen` (`cliente`, `agente`), `asignado_a`, `actor_id` (agente) | El cliente escribió a una resuelta, o reabrir a mano. |
| `conversacion.cliente_asociado` | `conversacion_id`, `empresa_id`, `cliente_id` (o null al desasociar), `servicio_id`, `asociacion`, `actor_id`, `origen` (opcional: `referencia` cuando el número estaba en varios clientes y el número de servicio coincidió, B22; `contacto` cuando Iris leyó los contactos de Argos al abrir la conversación y el número quedó de una sola persona, con `actor_id` null) | Asociar o desasociar (la automática al llegar el mensaje va en `conversacion.creada`). |
| `conversacion.vinculo_creado` | `conversacion_id`, `empresa_id`, `tipo` (`ticket`, `orden`, `prospecto`, `boleta`, `promesa`), `entidad_id`, `referencia`, `actor_id` | Algo creado desde la conversación (además del evento del módulo dueño: `ticket.creado`, `orden.creada`…). |
| `conversacion.sla_vencido` | `conversacion_id`, `empresa_id`, `cola_id`, `asignado_a`, `vence_en`, `espera_desde` | Una vez por espera (mantenimiento). |
| `conversacion.cliente_confirmado` | `conversacion_id`, `empresa_id`, `cliente_id`, `actor_id`, `contacto_argos_id` (o null) | Un supervisor confirma una asociación manual cuyo número no coincide con el cliente. Si el número es de un contacto del cliente (§3.5), confirma a ese contacto (`contacto_argos_id`), no al titular. |
| `contacto_iris.consentimiento` | `contacto_iris_id`, `empresa_id`, `estado` (`acepta`, `baja`), `origen` (`agente`) | Permiso de contacto por número de un contacto de Iris. |
| `contacto_iris.creado` / `contacto_iris.unido` | `contacto_iris_id`, `empresa_id`; `tipo`, `conversacion_id` (creado); `cliente_id`, `conversaciones[]` (unido) | Contacto propio de Iris; unido a su ficha de Argos. |
| `cola.creada` / `cola.actualizada` | `cola_id`, `empresa_id`; `nombre` (creada); `campos[]`, `activa` (actualizada) | Configuración. |
| `etiqueta.creada` / `etiqueta.actualizada` | `etiqueta_id`, `empresa_id`; `nombre` (creada); `campos[]`, `activa` (actualizada) | Configuración. |
| `respuesta_rapida.creada` / `respuesta_rapida.actualizada` | `respuesta_id`, `empresa_id`; `atajo` (creada); `campos[]`, `activa` (actualizada) | Configuración. |
| `boleta.registrada` | `boleta_id`, `conversacion_id`, `empresa_id`, `cliente_id`, `servicio_id` | El agente la mandó (queda `por_enviar`). |
| `boleta.registrada_en_argos` / `boleta.fallida` | `boleta_id`, `conversacion_id`, `empresa_id`; `comprobante_id` / `detalle` | Argos la aceptó (comprobante pendiente de caja) o la rechazó por negocio (o se agotaron los reintentos). |
| `boleta.aprobada` | `boleta_id`, `conversacion_id`, `empresa_id`, `comprobante_id`, `pago_id` (o null si Argos no lo informó), `actor_id` (usuario de Iris que decidió; null si decidió caja en Argos o Argos no dice quién), `origen` (`iris` si hay actor de Iris, `argos` si no), `aviso_cliente` (`enviado` \| `sin_aviso`) | La boleta pasó de `registrada` a `aprobada` (contratos/conversaciones.md §6.4.1): por la respuesta de Argos a `boletas.aprobar` (el actor es la persona de la sesión), o al confirmar con `GET /comprobantes/:id` el webhook `comprobante.aprobado` o una relectura (el actor es el `verificado_iris_actor` del comprobante, contrato 0.7.1, si es un usuario de esta instalación). **Una sola vez por boleta**, con el aviso al cliente. |
| `boleta.rechazada` | lo mismo, con `pago_id: null` y `causa` (`no_aparece` \| `monto_distinto` \| `ilegible` \| `falta_dato` \| `duplicado` \| `otro`) | Igual, para `boletas.rechazar` y `comprobante.rechazado`. El motivo (texto libre) no va en el evento: está en la boleta. |
| `boleta.corregida` | `boleta_id`, `conversacion_id`, `empresa_id`, `comprobante_id`, `de`, `a` (`aprobada` \| `rechazada`), `pago_id` (o null), `causa` (o null), `actor_id` (o null), `origen` | Argos dice otro estado que el que Iris tenía guardado (al confirmar un webhook o en una relectura): gana Argos. Mensaje de sistema `boleta_corregida`; **sin** segundo aviso al cliente. |
| `boleta.confirmacion_agotada` | `boleta_id`, `conversacion_id`, `empresa_id`, `comprobante_id`, `intentos`, `detalle` (por qué no se confirmó) | El webhook de Argos avisó una resolución e Iris no pudo confirmarla con `GET /comprobantes/:id` después de 6 intentos (Argos no responde, lo sigue teniendo pendiente o el comprobante no coincide en canal o empresa). La boleta no cambia de estado y queda `confirmacionArgos: agotada`; mensaje de sistema `boleta_sin_confirmar`. |
| `promesa.solicitada` | `promesa_id`, `conversacion_id`, `empresa_id`, `cliente_id`, `servicio_id`, `estado` (`activa`, `rechazada`, `pendiente_aprobacion`), `promesa_argos_id` | Iris pidió la promesa y Argos decidió. |

| `horario.creado` / `horario.actualizado` | `horario_id`, `empresa_id`; `nombre` (creado); `campos[]`, `activo` (actualizado) | Configuración (PR B). |
| `conversacion.fuera_de_horario` | `conversacion_id`, `empresa_id`, `mensaje_id` (la respuesta automática), `hasta` (próxima apertura) | Una vez por ventana cerrada (PR B). |
| `conversacion.guardia_avisada` | `conversacion_id`, `empresa_id`, `usuario_id`, `motivo` (`etiqueta`, `falla_masiva`) | Una vez por conversación y ventana cerrada; para el futuro push (PR B). |
| `manual.creado` / `manual.actualizado` | `manual_id`, `empresa_id`; `visibilidad` (creado); `campos[]`, `activo` (actualizado) | Biblioteca (PR B). |
| `manual.enviado` | `manual_id`, `conversacion_id`, `empresa_id`, `mensaje_id`, `modo` (`enlace`, `texto`) | Se mandó al cliente (PR B). |
| `manual.enlace_revocado` | `enlace_id`, `manual_id`, `conversacion_id`, `empresa_id`, `actor_id` | Un supervisor revocó el enlace público (PR B). |

`canal_mensajeria.actualizado` (ya existente) también sale al configurar uso, cola o número de avisos
(`campos[]` con `usos`, `avisos_empresa`, `cola_id`; desde 1.1 también `bot`), y para el número que
deja de ser el de avisos.

### Bandeja 1.1 (contratos/conversaciones.md §14)

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `conversacion.iniciada` | `conversacion_id`, `empresa_id`, `canal_id`, `cola_id` (o null), `mensaje_id`, `plantilla`, `autor_id`, `nueva` (bool), `cliente_id` (o null), `contacto_iris_id` (o null) | Un agente escribió primero con una plantilla (`POST /conversaciones/iniciar`). Un reenvío del mismo id no emite. Sale con `conversacion.mensaje_enviado` y, si cambió quién la tiene, `conversacion.asignada` (`origen: iniciar`). |
| `conversacion.aviso_sin_atender` | `conversacion_id`, `empresa_id`, `cola_id` (o null), `usuarios[]` (los avisados), `bot_contesta_en` (o null) | Una conversación sin asignar espera con agentes disponibles en la cola (una vez por espera; también al elegir una opción del menú, con `bot_contesta_en` null). Para un futuro push. |
| `conversacion.bot_atendio` | `conversacion_id`, `empresa_id`, `cola_id` (o null), `motivo` (`sin_confirmacion`, `sin_disponibles`, `fuera_de_horario`), `mensaje_id` (o null), `menu` (bool: quedó esperando una opción), `respondio` (bool, en los temporizadores: el bot le mandó algo al cliente en esa espera) | Le tocó al bot: nadie la tomó a tiempo, no había disponibles, o fuera de horario con el bot activo. Una vez por espera (el mantenimiento revisa con la fila bloqueada). Con `respondio: false` no pudo escribirle (BAJA o ventana de 24 h cerrada): el hilo dice `bot_sin_respuesta`, no `bot_atendio`. |
| `conversacion.menu_elegido` | `conversacion_id`, `empresa_id`, `opcion` (1–9), `texto`, `de_cola_id` (o null), `a_cola_id` (o null), `etiquetas[]` (las que se pusieron) | El cliente contestó con una opción del menú del bot. |
| `agente.disponibilidad_cambiada` | `usuario_id`, `empresa_id` (o null), `estado` (`disponible`, `no_disponible`), `origen` (`manual`, `cierre_sesion`, `inactividad`, `horario`), `anterior`, `desde` | Cambio manual (auditado) o automático del mantenimiento (sin fila de auditoría). Los latidos no emiten. |
| `conversaciones.ajustes_cambiados` | `empresa_id` (o null), `campos[]`, `minutos_inactividad` | `POST /conversaciones/ajustes`. |

Cambios a eventos existentes (aditivos): `conversacion.asignada` suma los orígenes `pasar` (con
`disponible` del destino y `nota_id` o null) e `iniciar`; `conversacion.creada` suma `origen`
(`cliente` si la abrió un mensaje del cliente, `agente` al iniciar);
`cola.actualizada` puede traer `campos: ["supervisores"]`.

### Conexión con Meta, QR y préstamo de números (contratos/conversaciones.md §15)

Ninguno lleva secretos (app secret, clave de enlace, verify token, token del negocio) ni el `code` de
Meta.

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `meta.conexion_cambiada` | `modo` (`propia`, `compartida`), `anterior` (`sin_configurar`, `propia`, `compartida`), `campos[]` (los secretos cambiados van con su nombre, nunca el valor) | `POST /mensajeria/conexion-meta/actualizar` (una por instalación; sin `empresa_id`). |
| `meta.conexion_probada` | `modo`, `estado` (`lista`, `error`, `sin_servicio_central`) | `POST /mensajeria/conexion-meta/probar`. |
| `canal_mensajeria.desconectado` | `canal_id`, `empresa_id`, `motivo` (`partner_removed`, `account_offboarded` o la razón de Meta en minúsculas: `primary_inactivity`, `companion_inactivity`, `business_downgrade`…) | Webhook `account_update` de un número QR. Una vez por desconexión. |
| `canal_mensajeria.reconectado` | `canal_id`, `empresa_id`, `origen` (`qr`: se repitió el QR; `meta`: `ACCOUNT_RECONNECTED`) | El número vuelve a recibir y enviar. |
| `canal_mensajeria.prestado` | `canal_id`, `empresa_id` (la dueña), `prestatario_id`, `usos[]`, `cola_id` (o null), `nuevo` (bool: alta o reactivación) | `POST /mensajeria/canales/{id}/prestamos`. |
| `canal_mensajeria.prestamo_revocado` | `canal_id`, `empresa_id` (la dueña), `prestatario_id`, `motivo`, `conversaciones_cerradas`, `avisos_cancelados` | `POST /mensajeria/canales/{id}/prestamos/{empresa}/revocar`. Sale con un `conversacion.resuelta` (`actor_id` null, `motivo: prestamo_revocado`) por conversación y un `aviso.fallido` por aviso. |
| `conversacion.empresa_asignada` | `conversacion_id`, `empresa_id` (la elegida), `canal_id`, `motivo` (`iniciada_por_empresa`, `cliente_de_empresa`, `sin_coincidencia`, `varias_coincidencias`, `pasada`), `anterior_empresa_id` (o null), `actor_id` (solo `pasada`) | Un mensaje que llega a un número compartido sin conversación abierta, o la dueña pasa una conversación (`POST /conversaciones/{id}/pasar-empresa`). |

Cambios a eventos existentes (aditivos): `canal_mensajeria.creado` suma `tipo_conexion` (`qr` en
`POST /canales/qr`); `canal_mensajeria.actualizado` sale con `campos: ["sincronizacion"]` al pedir
contactos o historial y con la `empresa_id` de quien usa el número al marcarlo como su número de
avisos; `conversacion.creada` suma `origen: telefono` (un eco del teléfono abrió la conversación);
`conversacion.mensaje_enviado` suma `origen` (`telefono` con `autor_id: null` para lo mandado desde
el teléfono); `contacto_iris.creado` suma `origen` (`agente` | `telefono`, con `conversacion_id`
null para los contactos del teléfono); `conversacion.resuelta` suma `motivo: prestamo_revocado`
(con `actor_id` null). El historial traído del teléfono no emite eventos de conversación.

### Asistente de IA que sugiere respuestas (contratos/conversaciones.md §16)

Ninguno lleva el texto de una sugerencia, de un mensaje ni de un resumen, ni la clave del proveedor.
`empresa_id` en el sobre = la de la conversación (o la de los ajustes).

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `asistente.ajustes_cambiados` | `empresa_id` (o null), `campos[]` (`activo`, `proveedor`, `url`, `modelo`, `modo`, `fuentes`, `tope_tokens_mes`, `precio_entrada_millon`, `precio_salida_millon`, `clave_guardada`; la clave solo por su nombre) | `POST /mensajeria/asistente/ajustes/actualizar` (`mensajeria.asistente_configurar`). |
| `asistente.sugerencia_creada` | `sugerencia_id`, `conversacion_id`, `empresa_id`, `origen` (`automatico`, `boton`, `otra`), `proveedor` (o null), `modelo` (o null), `tokens_entrada`, `tokens_salida`, `estado` (`lista`, `error`, `vencida`, `descartada`), `error` (código o null: `proveedor_no_responde`, `proveedor_error`, `clave_invalida`, `tope_alcanzado`, `sin_configurar`, `rechazo_modelo`, `respuesta_invalida`, `url_no_permitida`, `proveedor_redirige`) | Al terminar de generar (job, actor sistema) o al vencer una atascada (mantenimiento, `proveedor_no_responde`). `vencida`/`descartada`: llegó otro mensaje del cliente o la descartaron mientras se generaba (sus tokens cuentan). Una sugerencia cuya conversación pasó a otra empresa queda `vencida` sin este evento ni cobro. |
| `asistente.sugerencia_usada` | `sugerencia_id`, `conversacion_id`, `empresa_id`, `editada` (`null` al pulsar «Usar»; `true`/`false` al enviar el mensaje con `sugerenciaId`) | `POST /sugerencias/{id}/usar` y `POST /conversaciones/{id}/mensajes` con `sugerenciaId`. El mensaje lo envía la persona. |
| `asistente.sugerencia_calificada` | `sugerencia_id`, `conversacion_id`, `empresa_id`, `util` (bool) | `POST /sugerencias/{id}/calificar` (solo si cambia). |
| `asistente.tope_alcanzado` | `empresa_id`, `porcentaje` (`80` \| `100`), `mes` (`YYYY-MM`, Guatemala), `tokens` (entrada + salida del mes), `tope` | Al sumar el uso de una sugerencia o un resumen que cruza el 80 % o el 100 % del tope; una vez por mes y tope (subir el tope recalcula). Con 100 % el asistente queda en pausa hasta el mes siguiente o hasta subir el tope. Lo consume `internal/notificaciones` (2.1): aviso en la campana y correo a quienes tienen `mensajeria.asistente_configurar` (§16.9), una vez por umbral, mes, valor del tope y usuario. |

### Asistente autónomo y categorías (contratos/conversaciones.md §17, fase 3a)

Ninguno lleva texto de mensajes, de respuestas del modelo ni montos. En 3a el asistente todavía no
actúa solo; desde 3b sí (§17.8): `asistente.accion_ejecutada` y `asistente.paso_a_persona`. `conversacion.mensaje_enviado` de un mensaje del asistente lleva `autor_id: null` y `automatico: "asistente"`.

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `conversacion.categorizada` | `conversacion_id`, `empresa_id`, `categoria_id` (o null al quitarla), `anterior_id` (o null), `origen` (`asistente` \| `agente`), `confianza` (0 a 1, o null), `actor_id` (o null si fue el asistente) | El asistente clasificó el último mensaje del cliente (en la misma llamada de la sugerencia o en una aparte; actor sistema) o un agente la corrigió con `POST /conversaciones/{id}/categorizar`. Solo si la categoría cambia. |
| `categoria.creada` | `categoria_id`, `empresa_id`, `nombre` | `POST /mensajeria/asistente/categorias` (`mensajeria.asistente_configurar`). Las seis iniciales se siembran sin evento. |
| `categoria.actualizada` | `categoria_id`, `empresa_id`, `campos[]` (`nombre`, `descripcion`, `tono`, `cola_id`, `sla_primera_respuesta_min`, `modo`, `condicion`, `manual_ids`, `activa`), `activa` | Editar, activar o desactivar una categoría. |
| `asistente.mensaje_probado` | `empresa_id`, `haria` (`responde_solo` \| `sugiere` \| `pasa_a_persona`), `categoria_id` (o null), `tokens_entrada`, `tokens_salida` | `POST /mensajeria/asistente/probar-mensaje` cuando el modelo respondió algo válido (no ejecuta nada; cuenta para el tope). |
| `asistente.accion_ejecutada` | `conversacion_id`, `empresa_id`, `accion` (`responder`, `responder_dudas`, `avisar_fallas`, `dar_saldo`, `crear_ticket`, `simular_promesa`, `promesa_pago`, `cuentas_para_boleta`, `recibir_boleta`, `reconexion_temporal`, `pasar_a_persona`; rechazos del servidor: `herramienta_no_permitida`, `limite_herramientas`), `resultado` (`ok` \| `rechazada` \| `fallo`), `entidad` (o null: `mensaje`, `ticket`, `promesa`, `boleta`, `comprobante`), `entidad_id` (o null), `turno` | 3b (§17.8): cada herramienta que el asistente pidió en un turno autónomo y el mensaje que envió (actor `asistente_ia`, canal `ia`). Una vez por turno y paso (un reintento no lo repite). Nunca argumentos, texto ni montos. |
| `conversacion.identidad_verificada` | `conversacion_id`, `empresa_id`, `cliente_id`, `resultado` (`coincide` \| `no_coincide` \| `no_disponible` \| `error`), `origen` (`asistente`) | 3b (§17.8.2): un intento de verificación de identidad contra Argos (§7.5). Nunca el dato. |
| `reconexion.solicitada` | `reconexion_id`, `conversacion_id`, `empresa_id`, `cliente_id`, `servicio_id`, `comprobante_id`, `motivo` (`fuera_de_horario` \| `sin_personal`; lo decide el servidor: fuera del horario hábil, o en horario hábil con el interruptor encendido, Argos con contrato ≥ 0.8.0 y nadie con el rol `caja` disponible; contrato `argosisp.md` 0.8.0 §7.3), `estado` (`aceptada` \| `rechazada`), `promesa_argos_id` (o null) | 3b (§17.8.2): se pidió a Argos la reconexión temporal mientras caja revisa la boleta (`conversaciones.pedir_reconexion`). La auditoría del intento lleva el criterio (`fuera_de_horario`, `sin_personal_encendido`, `argos_080`, `caja_total`, `caja_disponibles`), nunca quiénes. |
| `reconexion.actualizada` | `reconexion_id`, `conversacion_id`, `empresa_id`, `comprobante_id`, `promesa_argos_id`, `estado` (`activa` \| `cumplida` \| `incumplida` \| `cancelada`), `motivo` (`fuera_de_horario` \| `sin_personal`: el de Argos si lo mandó), `fin` (`comprobante_rechazado` \| `vencida_sin_revisar` \| `otro`, o null), `hasta` (o null), `entrante_id` | B4 (0.8.0 §7.3): Argos contó qué pasó con la reconexión (webhooks `promesa.*`, por `argos.promesa_cambiada`). Una vez por entrante; un estado final no lo pisa uno atrasado. |
| `reconexion.cliente_avisado` | `reconexion_id`, `conversacion_id`, `empresa_id`, `estado` (`cumplida` \| `incumplida` \| `cancelada`), `fin` (o null), `aviso_cliente` (`enviado` \| `sin_aviso`), `mensaje_id` (o null). Nunca el texto | B4 (§7.3 «Cómo termina»): el proceso de mensajería le dijo (o decidió no decirle) al cliente cómo terminó su reconexión temporal. Una vez por final; solo dentro de la ventana de 24 h. |
| `reconexion.ajustes_cambiados` | `empresa_id` (o null), `campos[]` (`sin_personal`), `sin_personal` | B4: un admin encendió o apagó la reconexión sin personal de caja (`mensajeria.ajustes_reconexion`). |
| `asistente.paso_a_persona` | `conversacion_id`, `empresa_id`, `motivo` (`cliente_lo_pidio` \| `enojo` \| `sin_respuesta` \| `herramienta_fallo` \| `max_turnos` \| `persona_presente` \| `tope`), `cola_id` (o null), `turnos` | 3b: el asistente dejó la conversación a una persona. Con `persona_presente` una persona la tomó o escribió a mitad del turno (no se envió nada); en los demás queda `espera_persona`, en la cola de su categoría, con una nota interna. |
| `conversacion.resumen_guardado` | `conversacion_id`, `empresa_id`, `resumen_id`, `usuario_id` (quién lo pidió), `proveedor`, `modelo`, `hasta_mensaje_id`, `tokens_entrada`, `tokens_salida` | §16.4: se generó (o regeneró) el resumen del asistente y quedó guardado en la conversación. **Nunca el texto.** |
| `conversacion.asistente_pausado` | `conversacion_id`, `empresa_id`, `actor_id`, `motivo` (`manual` \| `caso_delicado` \| `respuesta_incorrecta` \| `cliente_lo_pidio`), `atendia` (bool: el asistente la estaba atendiendo), `asignado_a` (o null) | §17.11: una persona pausó el asistente en esa conversación (`POST …/asistente/pausar`). No contesta solo hasta `conversacion.asistente_reanudado` o `conversacion.resuelta` (que quita la pausa sin otro evento). Una vez por pausa. |
| `conversacion.asistente_reanudado` | `conversacion_id`, `empresa_id`, `actor_id`, `de` (`pausado` \| `persona_presente` \| `espera_persona`), `motivo_paso` (o null: el `motivoPaso` que tenía, p. ej. `cliente_lo_pidio`), `pausa_motivo_anterior` (o null), `asignado_a` (o null) | §17.11: una persona le devolvió la conversación al asistente (`POST …/asistente/reanudar`). Solo si había algo que reanudar. |

`conversacion.transferida` suma `origen` cuando no la pasó una persona con «Transferir»: `asistente`
(la clasificó el asistente y su categoría tiene otra cola; `actor_id` null) o `categoria` (un agente
corrigió la categoría; `actor_id` = el agente). `asistente.ajustes_cambiados` suma en `campos[]`:
`modo_autonomo`, `espera_min`, `max_turnos`, `perfiles`, `por_cola`.

### Voz: llamadas, asistente en voz y centralita (contratos/voz.md)

**Contrato, aún sin código:** cada etapa (E1–E4 de `voz.md` §1) emite los suyos cuando se
implemente. Ninguno lleva texto de turnos, notas, documento, dígitos del teclado, SDP, audio,
claves ni el número del cliente. Todos llevan `empresa_id`, salvo los del servidor de voz
(`voz.motor_*`), que van con `empresa_id: null` porque el servidor es de la instalación (voz.md §8.1).

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `llamada.entrante` | `llamada_id`, `conversacion_id` (o null en centralita), `empresa_id`, `canal_id` (o null), `centralita_id` (o null), `cola_id` (o null), `cliente_id` (o null), `origen` (`whatsapp` \| `centralita`) | E1: llegó el webhook `connect` de Meta (o, en E4, el evento de la centralita) y la llamada quedó `sonando`. Una vez por `id_externo`. |
| `llamada.saliente_iniciada` | `llamada_id`, `conversacion_id`, `empresa_id`, `canal_id`, `usuario_id`, `devolucion_id` (o null) | E1: `llamadas.llamar` guardó la saliente en `marcando` (E1b), justo antes de mandar `connect` a Meta. Si Meta lo rechaza, sigue `llamada.fallida` (`error_meta`, etapa `marcando`). Nunca el número ni el SDP. |
| `llamada.contestada` | `llamada_id`, `conversacion_id`, `empresa_id`, `atendida_por` (`persona` \| `asistente`), `usuario_id` (o null), `espera_seg` | Meta aceptó el `accept` (o el cliente contestó una saliente): `en_curso`. |
| `llamada.terminada` | `llamada_id`, `conversacion_id`, `empresa_id`, `direccion`, `atendida_por` (quien la tenía al final), `usuario_id` (o null), `duracion_seg`, `motivo_fin`, `tramos` (número) | Una llamada contestada terminó. Si el `terminate` de Meta llega después con otra duración, no se emite otra vez. |
| `llamada.perdida` | `llamada_id`, `conversacion_id`, `empresa_id`, `direccion`, `cola_id` (o null), `sono_seg`, `devolucion_id` (o null), `usuarios[]` (a quién avisa el push: quien tiene la conversación y quienes supervisan la cola) | Entrante que nadie contestó o saliente sin contestar. |
| `llamada.rechazada` | `llamada_id`, `conversacion_id`, `empresa_id`, `motivo` (`rechazada_agente`, `fuera_de_horario`, `sin_capacidad`, `motor_no_disponible`, `limite`, `mal_uso`, `tope`), `usuario_id` (o null) | Se respondió `reject` a Meta. `fuera_de_horario` corresponde al estado del mismo nombre. |
| `llamada.fallida` | `llamada_id`, `conversacion_id`, `empresa_id`, `motivo` (`error_medio`, `error_meta`, `motor_no_disponible`, `transferencia_fallida`), `etapa` (`sonando`, `conectando`, `marcando`, `en_curso`) | Error técnico. Si el servidor de voz se cae, una por cada llamada que atendía el asistente. |
| `llamada.anotada` | `llamada_id`, `conversacion_id`, `empresa_id`, `usuario_id`, `resultado` (o null) | `llamadas.anotar`. La nota no va en el evento. |
| `llamada.permiso_solicitado` | `conversacion_id`, `empresa_id`, `canal_id`, `usuario_id` (o null si la mandó el asistente al pasar a persona), `mensaje_id` | E1b: se pidió al contacto permiso para llamarlo (`solicitar-permiso-llamada`). Nunca el número. |
| `llamada.permiso_cambiado` | `conversacion_id`, `empresa_id`, `canal_id`, `de`, `a` (`sin_pedir`, `pedido`, `temporal`, `permanente`, `negado`, `revocado`), `vence_en` (o null) | E1b: el contacto respondió (webhook `call_permission_reply`), se le pidió (`a: pedido`), Meta dice otra cosa al llamar (lectura de `call_permissions`) o lleva cuatro salientes seguidas sin contestar (`a: revocado`). `conversacion_id` es null cuando el cambio llega por el webhook (vale para el número en ese canal). Nunca el número. |
| `llamada.devolucion_pedida` | `devolucion_id`, `conversacion_id`, `empresa_id`, `llamada_id`, `pedida_por` (`sistema`, `asistente`, `cliente`) | Quedó una devolución pendiente (una viva por conversación). |
| `llamada.devolucion_cerrada` | `devolucion_id`, `conversacion_id`, `empresa_id`, `estado` (`hecha`, `descartada`, `vencida`), `usuario_id` (o null), `llamada_devuelta_id` (o null) | Se devolvió la llamada, se descartó o venció (72 h). |
| `llamada.grabacion_disponible` | `llamada_id`, `grabacion_id`, `empresa_id`, `origen` (`meta` \| `centralita`), `duracion_seg`, `vence_en` | La grabación quedó guardada en adjuntos. |
| `llamada.grabacion_borrada` | `llamada_id`, `grabacion_id`, `empresa_id`, `motivo` (`retencion` \| `manual`), `usuario_id` (o null) | Retención vencida o `llamadas.borrar_grabacion`. Borra también la transcripción. |
| `llamada.transcripcion_lista` | `llamada_id`, `transcripcion_id`, `empresa_id`, `origen` (`turnos` \| `grabacion`), `segmentos` (número) | E2: transcripción redactada guardada. |
| `llamada.tomada` | `llamada_id`, `conversacion_id`, `empresa_id`, `usuario_id`, `de` (`asistente`) | E3: una persona tomó en vivo la llamada del asistente. |
| `llamada.transferida` | `llamada_id`, `conversacion_id`, `empresa_id`, `usuario_id` (quien transfiere), `a_usuario_id` (o null), `a_cola_id` (o null) | E3: transferencia entre personas por el puente. |
| `llamada.escucha_iniciada` / `llamada.escucha_terminada` | `llamada_id`, `conversacion_id`, `empresa_id`, `usuario_id` (quien escucha); `duracion_seg` (terminada) | E3: `llamadas.escuchar_en_vivo` (solo recepción; voz.md §3.8.1). |
| `canal_mensajeria.llamadas_cambiadas` | `canal_id`, `empresa_id`, `campos[]`, `llamadas_activas` | `llamadas.configurar` sobre un número. |
| `voz.rafaga_levantada` | `empresa_id`, `canal_id`, `conversacion_id` (o null), `origen` (`manual`, `llamada`), `usuario_id` (o null), `llamada_id` (o null: la saliente que la levantó) | Se dejó que el número vuelva a llamar: desde ahora sus entrantes anteriores no cuentan para la ráfaga. Solo sale si había ráfaga: `manual` (`POST /conversaciones/{id}/llamadas/desbloquear`, `llamadas.desbloquear_numero`; sin ráfaga a la vista se mueve igual la marca y queda auditado, pero no sale el evento) o `llamada` (una saliente a ese número que Meta aceptó). Nunca el número. |
| `voz.ajustes_cambiados` | `empresa_id`, `campos[]` (Iris no guarda claves de STT/TTS: viven en el servidor de voz) | `voz.configurar`. |
| `voz.tope_alcanzado` | `empresa_id`, `tipo` (`asistente` \| `salientes`), `porcentaje` (80, 100), `mes`, `minutos`, `tope` | Una vez por umbral, mes y valor del tope. Al 100 % el asistente deja de contestar (suenan a personas). Con `tipo: salientes` (E1b) sale al terminar la saliente que cruza el umbral; `minutos` son los de salientes contestadas del mes (hora de Guatemala) y pasado el 100 % `llamar` responde 422 `tope_alcanzado`. |
| `voz.mal_uso_detectado` | `empresa_id`, `llamada_id`, `conversacion_id`, `tipo` (`inyeccion`, `extraccion_prompt`, `datos_de_terceros`, `suplantacion`, `abuso_coste`, `lenguaje`, `verificacion_fallida`, `herramienta_rechazada`), `regla`, `accion` (`aviso`, `paso_a_persona`, `corte`, `bloqueo`) | Uno por incidente (no por turno). Avisa al supervisor de la cola. |
| `voz.numero_bloqueado` / `voz.numero_desbloqueado` | `empresa_id`, `bloqueo_id`, `hasta`, `motivo`, `usuario_id` (o null = automático) | Bloqueo del asistente de voz para un número (el número no va en el evento). |
| `voz.motor_enlazado` | `motor_id`, `version`, `protocolo`, `usuario_id` | Intercambio de enlace completado (`conectar`, voz.md §4.8); falta activar. Nunca el código ni el token. |
| `voz.motor_activado` | `motor_id`, `usuario_id`, `version` | El admin comparó la huella y activó; el token ya es el aleatorio. |
| `voz.motor_desactivado` | `motor_id`, `usuario_id`, `motivo` (o null) | Desactivado en el panel: deja de usarse en el acto. |
| `voz.motor_empresas_cambiadas` | `motor_id`, `usuario_id`, `empresas` (ids o null = todas), `simultaneas_por_empresa` | Empresas permitidas o reparto cambiados. |
| `voz.motor_modelos_cambiados` | `motor_id`, `usuario_id`, `modelos` (ids o null = todos los del catálogo) | Modelos que las empresas pueden elegir. |
| `servidor_ia.umbral_superado` | `motor_id`, `tipo` (`vram`, `disco`, `temperatura`, `saturacion`), `valor`, `umbral` | Una vez por episodio (voz.md §4.12). `empresa_id: null`. |
| `servidor_ia.umbral_normalizado` | `motor_id`, `tipo`, `valor`, `umbral` | Cierre del episodio. |
| `servidor_ia.ajustes_cambiados` | `motor_id`, `campos[]` | Umbrales o retención del tablero. |
| `voz.motor_token_rotado` | `motor_id`, `origen` (`automatica`, `manual`, `enlace`) | Rotación del token de servicio. |
| `voz.motor_estado_cambiado` | `motor_id`, `de`, `a` (`listo`, `degradado`, `no_disponible`, `incompatible`, `desactivado`), `en_curso`, `version`, `compatible`, `llamadas_cortadas` | Cambio de estado del servidor de voz (salud o compatibilidad). |
| `centralita.creada` / `centralita.actualizada` | `centralita_id`, `empresa_id`; `tipo`, `modo` (creada); `campos[]`, `activa` (actualizada; el secreto como `secreto_guardado`) | E4: `centralita.configurar`. |
| `centralita.estado_cambiado` | `centralita_id`, `empresa_id`, `de`, `a` (`conectada`, `desconectada`, `error`, `desactivada`) | E4: la conexión AMI cambió. |
| `extension.asignada` / `extension.quitada` | `centralita_id`, `extension_id`, `empresa_id`, `usuario_id` | E4: `centralita.asignar_extension`. |
| `llamada.vinculada` | `llamada_id`, `empresa_id`, `ticket_id`, `cliente_id` (o null), `usuario_id`, `origen` (`centralita`) | E4a: la persona agregó la llamada de la centralita a un ticket (a elección; nunca automático). |
| `llamada.cliente_asociado` | `llamada_id`, `empresa_id`, `cliente_id`, `servicio_id` (o null), `usuario_id`, `origen` (`centralita`) | E4a: la persona eligió el cliente de la llamada (varios posibles o número desconocido). |

En la centralita (E4a) los `llamada.*` de siempre salen con `origen: centralita`, `centralita_id` y
`conversacion_id: null` (`llamada.entrante`, `llamada.contestada`, `llamada.terminada`, `llamada.perdida`
sin `usuarios`, `llamada.saliente_iniciada` desde el teléfono de escritorio a un cliente y
`llamada.anotada`). `centralita.estado_cambiado` sale solo en cambios reales (una caída de menos de 60 s
no cuenta).

Cambian eventos existentes: `asistente.accion_ejecutada`, `asistente.paso_a_persona` y
`conversacion.identidad_verificada` suman `canal` (`whatsapp` \| `voz`; ausente = `whatsapp`) y
`llamada_id`; `asistente.paso_a_persona.motivo` suma `no_se_entiende`, `mal_uso` y
`duracion_maxima`; `conversacion.mensaje_recibido` / `conversacion.mensaje_enviado` aceptan
`tipo: llamada`; `notificaciones.avisos_creados.tipo` suma `voz_tope`, `voz_motor_caido`,
`voz_mal_uso` y `llamada_perdida`.

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
| `soporte.posible_falla_general` | `ticket_id` (el de la posible falla), `empresa_id`, `tickets` (los de caídas de los últimos 10 min), `agrupados` | B29: la empresa pasó del umbral de tickets de caídas en 10 min; Iris dejó de abrirlos y los agrupó. Lo consume notificaciones (campana de quien tiene `tickets.configurar_caidas`). Uno por ticket. |
| `soporte.caidas_ajustes_cambiados` | `empresa_id`, `campos[]`, `caidas_umbral`, `caidas_ventana_min`, `falla_umbral`, `verificacion_horas` | B29/B30: cambiaron los umbrales de la empresa (`POST /tickets/ajustes-caidas`). |
| `soporte.aviso_falla` | `incidente` (id en `soporte.incidentes_red`), `empresa_id`, `cliente_id`, `tipo` (`apertura` \| `cierre`), `ticket_id` | B30: un aviso de falla para el cliente de un hijo del incidente (uno por cliente, incidente y tipo; máximo uno de cada tipo cada 6 h). Lo consume la mensajería: aviso de WhatsApp `falla_general` / `falla_general_resuelta` con las reglas de §6.5. Nunca servicio ni dirección. |
| `soporte.verificacion_vencida` | `ticket_id`, `numero`, `empresa_id`, `asignado_a` (o null) | B30: el cliente no respondió a la verificación tras cerrarse la falla general en las horas de la empresa; el ticket volvió a abierto. Lo consume notificaciones (campana del asignado). |

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
| `orden.creada` | `orden_id`, `numero`, `tipo` (`instalacion` \| `visita` \| `retiro` \| `cambio_equipo` \| `mantenimiento`), `empresa_id`, `ticket_id`, `prospecto_id`, `cliente_id`, `servicio_id`, `zona_id` (o null), `origen` (`panel` \| `api` \| `argos` \| `sistema` \| `asistente` (la agendó el asistente de IA, conversaciones.md §17.10)) | `ordenes.crear` (también desde un ticket con `ticketId`, y la orden de retiro automática con `origen = argos`). También `tickets.crear_orden` (`POST /tickets/{id}/crear-orden`), que en la misma transacción emite después `orden.asignada` y, con fecha, `orden.programada`. |
| `orden.asignada` | `orden_id`, `tecnico_id`, `cuadrilla_id`, `tecnico_anterior_id` (o null), `origen` (`asignar` \| `tomar` \| `programar` \| `reprogramar` \| `crear`), `motivo` (`tomada` o null) | `ordenes.asignar`, y `programar`/`reprogramar` cuando cambian el técnico. También `tickets.crear_orden` (con técnico, vía `ordenes.asignar`; solo con cuadrilla, `tecnico_id = null`, `origen = crear`), `ordenes.igualar_asignacion` con `desde = ticket` y `ordenes.tomar` (el técnico toma una orden libre: `origen = tomar`, `motivo = tomada`, con la misma lógica de asignar). |
| `orden.programada` | `orden_id`, `numero`, `tecnico_id`, `cuadrilla_id` (de la orden, o null), `tecnico_cuadrilla_id` (de la ficha del técnico, o null), `inicio`, `fin`, `ventana_inicio`/`ventana_fin` (ventana de llegada acordada con el cliente, o null), `reprogramada` (bool), `origen` (`programar` \| `reprogramar` \| `asignar`), `motivo` (o null) | Lo emite **agenda** (`agenda.ReservarCita`) en la transacción de `ordenes.programar`, `ordenes.reprogramar` y de `ordenes.asignar` cuando una orden programada cambia de técnico (`origen = asignar`, misma hora). |
| `orden.estado_cambiada` | `orden_id`, `numero`, `de`, `a`, `motivo` (o null), `tecnico_id` | Toda transición (programar, iniciar camino, llegar, cerrar —`motivo` = resultado—, fallar —`motivo` = `<motivo_falla>: <detalle>`—, pausar —`a = pausada`, `motivo` = `<motivo>` o `<motivo>: <nota>`—, reanudar —`de = pausada`, `motivo = reanudada`—, cancelar, reabrir, reprogramar y la vuelta a pendiente o programada al reasignar). |
| `orden.pausada` | `orden_id`, `numero`, `pausa_id`, `motivo` (`esperando_material` \| `cliente_ausente` \| `clima` \| `acceso` \| `otro`), `nota` (o null), `desde` (`en_camino` \| `en_sitio`), `inicio`, `tecnico_id` | `ordenes.pausar` (panel u `orden.pausar` del lote), junto con `orden.estado_cambiada`. |
| `orden.reanudada` | `orden_id`, `numero`, `pausa_id`, `a` (el estado de antes de la pausa), `duracion_seg`, `tiempo_pausado_seg` (total de la orden), `tecnico_id` | `ordenes.reanudar` (panel u `orden.reanudar` del lote). Cancelar o reprogramar una pausada cierra la pausa sin este evento (solo `orden.estado_cambiada`). |
| `orden.formulario_agregado` | `orden_id`, `formulario_id` (uuid), `plantilla_id`, `version_id`, `numero_version`, `origen` (`despacho` \| `tecnico`), `tecnico_id` | `ordenes.asignar_formulario` (despacho) u `orden.agregar_formulario` del lote (el técnico). El formulario del tipo (origen `tipo`) va con `orden.creada`, sin este evento. |
| `orden.formulario_quitado` | `orden_id`, `formulario_id`, `plantilla_id`, `origen`, `canal` | Quitar un formulario sin respuestas (despacho, o el técnico uno que agregó él). |
| `orden.datos_argos_aplicados` | `orden_id`, `numero`, `campos[]` (grupos pedidos: `direccion`, `coordenadas`, `referencia`, `contacto`), `cambiados[]` (los grupos que de verdad cambiaron), `campos_argos[]` (la marca que se atendió: `servicio.direccion`…), `campos_restantes[]` (lo de la marca que no se aplicó y sigue avisado) | I1 (en campo): `ordenes.aplicar_datos_argos`. La orden abierta toma de la instantánea de Argos (la que sigue siendo de su cliente y empresa) esos datos, quita de la marca lo aplicado y sube la versión. |
| `orden.datos_argos_descartados` | `orden_id`, `numero`, `campos_argos[]`, `motivo` (o null) | I1 (en campo): `ordenes.descartar_datos_argos`. La orden se queda con sus datos; solo pierde la marca (sube la versión). |
| `orden.datos_argos_cambiados` | `orden_id`, `numero`, `entidad` (`servicio` \| `cliente`), `campos[]` (los acumulados de este aviso, con prefijo: `servicio.referencia`…), `entrante_id` | I1, contrato 0.5.0: `argos.datos_cambiados` sobre una orden abierta (no cerrada, fallida ni cancelada) de ese servicio o cliente. La orden conserva lo copiado y queda marcada (`datosArgosCambiados { en, campos[] }` en la API; no sube la versión) para que despacho lo aplique. |
| `orden.cerrada` | `orden_id`, `numero`, `tipo`, `resultado` (`exitosa` \| `parcial`), `empresa_id`, `cliente_id`, `servicio_id`, `ticket_id`, `prospecto_id`, `tecnico_id`, `tecnico` (0.5.0: `{ id_externo }` del vínculo con Argos de la ficha del técnico, o null), `bodega_id` (o null), `cerrada_en`, `retirar_servicio` (bool: retiro exitoso), `equipos_validados` (bool: false si algún equipo retirado no se verificó contra el comodato de Argos —sin conector o Argos caído—; tratarlo como dato no verificado), `materiales[]` (`producto_id`, `cantidad` —sumada por producto, bodega y usuario, 3 decimales—, `bodega_id` (o null), `usuario_id` (0.14.0: quien lo registró), `excede_existencia`, `modalidad` (0.5.0, o null)), `equipos[]` (`id`, `accion` `instalar` \| `retirar`, `unidad_id`, `serie`, `mac` `AA:BB:CC:DD:EE:FF`, `modelo`, `condicion` `buena` \| `danada` \| `perdido` (solo retirar; `perdido` = pérdida del comodato), `bodega_id`, `validado`, `usuario_id` (0.14.0, en `instalar` y `retirar`: quien lo registró); desde 0.12.0 también `cambio` y `no_recuperado`, con `foto_ref` obligatoria en `no_recuperado` desde 0.14.0; desde 0.14.0 solo los resumen, porque salen a Argos al aplicarse la operación del lote) | `ordenes.cerrar`. Lo consume el núcleo (I1) para los pendientes en Argos (contrato §3): con `resultado = exitosa`, activar la instalación y el comodato, devoluciones o pérdidas, retiro del servicio; materiales → consumo desde la bodega. `cargo` (o null): `{ producto_id?, monto? (texto/número > 0, 2 decimales), descripcion }` si el tipo de orden o su plantilla dicen cobrar (visita cobrada; ver «Cargo» en `apps/api/internal/campo/README.md`). |
| `orden.identidad_registrada` | `orden_id`, `pendiente_id`, `toma`, `documento_tipo` (`dpi` \| `pasaporte`), `metodo` (`foto_documento`), `reemplaza_id` (o null: la toma rechazada que reemplaza) | `orden.registrar_identidad` del lote (contrato argosisp.md 0.10.0 §3.4.3, A41). Nunca el número, el nombre ni la foto. |
| `orden.cambio_titular_registrado` | `orden_id`, `pendiente_id`, `cambio`, `documento_tipo` (`dpi` \| `pasaporte`: el de la identidad; con empresa, el del representante), `tipo_titular` (`persona` \| `empresa`, 0.14.1), `motivo_codigo` (`menor_de_edad` \| `otro`), `reemplaza_id` (o null) | `orden.cambiar_titular` del lote (§3.4.4, A39). El cambio lo confirma Argos después (`servicio.titular_cambiado`). Nunca el documento, el teléfono, el correo ni el texto del motivo. |
| `orden.datos_completados` | `orden_id`, `pendiente_id`, `n`, `campos[]` (solo nombres: `nombre`, `telefono`, `whatsapp`, `email`, `tipo_documento_fiscal`, `nit_cui`, `relacion`, `direccion`, `referencia`, `coordenadas`) | `orden.completar_datos` del lote (§3.4.6, A43). Nunca los valores. |
| `orden.cambio_plan_registrado` | `orden_id`, `pendiente_id`, `cambio`, `plan_id`, `plan_anterior_id` (o null: el último pedido en la orden), `motivo` (`mas_velocidad` \| `menos_velocidad` \| `otro`) | `orden.cambiar_plan` del lote (§3.4.5, A40). Sin el texto del motivo. |
| `orden.identidad_validada` | `orden_id`, `usuario_id` (el supervisor), `metodo` (`revision_panel` \| `sin_identidad`), `pendiente_id` (o null: la identidad mandada como revisión), `reemplaza_id` (o null) | `POST /ordenes/{id}/validar-identidad` (`ordenes.validar_identidad`, §3.4.8): solo permite activar. Sin el motivo (queda en la auditoría). |
| `orden.revision_titular_levantada` | `orden_id`, `usuario_id` (el supervisor) | `POST /ordenes/{id}/levantar-revision-titular` (`ordenes.validar_identidad`): quita la marca de revisión que dejó `titular_no_coincide`. Sin el motivo (queda en la auditoría). |
| `orden.contrato_sin_firma` | `orden_id`, `prospecto_id`, `empresa_id`, `contrato_id` (o null), `cotizacion_id` (la revisión, o null si no se pudo crear), `operacion` (`cambiar_plan` \| `cambiar_titular`, o null), `motivo`, y en los diagnósticos `clave` (la del pendiente) y `detalle` (o null). `motivo`: `cierre_sin_firma` (un supervisor cerró la orden sin la firma), `cambio_tras_cierre` (Argos aceptó el cambio con la orden ya cerrada: la revisión se crea sin contrato), `sin_regenerar` (la orden reabierta se cerró antes de regenerar el contrato), y los diagnósticos (uno por clave y motivo, auditados) de un cambio hecho cuya revisión no se pudo crear —así no habrá contrato que firmar y la orden no cierra sin un supervisor—: `sin_cotizacion_viva`, `sin_plan`, `revision_rechazada` (`detalle`: el código) y `sin_titular` (cambio de titular sin el nombre del titular nuevo: la revisión se crea sin contrato) | comercial (B20, verificación de #280) —y campo, con `cierre_sin_firma` y la `clave` del pendiente, cuando el supervisor cierra con el cambio todavía en la cola—, en la misma transacción en que anula el regenerado que seguía en borrador con la orden cerrada, crea la revisión sin contrato o detecta el diagnóstico. Argos ya tiene el plan o el titular nuevo sin la firma de B6. Notificaciones lo pone en la campana de los supervisores de la orden (`contrato_sin_firma`, clave por `contrato_id`, si no por `cotizacion_id` y, en los diagnósticos, por la clave del pendiente). |
| `orden.paso_titular_tras_cancelar` | `orden_id`, `pendiente_id`, `operacion`, `estado_orden` (`cancelada` \| `fallida`) | integracion (pendientes): un paso del titular que estaba saliendo cuando la orden se canceló o falló quedó hecho en Argos. Con la orden cerrada, un cambio de plan o de titular lo avisa comercial (`orden.contrato_sin_firma`): una sola campana. Supervisión lo revisa (no se deshace solo): notificaciones lo pone en la campana de los supervisores de la orden (`paso_titular`, clave `paso_titular:<pendiente_id>`). |
| `orden.titular_movido` | `orden_id`, `numero`, `servicio_id`, `de_cliente_id` (o null), `a_cliente_id`, `tipo_titular` (o null), `entrante_id` | B17: `argos.titular_cambiado` pasa las órdenes abiertas del servicio al titular nuevo (las cerradas conservan su cliente), con `guardar` (versión, feed del técnico y diff), solo si la instantánea sigue diciendo que el servicio es de `a_cliente_id` (MEDIA 2 de #280). Sin datos del cliente. |

### Ajustes de la app de campo

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `campo.ajustes_cambiados` | `empresa_id` (o null), `campos[]` (`tecnicos_toman_libres`), `tecnicos_toman_libres` | `campo.configurar` (`POST /campo/ajustes`). |

## Comercial (I2)

Emite el módulo `comercial`, con `empresa_id` en el sobre = la del prospecto (null sin conector).
Montos como texto decimal con 2 cifras (`"250.00"`). Fechas en UTC.

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `prospecto.creado` | `prospecto_id`, `numero`, `empresa_id`, `origen` (`panel`, `web`, `referido`, `argos`, `argos_panel`, `otro`), `zona_id` (o null), `vendedor_id` (o null) | `prospectos.crear`; con `argos_panel`, `prospectos.registrar_integracion` (`POST /integraciones/argosisp/prospectos`, contrato con Argos 0.10.0 §3.6). |
| `prospecto.actualizado` | `prospecto_id`, `campos[]` (`nombre`, `documento`, `telefono`, `correo`, `direccion`, `lat`, `lon`, `zona_id`, `plan_interes_id`, `plan_interes_nombre`, `origen_detalle`, `notas`) | `prospectos.editar` (hasta cotizado). |
| `prospecto.asignado` | `prospecto_id`, `vendedor_id` (o null), `vendedor_anterior_id` (o null) | `prospectos.asignar`. |
| `prospecto.estado_cambiado` | `prospecto_id`, `de`, `a`, `motivo` (o null), `empresa_id` (o null), `origen` | Cada cambio de estado del embudo: `contactar`, factibilidad (nuevo/contactado → factibilidad), cotizar (→ cotizado; también desde contrato si se anula el borrador), contratar (→ contrato), `perder`, `reabrir` (perdido → contactado) y `ganar`. Con `origen = argos_panel` y un conector con contrato ≥ 0.10.0, va a Argos (§6.6) solo con `prospecto_id`, `empresa_id`, `de`, `a` y `motivo_codigo` (siempre null): nunca el motivo escrito. |
| `prospecto.actividad_registrada` | `prospecto_id`, `actividad_id`, `tipo` (`llamada`, `visita`, `nota`, `mensaje`), `proxima_fecha` (o null) | `prospectos.registrar_actividad`. |
| `prospecto.perdido` | `prospecto_id`, `empresa_id`, `motivo`, `de` (estado anterior) | `prospectos.perder`. Anula el contrato vivo (`contrato.anulado`). |
| `prospecto.ganado` | `prospecto_id`, `empresa_id`, `modo` (`local` \| `argos`), `contrato_id`, `cotizacion_id`, `plan_id` (o null), `obra_confirmada`, `router_id` (argos), `orden_id` (local: la orden de instalación; argos: null hasta el alta), `pendientes[]` (argos: el id del pendiente `crear_cliente`) | `prospectos.ganar`. |
| `prospecto.alta_actualizada` | `prospecto_id`, `estado` (`esperando_cliente` \| `esperando_servicio` \| `esperando_activacion` \| `completa`), `cliente_id`, `servicio_id` (o null), `orden_id` (o null), `error` (solo si el alta quedó para revisión manual) | El alta en Argos avanza (actor sistema): cliente creado → se encola el servicio; servicio creado → orden de instalación (`orden.creada` con `origen = sistema` y `prospecto_id`). |
| `prospecto.venta_revisada` | `prospecto_id`, `empresa_id`, `vendedor_id` (o null), `orden_id`, `motivo` (`plan` \| `titular`), `cotizacion_id` (la revisión nueva, **vigente**), `contrato_id` (el regenerado en borrador, ligado a la orden; null si la orden ya estaba cancelada o fallida —se regenera al reabrirla o reprogramarla— o cerrada —no se ofrece por firmar y supervisión recibe `orden.contrato_sin_firma`—) | B20 (contrato 0.10.0 §3.4.8; ALTA 1 de la revisión de #280): Argos aceptó el cambio de plan (A40) o de titular (A39) de la orden de instalación de una venta de Iris (`integracion.pendiente_hecho` de `cambiar_plan` o `cambiar_titular`). Se crea una revisión de la cotización **vigente** con vigencia nueva (`cotizacion.creada`; `reemplaza_id` = la revisión anterior aún sin firmar, o null) y el contrato se regenera en borrador a nombre del titular confirmado en la orden (`contrato.creado`; el documento del titular nuevo vive solo en Argos). La cotización aceptada y el contrato firmado **siguen vivos**: el cambio ocurre cuando el titular firma el regenerado en la orden (`orden.firmar_contrato`): ahí el firmado anterior se anula, la aceptada queda `reemplazada` y la revisión, `aceptada` (`contrato.anulado`, `contrato.firmado` con `orden_id`, `cotizacion.aceptada`). Un cambio desde el panel no se da por aceptado sin esa firma. Notificaciones lo pone en la campana del vendedor (`venta_revisada`). Uno por pendiente. |
| `factibilidad.evaluada` | `prospecto_id`, `factibilidad_id`, `resultado` (`viable`, `no_viable`, `requiere_obra`, `indeterminado`), `motor` (`distancia`, `manual`; `ozmap` en la fase 6), `motivo` (indeterminado: `sin_conector`, `conector_apagado`, `sin_coordenadas`, `sin_empresa`, `sin_instantanea`, `sin_routers_con_coordenadas`, `planta_no_disponible`; si no, null), `router_id` (sugerido, o null), `caja` (o null), `distancia_m` (o null), `anula_id` (la automática no_viable que anula una manual, o null) | `factibilidad.evaluar` y `factibilidad.registrar_manual`. |
| `factibilidad.umbrales_cambiados` | `empresa_id`, `viable_m`, `obra_m` | `factibilidad.configurar`. |
| `cotizacion.creada` | `cotizacion_id`, `prospecto_id`, `numero`, `revision`, `plan_id` (o null sin conector), `total_mensual`, `total_unico`, `moneda`, `reemplaza_id` (la anterior, que queda `reemplazada`, o null) | `cotizaciones.crear`. |
| `cotizacion.aceptada` | `cotizacion_id`, `prospecto_id`, `numero` | `cotizaciones.aceptar`. |
| `contrato.creado` | `contrato_id`, `prospecto_id`, `cotizacion_id`, `numero` | `contratos.crear`. |
| `contrato.firmado` | `contrato_id`, `prospecto_id`, `numero`, `adjunto_id` (PDF firmado), `sha256`, `orden_id` (o null: el contrato regenerado en la instalación, B20) | `contratos.firmar` u `orden.firmar_contrato` del lote. El contrato regenerado (con `orden_id`) solo se firma en sitio con la operación del lote: `POST /contratos/{id}/firmar` lo rechaza con `409 se_firma_en_la_orden`; al firmarlo se anula el firmado anterior. El SVG de la firma nunca va en el evento, y el documento del firmante sale como `***` en la auditoría. |
| `contrato.anulado` | `contrato_id`, `prospecto_id`, `de` (`borrador` \| `firmado`), `motivo` | Una cotización nueva anula el borrador; perder anula el vivo (y el regenerado por firmar); firmar el regenerado en la orden anula el firmado anterior; una revisión nueva de la instalación anula el regenerado que aún no se firmó; cancelar, fallar o cerrar la orden (`orden.estado_cambiada`) anula el regenerado por firmar (la revisión queda sin aceptar y el firmado sigue vivo); al reabrirla o reprogramarla (de cancelada o fallida a un estado abierto) se regenera otro para esa orden con la última revisión vigente (`contrato.creado`; vigencia y PDF nuevos si venció). |

Pendientes hacia Argos (contratos/argosisp.md §3, «Prospecto ganado»), `entidad = prospecto`:
`crear_cliente` (`POST /clientes`, `iris:cliente:<prospecto_id>`) y `crear_servicio`
(`POST /servicios`, `iris:servicio:<prospecto_id>`, `depende_de` = el del cliente). Cuerpos según
Argos 1.2 (solo los opcionales con valor; se serializan una vez y se guardan en el pendiente: la
cola y la repetición mandan los mismos bytes, que es la huella de idempotencia de Argos):

- `POST /clientes`: `nombre`, `codigo_externo = "iris:prospecto:<id>"`, y si hay: `nit_cui` (documento), `email`, `telefono`, `direccion`, `coordenadas` (`"lat,lng"`), `zona_id`. Sin `empresa_id` (el cliente es de la instalación).
- `POST /servicios`: `cliente_id`, `router_id` (el sugerido por la factibilidad o el elegido al ganar), `plan_id`, `empresa_id` (la del prospecto), `estado_inicial = "pendiente_instalacion"`, y si hay: `direccion`, `coordenadas`. Sin `origen_tipo`/`origen_id`.
- Respuesta: `2xx` con `{ id }`; repetir la clave con los mismos bytes devuelve el 2xx original (`Idempotent-Replayed: true`). Tras recuperar el id, Iris comprueba con `GET /clientes/{id}` que `codigo_externo` sea el del prospecto (y con `GET /servicios/{id}` que el servicio sea de ese cliente); si no, `alta.error`.

Consume `integracion.pendiente_hecho` con `entidad = prospecto` (avanza el alta; idempotente).
