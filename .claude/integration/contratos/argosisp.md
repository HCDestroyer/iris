Versión: 0.14.3 (22 nov 2026) · Iris ↔ ArgosISP

# Contrato de integración Iris ↔ ArgosISP (copia de ArgosISP)

## Gobierno

- Este texto es la fuente de verdad compartida. Vive con el mismo contenido en `HCDestroyer/iris` (`.claude/integration/contratos/argosisp.md`) y en `HCDestroyer/argos-isp` (`.claude/integration/contratos/iris.md`). La línea de versión identifica la copia al día.
- Cada sistema toca solo su código. Lo que uno necesita del otro se pide en §5, y el dueño lo implementa en su repositorio.
- Cambiar el contrato requiere dos PRs enlazadas, una por repositorio. La versión sigue semver.
- Se reutilizan los mecanismos ya probados del contrato Argos ↔ Horus (`argos-isp/.claude/integration/contratos/horus.md` §1): códigos de conexión, webhooks firmados, enlace de conexión y autologin por ticket. Aquí solo se escriben las diferencias.
- **La 0.8.0 es final: acordada con Argos y decidida por el PM (18 nov 2026).** El líder de Argos revisó el borrador contra su código (`argos-isp`, rama `lider/iris-072`, commit `f857fb74`, `.claude/integration/revision-iris-080.md`; Argos 1.17.0, contrato 0.7.1) y este texto incluye sus respuestas. El PM adoptó las propuestas de §10.2. Argos lo implementó en `argos-isp#249` y lo publicó todo junto en su versión 1.18.0; en Iris está implementada (Iris 0.33.0, que informa contrato 0.8.1). La 0.8.0 ya está espejada en `argos-isp` (§10.1). Lo poco que sigue abierto está en §10.1.
- **0.8.1 (19 nov 2026): cambio aditivo sobre la 0.8.0, aceptado por el líder de Argos.** Agrega `buscar_red` a la búsqueda `q` de `GET /clientes` y `GET /servicios` (§2.1, pedido A29). Argos lo publica en su versión 1.21.0. Espejo: PRs enlazadas `iris#222` y `argos-isp#258`. Nada más cambia respecto de la 0.8.0.
- **0.9.0 (21 nov 2026): aditiva sobre la 0.8.1; acordada con Argos y decidida por el PM (21 nov 2026); pendiente de implementar en Argos e Iris.** Pedido del PM por el líder de Argos; propuesta de Iris, revisada por Argos contra su código (1.23.0) y aprobada sin objeciones, con los ajustes que este texto ya incluye. Dos bloques que pueden publicarse por separado, cada uno con su propia marca en `capacidades` (§4.3): **P**, el portal del cliente de Argos con tickets y solicitudes de servicio de Iris (§11); **E**, equipos del cliente y activación desde la orden de Iris, con Argos como único dueño de SmartOLT y del ACS (§12). Decisiones del PM (detalle en §10.4): una solicitud de servicio la atiende ventas en Iris (prospecto) y la convierte en orden; un cliente identificado solo por su IP abre tickets y ve la lista, nada más; el cambio de Wi-Fi que hace el cliente desde su portal es solo de Argos (con reautenticación) y queda fuera del contrato; Iris nunca guarda credenciales de SmartOLT ni del ACS; la activación la pide el técnico desde la orden o un agente desde el panel de Iris, para FTTH y wireless, y cada empresa elige si se puede cerrar la orden sin ella; el mensual empieza con el servicio activo y la instalación se cobra como factura única que registra un agente o un formulario de la orden; el asistente de IA puede consultar el equipo y cambiar el Wi-Fi solo si la empresa lo habilita, con permisos propios y el cliente verificado.
- **0.10.0 (21 nov 2026): aditiva sobre la 0.9.0. Acordada con Argos y decidida por el PM (21 nov 2026, §10.5); todos los ajustes de Iris de §10.5 aceptados por Argos; pendiente de implementar en los dos lados.** De la venta a la instalación (§3.4): servicio pendiente sin IP (A38), cambio de titular (A39) y de plan (A40) antes de activar, identidad del titular registrada en Argos (A41), datos del titular y del lugar completados en la instalación (A43); contactos del cliente con su rol y sus permisos (A42, §3.5); prospectos registrados desde el panel de Argos (A44, §3.6); y código y nombre del servicio (A45, §3.7). Incluye la revisión de seguridad de la 0.10.0, aceptada por Argos.
- **0.10.1 (21 nov 2026): aditiva sobre la 0.10.0, decidida por el PM y acordada con Argos; pendiente de implementar en los dos lados.** Formato de `X-Iris-Contacto` en §7.1 y un cliente a cargo del servicio de otro (A46, §3.8).
- **0.11.0 (21 nov 2026): minor y aditiva sobre la 0.10.1, pedida por el PM y acordada con Argos; pendiente de implementar en los dos lados.** Bodega de cuadrilla (A47 y B26, §3.9; decisiones del PM en §10.6).
- **0.12.0 (21 nov 2026): minor y aditiva sobre la 0.11.0, pedida por el PM y acordada con Argos; pendiente de implementar en los dos lados.** Equipos del cliente con la consistencia de Argos: cambio de equipo en una transacción, estados de la unidad, revisión, garantía y equipos no recuperados (A48 y B27, §3.10; decisiones del PM en §10.7).
- **0.12.1 (21 nov 2026): parche aditivo sobre la 0.12.0, acordado con Argos; pendiente de implementar en los dos lados.** Clave del consentimiento por número (§6.4) y tickets del portal pedidos por un contacto de un servicio a cargo (§11, B28; §10.8).
- **0.13.0 (21 nov 2026): minor y aditiva sobre la 0.12.1, decidida por el PM y acordada con Argos; pendiente de implementar en los dos lados.** Caídas repetidas con umbral de Iris y falla general (§3.11), gestiones de cobro (§3.12) y lectura de plantillas de WhatsApp desde Argos (§6.7). Decisiones del PM en §10.9.
- **0.14.0 (21 nov 2026): minor sobre la 0.13.0, acordada con Argos; pendiente de implementar en los dos lados.** No es aditiva: cambia claves, reintentos y lo que cuenta como intento de recuperación, con la transición de §4.5. Sale de la revisión de seguridad de B26 y B27 en Iris y de tres revisiones de este cambio: qué errores de bodega y equipos no se reintentan solos, quién va en `X-Iris-Actor` en el material y en los equipos, el vínculo de cada técnico enviado a Argos, claves por operación y consumo por actor, foto obligatoria en todo intento con verificación del intento de servidor a servidor (ruta nueva de Iris), y la validación de ubicación y foto que hace Iris (§3.9, §3.10, §6.6; §10.10). Espejo: `iris#274` · PR enlazada en `argos-isp`.
- **0.14.1 (22 nov 2026): parche aditivo sobre la 0.14.0, propuesto por Argos y acordado; pendiente de implementar en los dos lados.** Cambio de titular a una **empresa** en A39 (§3.4.4): `cliente.tipo`, NIT en los campos fiscales que ya existían y `representante` con DPI o pasaporte; titular existente por NIT verificado, con asignación al momento solo si el representante ya está registrado en esa empresa y, si no, revisión (`titular_empresa_en_revision`); `tipo_titular` en `servicio.titular_cambiado`. Incluye la revisión de seguridad y las decisiones del PM del 22 nov 2026 (§10.11).
- **0.14.2 (22 nov 2026): parche sobre la 0.14.1, acordado con Argos; pendiente de implementar en los dos lados.** Ajustes del portal (§11): sin HEIC ni WebP en los adjuntos (el portal de Argos convierte a JPEG), el plan de una solicitud es un plan activo de Argos (los planes son globales), actor `sistema` en una lista cerrada de lecturas (§7.1) y una ruta propia de `capacidades` para el rol `integracion_argos` (§4.3) (§10.12).
- **0.14.3 (22 nov 2026): parche aditivo sobre la 0.14.2, acordado con Argos; pendiente de implementar en los dos lados.** Dudas del bloque E que dejó la implementación en Iris (B12, B14–B16): técnico en la activación pedida por un agente (§12.2), marcas por función en `capacidades` (§4.3), relación de la bienvenida sin `origen` (§12.5) y el `409 aprovisionamiento_en_curso` ajeno (§12.2) (§10.13). Pedidos A32, A33, A35, A36, A37, B12, B14, B15 y B16 ampliados. Suma la lectura `GET /facturas/origen-iris` para conciliar un «cobro por confirmar» (§3), con el permiso nuevo `facturas.leer_origen_iris`.
- **0.15.0 (26 nov 2026): PROPUESTA de Argos, aceptada por Iris con ajustes (§10.14); pendiente de la PR espejo y de implementar en los dos lados.** Minor y aditiva sobre la 0.14.3. Decisiones del PM del 25 y 26 nov 2026 (§10.14), plan en `argos-isp` (`.claude/integration/contratos/plan-servicios-suscripciones-recordatorios.md`). Tres bloques que se activan por separado, cada uno con su marca en `capacidades` (§4.3, §4.6): **A**, tipo de servicio (`internet`, `cable`, `iptv`, `mixto`) en la ficha y los webhooks de servicios (§13.1); **B**, corte manual de respaldo del CATV avisado a Iris, que lleva tickets y órdenes (§13.2); **C**, recordatorios entregados por Iris por los canales que declare para cada empresa (hoy WhatsApp; los canales que Iris no declara los sigue enviando Argos), con `canal_preferido: "auto"` y códigos de aviso nuevos (§13.3). Pedidos A53 a A55 y B33 a B35 (§5). La línea de versión sigue en 0.14.3 hasta que se fusionen las dos PRs (`argos-isp` e `iris`); hasta entonces Argos no envía nada de la 0.15.0. Los ajustes de Iris están en §10.14.
- **0.15.1 (26 nov 2026): PROPUESTA de Iris, pedido del PM, con el diseño de Argos (§10.15); pendiente de la PR espejo y de implementar en los dos lados.** Parche aditivo sobre la 0.15.0: cambiar el tipo de servicio, por ejemplo de cable a cable con internet (§13.4, A56 / B36). `cambiar-plan` en pendiente cruza tipos y la ruta nueva `POST /servicios/:id/cambiar-tipo` lo hace con el servicio activo. Para el bloque C (§6.6, §13.3), el webhook `aviso.bloqueado` y el motivo `baja` en `aviso.fallido`.

## 0. Principios

| Regla | Detalle |
| --- | --- |
| Fuente de verdad | Argos es dueño de clientes, servicios, planes, routers, zonas, facturación e inventario. Iris es dueño de tickets, órdenes, agenda, rutas, prospectos y evidencias. |
| Independencia | Iris funciona sin Argos: con el conector apagado, los tickets y órdenes usan los datos capturados a mano. Argos nunca depende de Iris para AAA, facturación ni corte. |
| Degradación | Si Argos no responde, Iris muestra la última instantánea marcada `stale`. Las acciones hacia Argos quedan en cola de pendientes y se reintentan (1 min, 5 min, 30 min, 2 h, 12 h; después, alerta al supervisor). |
| Mínimo privilegio | El token de Iris en Argos lleva solo los permisos de §1.1, con alcance de empresas acotado. |
| Idempotencia | Toda escritura de Iris en Argos lleva `Idempotency-Key = iris:<operacion>:<id de la orden u operación>`. Los webhooks llevan un `id` estable y el receptor deduplica. |
| Auditoría | Argos audita las acciones del usuario técnico «Iris» con actor `token` y la referencia de la orden (`origen_tipo = 'iris_orden'`, `origen_id`). El canal es el de §7.1: `ia` con `X-Iris-Actor: asistente_ia` y `api` en el resto (`usuario:<id>` y las llamadas sin `X-Iris-Actor`). Los webhooks de §6.6 quedan con canal `webhook`. Única excepción (0.7.0): al aprobar o rechazar un comprobante desde Iris (§7.7), el actor de la auditoría es el usuario de Argos vinculado y el token de Iris queda como «vía». Iris audita sus lecturas de datos personales. |

## 1. Conexión, firma y autologin

### 1.1 Iris → Argos (Iris lee y escribe en Argos)

Iris guarda, cifrado y de solo escritura, un token de Argos `atk_…` del usuario técnico «Iris» (rol creado por el seed de Argos), con estos permisos:

- **Lectura:** `clientes.ver`, `clientes.estado_cuenta`, `servicios.ver`, `planes.ver`, `routers.ver`, `zonas.ver`, `ip.resolver`, `inventario.ver`.
- **Instalación y retiro:** `servicios.activar_instalacion` (pedido A1), `servicios.retirar`, `inventario.asignar_comodato`, `inventario.devolver_comodato`, `inventario.declarar_perdida_comodato`, `inventario.consumir`, `inventario.recibir_traslado` (0.5.0) y `facturas.emitir_cargo_unico`.
- **Comercial:** `clientes.crear` y `servicios.crear`.
- **Autologin:** `sso.emitir`.
- **Pagos y atención (0.6.0, §7, con los nombres de §9.1):** `comprobantes.registrar` (+ `comprobantes.ver` y `cuentas_bancarias.ver`), `comprobantes.reconexion_temporal`, `promesas.ver` (simulador, §7.4), `promesas.crear` y `clientes.verificar_identidad`. La búsqueda por teléfono (§7.6) es el filtro `telefono` de `GET /clientes`, con `clientes.ver`.
- **Aprobar y rechazar comprobantes (0.7.0, §7.7):** `comprobantes.aprobar` y `comprobantes.rechazar`. Argos migra los tokens que ya existen. El token solo abre la ruta: el permiso real lo decide el usuario de Argos vinculado al `X-Iris-Actor` (§7.7), y con `asistente_ia` nunca valen.
- **0.8.0:** no pide permisos nuevos en Argos (confirmado por Argos). Las cuentas bancarias (§7.8) usan `cuentas_bancarias.ver`, los datos del servicio (§2.4) usan `servicios.ver`, y la primera sincronización (§2.5) usa las lecturas de arriba. Informar el avance de la sincronización (`PUT /integraciones/iris/sincronizacion`, §2.5) va con `sso.emitir`, igual que `PUT /integraciones/iris/contrato`. Un permiso nuevo obligaría a migrar los tokens vigentes, porque el token se recorta a la lista de permisos de Iris al crearse.
- **0.9.0, bloque E (§12):** `servicios.aprovisionar`, `servicios.ver_equipo` y `servicios.cambiar_wifi`, separados (el asistente de IA de Iris usa los dos últimos solo si la empresa lo habilita). Son permisos nuevos: Argos migra los tokens que ya existen (como en 0.7.0) y solo los usa con un Iris que informó contrato ≥ 0.9.0. El token nunca recibe credenciales de SmartOLT ni del ACS.
- **0.9.0, bienvenida (§12.5, A37):** `servicios.reenviar_bienvenida` (nuevo, con migración de tokens).
- **0.9.0, cobro de la instalación (§3, A36):** sin permisos nuevos. El cargo único sigue con `facturas.emitir_cargo_unico` (arriba), más la lectura `GET /conceptos-cargo` del catálogo de conceptos. El token de Iris **no** tiene `facturas.cargo_sin_costo`: Iris nunca pide cargos en Q0. Desde 0.14.3, el token de Iris suma `facturas.leer_origen_iris` (lectura de §3, «cobro por confirmar»), con migración que lo agrega a los tokens de Iris existentes; solo ese token lo tiene.
- **0.10.0 (§3.4):** `servicios.reservar_ip`, `servicios.cambiar_titular`, `servicios.cambiar_plan_pendiente`, `clientes.registrar_identidad` y `servicios.completar_datos`. Son permisos nuevos (Argos migra los tokens que ya existen) y solo valen sobre servicios `pendiente_instalacion` con una orden de Iris en curso. El token de Iris **no** recibe un permiso general para editar clientes ni `clientes.ver_identidad`: sube la identidad y no la puede leer.
- **0.12.0 (§3.10):** `servicios.cambiar_equipo` (cambiar un equipo y registrar un intento de recuperación) e `inventario.ver_unidad` (leer una unidad). Son permisos nuevos (Argos migra los tokens). Con contrato ≥ 0.12.0, Argos rechaza `…/perdida` con el token de Iris (`403`) y la misma migración le **retira** `inventario.declarar_perdida_comodato`: declarar una pérdida lo decide una persona en Argos.
- **0.13.0 (§3.11, §3.12):** `incidentes_red.leer` (leer los servicios afectados de una falla general) y `clientes.registrar_gestion_cobro`. Son permisos nuevos (Argos migra los tokens).
- **0.15.1 (§13.4):** `servicios.cambiar_tipo` (nuevo, con migración de tokens). Solo vale con una orden de Iris como origen (`origen_tipo: "iris_orden"`).
- **0.10.0, contactos (§3.5, A42):** `contactos.buscar` y `contactos.gestionar` (nuevos, con migración de tokens). Con `contactos.gestionar` el token de Iris nunca crea ni edita un titular (§3.5).

El usuario técnico Iris solo tiene alcance por empresas (o global), nunca por zona ni router: una lista completa y vacía significa sin servicios. Argos lo hace cumplir: un token, un rol o un correo que le darían alcance por zona o router responde `422 usuario_tecnico_solo_empresas`, y el canje nunca emite un token así.

### 1.2 Argos → Iris (Argos lee de Iris)

Token `iris_pat_<32B base62>` con `tickets.ver`, `tickets.crear`, `ordenes.ver`, `sso.emitir` y `portal.emitir`. Argos lo usa para mostrar los tickets y las órdenes de un cliente en la ficha de su panel (A7: `GET /api/v1/tickets?clienteId=`; desde 0.9.0 Iris acepta también `cliente_id` como alias) y para el acceso al portal de soporte de Iris (§11.9).

**0.6.0:** el mismo token suma `mensajeria.enviar` y `mensajeria.ver` para usar el servicio de mensajería de §6. Son permisos de la integración `integracion_argos`; un usuario de Iris no los recibe por rol.

**0.9.0:** el portal del cliente de Argos (§11) usa un **token aparte**, `token_portal` (otro `iris_pat_` del mismo usuario de integración, recortado **solo** a `portal_argos.ver_tickets`, `portal_argos.crear_ticket`, `portal_argos.responder_ticket`, `portal_argos.confirmar_solucion`, `portal_argos.solicitar_servicio` y `portal_argos.ver_solicitudes`). Lo usa únicamente el servidor del portal de Argos; el token de arriba queda en el panel y no tiene `portal_argos.*`. Iris lo entrega en el cuerpo del canje (§1.5, campo `token_portal`) y, para un conector ya conectado, en `PUT /integraciones/iris/contrato` cuando informa contrato ≥ 0.9.0; se rota con cada canje y Argos lo guarda cifrado y de solo escritura (S5, S6). Son permisos del rol `integracion_argos`: ningún rol de persona de Iris los recibe, y aunque un admin los tenga, las rutas de §11 exigen el `token_portal` de un conector Argos (`403 solo_integracion_argos`).

**0.10.0:** el token de arriba (no el `token_portal`) suma `prospectos.registrar_integracion` para registrar prospectos (§3.6, A44). No lee, no lista ni edita prospectos.

**0.11.0:** el mismo token suma `cuadrillas.leer_integracion` para `GET /integraciones/argosisp/cuadrillas/:id` (§3.9), solo en las empresas del alcance del conector (fuera, `404`).

**0.13.0:** el mismo token suma `plantillas_whatsapp.leer_integracion` para `GET /integraciones/argosisp/plantillas-whatsapp` (§6.7), solo en las empresas del alcance del conector.

**0.14.0:** el mismo token suma `adjuntos.verificar_integracion` (rol `integracion_argos`) para `GET /integraciones/argosisp/intentos-recuperacion/verificacion` (§3.10), que verifica intentos y cambios, solo en las empresas del alcance del conector (fuera, `404`). Nunca devuelve la imagen, el autor ni las horas. Con `cuadrillas.leer_integracion` lee además `GET /integraciones/argosisp/tecnicos/:usuario_id` (§6.6). Iris lo agrega a los `iris_pat_` vigentes cuando informa contrato 0.14.0 (§4.5).

### 1.3 Webhooks, enlace de conexión y autologin

Igual que en el contrato Argos ↔ Horus, §1.3–§1.5:

- **Webhooks:** cabeceras `X-Webhook-Id`, `X-Webhook-Timestamp` y `X-Webhook-Signature: v1=…`, con HMAC-SHA256. El receptor genera el secreto. Reintentos a 1 min, 5 min, 30 min, 2 h y 12 h.
- **Enlace de conexión:** `https://<A>/conectar#<codigo>`, válido 15 min y de un solo uso. El canje es servidor a servidor:
    - en Argos, `POST /api/v1/integraciones/iris/conexion/canjear`;
    - en Iris, `POST /api/v1/integraciones/argosisp/conexion/canjear`.
    - `sistema` vale `"iris"` o `"argosisp"`. El conector queda apagado hasta que un administrador lo active. Desde 0.8.0 hay una excepción solo del lado de Argos: si la conexión nació del botón (§1.4), Argos enciende su conector al confirmarse el canje (§1.5).
    - El canje de Argos responde `{ token, webhook: { url, secreto }, conector_id, alcance_global, empresas[] }`. `empresas[]` son las empresas activas del alcance del usuario técnico «Iris» al canjear, en orden de id, cada una `{ id, nombre, nit }` (`nombre` = nombre comercial; `nit` sin guiones, o `null` si la empresa aún no lo tiene). `alcance_global: true` significa que Iris verá también las empresas que se creen después; `false`, solo las listadas.
    - El host de `webhook.url` es siempre el de la base que Iris usó para canjear: el `<base>` del enlace (§1.3) o `api=` del botón (§1.4). Nunca el del panel: en producción la API y el panel pueden estar en dominios distintos. Si falta `webhook` o su host es otro, Iris rechaza el canje con `422 webhook_invalido` (0.6.2).
    - **Canje rechazado o incompleto (0.6.3).** Cuando Argos responde `200` al canje pero Iris no lo completa por cualquier motivo (`422 webhook_invalido`, un error al guardar, un pánico), Iris llama a `POST /api/v1/integraciones/iris/conexion/revocar` con el `token` (`atk_`) de esa respuesta, sin cuerpo, y lo reintenta en segundo plano (solo `5xx` y timeouts). Argos revoca ese token. Si es el de una reconexión pendiente, la descarta y el conector sigue igual. Si es el del conector, lo deja **desconectado**: apagado, sin el `iris_pat_` ni los secretos de webhooks, sin borrar el historial. Responde `204`. Repetirlo con el mismo token responde `401`, porque el token ya no autentica. Para Iris, `204` y `401` significan lo mismo: el token ya no sirve y no se reintenta. Con otra credencial, `403`. Si Iris no recibió la respuesta del canje (timeout), puede repetir la misma petición mientras el código no venza para obtener el token y revocarlo. Repetir un canje cuyo token ya se revocó responde `410 enlace_vencido`.
    - **Canje sin uso (0.6.3).** El canje queda pendiente de confirmar hasta la primera llamada autenticada con su `atk_`: cualquiera, sea `PUT /integraciones/iris/contrato`, una lectura o una escritura. Si pasan 24 h sin ninguna, Argos revoca el token y desconecta el conector solo; en una reconexión solo descarta el canje nuevo (punto siguiente). Un Iris que acepta el canje usa el token enseguida, al activar su conector, así que no nota nada. Los conectores anteriores a 0.6.3 cuentan como usados. `GET /integraciones/iris` (panel de Argos) muestra `estado_canje` (`pendiente`, `confirmado`, `desconectado` o `null` sin enlace), `canje_vence_en`, `desconectado_en` y `motivo_desconexion` (`revocado_por_iris` o `canje_sin_uso`). Volver a conectar es otro canje.
    - **Reconexión (0.6.3).** Si el conector de Argos ya está en uso (lo usó el `atk_` de un canje anterior, o se configuró a mano), un canje nuevo no lo reemplaza enseguida: queda pendiente hasta la primera llamada autenticada con su `atk_`. Iris hace esa llamada (el PUT del contrato) solo cuando el conector nuevo está activo de su lado. Hasta entonces, el conector anterior sigue igual: su `atk_`, la URL de Iris, el `iris_pat_`, los dos secretos de webhooks, `contrato` y `receptor_argos`. En la primera llamada del `atk_` nuevo, antes de atenderla, Argos aplica todo lo que trajo el canje nuevo y revoca el `atk_` anterior; el encendido del conector no cambia. Si el `atk_` nuevo se revoca o vence sin uso en 24 h, Argos lo descarta y el conector anterior queda vivo y sin cambios. Un tercer canje antes de confirmar reemplaza al pendiente, cuyo `atk_` se revoca. Si el conector no está en uso (no existe, está desconectado o su canje nunca se usó), el canje lo reemplaza al momento y revoca el `atk_` anterior. Así, de los canjes nunca quedan más de dos `atk_` vivos, el que está en uso y uno pendiente, y el pendiente vence solo. `GET /integraciones/iris` muestra `reconexion_pendiente_desde` y `reconexion_vence_en`.
- **Autologin:** `POST /api/v1/<prefijo>/sso/tickets` devuelve una URL `/sso/entrar#<ticket>`, válida 60 s y de un solo uso, con vinculación de usuarios y sin crearlos nunca.
    - Destinos en Argos: `cliente` y `servicio`. Desde 0.7.0 el destino es opcional (ver «Autologin desde Iris» abajo).
    - Destinos en Iris: `ticket`, `orden` y `prospecto`.
    - Autologin desde Argos («Ver en Iris»): el cuerpo de `POST <iris>/api/v1/integraciones/argosisp/sso/tickets` es `{ usuario: { id_externo, correo, nombre, correo_verificado }, destino }`. `usuario.correo_verificado` (boolean): Argos hoy siempre manda `false` (no verifica correos); con `false` o ausente, Iris no vincula por su cuenta y el vínculo lo hace un admin de Iris. «Ver en Iris» vincula al usuario de Argos **dentro de Iris**; no crea el vínculo de §7.7.
    - **Autologin desde Iris (0.7.0, «Abrir en Argos» y «Vincular mi usuario de Argos»):** `POST <argos>/api/v1/integraciones/iris/sso/tickets`.
        - **Credencial:** solo el `atk_` del usuario técnico Iris, con `sso.emitir`. Cualquier otra credencial responde `403`. Si el conector se preparó con un enlace, solo vale el `atk_` de ese canje. Con el conector apagado: `409 iris_apagado`.
        - **Cuerpo:** `{ usuario: { id_externo, correo?, nombre? }, destino? }`. `usuario.id_externo` es el id del usuario en Iris como string (por ejemplo `"42"`), limitado a `[A-Za-z0-9_-]{1,64}`: el mismo que va en `X-Iris-Actor: usuario:<id>` (§7.1). `correo` y `nombre` (string o `null`) son informativos: Argos nunca vincula por correo.
        - **`destino` es opcional desde 0.7.0** (cambio de Argos dentro de A23): `{ tipo: "cliente" | "servicio", id_externo }`, con `id_externo` = id de Argos (`^[1-9]\d{0,17}$`). Con `destino` es «Abrir en Argos». Sin `destino`, el usuario cae en el inicio del panel: es lo que usa «Vincular mi usuario de Argos».
        - **Respuesta:** `201 { url: "https://<panel>/sso/entrar#<ticket>", expira_en }`. El ticket dura 60 s y es de un solo uso.
        - **Canje en el panel de Argos:** si el usuario de Iris ya está vinculado, abre la sesión. Si no, Argos le pide su correo y su contraseña de Argos y crea el vínculo (conector `iris`, `id_externo` = el del ticket). Nunca crea usuarios.
        - **Pantalla de confirmación del vínculo (0.7.1, A24):** el panel de Argos muestra a quién se le va a dar acceso **antes de que la persona escriba su correo y su contraseña de Argos**, en la misma pantalla y encima del formulario:
            - el sistema correcto (Iris);
            - el nombre **y** el correo del usuario de Iris (los `usuario.nombre` y `usuario.correo` del ticket);
            - la instalación (`instalacion`, abajo); si es `null`, la pantalla dice «instalación no informada»;
            - el aviso «Vas a dar acceso a tu cuenta de Argos a esta persona de Iris. Si no eres tú, no continúes».
            - El vínculo se crea solo después de enviar ese formulario con éxito.
        - **`instalacion` en `vinculacion` (0.7.1, A24):** cuando el canje del ticket queda pendiente de vincular, su `vinculacion` trae además `instalacion` (string o `null`): el host de la `url_base` del conector Iris en Argos, con puerto solo si no es el estándar (por ejemplo, `iris.isp.example:8443`). Nunca lleva esquema, ruta, consulta ni credenciales. Vale `null` si el conector no tiene `url_base`. Es un cambio del panel de Argos: Iris no manda nada nuevo. `usuario.correo` y `usuario.nombre` siguen siendo informativos (Argos nunca vincula por correo); Iris los manda siempre que los tenga, para que la pantalla pueda mostrarlos.
        - **Este vínculo es el que autoriza aprobar y rechazar comprobantes desde Iris (§7.7), y nace solo aquí.** Por eso el `id_externo` del ticket debe ser exactamente el id que Iris manda en `X-Iris-Actor`.

### 1.4 Conectar con un botón (0.4.0)

Alternativa al enlace de §1.3, que sigue vigente: el admin conecta Iris con Argos sin copiar enlaces. Flujo de autorización con `state` y PKCE (S256), al estilo OAuth.

1. **En Iris**, un admin escribe la dirección del **panel** de Argos (`https://<panel-argos>`) y pulsa «Conectar con Argos». Iris genera:
    - `state`: 32 bytes aleatorios, un solo uso, 10 min, ligado a la sesión del admin;
    - `code_verifier`: 32 bytes aleatorios en base64url, que **nunca sale del servidor de Iris**, y `code_challenge = base64url(SHA-256(code_verifier))`.
2. **Iris abre** `https://<panel-argos>/integraciones/iris/autorizar?iris=<IRIS_BASE_URL>&state=<state>&code_challenge=<challenge>&code_challenge_method=S256`.
3. **Argos (panel)** exige sesión. Si no la hay, va a `/login` y vuelve a la misma URL. Además exige el permiso `iris.configurar`.
    - **Origen de Iris.** `iris` se normaliza a `https://host[:puerto]`, sin ruta (con `http` solo si `IRIS_PERMITIR_HTTP`). Se acepta sin más si coincide con la URL del conector Iris ya configurada o con `ARGOSISP_IRIS_ORIGENES` (lista separada por espacios). Si no, la pantalla muestra un aviso fuerte («Solo aprueba si tú abriste esta conexión desde tu Iris») y exige escribir el dominio de Iris para confirmar. Nunca se redirige a un origen que el admin no aprobó.
    - **Pantalla.** «Iris en `<origen>` pide conectarse con Argos» y las empresas que verá: el alcance del usuario técnico Iris, igual que `empresas[]` del canje, más `alcance_global`. Botones «Aprobar» y «Rechazar».
4. **Al aprobar**, `POST /api/v1/integraciones/iris/autorizar` con `{ iris, state, code_challenge, code_challenge_method: "S256", confirmar_origen? }` (permiso `iris.configurar`, auditado):
    - Crea el mismo código de conexión de §1.3 (15 min, un uso; un código nuevo anula los anteriores sin usar) y lo liga al `code_challenge` y al origen `iris`.
    - Responde `{ redirigir_a }`, y el panel navega a `<iris>/administracion/integraciones/argos/vuelta#codigo=<codigo>&api=<API_PUBLIC_URL>&state=<state>`.
    - **Al rechazar**, `<iris>/administracion/integraciones/argos/vuelta#error=rechazado&state=<state>`, sin llamar a la API.
    - Todo va en el **fragmento**, nunca en la query, para que no quede en logs ni en `Referer`.
    - Errores 422: `iris_origen_invalido`, `iris_origen_no_confirmado` y `code_challenge_invalido` (base64url de 43 caracteres).
5. **Iris** comprueba el `state`, y que `api` esté en `IRIS_ARGOS_ORIGENES`. Canjea servidor a servidor en `POST <api>/api/v1/integraciones/iris/conexion/canjear` con `{ codigo, sistema: "iris", code_verifier }`.
    - Argos exige `code_verifier` si el código nació con `code_challenge`: si no coincide, responde `401 codigo_invalido` y el código queda gastado. Un código de §1.3 (sin challenge) no lo pide.
    - Iris muestra el resumen y el conector queda **apagado en Iris** hasta que el admin pulse «Activar», como hoy. Del lado de Argos, desde 0.8.0 el conector se enciende solo al confirmarse este canje (§1.5).
6. **`panel_url` en el canje.** Las dos variantes del canje responden además `panel_url` (`PANEL_URL_PUBLICA` de Argos, por ejemplo `https://argos.kns.gt`). Iris guarda por separado `panel_url`, para «Ver en Argos» y para volver a autorizar, y `api_url`, el `api` del paso 5 o el origen del enlace de §1.3.

### 1.5 Conexión de un paso (0.8.0)

**Estado: acordada con Argos y decidida por el PM; implementada en Argos (PR #249, sale en 1.18.0); implementado en Iris (0.33.0, que informa contrato 0.8.1).** No reemplaza a §1.3 ni a §1.4: los usa. «Un paso» significa que, después de aprobar en Argos y activar en Iris, nadie copia URLs, secretos ni tokens, ni lanza cargas a mano. La 0.8.0 agrega cuatro cosas: (a) la primera sincronización automática (§2.5); (b) dos validaciones nuevas del receptor de Iris en el canje de Argos; (c) con el botón, el conector de Argos se enciende solo al confirmarse el canje; y (d) una pantalla de estado en cada panel (§2.5).

**Requisitos de seguridad (los fija Argos; no son negociables).** Cualquier implementación de esta sección los cumple todos; si uno no se puede cumplir, esa parte no se hace.

| # | Requisito |
| --- | --- |
| S1 | El código de conexión es de **un solo uso** y de **vida corta** (15 min, §1.3). Un código nuevo anula los anteriores sin usar. |
| S2 | El código viaja en el **fragmento** de la URL (`#…`), **nunca en la query**: no queda en logs de servidor ni en `Referer` (§1.3, §1.4 paso 4). |
| S3 | El canje es **servidor a servidor y por HTTPS**. El navegador nunca ve el token ni los secretos. |
| S4 | Si la conexión nace del botón (§1.4), **PKCE S256 es obligatorio**: sin el `code_verifier` correcto, `401 codigo_invalido` y el código queda gastado. |
| S5 | **En la conexión de un paso**, el token que recibe Iris (`atk_`) tiene **alcance acotado a empresas** (§1.1), **nunca es de administrador** (siempre recortado a los permisos de §1.1) y **nunca se muestra en un panel**, ni en Argos ni en Iris: se guarda cifrado y de solo escritura. Lo mismo vale para el `iris_pat_` que recibe Argos. La vía manual que el panel de Argos conserva (generar un `atk_` que se muestra una sola vez y pegar un `iris_pat_`) queda solo como **vía de recuperación**: solo `admin`, auditada y rotulada así en el panel (falta la confirmación del lado de Iris, §10.1). |
| S6 | Los secretos (tokens y secretos de webhook) **se rotan y no se reutilizan**: cada canje genera secretos nuevos en los dos lados con las reglas de reconexión de §1.3 (0.6.3). Al confirmarse el canje, Argos rota el secreto de su receptor y revoca el `atk_` anterior. El `iris_pat_` anterior lo revoca **Iris** de su lado: Argos solo deja de usarlo. Ningún secreto de un canje anterior vuelve a valer. |
| S7 | La conexión **la aprueba en Argos un usuario con `iris.configurar`**, con sesión propia y viendo el origen de Iris y las empresas del alcance (§1.4 paso 3). Nada la aprueba solo. |
| S8 | **La vinculación de usuarios nunca va en el setup.** Sigue siendo por persona, con el correo y la contraseña de Argos de cada quien y la pantalla de confirmación de 0.7.1 (§1.3, A24). Nada en bloque, nada por correo, nada por coincidencia de nombres. La primera sincronización no trae usuarios de Argos ni crea vínculos. |

**Capa extra: el certificado TLS de Argos, solo «registrar y avisar».** La huella TLS **no se fija ni se compara**, y nunca bloquea escrituras. El motivo es de la infraestructura de Argos:

- Argos no termina TLS: lo hace Caddy con Let's Encrypt, y hay instalaciones detrás de otro proxy. La API de Argos no conoce el certificado que ve Iris.
- En el canje, Argos no abre ninguna conexión TLS hacia Iris, así que no existe una «huella que Argos vio de Iris».
- Let's Encrypt renueva cada 60 a 90 días y Caddy, por defecto, cambia también la clave en cada renovación. Fijar el certificado **o** la clave pública cortaría las escrituras de Iris en cada renovación.

Lo que queda:

- **Argos no muestra ninguna huella** en la pantalla de aprobación. Ahí la protección es S1–S7.
- **Iris, si quiere la capa extra, la hace informativa:** muestra a su admin el emisor, el sujeto y la huella SHA-256 del certificado de la API de Argos. Si cambia sin una reconexión pero la cadena es válida para el mismo host, Iris **registra el cambio y avisa al admin**. No marca el conector, no detiene las escrituras y no pide confirmar.
- Iris solo bloquea si la cadena no valida contra las CA públicas, que es lo que TLS ya hace.

**(b) El receptor de Iris en el canje.** Este contrato remite al de Argos ↔ Horus para el cuerpo del canje y nunca lo escribió aquí. Iris ya manda hoy este cuerpo en el canje de Argos (`POST /api/v1/integraciones/iris/conexion/canjear`), y la 0.8.0 lo deja por escrito:

```json
{
  "codigo": "<código del fragmento>",
  "sistema": "iris",
  "base_url": "https://iris.isp.example",
  "token": "iris_pat_…",
  "webhook": { "url": "https://iris.isp.example/api/v1/integraciones/argosisp/webhook/7", "secreto": "…" },
  "code_verifier": "<solo si el código nació del botón>",
  "contrato": "0.8.0",
  "receptor_argos": true
}
```

- `token` es el `iris_pat_` de §1.2, que Iris emite en ese momento. `webhook` es el **receptor de Iris** para los webhooks de §2.3, con el secreto que Iris generó (el receptor genera el secreto, §1.3).
- **Ya existe, desde el conector original (A2), no desde la 0.8.0:** Argos guarda en la misma transacción del canje el origen normalizado de `base_url`, el `token` (`iris_pat_`, cifrado), `webhook.url` y `webhook.secreto` (cifrado), el secreto nuevo de su propio receptor, `contrato` y `receptor_argos`. No hay que configurarlos aparte en su panel. En una reconexión, el mismo juego queda pendiente y cifrado, y se aplica en la primera llamada del `atk_` nuevo (§1.3, 0.6.3).
- **Hoy Argos solo comprueba que `base_url` y `webhook.url` sean `https`** (`http` solo con `IRIS_PERMITIR_HTTP`). No comprueba que el host del receptor sea el de `base_url`, ni que `base_url` sea el origen que el admin aprobó con el botón. PKCE (S4) impide que un tercero sin el `code_verifier` use el código, así que el riesgo real es bajo, pero la regla no existe todavía.
- **Cambio de la 0.8.0 (A28): dos validaciones nuevas en el canje de Argos.** Si falla cualquiera, Argos responde `422` con `type` `urn:argosisp:problema:webhook_invalido`, el mismo nombre que usa Iris para el receptor de Argos (0.6.2):

    | `errors[0]` | Cuándo |
    | --- | --- |
    | `{ field: "webhook.url", rule: "mismo_host" }` | El host de `webhook.url` no es el host de `base_url` |
    | `{ field: "base_url", rule: "origen_aprobado" }` | El código nació del botón (§1.4) y el origen de `base_url` no es el origen `iris` que aprobó el admin |

    - En los dos casos **el código queda gastado**, igual que con un `code_verifier` que no coincide. Repetir el canje con ese código responde `410 enlace_vencido`. Hay que volver a pedir la conexión.
    - Los errores de formato (`422 validacion`) siguen sin gastar el código.
    - Un Argos anterior a la 0.8.0 no hace estas dos validaciones. Iris no depende de ellas: manda siempre su propio origen y su propio receptor.

**(c) Quién enciende el conector en Argos.** Lo único que el canje no hace es encender (`activo`).

| Cómo nació la conexión | Conector en Argos | Conector en Iris |
| --- | --- | --- |
| Botón (§1.4), desde 0.8.0 | Se enciende solo al **confirmarse** el canje: en la primera llamada autenticada del `atk_` (normalmente `PUT /integraciones/iris/contrato`), no al canjear | Apagado hasta que un admin pulsa «Activar» |
| Botón (§1.4), Argos anterior a 0.8.0 | Apagado hasta que un admin con `iris.configurar` lo enciende | Igual |
| Enlace (§1.3), cualquier versión | Apagado hasta que un admin con `iris.configurar` lo enciende, en el panel o con `PUT /integraciones/iris` | Igual |
| Reconexión (§1.3, 0.6.3) | `activo` no cambia | No cambia |

- Como el encendido va en la confirmación y no en el canje, si Iris rechaza el canje y lo revoca (§1.3, 0.6.3), el conector de Argos nunca se encendió.
- La configuración a mano (`PUT /integraciones/iris` y los tokens `atk_` del panel) sigue existiendo como vía de recuperación (S5).

**Qué queda igual.** El conector nace **apagado en Iris** hasta que un admin pulsa «Activar» (decisión del PM: ese paso se mantiene, §10.2). El canje sin uso vence a las 24 h. La reconexión es la de 0.6.3. `PUT /integraciones/iris/contrato` sigue siendo la primera llamada autenticada y es la que confirma el canje. La primera sincronización (§2.5) empieza al activar en Iris, no al canjear, y siempre después de esa primera llamada.

## 2. Iris consume de Argos

### 2.1 Búsqueda y ficha

| Uso en Iris | Llamada a Argos (API existente) |
| --- | --- |
| Buscar cliente al crear un ticket u orden | `GET /api/v1/clientes?q=&limit=` |
| Ficha del cliente con sus servicios | `GET /api/v1/clientes/:id` y `GET /api/v1/servicios?cliente_id=` |
| Estado de cuenta resumido (en la ficha del ticket) | `GET /api/v1/clientes/:id/estado-cuenta`, solo el saldo y las facturas vencidas (requiere `clientes.estado_cuenta`). **0.10.0:** `?servicio_id=` devuelve solo el saldo y las facturas de ese servicio; una factura que cubre varios servicios se muestra sin el desglose de los otros |
| Cliente por IP (ticket desde Horus o desde el portal) | `POST /api/v1/integraciones/iris/resolver` con la IP en el cuerpo (pedido A8; hoy solo existe `GET /ip/{ip}` y el resolver del conector Horus) |
| Planes para cotizar | `GET /api/v1/planes?activo=true&router_id=<router sugerido>` (planes globales de la instalación más los implementados en ese router; `precio` como string decimal; sin `empresa_id`) |
| Routers, zonas y coordenadas para factibilidad | `GET /api/v1/routers` (cada router trae `coordenadas`: `"lat,lng"` en grados decimales, sin espacios, o `null` si no está ubicado; el mismo formato que `coordenadas` de clientes y servicios) y `GET /api/v1/zonas` |
| Equipos en comodato de un servicio | `GET /api/v1/servicios/:id/equipos` |
| Bodega y kardex del técnico (0.5.0) | `GET /api/v1/inventario/mi-bodega?id_externo=<usuario de Argos>` (o `?correo=`): `{ bodega, existencias[], unidades[], traslados_pendientes[], kardex }` (últimos 50; más con `GET /api/v1/inventario/movimientos?bodega_id=` y cursor). Sin bodega activa: `404 tecnico_sin_bodega`. `GET /api/v1/inventario/existencias?bodega_id=` sigue igual |
| Ubicación y contacto en sitio (0.5.0) | `Cliente.referencia`; `Servicio.referencia`, `Servicio.contacto_nombre` y `Servicio.contacto_telefono` (null si no hay) |
| Cuentas bancarias de la empresa (0.8.0) | `GET /api/v1/cuentas-bancarias` (un arreglo, sin paginar) y `GET /api/v1/cuentas-bancarias/:id` (`cuentas_bancarias.ver`), y `GET /api/v1/bancos` (un arreglo, solo bancos activos). Detalle en §7.8 |
| Red, precio y ubicación del servicio (0.8.0) | `GET /api/v1/servicios/:id` y la lista: los campos de §2.4 |

**Búsqueda sin datos de red (0.8.1, A29).** `GET /api/v1/clientes?q=` y `GET /api/v1/servicios?q=` aceptan el parámetro opcional `buscar_red`: booleano que acepta `true`, `false`, `1` o `0`; por defecto `true`.

- **`buscar_red=true` o ausente:** igual que hoy; `q` coincide también con los datos de red de los servicios.
- **`GET /clientes?q=&buscar_red=false`:** `q` solo coincide por nombre, código, código externo, nombre fiscal, email, NIT/CUI y teléfono o móvil (para el token de Iris, el NIT/CUI ya estaba excluido de `q` y sigue igual). Quedan fuera la IPv4 vigente, una IPv6 dentro de un prefijo vigente y el `user_ppp` (parcial o completo) de sus servicios. Una `q` con forma de IP ya no coincide por dígitos con un teléfono.
- **`GET /servicios?q=&buscar_red=false`:** `q` solo coincide por el nombre del cliente. Quedan fuera la IPv4, el prefijo IPv6 y el `user_ppp`.
- La dirección, la MAC y el router nunca formaron parte de `q`, con ningún valor de `buscar_red`. Los demás filtros (`telefono`, `estado`, `excluir_retirados`…) no cambian.
- Cualquier otro valor responde `422` con `type` `urn:argosisp:problema:validacion` y `errors[0].field = "buscar_red"`, sin buscar.
- **Uso de Iris:** manda `buscar_red=false` siempre que el actor de Iris no tiene `servicios.ver_red` con alcance global (§2.4), para que una búsqueda no sirva para encontrar a un cliente por sus datos de red, sin depender de la versión que informó Argos. Con `servicios.ver_red` global no lo manda.
- **Compatibilidad:** un Argos anterior a 1.21.0 ignora el parámetro y sigue buscando también en los datos de red. Con él queda el límite conocido de la 0.8.0: Iris corta las búsquedas que parecen una IP sin `servicios.ver_red` global (lista vacía, sin llamar a Argos), pero un `q` con un `user_ppp` todavía puede encontrar al cliente. Iris usa `version_argos` (§3.3) solo para registrar si el parámetro tuvo efecto (§4.2).

### 2.2 Instantánea en Iris

Iris guarda en `integracion.argos_clientes` y `integracion.argos_servicios` solo lo que muestra: nombre, código, teléfono, `email`, dirección, coordenadas, `referencia` (cliente y servicio), contacto en sitio (`contacto_nombre`, `contacto_telefono`), zona, router, plan, estado y `empresa_id`. Cada fila lleva `sincronizado_en` y se refresca por webhook (§2.3) o al abrir la ficha si tiene más de 24 h.

**0.8.0.** La instantánea del servicio suma los datos de red y el precio de §2.4, con permisos propios en Iris. Iris la llena con una **lista blanca** de campos: lo que Argos mande de más no se guarda (§2.4). Se agrega una instantánea de las cuentas bancarias (§7.8). Con la primera sincronización (§2.5), las instantáneas se llenan al activar el conector y no solo al abrir cada ficha.

### 2.3 Webhooks Argos → Iris

`POST https://<iris>/api/v1/integraciones/argosisp/webhook/{conector_id}`. Cuerpo: `{ id, evento, ocurrido_en, payload }`, con `empresa_id` en el payload.

| Evento | Efecto en Iris |
| --- | --- |
| `cliente.actualizado`: cliente_id, empresa_id, empresas[], campos[] (los que cambiaron, solo nombres, nunca valores: `nombre`, `tipo`, `codigo`, `nit_cui`, `tipo_documento_fiscal`, `telefono`, `movil`, `direccion`, `coordenadas`, `zona_id`; desde 0.5.0 también `email` y `referencia`) | Refresca la instantánea del cliente. |
| `servicio.datos_cambiados` (0.5.0): servicio_id, empresa_id, empresas[] (= [empresa_id], igual que en `cliente.actualizado`), cliente_id, campos[] (`direccion`, `coordenadas`, `referencia`, `contacto_nombre`, `contacto_telefono`; solo nombres). No lleva cambios de red (esos van en `servicio.config_cambiada`) | Refresca la instantánea del servicio. Iris decide qué hace con sus órdenes abiertas. |
| `catalogo.cambiado` (0.5.0): catalogo (`zona` \| `plan` \| `router`), id, empresa_id (empresa sugerida de la zona o del router; null = compartido, llega a cualquier alcance; en `plan` siempre null), campos[]: `nombre` (los tres), `activo` (desactivar o reactivar, los tres) y `coordenadas` (router). Desde 0.8.0, en `plan` también `precio`, cuando cambia el precio del plan (hoy ese cambio no emite nada) | Refresca sus catálogos locales. Con `precio` en un plan, Iris relee los servicios de ese plan (`GET /servicios?plan_id=`, paginado) para refrescar su `precio_mensual` (§2.4). |
| `inventario.traslado_enviado` / `inventario.traslado_recibido` / `inventario.traslado_cancelado` (0.5.0): traslado_id, empresa_id, bodega_destino_id, tecnico (`{ id_externo, correo }` o null si el destino no es la bodega de un técnico), estado, lineas[] (`{ producto_id, cantidad, series[] }`), enviado_en; recibido: recibido_en, recibido_por, metodo_recepcion, origen (`iris` \| `panel`); cancelado: cancelado_en, motivo. Solo a conectores con contrato ≥ 0.5.0 (§3.3). Desde 0.11.0 suman `cuadrilla_id` (o null) cuando la bodega es de una cuadrilla (§3.9). | Enviado: muestra la orden por recibir en la app de campo del técnico (antes puede confirmar con `GET /inventario/traslados/:id`). Recibido o cancelado: la cierra. |
| `servicio.creado`, `servicio.config_cambiada`, `asignacion.cambiada`. Los `campos[]` de `servicio.config_cambiada` (solo nombres, nunca valores) son: `plan`, `ips`, `prefijo_v6`, `router`, `tipo_conexion`, `mac`, `amarre_ip_mac`, `empresa`, `servidor_pppoe` y `credenciales` (cambió `user_ppp` o la clave PPP; la clave nunca viaja). Desde 0.8.0 también `precio`: cambió `costo_override`, o empezó o terminó un plan temporal con otro precio (hoy ese cambio no emite nada). `asignacion.cambiada` trae `ipv4[]`, una lista | Refresca la instantánea del servicio releyendo `GET /servicios/:id`. |
| `servicio.estado_cambiado` | Refresca el estado. Con `a = retirado`, propone una orden de retiro si el servicio tiene equipos en comodato. |
| `inventario.equipo_por_recuperar` | Crea una orden de retiro (`origen = argos`), si no hay una abierta para esa unidad. |
| `servicio.caidas_repetidas` (pedido A6): servicio_id, empresa_id, cliente_id, router_id, sesion_id, sesiones, desde, hasta, umbral, ventana_minutos, causas. Por defecto 5 sesiones terminadas en 120 min, a lo sumo una vez por servicio y ventana; no cuentan los cortes que manda Argos ni los reinicios del NAS. **Desde 0.13.0 (A49)**, a conectores con contrato ≥ 0.13.0 con umbral de 3 caídas en 60 min, configurable por empresa en Argos | Desde 0.13.0, Iris aplica su umbral por empresa y abre un ticket por servicio (o suma una nota al abierto de esa categoría), §3.11. |
| `servicio.eliminado` (0.3.1): servicio_id, empresa_id, cliente_id, router_id. Solo servicios que nunca se usaron (sin facturas, pagos, sesiones ni otro historial); en Argos la fila ya no existe. Llega junto con un `asignacion.cambiada` de ips vacías. Solo si `empresa_id` está en el alcance del usuario técnico | Quita la instantánea del servicio y refresca la lista de servicios del cliente (`cliente_id`). Órdenes y tickets que lo referencian conservan su copia y lo muestran «eliminado en Argos». Un evento atrasado de ese servicio no recrea la instantánea; `GET /servicios/:id` responde 404 (esperado, no se reintenta; ver «Confirmar un borrado» abajo). |
| `router.eliminado` (0.3.1): router_id, empresa_id (empresa sugerida del router, o null = router compartido, que llega a cualquier alcance; con empresa, solo si está en el alcance). Solo routers sin historial | Quita el router de factibilidad (router o nodo sugerido); una factibilidad ya evaluada conserva el id. `GET /routers/:id` responde 404 (ver «Confirmar un borrado» abajo). |
| `comprobante.aprobado` / `comprobante.rechazado` (0.6.0): forma en §7.2. Solo a conectores con contrato ≥ 0.6.0 | Resuelve la boleta en la conversación y avisa al cliente (§6.3). |
| `promesa.creada` / `promesa.cumplida` / `promesa.incumplida` / `promesa.cancelada` (0.6.0): forma en §7.4. Solo a conectores con contrato ≥ 0.6.0. Desde 0.8.0 el payload suma `motivo_reconexion`, sin filtro de versión | Actualiza la promesa o la reconexión temporal de la conversación y explica al cliente. |
| `cuenta_bancaria.cambiada` (0.8.0; hoy Argos no emite ningún evento de cuentas): cuenta_bancaria_id, empresa_id, campos[] (solo nombres, nunca valores: `banco_id`, `numero`, `alias`, `titular`, `tipo`, `moneda`, `activa`), activa (el valor **después** del cambio), creada (boolean). Se emite al editar y **también al crear** (`creada: true`, con todos los campos guardados en `campos[]`). `empresa_id` no se edita. Solo a conectores con contrato ≥ 0.8.0, con el alcance de la empresa de la cuenta | Relee `GET /cuentas-bancarias/:id` y refresca la instantánea de la cuenta (§7.8). Una cuenta con `activa: false` deja de ofrecerse; nunca se borra. |
| `instalacion.aprovisionada` / `instalacion.fallida` (0.9.0, §12.2): aprovisionamiento_id, servicio_id, empresa_id, cliente_id, origen_tipo (`iris_orden`), origen_id, idempotency_key; aprovisionada: servicio_estado, equipo `{ tipo, serie, mac, unidad_id }`, senal `{ rx_dbm, tx_dbm }` (o null), activado_en; fallida: causa (§12.2), reintentable, detalle (texto sin secretos). Nunca la clave Wi-Fi ni credenciales. Solo a conectores con contrato ≥ 0.9.0 | Actualiza la activación de la orden; con `aprovisionada`, deja la orden lista para cerrar. |
| `factura.emitida` / `factura.anulada` / `pago.registrado` / `pago.anulado` (0.9.0, A36): solo de facturas con `origen_tipo = iris_orden` y a conectores con contrato ≥ 0.9.0. Factura: factura_id, numero (el del DTE, o `null` si no está certificada; aclarado en 0.14.3), empresa_id, cliente_id, servicio_id, origen_tipo, origen_id, estado, total, saldo, moneda. Pago: pago_id, factura_id, origen_tipo, origen_id, empresa_id, monto, fecha. Son eventos que Argos ya tiene (§6.3); hoy solo envía a Iris `comprobante.*` y `promesa.*` | Actualiza el cobro de la orden: «facturado», «pagado», «anulado» o, con `pago.anulado`, vuelve a «facturado». Desde 0.14.3, Iris aplica `factura.*` y `pago.*` solo al cargo cuyo `factura_id` coincide; un evento con un `factura_id` que no está ligado a ningún cargo se guarda en `integracion.entrantes` y se muestra en la orden como aviso, sin cambiar el estado de ningún cargo ni la regla del cargo vigente con la línea de instalación (§3). La factura por anular de §3 no liga el cargo: sus eventos son solo aviso. |
| `servicio.bienvenida_cambiada` (0.9.0, §12.5): servicio_id, empresa_id, cliente_id, origen_tipo (`iris_orden`, o null si la activación fue desde el panel de Argos), origen_id (o null), canales[] (`{ canal: whatsapp \| correo \| telegram, estado: pendiente \| enviada \| fallida \| omitida, motivo (o null), aviso_id (o null) }`; en WhatsApp, Argos reenvía lo que Iris le informó con `aviso.entregado` / `aviso.fallido`, §6.6, y `aviso_id` es el de Iris), reenvio (bool). Uno por cambio de estado, deduplicado por `X-Webhook-Id`. Nunca el texto ni las cuentas. Solo a conectores con contrato ≥ 0.9.0 | Actualiza el bloque «Bienvenida» de la orden. |
| `equipo.wifi_resuelto` (0.9.0, §12.4): solicitud_id, servicio_id, empresa_id, origen_tipo (`iris_orden` \| `iris_ticket` \| `iris_conversacion`), origen_id, idempotency_key, estado (`aplicado` \| `fallido`), causa (o null). Nunca el SSID ni la clave | Muestra el resultado en la orden o el ticket. |
| `servicio.titular_cambiado` (0.10.0, §3.4.4): servicio_id, empresa_id, de_cliente_id, a_cliente_id, existente (bool), posible_duplicado (bool), de_cliente_eliminado (bool), motivo_codigo (`menor_de_edad` \| `otro`; el texto queda solo en la auditoría de Argos), origen_tipo (`iris_orden` o null), origen_id (o null), tipo_titular (`persona` \| `empresa`; desde 0.14.1). Nunca el documento ni la identidad. Solo a conectores con contrato ≥ 0.10.0 | Mueve el servicio y la orden al titular en la instantánea; si `de_cliente_eliminado`, quita el cliente anterior y pasa el prospecto al nuevo. |
| `cliente.identidad_registrada` (0.10.0, §3.4.3): cliente_id, empresa_id, documento_tipo, metodo. Nunca el número ni la foto. Solo a conectores con contrato ≥ 0.10.0 | Marca la identidad del titular como registrada en la orden y borra la foto del depósito de tránsito. |
| `cliente.contactos_cambiados` (0.10.0, §3.5): cliente_id, empresa_id, contacto_id, accion (`creado` \| `editado` \| `quitado`), campos[] (solo nombres: `nombre`, `relacion`, `servicios`, `permisos`, `telefonos`, `persona_cliente_id`), persona_cliente_id (0.10.1, o null). Nunca los números. Solo a conectores con contrato ≥ 0.10.0 | Refresca la caché de contactos de ese cliente (relee `GET /clientes/:id/contactos`). |
| `servicio.config_cambiada` y `catalogo.cambiado` (0.10.0, cambios sin forma nueva): `servicio.config_cambiada` lleva además `origen_tipo`/`origen_id` cuando el cambio de plan vino de Iris (§3.4.5); `catalogo.cambiado` de plan suma `modo` y `routers` en `campos[]` | Refresca la instantánea; con `origen_tipo = iris_orden`, no avisa de nuevo al vendedor. |
| `inventario.equipo_cambiado` (0.12.0, §3.10): servicio_id, cliente_id, empresa_id, sale_unidad_id, entra_unidad_id, motivo, destino_sale, origen (`iris_orden` o null; `origen_id`). Solo a conectores con contrato ≥ 0.12.0 | Actualiza los equipos del servicio en la orden y la ficha. |
| `inventario.unidad_estado_cambiado` (0.12.0, §3.10): unidad_id, empresa_id, servicio_id (o null), de, a, motivo. Sin costos, proveedor ni RMA. Solo a conectores con contrato ≥ 0.12.0 | Actualiza el estado y el historial del equipo. |
| `red.falla_general_abierta` / `red.falla_general_cerrada` (0.13.0, A50, §3.11): incidente_id, empresa_id, alcance (`router` \| `olt` \| `pon` \| `zona`), alcance_id, servicios_afectados `{ cantidad, ids[] }` (hasta 5000 ids; el resto con `GET /incidentes-red/:incidente_id/servicios`), desde, hasta (o null). Un evento por empresa afectada (mismo `incidente_id`). Nunca nombres ni direcciones. Solo a conectores con contrato ≥ 0.13.0 | Abre o cierra el ticket general del incidente (§3.11); lee con `GET /servicios/:id` los ids que no conoce y descarta uno por uno los de otra empresa. |
| `servicio.creado`, `servicio.config_cambiada` y `catalogo.cambiado` de plan con tipo (0.15.0, propuesta aceptada con ajustes, A53, §13.1): `servicio.creado` suma `tipo_servicio` y `componentes[]` (`router_id` puede ser null); `servicio.config_cambiada` admite `tipo_servicio` en `campos[]`; `catalogo.cambiado` de plan admite `tipo_servicio` y `componentes` en `campos[]`. Si cambia el tipo de un plan o de un servicio, **cada servicio afectado** emite `servicio.config_cambiada` con `tipo_servicio` en `campos[]`: `catalogo.cambiado` solo no alcanza. Solo a conectores con contrato ≥ 0.15.0 | Refresca la instantánea releyendo `GET /servicios/:id` o `GET /planes`. |
| `servicio.corte_manual_solicitado` / `servicio.corte_manual_cancelado` (0.15.0, propuesta aceptada con ajustes, A54, §13.2): solicitud_id, version, solicitada_en, servicio_id, empresa_id, cliente_id, componente (`cable`), accion (`corte` \| `reconexion`; también en `cancelado`), causa (`sin_onu` \| `instancia_no_operativa` \| `smartolt_fallo` \| `catv_no_soportado`). Sin datos personales. Solo a conectores con contrato ≥ 0.15.0 y la marca `corte_manual` | Solicitado: abre una orden de trabajo nueva de tipo `corte_catv` o `reconexion_catv` (§13.2), o la guarda sin abrir nada si la empresa está fuera del alcance o el servicio es desconocido. Cancelado: la cancela si no se ha hecho; las cancelaciones fuera de orden se resuelven como en §13.2. |

**Confirmar un borrado (0.3.2).** Antes de quitar una instantánea por `servicio.eliminado` o `router.eliminado`, Iris puede confirmar con `GET /api/v1/servicios/:id` o `GET /api/v1/routers/:id`. Solo cuenta como borrado un `404` `application/problem+json` con `type` `urn:argosisp:problema:no_encontrado`. Un servicio de una empresa fuera del alcance del usuario técnico responde `403` (`type` `urn:argosisp:problema:empresa_no_permitida`, o el 403 genérico del ACL), nunca 404, y no es un borrado; los routers no se filtran por empresa (§2.1). Cualquier otra respuesta (5xx, timeout, 401) se reintenta con espera exponencial.

### 2.4 Más datos del servicio (0.8.0)

**Lo que Argos ya entrega (1.17.0).** `GET /api/v1/servicios/:id` y la lista (`GET /api/v1/servicios`) usan el mismo esquema `Servicio`, así que todo campo viene en los dos. Con `servicios.ver`, que el token ya tiene, devuelven:

| Campo | Forma |
| --- | --- |
| `plan_id`, `plan_nombre`, `zona_nombre`, `router_id`, `router_nombre` | Los tres nombres **ya existen**: no hay que cruzarlos con el catálogo |
| `tipo_conexion` | `pppoe` \| `ip_estatica` \| `dhcp_fija` |
| `user_ppp` | string; `null` fuera de `pppoe`. **Nunca la clave PPP.** Cambia cuando se edita el servicio y cuando cambia `tipo_conexion` (se genera o se borra); lo avisa `servicio.config_cambiada` con `credenciales` o `tipo_conexion` en `campos[]` (§2.3), sin valores |
| `ipv4` | La dirección **sin máscara**, por ejemplo `"100.64.12.34"`. Es **una sola**: si un servicio tuviera más de una IPv4, Iris solo vería una (no es un caso normal hoy; `asignacion.cambiada` sí trae `ipv4[]`). Un `ipv4s[]` no entra en la 0.8.0 |
| `prefijo_enlace_v6`, `prefijo_delegado_v6` | CIDR **con máscara**, por ejemplo `"2001:db8:10::/64"` |
| `direccion`, `referencia`, `coordenadas`, contacto en sitio | Como en 0.5.0 |
| `planta` | El objeto de A9, o `null` sin datos. Cuando viene trae **siempre las seis claves** (`caja`, `puerto`, `olt`, `slot`, `pon`, `potencia_dbm`), **todas string** (`puerto` es `"3"`, no `3`; `potencia_dbm` es un decimal con dos decimales, como `"-19.40"`) y cada una puede ser `null` |

- Sin asignación, las IP y los prefijos vienen `null`, nunca `""`.
- **Caja o nodo:** Argos no tiene caja ni nodo de planta y no los va a exponer. La única referencia es `planta`, de solo lectura, y su fuente es OZmap.
- Los cambios llegan por `servicio.datos_cambiados` (dirección, coordenadas, referencia, contacto) y `servicio.config_cambiada` (lista completa de `campos[]` en §2.3).
- **El token de Iris recibe además campos que Iris no usa:** `mac`, `ipv6_duid`, `ipv6_modo`, `local_address`, `servidor_pppoe_id`, `excepciones`, `costo_override`, `suspension_manual_motivo` y `externo`. Argos no los recorta en la 0.8.0. Ver «Lista blanca» abajo.

**Lo que Argos agrega en 0.8.0 (A27).** En el detalle y en la lista.

| Campo | Tipo | Detalle |
| --- | --- | --- |
| `precio_mensual` | string decimal con 2 decimales | El precio con el que Argos factura el servicio, **ya calculado por Argos**: `costo_override ?? precio del plan vigente`, **con IVA incluido**. Un plan temporal activo cuenta. Iris nunca lo calcula ni lo deduce de `costo_override`. |
| `moneda` | string | `"GTQ"` siempre en la 0.8.0 (los planes no tienen moneda). |

- `precio_mensual` **no** incluye adicionales, descuentos de lealtad o bonificaciones, mora, prorrateo ni cargos únicos. No es el total de la factura.
- **Qué puede decir Iris con él:** «su plan cuesta Q250.00 al mes». **Nunca** «su factura es de…». El total facturado sale de `GET /clientes/:id/estado-cuenta`.
- **Cómo se entera Iris de un cambio de precio** (hoy no hay ningún evento; los dos son nuevos en la 0.8.0 y aditivos, §2.3):
    - cambió el precio de ese servicio (`costo_override`, o un plan temporal con otro precio) → `servicio.config_cambiada` con `precio` en `campos[]`;
    - cambió el precio del plan → `catalogo.cambiado` (`catalogo: "plan"`) con `precio` en `campos[]`; Iris relee los servicios de ese plan con `GET /servicios?plan_id=`.

```json
{
  "id": 4410,
  "cliente_id": 1201,
  "empresa_id": 3,
  "estado": "activo",
  "plan_id": 12,
  "precio_mensual": "250.00",
  "moneda": "GTQ",
  "router_id": 5,
  "router_nombre": "Nodo Centro",
  "tipo_conexion": "pppoe",
  "user_ppp": "cli1201-4410",
  "ipv4": "100.64.12.34",
  "prefijo_enlace_v6": null,
  "prefijo_delegado_v6": null,
  "direccion": "3a calle 4-56 zona 1",
  "referencia": "Portón negro, frente a la tienda",
  "coordenadas": "14.634900,-90.506900",
  "planta": { "caja": "C-014", "puerto": "3", "olt": null, "slot": null, "pon": null, "potencia_dbm": "-19.40" }
}
```

El ejemplo solo muestra los campos de esta sección; el resto del objeto no cambia.

**Datos sensibles (clasificación de Argos).** Son sensibles la IP y los prefijos, las coordenadas y la dirección (datos personales) y `user_ppp`: Iris les pone permiso propio. El precio y el nombre del plan no son sensibles.

**Lista blanca (requisito del lado de Iris, pedido por Argos).** Iris guarda en su instantánea **solo** los campos de la tabla siguiente, con una lista escrita en el código. Lo que no está en la lista no se guarda, no se registra en logs ni en auditoría y no sale por la API de Iris, aunque Argos lo mande. Un campo nuevo de Argos no entra solo: hay que agregarlo a la lista. Si más adelante Iris prefiere que Argos oculte esos campos a su token (como hace con `nit_cui`), es otro pedido, fuera de la 0.8.0.

**Qué entra a la instantánea de Iris** (`integracion.argos_servicios`, §2.2):

| Dato | En la instantánea | Quién lo ve en Iris |
| --- | --- | --- |
| `plan_id`, `plan_nombre`, `zona_nombre`, `estado`, `precio_mensual`, `moneda` | sí | Cualquiera con `clientes.ver` |
| `codigo` (0.10.0, A45) | sí | Cualquiera con `clientes.ver`, y el asistente de IA |
| `nombre` (0.10.0, A45) | sí | Cualquiera con `clientes.ver`. **Nunca** al proveedor de IA; en los logs solo aparece el `codigo` |
| `router_id`, `router_nombre`, `tipo_conexion`, `ipv4`, `prefijo_enlace_v6`, `prefijo_delegado_v6`, `user_ppp`, `planta` | sí | Solo con `servicios.ver_red` |
| `direccion`, `referencia`, `coordenadas`, contacto en sitio | sí (ya estaban) | Solo con `servicios.ver_ubicacion` |
| `costo_override` | **no** | Nadie: Iris usa `precio_mensual` y no guarda el dato del que sale |
| `mac`, `ipv6_duid`, `ipv6_modo`, `local_address`, `servidor_pppoe_id`, `excepciones`, `suspension_manual_motivo`, `externo` | **no** | Nadie: quedan fuera de la lista blanca |
| Clave PPP | **no** (Argos nunca la entrega) | Nadie |

- La instantánea se refresca como en §2.2: por `servicio.config_cambiada` y `servicio.datos_cambiados`, por `catalogo.cambiado` de un plan con `precio`, y al abrir la ficha si tiene más de 24 h. Con un Argos anterior a la 0.8.0 no hay aviso de cambio de precio (y tampoco hay `precio_mensual`).
- Sin el permiso, la API de Iris **no devuelve** el campo. No lo manda vacío ni lo oculta la pantalla: el filtro es del servidor.

**Permisos nuevos en Iris (decididos por el PM, §10.2; pendientes de implementar en Iris).** Hoy Iris no tiene permisos `servicios.*`: todo lo del servicio se ve con `clientes.ver`, que tienen todos los roles base de persona.

| Permiso | Qué abre | Roles base | Roles que no |
| --- | --- | --- | --- |
| `servicios.ver_red` | IP, prefijos, `user_ppp`, `tipo_conexion`, router y `planta` del servicio | `admin`, `supervisor`, `soporte` y `tecnico` (este solo en los servicios de sus órdenes, con el alcance de `ordenes.ver_propias`) | `despacho`, `ventas`, `caja`, `lectura` |
| `servicios.ver_ubicacion` | Dirección, `referencia`, coordenadas y contacto en sitio del servicio | `admin`, `supervisor`, `despacho`, `soporte`, `ventas` y `tecnico` (solo en sus órdenes) | `caja`, `lectura` |

- Las órdenes de trabajo guardan su propia copia de la dirección y las coordenadas para que el técnico llegue. Esa copia se sigue viendo con los permisos de la orden y no cambia.
- Es un **cambio de comportamiento dentro de Iris**: `caja` y `lectura` dejan de ver la dirección y las coordenadas del servicio, que hoy ven con `clientes.ver`. Lo decidió el PM (§10.2, puntos 4 y 5).
- Los usuarios de tipo integración (`integracion_argos`, `integracion_horus`) no reciben ninguno de los dos.
- Cada lectura de estos datos por una persona se audita en Iris como lectura de datos personales (§0), sin el valor.

**Qué recibe el asistente de IA (decidido por el PM, §10.2).** Amplía `conversaciones.md` §17.9 sin relajarla:

- **Nunca viajan al proveedor de IA:** IP, prefijos, `user_ppp`, coordenadas, la dirección, el texto libre de `referencia`, el contacto en sitio, `planta` ni el nombre del router. El asistente no tiene `servicios.ver_red` ni `servicios.ver_ubicacion`.
- **Sí viajan:** el nombre del plan, `precio_mensual` con `moneda` (el asistente lo dice sin verificación fuerte, solo a quien escribe desde un número registrado; §10.2, punto 7), el estado del servicio y una **etiqueta corta** que arma Iris para distinguir los servicios de un mismo cliente.
- **Etiqueta corta:** desde 0.10.0, `<codigo> · <plan> · <zona>` (`codigo` de A45, §3.7; con un Argos que no lo tiene, sigue `Servicio <n> · <plan> · <zona>`, con `<n>` el orden del servicio dentro del cliente). `<zona>` es el nombre de la zona del catálogo. No lleva calle, número de casa, coordenadas, el `nombre` que puso el cliente ni otros códigos internos. Si dos servicios quedan con la misma etiqueta, el asistente no adivina: pasa a una persona.
- Los ids de servicio siguen sin ir en los argumentos de las herramientas: los pone el servidor (§17.9).

**Cómo se distinguen los servicios de un mismo cliente en el panel.** Cada servicio va en una línea: código del servicio, plan, `precio_mensual` y estado (con icono, texto y color). La dirección corta se ve solo con `servicios.ver_ubicacion`, y la IP y el router solo con `servicios.ver_red`. Sin esos permisos se ve la misma etiqueta corta que usa el asistente. Vale para la ficha del cliente, el selector de servicio de un ticket, una orden o una boleta, y el panel lateral de la conversación.

### 2.5 Primera sincronización y estado de la conexión (0.8.0)

**Estado: acordada con Argos y decidida por el PM; implementada en Argos (PR #249, sale en 1.18.0); implementado en Iris (0.33.0, que informa contrato 0.8.1).** Hoy Iris carga los catálogos (zonas, planes, routers) con un job y trae clientes y servicios a demanda, al abrir cada ficha. Con la 0.8.0, al **activar en Iris** un conector recién canjeado (§1.5), Iris hace sola una primera sincronización completa. Antes va siempre la primera llamada autenticada (`PUT /integraciones/iris/contrato`), que confirma el canje. Argos no necesita enterarse antes: se entera por el primer `PUT …/sincronizacion` con `en_curso` (abajo).

**Qué trae y en qué orden.** Las lecturas ya existen y van con el token de §1.1. La única ruta nueva es la que informa el avance.

| Paso | Recurso (`recurso`) | Lectura en Argos | Forma de la respuesta |
| --- | --- | --- | --- |
| 1 | `empresas` | `empresas[]` y `alcance_global` de la respuesta del canje (§1.3) | — |
| 2 | `zonas` | `GET /api/v1/zonas` | Arreglo, **sin paginar** |
| 3 | `planes` | `GET /api/v1/planes` | `{ data, nextCursor }` |
| 4 | `routers` | `GET /api/v1/routers` | `{ data, nextCursor }` |
| 5 | `cuentas_bancarias` | `GET /api/v1/cuentas-bancarias`, sin `activa` (trae activas e inactivas; §7.8) | Arreglo, **sin paginar**; `limit` y `cursor` no existen y se ignoran |
| 6 | `bancos` | `GET /api/v1/bancos` (solo bancos activos; §7.8) | Arreglo |
| 7 | `clientes` | `GET /api/v1/clientes`, sin `q` | `{ data, nextCursor }` |
| 8 | `servicios` | `GET /api/v1/servicios`, sin `q` ni `cliente_id` | `{ data, nextCursor }` |

- **Cabeceras de Iris en el paso 5 y 6.** Hoy `GET /cuentas-bancarias` y `GET /bancos` **exigen** `X-Iris-Actor` y `X-Iris-Conversacion` con el token de Iris: sin ellas responden `400 cabecera_iris_requerida`. La primera sincronización no tiene conversación. **Cambio de la 0.8.0 (A28):** en esas dos lecturas las cabeceras pasan a ser **opcionales**: si vienen, se validan y van a la auditoría; si faltan, no hay `400`. Iris nunca inventa un actor ni una conversación para el sistema. Con un Argos anterior a la 0.8.0, Iris no hace los pasos 5 y 6 en la primera carga: los deja `con_errores` con el motivo «Argos anterior a 0.8.0» y las cuentas se siguen leyendo desde una conversación, como hoy. `GET /cuentas-bancarias/:id` ya no las exige.
- **Listas completas.** `GET /clientes` y `GET /servicios` se recorren enteras con `cursor`. El orden es por `id` ascendente y el resultado se filtra por el alcance de empresas del token. `limit` máximo: **200** (por defecto, 50).
- **Filtros.** Existen `GET /servicios?estado=` (un solo estado) y `GET /clientes?estado_servicio=` (clientes con al menos un servicio en ese estado). No hay filtro por fecha de cambio. Para reanudar alcanza el cursor por id. Como el PM decidió cargar solo los activos (§10.2, punto 9), Argos agrega `excluir_retirados=true` a las dos listas (implementado en la PR #249, sale en 1.18.0).
- **Ritmo (lo pide Argos).** Una petición a la vez por recurso, `limit=200` y **como máximo 5 peticiones por segundo en total**. La consulta de servicios lleva subconsultas por fila. Hoy `GET /clientes` y `GET /servicios` no tienen límite de peticiones ni responden `429`, y Argos no lo agrega en la 0.8.0; aun así Iris respeta `429` con `Retry-After` si algún día aparece.
- **Progreso.** Iris guarda por conector y por recurso: estado (`pendiente`, `en_curso`, `completa`, `con_errores`), cuántos lleva, el último cursor, cuándo empezó y terminó, y el último error. La sincronización entera tiene un `sincronizacion_id` (UUID v7 generado por Iris) y un estado `en_curso`, `completa` o `con_errores`. La pantalla de estado lo muestra mientras corre.
- **Reanudable.** Si se corta (reinicio, Argos caído, token revocado), sigue desde el último cursor guardado de ese recurso y no vuelve a empezar. Es un job único por conector: lanzarlo dos veces no lo duplica.
- **Idempotente.** Cada fila se escribe como instantánea con su `sincronizado_en`. Un webhook que llega durante la carga se aplica normal. Entre la carga y un webhook gana la lectura más reciente de Argos. Una lista nunca revive una lápida de `servicio.eliminado` o `router.eliminado` (§2.3).
- **Sin escrituras.** La primera sincronización solo lee de Argos. No crea clientes, tickets, órdenes ni usuarios, y **no trae usuarios de Argos ni vincula a nadie** (§1.5, S8).
- **Lista blanca.** Cada fila de servicio se guarda solo con los campos de §2.4.
- **Alcance (decisión del PM, §10.2, punto 9).** La primera carga trae solo los clientes que tienen un servicio no retirado, y sus servicios, con `excluir_retirados=true` en `GET /clientes` y `GET /servicios`. Lo que no se cargue sigue llegando a demanda, como hoy. Con un Argos sin el filtro, Iris recorre las listas completas.
- **Qué puede leer en bloque.** No hay un mecanismo nuevo: el token ya está acotado a las empresas que el admin vio y aprobó (S5, S7), y la primera carga solo lee lo que Iris ya podía leer a demanda. Argos lo dice en su pantalla de aprobación (§1.4) y en la del enlace: «Iris copiará los clientes, los servicios y las cuentas bancarias de estas empresas».
- **Sin Argos.** Iris sigue funcionando con lo que tenga (§0). Una sincronización a medias no bloquea nada, y la pantalla dice qué falta.

**Informar el avance a Argos (ruta nueva de la 0.8.0, A28).** `PUT /api/v1/integraciones/iris/sincronizacion`. Hoy no existe.

- **Credencial.** Solo el `atk_` de Iris, con el permiso de ruta `sso.emitir` y el mismo control del token del canje que `PUT /integraciones/iris/contrato`. Cualquier otra credencial responde `403`. Funciona con el conector apagado. No obliga a migrar tokens.
- **Cuerpo:**

    ```json
    {
      "sincronizacion_id": "<uuid v7 de Iris>",
      "estado": "en_curso",
      "iniciada_en": "2026-11-18T21:20:00.000Z",
      "actualizada_en": "2026-11-18T21:24:10.000Z",
      "etapas": [
        {
          "recurso": "clientes",
          "estado": "en_curso",
          "procesados": 1200,
          "total": null,
          "error": null,
          "terminada_en": null
        }
      ]
    }
    ```

    | Campo | Valores |
    | --- | --- |
    | `sincronizacion_id` | UUID v7 que genera Iris, uno por sincronización |
    | `estado` | `en_curso` \| `completa` \| `con_errores` |
    | `iniciada_en`, `actualizada_en` | Instantes RFC 3339 en UTC |
    | `etapas[].recurso` | `empresas` \| `zonas` \| `planes` \| `routers` \| `cuentas_bancarias` \| `bancos` \| `clientes` \| `servicios` |
    | `etapas[].estado` | `pendiente` \| `en_curso` \| `completa` \| `con_errores` |
    | `etapas[].procesados` | Cuántas filas lleva |
    | `etapas[].total` | Número o `null` si Iris no lo sabe (las listas por cursor no lo dicen) |
    | `etapas[].error` | Texto de hasta 300 caracteres o `null`. **Sin datos de clientes** |
    | `etapas[].terminada_en` | Instante o `null` |

- **Respuesta:** `200 { sincronizacion_id, estado, actualizada_en }`.
- **Idempotente.** La misma `sincronizacion_id` con el mismo `actualizada_en` responde `200` con lo guardado.
- **`409 sincronizacion_vieja`.** Si `actualizada_en` es menor que el guardado para esa `sincronizacion_id`, o si la `sincronizacion_id` es más vieja que la guardada (se comparan como UUID v7). Es de negocio y no se reintenta: Iris lo descarta.
- **Cuándo lo manda Iris.** Solo al cambiar de etapa o de estado, **no por página**, para no llenar la bitácora de Argos. Un `5xx` o un timeout se reintentan; un informe atrasado que ya es viejo se descarta.
- **Qué guarda Argos.** Solo el último informe, en su conector. Es estado de Iris, no de Argos. Lo audita como `iris.informar_sincronizacion`. `GET /integraciones/iris` lo devuelve como `sincronizacion` (o `null`, que su panel muestra como «no informado»).
- Con un Argos anterior a la 0.8.0 la ruta no existe: Iris no la llama (§4.1).

**(d) Pantalla de estado, en los dos paneles.** Cada lado muestra lo que **él** sabe. Ninguno inventa el estado del otro.

| Qué muestra | Panel de Iris (Administración → Integraciones → Argos) | Panel de Argos (Integraciones → Iris) |
| --- | --- | --- |
| Conexión | Conector activo o apagado, desde cuándo y quién lo activó | Lo que `GET /integraciones/iris` ya devuelve: `estado_canje`, `canje_vence_en`, `desconectado_en`, `motivo_desconexion`, `reconexion_pendiente_desde`, `reconexion_vence_en` y `requiere_reconectar` |
| Versión de contrato de cada lado | La propia y la de Argos (`contrato` y `version_argos` del canje y de `GET /integraciones/iris/estado`) | La propia y la que Iris informó con `PUT /integraciones/iris/contrato` |
| Qué se sincronizó y cuándo | Por recurso: cuántos, último `sincronizado_en` y estado de la primera carga | El último informe de Iris (`sincronizacion`), o «no informado» |
| Qué falló | Último error por recurso, webhooks entrantes rechazados y pendientes hacia Argos (`integracion.pendientes`) | Webhooks a Iris pendientes y fallidos, el último error y `ultimo_enviado_en` (`GET /integraciones/iris/envios/resumen`) |
| Empresas | Las del alcance y `alcance_global` | Las del usuario técnico Iris y `alcance_global` |
| Certificado de Argos (§1.5) | Emisor, sujeto y huella del certificado de la API de Argos, y el aviso si cambió. Solo informa | No aplica: Argos no muestra huellas |

- Con versiones de contrato distintas, las dos pantallas dicen qué funciones quedan apagadas por eso (§4.1), no solo los números de versión.
- Ninguna de las dos pantallas muestra tokens ni secretos (§1.5, S5): solo si existen y cuándo se generaron.
- **Qué tiene ya el panel de Argos (1.17.0):** el estado del canje y de la reconexión, `requiere_reconectar` y el resumen de webhooks enviados a Iris. **Qué le falta (0.8.0):** la versión de contrato de cada lado con las funciones apagadas, la sincronización informada por Iris, y las empresas del alcance con `alcance_global` en la página de configuración (hoy solo salen en la pantalla de aprobación).

## 3. Iris escribe en Argos (al cerrar órdenes y ganar prospectos)

Todas las llamadas llevan `Idempotency-Key`. Cada una queda en `integracion.pendientes` hasta su `2xx`. Un `4xx` de negocio no se reintenta (salvo el reenvío sin la línea de instalación ante `422 capacidad_no_disponible`, como cobro nuevo; abajo): la orden queda «cerrada con pendiente en Argos» y se avisa al supervisor con el `detail` del error.

| Hecho en Iris | Llamada a Argos | Idempotency-Key |
| --- | --- | --- |
| Instalación cerrada como exitosa | `POST /servicios/:id/activar-instalacion` (pedido A1), sin `version` (§3.1), con `planta` si la orden la trae (pedido A9) y, por cada equipo con serie, `POST /servicios/:id/equipos` con `{ unidad_id }` o `{ serie }` y, desde 0.5.0, `tecnico` (§3.3): la unidad sale de la bodega del técnico | `iris:instalacion:<orden_id>` · `iris:comodato:<orden_id>:<serie>` |
| Consumibles usados en una orden | `POST /inventario/consumos` con `{ tecnico, lineas[], servicio_id, origen_tipo: 'iris_orden', origen_id }` (0.5.0; `bodega_id` explícita sigue valiendo en todo 0.x y manda sobre `tecnico`). Solo productos `consumible_gasto` o `consumible_cobro`; una línea `comodato` responde `422 producto_es_comodato` (los equipos van por `POST /servicios/:id/equipos`). Detalle en §3.3. Desde 0.14.0, uno por actor y bodega (§3.9) | `iris:consumo:<orden_id>` · desde 0.14.0, con un Argos que informó contrato ≥ 0.14.0: `iris:consumo:<orden_id>:<usuario_id>:<bodega_id>` |
| Técnico recibe una orden de traslado (0.5.0) | `POST /inventario/traslados/:id/recibir`, cuerpo y errores en §3.3 | `iris:recepcion:<traslado_id>` |
| Retiro con equipo recuperado | `POST /servicios/:id/equipos/:unidad_id/devolver` con `{ tecnico | bodega_id, condicion }` (0.5.0: con `tecnico`, a la bodega activa del técnico en la empresa del servicio) | `iris:devolucion:<orden_id>:<unidad_id>` |
| Retiro sin equipo (perdido) | Hasta 0.11.0: `POST /servicios/:id/equipos/:unidad_id/perdida`. **Desde 0.12.0**: «no se pudo recuperar», `POST /servicios/:id/equipos/:unidad_id/no-recuperado` (§3.10); la pérdida la declara una persona en Argos. **Desde 0.14.0**, sale al aplicarse la operación `equipo.no_recuperado` del lote, no al cerrar | `iris:perdida:<orden_id>:<unidad_id>` · `iris:no_recuperado:<orden_id>:<unidad_id>` · desde 0.14.0, con un Argos que informó contrato ≥ 0.14.0: `iris:no_recuperado:<orden_id>:<unidad_id>:<intento_id>` |
| Retiro del servicio pedido por el cliente | `POST /servicios/:id/retirar` | `iris:retiro:<orden_id>` |
| Cambio de equipo | Hasta 0.11.0: una devolución y una asignación, en ese orden. **Desde 0.12.0**: `POST /servicios/:id/equipos/cambiar` en una sola transacción (§3.10). **Desde 0.14.0**, sale al aplicarse la operación `equipo.cambiar` del lote, no al cerrar | Las dos de arriba · `iris:cambio:<orden_id>:<sale_unidad_id>` · desde 0.14.0, con un Argos que informó contrato ≥ 0.14.0: `iris:cambio:<orden_id>:<sale_unidad_id>:<operacion_id>` |
| Prospecto ganado | `POST /clientes` y luego `POST /servicios` con `estado_inicial: pendiente_instalacion` (pedido A1), según §3.2 | `iris:cliente:<prospecto_id>` · `iris:servicio:<prospecto_id>` |
| Visita o instalación cobrada | `POST /facturas/cargo-unico` (ya existe). Desde 0.9.0, con el cuerpo real de Argos (fila siguiente): una o más líneas con el `concepto_id` y el `precio_unitario` que fija la plantilla de Iris | `iris:cargo:<orden_id>` |
| Técnico o agente pulsa «Activar servicio» en una instalación (0.9.0, §12) | `POST /servicios/:id/aprovisionar` | `iris:aprovisionar:<orden_id>:<intento>` |
| Técnico confirma que firma la misma persona de la venta (0.10.0, §3.4.3) | `POST /clientes/:id/identidad` (multipart) | `iris:identidad:<orden_id>:<toma>` |
| Técnico cambia el titular antes de activar (0.10.0, §3.4.4) | `POST /servicios/:id/cambiar-titular` (multipart, con la identidad) | `iris:titular:<orden_id>:<cambio_id>` |
| Técnico completa los datos del titular o del lugar (0.10.0, §3.4.6) | `POST /servicios/:id/completar-datos` | `iris:datos:<orden_id>:<n>` |
| Técnico cambia el plan antes de activar (0.10.0, §3.4.5) | `POST /servicios/:id/cambiar-plan` | `iris:plan:<orden_id>:<cambio_id>` |
| Cambiar el tipo de un servicio activo al cerrar la orden de cambio (0.15.1, §13.4) | `POST /servicios/:id/cambiar-tipo` | `iris:tipo:<orden_id>:<cambio_id>` |
| Técnico reserva la IP en sitio (0.10.0, §3.4.1) | `POST /servicios/:id/reservar-ip` | `iris:reservar_ip:<orden_id>:<router_id>` |
| (Argos → Iris) Una persona de Argos registra un prospecto (0.10.0, A44, §3.6) | `POST <iris>/api/v1/integraciones/argosisp/prospectos` | `argos:prospecto:<uuid>` |
| Iris crea un servicio al ganar (§3.2), desde 0.10.0 con `nombre` opcional (A45, §3.7) | `POST /servicios` con `nombre?`; el `codigo` lo pone Argos y vuelve en la respuesta | `iris:servicio:<prospecto_id>` |
| Un agente de Iris vincula un contacto con otro cliente (0.10.1, A46, §3.8) | `POST /clientes/:id/contactos` o `PUT /contactos/:id` con `persona_cliente_id`, solo si el número es el teléfono verificado de ese cliente | `iris:contacto:<cliente_id>:<contacto_iris_id>` · `iris:contacto:<contacto_id>:<version>` |
| Un miembro de la cuadrilla usa material o equipos en una orden (0.11.0, A47, §3.9) | `POST /inventario/consumos` con `tecnico { id_externo }`, `bodega_id` elegido en la app (por defecto la de la cuadrilla) y `X-Iris-Actor` del técnico que lo usó; `POST /servicios/:id/equipos` igual pero **sin `bodega_id`** (vale la bodega donde está la unidad) | Las de §3 (`iris:consumo:<orden_id>`, `iris:comodato:<orden_id>:<serie>`); desde 0.14.0, el consumo por actor y bodega (`iris:consumo:<orden_id>:<usuario_id>:<bodega_id>`) |
| Un agente o el asistente registra una gestión de cobro con un cliente confirmado (0.13.0, A51, §3.12) | `POST /clientes/:id/gestiones-cobro` con `X-Iris-Actor` (y `X-Iris-Contacto` si habló un contacto) | `iris:gestion:<gestion_id>` |
| Iris lee los servicios afectados de una falla general con más de 5000 (0.13.0, A50, §3.11) | `GET /incidentes-red/:incidente_id/servicios?empresa_id=&cursor=&limit=` (lectura, sin `X-Iris-Actor`, `limit` ≤ 1000) | — |
| Cobro de la instalación registrado por un agente o por el formulario de la orden (0.9.0, A36) | `POST /facturas/cargo-unico` con el cuerpo real de Argos: `{ servicio_id, cliente_id?, vence_en?, lineas: [{ concepto_id, precio_unitario, descripcion?, cantidad?, descuentos? }], origen_tipo: "iris_orden", origen_id }`, con `X-Iris-Actor` (§7.1: quien lo registró). No hay `concepto_id`, `monto` ni `descripcion` en la raíz. `concepto_id` es de `GET /conceptos-cargo` (por defecto el concepto con `codigo_sistema: "instalacion"`, sembrado por empresa, §4.3); los conceptos **no tienen precio**: `precio_unitario` (string decimal, bruto con IVA, **> 0**, obligatorio) lo pone Iris, de su plantilla o del formulario de la orden. **Iris nunca manda un cargo en Q0** (exigiría `motivo_sin_costo_id`, `motivo_sin_costo_texto` y `facturas.cargo_sin_costo`, que su token no tiene). Responde `201 CargoUnicoEmitido` sin cambios: Iris lee `factura.id`, `numero`, `estado`, `total` y `moneda` (`documento = bonificacion` es una respuesta posible de Argos, pero Iris nunca la pide). Sale en el momento (no espera al cierre), por la cola de pendientes si Argos falla. El precio que trae la plantilla o el formulario se manda tal cual; cambiarlo exige en Iris el permiso aparte `ordenes.cobro_editar_monto`, con motivo auditado | `iris:cargo:<orden_id>:<cargo_id>` |

**`tecnico` (0.5.0):** ver §3.3.

**0.9.0:** el mensual empieza cuando el servicio queda activo; la instalación se cobra solo como factura única (fila anterior). Cada cobro de la orden tiene su `cargo_id` de Iris y su clave `iris:cargo:<orden_id>:<cargo_id>`; si la orden ya tiene un cargo de A36 vigente con la línea de instalación (encolado, enviado o emitido; no `fallido` ni anulado; un cargo en «cobro por confirmar» cuenta como vigente hasta que el supervisor concilie), el cargo del cierre (`iris:cargo:<orden_id>`) sale **sin** la línea de instalación y con las demás líneas; si no queda ninguna línea, no sale. La decisión y el cuerpo se fijan al encolar el cargo y no cambian en los reintentos (§3.1). Iris toma esa decisión con la orden bloqueada (una sola transacción por orden a la vez), así que dos cargos con la línea de instalación nunca quedan vigentes a la vez. Después de un `factura.anulada`, un cobro nuevo lleva **otro** `cargo_id` (nunca se reusa la clave). Si la orden tiene una activación `lista` o pendiente para un agente (§12), el cierre **no** manda `activar-instalacion` ni el `POST /servicios/:id/equipos` del equipo aprovisionado: Argos ya activó el servicio y asignó ese equipo en comodato. Un `409 servicio_no_pendiente` de una orden con activación `lista` es conciliado, no error. Desde 0.14.3, la línea con el concepto de sistema `instalacion` de la empresa (§4.3) sale solo con `cobro_equipo: true`; con `false`, ni el registro de A36 ni el cierre la mandan, y las demás líneas (visita u otros conceptos; los materiales `consumible_cobro` los cobra Argos con el consumo, §3.3) salen igual. Si un cargo pendiente recibe `422 capacidad_no_disponible`, Iris lo marca `fallido` con esa causa; si el cargo rechazado, del cierre o de A36, tenía otras líneas, las reenvía sin la de instalación como un cobro nuevo, con un `cargo_id` nuevo (UUID generado por Iris) y su clave `iris:cargo:<orden_id>:<cargo_id>`, y avisa en la orden. Nunca reusa una clave con otro cuerpo. Un cargo marcado `fallido` por `capacidad_no_disponible` queda **terminal**: no se reintenta, ni solo ni a mano, con su clave (Argos no la guarda en un `4xx`, §3.1, y el reintento crearía la factura completa); el cargo original del cierre, una vez reenviado sin la línea de instalación, queda como reemplazado. Si ese `422` llega en el reintento de un cargo cuyo envío anterior quedó sin respuesta (timeout o `5xx`), Iris **no** manda el cobro nuevo de las demás líneas hasta conciliar: espera a que un supervisor confirme en Argos si existe o no una factura de esa orden con esas líneas (un `factura.emitida` con el mismo `origen_id` no basta: puede ser del consumo o de otro cargo de la orden, e Iris nunca concilia solo por `origen_id`); mientras tanto, la orden muestra «cobro por confirmar». Si el supervisor confirma que la factura existe, indica su «Factura #» del panel de Argos (el `factura_id`) o la serie y el número del DTE; Iris lo valida leyendo Argos con `GET /api/v1/facturas/origen-iris` (abajo): misma empresa, mismo servicio, `origen_tipo: iris_orden`, `origen_id` de la orden, líneas iguales al cuerpo del cargo (mismo `concepto_id`, `precio_unitario` y `cantidad`, sin líneas de más), `estado` distinto de `anulada`, `emitida_en` igual o posterior al primer envío de ese cargo menos 5 minutos (tolerancia de reloj), y un `factura_id` que no esté ligado a otro cargo de Iris (único en Iris). Si algo no coincide, Iris rechaza la conciliación y el cargo sigue en «cobro por confirmar»; nunca lo pasa a `fallido` por eso. Conciliar («existe» o «no existe») es una acción con `version` sobre el cargo, tomada con la orden bloqueada: solo se aplica si el cargo sigue en «cobro por confirmar»; si no, `409`. Las validaciones de la lectura se repiten contra el cargo bloqueado, y la unicidad de `factura_id` la garantiza una restricción única en Iris. Si coincide, Iris marca el cargo como `emitido` con ese `factura_id` (sigue vigente y recibe sus `factura.*`) y no manda el cobro de reemplazo. Si confirma que no existe, el cargo queda `fallido` y sale el cobro de reemplazo. El cobro de reemplazo nunca lleva la línea de instalación; si aun así recibe `422 capacidad_no_disponible`, queda `fallido` y se avisa al supervisor, sin otro reenvío. Cobrar la instalación más adelante es un cobro nuevo de A36, con otro `cargo_id` y solo la línea de instalación, y solo si la orden no tiene otro cargo vigente con esa línea. Iris cobra la instalación solo con el concepto de `codigo_sistema: "instalacion"`. Desde 0.14.3, cada elemento de `GET /conceptos-cargo` suma `codigo_sistema` (string o `null`): el concepto sembrado de instalación trae `codigo_sistema: "instalacion"` en cada empresa, y Argos no permite cambiarlo ni dárselo a otro concepto. Iris lo identifica solo por ese campo, nunca por el nombre.

**Lectura de una factura de origen Iris (0.14.3).** `GET /api/v1/facturas/origen-iris?empresa_id=<e>&origen_id=<orden_id>&numero=<n>` (o `&serie=<s>&numero_dte=<d>` para una factura certificada; `serie` y `numero_dte` van juntos y son excluyentes con `numero`), permiso `facturas.leer_origen_iris`. `empresa_id` y `origen_id` (id de la orden en Iris) son obligatorios en las dos formas; parámetros que faltan o se mezclan: `422 validacion`. `numero` es el que el panel de Argos muestra como «Factura #n», igual al `factura_id` (entero en texto); la búsqueda por el papel FEL es `serie` + `numero_dte`. En la respuesta, `numero` es el del DTE (o `null` si no está certificada), como en `factura.emitida`. Los cargos únicos emitidos antes de que Argos pasara a la 0.14.3 pueden traer `concepto_id: null` en sus líneas: no coinciden con el cuerpo del cargo e Iris no los concilia como «existe». Puede pasar con un cargo cuyo primer envío fue anterior a la 0.14.3 y cuyo reintento recibió `422 capacidad_no_disponible` (solo si Argos ya no tiene la clave del primer envío; si la tiene, responde el `201` guardado): Iris muestra «Factura anterior a la 0.14.3: no se puede validar», y el cargo sigue en «cobro por confirmar» y cuenta como vigente. Iris la acepta como factura por anular solo si cumple todas las validaciones de «existe» salvo el `concepto_id`: mismo servicio, mismas líneas por `precio_unitario` y `cantidad`, sin líneas de más, `estado` distinto de `anulada`, `emitida_en` igual o posterior al primer envío de ese cargo menos 5 minutos y un `factura_id` que no esté ligado a otro cargo de Iris ni guardado como factura por anular de otro cargo (con la orden bloqueada); si algo no coincide, Iris rechaza la conciliación y no la guarda. Si la acepta, Iris guarda su `factura_id` en el cargo como factura por anular y no habilita «no existe» hasta que una nueva lectura de `GET /facturas/origen-iris` con ese `factura_id` responda `estado: anulada`; una lectura con otro estado, o un error de lectura, deja el cargo en «cobro por confirmar». La factura por anular no liga el cargo: no es su `factura_id`, no ocupa la restricción única y sus `factura.*` y `pago.*` se guardan solo como aviso en la orden, sin cambiar el estado del cargo. Mientras no esté anulada en Argos, el supervisor puede quitarla con motivo auditado (`integraciones.confirmar_cobro`): es una acción con `version` sobre el cargo, tomada con la orden bloqueada, que solo se aplica si el cargo sigue en «cobro por confirmar» (si no, `409`) y si una lectura de `GET /facturas/origen-iris` hecha en la misma acción responde un estado distinto de `anulada` (si responde `anulada`, Iris no la quita y habilita «no existe»). Quitada, el cargo sigue en «cobro por confirmar» sin factura por anular, y «no existe» sigue bloqueado hasta que se indique y se lea otra factura por anular válida. Iris no espera el webhook `factura.anulada`: decide con la lectura, hecha con la orden bloqueada en la misma acción de «no existe». Si su `saldo` es menor que su `total`, la factura tiene pagos: Iris lo indica en la pantalla de «no existe» y, además de la nueva lectura con `estado: anulada`, exige que el supervisor confirme con motivo auditado que el pago se anuló o pasó a abono en Argos. La lectura devuelve también las facturas anuladas, con `estado: anulada` y el mismo cuerpo: una factura anulada nunca responde `404`. Un `numero` que no es un entero positivo responde `422 validacion`; para Iris, igual que el `404 factura_no_encontrada`, solo rechaza la conciliación. Iris toma de la respuesta el `factura_id` y lo guarda como identificador del cargo; nunca compara el `numero` de la respuesta con el de la consulta. La pantalla pide por separado «Factura # (panel de Argos)», solo dígitos, o «Serie y número DTE». Responde un objeto `{ factura_id, numero, empresa_id, servicio_id, cliente_id, origen_tipo, origen_id, estado, lineas: [{ concepto_id, precio_unitario, cantidad }], total, saldo, moneda, emitida_en, dte: { serie, numero } | null }` (`saldo` string decimal, como `total`), sin el detalle de los pagos, NIT, nombre ni dirección. Si no existe, si no es `origen_tipo = iris_orden`, si su `origen_id` no es el pedido o si su empresa está fuera del token (también un `empresa_id` fuera del token, no `403`): el mismo `404 factura_no_encontrada`. Máximo 60 lecturas por minuto por token (`429 demasiadas_peticiones` con `Retry-After`). `X-Iris-Actor: usuario:<id>` obligatoria (si falta o está mal formada: `400 cabecera_iris_requerida`); con `asistente_ia` o `sistema`, `403 actor_no_permitido` (§7.1); sin `X-Iris-Conversacion` ni `X-Iris-Contacto`. Argos audita cada lectura con ese actor y canal `api`. Un `404` con `type` `urn:argosisp:problema:factura_no_encontrada` solo dice que ese número no es una factura de esa orden en esa empresa: Iris rechaza la conciliación y el cargo sigue en «cobro por confirmar»; una lectura nunca pasa el cargo a `fallido` ni dispara el cobro de reemplazo. Cualquier otro `404`, un `429`, un `5xx` o un timeout es un error de lectura: Iris lo muestra con «Reintentar» y el cargo no cambia. Resolver como «no existe» es una acción aparte del supervisor (`integraciones.confirmar_cobro`, con motivo auditado), después de comprobarlo en Argos, con cualquier versión de Argos. Con un Argos < 0.14.3 (sin la ruta), Iris no concilia como «existe». Iris la usa solo para conciliar un «cobro por confirmar», con el permiso `integraciones.confirmar_cobro` de Iris.

Al cerrar órdenes, Iris nunca registra pagos ni asientos. Solo pide a Argos el cargo único, y Argos factura y contabiliza. El único pago que nace de una llamada de Iris es el de un comprobante aprobado por un usuario de Argos vinculado (§7.7), y también lo registra Argos.

### 3.1 Idempotencia en Argos

- Con la misma `Idempotency-Key`, la misma ruta y el mismo cuerpo, Argos devuelve el `2xx` original (mismo status, cuerpo e id) con `Idempotent-Replayed: true` y no crea nada.
- La huella es el sha256 de método + URL completa con query + el cuerpo JSON **crudo**: el orden de las claves y los campos extra cuentan. Un reintento manda exactamente los mismos bytes.
- Misma clave con otro cuerpo u otra ruta: `422 idempotency_key_reutilizada` (de negocio, no se reintenta). Primera petición aún en curso: `409 idempotencia_en_curso` (se reintenta con espera). Un `4xx` o `5xx` no guarda la clave. Desde 0.14.3, una clave ya guardada con la misma huella devuelve su `2xx` original **antes** de cualquier comprobación de capacidad (§4.3): un replay nunca responde `422 capacidad_no_disponible`, y una petición en curso con la misma clave responde `409 idempotencia_en_curso`, nunca `422 capacidad_no_disponible`.
- En sentido contrario (Argos → Iris, §6), Iris responde `409` con un `type` que termina en `idempotencia_en_curso` (mensajería) o `idempotencia_en_proceso` (resto de su API). Argos reintenta cualquiera de los dos con la misma clave y los mismos bytes; otro `409` es de negocio y no se reintenta (0.6.2).
- El alcance de la clave es el actor (el usuario técnico «Iris»). Argos la conserva al menos 30 días.
- En aprobar y rechazar comprobantes (§7.7, 0.7.0) la huella incluye además `X-Iris-Actor`: la misma clave con otro actor responde `422 idempotency_key_reutilizada` y nunca un replay.
- En `activar-instalacion` la cabecera es obligatoria (`400 idempotency_key_requerida`); en el resto de §3, opcional, pero Iris siempre la manda.
- En `activar-instalacion`, `version` es opcional e Iris no la manda: una versión leída al encolar queda vieja y el reintento nunca avanzaría. Si llega y no coincide, `409 conflicto_version`. Sin ella, Argos se protege con la máquina de estados (solo sale de `pendiente_instalacion`) y con la `Idempotency-Key`.
- Una activación con otra clave (o cuya respuesta ya no está guardada) sobre un servicio que ya no está `pendiente_instalacion` responde `409 servicio_no_pendiente` (`type: urn:argosisp:problema:servicio_no_pendiente`, `detail` con el estado actual). Es de negocio y no se reintenta: la orden queda «cerrada con pendiente en Argos» y se avisa al supervisor. De los 409, solo `idempotencia_en_curso` se reintenta.

### 3.2 Alta desde un prospecto ganado

1. `POST /clientes`: `nombre` (2–200) obligatorio (0.10.0: un `nit_cui` que manda Iris aquí no vale como `documento` de §7.5 hasta que coincida con una identidad A41; Argos guarda su origen); opcionales `tipo` (`persona` | `empresa`), `tipo_documento_fiscal`, `nit_cui` (Argos valida el dígito verificador), `nombre_fiscal`, `direccion_fiscal`, `email`, `telefono`, `movil`, `direccion`, `coordenadas` (`"lat,lng"`), `zona_id`, `canal_preferido`, `codigo_externo` (Iris manda `iris:prospecto:<id>`). Sin `empresa_id`: el cliente es de la instalación. Argos genera el `codigo`.
2. `POST /servicios`: `cliente_id`, `router_id` y `plan_id` obligatorios; Iris manda además `empresa_id` (la del prospecto), `estado_inicial: pendiente_instalacion`, `direccion` y `coordenadas`. Sin `origen_tipo`/`origen_id` (van solo en `activar-instalacion`). Si no llega `user_ppp`, Argos lo genera; `409 user_ppp_duplicado` es de negocio. La respuesta nunca trae la clave PPP.
3. **0.5.0:** `POST /clientes` acepta `referencia` (≤ 300), y `POST /servicios`, `referencia`, `contacto_nombre` (≤ 200) y `contacto_telefono`. `telefono`, `movil` y `contacto_telefono` se validan en el alta y en la edición (Guatemala, 8 dígitos que empiezan en 2–7, con `+502` opcional; o internacional `+` y 8–15 dígitos), y Argos los guarda normalizados (`+502 5512-3344`). Si no es válido: `422 validacion` con `rule: 'telefono'` en el campo; es de negocio y no se reintenta.
4. **0.10.0 (A44):** `POST /clientes` acepta `telefonos_adicionales[] { numero (E.164), whatsapp }` (hasta 4), que Argos guarda como números del contacto titular y que no cuentan como «registrados» para §7.5 y §12.4. Iris los manda al ganar un prospecto con varios números (§3.6).
5. Iris guarda el `cliente_id` y el `servicio_id` de cada `2xx`; si los perdiera, repite la llamada con la misma clave (§3.1) para recuperarlos.

### 3.3 Inventario del técnico (0.5.0)

Detalle de Argos en su contrato de inventario (Fase D). Aquí va lo que Iris necesita.

**Técnico.** `tecnico` es `{ id_externo }` o `{ correo }`.
- `id_externo` es el id del usuario de Argos. Iris lo obtiene de `GET /inventario/tecnicos` y guarda su propio vínculo, que enlaza un admin. No depende del vínculo de SSO.
- Si llegan los dos, manda `id_externo`.
- `correo`: Argos no verifica correos (§1.3). Solo se acepta si coincide con un único usuario activo, sin distinguir mayúsculas. Si coincide con más de uno: `422 tecnico_ambiguo`.
- La bodega es la bodega `tecnico` activa de ese usuario en la empresa de la operación: la del servicio. Sin servicio, la de `X-Empresa-Id`; sin ella y con varias posibles, `422 empresa_requerida`.

**Lecturas.**
- `GET /inventario/tecnicos?cursor=&limit=` (`inventario.ver`) → `{ data: [{ id_externo, correo, nombre, bodegas: [{ id, empresa_id, activa, nombre }] }], nextCursor }`. Solo usuarios con al menos una bodega `tecnico`, dentro del alcance de empresas del usuario técnico Iris. `bodegas[].nombre` (0.6.4) es el nombre de la bodega, para elegirla de una lista sin teclear códigos.
- `GET /inventario/mi-bodega?id_externo=|correo=` con `empresa_id` o `X-Empresa-Id` → `{ bodega, existencias[], unidades[], traslados_pendientes[], kardex[] }`.
    - Sin empresa y con bodegas en varias: `{ bodegas: [ … una por empresa … ] }`.
    - `existencias[]` y `unidades[]` (disponibles y en reparación) vienen completas, y `traslados_pendientes[]` también.
    - `kardex[]` trae los últimos 50 movimientos. El resto, con `GET /inventario/movimientos?bodega_id=&cursor=&limit=` (≤ 100) → `{ data, nextCursor }`.
- Cada línea de `existencias[]`, `mi-bodega` y `GET /productos` lleva `modalidad` (`comodato` | `consumible_gasto` | `consumible_cobro`).
- `GET /inventario/traslados/:id` → objeto directo, sin envoltura (`OrdenTrasladoDetalle`; 0.5.1):
    - **Identificación:** `id`, `empresa_id`, `numero` (correlativo por empresa) y `estado` (`en_transito` | `recibida` | `recibida_con_diferencias` | `cancelada`).
    - **Bodegas:** `bodega_origen_id`/`_nombre` y `bodega_destino_id`/`_nombre`/`_tipo` (`bodega` | `tecnico`).
    - **Técnico:** `usuario_destino_id` (= `id_externo` del técnico, o null) y `usuario_destino_nombre`. No trae objeto `tecnico` ni correo; el correo sale de `GET /inventario/tecnicos`.
    - **Envío:** `motivo`, `enviado_por`/`_nombre` y `enviado_en`.
    - **Recepción** (todo puede ser null): `metodo_recepcion`, `recibido_por`/`_nombre`, `registrado_por`/`_nombre`, `recibido_en`, `observaciones`, `firma_sha256`, `firma_en` y `documento_sha256`/`_tipo`/`_en`.
    - **Cancelación:** `cancelado_por`, `cancelado_en` y `motivo_cancelacion`.
    - **Totales y control:** `lineas_total`, `costo_total` (string decimal), `version`, `created_at` y `updated_at`.
    - **`lineas[]`:** `{ id, producto_id, producto_sku, producto_nombre, unidad, seguimiento, modalidad, cantidad, costo_total, cantidad_recibida, costo_recibido, faltante, unidades[] }`. El `id` de la línea es el `linea_id` que se manda al recibir.
    - **`unidades[]`:** `{ unidad_id, serie, mac, costo, recibida }`. `series_faltantes[]` usa la `serie` de aquí.

**Consumos** (`POST /inventario/consumos`):
- **Forma:** `lineas[]` (o, como hasta hoy, un solo producto arriba). Desde 0.12.0, el cuerpo lleva además `fecha` (la del teléfono) y `recibido_en_iris` (la hora del servidor de Iris al aceptar el lote), con la ventana de §3.10; Argos los usa para evaluar la membresía de una bodega de cuadrilla (§3.9). Responde `MovimientoInventario` (el de la primera línea) más `movimientos[]`, `cargos[]` y `factura_id` (o null).
- **Atómico:** si una línea falla (por ejemplo `409 existencia_insuficiente`), no se aplica ninguna.
- **Solo consumibles:** una línea `comodato` responde `422 producto_es_comodato`.
- **`consumible_cobro`:**
    - Argos emite el cargo único al cliente del servicio con el precio del producto, con `origen_tipo`/`origen_id` de la orden y la misma `Idempotency-Key`: un reintento no cobra dos veces.
    - Sin servicio o sin cliente: `422 cobro_sin_cliente`.
    - La respuesta trae `cargos[] { id, producto_id, monto }` (montos como string decimal).
    - Un `POST /facturas/cargo-unico` de la misma orden (`origen_id`) con una línea cuyo `producto_id` ya se cobró por modalidad en esa orden responde `409 cargo_duplicado`. El cobro de la visita u otros conceptos de la misma orden sigue permitido.
- **`consumible_gasto`:** va a gasto del ISP y no genera cargo.

**Comodato y devolución:**
- `POST /servicios/:id/equipos` acepta `tecnico`: la unidad debe estar en su bodega; si no, `409 unidad_en_otra_bodega`. El comodato nunca llevó `bodega_id`: la unidad dice dónde está.
- `…/devolver` acepta `{ tecnico | bodega_id, condicion }`.

**Recibir una orden de traslado** (`POST /inventario/traslados/:id/recibir`, permiso `inventario.recibir_traslado`). El técnico confirma sin señal y se sincroniza después.
- **Cuerpo:** `{ recibido_por: tecnico, metodo_recepcion: 'firma_digital', firma: { png_base64, sha256 }, recibido_en (RFC 3339, hora del teléfono), lineas? }`.
    - El PNG pesa como máximo 200 KB decodificado y mide entre 50×20 y 1000×400 px. `sha256` es el hash del PNG decodificado.
    - `recibido_en` no puede ser anterior al envío ni futuro, con 5 min de tolerancia de reloj.
    - `lineas[]` solo si faltó algo: por línea `{ linea_id, cantidad_recibida, series_faltantes[] }`. Lo faltante vuelve al origen.
- **Idempotencia:** un 2xx es definitivo. Otra recepción de la misma orden, con cualquier clave, responde `409 traslado_ya_recibido` con la recepción guardada en el cuerpo. Las diferencias posteriores se corrigen con un ajuste en Argos, no repitiendo la llamada.
- **Errores** (de negocio, no se reintentan):

| Status | Código |
| --- | --- |
| 404 | `traslado_no_encontrado` |
| 409 | `traslado_ya_recibido` |
| 409 | `traslado_cancelado` |
| 422 | `traslado_de_otro_tecnico` |
| 422 | `lineas_invalidas` |
| 422 | `firma_invalida` |
| 422 | `tecnico_no_encontrado` |
| 422 | `tecnico_ambiguo` |
| 422 | `tecnico_sin_bodega` |

Los mismos códigos valen en lecturas y escrituras: `GET /inventario/mi-bodega` sin bodega responde `404 tecnico_sin_bodega`.

**Qué se reintenta:** solo `5xx` (desde 0.14.0, también el `503` con `Retry-After` de `…/no-recuperado`, §3.10), timeouts, `409 idempotencia_en_curso` (§3.1) y, desde 0.14.0, `409 vinculos_en_carga` (§6.6).

**Versión y compatibilidad.**
- El canje (§1.3/§1.4) y `GET /integraciones/iris/estado` devuelven `version_argos` (por ejemplo `"1.6.0"`) y `contrato` (por ejemplo `"0.5.0"`).
- El canje acepta `contrato` (la versión que habla Iris), y `PUT /integraciones/iris/contrato { contrato }` la actualiza con el token de Iris.
- **Sin retroceso (0.14.0).** Un `PUT /integraciones/iris/contrato` con una versión menor que la última aceptada responde `409 contrato_regresivo` y Argos avisa al admin. Para bajar de versión hace falta un canje nuevo aprobado en el panel de Argos. Una versión mayor que el `contrato` que habla Argos responde `422 contrato_no_soportado` y no se registra.
- **Receptor de Argos (0.6.2).** El canje y el PUT aceptan además `receptor_argos?: boolean`: `true` cuando Iris tiene guardados la `url` y el `secreto` del receptor de §6.6. Con B1, el canje siempre manda `true`. En el PUT, Iris manda `false` para los conectores canjeados antes de B1, que no tienen receptor guardado y a los que Iris no envía `aviso.*` ni `contacto.baja` (quedan `omitido`). Si el PUT no lo trae, Argos conserva el último valor. `GET /integraciones/iris` devuelve `requiere_reconectar` (`true` solo si el último `receptor_argos` es `false`) y `motivo_reconectar` (texto o `null`), y el panel pide volver a conectar.
- Argos solo envía los webhooks nuevos de 0.5.0 a conectores con contrato ≥ 0.5.0. Así un Iris anterior no los recibe como desconocidos.
- Argos 1.5.x no conoce `tecnico`: lo ignora y, sin `bodega_id`, responde `422 bodega_id required`. Iris consulta `version_argos` antes de mandar `tecnico`.
- `bodega_id` sigue válido en todo 0.x.
- **0.8.0:** qué hace cada lado cuando el otro no la habla está en §4.1. No hay mecanismos nuevos: se usan `contrato`, `version_argos` y `PUT /integraciones/iris/contrato`.
- **0.8.1:** un Argos anterior a 1.21.0 no conoce `buscar_red` y lo ignora. Iris lo manda igual (§2.1, §4.2) y usa `version_argos` solo para registrar si tuvo efecto.

### 3.4 De la venta a la instalación (0.10.0)

El cliente no siempre da todos sus datos al vender, y en la instalación puede cambiar el titular (por ejemplo, pidió el servicio un menor y queda de titular un adulto) o el plan. Todo esto pasa **solo mientras el servicio está `pendiente_instalacion`**; después de activar, un cambio de titular es un traspaso y un cambio de plan es A34, fuera de esta sección. Decisiones del PM en §10.5.

**Principios.**

| Regla | Detalle |
| --- | --- |
| Quién | El técnico, en el paso «Confirmar titular y plan» de la orden de instalación de Iris (app de campo, también sin señal), o un agente o supervisor desde el panel. Sin aprobación previa: queda auditado y ventas lo ve después. En Iris el técnico solo puede sobre **sus órdenes en curso** (asignadas a él, en sitio y no cerradas). |
| Orden | Iris aplica en Argos, en este orden y con dependencias en su cola de pendientes: cambiar el titular (A39, que incluye su identidad) o registrar la identidad del mismo titular (A41) → completar los datos del titular y del lugar (A43) → cambiar el plan (A40) → activar (`activar-instalacion` o `aprovisionar`, §12). Si una falla, las siguientes no salen (§3.4.8). |
| Actor | Todas las llamadas llevan `X-Iris-Actor: usuario:<id>` (§7.1) e `Idempotency-Key`. Argos audita con ese actor y canal `api`. Con `X-Iris-Actor: asistente_ia`, todas las rutas de esta sección responden `403 actor_ia_no_permitido`. |
| Alcance | Argos acepta estas rutas solo sobre servicios creados por Iris (`codigo_externo` del cliente `iris:*`, §3.2) o con un `origen_id` de orden de Iris que Argos ya registró **para ese mismo servicio**, en una llamada aceptada de esta sección o en `activar-instalacion`/`aprovisionar` sobre un servicio que ya cumplía esta regla; si no, `403 servicio_no_de_iris`. |
| `version` | Opcional para Iris en todas las rutas de esta sección, como en `activar-instalacion` (§3.1): una versión leída al encolar queda vieja. Si llega y no coincide, `409 conflicto_version`. |
| Idempotencia en multipart | La huella es el sha256 de cada parte (nombre, tipo y bytes), en orden, **sin el boundary**: un reintento con otro boundary es el mismo cuerpo. |
| Datos personales en errores | El `detail` y los `errors[]` de Argos **nunca repiten el valor recibido** (documento, teléfono, correo, nombre): solo el campo y la regla. |
| Identidad | La foto del documento es del cliente y **vive en Argos** (A41). Iris solo la tiene en tránsito (§3.4.8). El número de documento nunca va en eventos ni logs de ninguno de los dos lados. |

#### 3.4.1 Servicio pendiente sin IP (A38)

- `POST /servicios` con `estado_inicial: pendiente_instalacion` **no asigna** IPv4 ni prefijos IPv6. El IPAM asigna con el router final al activar: en `POST /servicios/:id/activar-instalacion` o en `POST /servicios/:id/aprovisionar` (§12.2). `user_ppp` y la contraseña PPP se generan al crear, como hoy.
- **Reservar en sitio** (para conexiones que necesitan la IP en el equipo antes de activar: IP fija o amarre IP-MAC): `POST /servicios/:id/reservar-ip` (`servicios.reservar_ip`), `Idempotency-Key = iris:reservar_ip:<orden_id>:<router_id>`, cuerpo `{ router_id?, origen_tipo: "iris_orden", origen_id }`. Solo en `pendiente_instalacion` (`422 servicio_no_pendiente`). Responde `200 { ipv4, prefijo_v6 }` (o `null`); una segunda llamada devuelve la misma reserva. La activación usa esa reserva; si el servicio se retira sin activar, se libera, y **vence a los 30 días** sin activar (B5). Con PPPoE no hace falta.
- Sin IP libre en el router: `422 sin_ip_disponible`, en `reservar-ip`, en `activar-instalacion` y como `causa` de `aprovisionar`. Es de negocio y no se reintenta solo.
- El servicio pendiente ya no lleva IP en `GET /servicios/:id` ni en la primera sincronización (§2.5). Al asignarse, Argos emite `asignacion.cambiada` (ya existe).

#### 3.4.2 El titular: qué pide la orden

**¿Quién firma?** «La misma persona de la venta» (A41 sobre el cliente del servicio) u «Otra persona» (A39: Argos busca o crea el cliente por documento y le pasa el servicio). Para activar, el titular necesita:

| Dato | Regla |
| --- | --- |
| Tipo de documento | `dpi` o `pasaporte`. En empresa (`cliente.tipo: empresa`, 0.14.1): NIT en `tipo_documento_fiscal` + `nit_cui` y un **representante** con DPI o pasaporte (decisión del PM, 22 nov 2026, que amplía la decisión 4) |
| Número | DPI: 13 dígitos con la verificación de departamento y municipio. Pasaporte: alfanumérico de 5 a 20. NIT: con dígito verificador (`K` permitido) |
| Nombre completo | Tal como aparece en el documento (2 a 200) |
| Mayor de edad | Casilla obligatoria «El titular es mayor de edad» (`mayor_de_edad: true`); **sin** fecha de nacimiento |
| Teléfono | 8 dígitos de Guatemala (regla de §3.2) y `whatsapp` (bool) |
| Correo | Opcional |
| Facturar a | El receptor de la factura (FEL): `tipo_documento_fiscal` = `cf`, `nit` (con dígito verificador) o `cui`, y `nit_cui` con el número (vacío con `cf`). En Argos es el mismo campo que `nit_cui`; no es la identidad de A41 |
| Relación con quien pidió el servicio | Opcional: `titular`, `familiar` u `otro` |
| Validación de identidad | Foto del documento: **frente obligatorio**, reverso opcional; el técnico marca «Coincide con el titular» (`coincide: true`); la firma del titular va en el contrato (en Iris) |

**Lugar del servicio:** dirección escrita, referencia, coordenadas GPS con su precisión en metros y foto de fachada opcional (la foto de fachada queda en Iris como adjunto de la orden; no viaja a Argos).

#### 3.4.3 Registrar la identidad del titular (A41)

`POST /api/v1/clientes/:id/identidad` (`clientes.registrar_identidad`), `multipart/form-data`, con `X-Iris-Actor` e `Idempotency-Key = iris:identidad:<orden_id>:<toma>` (`toma` = número de toma de la foto en esa orden: 1, 2…):

| Parte | Regla |
| --- | --- |
| `frente` | Obligatoria. JPEG o PNG, hasta 5 MB |
| `reverso` | Opcional, igual |
| `documento_tipo` | `dpi` o `pasaporte` (en empresa, el del representante) |
| `documento_numero` | Con las reglas de §3.4.2 |
| `nombre` | Como aparece en el documento |
| `mayor_de_edad` | `true` (otro valor: `422 validacion`) |
| `coincide` | `true`: el técnico comprobó que la foto es de quien está presente |
| `verificado_en` | RFC 3339, hora del teléfono corregida |
| `metodo` | `foto_documento` (el técnico, en sitio). Desde el panel de Iris, `revision_panel`: un supervisor revisa la foto **tomada en sitio**, con motivo; nunca `foto_documento` |
| `origen_tipo`, `origen_id` | `iris_orden` y la orden |

- Solo para el cliente de un servicio `pendiente_instalacion` con una orden de Iris en curso (`422 cliente_sin_instalacion`): el token de Iris no sube identidades de cualquier cliente.
- Argos la guarda **cifrada en reposo**. Solo la ve quien tiene `clientes.ver_identidad` en Argos; cada vista se audita; nunca sale por el portal, webhooks, eventos, logs ni MCP. Iris no la puede leer de vuelta.
- **Sobre un cliente con historial** (otros servicios que no están en `pendiente_instalacion`, o facturas o pagos): solo se acepta si coinciden `documento_numero` y el nombre normalizado con lo que Argos ya tiene; si no, `422 titular_no_coincide`. **A41 nunca escribe `nit_cui`:** la identidad vive en su propia tabla y el receptor fiscal es otra cosa. En un cliente con historial sin documento, la identidad se guarda, pero no vale como `documento` de §7.5 ni como `datos_titular` de §12.4 hasta que el titular la confirme con un código a sus datos ya registrados.
- Argos conserva solo la identidad **vigente y la anterior**; todas se borran **12 meses después de retirado el cliente**, y en el acto si el cliente se elimina (A39).
- `201 { identidad_id, version }`. **Idempotente por `Idempotency-Key`** (§3.1): misma clave y mismos bytes (las partes del multipart incluidas) devuelven el `201` original con `Idempotent-Replayed: true` y no registran otra versión; misma clave con otro cuerpo, `422 idempotency_key_reutilizada`. Emite `cliente.identidad_registrada { cliente_id, empresa_id, documento_tipo, metodo }` (§2.3), sin número ni foto.

#### 3.4.4 Cambiar el titular antes de activar (A39)

`POST /servicios/:id/cambiar-titular` (`servicios.cambiar_titular`), `multipart/form-data`, `Idempotency-Key = iris:titular:<orden_id>:<cambio_id>`: la parte `datos` (JSON) y las partes `frente` y `reverso?` de A41.

```json
{
  "cliente": { "nombre": "Ana Pérez", "documento_tipo": "dpi", "documento_numero": "1234567890101", "mayor_de_edad": true, "telefono": "+502 5512-3344", "whatsapp": true, "email": null, "tipo_documento_fiscal": "cf", "nit_cui": null, "relacion": "familiar" },
  "identidad": { "coincide": true, "verificado_en": "2026-11-21T16:02:00Z", "metodo": "foto_documento" },
  "motivo_codigo": "menor_de_edad",
  "motivo": "El servicio lo pidió un menor; queda de titular su madre",
  "origen_tipo": "iris_orden",
  "origen_id": "9123"
}
```

- **Titular empresa (0.14.1).** `cliente.tipo` es `persona` (por defecto) o `empresa`. Con `empresa`:
    - `cliente` lleva `nombre` (la razón social), `tipo_documento_fiscal: "nit"` y `nit_cui` (obligatorio: `422 nit_requerido`), y los mismos `telefono`, `whatsapp`, `email` y `relacion`; **no** lleva `documento_tipo`, `documento_numero` ni `mayor_de_edad`: mandarlos con `tipo: empresa` responde `422 validacion`.
    - `cliente.tipo: empresa` no obliga a un plan `modo: empresarial`: el plan se elige aparte (A40).
    - `representante` es obligatorio (`422 representante_requerido`): `{ nombre, documento_tipo: "dpi" | "pasaporte", documento_numero, mayor_de_edad: true }`. Con `tipo: persona`, `representante` está prohibido (`422 representante_no_permitido`).
    - `frente`, `reverso` e `identidad.coincide` son del documento del **representante**; Argos registra esa identidad (A41) a nombre del representante, ligada al cliente empresa.
    - La identidad del representante nunca vale como `documento` de §7.5 ni como `datos_titular` de §12.4 del cliente empresa, y nunca escribe `nit_cui`. En una empresa con historial, la regla de §3.4.3 compara al representante con las identidades A41 de esa empresa; si no coincide, `422 titular_empresa_en_revision`.
    - **Titular existente en empresas: por NIT.** Argos solo considera clientes guardados como `tipo = empresa` y con `nit_cui` de origen verificado en Argos: nunca uno llenado por A43 ni creado por el token de Iris en `POST /clientes` (§3.2). Un cliente `persona` nunca coincide por NIT. El NIT se compara sin guiones ni espacios y sin distinguir mayúsculas (la `K`), como en §7.5. En personas, la búsqueda sigue siendo por documento (abajo).
        - **Mismo NIT y misma razón social normalizada:** Argos asigna al momento solo si el `documento_tipo`, el `documento_numero` y el nombre normalizado del representante coinciden con una identidad A41, vigente o anterior, de esa empresa. Si no coinciden, `422 titular_empresa_en_revision`, sin datos del cliente: no cambia nada, la orden pasa a revisión de un supervisor y decide una persona de Argos con `clientes.ver_identidad`.
        - **Mismo NIT y otra razón social:** `422 titular_no_coincide`.
        - **Más de un candidato:** `422 titular_empresa_en_revision`.
        - La identidad de un representante nuevo **nunca reemplaza** la vigente de un cliente empresa con historial: queda como representante adicional hasta que Argos la apruebe.
    - El documento del representante sigue las mismas reglas que el del titular persona: solo en tránsito en Iris, nunca en `payload`, eventos, logs ni errores. En pendientes, panel y auditoría, todo `representante.*` sale como `***`, salvo `documento_tipo`.

  Ejemplo de `datos` con empresa:

  ```json
  {
    "cliente": { "tipo": "empresa", "nombre": "Ferretería El Sol, S.A.", "tipo_documento_fiscal": "nit", "nit_cui": "1234567-8", "telefono": "+502 2233-4455", "whatsapp": true, "email": null, "relacion": "otro" },
    "representante": { "nombre": "Ana Pérez", "documento_tipo": "dpi", "documento_numero": "1234567890101", "mayor_de_edad": true },
    "identidad": { "coincide": true, "verificado_en": "2026-11-21T16:02:00Z", "metodo": "foto_documento" },
    "motivo_codigo": "otro",
    "motivo": "La venta se hizo a nombre de la empresa del cliente",
    "origen_tipo": "iris_orden",
    "origen_id": "9123"
  }
  ```
- **Quién es el titular lo decide Argos.** Iris **nunca** manda un `cliente_id`. Argos compara en su lado, en la empresa del servicio:
    - **Mismo `documento_tipo` y `documento_numero` y mismo nombre normalizado** (sin tildes, sin distinguir mayúsculas y sin espacios dobles): con la identidad de A41 tomada en sitio (foto del documento, «Coincide con el titular» y la firma del contrato a su nombre, B20), Argos **asigna el servicio a ese cliente al momento**, registra en él la identidad como **versión nueva** y le manda un aviso **informativo**, que no bloquea nada, por sus canales registrados («Se agregó un servicio a tu nombre; si no fuiste tú, comunícate con nosotros»). La respuesta solo dice `titular: { cliente_id, existente: true }`, sin ningún otro dato de ese cliente. `activar-instalacion` y `aprovisionar` siguen normal (decisión 9 del PM, §10.5).
    - **Mismo documento y otro nombre:** `422 titular_no_coincide`, sin datos del otro cliente, y no cambia nada. Cada `titular_no_coincide` suma a un **contador aparte** del documento que coincidió: no bloquea la verificación del titular en §7.5; pasado el tope (5 en 24 h), solo bloquea nuevos cambios de titular hacia ese documento (`429 demasiados_cambios_titular`). La orden queda en revisión de un supervisor. En empresas, el contador cuenta por NIT normalizado y suman también los `titular_empresa_en_revision`; el tope bloquea los cambios hacia ese NIT con cualquier representante.
    - **Ningún cliente con ese documento** (en empresas, el documento es el NIT): crea uno nuevo (`titular: { cliente_id, existente: false }`). Solo marca `posible_duplicado: true` cuando coinciden el nombre o el teléfono y el documento no, para que una persona de Argos lo revise.
- **Límites:** como máximo **2 cambios de titular por servicio** y **10 por empresa y técnico** (`X-Iris-Actor`) al día: `429 demasiados_cambios_titular` con `Retry-After`.
- La identidad que registra (A41) es la `evidencia` del cambio: no hay adjuntos de Iris.
- Solo en `pendiente_instalacion` (`422 servicio_no_pendiente`). `version` opcional (principios). `motivo_codigo` (`menor_de_edad` u `otro`) y `motivo` de 10 a 500 caracteres; el texto queda solo en la auditoría de Argos (B1). Sin `frente` o sin `coincide: true`: `422 evidencia_requerida`.
- Argos mueve el servicio y sus credenciales PPP al titular. Las facturas ya emitidas no se mueven (si hiciera falta, las rehace una persona en Argos); por eso Iris manda el cobro de la instalación (§3, A36) **después** de confirmar el titular. El cliente anterior, si se queda sin servicios ni movimientos, se **elimina**; si tiene historial, queda como está.
- Responde `200 { servicio_id, titular: { cliente_id, existente }, posible_duplicado, identidad_id, de_cliente_id, de_cliente_eliminado }` y emite `servicio.titular_cambiado` y `cliente.identidad_registrada` (§2.3).

#### 3.4.5 Cambiar el plan antes de activar (A40)

- `GET /planes?router_id=&modo=residencial|empresarial` devuelve **solo** los planes aplicables: activos, del mismo `modo` y asignados a todos los routers o a ese router. Cada plan suma `modo`. Iris guarda `modo` y los routers en su instantánea (`catalogo.cambiado` de plan suma `modo` y `routers` en `campos[]`).
- Por defecto, la orden propone el plan de la cotización. Si el técnico elige otro, el motivo es obligatorio (`mas_velocidad`, `menos_velocidad` u `otro` con texto) y la orden muestra el precio nuevo antes de confirmar.
- `POST /servicios/:id/cambiar-plan` (`servicios.cambiar_plan_pendiente`), `Idempotency-Key = iris:plan:<orden_id>:<cambio_id>`, cuerpo `{ plan_id, motivo, motivo_texto?, version?, origen_tipo: "iris_orden", origen_id }`. Solo en `pendiente_instalacion` (`422 servicio_no_pendiente`). Sin factura ni prorrateo (no hay facturación todavía). Un plan que no sea de los aplicables: `422 plan_no_permitido`.
- Responde `200` con el servicio y emite `servicio.config_cambiada` con `plan` y `precio` en `campos[]` (ya existe; con `origen_tipo`/`origen_id`).
- Argos vuelve a comprobar el plan al activar: si el router final cambió y el plan ya no aplica, `activar-instalacion` y `aprovisionar` responden `422 plan_no_permitido`.
- **Desde 0.15.1 (§13.4):** el plan nuevo puede ser de otro tipo de servicio. Si agrega `internet` o `iptv`, el cuerpo suma `router_id` (obligatorio: `422 router_requerido`); si quita `internet`, Argos libera la IP reservada.

#### 3.4.6 Completar los datos del titular y del lugar (A43)

Argos solo exige nombre y teléfono al crear. Lo que falte lo manda Iris **antes de activar**:

- `POST /servicios/:id/completar-datos` (`servicios.completar_datos`), `Idempotency-Key = iris:datos:<orden_id>:<n>`, cuerpo `{ cliente?: { nombre?, telefono?, whatsapp?, email?, tipo_documento_fiscal?, nit_cui?, relacion? }, servicio?: { direccion?, referencia?, coordenadas?, precision_m? }, identidad_id?, version?, origen_tipo: "iris_orden", origen_id }`.
- Solo con el servicio en `pendiente_instalacion` (`422 servicio_no_pendiente`) y para el cliente **de ese servicio**. Del cliente, **solo llena** campos vacíos (`422 campo_ya_registrado`); con `identidad_id` de una identidad registrada en esta instalación (A41 o A39), también corrige `nombre` para que coincida con el documento. Los del servicio (dirección, referencia y coordenadas de la instalación) sí se corrigen.
- **Cliente con historial** (otros servicios que no están en `pendiente_instalacion`, o facturas o pagos): A43 no acepta `telefono`, `movil`, `email`, `nombre`, `tipo_documento_fiscal` ni `nit_cui` (`422 cliente_con_historial`): desde Iris nadie toca el receptor fiscal de ese cliente: esos datos los cambia Argos o el titular.
- **Facturar a (cliente sin historial):** A43 llena `nit_cui` y `tipo_documento_fiscal` con lo elegido en «Facturar a» (`cf`; `nit` con su dígito verificador; o `cui`). Argos valida (`422 validacion`). A41 nunca escribe `nit_cui`.
- Un teléfono o correo agregado por A43 **no cuenta como registrado** para `codigo_telefono` ni `codigo_correo` (§7.5, §12.4) hasta que el titular lo confirme con un código. Mientras tanto, Argos avisa al titular por los datos que ya tenía.
- No es un `PUT /clientes/:id` general: el token de Iris no edita clientes fuera de este caso.
- Emite `cliente.actualizado` y `servicio.datos_cambiados` (ya existen), solo con nombres en `campos[]`.

#### 3.4.7 Errores

| Status | Código | Dónde |
| --- | --- | --- |
| 422 | `servicio_no_pendiente` | A38 (reservar), A39, A40, A43 |
| 422 | `cliente_sin_instalacion` | A41 |
| 409 | `conflicto_version` | A39, A40, A43 |
| 422 | `sin_ip_disponible` | A38, `activar-instalacion`, `aprovisionar` (`causa`) |
| 422 | `plan_no_permitido` | A40, `activar-instalacion`, `aprovisionar` |
| 422 | `evidencia_requerida` | A39 |
| 422 | `titular_no_coincide` | A39 y A41: el documento es de otro cliente con otro nombre (en empresas, el NIT es de otra razón social). En Iris, la operación de la cola queda `rechazada` con este código y la orden pasa a revisión de un supervisor |
| 422 | `campo_ya_registrado` | A43 |
| 422 | `cliente_con_historial` | A43 (`telefono`, `movil`, `email`, `nombre`, `tipo_documento_fiscal` o `nit_cui` de un cliente con historial) |
| 403 | `servicio_no_de_iris` | Todas las de §3.4 |
| 403 | `actor_ia_no_permitido` | Todas las de §3.4 y `contactos.gestionar` con `asistente_ia` |
| 429 | `demasiados_cambios_titular` | A39 (2 por servicio y 10 por empresa y técnico al día) |
| 413 / 422 | `tamano_excedido` / `mime_no_permitido` | A39, A41 (fotos) |
| 422 | `validacion` | Cualquier campo (`errors[]`): documento, NIT, teléfono, `mayor_de_edad` |
| 422 | `representante_requerido` / `nit_requerido` / `representante_no_permitido` | A39 con `cliente.tipo` (0.14.1): empresa sin representante o sin NIT; persona con representante |
| 422 | `titular_empresa_en_revision` | A39 con empresa (0.14.1): el representante no coincide con una identidad A41 de la empresa del NIT, o hay más de un candidato. Sin datos del cliente; no cambia nada. En Iris, la operación de la cola queda `rechazada` con este código y la orden pasa a revisión de un supervisor; decide una persona de Argos |

Todos son de negocio y no se reintentan solos (§3.1).

#### 3.4.8 En Iris (informativo para Argos)

- **Sin señal primero.** En el teléfono, la foto del documento vive **solo en la cola local**, cifrada con una llave WebCrypto **no exportable**, hasta que el servidor de Iris confirma que la recibió; entonces se borra del teléfono, y también al cerrar o revocar la sesión. En el servidor de Iris queda en un **depósito de tránsito cifrado**, fuera de los adjuntos y fuera de la auditoría (`***`), y sale a Argos por `integracion.pendientes` con `Idempotency-Key = iris:identidad:<orden_id>:<toma>` (o dentro de A39). En `integracion.pendientes` el documento y las fotos se guardan **solo como referencia al depósito**, nunca en `payload`; los eventos `integracion.pendiente_*` y el panel muestran `documento_numero`, `telefono` y `email` como `***`, y con empresa (0.14.1), todo `representante.*` salvo `documento_tipo`.
- **Borrado:** la foto se borra del servidor de Iris al recibir el `2xx` de Argos. En cualquier otro caso (rechazada, cancelada o resuelta a mano) se borra a los **7 días de tomada**, y la orden muestra el error para tomarla otra vez. Nadie la ve en el depósito de tránsito, tampoco el agente.
- **Sin identidad, la orden no activa**, salvo que un agente la valide desde el panel con `ordenes.validar_identidad` (motivo, auditado). Ese permiso **solo** permite activar: nunca cambiar el titular ni registrar A41. La revisión desde el panel (`metodo: revision_panel`) la hace un supervisor sobre la foto tomada en sitio, con motivo.
- **El número de documento** nunca va en eventos ni logs de Iris; en la auditoría, `***`. Con empresa, en pendientes, panel y auditoría todo `representante.*` sale como `***`, salvo `documento_tipo`.
- **El prospecto** queda «ganado, alta pendiente» (`alta.estado = esperando_activacion`) ligado al cliente y al servicio de Argos hasta que el servicio se activa; ahí pasa a `completa`. Si cambia el titular, el prospecto pasa al titular nuevo.
- **La firma** del titular es la del **contrato regenerado** a su nombre, en la orden (un menor no puede quedar como quien firmó). Si cambió el plan, ese contrato ya lleva el precio nuevo: la firma en sitio cubre la subida de precio (B6).
- **El técnico no ve** `posible_duplicado` (B4) ni, con `existente: true`, otros servicios ni datos del cliente existente.
- **El cambio de plan** crea una revisión nueva de la cotización con el plan nuevo (reemplaza la vigente) y avisa al vendedor. Los planes que el técnico puede elegir son los de `GET /planes?router_id=&modo=`, guardados en el paquete del día.
- **El cobro de la instalación** de la orden (§3) sale después del paso «Confirmar titular y plan», a nombre del titular final y con el plan final.
- **`titular_no_coincide` y `titular_empresa_en_revision`:** la operación `orden.cambiar_titular` queda `rechazada` en la sincronización con ese código (regla en `contratos/sync.md`), la orden pasa a revisión de un supervisor y la activación no sale. El técnico no ve datos del otro cliente.
- **Si Argos rechaza** un paso, Iris no lo muestra como hecho: la orden queda «pendiente en Argos» con el motivo, la activación queda bloqueada (`integracion.pendiente_bloqueado`) y el técnico o un agente corrigen o lo resuelven a mano. Nunca se activa con el titular o el plan equivocados.

### 3.5 Contactos del cliente (0.10.0, A42)

Argos es el dueño de los contactos. Un cliente tiene varios (`core.contactos_cliente`); quien escribe por WhatsApp o llama no siempre es el titular, y cada contacto puede hacer solo lo que el titular le permite. Decisión del PM en §10.5.

**El contacto.**

```json
{
  "contacto_id": 77,
  "cliente_id": 1201,
  "empresa_id": 3,
  "nombre": "Luis Pérez",
  "relacion": "familiar",
  "es_titular": false,
  "servicios": [4410],
  "permisos": { "consultar": true, "reportar": true, "pagar": true, "avisos": false },
  "telefonos": [{ "numero": "+50255123344", "whatsapp": true, "principal": true }],
  "version": 3
}
```

- `relacion`: `titular`, `familiar`, `inquilino`, `empleado` u `otro`. `servicios`: los ids de servicio del cliente a los que aplica, o `null` = todos.
- `permisos`: `consultar` (saldo, estado del servicio, facturas), `reportar` (tickets y fallas), `pagar` (comprobantes y promesas) y `avisos` (recibe avisos de cobro). Todos encendidos por defecto; se apagan por contacto.
- **Los permisos son por servicio** (decisión 11 del PM): `consultar` y `pagar` aplican solo a los `servicios[]` del contacto (con `null`, a todos los del cliente). Una promesa o un comprobante pedido por un contacto siempre lleva `servicio_id` de uno de los suyos, y las reglas (cuántas promesas, días y monto) se evalúan con la plantilla de ese servicio, como en §7. Saldo y estado, solo de sus `servicios[]`: con un contacto de `servicios` acotados, Iris solo usa `GET /clientes/:id/estado-cuenta?servicio_id=` (§2.1); si Argos no la soporta, el contacto ve el estado del servicio, nunca el saldo.
- **`X-Iris-Contacto: <contacto_id>`** (forma en §7.1, 0.10.1; Argos valida también el permiso del contacto: `pagar` o `consultar`, `403 permiso_de_contacto`) en las promesas (§7.4), los comprobantes (§7.2) y el estado de cuenta por servicio que pide un contacto: Argos valida que el `servicio_id` esté entre los `servicios[]` de ese contacto; si no, `403 servicio_fuera_del_contacto`.
- `telefonos[]`: `numero` en E.164, `whatsapp` y `principal` (uno por contacto). **Un mismo número puede estar en varios clientes.**
- **Reservado al titular**, siempre con la identidad de A41: cambiar titular, cambiar plan, cambiar el Wi-Fi, traspaso y baja. Ningún permiso de contacto los abre.

**Quién los gestiona.** El personal, con `contactos.gestionar` y `X-Iris-Actor` (§7.1), y el titular desde el portal de Argos.

**Rutas.**

| Método y ruta | Permiso | Qué hace |
| --- | --- | --- |
| `GET /contactos/buscar?telefono=` | `contactos.buscar` | `telefono` en E.164. Responde `[{ cliente_id, empresa_id, contacto_id, nombre, relacion, es_titular, adicional, servicios[], permisos }]` (`adicional: true` cuando el número es uno de los `telefonos_adicionales` del titular, §3.2; Iris lo trata como un contacto no verificado; desde 0.10.1, `a_cargo: true` en las filas donde el número no es titular, §3.8), dentro del alcance de empresas del token. **No** devuelve otros números ni documentos. Exige `X-Iris-Actor`. Límite por tasa: 60 por minuto por token y **20 por minuto por actor** (`429 demasiadas_busquedas` con `Retry-After`) |
| `GET /clientes/:id/contactos` | `contactos.buscar` | Los contactos del cliente, con sus teléfonos. Exige `X-Iris-Actor`, con los mismos límites |
| `POST /clientes/:id/contactos` | `contactos.gestionar` | Crea un contacto. `Idempotency-Key` obligatoria; `201` con el contacto |
| `PUT /contactos/:id` | `contactos.gestionar` | Lo edita, con `version` (`409 conflicto_version`). `Idempotency-Key` obligatoria |
| `DELETE /contactos/:id` | `contactos.gestionar` | Lo quita, con `version`. `Idempotency-Key` obligatoria. El contacto titular no se quita (`422 contacto_titular`) |

- Idempotencia como §3.1. Claves de Iris: `iris:contacto:<cliente_id>:<contacto_iris_id>` al crear, `iris:contacto:<contacto_id>:<version>` al editar o quitar.
- **Desde el token de Iris** (ajuste i de §10.5): un contacto nunca puede ser titular (`relacion: titular` o cambiar `es_titular`: `422 contacto_titular_no_permitido`), y **todo `PUT` o `DELETE` sobre un contacto con `es_titular: true`** responde `422 contacto_titular_no_permitido`, sea cual sea el campo. Iris no le da permisos por encima de los que tiene el agente que lo crea o edita (lo valida Iris; Argos audita el actor). Con `asistente_ia`, `403 actor_ia_no_permitido`.
- **Permisos iniciales** (decisión 7 del PM, §10.5): un contacto creado desde el token de Iris, aunque el titular no esté verificado en esa conversación, tiene los mismos permisos por defecto que cualquier otro (`consultar`, `reportar`, `pagar` y `avisos`). Cada alta avisa al titular por sus canales, con la opción de quitarlo. Siguen valiendo: el identificador de una llamada nunca basta (código por WhatsApp); nunca puede ser titular; sus números no cuentan como «registrados» para §7.5 y §12.4.
- **Avisos al titular:** agregar o cambiar un número en cualquier contacto avisa al titular por sus datos registrados, con el número enmascarado (`+502 ****-3344`).
- **«Teléfonos registrados»** para los códigos de §7.5 y §12.4 son solo `telefono` y `movil` **del cliente** (y confirmados, §3.4.6), nunca los de `contactos_cliente`.
- Cada cambio emite `cliente.contactos_cambiados` (§2.3), sin números.
- **Avisos de cobro:** las plantillas de cobro de §6.3 (`factura_por_vencer`, `factura_vencida`, `corte_por_falta_de_pago`) también salen a los contactos con `avisos` encendido del servicio, respetando el permiso de contacto **de cada número** (§6.4, con `telefono` opcional en E.164). Iris toma los números de su caché de contactos, nunca del cuerpo del aviso.
- **§7.6:** `GET /clientes?telefono=` sigue existiendo; para atender por chat o llamada, Iris usa `GET /contactos/buscar`, que dice además el rol y los permisos.

**En Iris** (informativo para Argos):

- Al llegar un mensaje o una llamada, Iris busca el número (en su caché o con `buscar`) y asocia la conversación al cliente **con el rol del contacto**. **Que el número coincida identifica, pero no verifica** (M6): un mensaje entrante de WhatsApp desde el número del contacto basta para `consultar`, `reportar` y `pagar`; el identificador de una llamada (SIP o centralita) **nunca** basta: ahí se pide un código de un solo uso enviado por WhatsApp a ese número. Lo reservado al titular sigue con §7.5 sobre el titular.
- **Varios clientes con el mismo número** (ajuste g de §10.5): el chat pregunta de cuál se trata **sin mostrar datos de ninguno**; solo pide una referencia que el cliente conoce (su número de servicio, o la dirección que él mismo escribe) y la compara. Nunca lista nombres ni direcciones. El número de servicio es el `codigo` de A45 (§3.7). **3 intentos por conversación y 5 por número al día** (B3); acertar asocia la conversación, pero no la verifica.
- **Contacto con varios servicios:** el chat pregunta de cuál se trata y muestra solo el `codigo` y el `nombre` de **los suyos** («S-000412 · Casa»), nunca los de otros servicios del cliente.
- Iris audita las búsquedas que inicia una persona y solo muestra los números a los roles con permiso (B2).
- El asistente y los agentes ofrecen solo lo que permiten los `permisos` del contacto; para lo reservado al titular piden su validación (A41 o §7.5).
- Si un contacto de Iris (`contacto_iris`) se une a un cliente, Iris lo crea en Argos con `POST /clientes/:id/contactos`.
- La caché se refresca con `cliente.contactos_cambiados`.

### 3.6 Prospectos registrados desde Argos (0.10.0, A44)

Una persona de Argos (oficina, caja o soporte) registra en Iris a alguien interesado en un servicio; ventas lo atiende en Iris como cualquier prospecto.

`POST https://<iris>/api/v1/integraciones/argosisp/prospectos`, con el token de §1.2 (rol `integracion_argos`) y el permiso nuevo `prospectos.registrar_integracion`. Ese token **no** lee, lista ni edita prospectos.

- **Cabeceras:** `X-Iris-Empresa` (obligatoria; fuera del alcance del token, `403 empresa_no_permitida`), `X-Iris-Actor: usuario:<id de Argos>` (obligatoria) e `Idempotency-Key = argos:prospecto:<uuid>` (obligatoria). La huella incluye el cuerpo, `X-Iris-Empresa` y `X-Iris-Actor`; Iris guarda la clave al menos **30 días**. Misma clave y misma huella: la respuesta original; misma clave con otra huella: `422 idempotency_key_reutilizada`; la primera aún en curso: `409 idempotencia_en_proceso` (se reintenta con la misma clave y los mismos bytes).
- **Credencial:** con el `token_portal`, `403`. Con el conector Argos apagado en Iris, `409 argos_apagado`.
- **Cuerpo:**

```json
{
  "origen": "oficina",
  "nombre": "Marta López",
  "telefonos": [
    { "numero": "+50255123344", "whatsapp": true, "principal": true },
    { "numero": "+50244556677", "whatsapp": true, "principal": false, "nombre": "Jorge López", "relacion": "familiar" }
  ],
  "modo": "residencial",
  "direccion": "3a calle 4-50, zona 2, Mixco",
  "zona_id": 12,
  "coordenadas": "14.6349,-90.5069",
  "plan_interes_id": 7,
  "comentario": "Pregunta por 100 Mbps",
  "cliente_id": null
}
```

| Campo | Regla |
| --- | --- |
| `origen` | `oficina`, `llamada`, `whatsapp`, `referido` u `otro` |
| `nombre`, `direccion` | Obligatorios (2 a 200 y 1 a 300) |
| `telefonos[]` | De 1 a 5, cada uno `{ numero (E.164), whatsapp (bool), principal (bool), nombre?, relacion? (`titular` \| `familiar` \| `otro`) }`, con **exactamente un** `principal`. El `principal` no lleva `relacion` o lleva `titular`; `relacion: titular` solo vale en el principal. Si no: `422 telefonos_invalidos` (con `errors[]` por posición, sin repetir el número) |
| `modo` | `residencial` o `empresarial` |
| `zona_id`, `coordenadas`, `plan_interes_id` | Opcionales. El plan debe ser de esa empresa y aplicable al modo (`422 plan_no_permitido`) |
| `comentario` | Opcional, hasta 500 |
| `cliente_id` | Solo si quien lo registra eligió un cliente existente. Si no es de la empresa de `X-Iris-Empresa`: `422 cliente_no_valido`. Si Iris no lo tiene en su instantánea y Argos no responde, crea el prospecto con `cliente_validado: false` y lo concilia después, como §11.2 |

- **Respuesta** `201 { prospecto_id, numero, estado, asignado, url_iris }` (`asignado`: bool, ya tiene vendedor). `url_iris` es un **enlace plano** al prospecto en Iris: el ticket de autologin se pide al hacer clic, con el flujo de §1.3, y nunca va dentro de la URL.
- **Qué hace Iris:** crea el prospecto con origen `argos_panel`, le asigna vendedor por zona y revisa la factibilidad, igual que con las solicitudes del portal (§11.7).
- **Errores:** `422 plan_no_permitido`, `422 telefonos_invalidos`, `422 cliente_no_valido`; `409 prospecto_duplicado { url_iris }` (sin el campo `prospecto_id`, nombre, vendedor ni **cuál número** coincidió; `url_iris` sí incluye el id del prospecto en la ruta, aceptado) si **cualquiera** de los números ya está en un prospecto abierto de la empresa: Iris suma el pedido como actividad de ese prospecto; `429` (con `Retry-After`) al pasar **30 números por minuto por actor** (cuenta números, no peticiones) o 300 por minuto por empresa.
- **Al ganar con un cliente nuevo** (§3.2): el número `principal` pasa a ser el `telefono` del cliente; los números **con `nombre`** se crean como contactos (A42, `POST /clientes/:id/contactos`) con su `relacion`; los números **sin nombre** van como números adicionales del contacto titular **dentro del mismo alta** (`telefonos_adicionales[] { numero, whatsapp }` en el cuerpo de `POST /clientes`), nunca con un `PUT` posterior (Iris no hace `PUT` sobre el contacto titular, §3.5). Ni los números adicionales ni los de los contactos cuentan como «registrados» para §7.5 y §12.4.
- **Contactos que nacen de un prospecto** (implementación de B23): `Idempotency-Key = iris:contacto:<cliente_id>:pt<id del teléfono>` (el id es el de la fila de `comercial.prospecto_telefonos`, único para cada prospecto); se crean con todos los permisos (decisión 7 del PM, §10.5) y solo sobre el servicio nuevo (`servicios: [<servicio_id>]`).
- **Al ganar con `cliente_id`** (cliente existente): Iris **no crea ni edita** el cliente: el `principal` y los números sin nombre no se escriben en él. Los números con `nombre` se crean como contactos (A42); los demás quedan solo en el prospecto.
- **En el chat** (P2): un número del prospecto que no es el principal lo ubica, pero no ve la cotización, la dirección ni el plan hasta confirmar con un código enviado al principal.
- **Webhook a Argos (opcional para Argos):** `prospecto.estado_cambiado { prospecto_id, empresa_id, de, a, motivo_codigo }` (§6.6), solo de prospectos con origen `argos_panel` y sin datos personales.
- **En Argos:** guarda solo el vínculo en su bitácora y reenvía desde su cola de pendientes con la misma clave. Permiso en Argos: `prospectos.registrar` (admin, cajero y soporte).

### 3.7 Código y nombre del servicio (0.10.0, A45)

- `servicios.codigo`: lo pone Argos, automático por empresa, legible, único e **inmutable**, con el formato `S-000412`.
- `servicios.nombre`: opcional y editable, hasta 60 caracteres («Casa», «Negocio», «Cámaras»).
- Van en `GET /servicios`, en `GET /servicios/:id` y, como `servicio_codigo` y `servicio_nombre`, en los payloads hacia Iris que ya llevan `servicio_id` (webhooks de §2.3 y respuestas). `POST /servicios` (§3.2) acepta `nombre`; el `codigo` nunca se manda.
- **`servicio_nombre` es texto del cliente:** se muestra escapado, no va en logs ni al proveedor de IA (§2.4). El `codigo` sí puede ir al asistente y a los logs.
- El «número de servicio» con que el cliente se identifica en el chat (§3.5, ajustes g y ac) es ese `codigo`, y se compara **solo contra los clientes que comparten ese número de teléfono**, nunca contra toda la empresa. Como es secuencial, se puede adivinar: lo que lo impide son los límites de 3 intentos por conversación y 5 por número al día. Acertar solo asocia la conversación; **no la verifica** (§7.5).

### 3.8 Un cliente a cargo del servicio de otro (0.10.1, A46)

«Alguien más puede estar a cargo de mi servicio, y viceversa» (decisión 12 del PM, §10.5). Ejemplo: José está a cargo del servicio de su mamá.

- **`contactos_cliente.persona_cliente_id`** (opcional): vincula el contacto con **otro cliente de la misma empresa** (José, que también es cliente). Lo pone el personal de Argos o el titular desde su portal.
    - **Desde Iris** solo se vincula si el número del contacto es el `telefono` o `movil` **verificado** de ese otro cliente (§7.5); si no, queda como contacto suelto, sin `persona_cliente_id`. **Verificado** = confirmado con un código en los últimos 12 meses (§3.4.6).
    - Cambiar los `telefonos` de un contacto con `persona_cliente_id`, o el `telefono`/`movil` de ese cliente, **vuelve a validar el vínculo**: si ya no coincide, se rompe y se avisa a los dos titulares.
    - Al vincular, Argos avisa a los dos titulares: al dueño del servicio («José quedó a cargo de S-000412») y a José, con **solo el `codigo`** del servicio, nunca su `nombre`.
    - Quitar el contacto rompe el vínculo al momento.
- **`GET /contactos/buscar`** marca con `a_cargo: true` las filas en que el número no es el titular. El chat ofrece por separado «sus servicios» y «los que tiene a cargo», mostrando solo `codigo` y `nombre` (ajuste g).
- **Verificación heredada:** la verificación de José (su identidad A41, o un código a su teléfono registrado) vale para actuar como contacto, **solo con los permisos de ese contacto** y nunca para lo reservado al titular del otro servicio (§3.5).
    - La conversación sigue asociada a José.
    - Las acciones sobre el servicio de la mamá llevan `X-Iris-Contacto` con el contacto de José en el cliente de la mamá (§7.1), y se aplica `403 servicio_fuera_del_contacto`.
- **En Argos:**
    - La ficha del cliente tiene una pestaña «Contactos» con dos listas: «A cargo de sus servicios» y «Está a cargo de».
    - En caja, si su contacto tiene `pagar`, las cuentas de los servicios que José tiene a cargo aparecen **plegadas** bajo «Cuentas a su cargo · verificar documento», con el código y el nombre del servicio. El cajero las abre marcando «Verifiqué el DPI de José»: la marca queda auditada y vale solo para esa visita a la caja. Si tiene `pagar` pero no `consultar`, el cajero escribe el monto que José indica y no ve saldo ni facturas. Nunca se muestran el documento, los otros servicios ni los contactos del dueño.
- **Evento:** `cliente.contactos_cambiados` suma `persona_cliente_id` (o null), sin números (§2.3).

### 3.9 Bodega de cuadrilla (0.11.0, A47)

Decisiones del PM en §10.6: la cuadrilla tiene una bodega compartida (lo que va en el vehículo) y cada técnico puede tener además la suya; al instalar se descuenta primero de la de la cuadrilla; el jefe de cuadrilla recibe y firma los traslados y responde por los faltantes, y cualquier miembro puede usar el material, con registro de quién lo usó.

**En Argos (A47).**

- Bodega `tipo = cuadrilla`, con `cuadrilla_id_iris`, `empresa_id`, `jefe_id` y un vínculo por miembro con su historial (desde, hasta). Una cuadrilla que atiende varias empresas tiene **una bodega por empresa**: cada evento `cuadrilla.*` lleva un solo `empresa_id`, e Iris emite uno por cada empresa que atiende la cuadrilla, con los miembros que tienen vínculo I1 en esa empresa.
- **Membresía.** Iris es la fuente. Argos la sincroniza con los webhooks `cuadrilla.creada` y `cuadrilla.actualizada` (§6.6), que llevan `empresa_id` (obligatorio), `version`, `miembros[] { usuario_id, id_externo (o null si no está vinculado por I1) }` y `jefe_usuario_id`.
    - Argos solo aplica una `version` **mayor** que la última que aplicó (no usa `ocurrido_en` para el orden). Para conciliar, lee `GET <iris>/api/v1/integraciones/argosisp/cuadrillas/:id?empresa_id=` (con el `iris_pat_` del panel de §1.2, no el `token_portal`, y el permiso nuevo `cuadrillas.leer_integracion` del rol `integracion_argos`; solo responde para las empresas del alcance del conector, fuera de él `404`; `{ cuadrilla_id, empresa_id, version, activa, miembros[], jefe_usuario_id }`).
    - Solo acepta como miembro un `id_externo` **activo**, con bodega o rol de técnico en la empresa de la cuadrilla y dentro del alcance del usuario técnico Iris (§1.1). Los demás los ignora y los muestra en su panel como `miembro_rechazado`.
    - Un miembro sin vínculo I1 (`id_externo: null`) no consume de la bodega de la cuadrilla.
    - `jefe_usuario_id` debe estar en `miembros[]` con `id_externo` no nulo; si no, Argos lo ignora y la cuadrilla queda **sin jefe** (y los traslados esperan).
- **`GET /inventario/mi-bodega`** con `tecnico` (§3.3) devuelve `bodegas[]`: la de la cuadrilla en esa empresa (`cuadrilla: true`, con su `jefe { id_externo, nombre }` o `null`) y la personal si la tiene. Se mantiene `nombre` (A22).
- **Quién actúa sobre una bodega de cuadrilla.** En consumos, comodatos, devoluciones y recepciones sobre una bodega de cuadrilla:
    - `X-Iris-Actor: usuario:<id>` (§7.1) es **obligatoria** (`400 cabecera_iris_requerida`);
    - ese usuario debe ser miembro y su `id_externo` el mismo de `tecnico.id_externo` (`403 actor_no_coincide`);
    - `tecnico` se identifica **solo por `id_externo`**, nunca por `correo` (§3.3).
- **Qué bodega (antes de mirar existencias).** Con `bodega_id`, Argos solo acepta la bodega personal del técnico o una bodega de cuadrilla de la que sea miembro, y solo en la empresa del servicio; si no, `403 no_es_miembro` **antes de consultar existencias**: la respuesta nunca depende del stock.
- **Consumos** con `tecnico` (con `fecha` y `recibido_en_iris` en el cuerpo, §3.3): sin `bodega_id`, primero la de la cuadrilla y después la personal. Sin existencia: `409 sin_existencia { bodegas[] }`, solo con las bodegas a las que el técnico tiene acceso y las cantidades de las líneas pedidas, nunca otras.
- **Comodatos y devoluciones** (`POST /servicios/:id/equipos`, `…/devolver`): **no llevan `bodega_id`**; la regla se aplica a la bodega donde está la unidad: `409 unidad_en_otra_bodega` salvo que esté en su bodega personal o en una cuadrilla de la que sea miembro.
- **Membresía en el tiempo.** Argos acepta un movimiento si el actor era miembro en la `fecha` (u `ocurrido_en`) de la operación, que debe ser anterior a su recepción en Iris: desde 0.12.0, con `recibido_en_iris` y la ventana de §3.10 (`recibido_en_iris − 72 h ≤ fecha ≤ recibido_en_iris + 5 min`). Si llega después de que lo quitaran, lo marca `fuera_de_membresia` (y responde `403 no_es_miembro`). Cada movimiento guarda `actor_tecnico`, que sale de `X-Iris-Actor`, **nunca del cuerpo**.
- **Recepción de traslados a una bodega de cuadrilla** (`POST /inventario/traslados/:id/recibir`, §3.3):
    - Argos evalúa quién era el jefe con su historial de membresía en el momento de `recibido_en`, y exige además que ese usuario siga siendo miembro al procesar; `recibido_por.id_externo` y `X-Iris-Actor` deben ser ese jefe. Si no: `403 solo_jefe_recibe`.
    - Si el jefe cambió mientras el traslado venía en camino, recibe el nuevo. Sin jefe, el traslado espera.
    - El faltante queda a cargo de la bodega de la cuadrilla.
- Los eventos `inventario.traslado_enviado`, `inventario.traslado_recibido` e `inventario.traslado_cancelado` (§2.3) suman `cuadrilla_id` (o null).

**En Iris (B26, informativo para Argos).**

- La cuadrilla tiene `jefe_usuario_id`, que se cambia con la acción `POST /cuadrillas/:id/asignar-jefe` (permiso `cuadrillas.asignar_jefe`, solo `admin` y `supervisor`; nadie se asigna a sí mismo: `403 autoasignacion`). El jefe debe ser miembro con vínculo I1: si no, `422 jefe_no_miembro`. Si el jefe sale de la cuadrilla o pierde el vínculo I1, la cuadrilla queda sin jefe.
- Iris emite `cuadrilla.actualizada` con `miembros[]` completo y `version` nueva de **cada cuadrilla afectada** cuando cambia `tecnico.cuadrilla_id`, `tecnico.activo` o el vínculo I1, o cuando se desactiva el usuario.
- Al quitar a un miembro, Iris corta en su servidor, en ese momento, los lotes nuevos de esa persona para esa bodega.
- En la app de campo, «Mi bodega» tiene dos pestañas, «Cuadrilla» y «Mía». Al cerrar la orden, el material sale por defecto de la cuadrilla y se puede cambiar; la cola local guarda el `bodega_id` elegido para que el reintento sea determinista. Iris valida en su servidor, al recibir el lote, que el `bodega_id` sea la personal del técnico o una de sus cuadrillas.
- Un `409 sin_existencia` o un `403 no_es_miembro` al sincronizar se muestran como conflicto (regla en `contratos/sync.md`); Iris **nunca** cambia de bodega por su cuenta.
- **Desde 0.14.0 (§10.10).** Lo que registra un técnico desde la app de campo:
    - **consumos:** `tecnico` + `bodega_id` + `X-Iris-Actor`, también desde su bodega personal;
    - **comodatos y devoluciones:** `tecnico` + `X-Iris-Actor`, sin `bodega_id` (como hasta ahora).
    - En todos, `X-Iris-Actor` es el usuario autenticado que envió el lote a `POST /sync/lote` (nunca un campo de la operación) y `tecnico.id_externo` es el vínculo I1 de ese actor; Iris lo valida en su servidor, junto con que la bodega personal sea de la empresa de la orden.
    - Con contrato ≥ 0.14.0, Argos guarda `actor_tecnico` también en la bodega personal y conoce el vínculo de cada técnico por los webhooks `tecnico.argos_vinculado` / `tecnico.argos_desvinculado` (§6.6), tenga cuadrilla o no. En las peticiones que traen `tecnico`, un actor sin vínculo conocido responde `403 actor_desconocido` (mientras dura la carga inicial de vínculos, `409 vinculos_en_carga`, §6.6); uno cuyo vínculo no coincide con `tecnico`, `403 actor_no_coincide`, también en la bodega personal. Con contrato de 0.11.0 a 0.13.0, Iris manda lo mismo y Argos solo lo exige en las bodegas de cuadrilla.
    - **Consumo por actor.** Con un Argos que informó contrato ≥ 0.14.0, el consumo de una orden sale uno por actor y bodega, con `Idempotency-Key = iris:consumo:<orden_id>:<usuario_id>:<bodega_id>`; con uno anterior, uno por orden, como hasta ahora. Con un Argos ≥ 0.14.0, `tecnico` va por línea (consumo, comodato y devolución) y es el I1 del `usuario_id` de esa línea.
    - Despacho desde el panel solo usa bodegas `tipo = bodega` (con `bodega_id` y sin `tecnico`); una bodega de cuadrilla o personal requiere al técnico.
- **Errores de negocio sin reintento (0.14.0).** Iris reconoce los `403` de negocio de esta sección y de §3.10 por la URN completa del `type`: `urn:argosisp:problema:no_es_miembro`, `…:actor_no_coincide`, `…:solo_jefe_recibe`, `…:empresa_no_permitida`, `…:actor_ia_no_permitido` y `…:actor_desconocido`. Son definitivos en Argos: Iris no los reintenta solo y los muestra como conflicto a una persona.
- «Recibir traslado» solo aparece al jefe; los demás miembros ven el traslado «en camino». Iris rechaza en su servidor, con `403`, una recepción de alguien que no es el jefe, antes de encolarla; un `403 solo_jefe_recibe` de Argos se muestra como conflicto para el supervisor.

### 3.10 Equipos del cliente: cambio, revisión, garantía y no recuperados (0.12.0, A48)

Pedido del PM: «Iris con la misma consistencia de equipos que Argos» (§10.7). **Argos es el dueño de las unidades y de su estado**; Iris pide los movimientos y los muestra.

**Estados de la unidad:** `disponible`, `en_comodato`, `por_recuperar`, `revision`, `garantia`, `dada_de_baja`, `no_recuperada`. Una unidad en `revision`, `garantia`, `dada_de_baja` o `no_recuperada` **nunca se puede instalar**.

**Reglas comunes a las rutas de esta sección.**

- `X-Iris-Actor: usuario:<id>` obligatoria (§7.1; `400 cabecera_iris_requerida`); con `asistente_ia`, `403 actor_ia_no_permitido`. Desde 0.14.0, en `…/cambiar` y `…/no-recuperado` es **quien registró la acción**: el usuario autenticado que envió el lote en el que llegó la operación a `POST /sync/lote`, nunca un campo de la operación, y no el técnico que tenga la orden al enviarse a Argos (una orden reasignada no cambia el autor). Si en un dispositivo cambia la sesión, las operaciones pendientes de la sesión anterior no se envían con la nueva: quedan bloqueadas hasta que vuelva su autor. Argos no conoce la asignación de la orden.
- **Alcance:** el servicio debe estar dentro de las empresas del alcance del token (`403 empresa_no_permitida`); también vale para servicios antiguos creados en Argos. Argos registra y audita `origen_id`: la orden queda registrada para ese servicio en la primera llamada aceptada. Iris valida en su servidor que el servicio sea el de la orden y que la orden esté asignada al actor, con la excepción que sigue.
- **El técnico que dejó la orden (0.14.0).** Excepción a `contratos/sync.md` §3 para `equipo.cambiar` y `equipo.no_recuperado`: Iris acepta la operación si, según el historial de asignación que guarda con la hora de su servidor, el actor tenía la orden en la `fecha`, a su nombre o como miembro de su cuadrilla, **y** el lote llega antes de 12 h, por la hora del servidor, contadas desde lo primero que ocurra: que le quitaran la orden o que lo quitaran de la cuadrilla. En un `cambiar` cuya unidad que entra viene de una bodega de cuadrilla, manda el corte inmediato de §3.9. Pasado el plazo, `orden_reasignada`, cualquiera que sea la `fecha`.
- **Cuándo salen (0.14.0).** `equipo.cambiar` y `equipo.no_recuperado` son tipos de operación del lote (`contratos/sync.md` §2); su `id` (UUID v7) es el `operacion_id` o el `intento_id` de la clave. Iris los envía a Argos **al aplicarse**, no al cerrar la orden; `orden.cerrada.equipos[]` solo los resume. Un intento sale solo cuando su foto está `completo` (el estado del adjunto en Iris, `contratos/sync.md` §4). Si la foto tarda más de 72 h desde la `fecha` en quedar `completo`, el intento no sale y queda como conflicto para una persona (`foto_fuera_de_ventana`).
- **Día.** «Al día» es el día calendario de America/Guatemala de la `fecha`, en los dos lados.
- **Fecha.** `fecha` es la del teléfono; Iris manda también `recibido_en_iris`, la hora de su servidor al aceptar el lote. Argos exige `recibido_en_iris − 72 h ≤ fecha ≤ recibido_en_iris + 5 min` (si no, `422 validacion`) y evalúa la membresía de §3.9 con `fecha`. Iris rechaza en su servidor una `fecha` anterior a la asignación de la orden.
- **Conflictos.** Un conflicto (por ejemplo, la unidad ya cambió de estado: `409 estado_unidad_cambiado`) se muestra en la cola de Iris y **no se reintenta solo**. Si una petición tuvo éxito y se quedó sin respuesta, la corrección se concilia leyendo `GET /servicios/:id/equipo`, no reenviando.
- **Textos libres.** `nota` y `cobro_propuesto.nota`, hasta 500 caracteres; se muestran escapados y no van en eventos ni en logs.

**Cambiar un equipo** — `POST /servicios/:id/equipos/cambiar` (`servicios.cambiar_equipo`), **en una sola transacción**, con `Idempotency-Key = iris:cambio:<orden_id>:<sale_unidad_id>` (desde 0.14.0, con un Argos que informó contrato ≥ 0.14.0, `iris:cambio:<orden_id>:<sale_unidad_id>:<operacion_id>`, con el `id` de la operación `equipo.cambiar`; la huella incluye `X-Iris-Actor`, como en §7.7):

```json
{
  "sale": { "unidad_id": 812 },
  "entra": { "serie": "ZTEGC8A1B2C3" },
  "motivo": "danado",
  "destino_sale": "revision",
  "cobro_propuesto": { "motivo": "golpe", "nota": "Carcasa rota" },
  "foto_ref": "0193f1c2-7d4e-7a10-9b2c-5e6f7a8b9c0d",
  "nota": null,
  "fecha": "2026-11-21T16:02:00Z",
  "recibido_en_iris": "2026-11-21T16:40:11Z",
  "origen_tipo": "iris_orden",
  "origen_id": "9130"
}
```

- `motivo`: `danado`, `actualizacion`, `garantia` u `otro`. `destino_sale`: `funciona`, `revision`, `garantia` o `danado`; con `motivo` `danado` o `garantia`, `destino_sale: funciona` responde `422 validacion`. `entra`: `serie`, `mac` o `unidad_id`. `cobro_propuesto`, `foto_ref` (id opaco del adjunto, como en «No recuperado») y `nota` son opcionales; desde 0.14.0, con `cobro_propuesto`, `foto_ref` es obligatoria (`422 validacion`).
- **La que sale** debe estar en comodato en ese servicio; si no, `409 unidad_no_es_del_servicio`. Queda, con su estado, en la **bodega de donde salió la que entra**.
- **La que entra** sigue la regla de A47 (§3.9): solo de la bodega personal del técnico o de una cuadrilla de la que es miembro, comprobado **antes de mirar existencias** (`403 no_es_miembro`, `409 unidad_en_otra_bodega`), y debe estar `disponible` (si no, `409 estado_unidad_cambiado`).
- Argos devuelve la que sale con su destino y asigna la que entra. Con `destino_sale` `danado` o `garantia`, la unidad **no se da de baja** en el momento: queda en `revision` o en `garantia`, y la decisión final es de Argos. Con `funciona`, vuelve `disponible`.
- **El técnico no decide cobros.** `cobro_propuesto` va **sin monto** y entra en la misma cola de aprobación de Argos que «Equipo no devuelto»; lo aprueba una persona con `inventario.cobrar_equipo`. Desde 0.14.0, Argos verifica el cambio con la misma ruta que los intentos (abajo), y una propuesta de cobro de un cambio sin verificar no entra a la cola.
- Emite `inventario.comodato_devuelto`, `inventario.comodato_asignado` y `inventario.equipo_cambiado` (§2.3). `200 { sale: { unidad_id, estado }, entra: { unidad_id, estado } }`.

**Leer una unidad** — `GET /inventario/unidades/:id` (`inventario.ver_unidad`): serie, MAC, modelo, estado e historial de estados (fecha, de, a, motivo). En el historial, `servicio` solo aparece para el servicio actual de la unidad y para el de la orden en curso. Solo unidades de las empresas del alcance del token (fuera, `404`), **sin costos, proveedor ni número de RMA**: para Iris basta «en garantía». En Iris, solo la piden los roles con `inventario.ver_unidad` y la lectura se audita. `GET /servicios/:id/equipo` (§12.3) suma `estado` de la unidad.

**Revisión y garantía** las resuelve el personal de Argos (funciona → `disponible` usado; RMA con el proveedor: reparada, reemplazada o rechazada; o `dada_de_baja`). Iris solo las ve, por `inventario.unidad_estado_cambiado` (§2.3).

**No recuperado** — `POST /servicios/:id/equipos/:unidad_id/no-recuperado` (`servicios.cambiar_equipo`), con `Idempotency-Key = iris:no_recuperado:<orden_id>:<unidad_id>` (desde 0.14.0, con un Argos que informó contrato ≥ 0.14.0, `iris:no_recuperado:<orden_id>:<unidad_id>:<intento_id>`, con el `id` de la operación `equipo.no_recuperado`; la huella incluye `X-Iris-Actor`, como en §7.7; una orden puede llevar más de un intento por unidad, en días distintos), cuerpo `{ motivo: cliente_ausente | se_nego | equipo_perdido | otro, foto_ref (obligatoria con cualquier `motivo` desde 0.14.0; antes, solo con `se_nego` y `equipo_perdido`), ubicacion: { lat, lng, precision_m }, nota?, fecha, recibido_en_iris, origen_tipo: "iris_orden", origen_id }`.

- Registra un **intento** de recuperación; `intento_n` lo calcula Argos. Un intento verificado deja la unidad en `por_recuperar` (o la pasa a ese estado); uno sin verificar no la cambia (abajo).
- `foto_ref` es **obligatoria** con `se_nego` y `equipo_perdido` (`422 validacion`); **desde 0.14.0, con cualquier `motivo`** (con `cliente_ausente`, la foto de la fachada o del acceso). Es el **id opaco** del adjunto de la orden en Iris: Argos la abre por autologin (§1.3) con el permiso del usuario de Argos, nunca con una URL pública o firmada guardada. La foto se borra con la retención de la orden en Iris.
- `ubicacion` es obligatoria: la del intento, que Iris exige tomada en sitio.
- Como máximo **1 intento por unidad al día** desde Iris: `429 demasiados_intentos`. Desde 0.14.0 es de negocio: en `…/no-recuperado`, Iris lo reconoce por el `type` exacto `urn:argosisp:problema:demasiados_intentos` (no por el status), y es definitivo **aunque venga con `Retry-After`**: no lo reintenta solo y lo muestra como conflicto. Los demás `429` con `Retry-After` (topes de peticiones) se siguen reintentando. Antes de responder `429 demasiados_intentos`, Argos verifica el intento que ocupa el cupo: si sale `404`, `no_enviado_por_iris` o `no_coincide`, ese intento no cuenta y acepta el nuevo. Si esa verificación no es concluyente (`envio_en_curso`, `foto_pendiente`, `5xx`, timeout o `429`), Argos responde `503` con `Retry-After`, que se reintenta, y nunca `429 demasiados_intentos`. Argos cuenta el cupo por el día de la `fecha` en America/Guatemala, no por el día en que le llega. Iris muestra como «posible intento ajeno» un `demasiados_intentos` de una unidad sin intento propio ese día. Iris aplica el mismo límite en su servidor, por la hora del teléfono y por la de su servidor, serializado por unidad.
- **Validación en Iris (0.14.0).** Antes de encolar un intento, Iris exige (lo de la foto vale también para la foto de un cambio):
    - ubicación con `precision_m` ≤ 200, a 500 m o menos de las coordenadas del servicio y de la última posición del técnico en los 30 min anteriores a la `fecha` del intento, contando solo las posiciones que Iris recibió antes de `recibido_en_iris` (si no, `422 ubicacion_fuera_de_sitio` en Iris). Sin ninguna posición en esos 30 min, o con un servicio sin coordenadas, `422 ubicacion_sin_referencia`;
    - la foto registrada por el mismo actor, no rechazada y no usada en otra operación (ni intento ni cambio), tomada después de la llegada: el último `orden.llegar` **del mismo actor** en la orden, el mismo día (America/Guatemala) de la `fecha` y anterior a ella;
    - `tomada_en` es el `tomadoEn` del adjunto (`contratos/sync.md` §4), obligatorio en las fotos de intento o cambio y acotado por el `recibido_en` del servidor.

    Esta validación se basa en datos del teléfono: detiene errores, no a una app modificada. **Argos no revalida distancia ni frescura de la foto**: mantiene foto y ubicación obligatorias y un intento por unidad al día, verifica el intento con Iris (abajo) y su pantalla indica que «la ubicación y la autoría las afirma Iris».
- **Verificación del intento o del cambio, de servidor a servidor (0.14.0).** Argos verifica la operación completa, no solo la foto. Guarda la `Idempotency-Key` con la que recibió cada intento y cada cambio, y llama a `GET <iris>/api/v1/integraciones/argosisp/intentos-recuperacion/verificacion?empresa_id=&idempotency_key=` con esa clave tal cual, el `iris_pat_` del panel de §1.2 (nunca el `token_portal`) y el permiso `adjuntos.verificar_integracion` (rol `integracion_argos`). La ruta acepta claves de `cambio` y de `no_recuperado`.
    - Iris responde `200 { verificado, motivo }`. `verificado` es `true` solo si Iris recibió de Argos un `2xx` (original o con `Idempotent-Replayed`) para esa clave, enviada a ese conector con los bytes que guardó, y si además la foto (siempre en un intento; en un cambio, si la lleva) está `completo`, es de esa orden, es del mismo autor, es posterior a su llegada y no se usó en otra operación (ni intento ni cambio). Como la huella incluye `X-Iris-Actor`, ese `2xx` prueba que lo que guardó Argos es lo que mandó Iris.
    - `motivo`, de lista cerrada (`null` si `verificado` es `true`):
        - `envio_en_curso`: Iris la está enviando o no tuvo respuesta; Argos reintenta;
        - `no_enviado_por_iris`: Iris conoce la clave pero nunca la mandó a ese conector;
        - `no_coincide`: lo enviado no coincide. Un `422 idempotency_key_reutilizada` de Argos a esa clave en `…/no-recuperado` o `…/cambiar` la deja en `no_coincide` y es una alerta de seguridad para el admin de Iris;
        - `foto_pendiente` y `foto_invalida`.
    - `404` si Iris no conoce la clave o si está fuera de las empresas del alcance del conector: la misma respuesta para los dos casos. Argos deja la operación sin verificar con `desconocido_en_iris` y no la reintenta.
    - Nunca devuelve la imagen, el autor ni las horas. Iris audita cada consulta con actor `token` y la limita a 60 consultas por minuto por conector y 20 por clave al día (`429` con `Retry-After`).
- **Reintentos de la verificación (0.14.0).** Argos reintenta con `envio_en_curso`, `foto_pendiente`, `5xx`, timeouts, `401`/`403` y `429` (según su `Retry-After`); con `401`/`403`, además avisa al admin en el registro del sistema. Mientras tanto, la operación queda «pendiente de verificar». Argos vuelve a verificar cuando una persona abre la pantalla de declaración.
- **Intento sin verificar (0.14.0).** No cambia la unidad a `por_recuperar`, no avisa al titular y no cuenta para el mínimo de técnicos distintos. Queda solo como registro, con su `motivo` visible.
- **Aviso al titular (0.14.0).** Solo por intentos verificados, como máximo uno por servicio al día (aunque haya intentos sobre varias unidades), con la serie enmascarada: «Intentamos recuperar el equipo …1A2B; si ya lo entregaste, comunícate».
- **Declarar la unidad `no_recuperada` y el cobro «Equipo no devuelto» los decide una persona en Argos** con permiso de admin, nunca el técnico. Su pantalla muestra todos los intentos con su actor, foto, ubicación y verificación, y exige un **mínimo de intentos verificados de técnicos distintos** (configurable por empresa; 2 de fábrica) o un contacto con el cliente registrado por una persona en el panel de Argos. Ni una gestión de cobro de Iris (§3.12) ni un intento sin verificar cuentan como contacto.
- Desde 0.12.0, Iris **ya no llama** a `POST /servicios/:id/equipos/:unidad_id/perdida`, y Argos lo rechaza con el token de Iris (`403`); la migración de tokens retira `inventario.declarar_perdida_comodato`.

**Errores.**

| Status | Código | Dónde |
| --- | --- | --- |
| 409 | `unidad_no_es_del_servicio` | `cambiar` (la que sale), `no-recuperado` |
| 403 | `no_es_miembro` | `cambiar` (la que entra) |
| 409 | `unidad_en_otra_bodega` | `cambiar` (la que entra) |
| 409 | `estado_unidad_cambiado` | `cambiar` (la que entra no está `disponible`, o la que sale ya no está en comodato), `no-recuperado` |
| 403 | `actor_ia_no_permitido` | Todas, con `asistente_ia` |
| 400 | `cabecera_iris_requerida` | Sin `X-Iris-Actor` |
| 422 | `validacion` | `motivo`, `destino_sale` (`funciona` con `danado` o `garantia`), `fecha` fuera de la ventana, `foto_ref` o `ubicacion` que faltan |
| 429 | `demasiados_intentos` | `no-recuperado`: más de 1 por unidad al día; desde 0.14.0, definitivo aunque venga con `Retry-After` |
| 403 | — | `…/perdida` con el token de Iris (desde 0.12.0) |
| 403 | `empresa_no_permitida` | Todas: el servicio es de una empresa fuera del alcance del token |
| 403 | `actor_desconocido` | Las de §3.9 que traen `tecnico`, con contrato ≥ 0.14.0: el actor no tiene vínculo conocido (§6.6) |
| 422 | `validacion` | `cambiar` con `cobro_propuesto` y sin `foto_ref` (desde 0.14.0) |
| 404 | `no_encontrado` | `GET /inventario/unidades/:id` fuera del alcance |
| 422 | `validacion` | `no-recuperado` sin `foto_ref` (desde 0.14.0, con cualquier `motivo`) |
| 404 (Iris) | `no_encontrado` | `GET <iris>/…/intentos-recuperacion/verificacion`: clave desconocida o fuera del alcance del conector (0.14.0) |
| 409 | `contrato_regresivo` | `PUT /integraciones/iris/contrato` con una versión menor que la última aceptada (0.14.0, §3.3) |
| 422 | `contrato_no_soportado` | `PUT /integraciones/iris/contrato` con una versión mayor que el `contrato` que habla Argos; no se registra (0.14.0, §3.3) |
| 409 | `vinculos_en_carga` | Las de §3.9 que traen `tecnico`, mientras Iris no cierre la carga inicial de vínculos; se reintenta (0.14.0, §6.6) |
| 503 | — | `no-recuperado`: la verificación del intento que ocupa el cupo no fue concluyente; con `Retry-After`, se reintenta (0.14.0) |
| 429 (Iris) | `demasiadas_peticiones` | `GET <iris>/…/intentos-recuperacion/verificacion`: 60 por minuto por conector o 20 por clave al día, con `Retry-After` (0.14.0) |

**En Iris (B27, informativo para Argos).**

- **Orden `cambio_equipo`:** escanear la unidad que sale y la que entra; elegir el motivo y si la que sale funciona (por defecto, «a revisión»); foto y cobro propuesto opcionales (desde 0.14.0, foto obligatoria con cobro propuesto). Sin señal, se encola con su clave.
- **Orden `retiro`:** «Recuperado» devuelve con su condición (`funciona` o a `revision`); «No se pudo recuperar» guarda el motivo, la foto (obligatoria siempre desde 0.14.0; antes, si se negó o se perdió) y la ubicación, y permite reprogramar otro intento.
- La orden y la ficha del cliente muestran el estado de cada equipo y su historial, sin costos.
- **Con Argos < 0.12.0**, «No se pudo recuperar» queda pendiente en Iris para una persona con `ordenes.declarar_perdida` (rol supervisor); nunca sale solo desde el cierre.

### 3.11 Caídas repetidas y falla general (0.13.0, A49, A50)

Decisiones del PM en §10.9.

**Caídas repetidas por servicio (A49).**

- Argos emite `servicio.caidas_repetidas` (§2.3, A6) con el conteo (`sesiones`) y la ventana (`desde`, `hasta`, `ventana_minutos`). Desde 0.13.0, con contrato ≥ 0.13.0, su umbral es **3 caídas en 60 min**, configurable por empresa en Argos (confirmado por Argos).
- **Iris decide** si abre un ticket, con su propio umbral por empresa (de fábrica, 3 en 60 min). Abre como máximo **uno por servicio** mientras siga abierto el de esa categoría; si ya hay uno abierto, el aviso entra como nota en ese ticket.
- **Si el servicio ya está en una falla general abierta**, no abre un ticket: agrega una nota en el ticket general.
- **Posible falla general:** si en una empresa hay más de N tickets de caídas en 10 min (20 de fábrica, configurable), Iris deja de abrirlos, los agrupa en un ticket de «posible falla general» y avisa a supervisión. Cuando llega la falla general, los tickets ya abiertos de esos servicios pasan a ser sus hijos.

**Falla general (A50).**

- Eventos Argos → Iris `red.falla_general_abierta` y `red.falla_general_cerrada` (§2.3): `incidente_id`, `empresa_id`, `alcance` (`router`, `olt`, `pon`, `zona`), `alcance_id`, `servicios_afectados { cantidad, ids[] }` (hasta **5000** ids), `desde` y `hasta` (o null mientras sigue abierta). **Un evento por empresa afectada**, con el mismo `incidente_id` y distinto `empresa_id`.
- Con más de 5000 afectados, Iris pide el resto con `GET /api/v1/incidentes-red/:incidente_id/servicios?empresa_id=&cursor=&limit=` **en Argos** (permiso `incidentes_red.leer` del usuario técnico Iris, §1.1; sin `X-Iris-Actor`; Argos la audita con actor `token`). Devuelve solo ids de las empresas del alcance: `{ data: [servicio_id…], nextCursor }`, con `limit` hasta 1000. Un incidente fuera del alcance responde `404`.
- **Seguridad:** el evento nunca lleva nombres ni direcciones, solo ids; Iris solo lo acepta por el webhook firmado de Argos (§2.3). Si Iris no conoce un id, lo lee con `GET /servicios/:id` antes de vincularlo; si es de otra empresa o no existe, lo descarta (uno por uno; el resto del evento se procesa). Argos registra quién abrió a mano un incidente y con qué motivo.
- **En Iris:**
    - un solo ticket general por `incidente_id`, de forma idempotente, con los afectados vinculados; si el mismo `alcance_id` vuelve a abrir un incidente antes de 30 min, se reusa el ticket general;
    - mientras siga abierto, absorbe solo los tickets **de conectividad** de servicios que estén en `servicios_afectados` y que se hayan creado **después de `desde`** (nacen hijos, como los maestros de Horus); la bandeja puede responder con el aviso de falla general;
    - si Horus ya avisó `horus.router_estado_cambiado` del mismo router, el ticket general se vincula al maestro de Horus y no se crea otro;
    - el aviso automático «hay una falla en tu zona» solo va a números asociados a un cliente, sin decir el servicio ni la dirección;
    - al cerrarse, Iris cierra el ticket general; los tickets hijos **no se cierran solos**: pasan a «verificar con el cliente», o se resuelven solo si Horus o la sesión PPP confirman que el servicio está arriba;
    - cada cliente recibe **un solo aviso de cierre por incidente**, y como máximo 1 cada 6 h, con las reglas de §6.5 (permiso, baja, ventana o plantilla).

### 3.12 Gestiones de cobro (0.13.0, A51)

`POST /api/v1/clientes/:id/gestiones-cobro` en Argos (`clientes.registrar_gestion_cobro`), con `X-Iris-Actor` obligatoria (§7.1) e `Idempotency-Key = iris:gestion:<gestion_id>`, donde `gestion_id` es un UUID v7 que Iris genera al registrar la gestión:

```json
{
  "tipo": "llamada",
  "resultado": "promete_pagar",
  "ocurrido_en": "2026-11-21T16:02:00Z",
  "origen_tipo": "conversacion",
  "origen_id": "c-8812",
  "servicio_id": 4410
}
```

- `tipo`: `llamada`, `mensaje`, `visita` o `promesa`. `resultado` es una lista cerrada: `contesto`, `no_contesto`, `numero_equivocado`, `promete_pagar`, `rechaza`, `pago_informado`, `visita_sin_contacto`. `origen_tipo`: `conversacion`, `orden` o `llamada`. `servicio_id` opcional.
- `ocurrido_en` entre ahora − 7 días y ahora + 5 min; si no, `422 validacion`.
- **Sin texto libre, montos ni teléfonos.** Una gestión es **solo informativa**: nunca cambia estados, cortes ni promesas en Argos.
- Con `asistente_ia`, `pago_informado` y `promete_pagar` solo valen si hay una promesa o un comprobante real en esa conversación (lo valida Iris antes de mandarla).
- Si quien habló es un contacto (§3.5), va `X-Iris-Contacto` (§7.1).
- Argos valida que el cliente sea de una empresa del alcance (`403 empresa_no_permitida`) y que el servicio sea suyo (`422 validacion`).
- Idempotencia como §3.1: la misma clave con otro cuerpo responde `422 idempotency_key_reutilizada`.
- Iris solo la manda cuando la conversación tiene el **cliente confirmado**, nunca para un número sin cliente.
- `201 { gestion_id }` (el de Argos).

## 4. Versionado

Semver. Esta versión 0.14.3 es un parche aditivo sobre la 0.14.2, que es un parche sobre la 0.14.1, que es un parche aditivo sobre la 0.14.0, que es una minor sobre la 0.13.0, no aditiva (transición en §4.5); la 0.13.0 es una minor aditiva sobre la 0.12.1, que es un parche aditivo sobre la 0.12.0, que es una minor aditiva sobre la 0.11.0, que es una minor aditiva sobre la 0.10.1, que es aditiva sobre la 0.10.0, y esta sobre la 0.9.0 (ver «Gobierno»); todas están acordadas con Argos y decididas por el PM, y pendientes de implementar en los dos lados. La 0.8.1 está espejada (`iris#222` y `argos-isp#258`) y salió en Argos 1.21.0. Desde 1.0.0, solo cambios compatibles en versiones menores.

- **0.15.1** (26 nov 2026; **propuesta** de Iris por pedido del PM, con el diseño de Argos (§10.15), parche aditivo, pendiente de la PR espejo en `argos-isp`; §13.4): cambiar el tipo de servicio (A56 / B36). `cambiar-plan` en `pendiente_instalacion` acepta un plan de otro tipo (con `router_id` si agrega internet o IPTV). Ruta nueva `POST /servicios/:id/cambiar-tipo` (`servicios.cambiar_tipo`) para servicios activos, con prorrateo de Argos. `422 tipo_sin_router` queda solo para las rutas de red. Errores `router_requerido` y `servicio_no_activo`. Bloque C: webhook `aviso.bloqueado` (un aviso `auto` cuyo canal deja de servir antes del primer envío; Argos lo reenvía por sus canales) y motivo `baja` en `aviso.fallido` (§6.6, §13.3).
- **0.15.0** (26 nov 2026; **propuesta** de Argos, aceptada por Iris con ajustes (§10.14), minor aditiva, pendiente de la PR espejo en `iris`; §13): **Bloque A (A53 / B33):** `tipo_servicio` y `componentes[]` en `GET /servicios/:id`, en la lista del cliente y en los webhooks `servicio.creado` y `servicio.config_cambiada` (y `catalogo.cambiado` de plan); `router_id` puede ser null en un servicio de cable. **Bloque B (A54 / B34):** webhooks `servicio.corte_manual_solicitado` y `servicio.corte_manual_cancelado` (Argos → Iris) y `corte_manual.estado_cambiado` (Iris → Argos), con `version` por solicitud, orden de trabajo nueva `corte_catv` / `reconexion_catv` y estado `no_realizada`. **Bloque C (A55 / B35):** `POST /mensajeria/avisos` acepta `canal_preferido: "auto"` y `canales_permitidos[]`; códigos nuevos `corte_proximo`, `promesa_por_vencer`, `debito_fallido`, `debito_desactivado`, `suscripcion_suspendida`, `suscripcion_reactivada`; variable opcional `concepto` en `factura_por_vencer` y `factura_vencida`; `suscripcion_id` y `recordatorio` opcionales en el cuerpo; `aviso.entregado` con el `canal` real; `contacto.baja` también con `correo` y `telegram`; `capacidades.mensajeria[]` por empresa (`entrega_auto`, `canales: [{ canal, codigos[] }]`) y marcas `tipo_servicio` y `corte_manual`, todas dentro de las `capacidades` de §4.3, que Argos lee solo de `GET …/capacidades`. Deroga, solo para los avisos con `auto`, la frase de §6.1 «Argos solo pide avisos con `canal_preferido: "whatsapp"`». Sin datos personales nuevos. Compatibilidad en §4.6.
- **0.14.3** (22 nov 2026; parche aditivo, acordado con Argos, §10.13; espejo: PR enlazada en `argos-isp`): §4.3 `capacidades.equipos[]` con `{ empresa_id, estado_equipo, wifi, bienvenida, cobro_equipo }` por empresa (con contrato de Iris < 0.9.0, todas `false`; sin el campo, Iris actúa como en la 0.14.2), y una llamada sin su capacidad responde `422 capacidad_no_disponible` (solo a conectores ≥ 0.14.3, después de comprobar el alcance del token; `cobro_equipo` solo controla el cobro de la instalación); §12.2 con la orden cerrada, `tecnico` = el `tecnico_id` de `orden.cerrada` si tiene vínculo I1 en la empresa del servicio, si no va sin `tecnico` y la unidad tiene que estar ya asignada al servicio (Argos la busca también por `serie` o `mac`); el `409 aprovisionamiento_en_curso` suma `extensions.iniciado_por` (`argos` \| `iris`) e Iris no sigue un aprovisionamiento ajeno, lo consulta con `GET /aprovisionamientos/:id` (limitado a las empresas del token; `origen_tipo: argos_panel` si lo inició Argos) solo para mostrar `estado`, `causa`, `iniciado_por` y `actualizado_en` (Argos recorta la respuesta de un aprovisionamiento ajeno), sin webhooks del ajeno, y la orden activada fuera se cierra sin supervisor ni comodato doble; §12.5 la bienvenida sin `origen` se relaciona solo con una única orden de instalación del servicio y del titular actual en los 7 días anteriores a recibir el aviso en Iris, sin contar para `cierre_exige_bienvenida`, o queda sin relacionar. Un 409 de un intento de Iris de la misma orden no es ajeno; `orden.cerrada.activacion.estado: activado_fuera`; una empresa sin fila cuenta como todo en `false`; `cobro_equipo` por el concepto de sistema `instalacion` (`codigo_sistema` en `GET /conceptos-cargo`), también en el cargo al cierre de §3; `bienvenida: false` no rechaza nada en Iris. Un cargo con `422 capacidad_no_disponible` queda terminal y las demás líneas salen como un cobro nuevo con otro `cargo_id`; Iris reintenta primero su intento sin respuesta antes de tratar un 409 como ajeno. Un replay de una clave guardada va antes de la capacidad (§3.1); tras un envío sin respuesta, «cobro por confirmar» antes del cobro de reemplazo. `GET /api/v1/facturas/origen-iris` (permiso `facturas.leer_origen_iris`) para validar el número de factura al conciliar.
- **0.14.2** (22 nov 2026; acordada con Argos, §10.12; espejo: PR enlazada en `argos-isp`): §11.6 sin `image/heic` ni `image/webp` (el portal de Argos convierte a JPEG; en Iris, `422 mime_no_permitido`), dimensiones validadas antes de decodificar (40 MP, `422 imagen_demasiado_grande`); §11.7 `plan_interes_id` = plan activo de Argos, sin empresa; §7.1 `X-Iris-Actor: sistema` en una lista cerrada de lecturas, sin cabeceras de conversación, con `403`/`422 actor_no_permitido`; §4.3 `GET /integraciones/argosisp/capacidades` (`integraciones.ver_capacidades`) solo del conector del token.
- **0.14.1** (22 nov 2026; parche aditivo, propuesto por Argos, §10.11; espejo: PR enlazada en `argos-isp`): §3.4.4, A39 acepta `cliente.tipo` (`persona` por defecto, `empresa`), con NIT en `tipo_documento_fiscal` + `nit_cui` y `representante { nombre, documento_tipo, documento_numero, mayor_de_edad }` cuyo documento es el de la identidad; representante con DPI o pasaporte que no vale para §7.5 ni §12.4 ni escribe `nit_cui`; búsqueda del titular existente por NIT normalizado, solo entre clientes `tipo = empresa` con `nit_cui` verificado en Argos, con asignación al momento solo si el representante coincide con una identidad A41 de esa empresa y, si no o con más de un candidato, `422 titular_empresa_en_revision`; el representante nuevo no reemplaza la identidad vigente; contador por NIT; `representante.*` como `***` salvo `documento_tipo`; `cliente.tipo: empresa` no obliga a un plan empresarial; errores `representante_requerido`, `nit_requerido`, `representante_no_permitido` y `titular_empresa_en_revision`; `servicio.titular_cambiado` suma `tipo_titular`. Con Argos < 0.14.1, Iris no ofrece el cambio de titular a empresa (la orden lo muestra como no disponible).
- **0.14.0** (21 nov 2026; minor sobre la 0.13.0, acordada con Argos, §10.10; espejo `iris#274` · PR enlazada en `argos-isp`; transición en §4.5): §3.9, consumos desde la app con `tecnico` + `bodega_id` + `X-Iris-Actor` (comodatos y devoluciones sin `bodega_id`), `tecnico` = I1 del actor, consumo por actor y bodega (`iris:consumo:<orden_id>:<usuario_id>:<bodega_id>`), `403 actor_desconocido` y `403 actor_no_coincide` también en la bodega personal con contrato ≥ 0.14.0, despacho solo con bodegas `tipo = bodega`, y los `403` de negocio reconocidos por la URN completa y sin reintento. §3.10, `X-Iris-Actor` = usuario autenticado del lote; el técnico que dejó la orden, hasta 12 h (excepción a `contratos/sync.md` §3); `equipo.cambiar` y `equipo.no_recuperado` como tipos del lote, enviados al aplicarse; claves con el id de la operación y huella con `X-Iris-Actor`; `foto_ref` obligatoria en todo intento; `429 demasiados_intentos` definitivo en `…/no-recuperado` aunque venga con `Retry-After`; validación de Iris con `ubicacion_sin_referencia`; «al día» en America/Guatemala; `GET <iris>/api/v1/integraciones/argosisp/intentos-recuperacion/verificacion` (permiso `adjuntos.verificar_integracion`) que verifica el intento o el cambio completo, con tope; un intento sin verificar no cambia la unidad, no avisa ni cuenta; aviso al titular solo por intentos verificados, uno por servicio al día; `foto_ref` obligatoria en un cambio con `cobro_propuesto`, que sin verificar no entra a la cola; el cupo diario no cuenta intentos que Iris no confirma. §3.3, `409 contrato_regresivo` y `422 contrato_no_soportado`; cupo diario por el día de la `fecha` y `503` si su verificación no es concluyente. §6.6, webhooks `tecnico.argos_vinculado` y `tecnico.argos_desvinculado` con `empresa_id` y `version`, para todo técnico, con carga inicial cerrada con `vinculos_cargados` (`409 vinculos_en_carga` mientras tanto) y `GET <iris>/…/tecnicos/:usuario_id` para conciliar; `motivo: desactivado`; vínculo solo de inventario. Pedidos A47, A48, B26 y B27 ampliados.
- **0.13.0** (21 nov 2026; decidida por el PM, §10.9; acordada con Argos): **A49:** `servicio.caidas_repetidas` con umbral de 3 caídas en 60 min, configurable por empresa; Iris decide y abre un ticket por servicio. **A50:** `red.falla_general_abierta` / `red.falla_general_cerrada` con ids de servicios afectados (hasta 5000) y `GET /api/v1/incidentes-red/:incidente_id/servicios` para el resto; Iris abre un ticket general por incidente. **A51:** `POST /clientes/:id/gestiones-cobro` sin texto libre, montos ni teléfonos. **A52:** Argos lee las plantillas de WhatsApp de Iris (§6.7). Permisos `incidentes_red.leer`, `clientes.registrar_gestion_cobro` (Argos) y `plantillas_whatsapp.leer_integracion` (Iris). Pedidos A49–A52 y B29–B32.
- **0.12.1** (21 nov 2026; acordada con Argos, §10.8): §6.4, con `telefono` la `Idempotency-Key` del consentimiento es `argos:consentimiento:<empresa_id>:<cliente_id>:whatsapp:<h>:<ocurrido_en>` (`<h>` = 16 primeros hex del HMAC-SHA256 de `"consentimiento:" + número E.164` con el secreto del receptor de webhooks de Iris; el número en claro solo en el cuerpo). §11: `?servicio_id=` en `GET …/portal/tickets`, `X-Iris-Contacto` (`<contacto_id>` o `titular`) en toda ruta de §11 con `X-Iris-Contacto-Version`, `servicio_id` obligatorio, `403 servicio_fuera_del_contacto` con la caché de Iris (relectura y `503` si Argos no responde), los tickets ajenos del servicio solo con número, estado y fechas, y `creado_por_contacto`. Pedido B28.
- **0.12.0** (21 nov 2026; pedida por el PM, §10.7; acordada con Argos): equipos del cliente (§3.10). **A48:** `POST /servicios/:id/equipos/cambiar` en una transacción (sale en comodato del servicio, entra con la regla de A47, destino de la que sale, `cobro_propuesto` sin monto para la cola de aprobación); estados de la unidad (`disponible`, `en_comodato`, `por_recuperar`, `revision`, `garantia`, `dada_de_baja`, `no_recuperada`); `GET /inventario/unidades/:id` sin costos, proveedor ni RMA; `POST /servicios/:id/equipos/:unidad_id/no-recuperado` (intento, sin declarar la pérdida); eventos `inventario.equipo_cambiado` e `inventario.unidad_estado_cambiado`; permisos `servicios.cambiar_equipo` e `inventario.ver_unidad`. Iris deja de llamar a `…/perdida` desde el cierre. **B27:** órdenes `cambio_equipo` y `retiro` con esos flujos y estado de los equipos en la orden y la ficha. Pedidos A48 y B27.
- **0.11.0** (21 nov 2026; pedida por el PM, §10.6; acordada con Argos): bodega de cuadrilla (§3.9). **A47:** bodega `tipo = cuadrilla` con `cuadrilla_id_iris`, `jefe_id` y miembros sincronizados desde los webhooks `cuadrilla.creada` / `cuadrilla.actualizada` de Iris (`miembros[]`, `jefe_usuario_id`); `GET /inventario/mi-bodega` con `bodegas[]` (`cuadrilla: true`, `jefe`); consumos y comodatos primero de la cuadrilla, `409 sin_existencia { bodegas[] }`, `actor_tecnico` desde `X-Iris-Actor`; traslados a la cuadrilla los recibe solo el jefe vigente (`403 solo_jefe_recibe`); `403 no_es_miembro`; `cuadrilla_id` en `inventario.traslado_*`. **B26:** jefe de cuadrilla en Iris (`POST /cuadrillas/:id/asignar-jefe`), webhooks `cuadrilla.*` a Argos y bodega de cuadrilla en la app de campo. Pedidos A47 y B26.
- **0.10.1** (21 nov 2026; decidida por el PM, §10.5 punto 12; acordada con Argos): §7.1 fija `X-Iris-Contacto` (el `contacto_id` de Argos, entero positivo en decimal; solo en promesas, comprobantes y estado de cuenta por servicio pedidos por un contacto; `403 servicio_fuera_del_contacto` y `422` si está mal formada). **A46** (§3.8): `contactos_cliente.persona_cliente_id` vincula un contacto con otro cliente de la misma empresa (un cliente a cargo del servicio de otro); `a_cargo` en `GET /contactos/buscar`; verificación heredada solo para los permisos de ese contacto; `persona_cliente_id` en `cliente.contactos_cambiados`. Pedidos A46 y B25.
- **0.10.0** (21 nov 2026; acordada con Argos y decidida por el PM, con los ajustes de Iris de §10.5 aceptados; pendiente de implementar): de la venta a la instalación, §3.4. **A38:** un servicio `pendiente_instalacion` no reserva IP; el IPAM asigna al activar (`activar-instalacion` o `aprovisionar`) con el router final, y `POST /servicios/:id/reservar-ip` la reserva en sitio cuando el equipo la necesita antes de activar. **A39:** `POST /servicios/:id/cambiar-titular` (multipart: datos del titular, identidad, motivo, `version`), solo en pendiente; Argos busca o crea el titular por documento en su lado; el cliente anterior sin historial se elimina; evento `servicio.titular_cambiado`. **A40:** `GET /planes?router_id=&modo=` con solo los aplicables y `POST /servicios/:id/cambiar-plan` en pendiente, con motivo y sin factura, con `servicio.config_cambiada`. **A41:** `POST /clientes/:id/identidad` (foto del documento, número, `coincide`), guardada cifrada en Argos, visible solo con `clientes.ver_identidad`, borrada 12 meses después de retirado el cliente; evento `cliente.identidad_registrada`. **A42:** contactos del cliente en Argos (§3.5): varios por cliente con relación, servicios y permisos (`consultar`, `reportar`, `pagar`, `avisos`), teléfonos E.164 con WhatsApp; `GET /contactos/buscar?telefono=`, `GET/POST /clientes/:id/contactos`, `PUT/DELETE /contactos/:id`; evento `cliente.contactos_cambiados`; permisos `contactos.buscar` y `contactos.gestionar`; los avisos de cobro también salen a los contactos con `avisos`. **A43:** `POST /servicios/:id/completar-datos`, que solo llena campos vacíos del titular del servicio (o corrige el nombre con la identidad) y corrige el lugar. Permisos `servicios.reservar_ip`, `servicios.cambiar_titular`, `servicios.cambiar_plan_pendiente`, `clientes.registrar_identidad`, `servicios.completar_datos`, `contactos.buscar` y `contactos.gestionar`. **A44:** prospectos registrados desde Argos en Iris (§3.6). **Seguridad:** rutas de §3.4 solo sobre servicios de Iris y nunca con el asistente; `version` opcional y huella de multipart sin boundary; A43 sin datos de contacto en clientes con historial; identidad que no reemplaza `nit_cui` y solo vigente y anterior; titular existente asignado al momento, con aviso informativo (decisión 9 del PM); límites de cambios de titular y de búsquedas; contactos titulares intocables desde Iris; avisos al titular; números de contactos nunca «registrados»; la foto solo en tránsito y nunca en `payload` ni en errores. **A45:** código automático e inmutable del servicio (`S-000412`) y nombre opcional (§3.7). Pedidos A38–A45 y B17–B24.
- **0.9.0** (21 nov 2026; pedido del PM por el líder de Argos; acordada con Argos y decidida por el PM, §10.4; pendiente de implementar): aditiva sobre 0.8.1, en dos bloques con su propia marca en `capacidades` (§4.3). **P · Portal del cliente de Argos (§11):** rutas de Iris `/api/v1/integraciones/argosisp/portal/clientes/{cliente_id}/…` para listar, ver, abrir, responder y confirmar tickets del cliente con la vista reducida del portal (sin notas internas, asignación, SLA, prioridad, historial ni datos de Horus), con adjuntos por `multipart/form-data`, y para crear y seguir **solicitudes de servicio** (`alta`, `cambio_plan`, `traslado`, `otro`) que en Iris son prospectos de ventas con origen `portal_argos`; cabeceras `X-Iris-Empresa`, `X-Argos-Portal-Sesion` e `Idempotency-Key = argos:portal:<operacion>:<id>`; permisos `portal_argos.*` en el token de §1.2; webhooks Iris → Argos `ticket.estado_cambiado`, `ticket.respondido` y `solicitud_servicio.estado_cambiado` (§6.6); `portal.emitir` y el portal `/soporte` de Iris quedan escritos (§11.9); A7 usa `clienteId`. **E · Equipos y activación desde la orden (§12):** `POST /servicios/:id/aprovisionar` asíncrono para FTTH (serie de la ONT) y wireless (MAC del CPE o radio; TR-069 si lo soporta), pedido por el técnico desde la orden o por un agente desde el panel de Iris, `GET /aprovisionamientos/:id`, `GET /servicios/:id/equipo`, `POST /servicios/:id/wifi`, webhooks Argos → Iris `instalacion.aprovisionada`, `instalacion.fallida` y `equipo.wifi_resuelto`, y permisos `servicios.aprovisionar`, `servicios.ver_equipo` y `servicios.cambiar_wifi` en el token de §1.1; el asistente de IA de Iris cambia el Wi-Fi solo con `verificacion { metodo, verificado_en }` de menos de 10 min. **Cobro de la instalación:** factura única con `POST /facturas/cargo-unico` (`lineas[{ concepto_id, precio_unitario }]` con el concepto de `GET /conceptos-cargo` y el precio de Iris, nunca Q0; `origen_tipo = iris_orden`, permiso `facturas.emitir_cargo_unico`; la respuesta `CargoUnicoEmitido` no cambia) y webhooks `factura.emitida`, `factura.anulada`, `pago.registrado` y `pago.anulado` hacia Iris para las facturas con `origen_tipo = iris_orden`. **Bienvenida al activar (§12.5):** plantilla `bienvenida_servicio` con las cuentas de §7.8 que inserta Iris, `origen` opcional en §6.2, webhook `servicio.bienvenida_cambiada` y `POST /servicios/:id/bienvenida/reenviar`. Incluye la revisión de seguridad de Iris (token aparte para el portal, sesión `ip` reducida, verificación ligada al cliente y de un solo uso para el Wi-Fi, clave por enlace y nunca por el chat, límites de cambios y reenvíos, validación de la bienvenida). Pedidos A30–A37 y B8–B16. Fuera del contrato: el cambio de Wi-Fi que el cliente hace desde el portal de Argos.
- **0.8.1** (19 nov 2026; aceptada por el líder de Argos): A29, búsqueda sin datos de red. §2.1: `buscar_red` opcional (booleano: `true`, `false`, `1` o `0`; por defecto `true`) en `GET /clientes?q=` y `GET /servicios?q=`; con `false`, `q` deja de coincidir por la IPv4 vigente, una IPv6 dentro de un prefijo vigente y el `user_ppp` de los servicios (clientes: nombre, código, código externo, nombre fiscal, email, NIT/CUI y teléfono o móvil; servicios: solo el nombre del cliente); cualquier otro valor responde `422 validacion` (`field: buscar_red`). Iris lo manda en `false` siempre que el actor no tiene `servicios.ver_red` global, sin depender de la versión de Argos; con Argos < 1.21.0 se ignora y queda el límite de la 0.8.0 (§3.3, §4.2). Es compatible: el valor por defecto es el comportamiento de hoy y un Argos anterior ignora el parámetro. Requiere Argos ≥ 1.21.0.
- **0.8.0** (18 nov 2026, **final**; revisada por Argos contra su código, `argos-isp` `f857fb74`; decisiones del PM del 18 nov en §10.2; implementada en Argos en `argos-isp#249` y publicada en Argos 1.18.0; espejada en `argos-isp`; implementado en Iris (0.33.0, que informa contrato 0.8.1)): cuatro bloques, todos aditivos sobre 0.7.1, en **una sola versión** que Argos publica como 1.18.0 (decisión del PM; no hay 0.7.2). **A, reconexión temporal sin personal de caja** (lado Argos hecho primero en su rama `lider/iris-072`, hoy en la PR #249): §7.3: `motivo` acepta `fuera_de_horario` | `sin_personal` (otro valor, `422 validacion` con `field = "motivo"` y `rule = "enum"`); `reglas_promesa.reconexion_temporal.vigencia` = `horas` (por defecto) | `medianoche` (las 00:00 del día siguiente en Guatemala), con el vencimiento real siempre en `hasta` (RFC 3339 en UTC con milisegundos), que es el fin de la promesa y no la hora del corte; un solo cupo para los dos motivos; el criterio de `sin_personal` es de Iris y queda auditado en Argos. §7.4 y §2.3: `motivo_reconexion` en la `Promesa` y en los webhooks `promesa.*`, y filtro `motivo_reconexion` en `GET /promesas`. **B, cuentas bancarias de Argos como única fuente:** §7.8 (nueva): `titular` y `tipo` en la cuenta (los dos pueden ser `null`), webhook `cuenta_bancaria.cambiada` (también al crear), `rule: "inactiva"` al registrar un comprobante en una cuenta desactivada, instantánea de lectura en Iris y fin de las cuentas escritas a mano (lista «a migrar» en §10.3). **C, más datos del servicio:** §2.4 (nueva): `precio_mensual` (con IVA, sin adicionales) y `moneda`; `precio` en `campos[]` de `servicio.config_cambiada` y de `catalogo.cambiado` de plan; lista blanca de campos en la instantánea de Iris, los permisos `servicios.ver_red` y `servicios.ver_ubicacion` en Iris y qué recibe el asistente de IA. `router_nombre`, `plan_nombre` y `zona_nombre` ya existían. **D, conexión de un paso:** §1.5 (nueva): los requisitos de seguridad de Argos (S1–S8), el cuerpo del canje por escrito, dos validaciones nuevas del receptor de Iris en el canje de Argos (`422 webhook_invalido` con `rule` `mismo_host` u `origen_aprobado`, y el código queda gastado), el conector de Argos encendido al confirmarse un canje del botón, y el certificado TLS solo como «registrar y avisar»; §2.5 (nueva): primera sincronización automática, con progreso y reanudable, cabeceras de Iris opcionales en `GET /cuentas-bancarias` y `GET /bancos`, la ruta nueva `PUT /integraciones/iris/sincronizacion` (`409 sincronizacion_vieja`) y pantalla de estado en los dos paneles. §4.1 (nueva): compatibilidad y errores. §5: pedidos A25–A28 y B4–B7. `excluir_retirados=true` en `GET /clientes` y `GET /servicios` para la primera carga (§2.5). §10 (nueva): las decisiones del PM, lo que sigue abierto y la migración de cuentas en Iris. Es compatible: no cambia ningún campo ni ruta de 0.7.1; un Argos 1.17.x sin estos cambios sigue funcionando con Iris, y al revés (§4.1). Requiere Argos ≥ 1.18.0.
- **0.7.1** (16 nov 2026): A24: quién decidió el comprobante y a qué instalación se le da acceso al vincular. §7.2 y §7.7: `verificado_iris_actor` en el `Comprobante` (`GET /comprobantes/:id`, la lista y la respuesta `200` de aprobar y rechazar) y en el payload de los webhooks `comprobante.aprobado` y `comprobante.rechazado`: el `X-Iris-Actor` (`usuario:<id>`) de quien aprobó o rechazó desde Iris, o `null` si se decidió en el panel de Argos. `iris_actor` sigue siendo el actor que **registró** el comprobante, no el que lo decidió. §1.3: `instalacion` (string o `null`: host de la `url_base` del conector, con puerto solo si no es el estándar) en `vinculacion` del canje del autologin Iris → Argos, y la pantalla de confirmación del vínculo con el sistema, el nombre y el correo del usuario de Iris, la instalación y el aviso, encima del formulario y antes de que la persona escriba su correo y su contraseña; Iris no manda nada nuevo. §7.1: la forma exacta de `X-Iris-Actor` y `X-Iris-Conversacion` como expresiones regulares, sin cambiar los límites. §6.3: en `boleta_rechazada` y `boleta_falta_dato`, `motivo` es un texto fijo de Iris según la `causa`, y el motivo libre solo se le reenvía al cliente si pasa una lista blanca (letras del español, espacios y los signos `,;:¿?¡!()`, sin dígitos ni puntos salvo el final); el motivo completo se guarda y va a Argos sin cambios. §7.7: antes de aplicar un webhook `comprobante.*`, Iris lee `GET /comprobantes/:id` (con las dos cabeceras de §7.1; si lee el sistema, las de quien registró la boleta) y filtra por `canal = iris`. Es aditiva y compatible: solo agrega campos y aclara el comportamiento de Iris. Con Argos 1.15.0 (contrato 0.7.0) `verificado_iris_actor` no viene, y entonces Iris solo muestra `verificado_por_nombre`. Argos responde `contrato: "0.7.1"` desde la versión que la implemente.
- **0.7.0** (10 nov 2026, pedido del PM): A23: aprobar y rechazar comprobantes desde la conversación de Iris. §7.7 (nueva): `POST /comprobantes/:id/aprobar` y `POST /comprobantes/:id/rechazar` aceptan el token de Iris, solo para comprobantes con `canal = iris`, con `X-Iris-Actor`, `X-Iris-Conversacion` e `Idempotency-Key` obligatorias (`iris:aprobar:<comprobante_id>` / `iris:rechazar:<comprobante_id>`; la huella incluye el actor) y responden `200` con el `Comprobante` completo. Con el conector apagado o desconectado: `409 iris_apagado`, igual que en §1.3. Decide el usuario de Argos vinculado a `usuario:<id>`: activo, con el permiso y con la empresa del comprobante en su alcance; errores `403` `actor_sin_vinculo`, `actor_inactivo`, `actor_sin_permiso`, `actor_sin_empresa` y `actor_ia_no_permitido` (el asistente de IA nunca puede). La auditoría y el pago quedan a nombre del usuario vinculado, con el token de Iris como «vía». §1.1: el token de Iris suma `comprobantes.aprobar` y `comprobantes.rechazar` (Argos migra los que existen). §1.3: forma del autologin Iris → Argos (`POST /integraciones/iris/sso/tickets`), que es lo único que crea ese vínculo; `destino` pasa a ser opcional. §0, §3, §3.1, §7, §7.1–§7.4 y §9.3: se corrige el texto que decía que Iris no puede aprobar ni rechazar; Argos sigue decidiendo. Los webhooks `comprobante.*` y las promesas no cambian de forma. Pedido B3 a Iris. Es compatible: solo agrega rutas al token de Iris y vuelve opcional un campo. Requiere la primera versión de Argos que implemente A23.
- **0.6.4** (5 nov 2026): A22: `nombre` en `bodegas[]` de `GET /inventario/tecnicos` para elegir la bodega sin códigos; toda bodega `tecnico` tiene usuario (restricción en base). Es compatible: solo agrega el campo. Argos responde `contrato: "0.6.4"`.
- **0.6.3** (5 nov 2026): §1.3: `POST /integraciones/iris/conexion/revocar`. Iris lo llama, con el `atk_` recibido, cuando no completa un canje que Argos respondió con `200`; Argos responde `204` (o `401` si el token ya estaba revocado) y deja el conector desconectado. El `atk_` de un canje que nadie usa en 24 h vence solo. Repetir un canje cuyo token ya se revocó responde `410`. Reconexión: un canje sobre un conector en uso queda pendiente; en la primera llamada del `atk_` nuevo se aplica y se revoca el anterior; si el nuevo se revoca o vence, el anterior sigue igual. `GET /integraciones/iris` agrega `estado_canje`, `canje_vence_en`, `desconectado_en`, `motivo_desconexion`, `reconexion_pendiente_desde` y `reconexion_vence_en`. Es compatible: un Iris que no llama a revocar sigue igual porque usa el token al activar su conector. Argos responde `contrato: "0.6.3"`.
- **0.6.2** (5 nov 2026): §1.3: el host del receptor es el de la base del canje (enlace o `api=`), nunca el del panel, y `422 webhook_invalido` en Iris si falta o es otro. §3.1: Argos reintenta el `409` de idempotencia de Iris con `type` `idempotencia_en_curso` o `idempotencia_en_proceso`. «Versión y compatibilidad»: `receptor_argos` en el canje y en `PUT /integraciones/iris/contrato`, y `requiere_reconectar` / `motivo_reconectar` en `GET /integraciones/iris` para los conectores canjeados antes de B1. §6.6: solo se envía a conectores con receptor guardado. Argos responde `contrato: "0.6.2"`.
- **0.6.1** (5 nov 2026): solo texto, sin funciones nuevas; deja el contrato igual a lo que Argos implementó (PR #217) y a las formas que Iris confirmó. §0: el canal de auditoría de las peticiones de Iris es el de §7.1 (`ia` o `api`; los webhooks, `webhook`), no `integracion`. §1.1: los permisos de pagos y atención con los nombres de §9.1, más `promesas.ver` para el simulador. §6.4: `empresa_id` obligatorio en el consentimiento, `Idempotency-Key = argos:consentimiento:<empresa_id>:<cliente_id>:<canal>:<ocurrido_en>` y respuesta `{ consentimiento_id, estado_vigente }`. §6.6: cuerpo y cabeceras de los webhooks Iris → Argos, `empresa_id` en `aviso.*`, un solo webhook final por aviso y la forma de `GET /mensajeria/avisos/:id` con su correspondencia en Argos. §5.1: A16–A21 listos en Argos; B1 en curso y B2 pendiente en Iris.
- **0.6.0** (5 nov, propuesta de Iris, pedido del PM; revisada por Argos el 5 nov, §9): mensajería y pagos, **fase 1**. §6: el servicio de mensajería de Iris (WhatsApp por API oficial o por QR, Telegram y correo, con las reglas contra spam y el registro de permisos de contacto); Argos lo adopta como su canal WhatsApp. §7: comprobantes de pago y promesas de pago que llegan por chat o llamada (asistente de IA o persona), reconexión temporal como promesa ligada a un comprobante, verificación de identidad sin que Iris vea el documento y búsqueda de cliente por teléfono. Pedidos A16–A21, B1 y B2. Argos deja de entregar `nit_cui` al usuario técnico Iris. La fase 2 (cambios de Wi-Fi del cliente por Argos ↔ SmartOLT) queda anotada en §8, sin forma todavía. Requiere la primera versión de Argos que la implemente (después de 1.6.0).
- **0.5.1** (31 oct): §3.3 documenta la forma exacta de `GET /inventario/traslados/:id` (pregunta de Iris). Sin cambios de comportamiento; Argos 1.6.0 ya responde así.
- **0.5.0** (31 oct): inventario del técnico (pedido del PM; contrato de inventario, Fase D): `GET /inventario/mi-bodega`, `tecnico` en lugar de `bodega_id` en consumos y comodatos, modalidades por producto (`consumible_gasto`, `consumible_cobro`, `comodato`), recepción de órdenes de traslado con firma digital y los webhooks `inventario.traslado_enviado` / `inventario.traslado_recibido`; permiso `inventario.recibir_traslado`. Pedidos de Iris A10–A15: `servicio.datos_cambiados`, `catalogo.cambiado`, `email` y `referencia` en `cliente.actualizado`, `referencia` en cliente y servicio, contacto en sitio del servicio y teléfonos validados (Guatemala `+502 NNNN-NNNN` o internacional `+` 8–15 dígitos; solo se validan al cambiar). Requiere Argos ≥ 1.6.0.
- **0.4.0** (30 oct, pedido del PM): §1.4 «Conectar con un botón»: autorización desde el panel de Argos con `state` y PKCE S256, sin copiar enlaces. `panel_url` en la respuesta del canje. El enlace de §1.3 se mantiene.
- **0.3.2** (30 oct): §2.3 «Confirmar un borrado»: solo `404 no_encontrado` es un borrado y fuera de alcance es `403 empresa_no_permitida`, nunca 404. La 0.3.1 requiere Argos ≥ 1.4.0.
- **0.3.1** (30 oct): pedidos de la prueba de punta a punta contra Argos 1.3.0 y de la revisión de seguridad de Iris. `activar-instalacion` con `version` opcional y `409 servicio_no_pendiente` (antes 422); `cliente.actualizado` con `telefono`, `movil`, `direccion`, `coordenadas` y `zona_id`; `coordenadas` en `GET /routers`; el usuario técnico Iris solo con alcance por empresas (§1.1); `usuario.correo_verificado: false` en el ticket de autologin hacia Iris (§1.3); el canje del enlace informa `alcance_global` y `empresas[]` (`id`, `nombre`, `nit`); `servicio.eliminado` y `router.eliminado` en §2.3 (Argos elimina solo lo que nunca se usó; con historial, el servicio se retira y el router se desactiva). En Argos, `AAA_PPP_KEY` es obligatoria al arrancar.
- **0.3.0** (28 oct): §3.1 idempotencia de Argos y §3.2 cuerpos del alta desde un prospecto ganado; `GET /planes` filtrado por `activo` y `router_id`.
- **0.2.1** (28 oct): `servicio.caidas_repetidas` (A6) en §2.3; A1 y A9 listos en Argos.
- **0.2.0** (28 oct): A5 pasa a reemplazado por la integración Iris ↔ OZmap; nuevo pedido A9 (referencia de planta al activar la instalación).
- **0.1.0:** borrador inicial.

### 4.1 Compatibilidad de la 0.8.0

La 0.8.0 es aditiva sobre la 0.7.1. Un Argos 1.17.x sin estos cambios sigue funcionando con un Iris 0.8.0, y un Iris 0.7.1 sigue funcionando con un Argos que ya los tenga. Argos publica los cuatro bloques juntos en la 1.18.0 y su `contrato` pasa directo de `0.7.1` a `0.8.0`: no hay versiones intermedias.

**Cómo sabe cada lado qué soporta el otro.** No hay mecanismos nuevos: es el de «Versión y compatibilidad» de §3.3.

- **Iris sabe de Argos** por `contrato` y `version_argos`, que vienen en la respuesta del canje y en `GET /integraciones/iris/estado`. Iris los relee al activar el conector y en su revisión periódica de versión.
- **Argos sabe de Iris** por `contrato` en el cuerpo del canje y por `PUT /integraciones/iris/contrato { contrato, receptor_argos }`, que Iris manda al activar y cada vez que cambia de versión.
- Cada lado usa una función de la 0.8.0 **solo si el otro informó un contrato ≥ 0.8.0**. Con menos, se comporta como en 0.7.1.

| Función 0.8.0 | Iris 0.8.0 con un Argos sin ella (contrato < 0.8.0) | Argos 0.8.0 con Iris 0.7.1 |
| --- | --- | --- |
| `motivo: "sin_personal"` (§7.3) | Iris no lo manda **sin contrato ≥ 0.8.0**: en horario hábil responde su `422 reconexion_no_corresponde` y sigue una persona, como hoy. Si llegara a mandarlo, Argos 1.17.x responde `422 validacion` (`motivo` / `enum`) y no crea nada | Iris solo manda `fuera_de_horario`; Argos lo atiende igual |
| `vigencia: "medianoche"` (§7.3) | No aplica: es de la plantilla de Argos | Iris 0.7.1 ya lee `hasta` de la respuesta y lo pasa a la plantilla `reconexion_temporal`; no asume horas |
| `motivo_reconexion` en la promesa (§7.4) | El campo no viene: Iris lo trata como `null` y usa el motivo que guardó al pedirla | Iris ignora el campo que no conoce |
| `titular` y `tipo` de la cuenta (§7.8) | No vienen: Iris muestra banco, número, alias y moneda, y no le ofrece al cliente una cuenta sin `titular` (§7.8) | Iris ignora los campos que no conoce |
| Webhook `cuenta_bancaria.cambiada` (§2.3) | No llega: Iris refresca la instantánea de cuentas al usarla si tiene más de 24 h, y siempre relee la cuenta antes de registrar una boleta | Argos no lo envía a conectores con contrato < 0.8.0 |
| `precio_mensual`, `moneda` (§2.4) | No vienen: Iris no muestra el precio del servicio ni lo calcula, y el asistente no lo dice | Iris ignora los campos que no conoce |
| `precio` en `campos[]` de `servicio.config_cambiada` y `catalogo.cambiado` (§2.3) | No llega: no hay aviso de cambio de precio (tampoco hay `precio_mensual`) | Iris 0.7.1 refresca la instantánea como con cualquier otro campo |
| Validaciones `mismo_host` y `origen_aprobado` en el canje (§1.5) | Argos no las hace; solo exige `https`. Iris manda igual su origen y su receptor | Aplican a cualquier Iris: uno 0.7.1 ya manda su propio host |
| Conector de Argos encendido al confirmar el canje del botón (§1.5) | Queda apagado en Argos hasta que un admin lo enciende, como hoy | Argos lo enciende igual: depende de cómo nació el código, no de la versión de Iris |
| Cabeceras de Iris opcionales en `GET /cuentas-bancarias` y `GET /bancos` (§2.5) | Siguen siendo obligatorias (`400 cabecera_iris_requerida`): la primera sincronización no trae cuentas ni bancos, e Iris los lee desde una conversación, como hoy | Iris 0.7.1 las manda siempre; sin cambio |
| Primera sincronización (§2.5) | Trae empresas, zonas, planes, routers, clientes y servicios, con lecturas que ya existen. Sin cuentas ni bancos (fila anterior) y sin precios | No aplica: Iris 0.7.1 no la hace |
| `excluir_retirados=true` en `GET /clientes` y `GET /servicios` (§2.5) | Iris no lo manda: la primera carga recorre las listas completas | No aplica: Iris 0.7.1 no hace la primera carga |
| `PUT /integraciones/iris/sincronizacion` (§2.5) | La ruta no existe: Iris no la llama | Nadie la llama; el panel de Argos dice «no informado» |
| Pantalla de estado (§2.5) | Iris muestra la suya con lo que tiene | Argos muestra la suya; lo de Iris, «no informado» |
| Certificado TLS, «registrar y avisar» (§1.5) | Es solo de Iris y no depende de la versión de Argos | Sin cambio |

- Argos solo envía el webhook nuevo (`cuenta_bancaria.cambiada`) a conectores con contrato ≥ 0.8.0, igual que hizo con los de 0.5.0 y 0.6.0. Los campos nuevos en respuestas y en webhooks que ya existen (`motivo_reconexion`, `precio` en `campos[]`) van siempre, sin filtro de versión: quien no los conoce los ignora.
- Ningún campo ni ruta de 0.7.1 cambia de forma ni de significado. `motivo: "fuera_de_horario"` y `vigencia: "horas"` siguen siendo los valores por defecto.
- Los permisos `servicios.ver_red` y `servicios.ver_ubicacion` son internos de Iris y no dependen de la versión de Argos.

**Errores de la 0.8.0.** Estos son los que tocan las secciones nuevas; los marcados «nuevo» no existen antes de la 0.8.0. Todos son de negocio y no se reintentan.

| Dónde | Status | Código o regla | Cuándo |
| --- | --- | --- | --- |
| `POST /comprobantes/:id/reconexion-temporal` (§7.3) | 422 | `validacion` (`urn:argosisp:problema:validacion`), con `errors[0] = { field: "motivo", rule: "enum" }` | `motivo` distinto de `fuera_de_horario` y `sin_personal`. Argos 1.17.x responde lo mismo a `sin_personal` |
| `POST /comprobantes/:id/reconexion-temporal` (§7.3) | 422 | `reconexion_no_permitida`, con `errors[0].rule` | Las reglas de 0.7.1 (`no_habilitada`, `cupo_agotado`, `comprobante_no_pendiente`, `comprobante_no_cubre`, `servicio_no_cortado`, `suspension_manual`, `promesa_abierta`, `servicio_de_otro_cliente`), con cualquiera de los dos motivos |
| Iris, `conversaciones.pedir_reconexion` (§7.3) | 422 | `reconexion_no_corresponde` | En horario hábil con alguien de caja disponible, con `sin_personal` apagado en Iris, o con un Argos que no lo soporta. Lo responde Iris sin llamar a Argos |
| `GET /cuentas-bancarias/:id` (§7.8) | 404 | `no_encontrado` (`urn:argosisp:problema:no_encontrado`) | La cuenta no existe. Las cuentas nunca se borran, así que no es un borrado |
| `GET /cuentas-bancarias/:id` (§7.8) | 403 | `empresa_no_permitida` | Cuenta de una empresa fuera del alcance del usuario técnico Iris |
| `GET /cuentas-bancarias` (§7.8) | 403 | `empresa_no_permitida` | Solo con `?empresas=todas` sin alcance global. Sin ese filtro la lista trae solo lo del alcance y nunca responde `403` |
| `GET /cuentas-bancarias` y `GET /bancos` (§2.5) | 400 | `cabecera_iris_requerida` | **Deja de aplicar** en la 0.8.0 a estas dos lecturas cuando faltan las cabeceras de §7.1. Con un Argos anterior sigue aplicando |
| `POST /comprobantes` (§7.2, §7.8) | 422 | `cuenta_de_otra_empresa` | Sin cambio |
| `POST /comprobantes` (§7.2, §7.8) | 422 | `validacion`, con `errors[0] = { field: "cuenta_bancaria_id", rule: "inactiva" }` (nuevo; hoy `rule: "exists"`, la misma que para una cuenta que no existe) | La cuenta está `activa: false`. Iris pasa el caso a una persona (§7.8) |
| `POST /comprobantes` (§7.2, §7.8) | 422 | `validacion`, con `errors[0] = { field: "cuenta_bancaria_id", rule: "moneda" }` | Sin cambio: la cuenta no es en `GTQ` |
| `PUT /integraciones/iris/sincronizacion` (§2.5) | 409 | `sincronizacion_vieja` (nuevo) | El informe es más viejo que el guardado |
| `PUT /integraciones/iris/sincronizacion` (§2.5) | 403 | — (nuevo) | Cualquier credencial que no sea el `atk_` de Iris |
| Canje (§1.5) | 401 | `codigo_invalido` | `code_verifier` que no coincide; el código queda gastado (S1, S4) |
| Canje (§1.5) | 410 | `enlace_vencido` | Repetir un canje cuyo token ya se revocó |
| Canje (§1.5), en Iris | 422 | `webhook_invalido` | Falta el receptor de Argos o su host no es el de la base del canje |
| Canje (§1.5), en Argos | 422 | `webhook_invalido` (nuevo en Argos), con `errors[0].rule` = `mismo_host` (`field: "webhook.url"`) u `origen_aprobado` (`field: "base_url"`) | El host del receptor de Iris no es el de `base_url`, o el origen de `base_url` no es el que aprobó el admin con el botón. **El código queda gastado**; repetirlo responde `410 enlace_vencido` |
| Rutas de §1.3 y §7.7 con el conector apagado | 409 | `iris_apagado` | Sin cambio |

### 4.2 Compatibilidad de la 0.8.1

La 0.8.1 es aditiva sobre la 0.8.0. Argos la publica en la 1.21.0 y su `contrato` pasa a `0.8.1`.

| Función 0.8.1 | Iris 0.8.1 con un Argos sin ella (`version_argos` < 1.21.0) | Argos 0.8.1 con un Iris anterior |
| --- | --- | --- |
| `buscar_red=false` en `GET /clientes?q=` y `GET /servicios?q=` (§2.1) | Iris lo manda igual siempre que el actor no tiene `servicios.ver_red` global; ese Argos lo ignora y busca como hoy, y queda el límite de la 0.8.0: Iris corta las búsquedas que parecen una IP, pero un `q` con un `user_ppp` todavía encuentra al cliente | Iris no lo manda: Argos busca como hoy (`buscar_red=true`) |

| Dónde | Status | Código o regla | Cuándo |
| --- | --- | --- | --- |
| `GET /clientes` y `GET /servicios` (§2.1) | 422 | `validacion` (`urn:argosisp:problema:validacion`), con `errors[0].field = "buscar_red"` (nuevo) | `buscar_red` con un valor que no es `true`, `false`, `1` ni `0`. Argos anterior a 1.21.0 ignora el parámetro |

### 4.3 Compatibilidad de la 0.9.0

La 0.9.0 es aditiva sobre la 0.8.1. Ningún campo ni ruta de la 0.8.1 cambia de forma ni de significado. Cada lado usa una función de la 0.9.0 **solo si el otro informó un contrato ≥ 0.9.0** (canje, `PUT /integraciones/iris/contrato` y `GET /integraciones/iris/estado`, como en §3.3) **y**, para cada bloque, la marca de `capacidades`:

- `GET /integraciones/iris/estado` (Argos) y la respuesta del canje suman `capacidades: { aprovisionamiento: [{ empresa_id, ftth: boolean, wireless: boolean, tr069: boolean }] }` (`wireless`: Argos asigna IP y activa un CPE o radio; `tr069`: además lo configura por el ACS). Sin la marca, o con `false` para esa empresa y esa tecnología, Iris no ofrece «Activar servicio» y activa al cerrar como en §3.
- Desde 0.14.3, las mismas respuestas suman `capacidades.equipos: [{ empresa_id, estado_equipo: boolean, wifi: boolean, bienvenida: boolean, cobro_equipo: boolean }]`, una fila por empresa del token, calculada con la configuración de esa empresa en Argos (`estado_equipo` y `wifi` necesitan SmartOLT o el ACS conectados). Con un contrato de Iris < 0.9.0, todas en `false`; `aprovisionamiento` conserva su valor. Iris solo muestra y usa una función con su marca en `true` para la empresa del servicio. El parche agrega al token de §1.1 solo `facturas.leer_origen_iris` (§3, lectura para conciliar un «cobro por confirmar»).
    - **Qué controla cada marca:** `estado_equipo` → `GET /servicios/:id/equipo`; `wifi` → `POST /servicios/:id/wifi` y el campo `wifi` de `aprovisionar` (sin la marca, `422 capacidad_no_disponible` en lugar de `wifi_no_disponible`); `bienvenida` → si Argos manda `bienvenida_servicio` con `origen` (§6.2) para esa empresa (en `false`, Argos usa `bienvenida` e Iris no rechaza nada por la marca) y `POST …/bienvenida/reenviar`; `cobro_equipo` → las líneas de `POST /facturas/cargo-unico` con el concepto de instalación sembrado de esa empresa (código de sistema `instalacion`), salgan del registro de A36 o del cierre (`iris:cargo:<orden_id>`, §3); Iris cobra la instalación solo con ese concepto. Con `cobro_equipo: false`, Iris no ofrece el cobro de la instalación y el cierre no manda la línea de instalación de la plantilla; los demás conceptos (visita u otros; los materiales `consumible_cobro` los cobra Argos con el consumo, §3.3) salen siempre: un cargo sin la línea de instalación nunca recibe `capacidad_no_disponible` (un cargo que mezcla líneas se rechaza entero y se reenvía sin ella, §3). Con `bienvenida: false`, Iris no ofrece Reenviar ni aplica `cierre_exige_bienvenida`. Con `wifi: false`, Iris no manda `wifi` en `aprovisionar`; si aun así recibe `422 capacidad_no_disponible`, lo reenvía sin `wifi` con otra clave y descarta la clave Wi-Fi cifrada (§12.1, Secretos).
    - **Empresa sin fila:** con `capacidades.equipos` presente, una empresa del alcance sin fila cuenta como todas en `false`.
    - **Orden de validación:** Argos comprueba primero el alcance del token (un servicio fuera de sus empresas responde lo mismo que uno que no existe, sin decir si existe); después, una `Idempotency-Key` ya guardada con la misma huella devuelve su `2xx` original (`Idempotent-Replayed: true`) aunque la marca haya pasado a `false` (`capacidad_no_disponible` solo responde a una clave que Argos no tiene guardada); una clave en curso responde `409 idempotencia_en_curso` también antes de la capacidad; después la capacidad y al final los errores propios de la ruta (`equipo_sin_gestion`, `wifi_no_disponible`). El `422 capacidad_no_disponible` solo lo responde Argos, en `GET …/equipo`, `POST …/wifi`, el `wifi` de `aprovisionar`, `…/bienvenida/reenviar` y `cargo-unico` con el concepto de instalación: sin efectos, sin `extensions` y sin datos de la configuración (SmartOLT, ACS) en `detail`. Iris lo muestra como «no disponible para esta empresa», no reintenta solo (salvo el reenvío sin `wifi` de `aprovisionar` y sin la línea de instalación del cargo, arriba y §3) y vuelve a leer `GET /integraciones/iris/estado`.
    - **Compatibilidad:** sin `capacidades.equipos` (Argos < 0.14.3), Iris se comporta como en la 0.14.2: usa cada función según el contrato informado (≥ 0.9.0), sin marca por empresa. Argos responde `422 capacidad_no_disponible` solo a conectores con contrato ≥ 0.14.3; con uno anterior, la ruta responde como en la 0.14.2.
- `GET /api/v1/integraciones/argosisp/estado` (Iris, ya existe) suma `capacidades: { portal_argos: boolean, solicitudes_servicio: boolean, adjuntos_ticket: boolean }`. Argos muestra cada sección de su portal solo con su marca en `true`, leída de `GET …/capacidades` (el sistema de Argos nunca lee `…/estado`). **0.15.0 (propuesta aceptada con ajustes):** las marcas nuevas (`tipo_servicio`, `corte_manual` y `mensajeria[]`, §4.6) se agregan a este mismo objeto `capacidades`; no se crea otro. Desde 0.14.2, el token del panel de Argos (rol `integracion_argos`) lee **`GET /api/v1/integraciones/argosisp/capacidades`** con el permiso nuevo `integraciones.ver_capacidades`: responde solo `{ contrato_iris, capacidades }` del conector al que pertenece el token; con el token de otro conector o de uno apagado, `404`; con el `token_portal`, `403`. El rol no recibe `integraciones.ver` ni ningún otro permiso `integraciones.*`, y `GET …/estado` sigue siendo solo para personas.

| Función 0.9.0 | Iris 0.9.0 con un Argos < 0.9.0 | Argos 0.9.0 con un Iris < 0.9.0 |
| --- | --- | --- |
| Rutas `…/portal/clientes/{cliente_id}/…` (§11) | Existen; nadie las llama | No existen (404). Argos no muestra tickets ni solicitudes en su portal; si Iris tiene `portal.emitir`, puede mostrar el botón «Soporte» hacia `/soporte` (§11.9) |
| Webhooks `ticket.*` y `solicitud_servicio.*` (§6.6) | Iris no los envía (Argos los recibiría como desconocidos) | No llegan; el portal de Argos relee al abrir la pantalla |
| `orden.activar_servicio` (§12) | Iris no lo ofrece: la activación sale al cerrar (§3) | Nadie llama a `aprovisionar`; Argos sigue recibiendo `activar-instalacion` al cierre |
| `GET /servicios/:id/equipo`, `POST /servicios/:id/wifi` | Iris no muestra el equipo ni ofrece cambiar el Wi-Fi | Nadie los llama |
| Webhooks `instalacion.*`, `equipo.wifi_resuelto` | No llegan; Iris no los espera | Argos no los envía a conectores con contrato < 0.9.0 |
| Webhooks `factura.emitida`, `factura.anulada`, `pago.registrado` y `pago.anulado` hacia Iris | No llegan: el cobro de la orden se queda en «facturado» con el número de la respuesta | Argos no los envía a conectores con contrato < 0.9.0 |
| Bienvenida (§12.5): `bienvenida_servicio`, `origen` en §6.2, `servicio.bienvenida_cambiada`, reenviar | Iris no ofrece «Reenviar» y la orden solo muestra el estado del WhatsApp si lo puede relacionar por servicio; Argos sigue con `bienvenida` | Argos usa `bienvenida` y no manda `origen` (un Iris anterior respondería `422` a un campo que no conoce) ni el webhook |
| `token_portal` en el canje y en `PUT /integraciones/iris/contrato` (§1.2) | Iris no lo manda | Argos ignora el campo; sin él no hay portal del cliente con Iris |
| `capacidades.equipos` y `422 capacidad_no_disponible` (0.14.3) | Iris 0.14.3 con un Argos < 0.14.3: comportamiento 0.14.2 (cada función según el contrato informado) | Argos 0.14.3 con un Iris < 0.14.3: sin el `422` nuevo; la ruta responde como en la 0.14.2 |
| Alias `cliente_id` en `GET /tickets` | No aplica | Iris anterior lo ignora y devuelve toda la empresa: Argos manda **siempre** `clienteId` |

**Errores nuevos de la 0.9.0.** Todos `application/problem+json` (RFC 9457) con `type` `urn:iris:problema:<codigo>` (rutas de Iris) o `urn:argosisp:problema:<codigo>` (rutas de Argos). Los de negocio no se reintentan; solo `5xx`, timeouts, `429` (con `Retry-After`) y el `409` de idempotencia en curso (§3.1). Excepción desde 0.14.0: en `…/no-recuperado`, `urn:argosisp:problema:demasiados_intentos` es definitivo aunque venga con `Retry-After` (§3.10).

| Dónde | Status | Código | Cuándo |
| --- | --- | --- | --- |
| §11, todas | 403 | `solo_integracion_argos` | La credencial no es el `token_portal` de un conector Argos (§1.2) |
| §11, todas | 403 | `empresa_no_permitida` | `X-Iris-Empresa` fuera del alcance del token |
| §11, todas | 400 | `cabecera_requerida` | Falta o tiene mal formato `X-Iris-Empresa` o `X-Argos-Portal-Sesion` |
| §11, todas | 422 | `empresa_no_coincide` / `cliente_no_existe` | El cliente es de otra empresa / Argos no lo tiene |
| §11, ticket o solicitud | 404 | `no_encontrado` | No existe, es de otro cliente o de otra empresa (mismo `detail` en los tres casos) |
| §11, escrituras con sesión `ip` | 403 | `sesion_insuficiente` | La acción exige una sesión con `documento` (§11.2) |
| §11.4 | 422 | `validacion` | Campos (`errors[]` con `field` y `rule`); `servicio_id` que no es del cliente: `rule: "servicio"`; categoría no visible en el portal: `rule: "categoria"` |
| §11.4, §11.5 | 422 | `transicion_invalida` | Responder un ticket cerrado; confirmar uno que no está resuelto |
| §11.5 | 409 | `conflicto_version` | `version` vieja |
| §11.4, §11.5 | 429 | `demasiados_tickets` / `demasiados_mensajes` | Topes por cliente (§11.2) |
| §11, todas | 429 | `demasiadas_peticiones` | Tope del conector (§11.2) |
| §11.7 | 429 | `demasiadas_solicitudes` | Tope de solicitudes por cliente (§11.2) |
| §11, todas | 409 | `argos_apagado` | El conector Argos está apagado en Iris |
| §11 (salvo `POST …/tickets`) | 503 | `argos_no_disponible` | Iris no puede confirmar el cliente con Argos (caído o sin respuesta); se reintenta más tarde |
| §11.6 | 422 | `adjuntos_no_permitidos` | Adjuntos con una sesión `ip` |
| §11.6 | 413 / 422 | `tamano_excedido` / `mime_no_permitido` / `demasiados_adjuntos` | Adjuntos fuera de los límites |
| §11.7 | 409 | `solicitud_abierta` | Ya hay una solicitud abierta de ese tipo y servicio (`detail` y `extensions.solicitud_id`) |
| §11.7 | 422 | `servicio_requerido` | `cambio_plan` o `traslado` sin `servicio_id` |
| Idempotencia (Iris) | 422 / 409 | `idempotencia_reutilizada` / `idempotencia_en_proceso` | Misma clave con otro cuerpo / la primera sigue en curso (se reintenta) |
| §12.2 | 202 | — | Aprovisionamiento aceptado (`en_proceso`) |
| §12.2 | 409 | `servicio_no_pendiente` | El servicio ya no está `pendiente_instalacion` |
| §12.2 | 409 | `aprovisionamiento_en_curso` | Hay otro en curso para el servicio (`extensions.aprovisionamiento_id` y, desde 0.14.3, `extensions.iniciado_por`: `argos` \| `iris`) |
| §3 | 422 | `validacion` | (0.14.3) `GET /facturas/origen-iris`: parámetros que faltan o se mezclan, o `numero` que no es entero positivo. Para Iris solo rechaza la conciliación |
| §3 | 429 | `demasiadas_peticiones` | (0.14.3) `GET /facturas/origen-iris`: más de 60 lecturas por minuto por token (con `Retry-After`) |
| §3 | 404 | `factura_no_encontrada` | (0.14.3) `GET /facturas/origen-iris`: no existe, no es `iris_orden`, su `origen_id` no es el pedido o es de una empresa fuera del token. Para Iris solo rechaza la conciliación; nunca equivale a «no existe». Una factura anulada no es 404 |
| §4.3, §3 (A36), §12 | 422 | `capacidad_no_disponible` | (0.14.3) La función no está en `true` en `capacidades.equipos` para la empresa del servicio |
| §12.2 | 422 | `equipo_invalido` | `ont` sin serie, `cpe_wireless` sin MAC, `planta` con wireless, formato, o el tipo no coincide con la tecnología del servicio |
| §12.2 | 422 | `aprovisionamiento_no_disponible` | La empresa no tiene SmartOLT o ACS configurado |
| §12.2 | 409 | `unidad_en_otra_bodega` | El equipo escaneado está en inventario, pero no en una bodega permitida ni asignado al servicio (sin decir dónde está) |
| §12.2 | 422 | `wifi_no_disponible` | `wifi` con un equipo que Argos no gestiona (wireless sin TR-069) |
| §12.4 | 422 | `verificacion_requerida` | Petición del asistente de IA sin `verificacion`, con otro `metodo` o con `verificado_en` de más de 10 min |
| §12.5 | 409 | `servicio_no_activo` | Reenviar la bienvenida de un servicio que no está activo |
| §12.5 | 429 | `bienvenida_reciente` | Reenviar antes de 10 min del último envío (con `Retry-After`) |
| §6.2 | 422 | `origen_invalido` | `origen` con un tipo distinto de `iris_orden` o un id que no es de una orden de Iris de ese servicio |
| §12.4 | 422 | `wifi_invalido` | SSID de 1 a 32 caracteres, clave de 8 a 63 (WPA2), sin caracteres de control |
| §12.4 | 422 | `verificacion_requerida` | Falta `verificacion`, su `metodo` no vale para ese actor, tiene más de 10 min (asistente), o su `cliente_id` no es el titular del servicio |
| §12.4 | 429 | `demasiados_cambios_wifi` | Más de 3 cambios de Wi-Fi por servicio en el día (con `Retry-After`) |
| §12.5 | 429 | `demasiados_reenvios` | Más de 3 reenvíos de la bienvenida por servicio en el día (con `Retry-After`) |
| §6.2 (`bienvenida_servicio`) | 422 | `validacion` | Una variable fuera de forma (§12.5); el aviso no sale |
| §12.3, §12.4 | 409 | `equipo_sin_gestion` | El servicio no tiene un equipo gestionado por SmartOLT o el ACS |
| §12.3 | 503 | `equipo_sin_respuesta` | SmartOLT o el ACS no respondieron a tiempo (se puede reintentar a mano) |

### 4.4 Compatibilidad de la 0.10.0

La 0.10.0 es aditiva sobre la 0.9.0. Cada lado usa sus funciones **solo si el otro informó un contrato ≥ 0.10.0** (como en §4.3).

| Función 0.10.0 | Iris 0.10.0 con un Argos < 0.10.0 | Argos 0.10.0 con un Iris < 0.10.0 |
| --- | --- | --- |
| Pendiente sin IP (A38) | El servicio pendiente trae IP como hoy; Iris no llama a `reservar-ip` | Un Iris anterior no usa la IP del pendiente (la activación la asigna); si el técnico la necesitaba antes de activar, la ve en Argos |
| Identidad, titular, plan o datos (A39, A40, A41, A43) | Iris no ofrece el paso «Confirmar titular y plan»; el cambio y la identidad los registra una persona en Argos | Nadie llama a las rutas |
| `servicio.titular_cambiado`, `cliente.identidad_registrada`, `cliente.contactos_cambiados` | No llegan; Iris relee el servicio cuando le llega `servicio.datos_cambiados` o al activar | Argos no lo envía a conectores con contrato < 0.10.0 |
| `modo` y `routers` en planes | Iris ofrece los planes de `GET /planes?router_id=` y Argos valida al activar (`422 plan_no_permitido`) | Iris ignora los campos que no conoce |
| Contactos (A42) | Iris asocia por `GET /clientes?telefono=` (§7.6) como hoy, sin rol ni permisos; no crea contactos en Argos | Nadie llama a las rutas ni recibe `cliente.contactos_cambiados`; los avisos a contactos los manda Argos por su `notify` |
| Prospectos desde Argos (A44) | La ruta existe; nadie la llama; Iris no envía `prospecto.estado_cambiado` | La ruta no existe (404): Argos no ofrece «Registrar en Iris» |
| Código y nombre del servicio (A45) | No vienen: Iris muestra el servicio como hoy y el chat pide otra referencia (la dirección que escribe el cliente) | Iris ignora los campos que no conoce |
| Un cliente a cargo del servicio de otro (A46, 0.10.1) y `X-Iris-Contacto` en §7.1 | Con Argos < 0.10.1, `persona_cliente_id` y `a_cargo` no vienen: Iris no ofrece «los que tiene a cargo» y trata cada contacto por separado | Con Iris < 0.10.1, Argos no manda `persona_cliente_id` en el evento; Iris ignora los campos que no conoce |
| Bodega de cuadrilla (A47 y B26, 0.11.0) | Con Argos < 0.11.0, `mi-bodega` trae solo la bodega personal y los consumos salen de ella como hoy; Iris no muestra la pestaña «Cuadrilla» ni manda `bodega_id` de cuadrilla | Con Iris < 0.11.0, no llegan `cuadrilla.*` con miembros: Argos no crea bodegas de cuadrilla para ese conector |
| Equipos del cliente (A48 y B27, 0.12.0) | Con Argos < 0.12.0, el cambio sale como devolución más asignación; «No se pudo recuperar» queda pendiente en Iris para una persona con `ordenes.declarar_perdida` (supervisor) y nunca sale solo desde el cierre; Iris no muestra estados ni historial de la unidad | Con Iris < 0.12.0, Argos no envía `inventario.equipo_cambiado` ni `inventario.unidad_estado_cambiado`. Con contrato ≥ 0.12.0, Argos rechaza `…/perdida` con el token de Iris (`403`) |
| Clave del consentimiento por número y tickets por contacto (0.12.1) | Con Argos < 0.12.1, la clave del consentimiento con `telefono` es la anterior (sin `<h>`) y no llega `X-Iris-Contacto` en el portal: Iris trata la sesión como del titular, como hoy. **Con Argos < 0.12.1, el portal nunca abre a un contacto los tickets de otro cliente** | Con Iris < 0.12.1, un `X-Iris-Contacto` en §11 se ignora y la sesión se ve como del titular: Argos no manda al portal sesiones de contactos con un Iris anterior |
| Caídas, falla general, gestiones de cobro y plantillas (0.13.0) | Con Argos < 0.13.0: `servicio.caidas_repetidas` llega con el umbral anterior de Argos (5 en 120 min) e Iris aplica el suyo sobre lo que llega; no hay `red.falla_general_*` (Iris sigue con los maestros de Horus); Iris guarda las gestiones de cobro solo en su lado | Con Iris < 0.13.0: Argos no envía `red.falla_general_*`; nadie llama a `gestiones-cobro`; la ruta de plantillas no existe (404) y Argos usa los códigos de §6.3 como hoy |

Errores nuevos: los de §3.4.7.

### 4.5 Transición a la 0.14.0

La 0.14.0 no es aditiva: cambia claves, reintentos y lo que cuenta como intento de recuperación. Cada lado usa lo nuevo solo si el otro informó un contrato ≥ 0.14.0 (como en §4.3), salvo lo que se indica.

- **Foto y verificación.** Argos exige `foto_ref` con cualquier `motivo` y verifica el intento (§3.10) solo con un Iris que haya informado contrato ≥ 0.14.0.
- **Sin retroceso.** Un `PUT /integraciones/iris/contrato` con una versión menor que la última aceptada responde `409 contrato_regresivo` y avisa al admin; para bajar de versión hace falta un canje nuevo aprobado en el panel de Argos. Una versión mayor que el `contrato` que habla Argos responde `422 contrato_no_soportado` y no se registra (§3.3).
- **Intentos anteriores.** Un intento que Argos no pudo verificar con la ruta de §3.10 (anterior a la 0.14.0, o de un conector con contrato < 0.14.0) se muestra «anterior a la verificación»: no cambia la unidad, no avisa al titular y no cuenta para el mínimo. Decide una persona.
- **Con Argos < 0.14.0.** Un solo intento por unidad y orden (la clave anterior no lleva `intento_id`); el segundo queda `rechazada` con `intento_repetido`.
- **Intento sin foto.** Un intento encolado sin `foto_ref` no sale hacia un Argos ≥ 0.14.0: queda como conflicto para una persona.
- **Pendientes.** Un pendiente conserva la `Idempotency-Key` con la que se encoló, aunque el otro lado cambie de versión antes de enviarlo.
- **Permiso.** Iris agrega `adjuntos.verificar_integracion` a los `iris_pat_` vigentes cuando informa 0.14.0.
- **Claves nuevas.** `iris:cambio:…:<operacion_id>`, `iris:no_recuperado:…:<intento_id>` y el consumo por actor (`iris:consumo:<orden_id>:<usuario_id>:<bodega_id>`) solo se usan con un Argos que haya informado contrato ≥ 0.14.0; con uno anterior, las de la 0.12.0 y `iris:consumo:<orden_id>`.
- **Vínculo y actor.** Iris manda `tecnico.argos_*` (§6.6) a conectores con contrato ≥ 0.14.0, con un `vinculado` por cada vínculo vigente al informar 0.14.0 y el cierre de esa carga con `vinculos_cargados`; Argos responde `403 actor_desconocido` y exige `actor_no_coincide` en la bodega personal solo con contrato ≥ 0.14.0 (§3.9).
- **Reintentos.** Desde 0.14.0, Iris trata como definitivos `demasiados_intentos` en `…/no-recuperado` y los `403` de negocio de §3.9 por su URN completa, cualquiera que sea la versión de Argos.

### 4.6 Compatibilidad de la 0.15.0 (propuesta aceptada con ajustes)

La 0.15.0 es aditiva sobre la 0.14.3. Un conector que informa un contrato menor no recibe nada nuevo y todo sigue como hoy. Cada lado usa un bloque **solo si el otro informó un contrato ≥ 0.15.0 y la marca del bloque** (como en §4.3). Las marcas se **agregan a las `capacidades` de §4.3** (junto a `portal_argos`, `solicitudes_servicio` y `adjuntos_ticket`; no hay un objeto nuevo): `{ …, tipo_servicio: boolean, corte_manual: boolean, mensajeria: [{ empresa_id, entrega_auto: boolean, canales: [{ canal, codigos: string[] }] }] }`. Iris puede informar 0.15.0 con solo algunos bloques en `true`.

- **Fuente de las marcas.** Argos las lee **solo** de `GET /integraciones/argosisp/capacidades` (§4.3), nunca de `…/estado`, que es para personas. `mensajeria[]` trae solo las empresas que están en el alcance del conector.
- **Cuándo las relee Argos:** (1) al activar la integración; (2) en la revisión periódica de versión; (3) ante un `422 canal_no_permitido` o un aviso `auto` que vuelve `bloqueado` con `canal_no_disponible`.
- **Pendiente en Iris:** `GET …/capacidades` aún no existe en Iris (B16 de la 0.14.2). B33, B34 y B35 dependen de esa ruta.

| Función 0.15.0 | Iris 0.15.0 con un Argos < 0.15.0 | Argos 0.15.0 con un Iris < 0.15.0 (o sin la marca) |
| --- | --- | --- |
| Bloque A: `tipo_servicio` y `componentes[]` (§13.1) | Los campos no llegan: Iris trata todo servicio como `internet`, con router y velocidad, como hoy | Argos no envía los campos nuevos en los webhooks a ese conector (la lista `EVENTOS_DESDE_0150`). `GET /servicios` los trae siempre, aditivos; un Iris que no los conoce los ignora |
| Bloque B: corte manual (§13.2) | Iris no recibe `servicio.corte_manual_*`; nadie envía `corte_manual.estado_cambiado` | Argos no envía el evento; la solicitud queda `sin_iris`, se ve en la ficha del servicio y sale una alerta interna de su notify |
| Bloque C: entrega por Iris (§13.3) | Iris no recibe `canal_preferido: "auto"`; los avisos llegan solo con `whatsapp` y los códigos de §6.3 | Argos sigue como hoy: WhatsApp por Iris con `canal_preferido: "whatsapp"` (solo los códigos que ese Iris declara) y los canales que Iris no declara, desde Argos. Con Iris < 0.6.0 o apagado, correo y Telegram de Argos |
| `capacidades.mensajeria[]` | Argos no la lee | Sin la marca, Argos entiende `entrega_auto = false` para todas las empresas |
| Código nuevo pedido a un Iris que no lo declara en `canales[].codigos` del canal | — | Ese paso cae al texto local de Argos, sin intentar el `422` |

Errores nuevos (0.15.0):

| Sección | Estado | Código | Cuándo |
| --- | --- | --- | --- |
| §6.2, §13.3 | 422 | `canal_no_permitido` | `canales_permitidos` enviado sin `canal_preferido: "auto"`; lista vacía; valores repetidos; o un canal que Iris no declara para la empresa. Solo la **ausencia** de la lista significa «todos los declarados» |
| §6.2, §13.3 | 422 | `variable_invalida` | `concepto` de más de 60 caracteres, con salto de línea o con una URL |
| §13.1 | 422 | `tipo_sin_router` | `reservar-ip`, `aprovisionar`, `GET /planes?router_id=` o `cambiar-plan` con un servicio de cable; no cambia nada. Desde 0.15.1, `cambiar-plan` ya no (§13.4) |
| §13.4 (0.15.1) | 422 | `router_requerido` | `cambiar-plan` o `cambiar-tipo` a un plan que agrega `internet` o `iptv`, sin `router_id`; no cambia nada |
| §13.4 (0.15.1) | 422 | `servicio_no_activo` | `cambiar-tipo` con el servicio en otro estado que `activo` (en `pendiente_instalacion` se usa `cambiar-plan`) |
| §13.4 (0.15.1) | 422 | `plan_no_permitido` | `cambiar-tipo` a un plan inactivo, de otro `modo` o no asignado al router indicado |
| §13.4 (0.15.1) | 409 | `conflicto_version` | `cambiar-tipo` con una `version` del servicio que ya no es la actual |

Y los de §6.2.

## 5. Solicitudes

### 5.1 Iris solicita a ArgosISP

| # | Pedido | Estado |
| --- | --- | --- |
| A1 | **Servicio pendiente de instalación.** Estado inicial `pendiente_instalacion`: sin RADIUS, sin facturación y sin corte. Acción `servicios.activar_instalacion` (`POST /servicios/:id/activar-instalacion`), que lo pasa a `activo` y fija la fecha de inicio de facturación. Decidido por el PM (28 oct): un servicio vendido desde Iris nace `pendiente_instalacion` y la facturación arranca al activarse la instalación. | listo en Argos (sale en 1.2.0) |
| A2 | **Conector Iris** en Argos, como el de Horus: token del usuario técnico «Iris» con los permisos de §1.1, enlace de conexión, webhooks de §2.3 y autologin hacia `ticket`, `orden` y `prospecto`. | solicitado |
| A3 | **Origen en los movimientos de inventario:** aceptar `origen_tipo = 'iris_orden'` y `origen_id` en consumos, comodatos y devoluciones, y mostrarlos en el kardex con enlace a Iris. | solicitado |
| A4 | **Cargo único desde una orden:** el endpoint `POST /facturas/cargo-unico` ya existe. Solo falta aceptar `origen_tipo = 'iris_orden'`/`origen_id` (como A3) y dar el permiso al usuario técnico «Iris». | solicitado |
| A5 | ~~Cobertura por coordenada.~~ **Reemplazado por la integración Iris ↔ OZmap** (decisión del PM, 28 oct): la planta externa de fibra (cajas, splitters, OLT/slot/PON, potencias y viabilidad) vive en OZmap, y Argos no la lleva. | reemplazado |
| A6 | **Webhook de caídas repetidas** de un servicio (n sesiones cortadas en x horas) para tickets proactivos. Opcional. Argos emite `servicio.caidas_repetidas` (§2.3); llega a Iris por el webhook del conector (A2). | evento listo en Argos; el envío sale con el conector |
| A8 | **Resolver IP con la IP en el cuerpo** para el conector Iris (`POST /integraciones/iris/resolver`), igual que el de Horus. | solicitado |
| A9 | **Referencia de planta en el servicio.** `POST /servicios/:id/activar-instalacion` acepta un objeto opcional `planta { caja, puerto, olt, slot, pon, potencia_dbm }` (todos los campos opcionales; `potencia_dbm` decimal). Argos lo guarda tal cual en el servicio y lo muestra en su ficha. Es una referencia de lectura: la fuente de verdad sigue siendo OZmap. | listo en Argos (sale en 1.2.0) |
| A7 | **Ficha del cliente en Argos con sus tickets y órdenes de Iris**, leyendo `GET /api/v1/tickets?cliente_id=` con el token de §1.2, y botón «Ver en Iris». **Nota de Iris (21 nov 2026):** vale para `GET /api/v1/tickets` y para las órdenes de la ficha, `GET /api/v1/ordenes?cliente_id=`. El nombre del filtro en la API de Iris es `clienteId`; Iris acepta `cliente_id` como alias (y `servicio_id` de `servicioId`), y si llegan los dos con valores distintos responde `422 validacion`. Con el token de §1.2 el filtro de cliente es obligatorio en las dos listas: sin él, `422 filtro_cliente_requerido` (nunca la lista de toda la empresa). Un parámetro de consulta desconocido responde `422 validacion` (regla `desconocido`). Antes de este cambio Iris ignoraba `cliente_id` y devolvía todos los tickets y todas las órdenes de la empresa: Argos no debe mostrar esas listas a un cliente con un Iris anterior. | solicitado |
| A16 | **Comprobantes de pago desde Iris** (0.6.0, §7.2): `POST /comprobantes` (multipart) con la boleta que mandó el cliente, `canal = iris` en la cola de caja que ya existe, y webhooks `comprobante.aprobado` / `comprobante.rechazado` con `causa` y `servicio_estado`. | listo en Argos (1.x, PR #217): `POST /comprobantes` multipart con canal `iris`, filtro `canal=iris` en la cola de caja y webhooks `comprobante.*` con `causa` y `servicio_estado` |
| A17 | **Reconexión temporal** (0.6.0, §7.3): `POST /comprobantes/:id/reconexion-temporal`, como promesa de tipo `reconexion_temporal` con la política de la plantilla, y el webhook `promesa.cancelada`. | listo en Argos (1.x, PR #217): promesa `reconexion_temporal` con la política de la plantilla (apagada por defecto) y `promesa.cancelada` |
| A18 | **Promesas de pago** (0.6.0, §7.4): `POST /promesas/simular`, `POST /promesas` y los webhooks `promesa.*`. | listo en Argos (1.x, PR #217): permisos de Iris, canal `ia` u `operador` según `X-Iris-Actor` y webhooks `promesa.*` |
| A19 | **Verificar identidad sin exponer el documento** (0.6.0, §7.5): `POST /integraciones/iris/verificar-identidad` y `nit_cui` oculto para el usuario técnico Iris. | listo en Argos (1.x, PR #217): `{ coincide }` con 5 intentos fallidos cada 30 min; `nit_cui` sale `***` al usuario técnico Iris (null si el cliente no tiene documento) |
| A20 | **Buscar cliente por teléfono** (0.6.0, §7.6): `GET /clientes?telefono=` con coincidencia exacta sobre el número normalizado y `coincide_en[]`. | listo en Argos (1.x, PR #217): filtro exacto `telefono` con `coincide_en[]`; con el token de Iris exige las cabeceras de §7.1 |
| A21 | **Avisos de Argos por la mensajería de Iris** (0.6.0, §6): canal `whatsapp` de `notify` que llama a §6.2, envío de permisos de contacto desde el panel y el portal, y receptor de webhooks `/integraciones/iris/webhook`. | listo en Argos (1.x, PR #217): canal `whatsapp` de `notify`, consentimientos desde el panel y el portal, receptor `/integraciones/iris/webhook` y consulta `GET /mensajeria/avisos/:id` de los envíos `encolado` con más de 24 h |
| A22 | **Nombre de la bodega del técnico** (0.6.4, §3.3): `nombre` en cada elemento de `bodegas[]` de `GET /inventario/tecnicos`, para que Iris elija la bodega de una lista sin teclear números. | listo en Argos |
| A23 | **Aprobar y rechazar comprobantes desde Iris** (0.7.0, §7.7): `POST /comprobantes/:id/aprobar` y `POST /comprobantes/:id/rechazar` con el token de Iris, solo `canal = iris`, autorizadas por el usuario de Argos vinculado al `X-Iris-Actor` (los cinco `403 actor_*`), con el actor en la huella de idempotencia y la auditoría a nombre del usuario vinculado. Permisos `comprobantes.aprobar` y `comprobantes.rechazar` en el token de Iris, con migración de los que existen. `destino` opcional en `POST /integraciones/iris/sso/tickets` (§1.3). Pedido del PM. | en curso en Argos |
| A24 | **Quién decidió el comprobante e instalación en el vínculo** (0.7.1): `verificado_iris_actor` en el `Comprobante` (`GET /comprobantes/:id`, la lista y la respuesta de aprobar y rechazar, §7.7) y en los webhooks `comprobante.aprobado` / `comprobante.rechazado` (§7.2), con `null` cuando decide caja en el panel. `instalacion` (string o `null`) en `vinculacion` del canje del autologin Iris → Argos y la pantalla de confirmación del vínculo con el sistema, el nombre y el correo del usuario de Iris, la instalación y el aviso, antes de pedir el correo y la contraseña (§1.3). | en curso en Argos |
| A25 | **Reconexión temporal sin personal de caja** (0.8.0, §7.3): `motivo: "sin_personal"` en `POST /comprobantes/:id/reconexion-temporal`, `vigencia: "horas" \| "medianoche"` en la plantilla y `motivo_reconexion` en la promesa y en los webhooks `promesa.*`. Además: el filtro `motivo_reconexion` en `GET /promesas` y en la lista de promesas del panel, y corregir la descripción de `hasta` en el OpenAPI («según `vigencia`»). | implementado en Argos (PR #249, sale en 1.18.0) |
| A26 | **Cuentas bancarias como única fuente** (0.8.0, §7.8): `titular` (string o `null`, 2–120) y `tipo` (`monetaria` \| `ahorro` o `null`) en la cuenta; el webhook `cuenta_bancaria.cambiada` `{ cuenta_bancaria_id, empresa_id, campos[], activa, creada }`, que se emite **también al crear** (`creada: true`), solo a conectores con contrato ≥ 0.8.0; y `rule: "inactiva"` (hoy `exists`) al registrar un comprobante en una cuenta desactivada. La lista sigue siendo un arreglo sin paginar. Las desactivadas vienen con `activa: false` y nunca se borran (ya es así). | implementado en Argos (PR #249, sale en 1.18.0) |
| A27 | **Más datos del servicio** (0.8.0, §2.4): `precio_mensual` y `moneda` en `GET /servicios/:id` y en la lista; `precio` en `campos[]` de `servicio.config_cambiada` y de `catalogo.cambiado` de plan. `router_nombre` no se pide: ya existe. Caja o nodo: no; sigue siendo `planta`, con OZmap como fuente. Además, `excluir_retirados=true` en `GET /clientes` y `GET /servicios` (§2.5). | implementado en Argos (PR #249, sale en 1.18.0) |
| A28 | **Conexión de un paso, lado Argos** (0.8.0, §1.5 y §2.5). El receptor de Iris ya se guarda en el canje; lo que falta es: (1) validar en el canje el host del receptor y el origen aprobado (`422 webhook_invalido`, `mismo_host` \| `origen_aprobado`, con el código gastado); (2) encender el conector al confirmarse un canje del botón; (3) `PUT /integraciones/iris/sincronizacion` y `sincronizacion` en `GET /integraciones/iris`; (4) las cabeceras de Iris opcionales en `GET /cuentas-bancarias` y `GET /bancos`; (5) la pantalla de estado en el panel de Argos y el texto «Iris copiará los clientes, los servicios y las cuentas bancarias de estas empresas» en la pantalla de aprobación y en la del enlace; (6) rotular la generación manual de tokens como «vía de recuperación» (S5). **Sin huella TLS:** Argos no la muestra. Con los requisitos S1–S8. | implementado en Argos (PR #249, sale en 1.18.0) |
| A29 | **Búsqueda sin datos de red** (0.8.1, §2.1): `buscar_red` opcional (por defecto `true`) en `GET /clientes?q=` y `GET /servicios?q=`; con `false`, `q` no coincide por la IPv4 vigente, una IPv6 dentro de un prefijo vigente ni el `user_ppp` de los servicios (§2.1 dice qué sigue coincidiendo en cada lista); un valor que no sea `true`, `false`, `1` ni `0` responde `422 validacion` (`field: buscar_red`). | implementado en Argos, sale en 1.21.0 |
| A30 | **Portal del cliente con tickets de Iris** (0.9.0, §11): pantallas «Mis tickets», «Nuevo ticket» (categorías de `GET …/categorias`, servicio del cliente, hasta 3 adjuntos), detalle con mensajes públicos, responder y «Confirmar solución»; `X-Argos-Portal-Sesion` en cada llamada; `Idempotency-Key` por envío de formulario; con sesión `ip`, solo lo que permite §11.2 (y solo si la IP corresponde a un único servicio vigente; nunca detrás de CGNAT); `cliente_id` y `X-Iris-Empresa` salen de la sesión en el servidor del portal, nunca del navegador; el `token_portal` de §1.2 solo en ese servidor. Recibir `ticket.estado_cambiado` y `ticket.respondido` (§6.6) para avisar por `notify` (sin repetir el correo cuando `aviso_por_correo = true`, y un solo aviso por cliente cuando `masivo = true`). Usar las rutas solo con Iris en contrato ≥ 0.9.0 y `capacidades.portal_argos`. | acordado; pendiente en Argos |
| A31 | **Solicitar servicio desde el portal** (0.9.0, §11.7): formulario con tipo (`alta`, `cambio_plan`, `traslado`, `otro`), servicio afectado, dirección y coordenadas nuevas (alta y traslado), plan de interés y comentario; lista «Mis solicitudes» con `estado_cliente`; recibir `solicitud_servicio.estado_cambiado` para avisar. | acordado; pendiente en Argos |
| A32 | **Aprovisionar desde la orden de Iris** (0.9.0, §12.2): `POST /servicios/:id/aprovisionar` asíncrono para FTTH y wireless (IPAM, autorización de la ONT en SmartOLT por serie, del CPE/radio por MAC o del router por TR-069 con el perfil y la VLAN del plan y del router/OLT/AP, Wi-Fi inicial cuando el equipo es gestionado, y activación con el inicio del cobro mensual), `GET /aprovisionamientos/:id`, webhooks `instalacion.aprovisionada` / `instalacion.fallida` y `capacidades.aprovisionamiento` por empresa. Permiso `servicios.aprovisionar` con migración de tokens. 0.14.3: con la orden cerrada, `tecnico` solo si tiene I1 en la empresa del servicio; sin él, la unidad ya asignada al servicio; búsqueda por `unidad_id`, `serie` o `mac`; `409 aprovisionamiento_en_curso` con `iniciado_por`; `GET /aprovisionamientos/:id` limitado a las empresas del token, recortado para un aprovisionamiento ajeno y con `origen_tipo: argos_panel`; sin `instalacion.*` de lo ajeno; marca `wifi` en el `wifi` de `aprovisionar`. | acordado; pendiente en Argos |
| A33 | **Estado del equipo** (0.9.0, §12.3): `GET /servicios/:id/equipo` con en línea, señal óptica, firmware y último contacto. Permiso `servicios.ver_equipo`. 0.14.3: marca `estado_equipo` en `capacidades.equipos` y `422 capacidad_no_disponible` (§4.3). | acordado; pendiente en Argos |
| A34 | **Cambio de plan y traslado por API** (sin forma; para después de 0.9.0): rutas para que Iris aplique en Argos un cambio de plan o de dirección ganado en ventas (§11.7). Mientras no existan, lo hace una persona en Argos. | propuesto (sin forma) |
| A35 | **Cambiar el Wi-Fi pedido desde Iris** (0.9.0, §12.4): `POST /servicios/:id/wifi` asíncrono y webhook `equipo.wifi_resuelto`, para personas y para el asistente de IA (canal `ia`), siempre con `verificacion { metodo, verificado_en, cliente_id }` (asistente: menos de 10 min y solo código o documento; si no, o si el servicio no es de ese cliente, `422 verificacion_requerida`); máximo 3 cambios por servicio al día (`429 demasiados_cambios_wifi`) y aviso al titular por sus contactos registrados después de `aplicado`. Permisos separados `servicios.ver_equipo` y `servicios.cambiar_wifi`. La clave nunca se guarda en claro ni sale en auditoría, eventos, logs ni historial del chat. 0.14.3: marca `wifi` en `capacidades.equipos` y `422 capacidad_no_disponible` (§4.3). | acordado; pendiente en Argos |
| A36 | **Webhooks de facturas y pagos del cobro de la orden** (0.9.0, §3): `POST /facturas/cargo-unico` ya existe y acepta `origen_tipo = iris_orden` + `origen_id` (A4), con el permiso `facturas.emitir_cargo_unico`; **su respuesta no cambia**: `201 CargoUnicoEmitido = { documento: 'factura' \| 'bonificacion', factura: FacturaDetalle \| null, bonificacion: Bonificacion \| null }`. Iris lee `factura.id`, `factura.numero` (el del DTE, o `null` si aún no está certificada, como en `factura.emitida`; desde 0.14.3 Iris muestra la factura como «Factura #<factura_id>» y sus tipos aceptan `numero` null), `factura.estado`, `factura.total` y `factura.moneda`; `documento = bonificacion` (cargo en Q0 sin factura) es una respuesta posible, pero Iris nunca la pide: no manda Q0 ni tiene `facturas.cargo_sin_costo`. Cuerpo: `{ servicio_id, cliente_id?, vence_en?, lineas: [{ concepto_id, precio_unitario, descripcion?, cantidad?, descuentos? }], origen_tipo: "iris_orden", origen_id }`; `concepto_id` de `GET /conceptos-cargo` (el de instalación es el que trae `codigo_sistema: "instalacion"`, sembrado por empresa, §4.3; los conceptos no tienen precio) y `precio_unitario` (> 0, bruto con IVA) lo pone Iris. **Lo nuevo:** enviar a Iris `factura.emitida`, `factura.anulada`, `pago.registrado` y `pago.anulado`, solo de facturas con `origen_tipo = iris_orden`, con `origen_id` en el payload y a conectores con contrato de Iris ≥ 0.9.0 (hoy solo se envían `comprobante.*` y `promesa.*`). 0.14.3: marca `cobro_equipo` (líneas con el concepto de instalación) y `422 capacidad_no_disponible` (§4.3); `GET /api/v1/facturas/origen-iris` (permiso `facturas.leer_origen_iris`, `404` único, 60 por minuto; la respuesta suma `saldo` y devuelve también las facturas anuladas, con `estado: anulada`, nunca `404`) para conciliar un «cobro por confirmar»; desde 0.14.3, cada elemento de `GET /conceptos-cargo` suma `codigo_sistema` (string o `null`): el concepto sembrado de instalación trae `codigo_sistema: "instalacion"` en cada empresa, y Argos no permite cambiarlo ni dárselo a otro concepto. Iris lo identifica solo por ese campo, nunca por el nombre. | acordado; pendiente en Argos |
| A37 | **Bienvenida al activar, con seguimiento** (0.9.0, §12.5): pedir `bienvenida_servicio` (con `nombre`, `plan`, `precio_mensual`, `moneda`, `codigo_cliente`) en lugar de `bienvenida` cuando Iris informa contrato ≥ 0.9.0, con `origen: { tipo: "iris_orden", id }` si la activación vino de una orden de Iris; el webhook `servicio.bienvenida_cambiada` con el resultado de cada canal (también correo y Telegram); y `POST /servicios/:id/bienvenida/reenviar` (permiso `servicios.reenviar_bienvenida`; máximo 3 por servicio al día, `429 demasiados_reenvios`). 0.14.3: marca `bienvenida` en `capacidades.equipos` (con `false`, Argos pide `bienvenida` e Iris no rechaza por la marca); reenviar sin la marca, `422 capacidad_no_disponible` (§4.3). | acordado; pendiente en Argos |
| A38 | **Servicio pendiente sin IP** (0.10.0, §3.4.1): `POST /servicios` con `estado_inicial: pendiente_instalacion` no asigna IPv4 ni prefijos; el IPAM asigna con el router final en `activar-instalacion` o `aprovisionar`; `POST /servicios/:id/reservar-ip` para reservarla en sitio (IP fija o amarre); `422 sin_ip_disponible`. Cambia `contratos/instalacion.md` §1 de Argos («un servicio pendiente ya tiene IP asignada»). | acordado; pendiente en Argos (incluye la reserva en sitio, ajuste a) |
| A39 | **Cambiar el titular antes de activar** (0.10.0, §3.4.4): `POST /servicios/:id/cambiar-titular` en multipart con los datos del titular (§3.4.2), su identidad (A41), `motivo` y `version`. Iris nunca manda `cliente_id`: Argos usa el cliente existente solo si coinciden documento **y** nombre normalizado en la misma empresa (`titular: { cliente_id, existente: true }`, identidad como versión nueva, aviso al titular), responde `422 titular_no_coincide` si coincide el documento y no el nombre, y si no crea uno nuevo (`posible_duplicado` solo con nombre o teléfono iguales y documento distinto); el cliente anterior sin historial se elimina; evento `servicio.titular_cambiado`; permiso `servicios.cambiar_titular`. | acordado; pendiente en Argos |
| A40 | **Cambiar el plan antes de activar** (0.10.0, §3.4.5): `GET /planes?router_id=&modo=` solo con los aplicables (`modo` en el plan; `modo` y `routers` en `catalogo.cambiado`) y `POST /servicios/:id/cambiar-plan` en pendiente, con motivo (`mas_velocidad`, `menos_velocidad` u `otro`) y sin factura; `422 plan_no_permitido`, también al activar si el router final cambió; permiso `servicios.cambiar_plan_pendiente`. | acordado; pendiente en Argos |
| A41 | **Identidad del titular** (0.10.0, §3.4.2 y §3.4.3): `POST /clientes/:id/identidad` en multipart (frente obligatorio, reverso opcional, `documento_tipo`, `documento_numero`, `nombre`, `mayor_de_edad`, `coincide`, `verificado_en`, `metodo: foto_documento`), con `X-Iris-Actor`, solo para el cliente de un servicio pendiente con orden de Iris en curso; guardada cifrada en Argos, visible solo con `clientes.ver_identidad` (cada vista auditada), nunca por portal, webhooks, eventos, logs ni MCP; versiones anteriores conservadas y borradas 12 meses después de retirado el cliente; evento `cliente.identidad_registrada`; permiso `clientes.registrar_identidad`. | acordado; pendiente en Argos |
| A42 | **Contactos del cliente** (0.10.0, §3.5): varios contactos por cliente con `relacion`, `servicios[]`, `permisos` (`consultar`, `reportar`, `pagar`, `avisos`) y `telefonos[]` (E.164, WhatsApp, principal); lo reservado al titular siempre con la identidad de A41; `GET /contactos/buscar?telefono=` (sin otros números ni documentos, 60 por minuto), `GET/POST /clientes/:id/contactos`, `PUT/DELETE /contactos/:id` con `version` e idempotentes; `cliente.contactos_cambiados` sin números; avisos de cobro también a los contactos con `avisos`, con el permiso de cada número; permisos `contactos.buscar` y `contactos.gestionar`. | acordado; pendiente en Argos (decidido por el PM; incluye los ajustes g, h, i y k) |
| A43 | **Completar los datos en la instalación** (0.10.0, §3.4.6): `POST /servicios/:id/completar-datos`, solo con el servicio pendiente, solo para el titular de ese servicio y solo llenando campos vacíos (`422 campo_ya_registrado`), salvo el `nombre`, que se corrige con una identidad registrada en esa instalación; dirección, referencia y coordenadas de la instalación sí se corrigen; permiso `servicios.completar_datos`. Reemplaza el `PUT /clientes/:id` general de la propuesta de Argos. | acordado; pendiente en Argos (ajuste d) |
| A44 | **Registrar prospectos en Iris desde Argos** (0.10.0, §3.6): «Registrar en Iris» en el panel de Argos (permiso `prospectos.registrar`: admin, cajero y soporte) con `POST <iris>/api/v1/integraciones/argosisp/prospectos`, `X-Iris-Empresa`, `X-Iris-Actor` e `Idempotency-Key = argos:prospecto:<uuid>`, con `telefonos[]` (1 a 5, un `principal`); guardar solo el vínculo y reenviar desde su cola con la misma clave; recibir, si quiere, `prospecto.estado_cambiado`. Además, `telefonos_adicionales[]` en `POST /clientes` (§3.2) para los números sin nombre al ganar. | acordado; pendiente en Argos |
| A45 | **Código y nombre del servicio** (0.10.0, §3.7): `servicios.codigo` automático por empresa, legible, único e inmutable (`S-000412`) y `servicios.nombre` opcional y editable (hasta 60); en `GET /servicios`, el detalle y como `servicio_codigo` / `servicio_nombre` en los payloads hacia Iris que llevan `servicio_id`; `POST /servicios` acepta `nombre`. | acordado; pendiente en Argos |
| A46 | **Un cliente a cargo del servicio de otro** (0.10.1, §3.8): `contactos_cliente.persona_cliente_id` (misma empresa; desde Iris solo con el teléfono verificado de ese cliente); aviso a los dos titulares; vínculo roto al quitar el contacto; `a_cargo` en `GET /contactos/buscar`; validación de `X-Iris-Contacto` (§7.1); pestaña «Contactos» en la ficha y cuentas a cargo aparte en caja; `persona_cliente_id` en `cliente.contactos_cambiados`. | acordado; pendiente en Argos |
| A47 | **Bodega de cuadrilla** (0.14.0: `actor_tecnico` también en la bodega personal; vínculo de cada técnico por `tecnico.argos_*` con `version`, validado (`vinculo_rechazado`), separado del de §7.7 y conciliado con `GET <iris>/…/tecnicos/:usuario_id` con las reglas del webhook y `activo: false` como desvinculado; reevaluar los rechazados por unicidad al liberarse un `id_externo`; `409 vinculos_en_carga` hasta el cierre con `vinculos_cargados` y después `403 actor_desconocido`, solo con `tecnico`, y `403 actor_no_coincide` también en la bodega personal; consumo por actor y bodega; `403` de negocio reconocidos por la URN completa y sin reintento) (0.11.0, §3.9): bodega `tipo = cuadrilla` por cuadrilla y empresa, con `jefe_id` y miembros con historial, sincronizados desde `cuadrilla.*` de Iris por `version` (miembros solo activos, técnicos de esa empresa y dentro del alcance, los demás como `miembro_rechazado`; jefe solo si es miembro con I1); `bodega_id` validado antes de mirar existencias (`403 no_es_miembro`); `X-Iris-Actor` obligatoria y `403 actor_no_coincide`; `tecnico` solo por `id_externo`; comodatos y devoluciones sin `bodega_id`, con `409 unidad_en_otra_bodega`; membresía evaluada en `ocurrido_en` (`fuera_de_membresia`); `409 sin_existencia` solo con las bodegas accesibles; recepción solo del jefe en `recibido_en` y aún miembro (`403 solo_jefe_recibe`); `cuadrilla_id` en `inventario.traslado_*`. | acordado; pendiente en Argos |
| A48 | **Equipos del cliente** (0.14.0: `409 contrato_regresivo` y `422 contrato_no_soportado`; cupo diario por el día de la `fecha` y `503` con `Retry-After` si la verificación del intento que lo ocupa no es concluyente; `foto_ref` obligatoria en todo intento y en un cambio con `cobro_propuesto`; verificar también los cambios (sin verificar, el cobro propuesto no entra a la cola); verificar el intento que ocupa el cupo antes del `429`; `desconocido_en_iris` sin reintento; contacto con el cliente solo el registrado por una persona en Argos; guardar la clave con la que llegó cada intento y verificarlo completo con la ruta de Iris; un intento sin verificar no cambia la unidad, no avisa ni cuenta; aviso al titular solo por intentos verificados, uno por servicio al día; reintentar la verificación con `foto_pendiente`, `5xx`, timeouts y `401`/`403` (con aviso al admin) y verificar otra vez al abrir la declaración; intentos anteriores «anteriores a la verificación»; claves con el id de la operación y huella con `X-Iris-Actor`) (0.12.0, §3.10): `POST /servicios/:id/equipos/cambiar` en una transacción (`X-Iris-Actor` obligatoria y sin el asistente; servicio dentro de las empresas del token, con `origen_id` registrado y auditado; la que entra `disponible` y con la regla de A47; la que sale a la bodega de la que entra, sin baja inmediata; `destino_sale: funciona` no vale con `danado` o `garantia`; `cobro_propuesto` sin monto a la cola de «Equipo no devuelto»); estados de la unidad, nunca instalable fuera de `disponible`; `GET /inventario/unidades/:id` sin costos, proveedor ni RMA, con `servicio` solo del actual y el de la orden; `POST /servicios/:id/equipos/:unidad_id/no-recuperado` (motivo, foto por id opaco obligatoria si se negó o se perdió, ubicación, 1 por unidad al día, aviso al titular, mínimo configurable de intentos de técnicos distintos para declarar `no_recuperada`); ventana de `fecha` con `recibido_en_iris`; `…/perdida` rechazada al token de Iris y `inventario.declarar_perdida_comodato` retirado en la migración; eventos `inventario.equipo_cambiado` e `inventario.unidad_estado_cambiado`; permisos `servicios.cambiar_equipo` e `inventario.ver_unidad`. | acordado; pendiente en Argos |
| A49 | **Caídas repetidas con umbral de 3 en 60 min** (0.13.0, §3.11): emitir `servicio.caidas_repetidas` a conectores con contrato ≥ 0.13.0 con umbral de 3 caídas en 60 min, configurable por empresa (confirmado por Argos). | acordado; pendiente en Argos |
| A50 | **Falla general** (0.13.0, §3.11): webhooks `red.falla_general_abierta` / `red.falla_general_cerrada`, uno por empresa afectada, con ids de servicios afectados (hasta 5000) y sin datos personales; `GET /api/v1/incidentes-red/:incidente_id/servicios?empresa_id=&cursor=&limit=` (solo ids del alcance, `limit` ≤ 1000, `404` fuera del alcance, auditada con actor `token`, permiso `incidentes_red.leer`); registro de quién abrió a mano un incidente y por qué. | acordado; pendiente en Argos |
| A51 | **Gestiones de cobro** (0.13.0, §3.12): `POST /api/v1/clientes/:id/gestiones-cobro` con `tipo`, `resultado` de lista cerrada, `ocurrido_en` (ahora − 7 días a ahora + 5 min), `origen_tipo`, `origen_id` y `servicio_id?`; `X-Iris-Actor` obligatoria y `X-Iris-Contacto` si habló un contacto; `Idempotency-Key = iris:gestion:<gestion_id>` con `422 idempotency_key_reutilizada`; solo informativa; sin texto libre, montos ni teléfonos; permiso `clientes.registrar_gestion_cobro`. | acordado; pendiente en Argos |
| A52 | **Leer las plantillas de WhatsApp de Iris** (0.13.0, §6.7): usar `GET <iris>/api/v1/integraciones/argosisp/plantillas-whatsapp?empresa_id=` para mostrar qué avisos tienen plantilla aprobada, sin editarlas. | acordado; pendiente en Argos |
| A53 | **Tipo de servicio** (0.15.0 bloque A, §13.1; propuesta aceptada con ajustes): Argos agrega `tipo_servicio` (`internet` \| `cable` \| `iptv` \| `mixto`) y `componentes[]` a `GET /servicios/:id`, a la lista, a la primera sincronización (§2.5) y a cada relectura de `GET /servicios`; `router_id` puede ser `null` en `cable`; en planes de cable `bajada_kbps` y `subida_kbps` pueden ser null; `GET /planes` suma el filtro `tipo_servicio` (`GET /planes?tipo_servicio=cable` lista los planes de cable); con un servicio de cable, `reservar-ip`, `aprovisionar`, `GET /planes?router_id=` y `cambiar-plan` responden `422 tipo_sin_router` sin cambiar nada; `servicio.creado` y `servicio.config_cambiada` (`tipo_servicio` en `campos[]`) y `catalogo.cambiado` de plan (`tipo_servicio`, `componentes` en `campos[]`) llevan los datos a conectores con contrato ≥ 0.15.0, y si cambia el tipo de un plan o servicio cada servicio afectado emite `servicio.config_cambiada` con `tipo_servicio`. Los servicios existentes son `internet`. | aceptado por Iris con ajustes; pendiente de la PR espejo; pendiente en Argos (lotes T1 y T2) |
| A54 | **Corte manual de respaldo** (0.15.0 bloque B, §13.2; propuesta aceptada con ajustes): cuando el corte o la reconexión del CATV de un servicio de cable no se puede hacer por SmartOLT (sin ONU, instancia no operativa, reintentos agotados o «CATV not supported»), Argos envía `servicio.corte_manual_solicitado` y, si el servicio se reactiva antes, `servicio.corte_manual_cancelado` (con `accion`), cada uno con `version` y `solicitada_en`, solo a conectores con contrato ≥ 0.15.0 y la marca `corte_manual`. Argos guarda `solicitud_id` y el estado que Iris devuelva con `corte_manual.estado_cambiado` (receptor existente; solo aplica una `version` mayor y los estados terminales no retroceden); sin respuesta `en_curso` en 24 h lo muestra como `sin_respuesta`; no lleva cola propia. | aceptado por Iris con ajustes; pendiente de la PR espejo; pendiente en Argos (lote T3a) |
| A55 | **Recordatorios entregados por Iris** (0.15.0 bloque C, §13.3; propuesta aceptada con ajustes): Argos decide qué y cuándo (plantillas de recordatorio por empresa, asignadas por servicio o suscripción) y pide el aviso con `canal_preferido: \"auto\"` y `canales_permitidos[]` (la intersección con lo que Iris declara; vacía, no se usa `auto`); los códigos nuevos y la variable `concepto`; lee `capacidades.mensajeria[]` de Iris por empresa; los canales que Iris no declara los sigue enviando Argos; respeta las bajas de `correo` y `telegram` (`contacto.baja`) también cuando envía él; un código que Iris no declara en el canal cae al texto local de Argos; un `auto` `bloqueado` con `canal_no_disponible` se reenvía por los canales propios con otro `envio_id`. Sube `CONTRATO_IRIS` a 0.15.0 con el último bloque listo. | aceptado por Iris con ajustes; pendiente de la PR espejo; pendiente en Argos (lote R3) |
| A56 | **Cambiar el tipo de servicio** (0.15.1, §13.4): `cambiar-plan` en pendiente cruza tipos; `POST /servicios/:id/cambiar-tipo` para servicios activos, con red (reservar IP y aprovisionar, o liberar IP, CoA y `/ppp secret`), CATV por SmartOLT con respaldo en §13.2, prorrateo y `servicio.config_cambiada`. Permiso `servicios.cambiar_tipo` con migración de tokens. | propuesta de Iris; diseño de Argos; pendiente de la PR espejo; pendiente en Argos (lotes T1 a T3) |

### 5.2 ArgosISP solicita a Iris

| # | Pedido | Estado |
| --- | --- | --- |
| B1 | **Servicio de mensajería** (0.6.0, §6): API de avisos, plantillas, permisos de contacto y webhooks de entrega. Lo ofrece Iris aunque Argos no lo pidió; Argos lo adopta como canal WhatsApp (A21) y pide además las plantillas `factura_vencida` y `servicio_reactivado`. | en curso en Iris |
| B2 | **Autologin a una conversación** (0.6.0, §7.2): destino `conversacion` en `POST <iris>/api/v1/integraciones/argosisp/sso/tickets`, con el id de `X-Iris-Conversacion`, para el botón «Ver conversación en Iris» de la cola de caja. | pendiente en Iris (cuando exista la pantalla de conversaciones) |
| B3 | **Aprobar y rechazar desde la bandeja** (0.7.0, §7.7): emitir el ticket de autologin Iris → Argos (§1.3) con el botón «Vincular mi usuario de Argos»; los botones «Aprobar y registrar pago» y «Rechazar» en la boleta de la conversación, solo para una persona y nunca para el asistente de IA; y el manejo de los cinco `403` (`actor_sin_vinculo`, `actor_inactivo`, `actor_sin_permiso`, `actor_sin_empresa`, `actor_ia_no_permitido`). | listo en Iris |
| B4 | **Criterio de `sin_personal`** (0.8.0, §7.3): Iris la pide solo en horario hábil y sin nadie de caja disponible, apagada de fábrica, con verificación fuerte y solo con contrato ≥ 0.8.0; le dice al cliente hasta cuándo vale la reconexión con el `hasta` de Argos y **nunca dice «se corta a medianoche»**. Argos no objeta el criterio. | acordado con Argos y decidido por el PM (§10.2, puntos 1, 2 y 12); implementado en Iris (0.33.0, que informa contrato 0.8.1) |
| B5 | **Iris sin cuentas bancarias propias** (0.8.0, §7.8): instantánea de lectura, solo cuentas activas de la empresa de la conversación y migración de §10.3. Iris lee un **arreglo**, usa `alias` y `banco_nombre`, y maneja `rule: "inactiva"`. | implementado en Iris (0.33.0, que informa contrato 0.8.1) |
| B6 | **Permisos para los datos sensibles del servicio** (0.8.0, §2.4): `servicios.ver_red` y `servicios.ver_ubicacion` en Iris; el asistente de IA sin IP, `user_ppp`, coordenadas ni dirección; y la **lista blanca** de campos en la instantánea. Argos está de acuerdo. | acordado con Argos y decidido por el PM (§10.2, puntos 4 a 8); implementado en Iris (0.33.0, que informa contrato 0.8.1) |
| B7 | **Conexión de un paso, lado Iris** (0.8.0, §1.5 y §2.5): primera sincronización automática, con progreso y reanudable, al ritmo de §2.5; informar el avance con `PUT /integraciones/iris/sincronizacion`; pantalla de estado en el panel de Iris; y **registrar y avisar** (sin bloquear) si cambia el certificado TLS de la API de Argos. | implementado en Iris (0.33.0, que informa contrato 0.8.1) |
| B8 | **Rutas del portal del cliente** (0.9.0, §11.3–§11.6): tickets del cliente con la vista reducida del portal de Iris, adjuntos por `multipart/form-data`, permisos `portal_argos.*`, auditoría «en nombre del cliente», topes compartidos con `/soporte` y `capacidades.portal_argos`. | acordado; pendiente en Iris |
| B9 | **Webhooks de tickets y solicitudes a Argos** (0.9.0, §6.6): `ticket.estado_cambiado`, `ticket.respondido` y `solicitud_servicio.estado_cambiado`, solo a conectores con contrato ≥ 0.9.0. | acordado; pendiente en Iris |
| B10 | **Solicitudes de servicio como prospectos** (0.9.0, §11.7): `tipo`, cliente de Argos conocido, origen `portal_argos`, `estado_cliente`, una abierta por cliente, tipo y servicio, y «ganar» sin crear otro cliente en Argos. | acordado; pendiente en Iris |
| B11 | **Escribir lo que ya existe y corregir A7** (0.9.0, §11.9 y §1.2): `portal.emitir` y el portal `/soporte` de Iris; alias `cliente_id` en `GET /tickets`. | acordado; pendiente en Iris |
| B12 | **Activación desde la orden, lado Iris** (0.9.0, §12): FTTH y wireless; operación sin conexión `orden.activar_servicio` y activación por un agente desde el panel (también con la orden cerrada), estado en la orden, modo configurable por empresa o plantilla (al cerrar como hoy; exigir la activación lista salvo un supervisor con motivo; o cerrar con «activación pendiente por agente»), `orden.cerrada` con `aprovisionado` y sin repetir la activación ni el comodato de ese equipo, estado del equipo y cambio de Wi-Fi en la orden y en el ticket. 0.14.3: técnico en la activación con la orden cerrada, aprovisionamiento ajeno sin seguirlo y cierre «activado fuera de la orden» sin supervisor ni comodato doble; marcas de `capacidades.equipos`. | acordado; pendiente en Iris |
| B13 | **Origen `portal_argos` en tickets** y canal `portal_argos` en `ticket.mensaje_cliente` (0.9.0, §11.4). | acordado; pendiente en Iris |
| B14 | **Asistente de IA con los equipos** (0.9.0, §12.3 y §12.4): acciones apagadas de fábrica, permisos `servicios.ver_equipo` y `servicios.cambiar_wifi` asignables al asistente, verificación por código al teléfono o correo registrados en Argos o por el documento del titular (§7.5) antes de cambiar el Wi-Fi (para un solo cambio, caduca al cambiar el cliente asociado), el asistente **nunca pide ni recibe la clave**: Iris manda un enlace de un solo uso a un formulario propio (§12.4); una clave escrita en el chat se reemplaza por `***` antes del modelo y no se usa; el modelo no recibe serie, MAC, firmware ni modelo del equipo. 0.14.3: respeta las marcas `estado_equipo` y `wifi`. | acordado; pendiente en Iris |
| B15 | **Cobro de la instalación desde la orden** (0.9.0, §3): lo registra un agente o sale de un formulario de la orden (concepto y `precio_unitario` > 0 de la plantilla o del formulario; cambiar el precio exige `ordenes.cobro_editar_monto`, con motivo auditado; nunca Q0), sale en el momento por `integracion.pendientes` con `lineas[{ concepto_id, precio_unitario }]`, `X-Iris-Actor` e `Idempotency-Key = iris:cargo:<orden_id>:<cargo_id>`, no repite la instalación al cerrar y la orden sigue la factura (emitida, pagada, anulada). 0.14.3: respeta la marca `cobro_equipo`; sin ella no se ofrece el cobro de la instalación ni el cierre manda la línea de instalación; el cargo con `422 capacidad_no_disponible` queda terminal y las demás líneas salen como cobro nuevo, salvo «cobro por confirmar» tras un envío sin respuesta (§3), que concilia un supervisor con el permiso de Iris `integraciones.confirmar_cobro`, validando la factura con `GET /facturas/origen-iris`. | acordado; pendiente en Iris |
| B16 | **Bienvenida en la orden** (0.9.0, §12.5 y §6.3): plantilla `bienvenida_servicio` con las cuentas que Iris inserta de la instantánea de §7.8 (nunca de Argos ni de texto libre), `origen` opcional en §6.2, estado de la bienvenida en la orden, «Reenviar bienvenida», bandeja «Bienvenidas sin enviar» y el ajuste `cierre_exige_bienvenida` (apagado de fábrica). 0.14.3: relación sin `origen` (una sola orden, 7 días antes de recibir el aviso, titular actual), mostrada como «relacionada por servicio», sin contar para `cierre_exige_bienvenida` ni habilitar Reenviar; con `bienvenida: false`, sin Reenviar ni `cierre_exige_bienvenida`. | acordado; pendiente en Iris |
| B17 | **Alta pendiente hasta activar** (0.10.0, §3.4.8): prospecto «ganado, alta pendiente» (`alta.estado = esperando_activacion`) ligado al cliente y al servicio de Argos hasta la activación; pasa al cliente nuevo con `servicio.titular_cambiado`. | pendiente en Iris |
| B18 | **Paso «Confirmar titular y plan» en la orden** (0.10.0, §3.4.2 y §3.4.8): antes de «Activar»; «¿Quién firma?», datos del titular, lugar del servicio, plan y validación de identidad; operaciones sin conexión `orden.registrar_identidad`, `orden.cambiar_titular`, `orden.completar_datos` y `orden.cambiar_plan` (reglas en `contratos/sync.md`); pendientes en orden con dependencias y activación bloqueada sin identidad registrada o si Argos rechaza; permisos `ordenes.cambiar_titular`, `ordenes.cambiar_plan`, `ordenes.completar_datos` (técnico solo en sus órdenes en curso) y `ordenes.validar_identidad` (agente, para activar sin la identidad registrada, con motivo). | pendiente en Iris |
| B19 | **Foto del documento solo en tránsito** (0.10.0, §3.4.8): en el teléfono, solo en la cola local hasta que el servidor de Iris la confirma; en el servidor, en un depósito de tránsito cifrado, fuera de los adjuntos y de la auditoría (`***`), enviada por `integracion.pendientes` (`iris:identidad:<orden_id>:<version>`) y borrada al `201`, o a los 7 días si se agotan los reintentos (la orden muestra el error). El número del documento nunca en eventos ni logs. La retención y quién la ve son de Argos (A41). | pendiente en Iris |
| B20 | **Cotización y contrato al cambiar** (0.10.0, §3.4.8): revisión nueva de la cotización con el plan nuevo y aviso al vendedor; contrato regenerado a nombre del nuevo titular y firmado en la orden. | pendiente en Iris |
| B21 | **Planes permitidos sin señal y cobro después del titular** (0.10.0, §3.4.8): planes de `GET /planes?router_id=&modo=` en el paquete del día; el cobro de la instalación sale después de confirmar el titular. | pendiente en Iris |
| B22 | **Contactos en la atención** (0.10.0, §3.5): al llegar un mensaje o una llamada, buscar el número y asociar la conversación al cliente con el rol del contacto; con varios clientes, preguntar sin mostrar datos de ninguno; el asistente y los agentes ofrecen solo lo que permiten los `permisos` y piden la validación del titular para lo reservado; un `contacto_iris` unido a un cliente se crea en Argos; caché refrescada por `cliente.contactos_cambiados`; consentimiento por número. | pendiente en Iris |
| B23 | **Prospectos desde Argos** (0.10.0, §3.6): `POST /api/v1/integraciones/argosisp/prospectos` con el permiso `prospectos.registrar_integracion` (solo registrar), origen `argos_panel`, vendedor por zona y factibilidad como §11.7, `telefonos[]` guardados en una tabla aparte con índice por número y empresa, `409 prospecto_duplicado { url_iris }` (con el pedido sumado como actividad) si cualquiera de los números está en un prospecto abierto, límites por actor y empresa, el chat ubica al prospecto por cualquiera de sus números (como a los clientes), al ganar el principal como teléfono del cliente nuevo, los números con nombre como contactos y los demás en `telefonos_adicionales[]` (con `cliente_id` existente, sin tocar el cliente), un número no principal sin acceso a cotización, dirección ni plan hasta un código al principal, y el webhook `prospecto.estado_cambiado` sin datos personales. | pendiente en Iris |
| B24 | **Código y nombre del servicio en Iris** (0.10.0, §3.7): guardar `codigo` y `nombre` en la instantánea del servicio y mostrar «S-000412 · Casa» donde hoy se muestra el servicio; usar el `codigo` como referencia en la desambiguación del chat; con un contacto de varios servicios, preguntar mostrando solo los suyos; `consultar` y `pagar` solo sobre sus `servicios[]`, con `servicio_id` en promesas y comprobantes. | pendiente en Iris |
| B25 | **Servicios a cargo en la atención** (0.10.1, §3.8): el chat y el asistente distinguen «sus servicios» de «los que tiene a cargo» (solo `codigo` y `nombre`); la verificación de la persona vale solo para los permisos del contacto y nunca para lo reservado al titular; `X-Iris-Contacto` en las acciones sobre el servicio a cargo; el panel lo muestra en la ficha de la conversación. | pendiente en Iris |
| B26 | **Bodega de cuadrilla en Iris** (0.14.0: consumos de la app con `tecnico` + `bodega_id` + `X-Iris-Actor` del usuario autenticado del lote; consumo por actor y bodega, con `tecnico` por línea; webhooks `tecnico.argos_*` a Argos para todo técnico, con `empresa_id` y `version`, solo al conector del `id_externo`, carga inicial al informar 0.14.0 cerrada con `vinculos_cargados`, sin operaciones con `tecnico` antes del `2xx` de su `vinculado` (`depende_de`), `vinculado` del técnico nuevo solo tras el `2xx` del `desvinculado` del anterior, `motivo: desactivado`, y `GET /integraciones/argosisp/tecnicos/:usuario_id` (solo vínculos de ese conector; si no, `404`); `403` de negocio reconocidos por la URN completa y sin reintento) (0.11.0, §3.9): `jefe_usuario_id` con `POST /cuadrillas/:id/asignar-jefe` (`cuadrillas.asignar_jefe`, solo admin y supervisor, `403 autoasignacion`, `422 jefe_no_miembro`); webhooks `cuadrilla.*` a Argos con `empresa_id`, `version`, `miembros[]` completo y `jefe_usuario_id`, también al cambiar `tecnico.cuadrilla_id`, `tecnico.activo`, el vínculo I1 o al desactivar el usuario; `GET /api/v1/integraciones/argosisp/cuadrillas/:id` para conciliar (permiso `cuadrillas.leer_integracion`, `404` fuera del alcance); un evento `cuadrilla.*` por empresa; corte inmediato de lotes de un miembro quitado; `bodega_id` validado en el servidor; en la app, «Mi bodega» con «Cuadrilla» y «Mía», material por defecto de la cuadrilla, `bodega_id` guardado en la cola, `409 sin_existencia` y `403 no_es_miembro` como conflicto sin cambiar de bodega, y «Recibir traslado» solo para el jefe, con `403` en el servidor a otros. | pendiente en Iris |
| B27 | **Equipos del cliente en Iris** (0.14.0: `equipo.cambiar` y `equipo.no_recuperado` como tipos del lote, enviados al aplicarse; actor = usuario autenticado del lote y bloqueo al cambiar la sesión; el técnico que dejó la orden, hasta 12 h; validación de ubicación y foto con `ubicacion_sin_referencia` y `tomadoEn` obligatorio; intento enviado solo con la foto `completo` y conflicto `foto_fuera_de_ventana` pasadas 72 h; `GET /integraciones/argosisp/intentos-recuperacion/verificacion` (`adjuntos.verificar_integracion`, agregado a los tokens vigentes) para intentos y cambios, con `envio_en_curso`, `no_coincide` como alerta de seguridad y tope de 60 por minuto y 20 por clave al día; «posible intento ajeno»; `intento_repetido` con Argos < 0.14.0; `usuario_id` por línea en `orden.cerrada`; claves con el id de la operación; `429 demasiados_intentos` definitivo) (0.12.0, §3.10): orden `cambio_equipo` con escaneo de la unidad que sale y la que entra, motivo, destino (por defecto «a revisión»), foto y cobro propuesto opcionales (desde 0.14.0, foto obligatoria con cobro propuesto), encolada sin señal; orden `retiro` con «Recuperado» (funciona o a revisión) y «No se pudo recuperar» (motivo, foto obligatoria siempre desde 0.14.0, ubicación en sitio, reprogramar otro intento); `recibido_en_iris` en cada lote y `fecha` no anterior a la asignación; servicio de la orden y orden asignada al actor validados en el servidor; lectura de unidad limitada a `inventario.ver_unidad` y auditada; con Argos < 0.12.0, el no recuperado queda para una persona con `ordenes.declarar_perdida`; estado e historial de cada equipo, sin costos; `orden.cerrada` con `equipos[].accion` `cambio` y `no_recuperado`; conflictos sin reintento automático. | pendiente en Iris |
| B28 | **Tickets del portal por contacto** (0.12.1, §11.10): `X-Iris-Contacto` (`<contacto_id>` o `titular`) en toda ruta de §11, `400` si falta; caché de contactos con relectura por `X-Iris-Contacto-Version` y `503 argos_no_disponible` si Argos no responde; `servicio_id` obligatorio (`422 validacion`) y `403 servicio_fuera_del_contacto`; sus propios tickets completos y los demás del servicio solo con número, estado, categoría, incidente y fechas; crear, responder y confirmar con `reportar` y solo los suyos (`403 permiso_de_contacto`); adjuntos solo en lo que ve completo; `403 solo_titular` en solicitudes; topes propios; `creado_por_contacto` y «Abierto por <nombre> (a cargo)»; al quitar el contacto deja de verlos. | pendiente en Iris |
| B29 | **Caídas repetidas en Iris** (0.13.0, §3.11): umbral por empresa (3 en 60 min de fábrica); un ticket por servicio mientras siga abierto el de esa categoría, o una nota en el abierto; nota en el ticket general si el servicio está en una falla general; con más de N en 10 min (20 de fábrica), ticket de «posible falla general» y aviso a supervisión. | pendiente en Iris |
| B30 | **Falla general en Iris** (0.13.0, §3.11): un ticket general idempotente por `incidente_id` (reusado si el mismo `alcance_id` reabre antes de 30 min); absorbe solo tickets de conectividad de servicios afectados creados después de `desde`; ids desconocidos leídos con `GET /servicios/:id` y descartados si son de otra empresa; aviso de falla solo a números con cliente y sin servicio ni dirección; vínculo con el maestro de Horus del mismo router; al cerrarse, hijos a «verificar con el cliente» (o resueltos si Horus o PPP confirman), un solo aviso de cierre por cliente e incidente, máximo 1 cada 6 h, con §6.5. | pendiente en Iris |
| B31 | **Gestiones de cobro desde Iris** (0.13.0, §3.12): `gestion_id` UUID v7 como clave; solo con cliente confirmado; `X-Iris-Actor` y `X-Iris-Contacto` si habló un contacto; con el asistente, `pago_informado` y `promete_pagar` solo con promesa o comprobante real; sin texto libre, montos ni teléfonos. | pendiente en Iris |
| B32 | **Plantillas de WhatsApp para Argos** (0.13.0, §6.7): `GET /api/v1/integraciones/argosisp/plantillas-whatsapp?empresa_id=` con `plantillas_whatsapp.leer_integracion`, solo las que esa empresa puede usar (no las de un canal prestado no autorizadas), sin id de WABA, id de Meta ni motivo de rechazo completo. | pendiente en Iris |
| B33 | **Tipo de servicio en Iris** (0.15.0 bloque A, §13.1; propuesta aceptada con ajustes): guardar `tipo_servicio` y `componentes[]` en la instantánea del servicio (lista blanca); mostrar el tipo donde se muestra el plan; tiene router y velocidad el servicio cuyos `componentes` incluyen `internet` o `iptv` (un tipo desconocido se trata como internet, salvo con `router_id` null: sin router); un servicio de cable no tiene router ni velocidad (factibilidad `indeterminado` con motivo `tipo_sin_router`, sin «velocidad contratada»); tolerar `router_id` null; informar la marca `tipo_servicio` en `capacidades`. | aceptado con ajustes; pendiente en Iris (depende de `GET …/capacidades`, B16) |
| B34 | **Corte manual en Iris** (0.15.0 bloque B, §13.2; propuesta aceptada con ajustes): recibir `servicio.corte_manual_solicitado` y abrir una orden de trabajo nueva de tipo `corte_catv` o `reconexion_catv` (idempotente por `solicitud_id`; `referencia` y `referencia_tipo` en lugar de `ticket_numero`); con `servicio.corte_manual_cancelado`, cancelarla según §13.2; emitir `corte_manual.estado_cambiado` (`en_curso`, `resuelta`, `cancelada`, `no_realizada` con `motivo_codigo`; `version` creciente) a Argos; informar la marca `corte_manual`. | aceptado con ajustes; pendiente en Iris (depende de `GET …/capacidades`, B16) |
| B35 | **Entrega automática por canal** (0.15.0 bloque C, §13.3; propuesta aceptada con ajustes): aceptar `canal_preferido: \"auto\"` y `canales_permitidos[]`, elegir un solo canal por aviso, en el orden de la empresa, entre los que Iris implementa y tiene configurados (hoy WhatsApp); los códigos nuevos por canal y su aprobación; `concepto` (máx. 60), `suscripcion_id` y `recordatorio` opcionales; `aviso.entregado` con el canal real; `contacto.baja` de `correo` y `telegram`; `capacidades.mensajeria[]` por empresa. Las reglas de §6.5 no cambian y aplican a todos los canales. | aceptado con ajustes; pendiente en Iris (depende de `GET …/capacidades`, B16) |
| B36 | **Orden de cambio de tipo en Iris** (0.15.1, §13.4): orden `cambio_tipo` desde la ficha del servicio, con el plan destino y su precio; la visita configura el equipo y elige el router cuando se agrega internet; al cerrarla, `cambiar-tipo` (o `cambiar-plan` si el servicio está pendiente); mostrar el error y no reintentar los `422`. | propuesta; pendiente en Iris (depende de A56) |

## 6. Servicio de mensajería de Iris (0.6.0, fase 1)

Iris corre la mensajería como un **servicio aparte** (`apps/mensajeria`, su propio proceso y su propia base `mensajeria`), con API propia bajo `https://<iris>/api/v1/mensajeria`. Lo usan el panel de Iris, sus asistentes de IA y Argos. Argos entra con el token de §1.2 y el permiso `mensajeria.enviar`.

**Cómo lo usa Argos (A21).** Argos lo adopta como su canal **WhatsApp**: su módulo de notificaciones (`notify`) suma un canal `whatsapp` que, en vez de hablar con Meta, llama a §6.2. Correo y Telegram siguen saliendo de Argos como hoy. Argos decide **cuándo** avisar (sus eventos y recordatorios) e Iris decide **cómo y si** sale por WhatsApp (plantilla aprobada, permiso, límites, horario). Si el conector Iris está apagado o no habla 0.6.0, el canal `whatsapp` de Argos no existe y nada más cambia.

### 6.1 Canales

| Canal | Cómo se conecta en Iris | Para qué se usa |
| --- | --- | --- |
| WhatsApp, API oficial | Meta Cloud API, número de la empresa | Conversar dentro de la ventana de 24 h y enviar avisos con plantillas aprobadas |
| WhatsApp por QR | Sesión de un teléfono de la empresa (código abierto) | Solo responder a quien escribió. Nunca avisos masivos ni a contactos nuevos |
| Telegram | Bot de la empresa | Solo a clientes que ya iniciaron el chat |
| Correo | Buzón de Iris | Avisos y respuestas |

El admin de Iris elige qué método usa cada cosa (respuestas, avisos automáticos, avisos de Argos) y cuál es el respaldo. Los avisos de Argos nunca salen por WhatsApp QR. Argos solo pide avisos con `canal_preferido: "whatsapp"`: el correo y el Telegram de sus avisos los manda él mismo, así un aviso de Argos no sale dos veces por el mismo medio.

**0.15.0 (propuesta aceptada con ajustes, bloque C, §13.3):** con `canal_preferido: "auto"` Iris elige **un solo canal por aviso** entre los que implementa y tiene configurados para esa empresa (`capacidades.mensajeria[].canales`; hoy WhatsApp), en el orden de la empresa, y Argos **no** manda ese aviso por su propio correo ni Telegram. Los canales que Iris no declara los sigue enviando Argos. La frase anterior («Argos solo pide avisos con `canal_preferido: "whatsapp"`») sigue valiendo para los avisos que no llevan `auto` y para todo Iris que no informe la marca de entrega automática.

### 6.2 Enviar un aviso

`POST /api/v1/mensajeria/avisos` con `Idempotency-Key = argos:aviso:<plantilla>:<envio_id>`, donde `envio_id` es el id del envío en Argos (`notify.envios.id`, único por evento, cliente y canal). Por ejemplo `argos:aviso:factura_por_vencer:88123`.

```json
{
  "empresa_id": 3,
  "cliente_id": 1201,
  "servicio_id": 4410,
  "plantilla": "factura_por_vencer",
  "variables": { "monto": "250.00", "moneda": "GTQ", "vence_en": "2026-11-10" },
  "canal_preferido": "whatsapp",
  "prioridad": "normal"
}
```

- `cliente_id` y `servicio_id` son ids de Argos. Iris toma el teléfono y el correo de su instantánea (§2.2) y los refresca si tienen más de 24 h. **Argos nunca manda el número en el cuerpo:** así ningún aviso puede salir a un contacto que no es del cliente.
- `plantilla` es uno de los códigos de §6.3. `variables` solo acepta las claves declaradas en esa plantilla (las demás dan `422 variable_desconocida`). Los montos van como string decimal con dos decimales; las fechas, `YYYY-MM-DD` en hora de Guatemala.
- Respuesta `202 { aviso_id, estado, motivo? }`, con `estado` = `en_cola` | `bloqueado`. Un aviso `bloqueado` no se reintenta y lleva `motivo`:
    - `sin_permiso`: el cliente no aceptó avisos por ningún canal posible;
    - `baja`: el cliente pidió no recibir avisos;
    - `limite_diario`: superó los avisos automáticos del día;
    - `sin_contacto`: no tiene un teléfono ni un correo válido;
    - `canal_no_disponible`: ningún canal configurado puede enviarlo.
- En Argos, `en_cola` deja el envío `encolado` hasta el webhook de §6.6, y `bloqueado` lo deja `omitido` con el motivo (como un canal apagado: sin reintento ni respaldo). Única excepción (0.15.0, C12): un aviso `auto` `bloqueado` con `canal_no_disponible` (§13.3). Un `5xx` o un timeout se reintentan con la misma clave, con las reglas de reintento de `notify`.
- En horario de silencio el aviso queda `en_cola` y sale al terminar. Si al terminar el silencio ya no tiene sentido, se descarta con `aviso.fallido` y `motivo = vencido`. **Desde 0.15.0** cada código vence así (C9): `corte_proximo`, en `fecha_corte`; `promesa_por_vencer`, en `vence_en`; `debito_fallido`, en `reintenta_el`, o a las 72 h si no viene; `factura_por_vencer`, en `vence_en` (como desde la 0.6.0); `factura_vencida` y los demás, a las 72 h de pedido. El silencio y el tope diario aplican a todos los canales.
- `prioridad: "urgente"` (solo `corte_programado`) salta el límite diario, pero nunca el horario de silencio ni la falta de permiso. Argos no la usa en la fase 1.
- **`origen` (0.9.0, opcional):** `{ "tipo": "iris_orden", "id": "<orden_id>" }`, solo en `bienvenida_servicio`, cuando la activación vino de una orden de Iris (§12.5); sin `origen` (o `null`) si la activación fue desde el panel de Argos. Otro tipo, o una orden que no es de ese servicio: `422 origen_invalido`. No cambia la `Idempotency-Key`.
- **0.15.0 (propuesta aceptada con ajustes, §13.3):** `canal_preferido` admite `"auto"`; `canales_permitidos` (opcional, arreglo de canales sin repetir, de los que Iris declara para la empresa) solo con `auto`: enviado sin `auto`, vacío, con valores repetidos o con un canal que Iris no declara, `422 canal_no_permitido`; solo la ausencia de la lista significa «todos los declarados». `suscripcion_id` (opcional, id de Argos) y `recordatorio: { disparador, orden }` (opcional, `disparador` = `vencimiento` \| `corte_proximo` \| `promesa_por_vencer`, solo informativo para la bandeja de Iris). `variables` con `concepto` que no cumple §6.3: `422 variable_invalida`. La `Idempotency-Key` no cambia, y **un mismo `envio_id` nunca se reenvía con otro `canal_preferido`**: si cambia el canal, el `envio_id` es nuevo (C14).

### 6.3 Plantillas

Iris mantiene el texto, las traducciones y la aprobación de Meta. Argos solo usa el código. En Argos, cada plantilla de `notify` del canal `whatsapp` guarda el código de Iris en lugar del texto.

| Código | Variables | Lo pide | Cuándo, en Argos |
| --- | --- | --- | --- |
| `factura_por_vencer` | `monto`, `moneda`, `vence_en` | Argos | Recordatorio antes del vencimiento (`recordatorio.*` de `notify`) |
| `factura_vencida` | `monto`, `moneda`, `vence_en` | Argos (nueva, 0.6.0) | `factura.vencida` |
| `pago_recibido` | `monto`, `moneda`, `fecha` | Argos | `pago.registrado`, salvo el que nace de un comprobante de Iris (§7.2) |
| `corte_por_falta_de_pago` | `monto`, `moneda` | Argos | `servicio.estado_cambiado` a `suspendido` o `limitado` por facturas vencidas |
| `servicio_reactivado` | (ninguna) | Argos (nueva, 0.6.0) | `servicio.estado_cambiado` a `activo` después de un corte |
| `bienvenida` | `plan` | Argos | `servicio.instalacion_activada` |
| `corte_programado` | `inicio`, `fin`, `zona` | Iris u Horus | Argos no tiene ventanas de mantenimiento: no la usa en la fase 1 |
| `boleta_recibida`, `boleta_confirmada`, `boleta_rechazada`, `boleta_falta_dato` | `monto`, `moneda`, `motivo` | Iris, con la respuesta de §7.2 y los webhooks `comprobante.aprobado` / `comprobante.rechazado` | Argos **no** avisa por su cuenta de un comprobante con `canal = iris` (§7.2) |
| `reconexion_temporal` | `hasta` | Iris, al recibir el 201 de §7.3. Desde 0.8.0, Iris escribe en `hasta` hasta cuándo vale la reconexión, en hora de Guatemala y tomado del `hasta` de Argos (§7.3, «Qué le dice el asistente al cliente») | Argos no avisa por su cuenta (§7.3) |
| `promesa_aceptada` | `vence_en`, `monto` | Iris, al recibir el 201 de §7.4 con `estado = activa` | Argos no avisa por su cuenta de una promesa creada por Iris (§7.4) |
| `bienvenida_servicio` (0.9.0) | `nombre`, `plan`, `precio_mensual`, `moneda`, `codigo_cliente`; `cuentas` la inserta Iris (§7.8), nunca Argos. Iris valida las variables (§12.5) | Argos | `servicio.instalacion_activada`, con Iris en contrato ≥ 0.9.0 (con uno anterior, o desde 0.14.3 con `capacidades.equipos.bienvenida: false` para la empresa, `bienvenida`; Iris acepta cualquiera de las dos y no rechaza por la marca). Detalle en §12.5 |
| `corte_proximo` (0.15.0, propuesta aceptada con ajustes) | `monto`, `moneda`, `fecha_corte` | Argos | Paso de un recordatorio con disparador `corte_proximo` |
| `promesa_por_vencer` (0.15.0, propuesta aceptada con ajustes) | `monto`, `moneda`, `vence_en` | Argos | Paso con disparador `promesa_por_vencer` |
| `debito_fallido` (0.15.0, propuesta aceptada con ajustes) | `monto`, `moneda`, `reintenta_el` (opcional: se omite cuando no hay reintento; nunca se manda vacío, Meta rechaza parámetros vacíos) | Argos | `debito.fallido` (cobro automático rechazado) |
| `debito_desactivado` (0.15.0, propuesta aceptada con ajustes) | (ninguna) | Argos | `debito.desactivado` por rechazos, contracargo o tarjeta vencida |
| `suscripcion_suspendida` / `suscripcion_reactivada` (0.15.0, propuesta aceptada con ajustes) | `concepto` | Argos | `suscripcion.estado_cambiada` |

En `boleta_rechazada` y `boleta_falta_dato`, `motivo` es un texto fijo de Iris según la `causa` del rechazo. Iris solo le reenvía al cliente el motivo libre (el de `POST /comprobantes/:id/rechazar` o el `motivo_rechazo` que escribe caja) si, tras normalizarlo (Unicode NFKC y sin caracteres de formato), contiene únicamente letras del español, espacios y los signos `,;:¿?¡!()`. Cualquier dígito o punto lo deja fuera (salvo el punto final de la frase) y el cliente recibe solo el texto fijo de la causa. El motivo completo se guarda y se envía a Argos sin cambios.

**0.15.0 (propuesta aceptada con ajustes):** `factura_por_vencer` y `factura_vencida` suman la variable **opcional** `concepto` (nombre del producto de una suscripción, de la lista cerrada del catálogo de Argos; nunca texto libre de una persona; ausente en las facturas de servicio). `concepto` tiene **máximo 60 caracteres, sin saltos de línea ni URL**; si no cumple, `422 variable_invalida`. Nunca lleva datos de tarjeta ni del débito. Un código que Iris no declara en `capacidades.mensajeria[].canales[].codigos` del canal no se pide: Argos usa su texto local. **WhatsApp** sale del número de avisos de la empresa (o del número prestado; referencia informativa: contrato propio de Iris `.claude/integration/contratos/conversaciones.md` §15.3 «Prestar un número a otras empresas», que no se espeja), con plantillas de la cuenta de WhatsApp Business dueña del número (con préstamo, la de la empresa que presta); Meta le cobra a esa cuenta, Iris no factura mensajes y el reparto entre empresas queda fuera del contrato. Los códigos nuevos van como `utility`; si Meta pasa uno a `marketing` y la empresa no lo permite, ese código sale de `codigos` y Argos usa su texto local.

`GET /api/v1/mensajeria/plantillas` lista las vigentes, con sus variables y en qué canales están aprobadas. Una plantilla que Argos pide y no está aprobada responde `bloqueado` con `canal_no_disponible`.

### 6.4 Permisos de contacto

Iris guarda el registro: cliente, canal, estado (`acepta` | `baja`), fecha, origen y quién lo registró. Argos **no** lleva un registro propio: el de Iris es la fuente.

- `POST /api/v1/mensajeria/consentimientos` `{ empresa_id, cliente_id, canal: "whatsapp", estado: "acepta" | "baja", origen: "argos_panel" | "argos_portal", ocurrido_en }` (`empresa_id` obligatorio), con `Idempotency-Key = argos:consentimiento:<empresa_id>:<cliente_id>:<canal>:<ocurrido_en>`: el mismo cliente en dos empresas son dos permisos. Argos lo manda cuando un operador marca en el panel «Acepta avisos por WhatsApp» al crear o editar un cliente (`origen = argos_panel`), o cuando el cliente lo marca en el portal (`origen = argos_portal`). Si el conector está apagado, queda en la cola de envíos a Iris y sale al activarse.
- Cualquier `2xx` significa que quedó en el historial de Iris: `201` si es nuevo y `200` si es un reintento con la misma `Idempotency-Key`. Los dos responden `{ consentimiento_id, estado_vigente }`.
- `estado_vigente` puede diferir de lo que se mandó: gana el más reciente por `ocurrido_en` (por ejemplo, un `acepta` anterior a una baja dada en el chat queda en el historial, pero sigue vigente la `baja`). Argos lo guarda y lo adopta como último estado de contacto del cliente.
- `422` por validación (canal u origen desconocido, cliente fuera de la empresa del conector) no se reintenta. Un `5xx` o un timeout se reintentan con la misma clave.
- Una baja que da el cliente en el chat (palabras `BAJA` o `ALTO`) manda sobre cualquier alta anterior y llega a Argos por el webhook `contacto.baja`.
- Una baja corta los avisos, pero no las respuestas: el cliente puede seguir escribiendo y se le contesta.
- `contacto.baja` en Argos queda en la línea de tiempo del cliente (auditoría) y se muestra en su ficha. Solo afecta al canal que nombra. Desde 0.15.0 el canal puede ser `correo` o `telegram` y Argos la respeta también cuando envía él (abajo).

- **0.10.0 (A42, §3.5):** el permiso de contacto es también **por número**: `POST /api/v1/mensajeria/consentimientos` acepta `telefono` opcional (E.164) para el número de un contacto. Los avisos de cobro a los contactos con `avisos` respetan el de cada número.
- **0.12.1:** con `telefono`, la `Idempotency-Key` es `argos:consentimiento:<empresa_id>:<cliente_id>:whatsapp:<h>:<ocurrido_en>`, donde `<h>` son los **16 primeros caracteres hex del HMAC-SHA256 de la cadena `"consentimiento:" + número en E.164`** (el prefijo separa este uso del secreto), con el secreto del receptor de webhooks de Iris de ese conector como clave (§1.3, §1.5; lo conocen los dos lados). El número en claro va solo en el cuerpo, nunca en la clave. La `Idempotency-Key` cuenta como dato personal: no va en logs ni en eventos. Si el secreto de webhooks rota, una clave ya enviada se reintenta con los bytes guardados y **no se vuelve a calcular**. Sin `telefono`, sigue la clave anterior.
- **0.15.0 (propuesta aceptada con ajustes, C3; decisión del líder):** los avisos de cobro por **correo** y por **Telegram** son transaccionales: no piden alta, pero respetan la baja. `contacto.baja` admite `canal` `correo` (enlace de baja y cabecera `List-Unsubscribe`) y `telegram` (`/stop`), y Argos respeta esas bajas también cuando envía él. Un correo o un chat dado de baja sigue recibiendo las respuestas a lo que el cliente escriba, igual que en WhatsApp. `POST /mensajeria/consentimientos` sigue siendo solo de `whatsapp`. Hoy Iris no declara `telegram` (C4, §13.3); el valor queda admitido en el webhook.

### 6.5 Reglas contra spam (las aplica Iris, nunca Argos)

- Solo a clientes con permiso registrado.
- Máximo 3 avisos automáticos por cliente y por día (configurable en Iris).
- Horario de silencio de 21:00 a 07:00, hora de Guatemala.
- Los envíos masivos van espaciados: 20 por minuto por número, con pausa automática si suben los reportes de spam o baja la calidad del número.
- Ventana de 24 h de WhatsApp: fuera de ella, solo plantillas aprobadas.
- Un mismo hecho se avisa una sola vez por WhatsApp: lo que Iris ya contestó en la conversación (boletas, reconexión, promesas) Argos no lo repite (§6.3).
- Desde 0.15.0 el horario de silencio y el tope diario aplican a todos los canales, no solo a WhatsApp.

### 6.6 Webhooks Iris → Argos

A `POST https://<argos>/api/v1/integraciones/iris/webhook` (el receptor que Argos entrega en el canje de §1.3). Solo a conectores con contrato ≥ 0.6.0 y con el receptor guardado; un conector canjeado antes de B1 se vuelve a conectar («Versión y compatibilidad», `receptor_argos`).

- **Cuerpo:** `{ id, evento, ocurrido_en, payload }`.
- **Cabeceras:** `X-Webhook-Id` (= `id`), `X-Webhook-Timestamp` y `X-Webhook-Signature: v1=<hmac-sha256>`, firmadas con el secreto de entrada del canje de §1.3. Los reintentos son los de §1.3. Si `X-Webhook-Id` y `id` no coinciden, Argos responde `400`.
- Argos deduplica por `X-Webhook-Id`.

| Evento | Payload | Efecto en Argos |
| --- | --- | --- |
| `aviso.entregado` | aviso_id, idempotency_key, empresa_id, canal (`whatsapp`; desde 0.15.0 con `auto`, el canal real que Iris declaró: `whatsapp` o `correo`, y `telegram` cuando se acuerde), entregado_en (en correo, la aceptación del SMTP de destino), leido_en (o null; en correo, siempre null) | El envío pasa a `enviado` y emite `notificacion.enviada` |
| `aviso.fallido` | aviso_id, idempotency_key, empresa_id, canal (o null), motivo (`vencido` \| `numero_invalido` \| `destino_invalido` \| `rechazado_por_canal` \| `agotado` \| `baja`; `destino_invalido` es el del correo, desde 0.15.0; `baja`, desde 0.15.1: el cliente dio de baja ese canal después de que Iris encoló el aviso), detalle (o null; texto libre, Argos no lo interpreta) | El envío pasa a `fallido` con el motivo y emite `notificacion.enviada`. Con `auto` es el **único** webhook final, con el canal real: un fallo después del primer envío es final (§13.3) |
| `aviso.bloqueado` (0.15.1) | aviso_id, idempotency_key, empresa_id, cliente_id, canal (o null), origen, motivo (`canal_no_disponible`), detalle (o null) | Solo para avisos `auto` que nunca salieron y estaban `encolado`, una vez por aviso: el canal dejó de servir antes del primer envío (por ejemplo, una baja o el número de avisos desconectado durante el silencio). Argos busca el envío por `idempotency_key`, relee `capacidades` y lo reenvía por sus canales con un `envio_id` nuevo, igual que con una respuesta `bloqueado` con `canal_no_disponible` (C12). Un aviso que emite `aviso.bloqueado` ya no emite `aviso.entregado` ni `aviso.fallido`. Repetido, de un aviso que Argos ya cerró o con `origen` distinto de `argos`: Argos responde `200` y lo descarta |
| `contacto.baja` | cliente_id, empresa_id, canal (`whatsapp`; desde 0.15.0 también `correo` y `telegram`), ocurrido_en, origen (`cliente` \| `agente`) | Ver §6.4 |
| `tecnico.argos_vinculado` (0.14.0) | tecnico_id, usuario_id, empresa_id, version, id_externo, anterior_id_externo (o null) | Guarda el vínculo del técnico con su usuario de Argos, tenga cuadrilla o no (§3.9), o lo marca `vinculo_rechazado` |
| `tecnico.argos_desvinculado` (0.14.0) | tecnico_id, usuario_id, empresa_id, version, id_externo, motivo (o null; `desactivado` cuando se desactiva el técnico o su usuario) | Quita el vínculo: desde entonces, ese actor responde `403 actor_desconocido` (§3.9) |
| `corte_manual.estado_cambiado` (0.15.0, propuesta aceptada con ajustes, bloque B) | solicitud_id, empresa_id, version, estado (`en_curso` \| `resuelta` \| `cancelada` \| `no_realizada`), motivo_codigo (solo con `no_realizada`: `sin_acceso` \| `equipo_no_encontrado` \| `otro`), referencia (texto o null), referencia_tipo (`orden` \| `ticket` o null), ocurrido_en | Actualiza el estado de la solicitud de corte manual del servicio (§13.2) y su referencia. Solo aplica una `version` mayor que la que tiene; los estados terminales (`resuelta`, `cancelada`, `no_realizada`) no retroceden. Idempotente por `X-Webhook-Id`; una `solicitud_id` desconocida o de otra empresa, `404`. Solo desde conectores con contrato ≥ 0.15.0 y la marca `corte_manual` |

Argos encuentra su envío por `idempotency_key` (`argos:aviso:<plantilla>:<envio_id>`, §6.2). Llega un solo webhook final por aviso, `aviso.entregado` o `aviso.fallido`; desde 0.15.1, un aviso `auto` puede terminar en cambio con `aviso.bloqueado` (sin webhook final después). Un `leido_en` posterior no manda otro webhook: solo se ve en el GET.

**0.9.0:** suma tres eventos, **solo a conectores con contrato ≥ 0.9.0** (§4.3). Solo de tickets con `cliente_id` dentro del alcance del conector, de cualquier origen. Nunca llevan el cuerpo de un mensaje, notas internas, un motivo escrito por una persona, ni datos de otro cliente; Argos lee lo que muestra con §11.

| Evento | Payload | Efecto en Argos |
| --- | --- | --- |
| `ticket.estado_cambiado` | ticket_id, numero, empresa_id, cliente_id, servicio_id (o null), origen, de, a, puede_confirmar (`a = resuelto`) | Actualiza el portal y, si corresponde, avisa al cliente (`resuelto`: «revisa y confirma»; `cerrado`) |
| `ticket.respondido` | ticket_id, numero, empresa_id, cliente_id, mensaje_id, autor_tipo (`agente` \| `sistema`), masivo (true en el aviso de una falla general), aviso_por_correo (true si Iris ya lo mandó por correo al contacto del ticket) | Avisa «tienes una respuesta en tu ticket TK-…»; con `aviso_por_correo` no manda correo; con `masivo`, un solo aviso por cliente |
| `solicitud_servicio.estado_cambiado` | solicitud_id, numero, empresa_id, cliente_id, tipo, servicio_id (o null), de, a (`estado_cliente` de §11.7) | Actualiza «Mis solicitudes» y avisa al cliente |

Un mensaje que el cliente escribió desde el portal de Argos (`ticket.mensaje_cliente` con canal `portal_argos`) no vuelve como webhook. Un ticket fusionado con el de **otro** cliente solo llega como `ticket.estado_cambiado` a `cerrado`, sin el destino.

**0.10.0:** suma `prospecto.estado_cambiado` (prospecto_id, empresa_id, de, a, motivo_codigo), solo de prospectos con origen `argos_panel` (§3.6) y a conectores con contrato ≥ 0.10.0. Sin datos personales.

**0.11.0:** suma `cuadrilla.creada` y `cuadrilla.actualizada` (cuadrilla_id, `empresa_id` obligatorio, `version` entera y creciente por cuadrilla, nombre, activa, `miembros[] { usuario_id, id_externo }` completo, con `id_externo` null si el usuario no está vinculado por I1, y `jefe_usuario_id` o null), a conectores con contrato ≥ 0.11.0. Un evento por cada empresa que atiende la cuadrilla, con los miembros que tienen vínculo I1 en esa empresa. Argos sincroniza con ellos la bodega de cuadrilla (§3.9) y solo aplica una `version` mayor que la última; para conciliar lee `GET <iris>/api/v1/integraciones/argosisp/cuadrillas/:id`.

**0.14.0:** suma `tecnico.argos_vinculado` y `tecnico.argos_desvinculado` (tabla de arriba), para **todo** técnico, tenga cuadrilla o no, a conectores con contrato ≥ 0.14.0. Con ellos Argos conoce el vínculo de cada actor (§3.9).

- Iris los manda solo al conector del que salió el `id_externo`, y solo para las empresas de su alcance. `version` es entera y creciente por técnico; Argos los deduplica por `X-Webhook-Id` y solo aplica una `version` mayor que la última.
- Argos acepta el vínculo solo si `id_externo` es un usuario activo, con bodega o rol técnico en esa empresa y dentro del alcance del usuario técnico Iris (§1.1). Un `id_externo` no puede quedar vinculado a dos `usuario_id`. Si no se cumple, lo muestra como `vinculo_rechazado` en su panel.
- **Vínculo que cambia de técnico.** Cuando un `desvinculado` libera un `id_externo`, Argos vuelve a evaluar los vínculos rechazados por unicidad sobre ese `id_externo` y para eso lee `GET …/tecnicos/:usuario_id` (abajo). Iris no emite el `vinculado` del técnico nuevo hasta tener el `2xx` del `desvinculado` del anterior.
- Iris emite `tecnico.argos_desvinculado` con `motivo: desactivado` cuando se desactiva el técnico o su usuario.
- **Carga inicial.** Al informar 0.14.0, Iris manda un `vinculado` por cada vínculo vigente y, con el `2xx` de todos, cierra la carga con `PUT /integraciones/iris/contrato { contrato, vinculos_cargados: <n> }`. Hasta entonces, Argos responde `409 vinculos_en_carga` (se reintenta, §3.3) en lugar de `actor_desconocido`.
- Iris no envía una operación con `tecnico` si el `tecnico.argos_vinculado` de ese técnico todavía no tiene el `2xx` de Argos: el pendiente queda bloqueado con `depende_de`.
- **Conciliar.** Argos lee `GET <iris>/api/v1/integraciones/argosisp/tecnicos/:usuario_id?empresa_id=` (permiso `cuadrillas.leer_integracion`): `{ tecnico_id, usuario_id, empresa_id, id_externo, version, activo }`.
    - Responde solo si el vínculo salió de ese conector. Si no, si el usuario no tiene ficha de técnico en esa empresa o si está fuera del alcance, `404`: la misma respuesta en los tres casos.
    - Argos aplica la respuesta con las mismas reglas que el webhook: `version` mayor y validación.
    - `activo: false` equivale a desvinculado.
- Este vínculo es solo de inventario. Argos lo guarda aparte del vínculo de §7.7 y nunca autoriza con él una acción de §7.

**Consultar un aviso.** `GET /api/v1/mensajeria/avisos/:id` (permiso `mensajeria.enviar`), por si Argos perdió un webhook. Argos lo consulta para los envíos `encolado` con más de 24 h.

- `:id` es el `aviso_id` del `202` de §6.2. Iris solo muestra los avisos que creó el mismo conector; uno de otro conector o de otra empresa responde `404` problem+json.
- Responde `200 { aviso_id, idempotency_key, empresa_id, cliente_id, servicio_id (o null), plantilla, estado, canal (o null), motivo (o null), detalle (o null), creado_en, entregado_en (o null), leido_en (o null) }`.
- `estado` = `en_cola` | `bloqueado` | `entregado` | `fallido`. `leido_en` es un dato, no un estado. `motivo` sale con `bloqueado` (los motivos de §6.2) y con `fallido` (los de esta tabla).
- En Argos: `entregado` → `enviado`, `fallido` → `fallido`, `bloqueado` → `omitido`; `en_cola` sigue `encolado`.
- Si Argos perdió el `202`, reenvía el `POST` de §6.2 con la misma `Idempotency-Key`: Iris devuelve el mismo `aviso_id` y el estado actual.

### 6.7 Plantillas de WhatsApp: lectura de Argos (0.13.0, B32)

Las plantillas de WhatsApp se editan y se aprueban **solo en Iris** (decisión del PM, §10.9). Argos las lee para saber qué avisos puede pedir:

- `GET /api/v1/integraciones/argosisp/plantillas-whatsapp?empresa_id=`, con el token de §1.2 y el permiso `plantillas_whatsapp.leer_integracion`. Solo empresas del alcance del conector (fuera, `403 empresa_no_permitida`).
- Responde `{ data: [{ nombre, idioma, categoria, estado, codigos_aviso[], vista_previa }] }`: `codigos_aviso[]` son los códigos de §6.3 que atiende, y `vista_previa` el texto con las variables como `{{nombre}}`.
- **Nunca** devuelve el id de WABA, el id de Meta ni el motivo de rechazo completo (solo el `estado`).
- Solo devuelve las plantillas que **esa empresa puede usar**: las de un canal prestado que no la autorizan no se ven.

## 7. Pagos y atención (0.6.0, fase 1)

Los clientes mandan por chat o llamada la foto de su boleta de depósito o piden una promesa de pago. Los atiende un asistente de IA de Iris o una persona. **Iris nunca registra un pago, una promesa ni una reconexión por su cuenta: siempre lo pide a Argos, y Argos decide.** Desde 0.7.0, cuando la boleta la atiende una persona, esa persona puede aprobarla o rechazarla desde la conversación (§7.7). También ahí decide Argos: Iris solo transmite la decisión de un usuario de Argos vinculado, que es quien tiene el permiso. El asistente de IA nunca puede.

En Argos todo esto ya existe para el portal del cliente y el panel; Iris usa las mismas piezas:

| Lo que pide Iris | Pieza de Argos | Estado en Argos |
| --- | --- | --- |
| Boleta de pago | **Comprobante de depósito** (`comprobantes.registrar`): `pendiente` → `aprobado` (crea el pago) o `rechazado` | Existe, con canal `iris` (PR #217) |
| Aprobar o rechazar la boleta (una persona, 0.7.0) | Las acciones de caja `comprobantes.aprobar` y `comprobantes.rechazar`, ahora también con el token de Iris y a nombre del usuario de Argos vinculado (§7.7) | En curso (A23) |
| Promesa de pago | **Promesa** (`promesas.crear`, `POST /promesas`), con reglas por plantilla y por canal (`ia`, `operador`) | Existe |
| Política de promesas | **Simulador** (`POST /promesas/simular`) | Existe |
| Reconexión temporal | **Promesa de tipo `reconexion_temporal`** ligada a un comprobante pendiente | Existe (PR #217) |
| Reconexión temporal sin personal de caja (0.8.0) | La misma promesa, con `motivo: "sin_personal"` (§7.3) | Implementado en Argos (PR #249), sale en 1.18.0 (A25) |
| Cuentas donde deposita el cliente (0.8.0) | **Cuentas bancarias** de la empresa (`cuentas_bancarias.ver`), única fuente (§7.8) | Existe en 1.17.0; `titular`, `tipo` y el evento salen en 1.18.0 (A26) |
| Verificar identidad | `clientes.verificar_identidad` | Existe (PR #217) |
| Buscar por teléfono | Filtro `telefono` de `GET /clientes` | Existe (PR #217) |

### 7.1 Principios

| Regla | Detalle |
| --- | --- |
| Argos decide | Argos valida la boleta, aplica el pago y fija la política de promesas y de reconexión temporal. La boleta la aprueba o rechaza siempre un usuario de Argos con el permiso: caja en el panel o, desde 0.7.0, un usuario de Argos vinculado que atiende la conversación en Iris (§7.7). Iris solo transmite esa decisión; nunca decide por su cuenta, y el asistente de IA nunca puede. |
| Un comprobante no es un pago | Registrar un comprobante no mueve dinero ni toca el libro contable ni el estado del servicio. El pago, su asiento y la reactivación nacen solo cuando un usuario de Argos con `comprobantes.aprobar` lo aprueba: en el panel o desde Iris (§7.7). |
| Un solo juez del estado | El estado del servicio lo decide solo `evaluarEstadoServicio` de Argos. Ninguna llamada de Iris cambia un estado directo: la reconexión temporal es una promesa, y la promesa activa es lo que mantiene el servicio activo. |
| Identidad primero | Antes de dar el saldo o de reportar un pago, Iris verifica al cliente: número registrado más una prueba (§7.5). Sin verificar, solo responde información general. |
| Datos mínimos | Iris nunca recibe el documento (NIT o CUI) del cliente ni datos de tarjeta (§1.1). El saldo se muestra solo al cliente verificado y no se guarda en el historial del chat. |
| Actor | Cada llamada de §7 lleva `X-Iris-Actor` = `asistente_ia` \| `usuario:<id en Iris>` y `X-Iris-Conversacion` = id de la conversación. La forma exacta (0.7.1) es `^(asistente_ia\|usuario:[A-Za-z0-9_-]{1,64})$` para `X-Iris-Actor` (desde 0.14.2, `^(asistente_ia\|sistema\|usuario:[A-Za-z0-9_-]{1,64})$`) y `^[A-Za-z0-9._:-]{1,128}$` para `X-Iris-Conversacion`. Fuera de formato, o si faltan en una ruta de §7: `400 cabecera_iris_requerida`. Argos audita con actor `token` del usuario técnico Iris y guarda las dos cabeceras en la fila de auditoría, donde se pueden buscar. El canal de la auditoría es `ia` con `asistente_ia` y `api` con `usuario:<id>`. Excepción (0.7.0): en aprobar y rechazar (§7.7) el actor de la auditoría es el usuario de Argos vinculado a `usuario:<id>`, y el token de Iris queda como «vía». Desde 0.14.2, `X-Iris-Actor: sistema` vale **solo en una lista cerrada de lecturas** que Iris hace sin una persona detrás: `GET /clientes/:id/contactos` (su caché), `GET /cuentas-bancarias` y `GET /bancos`; cualquier otra ruta con `sistema` responde `403 actor_no_permitido` (nunca en `estado-cuenta`, promesas, comprobantes ni la búsqueda por `telefono`). Va siempre **sin** `X-Iris-Conversacion` ni `X-Iris-Contacto` (si llegan, `400 cabecera_iris_requerida`); Argos lo audita con canal `sistema`, como el token del conector y sin persona; en una escritura, `422 actor_no_permitido`. |
| Contacto (0.10.1) | `X-Iris-Contacto` = `<contacto_id>` (el id del contacto en Argos, entero positivo en decimal, `^[1-9]\d{0,17}$`) o el valor literal `titular`. Es **obligatoria en las rutas de §7 que llegan con `X-Iris-Conversacion`** (si falta: `400 cabecera_iris_requerida`), y con `<contacto_id>` va en las acciones que pide un contacto (§3.5): promesas (§7.4), comprobantes (§7.2) y `GET /clientes/:id/estado-cuenta?servicio_id=` (§2.1). Con `titular`, Argos exige que la conversación tenga registrada una verificación del titular de §7.5, o la audita como tal. Con `<contacto_id>`: si el contacto no es de ese cliente o el `servicio_id` no está entre sus `servicios[]`, `403 servicio_fuera_del_contacto`; si el permiso del contacto está apagado (`pagar` en promesas y comprobantes, `consultar` en `estado-cuenta`), `403 permiso_de_contacto`. Mal formada: `422 validacion` (`field: X-Iris-Contacto`). Argos la guarda en la auditoría junto a `X-Iris-Conversacion`. |
| Empresa | Cada documento es de una empresa: la del servicio (promesas) o la de la cuenta bancaria (comprobantes). Si Iris manda `empresa_id` y no coincide: `422 empresa_no_coincide`. Fuera del alcance del usuario técnico Iris: `403 empresa_no_permitida`. |
| Idempotencia | Todas las escrituras de §7 llevan `Idempotency-Key` (obligatoria: `400 idempotency_key_requerida`) con las reglas de §3.1. En §7.7 la huella incluye además `X-Iris-Actor`. Desde 0.10.1, en promesas y comprobantes la huella incluye `X-Iris-Contacto`: la misma clave con otro contacto responde `422 idempotency_key_reutilizada`. |

### 7.2 Comprobantes de pago (A16)

**Leer las cuentas.** El cliente deposita en una cuenta bancaria de la empresa. Iris lee las cuentas con `GET /api/v1/cuentas-bancarias?activa=true` (`cuentas_bancarias.ver`; solo las de moneda `GTQ` reciben depósitos) y los bancos de origen con `GET /api/v1/bancos`. Las dos responden un arreglo, sin paginar (§7.8). El asistente le pregunta al cliente a cuál depositó si la foto no lo dice. Desde 0.8.0 este catálogo es además la única fuente de las cuentas que Iris le ofrece al cliente (§7.8).

**Registrar.** `POST /api/v1/comprobantes` (`comprobantes.registrar`), `multipart/form-data`, con `Idempotency-Key = iris:boleta:<id de la boleta en Iris>`. Los campos son los mismos del portal de Argos más los de Iris:

| Campo | Obligatorio | Detalle |
| --- | --- | --- |
| `cliente_id` | sí | El cliente verificado |
| `servicio_id` | no | Del mismo cliente; si viene, el pago se aplica primero a sus facturas |
| `metodo` | sí | `deposito` \| `transferencia` \| `ach` \| `remesa` |
| `cuenta_bancaria_id` | sí | Cuenta propia de destino (de ella sale la empresa) |
| `banco_origen_id` | con `transferencia` y `ach` | De `GET /bancos` |
| `referencia` | sí | Número de boleta o de transacción |
| `monto` | sí | String decimal con 2 decimales, > 0, en GTQ |
| `fecha_deposito` | sí | `YYYY-MM-DD`, no futura |
| `archivo` | sí | La foto (JPG, PNG o WebP) o el PDF, hasta 5 MB. Argos decide el tipo por el contenido y guarda su propia copia |
| `canal_contacto` | sí | `whatsapp` \| `telegram` \| `correo` \| `llamada` |
| `leido_por` | sí | `ia` \| `persona` \| `cliente_dictado` |
| `confianza` | no | 0–1, o vacío: qué tan seguro está Iris de los datos que leyó de la foto |
| `empresa_id` | no | Si viene, debe coincidir con la de la cuenta (§7.1) |
| `nota` | no | Hasta 500 caracteres; caja la ve |

- Respuesta `201 Comprobante` con `{ id, estado: "pendiente", empresa_id, … }`. Iris guarda `id` como `comprobante_id`. El comprobante queda con `canal = iris`, y `leido_por`, `confianza`, `canal_contacto` y `X-Iris-Conversacion` se muestran a caja; Argos nunca los aplica solos.
- **Cola de caja:** el comprobante entra en la cola que ya existe en el panel de Argos (`GET /comprobantes?estado=pendiente`, filtro nuevo `canal=iris`), con el botón «Ver conversación en Iris» (pedido B2).
- Errores de negocio, que no se reintentan:
    - `409 referencia_duplicada`: la misma referencia (mismo método y cuenta) ya es un pago, o hay otro comprobante pendiente con ella. Uno rechazado la deja libre.
    - `422` por campo (`archivo` con `rule` `required`, `tipo` o `tamano`; `fecha_deposito` futura; `monto`).
    - `422 cuenta_de_otra_empresa` (campo `cuenta_bancaria_id`): la cuenta es de otra empresa que el servicio.
    - `422 empresa_no_coincide`.
- `GET /api/v1/comprobantes/:id` (`comprobantes.ver`) devuelve su estado, por si Iris perdió un webhook. Iris lo lee también antes de aplicar un webhook `comprobante.*` (§7.7, «Lectura para confirmar»).

**Resolución.** Aprueba o rechaza un usuario de Argos con `comprobantes.aprobar` o `comprobantes.rechazar`: caja en el panel o, desde 0.7.0, la persona que atiende la conversación en Iris, si es un usuario de Argos vinculado (§7.7). El asistente de IA nunca puede. Al aprobar, Argos registra el pago, su asiento y la reactivación en una sola transacción. Los webhooks son los mismos en los dos casos.

Webhooks Argos → Iris (§2.3), para comprobantes de cualquier canal dentro del alcance; solo a conectores con contrato ≥ 0.6.0:

| Evento | Payload |
| --- | --- |
| `comprobante.aprobado` | comprobante_id, empresa_id, cliente_id, servicio_id (o null), canal (`portal` \| `panel` \| `iris`), monto, pago_id, motivo: null, causa: null, servicio_estado (estado del servicio después de aplicar el pago, o null sin servicio), verificado_iris_actor (0.7.1: `usuario:<id>` de quien aprobó desde Iris, o null si decidió caja en el panel) |
| `comprobante.rechazado` | lo mismo, con pago_id: null, motivo (texto de caja) y causa (`no_aparece` \| `monto_distinto` \| `ilegible` \| `falta_dato` \| `duplicado` \| `otro`); verificado_iris_actor es quien rechazó desde Iris, o null |

- `servicio_estado = "activo"` después de un corte es lo que la propuesta llamaba `reconectado`. El cambio de estado llega además, como siempre, en `servicio.estado_cambiado`.
- `causa = falta_dato` es lo que la propuesta llamaba `falta_dato`: el comprobante queda rechazado, la referencia queda libre y el cliente lo vuelve a mandar completo (otro `POST /comprobantes` con otra `Idempotency-Key`). Iris usa `boleta_falta_dato` (§6.3).
- Con `canal = iris`, Argos no avisa al cliente por su cuenta del comprobante ni del pago que nace de él: lo hace Iris en la conversación.
- `verificado_iris_actor` (0.7.1, A24) es el `X-Iris-Actor` de quien decidió desde Iris (§7.7). Con Argos 1.15.0 (contrato 0.7.0) el campo no viene.

### 7.3 Reconexión temporal (A17; ampliada en 0.8.0)

Cuando un cliente cortado manda su comprobante y nadie de caja puede revisarlo pronto, Iris puede pedir que Argos lo reconecte mientras se revisa. En Argos es una **promesa de pago** de tipo `reconexion_temporal`, ligada al comprobante: `evaluarEstadoServicio` mantiene el servicio activo mientras esté activa, igual que cualquier promesa. Nunca es un cambio de estado directo.

`POST /api/v1/comprobantes/:id/reconexion-temporal` (`comprobantes.reconexion_temporal`) con `{ servicio_id?, motivo }` e `Idempotency-Key = iris:reconexion:<comprobante_id>`. `servicio_id` es obligatorio solo si el comprobante no tiene servicio.

- **`motivo`** = `fuera_de_horario` | `sin_personal`. `sin_personal` es nuevo en 0.8.0 (A25). Cualquier otro valor responde `422` con `type` `urn:argosisp:problema:validacion` y `errors[0] = { field: "motivo", rule: "enum", message }`, antes de crear nada. Un Argos 1.17.x responde eso mismo a `sin_personal`.

    | `motivo` | Qué significa | Desde |
    | --- | --- | --- |
    | `fuera_de_horario` | El comprobante llegó fuera del horario hábil de la empresa | 0.6.0 |
    | `sin_personal` | Es horario hábil, pero no hay nadie de caja que pueda revisarlo ahora | 0.8.0 |

- Las reglas y los `422` son **los mismos con los dos motivos y en el mismo orden** (plantilla, cupo, cubre lo vencido…). Con cualquiera de los dos, el comprobante queda `pendiente` en la cola de caja.
- Respuesta `201 { promesa_id, reconexion_id, hasta, servicio: { id, estado } }`. `reconexion_id` = `promesa_id` (alias para Iris); `hasta` = vencimiento de la promesa. La respuesta **no** trae `motivo_reconexion`: Iris ya sabe qué motivo mandó.

```json
{ "servicio_id": 4410, "motivo": "sin_personal" }
```

```json
{
  "promesa_id": 9012,
  "reconexion_id": 9012,
  "hasta": "2026-11-19T06:00:00.000Z",
  "servicio": { "id": 4410, "estado": "activo" }
}
```

El ejemplo es una reconexión pedida el miércoles 18 nov a las 15:20 de Guatemala con `vigencia: "medianoche"`: `hasta` son las 00:00 del jueves 19 en Guatemala.

- **Formato de `hasta`.** Un instante RFC 3339 en UTC, **con milisegundos** y `Z` (`.000Z`). Iris acepta el instante con y sin milisegundos. `vence_en` de la promesa (`GET /promesas/:id`) y `vence` de los webhooks `promesa.*` usan el mismo formato.
- **`hasta` es el fin de la promesa, no la hora del corte.** Cuando la promesa vence, Argos re-evalúa el servicio (su revisión de promesas corre cada minuto), pero el corte respeta la `hora_corte` de la plantilla de facturación: el servicio puede seguir activo después de `hasta` (ver «Cómo termina»).

- Política (la fija Argos en la plantilla de facturación, bloque `reconexion_temporal` de `reglas_promesa`, exceptuable por servicio). Valores aprobados por el PM (5 nov 2026):
    - apagada por defecto (`permitida: false`) hasta que cada empresa la active;
    - a lo sumo **una por periodo de facturación y servicio** (`max_por_periodo: 1`), aparte del cupo de promesas normales. Es **un solo cupo para los dos motivos**: cuenta todas las reconexiones temporales del servicio en el periodo, en cualquier estado;
    - solo si el comprobante está `pendiente`, es del mismo cliente y su `monto` cubre **todo lo vencido** del servicio;
    - dura según `horas_max` de la plantilla (24 h por defecto) desde que se crea, con `vigencia: "horas"`.
- **Vigencia (0.8.0).** `reglas_promesa.reconexion_temporal.vigencia` = `horas` (por defecto) | `medianoche`.
    - `horas`: como hasta hoy, dura `horas_max` desde que se crea.
    - `medianoche`: dura hasta la siguiente medianoche de Guatemala, que son las **00:00 del día siguiente** (`06:00:00.000Z`), no el último instante del día. Si faltan menos de 60 minutos para esa medianoche, dura hasta la medianoche siguiente (menos de 25 h en total). `horas_max` no se usa.
    - **El vencimiento real llega siempre en `hasta`.** Iris no asume horas ni calcula la medianoche: lee `hasta`.
- `422 reconexion_no_permitida`, con la razón en `errors[0].rule`: `no_habilitada`, `cupo_agotado`, `comprobante_no_pendiente`, `comprobante_no_cubre`, `servicio_no_cortado` (el servicio ya está activo), `suspension_manual` (lo suspendió un operador: solo él lo reactiva), `promesa_abierta` (ya tiene una promesa activa o pendiente), `servicio_de_otro_cliente`. Es de negocio y no se reintenta.
- **Cómo termina** (webhooks `promesa.*` de §7.4, con `tipo = reconexion_temporal`). Aquí «caja» es quien aprueba o rechaza el comprobante: en el panel o desde Iris (§7.7). No cambia en 0.8.0 ni depende del motivo:
    - caja aprueba el comprobante y el pago salda lo vencido → `promesa.cumplida`;
    - caja lo rechaza → la promesa se cancela al instante → `promesa.cancelada` con `motivo = comprobante_rechazado`, y el servicio se re-evalúa: vuelve a cortarse (si la plantilla tiene hora de corte y aún no llega, espera a esa hora, como cualquier corte);
    - llega `hasta` con el comprobante aún pendiente → `promesa.cancelada` con `motivo = vencida_sin_revisar` y re-evaluación; el comprobante sigue en la cola de caja. No cuenta como promesa incumplida en el historial del cliente: la demora fue de caja;
    - caja aprueba pero el pago no salda lo vencido → sigue activa hasta `hasta` y entonces `promesa.incumplida`, como cualquier promesa.
- **`motivo_reconexion` (0.8.0).** La promesa guarda con qué motivo se pidió: `motivo_reconexion` = `fuera_de_horario` | `sin_personal` | `null`.
    - Viene **siempre** en la `Promesa`: `GET /promesas`, `GET /promesas/:id` y la respuesta `201` de `POST /promesas`. También en los cuatro webhooks `promesa.*` (§7.4), sin filtro de versión.
    - **No** viene en la respuesta `201` de `POST /comprobantes/:id/reconexion-temporal`.
    - Es `null` en **toda** promesa con `tipo = promesa` y nunca es `null` en una `reconexion_temporal` (lo garantiza una restricción en la base de Argos). Las reconexiones anteriores a la 0.8.0 quedan con `fuera_de_horario`.
    - No es el `motivo` de `promesa.cancelada`, que dice por qué terminó.
    - **Revisar el criterio en Argos:** `GET /promesas` suma el filtro `motivo_reconexion=fuera_de_horario|sin_personal` (con `tipo=reconexion_temporal`) y el panel de Argos lo ofrece en su lista de promesas. La bitácora de Argos no filtra por motivo: el motivo queda en la entrada de la acción y en la promesa.
- Iris nunca pide una reconexión temporal sin un comprobante registrado. Argos no avisa al cliente por su cuenta: Iris le explica en los cuatro casos.

**`sin_personal`: Argos confía en Iris (0.8.0).** Argos no tiene un horario de caja propio, así que no puede comprobar si había o no personal. El criterio es **de Iris**, y cada petición queda auditada en Argos con `X-Iris-Actor`, el canal, `X-Iris-Conversacion` y el motivo (§7.1). Argos **no** quiere más datos para auditarlo (por ejemplo, cuántas personas de caja había): no podría verificarlos y darían una falsa sensación de control. Eso lo audita Iris de su lado. Las barreras de Argos son las mismas de siempre y no dependen de Iris:

- la plantilla debe tener la reconexión habilitada (viene apagada de fábrica);
- el cupo por periodo;
- el comprobante debe cubrir todo lo vencido;
- sin suspensión manual y sin otra promesa abierta.

**Del lado de Iris (decidido por el PM, §10.2, puntos 1, 2 y 12).** Amplía `conversaciones.md` §17.8, que hoy solo pide la reconexión fuera de horario.

- **Cuándo pide `sin_personal`.** Solo cuando se cumplen las dos cosas: es **horario hábil** de la empresa (el mismo horario con el que Iris decide hoy `fuera_de_horario`) y **ningún usuario con rol `caja` está en estado disponible en esa empresa**. Si hay al menos una persona de caja disponible, Iris no la pide: responde su `422 reconexion_no_corresponde` y la boleta la revisa esa persona, como hoy. No hay espera de minutos: cuenta la disponibilidad en ese momento. Si la empresa no tiene a nadie con rol `caja`, no se pide nunca (valores iniciales del líder de Iris, §10.2).
- **Cuándo pide `fuera_de_horario`.** Igual que hoy: fuera del horario hábil. Iris nunca manda los dos; el motivo sale del reloj y de la disponibilidad, no de lo que diga el cliente ni el modelo.
- **Apagado de fábrica.** `sin_personal` tiene su propio interruptor en Iris, apagado por defecto y aparte del de la reconexión fuera de horario. Además, Iris solo lo manda si Argos informó contrato ≥ 0.8.0 (§4.1). Con cualquiera de las dos cosas en falta, se comporta como en 0.7.1.
- **Verificación fuerte.** La exige siempre, sin opción, igual que la reconexión fuera de horario de hoy (`conversaciones.md` §17.8).
- **Un comprobante registrado.** Como hoy: una boleta registrada en Argos en esa conversación.
- **Qué le dice el asistente al cliente.** Vale para los dos motivos. El texto lo arma Iris con el `hasta` de Argos, pasado a hora de Guatemala; el modelo no escribe la fecha ni la hora.
    - **Sí dice** hasta cuándo **vale la reconexión temporal**, con el día y la hora de `hasta`. Con una hora cualquiera: «la reconexión temporal vale hasta las 10:00 de la mañana del jueves 19 de noviembre». Cuando `hasta` cae a las 00:00, nombra los dos días para que no haya duda: «la reconexión temporal vale hasta la medianoche del miércoles 18 (00:00 del jueves 19)».
    - **Sí dice** que **su comprobante queda pendiente de revisión**: la reconexión no es la confirmación del pago.
    - **Nunca dice** «se corta a medianoche», «a esa hora se le suspende» ni ninguna otra hora de corte: `hasta` es el fin de la promesa, y el corte lo decide Argos con la hora de corte de la plantilla.
    - **Nunca dice** «las 12:00 de la noche de hoy»: es ambiguo.
    - Si el cliente pregunta qué pasa después: si el pago se confirma antes, no pasa nada; si no, el servicio puede volver a suspenderse después de esa hora. Sin dar una hora de corte.
- **Evento.** `reconexion.solicitada` suma el motivo `sin_personal` a su campo `motivo` (hoy solo `fuera_de_horario`). Está en `eventos.md` e implementado en Iris 0.33.0.
- **Auditoría en Iris.** El intento se registra antes de llamar a Argos, como hoy, con el motivo y con cuántas personas de caja había y cuántas disponibles en ese momento, para poder revisar el criterio después.

### 7.4 Promesas de pago (A18)

Son las promesas que Argos ya tiene (contrato `promesas.md` de Argos). Una promesa es **por servicio** y **cubre todo lo vencido**: Argos calcula el monto, nadie lo escribe.

1. **Política:** `POST /api/v1/promesas/simular` (`promesas.ver`) `{ servicio_id, vence_en? }` evalúa todas las reglas sin crear nada ni auditar. Responde `resultado` (`aprobada` | `rechazada` | `pendiente_aprobacion`), `regla` y `motivo` (la primera que falla), `monto` (lo vencido que cubriría), `facturas[]` y `limites`. Correspondencia con lo que pedía Iris:

    | Propuesta de Iris | Argos (`limites` del simulador) |
    | --- | --- |
    | `permitida` | `resultado_creacion` distinto de `error` y `resultado` distinto de `rechazada` |
    | `max_dias` | `plazo_max_dias` y `fecha_maxima` (último día posible, Guatemala) |
    | `max_por_mes` | `max_por_periodo` (por **periodo de facturación**, no por mes calendario) |
    | `usadas_este_mes` | `en_periodo` |
    | `saldo_vencido` | `deuda_vencida` (y `monto_promesa`) |
    | `motivo` | `motivo` y `regla` |

2. **Crear:** `POST /api/v1/promesas` (`promesas.crear`) `{ servicio_id, vence_en }` con `Idempotency-Key = iris:promesa:<id en Iris>`. `vence_en` es un instante RFC 3339 (Iris manda el final del día prometido en hora de Guatemala). El canal de la promesa lo pone Argos según `X-Iris-Actor`: `ia` para `asistente_ia` y `operador` para `usuario:<id>`; cada uno tiene sus límites en la plantilla. `usuario:<id>` cuenta como `operador` sin exigir el vínculo de §7.7. En 0.7.0 Argos agrega a la plantilla la casilla «Mismas reglas para IA y operador»: es solo de Argos y no cambia la forma de esta llamada.
    - Respuesta **siempre** `201 Promesa`, porque Argos guarda también la promesa rechazada (cuenta para el historial). Iris mira `estado`:
        - `activa`: aceptada; el servicio se re-evalúa en la misma transacción y la respuesta trae `servicio`. Iris usa `promesa_aceptada` (§6.3);
        - `rechazada`: fuera de política; `resultado_evaluacion.regla` y `.motivo` dicen por qué. Iris pasa la conversación a una persona;
        - `pendiente_aprobacion`: la plantilla pide que un supervisor de Argos la apruebe. Iris le dice al cliente que queda en revisión y espera el webhook.
    - `409 promesa_abierta`: el servicio ya tiene una promesa activa o pendiente. `422` si `vence_en` no es futura, o el servicio está retirado, pendiente de instalación o sin plantilla. De negocio, no se reintentan.
    - `monto` en el cuerpo se ignora; `fecha_compromiso` de la propuesta es `vence_en`.
3. **Webhooks Argos → Iris** (§2.3), para promesas de cualquier canal dentro del alcance; solo a conectores con contrato ≥ 0.6.0. Payload común: promesa_id, empresa_id, cliente_id, servicio_id, tipo (`promesa` \| `reconexion_temporal`), canal (`portal` \| `ia` \| `operador` \| `supervisor`), comprobante_id (o null), vence (RFC 3339 en UTC con milisegundos), monto. Desde 0.8.0 suma `motivo_reconexion`: `fuera_de_horario` \| `sin_personal` \| `null` (`null` cuando no es una reconexión temporal); el mismo campo viene en la `Promesa` (`GET /promesas`, `GET /promesas/:id` y el `201` de `POST /promesas`; §7.3). Un Argos anterior no lo manda, e Iris lo trata como `null`.

    | Evento | Cuándo |
    | --- | --- |
    | `promesa.creada` | Queda activa: al crearla o al aprobarla un supervisor |
    | `promesa.cumplida` | Se saldaron todas sus facturas |
    | `promesa.incumplida` | Venció sin saldarse |
    | `promesa.cancelada` (nuevo en el catálogo de Argos) | Un supervisor la canceló o rechazó, o una reconexión temporal terminó sin pago (§7.3). Lleva además `motivo` |

    Con promesas creadas por Iris, Argos no avisa al cliente por su cuenta.

### 7.5 Verificar identidad sin exponer el documento (A19)

`POST /api/v1/integraciones/iris/verificar-identidad` (`clientes.verificar_identidad`) `{ cliente_id, metodo, valor }`, con `metodo` = `documento` | `nombre` | `codigo_cliente` (decisión del PM, 5 nov 2026: se verifica con el DPI/NIT completo, el nombre del cliente o su código).

- `documento`: el documento fiscal **completo** del cliente (`nit_cui`: NIT o CUI/DPI, el que tenga; Argos guarda uno solo). Sin guiones ni espacios y sin distinguir mayúsculas (el NIT puede terminar en `K`).
- `nombre`: el nombre del cliente, comparado sin mayúsculas, tildes, espacios repetidos ni signos. **Es una prueba débil**: Iris ya lo tiene en su instantánea. Solo vale junto con otra prueba, nunca sola.
- `codigo_cliente`: el código de cliente de Argos. **Es una prueba débil**: Iris ya lo tiene en su instantánea (§2.2) y lo trae cada factura. Solo vale junto con otra prueba, nunca sola.
- Argos compara en tiempo constante y responde `200 { coincide: true | false }`. **Nunca devuelve el dato.** Un cliente sin documento (`cf`) responde `422 metodo_no_disponible`, que no cuenta como intento.
- Límite: 5 intentos fallidos por cliente cada 30 minutos (ventana en Redis, solo un contador). Al pasarse, `429 demasiados_intentos` con `Retry-After`.
- Cada intento queda en la auditoría de Argos (`clientes.verificar_identidad`, con `valor` como `***`), también los rechazados por el límite.
- Desde 0.6.0 Argos ya no entrega `nit_cui` al usuario técnico Iris (§1.1): si Iris lo tuviera, pedir el documento no probaría nada.
- **0.10.0:** como `documento` solo vale un `nit_cui` que ya estaba en Argos o que viene de una identidad confirmada (A41); nunca el que llenó A43 por sí solo desde la orden (§3.4.6), ni el que mandó el token de Iris en `POST /clientes` (§3.2) hasta que haya una identidad A41 que coincida. Argos guarda de dónde vino cada `nit_cui`.
- El código por WhatsApp y la revisión de una foto del DPI los hace Iris (la foto la revisa siempre una persona) y no necesitan a Argos.
- **Teléfonos y correos registrados (0.10.0):** para `codigo_telefono` y `codigo_correo` cuentan solo `telefono`, `movil` y `email` **del cliente**, nunca los de sus contactos (§3.5), y uno agregado por A43 solo después de que el titular lo confirme con un código (§3.4.6).
- **Para cambiar el Wi-Fi (0.9.0, §12.4)** `datos_titular` solo vale si en esa conversación coincidió el `documento`. `nombre` y `codigo_cliente` nunca bastan, ni juntos. Un cliente sin documento (`cf`) solo se verifica con un código al teléfono o al correo registrados (`codigo_telefono`, `codigo_correo`).
- Iris nunca guarda el `valor`: en la conversación, la transcripción de la llamada, los logs y su auditoría queda como `***` desde que el cliente lo escribe o lo dicta, y el asistente de IA solo ve una marca en su lugar: Iris lo reemplaza antes de pasarle el mensaje al modelo y lo manda a Argos directamente.

### 7.6 Buscar cliente por teléfono (A20)

`GET /api/v1/clientes?telefono=+50255123344` (`clientes.ver`, el permiso que Iris ya tiene). Argos normaliza el número con la regla de §3.2 (`422 validacion` con `rule: 'telefono'` si no es un teléfono válido) y busca coincidencia **exacta** de dígitos sobre `telefono` y `movil` del cliente y `contacto_telefono` de sus servicios no retirados. Los teléfonos viejos guardados sin normalizar también coinciden: se comparan solo los dígitos, con `502` delante para los de 8 dígitos.

- Responde la lista corta habitual (`{ data, nextCursor }`), filtrada por el alcance de empresas del usuario técnico Iris. Cada cliente trae además `coincide_en[]`: `telefono`, `movil` y/o `contacto_servicio`.
- `contacto_servicio` es la persona de contacto en sitio, que puede no ser el titular: Iris no la trata como cliente verificado.
- Iris la usa para reconocer quién llama o escribe. Con más de un resultado, Iris le pide al cliente que diga cuál es su servicio, sin leerle la lista.
- `q` sigue igual: busca por partes del número entre otros campos. `telefono` es otro filtro, exacto.

### 7.7 Aprobar y rechazar desde la conversación (A23, 0.7.0)

Cuando la boleta la atiende una **persona** en Iris, esa persona puede aprobarla o rechazarla desde la conversación, igual que caja en el panel de Argos. Aprobar registra el pago. **Argos sigue decidiendo:** Iris solo transmite la decisión de un usuario de Argos vinculado, que es quien tiene el permiso. El asistente de IA nunca puede.

**Rutas.** Son las que caja ya usa en el panel; desde 0.7.0 aceptan también el token de Iris.

| Acción | Llamada | Cuerpo | Idempotency-Key |
| --- | --- | --- | --- |
| Aprobar | `POST /api/v1/comprobantes/:id/aprobar` (`comprobantes.aprobar`) | `{ facturas?: number[], nota?: string }` (`nota` ≤ 400) | `iris:aprobar:<comprobante_id>` |
| Rechazar | `POST /api/v1/comprobantes/:id/rechazar` (`comprobantes.rechazar`) | `{ motivo: string, causa?: string }` (`motivo` obligatorio, 3–500) | `iris:rechazar:<comprobante_id>` |

- **Aprobar va sin `monto` ni `fecha`:** se aprueba por el monto del comprobante y el pago queda con fecha de hoy. Si el monto no coincide con el depósito, no se aprueba: se rechaza con `causa: "monto_distinto"`.
- **`causa`** = `no_aparece` | `monto_distinto` | `ilegible` | `falta_dato` | `duplicado` | `otro` (las de §7.2). Sin `causa`, vale `otro`.
- **Cabeceras.** Con el token de Iris, las dos rutas exigen `X-Iris-Actor`, `X-Iris-Conversacion` e `Idempotency-Key`. Si falta alguna, `400` como en `POST /promesas` (`cabecera_iris_requerida`, `idempotency_key_requerida`; §7.1).
- **Idempotencia.** Reglas de §3.1, y además Argos incluye `X-Iris-Actor` en la huella: la misma clave con otro actor responde `422 idempotency_key_reutilizada` y nunca un replay. Así la decisión de una persona no se le atribuye a otra. Repetir la misma `Idempotency-Key` (mismo actor y mismo cuerpo) devuelve la respuesta guardada. Mientras la primera petición con esa clave sigue en curso, responde `409` (el `idempotencia_en_curso` de §3.1, que se reintenta con espera). Otra clave sobre un comprobante ya revisado responde `409 comprobante_revisado`.
- **Respuesta.** Las dos responden `200` con el `Comprobante` completo, el mismo esquema de `GET /comprobantes/:id` (§7.2). Campos que usa Iris:
    - `id`, `empresa_id`, `cliente_id`, `servicio_id`, `monto` (string decimal) y `canal`;
    - `estado`: `pendiente` | `aprobado` | `rechazado`;
    - `pago_id`: el pago creado al aprobar; `null` al rechazar;
    - `motivo_rechazo` y `causa_rechazo`: al rechazar;
    - `verificado_por` y `verificado_por_nombre`: el usuario de Argos vinculado que decidió;
    - `verificado_iris_actor` (0.7.1, A24): el `X-Iris-Actor` (`usuario:<id>`) de quien aprobó o rechazó desde Iris, o `null` si se decidió en el panel de Argos. Viene en `GET /comprobantes/:id`, en la lista (`GET /comprobantes`) y en esta respuesta;
    - `iris_actor` e `iris_conversacion`: el actor y la conversación que **registraron** el comprobante (§7.2). `iris_actor` no es quien lo decidió: ese es `verificado_iris_actor`.
- **Solo comprobantes de Iris.** Con el token de Iris solo se resuelven comprobantes con `canal = iris`. Otro canal: `409 comprobante_no_es_de_iris`. Uno ya aprobado o rechazado: `409 comprobante_revisado`. Son de negocio y no se reintentan.
- Conector apagado o desconectado: `409 iris_apagado`, igual que en §1.3.

**Quién puede (lo decide Argos).**

1. `X-Iris-Actor: usuario:<id>` lleva el id del usuario de Iris (`[A-Za-z0-9_-]{1,64}`). Argos lo busca en sus vinculaciones: conector `iris`, `id_externo = <id>`, vigente.
2. Ese vínculo nace **solo** del autologin **Iris → Argos** (§1.3): Iris pide un ticket con `POST /api/v1/integraciones/iris/sso/tickets` y `usuario.id_externo = <id del usuario en Iris>`; el usuario abre `/sso/entrar#<ticket>` en Argos y, la primera vez, confirma con su correo y su contraseña de Argos. El `id_externo` del ticket debe ser exactamente el id de `X-Iris-Actor`. «Ver en Iris» (Argos → Iris) no crea este vínculo.
3. El usuario de Argos vinculado debe estar activo, tener `comprobantes.aprobar` o `comprobantes.rechazar` (según la acción) y tener la empresa del comprobante en su alcance, intersectado con el alcance del token de Iris.
4. Los permisos del token de Iris (§1.1) solo abren la ruta; el permiso real es el del usuario vinculado.

Errores de autorización: todos `403` `application/problem+json` con `code`. Son de negocio y no se reintentan.

| `code` | Cuándo |
| --- | --- |
| `actor_sin_vinculo` | El `usuario:<id>` no tiene un vínculo vigente con un usuario de Argos |
| `actor_inactivo` | El usuario de Argos vinculado no está activo |
| `actor_sin_permiso` | El usuario vinculado no tiene `comprobantes.aprobar` o `comprobantes.rechazar` |
| `actor_sin_empresa` | La empresa del comprobante no está en el alcance del usuario vinculado, intersectado con el del token de Iris |
| `actor_ia_no_permitido` | Siempre con `X-Iris-Actor: asistente_ia` |

**Qué queda en Argos.**

- **Auditoría:** el actor es el usuario de Argos vinculado, con canal `api`. El token de Iris queda como «vía», y se guardan `iris_actor` e `iris_conversacion`.
- **Pago:** `verificado_por` y `registrado_por` son el usuario vinculado. El pago, su asiento, la reactivación y el cumplimiento de la promesa (también la reconexión temporal de §7.3) van en la misma transacción.
- **Quién decidió (0.7.1, A24):** `verificado_iris_actor` guarda el `X-Iris-Actor` de la decisión; queda `null` cuando decide caja en el panel. `iris_actor` no cambia: sigue siendo el de quien registró el comprobante.
- **Webhooks:** `comprobante.aprobado` y `comprobante.rechazado` (§7.2) llegan igual que cuando resuelve caja. Desde 0.7.1 traen `verificado_iris_actor`, con `null` cuando decidió caja en el panel. Iris avisa al cliente en la conversación como en §7.2.

**Lectura para confirmar (0.7.1).** Antes de aplicar un webhook `comprobante.*`, Iris lee `GET /api/v1/comprobantes/:id` y aplica el estado que diga Argos, no el del webhook. El token de Iris puede leer cualquier comprobante de sus empresas, así que Iris filtra por `canal = iris`: uno de otro canal no toca ninguna boleta. `GET /comprobantes/:id` exige las dos cabeceras de §7.1, `X-Iris-Actor` y `X-Iris-Conversacion`. Cuando la lectura la hace el sistema, sin una persona, Iris manda el actor de quien registró la boleta y la conversación de esa boleta. Esa lectura no deja acción de dominio en Argos.

**Atribución en Iris (0.7.1).** Cuando Iris se entera de la decisión por webhook o por relectura, usa `verificado_iris_actor` para atribuirla a su usuario (`usuario:<id>`). Con `null`, decidió caja en el panel de Argos. Con Argos 1.15.0 (contrato 0.7.0) el campo no viene, y entonces Iris solo muestra `verificado_por_nombre`.

**En Iris (pedido B3).** El botón «Vincular mi usuario de Argos» emite el ticket de §1.3 sin `destino`. La boleta de la conversación muestra «Aprobar y registrar pago» y «Rechazar» solo a una persona, nunca al asistente de IA. Los cinco `403` se le muestran a quien pulsó, con qué hacer (por ejemplo, vincular su usuario con `actor_sin_vinculo`).

### 7.8 Cuentas bancarias: Argos es la única fuente (0.8.0)

Las cuentas donde los clientes depositan son un catálogo **de Argos**, por empresa: son las mismas con las que se registra el comprobante (§7.2). Desde la 0.8.0, Iris no tiene cuentas propias ni textos con números de cuenta escritos a mano. Lo que el agente o el asistente le dice al cliente sale de este catálogo.

**Lo que existe hoy en Argos (1.17.0).** Con el permiso `cuentas_bancarias.ver`, que el token de Iris tiene desde 0.6.0 (§1.1).

- **`GET /api/v1/cuentas-bancarias`** responde un **arreglo plano** de cuentas, **sin paginar**: no es `{ data, nextCursor }`, y `limit` y `cursor` no existen (se ignoran). Son pocas filas por instalación y se queda así.
    - Sin `activa` trae **activas e inactivas**; `?activa=true` filtra las activas.
    - Orden: por banco, luego por `alias`, luego por `id`.
    - Solo trae las cuentas de las empresas del alcance del token.
- **`GET /api/v1/cuentas-bancarias/:id`** responde la cuenta. Si no existe: `404` `urn:argosisp:problema:no_encontrado`. De una empresa fuera del alcance: `403` `urn:argosisp:problema:empresa_no_permitida`. Es la misma semántica de «Confirmar un borrado» de §2.3, con una diferencia: **las cuentas nunca se borran** (no hay ruta ni acción de borrado), así que una cuenta que se deja de usar queda con `activa: false` y una boleta vieja siempre encuentra su cuenta.
- **Campos de la cuenta que Iris usa:** exactamente `id`, `empresa_id`, `banco_id`, `banco_nombre`, `numero` (3 a 40 caracteres), `alias` (2 a 80), `moneda` y `activa`.
- **Campos que también vienen y que Iris ignora** (no entran a su instantánea): `cuenta_id` y `cuenta_codigo` (la subcuenta del libro contable), `version`, `created_at` y `updated_at`.
- **No existen `nombre` ni un objeto `banco`,** y nunca existieron. El cliente de Iris los lee hoy por error. Es un **fallo de Iris**, que se corrige en la PR #203 de Iris, fuera de la 0.8.0; no es un cambio de contrato y Argos no agrega alias de compatibilidad (§10.3).
- **`moneda`** es `GTQ` | `USD` en el catálogo, pero solo las cuentas en `GTQ` reciben pagos y comprobantes. No cambia en la 0.8.0: Iris sigue filtrando por `GTQ`.
- **`GET /api/v1/bancos`** responde un arreglo con **solo los bancos activos** (`id`, `nombre`, `activo`). Es un catálogo fijo: no se edita por la API y solo cambia con una versión nueva de Argos, por eso no tiene evento. Iris lo relee al activar el conector, cuando cambia `version_argos` y cada 24 h. Lo mismo vale para `banco_nombre` dentro de las cuentas.
- **Cabeceras.** Hoy la lista de cuentas y la de bancos exigen `X-Iris-Actor` y `X-Iris-Conversacion` con el token de Iris; en la 0.8.0 pasan a ser opcionales en esas dos lecturas (§2.5). `GET /cuentas-bancarias/:id` no las exige.
- Hoy **no** existen `titular`, `tipo` ni un evento de cambio: crear o editar una cuenta no emite nada.

**Lo que Argos agrega en 0.8.0 (A26).**

- `titular` (string o `null`, de 2 a 120 caracteres): a nombre de quién está la cuenta. Las cuentas que ya existen quedan con `null` hasta que alguien las edite. El panel de Argos lo pide como obligatorio al crear y al editar; la API lo acepta nulo por compatibilidad. Argos no inventa un valor.
- `tipo`: `monetaria` | `ahorro`, o `null` en las cuentas viejas.
- Webhook `cuenta_bancaria.cambiada` (§2.3), solo con nombres de campos, nunca valores. Se emite al editar y también al crear (`creada: true`).
- `POST /comprobantes` con una cuenta `activa: false` se sigue rechazando, pero con `rule: "inactiva"` para distinguirla de una cuenta que no existe (hoy las dos responden `rule: "exists"`; §4.1).

```json
{
  "id": 7,
  "empresa_id": 3,
  "banco_id": 2,
  "banco_nombre": "Banco Industrial",
  "numero": "000-123456-7",
  "alias": "Monetaria principal",
  "titular": "Internet del Norte, S.A.",
  "tipo": "monetaria",
  "moneda": "GTQ",
  "activa": true,
  "cuenta_id": 118,
  "cuenta_codigo": "1.1.02.03",
  "version": 4,
  "created_at": "2026-03-02T15:00:00.000Z",
  "updated_at": "2026-11-18T21:20:00.000Z"
}
```

Webhook Argos → Iris, con el cuerpo de §2.3:

```json
{
  "id": "<id estable del webhook>",
  "evento": "cuenta_bancaria.cambiada",
  "ocurrido_en": "2026-11-18T21:20:00Z",
  "payload": { "cuenta_bancaria_id": 7, "empresa_id": 3, "campos": ["titular", "activa"], "activa": false, "creada": false }
}
```

**Del lado de Iris.**

- **Instantánea de lectura.** Iris guarda las cuentas en una tabla de instantánea (propuesta: `integracion.argos_cuentas_bancarias`) con los campos que usa (`id`, `empresa_id`, `banco_id`, `banco_nombre`, `numero`, `alias`, `titular`, `tipo`, `moneda`, `activa`) y `sincronizado_en`. Los campos contables no se guardan. Se llena en la primera sincronización (§2.5), con activas e inactivas, y se refresca con `cuenta_bancaria.cambiada`: al recibirlo, Iris relee `GET /cuentas-bancarias/:id` y guarda lo que diga Argos, no el payload. Sin el webhook (Argos anterior a 0.8.0), se refresca al usarla si tiene más de 24 h. Nunca se edita en Iris.
- **Sin cuentas propias.** Iris no tiene pantalla, tabla ni ajuste para crear o editar cuentas. Lo que hay que migrar está en §10.3.
- **Solo activas y solo de la empresa.** El agente y el asistente ofrecen únicamente cuentas con `activa: true`, de la **empresa de la conversación** y en la moneda que recibe depósitos (`GTQ`, §7.2). Una conversación sin empresa asignada no ofrece ninguna. El filtro es del servidor de Iris, no de la pantalla.
- **Antes de registrar una boleta,** Iris relee la cuenta elegida en Argos (`GET /cuentas-bancarias/:id`). Si no es de la empresa, no manda el comprobante y lo dice. Una instantánea vieja nunca decide a dónde se registra un pago.
- **Cuenta desactivada después del depósito.** Argos rechaza el comprobante con `422 validacion` y `rule: "inactiva"` en `cuenta_bancaria_id` (con un Argos anterior, `rule: "exists"`). Iris no reintenta: pasa la conversación a una persona, y caja registra el pago a mano en Argos. Aceptar depósitos hechos antes de desactivar la cuenta queda fuera de la 0.8.0.
- **Sin `titular`** (Argos anterior a 0.8.0, o cuenta que todavía lo tiene en `null`), Iris no le ofrece esa cuenta al cliente por chat ni por voz. La cuenta sigue sirviendo para registrar una boleta que ya se depositó. Decirle a un cliente dónde depositar sin decirle a nombre de quién facilita un fraude.
- **Cómo se le dice al cliente.** Con un texto fijo de Iris que lleva banco, tipo, número, titular y moneda.
    - Con una **persona**: un botón «Enviar cuentas» en la conversación inserta ese texto. La persona no lo escribe a mano.
    - Con el **asistente de IA** (decidido por el PM, §10.2, punto 6): solo a quien escribe desde un número registrado, sin verificación fuerte; a un número desconocido se las da una persona. El modelo no recibe los números ni los titulares. Escribe una marca y el servidor de Iris la reemplaza por el texto fijo al enviar, igual que ya hace con el saldo (`conversaciones.md` §17.9). Así el número no viaja al proveedor de IA y el modelo no puede cambiar un dígito.
- **Dónde queda el número de cuenta.** En la instantánea, en el mensaje que se le envió al cliente (es parte de la conversación) y en la lectura de Argos. No va en ningún evento de Iris ni en su auditoría: ahí va solo `cuenta_bancaria_id`, como hoy en la boleta. El webhook `cuenta_bancaria.cambiada` tampoco lo trae.
- **Si Argos no responde.** Para ofrecer cuentas, Iris usa la instantánea marcada `stale` (§0) si tiene menos de 24 h; con más, no ofrece cuentas y pasa a una persona. Para registrar la boleta, la relectura es obligatoria: sin Argos, la boleta queda en cola como hoy.

- **Bienvenida (0.9.0, §12.5):** las cuentas de la plantilla `bienvenida_servicio` salen de esta misma instantánea, con las mismas reglas que al ofrecerlas en una conversación: solo activas, de la empresa del servicio, en `GTQ` y con `titular`. Argos nunca las manda como variables, y ninguna persona las escribe a mano. Una cuenta creada o editada (`cuenta_bancaria.cambiada`) no entra en la bienvenida durante 24 h o hasta que la confirme un admin de Iris.

## 8. Fase 2 (sin forma todavía)

- **Cambios en el equipo del cliente:** pasan a §12 (0.9.0). El cambio de Wi-Fi que el cliente hace desde el portal de Argos es solo de Argos y no forma parte de este contrato.
- **Asistente de voz:** llamadas desde Issabel PBX o desde WhatsApp. Usa §7 tal cual; no agrega llamadas a Argos.

## 9. Cambios de Argos a la propuesta de Iris (0.6.0)

Argos revisó la propuesta de Iris contra lo que ya tiene. Iris copia esta versión tal cual; este apartado dice qué cambió y por qué.

### 9.1 Nombres

| Propuesta de Iris | Argos | Por qué |
| --- | --- | --- |
| `POST /pagos-reportados`, «pago reportado», `pago_reportado_id` | `POST /comprobantes`, «comprobante», `comprobante_id` | Argos ya tiene comprobantes de depósito con cola de caja, del portal y del panel |
| Permiso `pagos.reportar` | `comprobantes.registrar` (+ `comprobantes.ver`, `cuentas_bancarias.ver`) | Permiso = nombre de la acción que ya existe |
| `409 boleta_duplicada` | `409 referencia_duplicada` | Error que ya existe; además bloquea mientras otro comprobante con la misma referencia está pendiente |
| Webhook `pago_reportado.resuelto` con `estado` | `comprobante.aprobado` / `comprobante.rechazado` | Eventos que ya están en el catálogo de Argos |
| `estado: falta_dato` | `comprobante.rechazado` con `causa = falta_dato` | Sin un tercer estado: la referencia queda libre y el cliente lo vuelve a mandar |
| `reconectado` (bool) | `servicio_estado` | Dice el estado real después del pago |
| `POST /servicios/:id/reconexion-temporal { pago_reportado_id }` | `POST /comprobantes/:id/reconexion-temporal { servicio_id? }` | La reconexión nace de un comprobante; en Argos es una promesa ligada a él |
| Permiso `servicios.reconexion_temporal` | `comprobantes.reconexion_temporal` | Nombre de la acción |
| `servicio.reconexion_temporal_revertida` / `_vencida` | `promesa.cancelada` con `tipo = reconexion_temporal` y `motivo` = `comprobante_rechazado` / `vencida_sin_revisar` | Es una promesa; un solo juez del estado |
| `GET /clientes/:id/promesas/politica?servicio_id=` | `POST /promesas/simular { servicio_id, vence_en? }` | Ya existe, con las mismas reglas que crear |
| `max_por_mes`, `usadas_este_mes`, `saldo_vencido`, `max_dias` | `max_por_periodo`, `en_periodo`, `deuda_vencida`, `plazo_max_dias` / `fecha_maxima` | Campos del simulador; el cupo es por periodo de facturación |
| `POST /promesas-pago { cliente_id, servicio_id, fecha_compromiso, monto?, canal }` | `POST /promesas { servicio_id, vence_en }` | Ya existe; la promesa es por servicio y cubre todo lo vencido; el canal sale de `X-Iris-Actor` |
| `201 { estado: "aceptada" }` / `422 promesa_fuera_de_politica` | `201` con `estado` = `activa` \| `rechazada` \| `pendiente_aprobacion` | Argos guarda también la rechazada (cuenta para el historial) |
| `promesa_pago.cumplida` / `promesa_pago.incumplida` | `promesa.creada` / `.cumplida` / `.incumplida` / `.cancelada` | Eventos del catálogo; `promesa.cancelada` es nuevo |
| `metodo: dpi_ultimos4 \| nit_ultimos4 \| codigo_cliente` | `documento` (completo) \| `nombre` \| `codigo_cliente` | Decisión del PM: DPI/NIT completo, nombre o código. Argos guarda un solo documento por cliente (NIT o CUI) |
| Permiso `clientes.buscar_por_telefono` | (ninguno) `clientes.ver` | Es un filtro de una lista, no una acción |
| `Idempotency-Key = argos:aviso:<tipo>:<id de Argos>` | `argos:aviso:<plantilla>:<envio_id>` | El id único de Argos es el del envío |

### 9.2 Decisiones del PM (5 nov 2026)

1. Reconexión temporal: limitada y apagada por defecto, con los valores de §7.3.
2. Comprobantes y promesas que nacen en Iris: solo Iris confirma al cliente en la conversación; Argos no manda otro aviso.
3. Identidad: con el DPI/NIT completo, el nombre o el código de cliente (§7.5); `nit_cui` no se entrega al usuario técnico Iris.
4. Argos adopta la mensajería de Iris como su canal WhatsApp (A21); correo y Telegram siguen en Argos.

### 9.3 Semántica

1. **Un comprobante no es un pago** (§7.1): no toca el libro ni el estado hasta que caja lo aprueba. Desde 0.7.0 también puede aprobarlo desde Iris un usuario de Argos vinculado (§7.7); sigue decidiendo Argos.
2. **La reconexión temporal es una promesa** (§7.3): el estado lo sigue decidiendo solo `evaluarEstadoServicio`. Exige un comprobante pendiente que cubra todo lo vencido. Al rechazarse se re-evalúa al instante, pero un corte respeta la hora de corte de la plantilla. Vencida sin revisar no castiga el historial del cliente.
3. **Promesas:** son por servicio, Argos calcula el monto, y `X-Iris-Actor` decide el canal (`ia` u `operador`). Puede quedar `pendiente_aprobacion`.
4. **Identidad:** se verifica con el documento completo, el nombre o el código (PM). Argos deja de entregar `nit_cui` al usuario técnico Iris (§1.1); `nombre` y `codigo_cliente` son pruebas débiles; los intentos se auditan con el valor como `***`.
5. **Teléfono:** `coincide_en[]` distingue al titular del contacto en sitio.
6. **Cabeceras:** `X-Iris-Actor` y `X-Iris-Conversacion` son obligatorias en §7 (`400 cabecera_iris_requerida`). La auditoría de Argos usa canal `ia` o `api`; §0 se corrige (no existe un canal `integracion`).
7. **Archivo:** el comprobante sube como `multipart/form-data` con el archivo, igual que en el portal, en lugar de una URL firmada de 7 días. Argos no descarga nada de Iris (sin riesgo de SSRF) y la copia del archivo queda en Argos desde el primer momento.
8. **Avisos (A21):** Argos adopta la mensajería de Iris solo como su canal WhatsApp; correo y Telegram siguen en Argos. Lo que Iris contestó en la conversación (comprobantes, reconexión y promesas creadas por Iris) Argos no lo repite. `corte_programado` no lo usa Argos. Argos suma las plantillas `factura_vencida` y `servicio_reactivado`, e Iris suma `boleta_recibida`. El registro de permisos de contacto vive solo en Iris.
9. **Webhooks Iris → Argos:** van al receptor `/api/v1/integraciones/iris/webhook` que Argos ya entrega en el canje (activo desde PR #217).
10. **Nuevo pedido B2** a Iris: destino de autologin `conversacion`, para el botón «Ver conversación en Iris» de la cola de caja.

## 10. Decisiones y cierre de la 0.8.0 a la 0.14.0

El líder de Argos respondió los 32 puntos del borrador contra su código (ver «Gobierno») y sus respuestas están en el cuerpo del contrato, cada una en su sección. El PM adoptó las propuestas el 18 nov 2026 (§10.2). Argos implementó todo lo suyo en `argos-isp#249`, publicada en la 1.18.0. Aquí queda solo lo que sigue abierto (§10.1), las decisiones del PM (§10.2) y la migración de Iris (§10.3).

### 10.1 Lo que sigue abierto con Argos

Argos informó que la PR #249 implementa: la reconexión sin personal con su vigencia; las cuentas bancarias con `titular`, `tipo`, el webhook `cuenta_bancaria.cambiada`, las cabeceras de Iris opcionales y `rule: "inactiva"`; `precio_mensual`, `moneda` y `excluir_retirados=true`; la conexión de un paso con `422 webhook_invalido`; el encendido del conector al confirmar el canje y `PUT /integraciones/iris/sincronizacion`. Con eso se cierra lo que §10.1 tenía sobre `excluir_retirados=true` (el PM eligió «solo activos», §10.2, punto 9). Sigue abierto:

1. **S5 y la vía manual (§1.5).** Argos recomendó la opción (a): S5 vale «en la conexión de un paso» y la vía manual del panel de Argos (un `atk_` que se muestra una vez y pegar un `iris_pat_`) queda como recuperación, solo `admin`, auditada y rotulada así. El texto ya la escribe así. Falta que el líder de Iris lo confirme y decida lo mismo para la configuración manual del conector del lado de Iris.
2. **Revisar la PR #249 contra el contrato.** El aviso de Argos no nombra por separado: el filtro `motivo_reconexion` en `GET /promesas` y en el panel, la descripción de `hasta` en el OpenAPI (A25), la pantalla de estado y el texto «Iris copiará…» en el panel de Argos, el rótulo «vía de recuperación» (A28, puntos 5 y 6), ni la frase vieja de su `eventos.md` que dice que `motivo_reconexion` no se reenvía a Iris. Se comprueba al revisar la PR.
3. **El espejo.** Hecho: la 0.8.0 está espejada en `argos-isp` (§12 de `CLAUDE.md`) y publicada en Argos 1.18.0, que responde `contrato: "0.8.0"`. La 0.8.1 se espeja con `iris#222` y `argos-isp#258` (§4).

Todo lo demás que el contrato marca como «0.8.0» está implementado en Argos (A25–A28, PR #249) y en Iris (B4–B7, Iris 0.33.0).

### 10.2 Decidido por el PM (18 nov 2026)

El PM respondió «realiza las propuestas»: lo que estaba propuesto queda adoptado. Ya estaba decidido que la 0.8.0 sale en una sola versión con los cuatro bloques (no hay 0.7.2). Donde no había una propuesta clara, queda un **valor inicial del líder de Iris, que el PM puede cambiar sin tocar el contrato**.

1. **Criterio de `sin_personal` (§7.3).** Iris la pide solo dentro del horario hábil de la empresa y si ningún usuario con rol `caja` de esa empresa está disponible. Valores iniciales del líder de Iris: sin espera de minutos, y si la empresa no tiene a nadie con rol `caja`, no se pide nunca. Siempre con verificación fuerte y apagado de fábrica (punto 2). Argos no objeta el criterio y lo acepta con sus barreras de siempre.
2. **Quién enciende `sin_personal`.** Apagado de fábrica en Iris, con un interruptor aparte del de la reconexión fuera de horario. Lo enciende un admin por empresa.
3. **Vigencia (§7.3).** Valor inicial del líder de Iris: cada empresa la elige en la plantilla de Argos; por defecto, `horas`.
4. **Quién ve la IP y los datos de red (§2.4).** `admin`, `supervisor`, `soporte` y `tecnico` (este solo en sus órdenes). No los ven `despacho`, `ventas`, `caja` ni `lectura`.
5. **Quién ve la dirección y la ubicación (§2.4).** `admin`, `supervisor`, `despacho`, `soporte`, `ventas` y `tecnico` (solo en sus órdenes). `caja` y `lectura` dejan de verlas.
6. **El asistente y las cuentas bancarias (§7.8).** Basta el número registrado: son las cuentas de la empresa, no un dato del cliente. A un número desconocido se las da una persona.
7. **El asistente y el precio (§2.4).** Puede decir el precio mensual con número registrado, sin verificación fuerte. Dice «su plan cuesta Q250.00 al mes», nunca el total de la factura.
8. **Cómo distingue el asistente los servicios (§2.4).** «Servicio 1 · plan · zona», sin dirección. Desde 0.10.0, `<codigo> · <plan> · <zona>` con el código de A45 (§10.5, ajuste ag).
9. **Primera sincronización (§2.5).** Solo los clientes que tienen un servicio no retirado, con `excluir_retirados=true`, que Argos ya implementó (PR #249). El resto llega a demanda al buscarlo.
10. **Activar después de conectar (§1.5).** Se mantiene: el conector de Iris queda apagado hasta que un admin pulsa «Activar». El de Argos, con el botón, se enciende solo al confirmarse el canje.
11. **Textos con números de cuenta que ya existen (§10.3).** Valor inicial del líder de Iris: Iris solo avisa al admin para que los borre a mano; no bloquea guardar.
12. **Qué dice el asistente sobre la reconexión temporal (§7.3).** Queda el texto propuesto: «la reconexión temporal vale hasta la medianoche del miércoles 18 (00:00 del jueves 19); su comprobante queda pendiente de revisión». Nunca «se corta a medianoche».
13. **Cuenta desactivada después del depósito (§7.8).** Valor inicial del líder de Iris: queda así por ahora. Argos rechaza la boleta y caja la registra a mano.
14. **Cuentas sin titular (§7.8).** Valor inicial del líder de Iris: el admin de cada instalación escribe el titular en Argos. Mientras falte, Iris no ofrece esa cuenta al cliente.
15. **Certificado de Argos (§1.5).** Valor inicial del líder de Iris: se mantiene el aviso al admin de Iris cuando el certificado cambia, sin detener nada.

### 10.3 A migrar en Iris: cuentas bancarias propias (§7.8)

Revisión del repositorio de Iris (18 nov 2026). **Iris no tiene hoy una tabla ni una pantalla de cuentas bancarias propias:** las lee en vivo de Argos cada vez. Para cumplir §7.8 hay que cambiar esto.

**Arreglo de Iris fuera de la 0.8.0 (PR #203 de Iris).** Argos confirmó que la cuenta nunca trajo `nombre` ni un objeto `banco`: Iris lee campos que no existen. Además, la respuesta es un arreglo plano y `limit` no existe. Corregir el cliente para que lea un arreglo con `alias` y `banco_nombre` no espera a la 0.8.0 ni cambia el contrato; es la primera parte de las filas 1 y 3.

| # | Dónde | Qué hay hoy | Qué cambia |
| --- | --- | --- | --- |
| 1 | `apps/api/internal/integracion/argos/conversaciones.go:145-169` | `CuentaBancariaArgos` y `CuentasBancarias`: lectura en vivo de `GET /cuentas-bancarias?activa=true&limit=100`, sin instantánea ni `sincronizado_en`. Manda `limit=100`, que Argos ignora (la lista no pagina). Lee `nombre` y `banco`, que **no existen** en Argos (confirmado). No lee `alias`, `banco_nombre`, `titular`, `tipo` ni `activa` | Primero, el arreglo fuera de la 0.8.0: leer un arreglo con `alias` y `banco_nombre`. Después: llena la instantánea con los campos de §7.8 y maneja `rule: "inactiva"` |
| 2 | `apps/api/internal/mensajeria/conv_cuentas.go:12-13` y `:62-85` | El comentario dice «No se guardan en Iris». `CuentasBancarias` filtra por empresa y `GTQ` en memoria, sobre la lectura en vivo. Deja pasar la cuenta si la empresa activa es 0 o si la cuenta no trae `empresa_id` (`:79`) | Lee de la instantánea; solo activas de la empresa de la conversación, sin excepciones |
| 3 | `apps/api/internal/mensajeria/conv_cuentas.go:16-22` | `CuentaBancaria` de la API de Iris: `id`, `nombre`, `banco`, `numero`, `moneda` | Suma `titular`, `tipo` y `alias`; cambian el OpenAPI y `packages/contratos` |
| 4 | `apps/api/internal/mensajeria/conv_rutas.go:317-319` | `GET /mensajeria/conversaciones/cuentas-bancarias`, con el permiso de registrar boleta | Sigue; responde de la instantánea |
| 5 | `apps/api/internal/mensajeria/asistente_herramientas.go:557-589` | Herramienta `cuentas_para_boleta`: le da al modelo solo el `id` y el `nombre` de cada cuenta, leídos en vivo. Deja pasar cuentas sin `empresa_id` (`:575`) | Lee de la instantánea; el modelo sigue sin recibir números; se suma la marca para ofrecer cuentas (§7.8) |
| 6 | `apps/api/internal/mensajeria/asistente_herramientas.go:621-629` | Al registrar la boleta, valida la cuenta contra otra lectura en vivo de la lista, con la misma excepción de `empresa_id` nulo (`:626`) | Relectura de `GET /cuentas-bancarias/:id`, sin la excepción |
| 7 | `apps/api/internal/mensajeria/conv_acciones.go:279` y `:386` | La boleta que registra una persona guarda `cuentaBancariaId` sin comprobar en Iris que la cuenta sea de la empresa (lo decide Argos al registrar) | La misma relectura antes de mandar el comprobante |
| 8 | `apps/api/migraciones/20261106054851_mensajeria_conversaciones.sql:245` | `mensajeria.boletas.cuenta_bancaria_id`: solo el id de Argos | No cambia: es la referencia correcta |
| 9 | `apps/web/app/(panel)/conversaciones/[id]/boleta/page.tsx:64` y `:187-189` | El formulario de la boleta lee las cuentas y las muestra en un selector | Muestra banco, tipo, alias y titular; se suma el botón «Enviar cuentas» en la conversación |
| 10 | `apps/web/e2e/api-conversaciones.ts:291` | Fixture con una cuenta inventada (`"Banco Industrial"`, `"000-123456-7"`) con la forma vieja | Se tipa con el esquema nuevo de `packages/contratos` |
| 11 | `apps/web/lib/manuales/inicio/generales.ts:326` | Manual de inicio «Dónde pagar»: le pide al ISP **escribir a mano** «los bancos, números de cuenta, agencias y otros medios de pago» | El texto deja de pedir números de cuenta: las cuentas las inserta Iris desde el catálogo |
| 12 | `apps/api/migraciones/20261106064327_mensajeria_horarios_manuales.sql:43` (`mensajeria.manuales.cuerpo`) | Texto libre de hasta 50 000 caracteres: puede tener números de cuenta escritos a mano, y los manuales «Para el cliente» viajan al asistente | Revisar los que existan en cada instalación: Iris solo avisa al admin para que los borre a mano, no bloquea (§10.2, punto 11) |
| 13 | `apps/api/migraciones/20261106054851_mensajeria_conversaciones.sql:64` (`mensajeria.respuestas_rapidas.texto`) | Texto libre de hasta 4000 caracteres: es donde un agente guardaría «nuestras cuentas son…» | Igual que el 12; el botón «Enviar cuentas» lo reemplaza |
| 14 | `apps/api/migraciones/20261106064327_mensajeria_horarios_manuales.sql:26-29` (`mensajeria.canales.fuera_horario`, clave `mensaje`) | Texto libre de la respuesta de fuera de horario | Igual que el 12 |

No se encontraron cuentas ni números de cuenta en los ajustes de conversaciones (`mensajeria.ajustes_bandeja`), en los ajustes del asistente (`mensajeria.asistente_ajustes`, que no tiene un campo de instrucciones libres) ni en las plantillas de aviso. Los puntos 12 a 14 son campos de texto libre: el repositorio no trae números reales, pero cada instalación puede tenerlos en su base de datos.

### 10.4 Decidido por el PM para la 0.9.0 (21 nov 2026)

La 0.9.0 nació de un pedido del PM por el líder de Argos. Iris la propuso, Argos la revisó contra su código (1.23.0) y la aprobó sin objeciones con los ajustes que el texto ya incluye, y el PM decidió:

1. **Solicitar servicio** desde el portal de Argos (alta, cambio de plan, traslado u otro) es seguimiento comercial: lo revisa un asesor de ventas en Iris (prospecto), cotiza y lo convierte en orden (§11.7).
2. **Cliente identificado solo por su IP:** abre tickets y ve la lista; nada más (§11.2). Para pedir o hacer un cambio de Wi-Fi desde su portal, Argos le pide volver a confirmar su contraseña o su identidad aunque ya tenga sesión.
3. **El cambio de Wi-Fi que hace el cliente desde el portal** es solo de Argos y queda fuera de este contrato.
4. **Los equipos del cliente son de Argos:** SmartOLT (FTTH) y el ACS (TR-069). Iris nunca guarda sus credenciales y todo lo pide a Argos (§12.1).
5. **Activación:** la pide el técnico desde la orden o un agente desde el panel de Iris, para FTTH y wireless (CPE o radio, TR-069 si lo soporta; sin gestión, solo IP y activación). Es configurable por empresa o plantilla: al cerrar (como hoy), exigir la activación lista (salvo un supervisor con motivo) o cerrar con «activación pendiente por agente» (§12.2).
6. **Modo de fábrica de la activación: al cerrar**, como hoy.
7. **Bienvenida al activar:** Argos la dispara con los datos del servicio y las cuentas para pagar; Iris la sugiere y la valida en la orden, con «Reenviar», sin bloquear el cierre salvo que la empresa lo configure (§12.5).
8. **Asistente de IA:** puede consultar el equipo y cambiar el Wi-Fi solo si la empresa lo habilita, con los permisos separados `servicios.ver_equipo` y `servicios.cambiar_wifi` asignables a su rol, y con el cliente verificado en esa conversación al nivel del portal de Argos (código al teléfono o correo registrados, o datos del titular; menos de 10 min). La clave nunca queda en logs, eventos, auditoría ni historial del chat (§12.4).
9. **Cobro:** el mensual empieza con el servicio activo; la instalación se cobra como factura única en Argos (`POST /facturas/cargo-unico`, que ya existe), que registra un agente o sale de un formulario de la orden, con seguimiento por los webhooks de A36 (§3).
10. **Valores iniciales del líder de Iris**, que el PM puede cambiar sin tocar el contrato: el portal de Argos es el único que ve el cliente y `/soporte` de Iris queda de respaldo (§11.9); sin firma extra por petición de Argos (el control es el token, la ruta por cliente, los permisos `portal_argos.*`, los topes y la auditoría); hasta 3 adjuntos de 10 MB por mensaje; el traslado exige contrato firmado y el cambio de plan solo la cotización aceptada (cada empresa puede exigir contrato); cambio de plan y traslado se aplican a mano en Argos hasta A34; el cliente no ve la cotización en su portal; Argos avisa al cliente con un solo aviso por cliente en una falla general; exigir señal antes de cerrar lo elige cada empresa en la plantilla (apagado); el técnico puede escribir el Wi-Fi inicial y la clave no queda guardada en Iris después de enviarla.

### 10.5 Decidido por el PM para la 0.10.0 (21 nov 2026) y ajustes de Iris

Contexto del PM: el cliente no siempre da todos sus datos al inicio, y en la instalación puede cambiar el titular (un menor pide el servicio y queda de titular un adulto) y el plan.

1. **Titular:** lo corrige el técnico en la orden, con foto del documento de identidad y la firma del nuevo titular; queda auditado y ventas lo ve después; sin aprobación previa.
2. **Plan:** lo cambia el técnico, solo entre los planes permitidos (mismo modo residencial o empresarial y asignados a todos los routers o al router del servicio); ventas recibe aviso y la cotización de Iris se actualiza sola.
3. **IP:** se asigna al activar, no al ganar; un servicio `pendiente_instalacion` no reserva IP.
4. **Paso «Confirmar titular y plan»** (propuesta de Argos, aceptada): «¿Quién firma?» (la misma persona de la venta u otra, que se busca o crea por documento); datos obligatorios del titular para activar (documento DPI o pasaporte, o NIT y representante con DPI en empresarial; número validado; nombre como en el documento; casilla «mayor de edad» sin fecha de nacimiento; teléfono con WhatsApp; correo opcional; facturación CF o NIT; relación opcional); lugar con dirección, referencia, GPS con precisión y foto de fachada opcional; plan de la cotización por defecto y motivo obligatorio si cambia; foto del DPI (frente obligatorio), «Coincide con el titular» y firma en el contrato (§3.4.2).
5. **La foto del documento es del cliente y vive en Argos** (A41): cifrada, visible solo con `clientes.ver_identidad` y auditada, borrada 12 meses después de retirado el cliente. Iris solo la tiene en tránsito (§3.4.8).
6. **Contactos (A42):** Argos es el dueño. Un cliente tiene varios contactos con su relación, servicios y permisos (todos encendidos por defecto); lo reservado al titular (cambiar titular, cambiar plan, Wi-Fi, traspaso y baja) siempre con la identidad de A41; los gestionan el personal y el titular desde su portal; los avisos de cobro también salen a los contactos con `avisos`.

7. **Contactos creados desde Iris:** «todo desde el inicio»: aunque el titular no esté verificado en esa conversación, tienen los mismos permisos por defecto que cualquier otro contacto (`consultar`, `reportar`, `pagar`, `avisos`); el titular recibe un aviso con la opción de quitarlo. Siguen valiendo: el identificador de una llamada nunca basta, nunca puede ser titular y sus números no cuentan como «registrados».
8. **Prospectos desde Argos (A44):** una persona de Argos registra prospectos en Iris, que los atiende ventas.
9. **Titular que ya es cliente:** cuando el cambio de titular cae en un cliente existente (coinciden documento y nombre normalizado, con la identidad de A41 tomada en sitio y el contrato firmado a su nombre), Argos le asigna el servicio al momento (`existente: true`) y solo le manda un aviso informativo que no bloquea nada. Se quitan la bandera `titular_por_confirmar`, el código de confirmación, el `409 titular_por_confirmar` y las revisiones de N2; siguen `422 titular_no_coincide` con revisión de un supervisor, los límites de 2 por servicio y 10 por técnico al día, el contador aparte de N9 y N1 completo. **Riesgo aceptado:** un técnico que tenga el DPI físico, la firma y el nombre de un tercero podría asignarle un servicio. Lo mitigan la foto del DPI guardada en Argos, el aviso al titular, los límites, la auditoría con `X-Iris-Actor` y un **reporte en Argos** de los servicios con `existente: true` que llegan por Iris, para que caja o supervisión los revise.
10. **Código y nombre del servicio (A45):** cada servicio tiene un código automático, legible e inmutable por empresa (`S-000412`) y un nombre opcional que elige el cliente o el personal («Casa», «Negocio»). Iris lo muestra como «S-000412 · Casa» y es el «número de servicio» del chat; los límites de intentos impiden adivinarlo y acertar no verifica.
11. **Las promesas y la consulta son por servicio:** `pagar` y `consultar` de un contacto valen solo para sus `servicios[]` (con `null`, todos); toda promesa o comprobante de un contacto lleva `servicio_id` y se evalúa con la plantilla de ese servicio; con varios servicios, el chat pregunta mostrando solo el código y el nombre de los suyos.
12. **Un cliente a cargo del servicio de otro (0.10.1, A46):** «alguien más puede estar a cargo de mi servicio, y viceversa». Un contacto se vincula con otro cliente de la misma empresa; esa persona ve por separado sus servicios y los que tiene a cargo, con su propia verificación y solo con los permisos del contacto, nunca con lo reservado al titular.

**Ajustes de Iris a la propuesta de Argos** (ninguno cambia una decisión del PM; **todos aceptados por Argos**, a, c, e y f sin cambios):

- **a. Reservar la IP en sitio (A38).** Hoy el técnico configura el CPE con la IP del pendiente (`contratos/instalacion.md` §1 de Argos). Sin IP hasta activar, una conexión con IP fija o amarre no se podría configurar antes de cerrar en el modo de fábrica (activar al cerrar, §10.4 punto 6). `reservar-ip` la asigna en la instalación, nunca al ganar. Aceptado por Argos.
- **b. Cambio de titular sin `cliente_id` de Iris (A39; acordado con Argos).** El técnico no puede ver ni elegir otros clientes: decide Argos. Usa un cliente existente solo si coinciden el documento **y** el nombre normalizado, sin devolverle a Iris más que su id, y le avisa; si coincide el documento y no el nombre, `422 titular_no_coincide` y revisión de un supervisor. Así un número de documento escrito no basta para pasarle un servicio (y su cobro) a otra persona.
- **c. `de_cliente_eliminado` en el evento (A39).** Sin él, Iris no sabe que el cliente anterior ya no existe. Aceptado por Argos.
- **d. `completar-datos` en lugar de `PUT /clientes/:id` (A43).** El token de Iris no debe editar cualquier cliente: solo llena lo que falta del titular del servicio pendiente (y corrige el nombre con la identidad registrada).
- **e. Contrato firmado por el nuevo titular (B20).** La firma que pide el PM es la del contrato regenerado a su nombre: el que firmó el menor no sirve. Aceptado por Argos.
- **f. Cobro después del titular (B21).** La factura de la instalación sale a nombre del titular final; las ya emitidas no se mueven. Aceptado por Argos.
- **g. Varios clientes con el mismo número (A42, B22).** El chat pregunta de cuál se trata sin mostrar datos de ninguno: solo pide una referencia que el cliente conoce (número de servicio o la dirección que él escribe). Nunca lista nombres ni direcciones.
- **h. `buscar` acotado (A42).** No devuelve otros números ni documentos, y se limita por tasa.
- **i. Contactos desde Iris (A42).** Un contacto creado desde Iris nunca puede ser titular ni recibir permisos por encima de los del agente.
- **j. Clientes con historial (A41, A43).** A43 no cambia `telefono`, `movil`, `email` ni `nombre` de un cliente con otros servicios activos o con facturas o pagos (`422 cliente_con_historial`); A41 sobre ese cliente exige documento y nombre iguales y nunca reemplaza `nit_cui`; un teléfono o correo agregado por A43 no cuenta como registrado hasta que el titular lo confirme. Aceptado por Argos.
- **k. Contactos titulares y números registrados (A42).** Desde Iris, todo `PUT` o `DELETE` sobre un contacto titular responde `422 contacto_titular_no_permitido`; agregar o cambiar un número avisa al titular (enmascarado); los «registrados» de §7.5 y §12.4 son solo los del cliente. Aceptado por Argos.
- **l. Límites del cambio de titular (A39).** 2 cambios por servicio y 10 por empresa y técnico al día; `titular_no_coincide` con su propio contador. La confirmación del titular existente que pedía este ajuste la reemplaza la decisión 9 del PM. Aceptado por Argos.
- **m. Alcance de §3.4.** Solo servicios de Iris (`403 servicio_no_de_iris`) y nunca con el asistente de IA (`403 actor_ia_no_permitido`, también en `contactos.gestionar`). Aceptado por Argos.
- **n. Revisión desde el panel.** `metodo: revision_panel` con supervisor, motivo y la foto tomada en sitio; `ordenes.validar_identidad` solo activa; nadie ve la foto en tránsito. Aceptado por Argos.
- **o. Borrado de la foto.** En Iris, al `2xx` o a los 7 días de tomada; en el teléfono, cifrada con llave no exportable y borrada al cerrar o revocar la sesión; en Argos, solo vigente y anterior, y en el acto si el cliente se elimina. Aceptado por Argos.
- **p. Datos personales en pendientes y errores.** Fotos y documento solo como referencia, nunca en `payload`; `***` en eventos y panel; `detail` y `errors[]` sin el valor recibido. Aceptado por Argos.
- **q. Identificar no es verificar (A42).** WhatsApp desde el número del contacto basta para consultar, reportar y pagar; una llamada pide código por WhatsApp; lo reservado sigue con §7.5. Aceptado por Argos.
- **r. Idempotencia y versiones.** `version` opcional en §3.4; huella de multipart por partes sin boundary; claves `iris:identidad:<orden_id>:<toma>` e `iris:reservar_ip:<orden_id>:<router_id>`. Aceptado por Argos.
- **s. Hallazgos bajos.** `motivo_codigo` en `servicio.titular_cambiado` (texto solo en la auditoría de Argos); `buscar` y `GET /clientes/:id/contactos` con `X-Iris-Actor` y 20 por minuto por actor; 3 intentos al elegir cliente; el técnico no ve `posible_duplicado`; la reserva de IP vence a los 30 días; la firma en sitio del contrato regenerado cubre la subida de precio. Aceptado por Argos.
- **t. `nit_cui` y clientes con historial (N1, con la aclaración de Argos).** «Facturar a» es `nit_cui` + `tipo_documento_fiscal` (no hay `facturacion.nit`): A43 los llena en un cliente sin historial y nunca los toca en uno con historial (`422 cliente_con_historial`); A41 nunca escribe `nit_cui`; en un cliente con historial sin documento, la identidad no vale como `documento` (§7.5) ni `datos_titular` (§12.4) hasta que el titular la confirme con un código.
- **u. Titular existente sin confirmación (N2).** Reemplazado por la decisión 9 del PM: sin bandera, sin código y sin revisión a las 24 h.
- **v. Alcance de §3.4 por servicio (N3).** El `origen_id` debe ser de una orden ya registrada para ese mismo servicio.
- **w. Errores de §3.6 (N4).** `403 empresa_no_permitida`, `409 argos_apagado` y `403` con el `token_portal`.
- **x. Duplicados y respuesta de §3.6 (N5).** `409 prospecto_duplicado { url_iris }` que suma actividad al existente, sin decir qué número coincidió; `asignado` en lugar de `vendedor_id`; el límite cuenta números: 30 por minuto por actor.
- **y. `url_iris` plano (N6).** El ticket de autologin se pide al hacer clic (§1.3).
- **z. Idempotencia de §3.6 (N7).** Huella con `X-Iris-Empresa` y `X-Iris-Actor`, clave guardada 30 días, `409 idempotencia_en_proceso`.
- **aa. `cliente_id` sin Argos (N8).** `cliente_validado: false` y conciliación después.
- **ab. Contador de `titular_no_coincide` (N9).** Aparte de §7.5: solo bloquea nuevos cambios de titular hacia ese documento.
- **ac. Elegir entre clientes (N10).** 3 intentos por conversación y 5 por número al día.
- **ad. Ganar con un cliente existente (P1).** Iris no crea ni edita el cliente: el principal y los números sin nombre no se escriben en él; los números con nombre van como contactos (A42) y los demás quedan en el prospecto.
- **ae. Números del prospecto (P2).** El principal sin `relacion` o con `titular`, y `titular` solo en el principal (`422 telefonos_invalidos`); `buscar` marca los números adicionales con `adicional: true` y Iris los trata como contacto no verificado; en el chat, un número que no es el principal no ve cotización, dirección ni plan sin un código al principal.
- **af. Corrección de edición.** En §3.4.7, «Dónde» de `cliente_sin_instalacion` (A41) y de `evidencia_requerida` (A39), que un reemplazo de `cf9d1b2` había cambiado.
- **ag. Consulta y pago por servicio, y etiqueta con código (M-a, M-b).** `estado-cuenta?servicio_id=` sin el desglose de otros servicios; Iris solo usa esa variante con contactos acotados (sin ella, solo el estado); `X-Iris-Contacto` en promesas y comprobantes y `403 servicio_fuera_del_contacto`; `codigo` en la lista blanca (también para el asistente) y `nombre` sin ir al proveedor de IA ni a los logs; etiqueta corta `<codigo> · <plan> · <zona>`, que cambia la del punto 8 de §10.2.
- **ah. Código en el chat.** Se compara solo contra los clientes que comparten el número.
- **ai. `nit_cui` de `POST /clientes`.** No vale como `documento` de §7.5 hasta que coincida con una identidad A41; Argos guarda su origen.
- **aj. Reporte de titulares existentes.** Argos lista los servicios con `existente: true` que llegan por Iris para que caja o supervisión los revise.
- **ak. `X-Iris-Contacto` obligatoria (0.10.1, M1).** En las rutas de §7 con `X-Iris-Conversacion`, `<contacto_id>` o `titular` (`400 cabecera_iris_requerida` si falta); con `titular`, verificación de §7.5 registrada o auditada como tal. Aceptado por Argos.
- **al. Permiso del contacto en Argos (0.10.1, M2).** `403 permiso_de_contacto` sin `pagar` o `consultar`; `X-Iris-Contacto` en la auditoría y en la huella de idempotencia de promesas y comprobantes. Aceptado por Argos.
- **am. Revalidar el vínculo (0.10.1, B1).** Al cambiar los números del contacto o del cliente vinculado; si ya no coinciden, se rompe y se avisa a los dos titulares. Aceptado por Argos.
- **an. «Verificado» (0.10.1, B2).** Confirmado con un código en los últimos 12 meses. Aceptado por Argos.
- **ao. Caja con servicios a cargo (0.10.1, B3).** Cuentas a cargo plegadas bajo «Cuentas a su cargo · verificar documento»; se abren con «Verifiqué el DPI de José», auditado y solo para esa visita; con `pagar` sin `consultar`, el cajero escribe el monto sin ver saldo ni facturas. Aceptado por Argos.
- **ap. Aviso a José (0.10.1, B4).** Solo el `codigo` del servicio, nunca su `nombre`. Aceptado por Argos.

**Valor inicial del líder de Iris** (cambiable sin tocar el contrato): un cambio de plan que sube el precio también pide la firma del titular sobre la cotización nueva, apagado de fábrica (lo enciende la empresa).

### 10.6 Decidido por el PM para la 0.11.0 (21 nov 2026)

1. **Bodegas:** la cuadrilla tiene una bodega compartida (lo que va en el vehículo) y cada técnico puede tener además la suya personal.
2. **Orden de descuento:** al instalar se descuenta primero de la bodega de la cuadrilla.
3. **Responsabilidad:** el jefe de cuadrilla recibe y firma los traslados y responde por los faltantes. Cualquier miembro puede usar el material, y queda registrado quién lo usó.

**Ajustes de la revisión de seguridad de la 0.11.0** (acordados con Argos):

- **a. Bodega validada antes del stock (A1).** Con `bodega_id`, solo la personal o una cuadrilla de la que el técnico es miembro, en la empresa del servicio; `403 no_es_miembro` antes de mirar existencias. En comodatos y devoluciones, la misma regla sobre la bodega donde está la unidad (`409 unidad_en_otra_bodega`). Iris también valida `bodega_id` al recibir el lote.
- **b. Actor obligatorio (M1).** `X-Iris-Actor` obligatoria sobre bodegas de cuadrilla (`400 cabecera_iris_requerida`), miembro y con el mismo `id_externo` que `tecnico` (`403 actor_no_coincide`); `tecnico` solo por `id_externo`.
- **c. Miembros aceptables (M2).** Solo `id_externo` activos, técnicos de la empresa de la cuadrilla y dentro del alcance; los demás, `miembro_rechazado`. `empresa_id` obligatorio en `cuadrilla.*`; una bodega por empresa.
- **d. Jefe (M3).** Debe ser miembro con I1 (`422 jefe_no_miembro` en Iris; ignorado en Argos); `cuadrillas.asignar_jefe` solo para admin y supervisor, sin autoasignación; sin jefe, los traslados esperan.
- **e. Jefe al recibir (M4).** El jefe en `recibido_en` según el historial, aún miembro al procesar, y el mismo en `recibido_por` y `X-Iris-Actor`; si no, `403 solo_jefe_recibe`.
- **f. Eventos completos (M5).** `cuadrilla.actualizada` con `miembros[]` completo de cada cuadrilla afectada por cambios del técnico, del vínculo I1 o del usuario.
- **g. Quitar un miembro (M6).** Corte inmediato en Iris; Argos evalúa la membresía en `ocurrido_en` (`fuera_de_membresia`); `403 no_es_miembro` es conflicto, sin cambiar de bodega.
- **h. Orden por `version` (B1).** Entera y creciente por cuadrilla; `GET /integraciones/argosisp/cuadrillas/:id` para conciliar.
- **l. Una empresa por evento.** Cada `cuadrilla.*` lleva un solo `empresa_id`; Iris emite uno por empresa con los miembros vinculados por I1 en ella, y Argos mantiene una bodega por empresa.
- **m. Permiso de conciliación.** `cuadrillas.leer_integracion` del rol `integracion_argos`; solo en las empresas del alcance del conector, fuera `404`.
- **i. Comodatos sin `bodega_id` (B2).**
- **j. `sin_existencia` acotado (B3).** Solo las bodegas accesibles y las cantidades de las líneas pedidas.
- **k. Recepción rechazada en Iris (B4).** `403` en el servidor antes de encolar si no es el jefe.

### 10.7 Decidido por el PM para la 0.12.0 (21 nov 2026)

1. **Misma consistencia de equipos que Argos:** Argos es el dueño de las unidades y de su estado; Iris pide los movimientos y los muestra.
2. **Cambio de equipo en una sola operación:** sale una unidad y entra otra en la misma transacción; la que sale no se da de baja en el momento.
3. **El técnico no decide cobros ni pérdidas:** propone un cobro sin monto y registra los intentos de recuperación; aprobar el cobro, declarar una unidad no recuperada y cobrar «Equipo no devuelto» lo decide una persona en Argos.
4. **Revisión y garantía** las resuelve el personal de Argos; Iris solo las ve, sin costos, proveedor ni RMA.

**Ajustes de la revisión de seguridad de la 0.12.0** (todos aceptados por Argos):

- **a. Intentos de recuperación (M1).** Foto obligatoria con `se_nego` y `equipo_perdido`; `ubicacion` del intento, exigida en sitio; aviso al titular con la serie enmascarada; para declarar `no_recuperada` o cobrar, todos los intentos a la vista y un mínimo de técnicos distintos (2 de fábrica, configurable) o un contacto con el cliente registrado; 1 intento por unidad al día (`429 demasiados_intentos`). Aceptado por Argos.
- **b. Unidades del cambio (M2).** La que entra `disponible` (`409 estado_unidad_cambiado`); la que sale a la bodega de la que entra; nunca se instala una unidad en `revision`, `garantia`, `dada_de_baja` o `no_recuperada`; `destino_sale: funciona` no vale con `danado` o `garantia`. Aceptado por Argos.
- **c. Ventana de la fecha (M3).** `recibido_en_iris` en cada petición, `recibido_en_iris − 72 h ≤ fecha ≤ recibido_en_iris + 5 min`, membresía de §3.9 con `fecha`, y en Iris `fecha` no anterior a la asignación. Aceptado por Argos.
- **d. Compatibilidad sin pérdidas automáticas (M4).** Con Argos < 0.12.0, el no recuperado espera a una persona con `ordenes.declarar_perdida`; con ≥ 0.12.0, `…/perdida` rechazada al token de Iris y permiso retirado. Aceptado por Argos.
- **e. Leer una unidad (M5).** `servicio` solo del actual y del de la orden; `403 actor_ia_no_permitido`; `X-Iris-Actor` obligatoria; en Iris, limitada a `inventario.ver_unidad` y auditada. Aceptado por Argos.
- **f. Alcance (B1).** Servicio de la orden y orden asignada al actor, validados en Iris. En Argos, el servicio dentro de las empresas del token; `origen_id` registrado y auditado en la primera llamada aceptada (no la regla de §3.4, que dejaba fuera los servicios antiguos creados en Argos). Aceptado por Argos.
- **g. Foto por id opaco (B2).** Argos la abre por autologin con el permiso del usuario; se borra con la retención de la orden. Aceptado por Argos.
- **h. Conciliar sin reenviar (B3).** Con `GET /servicios/:id/equipo`. Aceptado por Argos.
- **i. Notas (B4).** Hasta 500 caracteres, escapadas, nunca en eventos ni logs. Aceptado por Argos.

### 10.8 Acordado con Argos para la 0.12.1 (21 nov 2026)

1. **Clave del consentimiento por número (§6.4):** con `telefono`, `argos:consentimiento:<empresa_id>:<cliente_id>:whatsapp:<h>:<ocurrido_en>`, con `<h>` = 16 primeros hex del HMAC-SHA256 de `"consentimiento:" + número E.164` (ajuste d); el número en claro solo en el cuerpo.
2. **Tickets del portal por contacto (§11.10):** `X-Iris-Contacto` en las rutas de tickets cuando la sesión es de un contacto, `servicio_id` obligatorio, regla aplicada por Iris con su caché de contactos y `creado_por_contacto`.

**Ajustes de la revisión de seguridad de la 0.12.1** (todos aceptados por Argos):

- **a. Cabecera en todo §11 (M1).** `X-Iris-Contacto` con `<contacto_id>` o `titular` en toda ruta de §11 (`400 cabecera_iris_requerida`); con `<contacto_id>`, confirmar solo sus tickets, adjuntos solo donde ve el ticket completo y `403 solo_titular` en solicitudes. Aceptado por Argos.
- **b. Qué ve el contacto (M2).** De los tickets que no abrió, solo número, estado, categoría, `afectado_por_incidente` y fechas; crear, responder y confirmar con `reportar` y solo sobre los suyos (`403 permiso_de_contacto`); nunca tickets sin `servicio_id`. Reemplaza lo que daba `consultar`. Aceptado por Argos.
- **c. Sesión de contacto y caché (M3).** Argos la abre solo con `reportar` o `consultar` sobre el servicio y manda `X-Iris-Contacto-Version` (la `version` en `core.contactos_cliente`, entero positivo; mal formada, `422 validacion`); Iris relee el contacto si su caché es menor o no lo tiene, y responde `503 argos_no_disponible` si Argos no responde. Aceptado por Argos.
- **d. Hash con HMAC (B1).** `<h>` con HMAC-SHA256 de `"consentimiento:" + número E.164` y el secreto del receptor de webhooks de Iris; la `Idempotency-Key` es dato personal, fuera de logs y eventos; si el secreto rota, una clave ya enviada se reintenta con los bytes guardados, sin recalcularla. Aceptado por Argos.
- **e. Topes del contacto (B2).** Propios, además de los del cliente. Aceptado por Argos.
- **f. `servicio_id` que falta (B3).** `422 validacion` (`field: servicio_id`). Aceptado por Argos.
- **g. Compatibilidad (B4).** Con Argos < 0.12.1, el portal nunca abre a un contacto los tickets de otro cliente. Aceptado por Argos.

### 10.9 Decidido por el PM para la 0.13.0 (21 nov 2026)

1. **Caídas repetidas por servicio:** Argos emite el conteo y la ventana; Iris decide si abre un ticket, uno por servicio mientras siga abierto el de esa categoría (si no, una nota). Umbral por empresa en Iris, 3 caídas en 60 min de fábrica.
2. **Falla general:** Argos avisa la apertura y el cierre con solo ids de los servicios afectados; Iris abre un ticket general por incidente que absorbe los tickets por servicio, responde con el aviso de falla, se vincula al maestro de Horus del mismo router y, al cerrarse, avisa a los clientes que habían escrito.
3. **Gestiones de cobro:** Iris registra en Argos las llamadas, mensajes, visitas y promesas con un resultado de lista cerrada, sin texto libre, montos ni teléfonos, solo con el cliente confirmado.
4. **Plantillas de WhatsApp:** se editan y se aprueban solo en Iris; Argos solo las lee.

**Notas de implementación (B23, aceptadas):** la clave de los contactos que nacen de un prospecto es `iris:contacto:<cliente_id>:pt<id del teléfono>`; se crean con todos los permisos (decisión 7, §10.5) y solo sobre el servicio nuevo; el `url_iris` del `409 prospecto_duplicado` incluye el id del prospecto.

**Ajustes de la revisión de seguridad de la 0.13.0** (todos aceptados por Argos):

- **a. Falla general acotada (M1).** Absorbe solo tickets de conectividad de servicios afectados creados después de `desde`; los hijos no se cierran solos (verificar con el cliente, o resueltos si Horus o PPP confirman); reabrir el mismo `alcance_id` antes de 30 min reusa el ticket general; un aviso de cierre por cliente e incidente, máximo 1 cada 6 h, con §6.5; Argos registra quién abrió a mano un incidente y por qué. Aceptado por Argos.
- **b. Caídas durante una falla (M2).** Nota en el ticket general en vez de un ticket nuevo; más de N tickets de caídas en 10 min (20 de fábrica) agrupados en «posible falla general» con aviso a supervisión; al llegar la falla, los abiertos pasan a hijos. Aceptado por Argos.
- **c. Clave y fecha de la gestión (M3).** `iris:gestion:<gestion_id>` (UUID v7 de Iris); `ocurrido_en` entre ahora − 7 días y ahora + 5 min. Aceptado por Argos.
- **d. Empresas en la falla general (M4).** Un evento por empresa con el mismo `incidente_id`; `empresa_id` en el GET y solo ids del alcance; ids desconocidos leídos con `GET /servicios/:id` y descartados si son de otra empresa. Aceptado por Argos.
- **e. Lectura de incidentes (B1).** `404` fuera del alcance, auditada con actor `token`, `limit` hasta 1000. Aceptado por Argos.
- **f. Gestión informativa (B2).** Nunca cambia estados, cortes ni promesas; con el asistente, `pago_informado` y `promete_pagar` solo con promesa o comprobante real; `X-Iris-Contacto` si habló un contacto. Aceptado por Argos.
- **g. Aviso de falla (B3).** Solo a números con cliente, sin servicio ni dirección. Aceptado por Argos.
- **h. Plantillas por empresa (B4).** Solo las que la empresa puede usar. Aceptado por Argos.
- **i. Clave `pt<id>` (B5).** Se queda: el id es el de `comercial.prospecto_telefonos`, único por prospecto. Aceptado por Argos.

### 10.10 Acordado con Argos para la 0.14.0 (21 nov 2026)

Sale de la revisión de seguridad de B26 y B27 en Iris y de tres revisiones de este cambio. Empezó como el parche 0.13.1; con la segunda revisión dejó de ser aditivo y pasó a minor (0.14.0, transición en §4.5). Todo aceptado por Argos, sin choque con A47/A48.

1. **Sin reintento automático.** `429 demasiados_intentos` de `…/no-recuperado` (aunque venga con `Retry-After`) y los `403` de negocio de §3.9, reconocidos por la URN completa (`no_es_miembro`, `actor_no_coincide`, `solo_jefe_recibe`, `empresa_no_permitida`, `actor_ia_no_permitido` y `actor_desconocido`), son definitivos en Argos; Iris los muestra como conflicto a una persona. Los `409`/`422` de §3.10 ya lo eran.
2. **Material desde la app.** Consumos con `tecnico` + `bodega_id` + `X-Iris-Actor`, también de la bodega personal, uno por actor y bodega; comodatos y devoluciones con `tecnico` + `X-Iris-Actor`, sin `bodega_id`. Lo indicado por despacho desde el panel solo usa bodegas `tipo = bodega`.
3. **Autor de cambios e intentos.** En `…/cambiar` y `…/no-recuperado`, `X-Iris-Actor` es quien registró la acción, no el técnico actual de la orden. En Iris los registra quien tenía la orden en la `fecha`, a su nombre o como miembro de su cuadrilla, también si se la quitaron, hasta 12 h después (§3.10).
4. **Validación de ubicación y foto en Iris.** Precisión ≤ 200 m; a 500 m o menos del servicio y de la última posición de los 30 min anteriores (sin posición o sin coordenadas del servicio, `ubicacion_sin_referencia`); foto del mismo técnico, posterior a su llegada, no rechazada ni usada en otro intento o cambio. Argos no revalida distancia ni frescura de la foto: verifica el intento con Iris.

**Ajustes de la primera revisión de seguridad** (a–i, todos aceptados por Argos; corregidos donde la segunda revisión los cambió):

- **a. Evidencia verificable (H1).** Con solo el token de Iris se podían fabricar dos intentos «de técnicos distintos» sin foto. `foto_ref` obligatoria en todo intento; Argos verifica de servidor a servidor, sin la imagen, y solo cuenta los intentos verificados; un aviso al titular por servicio al día; la pantalla de Argos indica que la ubicación y la autoría las afirma Iris. Iris proponía contar solo las fotos abiertas por una persona; Argos lo cambió a una verificación de la foto de servidor a servidor, y después la segunda revisión la llevó al intento completo (R-01). Aceptado por los dos.
- **b. Orden reasignada (H2).** El técnico que dejó la orden solo puede enviar cambios e intentos hasta 12 h después de que le quitaran la orden o lo quitaran de la cuadrilla, lo primero que ocurra, medido con la hora del servidor de Iris (`orden_reasignada`; R-05). Regla de Iris.
- **c. Origen del actor (H3).** `X-Iris-Actor` es el usuario autenticado del lote, nunca un campo de la operación; un cambio de sesión bloquea las operaciones de la anterior. Regla de Iris.
- **d. Claves por operación (H4).** `…:<operacion_id>` e `…:<intento_id>` en las claves de `cambiar` y `no-recuperado`, con `X-Iris-Actor` en la huella, solo con un Argos ≥ 0.14.0 (R-08). Aceptado por Argos.
- **e. Definiciones de la validación (H5).** Los 30 min hacia atrás desde `fecha`, con posiciones recibidas antes de `recibido_en_iris`; la llegada es el último `orden.llegar` del mismo actor ese día (R-07). Regla de Iris.
- **f. 429 de negocio (H6).** Definitivo en `…/no-recuperado`; Iris lo distingue por `type`, aunque venga con `Retry-After` (R-09). Aceptado por Argos.
- **g. Material y actor (H7).** Comodatos y devoluciones siguen sin `bodega_id`; `tecnico` es el I1 del actor y Argos responde `403 actor_no_coincide` también en la bodega personal, con el vínculo que recibe por `tecnico.argos_*` (R-10); despacho solo con bodegas `tipo = bodega`. Aceptado por Argos.
- **h. Versiones (H8).** El kardex de la bodega personal con actor aplica con contrato ≥ 0.14.0. Aceptado por Argos.
- **i. Espejo y pedidos (H9).** PRs enlazadas (`iris#274` · PR enlazada en `argos-isp`) y filas A47, A48, B26 y B27 ampliadas.

**Ajustes de la segunda revisión (R-01 a R-11), aceptados por Argos:**

- **R-01. Verificar el intento, no la foto.** `GET <iris>/api/v1/integraciones/argosisp/intentos-recuperacion/verificacion?empresa_id=&idempotency_key=` reemplaza la ruta por `foto_ref`. `200 { verificado, motivo }` con `motivo` de lista cerrada; `404` igual para una clave desconocida o fuera del alcance; sin imagen, autor ni horas; auditada con actor `token` y con tope por conector. Argos guarda la clave de cada intento y la consulta tal cual (§3.10).
- **R-02. Intento sin verificar.** No cambia la unidad, no avisa al titular ni cuenta para el mínimo: queda como registro con su motivo. El aviso sale solo por intentos verificados, uno por servicio al día.
- **R-03. Envío y reintentos.** Iris envía el intento con la foto `completo`; Argos reintenta con `foto_pendiente`, `5xx`, timeouts y `401`/`403` (con aviso al admin) y vuelve a verificar al abrir la pantalla de declaración.
- **R-04. Solo intentos de ese conector.** La ruta solo verifica intentos que Iris envió a ese conector (precisado en T-3).
- **R-05. El técnico que dejó la orden.** Excepción a `contratos/sync.md` §3: hasta 12 h desde que le quitaron la orden o lo quitaron de la cuadrilla, salvo el corte inmediato de §3.9 en un cambio desde una bodega de cuadrilla. Corrige el punto 3, que decía «solo el técnico asignado».
- **R-06. Tipos propios en el lote.** `equipo.cambiar` y `equipo.no_recuperado`, con su `id` en la clave, salen a Argos al aplicarse; `orden.cerrada.equipos[]` solo los resume; `foto_ref` obligatoria en `equipo.no_recuperado`.
- **R-07. Validación de la ubicación y la foto.** `422 ubicacion_sin_referencia`; llegada = último `orden.llegar` del mismo actor ese día; `tomadoEn` obligatorio y acotado por el servidor; «usada» abarca intentos y cambios.
- **R-08. Transición de versiones.** §4.5.
- **R-09. Errores de negocio por ruta y `type` exacto.** `demasiados_intentos` definitivo en `…/no-recuperado`; los `403` de §3.9 por la URN completa, con `actor_desconocido`; «al día» en America/Guatemala, en los dos lados.
- **R-10. Vínculo y actor.** Webhooks `tecnico.argos_*` para todo técnico (§6.6); `403 actor_desconocido`; `actor_no_coincide` también en la bodega personal; consumo por actor y bodega.
- **R-11. Restos.** Tabla de claves de §3 con su condición de versión, B27 con la foto siempre obligatoria, §1.2 en orden de versión, espejo y filas A47, A48, B26 y B27.

**Ajustes de la tercera revisión (T-1 a T-12), aceptados por Argos:**

- **T-1. Sin retroceso.** `409 contrato_regresivo` a un `PUT /integraciones/iris/contrato` con una versión menor que la última aceptada, con aviso al admin; bajar exige un canje nuevo aprobado en el panel de Argos (§3.3). Un intento que Argos no pudo verificar no cambia la unidad, no avisa ni cuenta: decide una persona (§4.5).
- **T-2. Cupo diario.** Antes del `429 demasiados_intentos`, Argos verifica el intento que ocupa el cupo y no lo cuenta si sale `404`, `no_enviado_por_iris` o `no_coincide`; Iris muestra «posible intento ajeno».
- **T-3. Semántica de la ruta.** `verificado` solo con un `2xx` de Argos (original o `Idempotent-Replayed`) para esa clave y los bytes guardados; `motivo` con `envio_en_curso`; `422 idempotency_key_reutilizada` → `no_coincide` y alerta de seguridad; `404` → `desconocido_en_iris` sin reintento. Precisa R-04: una clave conocida que no se mandó a ese conector responde `no_enviado_por_iris`.
- **T-4. Tope.** 60 consultas por minuto por conector y 20 por clave al día; Argos reintenta el `429` según `Retry-After` y la operación sigue «pendiente de verificar».
- **T-5. Actor por línea.** `usuario_id` en `materiales[]` (sumados por producto, bodega y usuario) y en `equipos[]` de `instalar` y `retirar`; con un Argos ≥ 0.14.0, `tecnico` por línea, el I1 de ese `usuario_id` (`contratos/eventos.md`).
- **T-6. Vínculo.** `tecnico.argos_*` con `empresa_id`, `version` y, en `desvinculado`, `motivo`; solo al conector del `id_externo`; validado por Argos (`vinculo_rechazado`); carga inicial al informar 0.14.0; `GET <iris>/…/tecnicos/:usuario_id` para conciliar; `actor_desconocido` solo con `tecnico` (§6.6, §3.9).
- **T-7. Cambio.** `foto_ref` obligatoria con `cobro_propuesto`; Argos verifica el cambio con la misma ruta, y un cobro propuesto de un cambio sin verificar no entra a la cola.
- **T-8. Contacto con el cliente.** Solo el registrado por una persona en el panel de Argos; ni una gestión de cobro de Iris ni un intento sin verificar.
- **T-9. Transición.** Con Argos < 0.14.0, un intento por unidad y orden (`intento_repetido`); un intento encolado sin `foto_ref` no sale hacia un Argos ≥ 0.14.0 (§4.5).
- **T-10. Vínculo solo de inventario.** Aparte del de §7.7; nunca autoriza una acción de §7.
- **T-11. Restos.** El estado del adjunto es `completo` (el de Iris); `foto_fuera_de_ventana` si la foto tarda más de 72 h desde la `fecha`; el espejo sigue como «PR enlazada en `argos-isp`».
- **T-12. Riesgo aceptado.** Con el `atk_` se pueden registrar consumos, comodatos, devoluciones y cambios sin cobro a nombre de un técnico vinculado. Argos guarda en el kardex `actor_tecnico` y la clave de idempotencia de cada movimiento, y alerta al admin cuando un técnico registra movimientos fuera de una orden informada por Iris o un volumen anormal en el día. La verificación de esos movimientos con la ruta de §3.10 se puede pedir en una versión futura si el PM lo decide.

**Verificación final (N-1 a N-6), aceptada por Argos:**

- **N-1. Fin de la carga de vínculos.** Iris la cierra con `PUT /integraciones/iris/contrato { contrato, vinculos_cargados: <n> }` tras el `2xx` de todos los `vinculado`; hasta entonces, `409 vinculos_en_carga`, que se reintenta. Una operación con `tecnico` espera el `2xx` del `vinculado` de su técnico (`depende_de`) (§6.6).
- **N-2. Vínculo que cambia de técnico.** Un `desvinculado` que libera un `id_externo` hace que Argos reevalúe los rechazados por unicidad con `GET …/tecnicos/:usuario_id`; Iris no emite el `vinculado` del nuevo hasta el `2xx` del `desvinculado` del anterior.
- **N-3. Ruta de conciliación.** Solo vínculos que salieron de ese conector; si no, sin ficha de técnico en esa empresa o fuera del alcance, el mismo `404`. Se aplica con las reglas del webhook; `activo: false` equivale a desvinculado; `motivo: desactivado` al desactivar el técnico o su usuario.
- **N-4. Verificación no concluyente.** Si la del intento que ocupa el cupo no es concluyente, `503` con `Retry-After`, que se reintenta, nunca `429 demasiados_intentos` (§3.10).
- **N-5. Día del cupo.** El de la `fecha` en America/Guatemala, no el día en que llega a Argos.
- **N-6. Versión mayor que la de Argos.** `422 contrato_no_soportado`, sin registrarla (§3.3, §4.5).

### 10.11 Acordado con Argos para la 0.14.1 (22 nov 2026)

Iris preguntó cómo va A39 cuando el titular es una empresa; Argos propuso no usar `nit` como `documento_tipo` (que es un documento de identidad de persona, cifrado y comparado por «coincide») y separar la empresa del representante:

1. `cliente.tipo` (`persona` | `empresa`); con empresa, NIT en los campos fiscales que ya existían y sin documento de identidad.
2. `representante` obligatorio con empresa y prohibido con persona; su documento es el de la identidad (A41) y el de las fotos.
3. Titular existente por NIT en empresas.
4. `tipo_titular` en `servicio.titular_cambiado`, nunca con documentos.

**Decisión del PM (22 nov 2026):**

- **Empresa existente:** se asigna al momento solo si el representante ya está registrado en esa empresa. Si es nuevo, va a revisión de un supervisor y de una persona de Argos (`titular_empresa_en_revision`).
- **Documento del representante:** se aceptan DPI o pasaporte.

**Ajustes de la revisión de seguridad de la 0.14.1** (todos aceptados por Argos, 22 nov 2026; Argos marca el NIT verificado con `nit_verificado_en` y `nit_verificado_por`, que pone solo una persona en su panel y se borra si cambia `nit_cui`):

- **a. Representante que no coincide (altos 1 y 3).** Con mismo NIT y misma razón social, asignación al momento solo si el representante coincide con una identidad A41, vigente o anterior, de esa empresa; si no, `422 titular_empresa_en_revision`, sin datos del cliente, y decide una persona de Argos con `clientes.ver_identidad`. Un representante nuevo nunca reemplaza la identidad vigente de una empresa con historial.
- **b. Qué NIT cuenta (alto 2).** Solo clientes `tipo = empresa` con `nit_cui` verificado en Argos (nunca de A43 ni de `POST /clientes` del token de Iris); un cliente `persona` nunca coincide; con más de un candidato, revisión; NIT normalizado como en §7.5.
- **c. Contador por NIT (medio 1).** Suman también los `titular_empresa_en_revision`; el tope bloquea ese NIT con cualquier representante.
- **d. Alcance de la identidad del representante (medio 2).** Nunca vale para §7.5 ni §12.4 del cliente empresa ni escribe `nit_cui`; §3.4.3 lo compara con las identidades A41 de la empresa.
- **e. Datos del representante (medio 3).** `***` en pendientes, panel y auditoría, salvo `documento_tipo`.
- **f. Forma del cliente empresa (medio 4).** `422 validacion` con `documento_tipo`, `documento_numero` o `mayor_de_edad` en el `cliente`; NIT como documento de la búsqueda; el plan no tiene que ser `modo: empresarial` (A40).

### 10.12 Acordado con Argos para la 0.14.2 (22 nov 2026)

Dudas que dejó la implementación del portal en Iris (B8–B11, B13, B28). Aceptado por Argos; la revisión de seguridad de la 0.14.2 ajustó los puntos 1 a 3 (sin WebP, lista cerrada para `sistema` y una ruta propia para `capacidades`):

1. **HEIC y WebP.** Fuera de §11.6: el portal de Argos convierte a JPEG antes de subir; Iris responde `422 mime_no_permitido` si llega uno. Iris valida dimensiones antes de decodificar (40 MP) y recodifica JPEG y PNG sin metadatos.
2. **Actor `sistema`.** Solo en una lista cerrada de lecturas (`GET /clientes/:id/contactos`, `/cuentas-bancarias`, `/bancos`), sin `X-Iris-Conversacion` ni `X-Iris-Contacto`; otra ruta, `403 actor_no_permitido`; en una escritura, `422 actor_no_permitido`; auditado con canal `sistema` (Argos A6).
3. **`capacidades`.** Ruta propia `GET /integraciones/argosisp/capacidades` (`integraciones.ver_capacidades`) con solo `{ contrato_iris, capacidades }` del conector del token; el rol `integracion_argos` no recibe `integraciones.ver`.
4. **Planes.** En Argos son globales: la solicitud valida que el plan exista y esté activo, sin empresa.

### 10.13 Acordado con Argos para la 0.14.3 (22 nov 2026)

Dudas que dejó la implementación del bloque E en Iris (B12, B14–B16), aceptadas por Argos:

1. **Técnico en la activación con la orden cerrada (§12.2).** Va el `tecnico_id` de `orden.cerrada` si tiene vínculo I1 en la empresa del servicio; si no, sin `tecnico`, y la unidad tiene que estar ya asignada al servicio. Argos busca la unidad también por `serie` o `mac`.
2. **Marcas por función (§4.3).** `capacidades.equipos[]` por empresa: `estado_equipo`, `wifi`, `bienvenida`, `cobro_equipo` (solo el cobro de la instalación). Dependen de la configuración de cada empresa en Argos (SmartOLT o el ACS para equipo y Wi-Fi). Con contrato < 0.9.0, todas `false` salvo `aprovisionamiento`. Sin la capacidad, `422 capacidad_no_disponible`, solo a conectores ≥ 0.14.3 y después de comprobar el alcance del token. Sin `capacidades.equipos`, Iris se comporta como en la 0.14.2.
3. **Bienvenida sin `origen` (§12.5).** Se relaciona solo con una única orden de instalación del servicio, del titular actual, en los 7 días anteriores a recibir el aviso; no cuenta para `cierre_exige_bienvenida`.
4. **`409 aprovisionamiento_en_curso` ajeno (§12.2).** Iris marca su intento como fallido y no sigue el ajeno; el 409 suma `iniciado_por`; la lectura del ajeno se limita a las empresas del token y a `estado`, `causa`, `iniciado_por` y `actualizado_en`; sin webhooks del ajeno; con el servicio activado fuera de la orden, se cierra sin supervisor ni comodato doble.
5. **Segunda revisión de seguridad.** Un `409` cuyo `aprovisionamiento_id` es de un intento de Iris de la misma orden no es ajeno; Argos recorta la respuesta de un aprovisionamiento ajeno; `cobro_equipo` se reconoce por el concepto de sistema `instalacion`, también en el cargo al cierre; `bienvenida: false` no rechaza nada en Iris; una empresa sin fila cuenta como todo en `false`; el plazo de la bienvenida se cuenta desde que Iris recibe el aviso.
6. **Tercera y cuarta revisión de seguridad.** `codigo_sistema` en `GET /conceptos-cargo`; la línea de instalación también en el cargo al cierre; ante `422 capacidad_no_disponible`, el cargo queda terminal (sin reintento con su clave) y las demás líneas salen como un cobro nuevo con otro `cargo_id`; reintento del intento propio sin respuesta antes de tratar un 409 como ajeno; excepciones al «no reintenta solo».
7. **Quinta revisión de seguridad.** El replay de una clave guardada va antes de la capacidad; tras un envío sin respuesta, «cobro por confirmar» antes del cobro de reemplazo; el cargo del cierre sale sin la línea de instalación si hay un cargo de A36 vigente con ella; el reemplazo no genera otro reemplazo; los `consumible_cobro` no van en el cargo.
8. **Sexta revisión de seguridad.** Una clave en curso responde `409 idempotencia_en_curso` antes de la capacidad; «cobro por confirmar» se concilia solo con la confirmación de un supervisor, nunca por `origen_id`, y cuenta como vigente mientras tanto.
9. **Lectura de la factura.** Para conciliar, `GET /api/v1/facturas/origen-iris` («Factura #» del panel, igual al `factura_id`, o serie y número del DTE), permiso nuevo `facturas.leer_origen_iris` solo en el token de Iris, `empresa_id` y `origen_id` obligatorios, 404 único, 60 lecturas por minuto y auditoría con el actor de Iris; Iris solo concilia con una factura no anulada, con `emitida_en` igual o posterior al primer envío menos 5 minutos, con las mismas líneas y no ligada a otro cargo; conciliar («existe» o «no existe») lleva `version` y se hace con la orden bloqueada (`409` si el cargo ya no está «por confirmar»); un `404` nunca cuenta como «no existe», que es una decisión aparte del supervisor con motivo. Una factura anterior a la 0.14.3 (líneas con `concepto_id: null`) que cumple lo demás queda como factura por anular, sin ligar el cargo; «no existe» exige leerla anulada y, con pagos (`saldo < total`), que el supervisor confirme que se anularon o pasaron a abono.

### 10.14 Decidido por el PM para la 0.15.0 (25 y 26 nov 2026; propuesta de Argos)

**Aceptada por Iris con ajustes, pendiente de la PR espejo** (la línea de versión sigue en 0.14.3 hasta que se fusionen los dos lados). Decisiones del PM que la motivan (plan de Argos §6):

1. **Tipos de servicio:** `internet`, `cable` (CATV por fibra, con ONU; no existe TV por coaxial), `iptv` (servicio de datos con router y plan propios; nunca se «limita») y `mixto`.
2. **Corte de campo (25 nov):** Argos solo avisa a Iris; Iris lleva tickets y órdenes; no hay cola de cortes en Argos, que muestra el estado que Iris devuelva. El camino normal del cable es SmartOLT; el aviso es respaldo.
3. **Entrega de recordatorios (26 nov, P-N12):** con Iris conectado, Iris entrega por los canales que declara en `capacidades` para cada empresa (ajuste de Iris: hoy solo WhatsApp); Argos entrega por sus canales propios los que Iris no declara y todo cuando Iris no está conectado (o es una versión anterior).
4. **Recordatorios:** Argos decide qué y cuándo (plantillas por empresa asignadas por servicio o suscripción, con personalizadas por servicio; nada por zona ni push). Iris entrega con la configuración de comunicación de la empresa.

**Ajustes de Iris, incorporados en las secciones indicadas.** Las decisiones del líder van marcadas.

- **Generales (G1 a G3):** las marcas nuevas van en las `capacidades` de §4.3, sin objeto nuevo; Argos las lee solo de `GET …/capacidades` y nunca de `…/estado`, y `mensajeria[]` trae solo las empresas del alcance del conector; Argos relee al activar la integración, en la revisión periódica de versión y ante `422 canal_no_permitido` o `bloqueado` con `canal_no_disponible` (§4.6). **Pendiente en Iris:** `GET …/capacidades` (B16 de la 0.14.2); B33, B34 y B35 dependen de ello.
- **A, tipo de servicio (§13.1):** A1, qué servicio tiene router y velocidad; A2, velocidades null en cable; A3, `422 tipo_sin_router` y el filtro `tipo_servicio` en `GET /planes`; A4, factibilidad `indeterminado`; A5, `servicio.config_cambiada` por servicio afectado; A6, los dos campos en la primera sincronización y en cada relectura.
- **B, corte manual (§13.2):** B1, orden nueva `corte_catv` / `reconexion_catv` con `referencia` y `referencia_tipo`; B2, estado `no_realizada` con `motivo_codigo`; B3, `version`; B4, cancelaciones fuera de orden; B5, `solicitada_en` y prioridad alta de la reconexión; B6, sin portal ni eventos `ticket.*` y sin avisos propios de Iris; B7, cerrar la orden no activa ni retira; B8, `mixto` solo toca `cable`; B9, empresa fuera del alcance o servicio desconocido, y `sin_respuesta` a las 24 h.
- **C, recordatorios (§13.3):** C1, `canales` solo con lo que Iris implementa y tiene configurado; C2, un canal por aviso con respaldo solo antes del primer envío; **C3 (decisión del líder, opción b):** correo y Telegram transaccionales, sin alta y con baja respetada por los dos lados; **C4 (decisión del líder):** Telegram sigue en Argos hasta que se acuerde, en otra versión, si Iris usa el bot de la empresa (token en la configuración de Iris, nunca en el contrato) o si los clientes se vuelven a vincular, porque el `chat_id` es por bot; C5, motivos y tiempos del correo; C6, `reintenta_el` opcional; C7, límites de `concepto`; C8, códigos por canal; C9, vencimiento de cada código; C10, `422 canal_no_permitido`; C11, WhatsApp desde el número de avisos de la empresa y plantillas `utility`; C12, `entrega_auto` y reenvío por canales propios; C13, correo solo al titular; C14, un `envio_id` no cambia de canal.

### 10.15 Decidido por el PM para la 0.15.1 (26 nov 2026)

**Propuesta de Iris, con el diseño de Argos; pendiente de la PR espejo.** El PM pidió que un servicio pueda cambiar de tipo, por ejemplo un cliente de cable que también quiere internet. La 0.15.0 lo impedía (`422 tipo_sin_router` en `cambiar-plan`).

1. **Dos rutas, no una (propuesta de Argos, aceptada por Iris).** `cambiar-plan` (A40) sigue siendo solo para `pendiente_instalacion` y no factura. Un servicio activo necesita prorrateo y la ruta de red completa, así que usa la ruta nueva `cambiar-tipo`.
2. **Siempre con una orden de Iris.** El cambio nace en una orden `cambio_tipo`: la visita configura el equipo cuando se agrega internet o cable, y la llamada a Argos sale al cerrarla. Así queda registro, técnico y evidencia.
3. **Argos factura.** El prorrateo es el mismo que el del cambio de plan en el panel de Argos. Iris solo muestra el precio del plan nuevo antes de confirmar.
4. **La ONU no cambia.** El paso de cable a mixto solo agrega la parte de internet (router, IP y PPPoE). La ONU sigue registrada en Argos.
5. **Bloque C, hallado al implementar B35 (líder de Iris, aceptado por Argos).** Un aviso `auto` puede quedarse sin canal después del `202` (una baja o el número de avisos desconectado mientras espera el silencio). Para que el reenvío de C12 no espere a la consulta de 24 h, Iris lo avisa con `aviso.bloqueado`. Con un canal explícito, una baja posterior al encolado termina en `aviso.fallido` con el motivo nuevo `baja`, para que Argos no tenga que leer `detalle`.

## 11. Portal del cliente de Argos (0.9.0, bloque P)

El cliente abre tickets de soporte, los sigue y pide servicios **desde el portal de Argos**, sin salir de él. Tickets y seguimiento comercial son de Iris: Argos llama a Iris servidor a servidor con el token de §1.2, **en nombre de un cliente que su portal ya identificó**. Iris nunca recibe la contraseña, el documento, la cookie ni la IP del cliente.

### 11.1 Principios

| Regla | Detalle |
| --- | --- |
| Quién identifica | Argos. Iris confía en el `token_portal` de §1.2 y en el `cliente_id` de la ruta, y lo comprueba contra la empresa (§11.2). **Responsabilidad de Argos:** el `cliente_id` de la ruta y `X-Iris-Empresa` salen, en su servidor, de la sesión del portal del cliente; nunca de un dato que mande el navegador. El `token_portal` vive solo en el servidor del portal. |
| Lo que ve el cliente | Lo mismo que el portal de soporte de Iris: el ticket reducido y sus mensajes públicos. **Nunca** notas internas, asignado, cuadrilla, prioridad, SLA, historial, motivos escritos por el personal, datos de Horus (IP, hallazgos, enlaces), ids de usuarios de Iris, ni nada de otro cliente o de otra empresa. |
| Un solo cliente por ruta | El cliente va en la ruta. Un ticket o una solicitud de otro cliente o de otra empresa responde `404 no_encontrado`, igual que uno que no existe. |
| Auditoría | Iris audita con actor `integracion` (el usuario de integración del conector), canal `integracion`, nombre «Argos (portal) · cliente `<id>`» y `en_nombre_de { cliente_id, empresa_id, metodo, sesion }`. Lo que escribe el cliente queda como mensaje **del cliente**. |
| Formato | Cuerpos y respuestas en `snake_case` (como §6). Listas `{ data, nextCursor }` con `?cursor=&limit=` (`limit` de 1 a 100, por defecto 20). Fechas RFC 3339 en UTC. |

### 11.2 Cabeceras, alcance y límites

- `Authorization: Bearer iris_pat_…`: el `token_portal` de §1.2 (con el token del panel, `403 solo_integracion_argos`).
- `X-Iris-Empresa: <empresa_id>`: **obligatoria**, la empresa en que se atiende (la del servicio). Fuera del alcance del token: `403 empresa_no_permitida`. Un cliente con servicios en varias empresas se consulta empresa por empresa.
- `X-Argos-Portal-Sesion: <metodo>:<id>`: **obligatoria**. `^(documento|ip):[A-Za-z0-9_-]{8,64}$`. `metodo` es cómo identificó Argos al cliente; `id` es un identificador opaco y estable de la sesión del portal (por ejemplo, un hash de su id). Nunca el documento, la IP ni la cookie. Iris lo guarda en la auditoría y lo usa en los topes.
- **Sesión `ip`** (el cliente entró por su IP, sin contraseña): Argos solo la da si la IP corresponde a **un único servicio vigente** (nunca una IP compartida ni detrás de CGNAT). Con ella, solo `GET …/tickets` (lista reducida: `id`, `numero`, `estado`, `categoria`, `afectado_por_incidente` y fechas; **sin `asunto`** ni mensajes), `GET …/categorias` y `POST …/tickets` **sin adjuntos** (`422 adjuntos_no_permitidos`). El resto responde `403 sesion_insuficiente` (§10.4, punto 2: abrir ticket y ver la lista, nada más). Fuera de este contrato y solo en Argos: para pedir o hacer un cambio de Wi-Fi desde su portal, el cliente vuelve a confirmar su contraseña del portal o su identidad aunque ya tenga sesión.
- `Idempotency-Key`: **obligatoria** en todo `POST` (`400 idempotency_key_requerida`). Forma: `argos:portal:<operacion>:<id>`, con `<id>` un UUID que el portal genera **por envío de formulario** y repite en cada reintento: `argos:portal:ticket:<uuid>`, `argos:portal:respuesta:<uuid>`, `argos:portal:confirmar:<ticket_id>:<version>`, `argos:portal:solicitud:<uuid>`. La guarda Iris por credencial y cliente. Misma clave y mismo cuerpo: la respuesta original (mismo status y cuerpo). Misma clave con otro cuerpo: `422 idempotencia_reutilizada`. Primera aún en curso: `409 idempotencia_en_proceso` (se reintenta con la misma clave y los mismos bytes, §3.1).
- **Cliente:** si Iris lo conoce (instantánea), debe ser de la empresa (`422 empresa_no_coincide`). Si no lo conoce, lo lee en Argos (`GET /clientes/:id`): inexistente `422 cliente_no_existe`, de otra empresa `422 empresa_no_coincide`. Si Argos no responde: **solo `POST …/tickets`** se acepta (auditado con `cliente_validado: false`; Iris lo concilia al volver Argos y, si el cliente no era de la empresa, lo marca para revisión); las lecturas, las respuestas, la confirmación y las solicitudes responden `503 argos_no_disponible`.
- **Topes** (los mismos del portal de soporte de Iris y **compartidos** con él, por cliente y empresa): tickets abiertos desde un portal (10) y creados en la última hora (20) → `429 demasiados_tickets`; mensajes del cliente en el último minuto (10) → `429 demasiados_mensajes`; solicitudes de servicio creadas en el último día (5) → `429 demasiadas_solicitudes`. Tope del conector: 600 peticiones por minuto → `429 demasiadas_peticiones`. Todos con `Retry-After`. Los valores son configurables en Iris; una petición repetida con la misma `Idempotency-Key` no cuenta.
- Con el conector Argos apagado en Iris: `409 argos_apagado`.

### 11.3 Rutas

Todas bajo `https://<iris>/api/v1/integraciones/argosisp/portal/clientes/{cliente_id}` (`cliente_id` = id del cliente en Argos, `^[1-9]\d{0,17}$`).

| Método y ruta | Permiso | Qué hace |
| --- | --- | --- |
| `GET /categorias` | `portal_argos.ver_tickets` | Categorías que el cliente puede elegir (activas y visibles en el portal, globales y de la empresa): `{ data: [{ id, codigo, nombre }] }` |
| `GET /tickets?estado=&servicio_id=&cursor=&limit=` | `portal_argos.ver_tickets` | Sus tickets en la empresa, de cualquier origen, más recientes primero. `estado` admite varios separados por coma |
| `GET /tickets/{ticket_id}` | `portal_argos.ver_tickets` | Un ticket |
| `GET /tickets/{ticket_id}/mensajes?cursor=&limit=` | `portal_argos.ver_tickets` | Mensajes públicos, más antiguos primero |
| `GET /tickets/{ticket_id}/adjuntos/{adjunto_id}` | `portal_argos.ver_tickets` | Descarga un adjunto de un mensaje público del ticket (§11.6) |
| `POST /tickets` | `portal_argos.crear_ticket` | Abre un ticket (§11.4) |
| `POST /tickets/{ticket_id}/responder` | `portal_argos.responder_ticket` | Mensaje del cliente (§11.5) |
| `POST /tickets/{ticket_id}/confirmar-solucion` | `portal_argos.confirmar_solucion` | El cliente confirma que quedó resuelto (§11.5) |
| `GET /solicitudes?estado=&cursor=&limit=` | `portal_argos.ver_solicitudes` | Sus solicitudes de servicio (§11.7) |
| `GET /solicitudes/{solicitud_id}` | `portal_argos.ver_solicitudes` | Una solicitud |
| `POST /solicitudes` | `portal_argos.solicitar_servicio` | Pide un servicio (§11.7) |

**Ticket** (lo que devuelven las rutas de tickets):

```json
{
  "id": 8812,
  "numero": "TK-008812",
  "asunto": "Sin internet desde la mañana",
  "estado": "abierto",
  "categoria": { "id": 3, "codigo": "sin_servicio", "nombre": "Sin servicio" },
  "servicio_id": 4410,
  "origen": "portal_argos",
  "afectado_por_incidente": false,
  "puede_confirmar": false,
  "resuelto_en": null,
  "cerrado_en": null,
  "version": 4,
  "creado_en": "2026-11-21T14:03:11Z",
  "actualizado_en": "2026-11-21T15:20:40Z"
}
```

- `estado`: `nuevo`, `abierto`, `en_espera`, `resuelto`, `cerrado`. `categoria` puede ser `null`. `origen`: `panel`, `portal`, `portal_argos`, `correo`, `telegram`, `horus`, `argos`, `api` (informativo: el portal puede mostrar «abierto por soporte»).
- `afectado_por_incidente`: el ticket está ligado a una falla general de la red que sigue abierta (el portal puede decir «estamos trabajando en una falla en tu zona»).
- `puede_confirmar`: está `resuelto`; el cliente puede confirmar (lo cierra) o responder (lo reabre).

**Mensaje:**

```json
{
  "id": 51230,
  "autor_tipo": "agente",
  "autor_nombre": "Ana López",
  "cuerpo": "Ya revisamos tu equipo, ¿puedes reiniciarlo?",
  "adjuntos": [{ "id": "0193a7c2-…", "nombre": "foto.jpg", "mime": "image/jpeg", "tamano": 183220 }],
  "creado_en": "2026-11-21T15:20:40Z"
}
```

- `autor_tipo`: `cliente` (el propio cliente, por cualquier portal o por correo), `agente` (soporte) o `sistema` (aviso automático; `autor_nombre` = «Soporte»). Con `agente`, `autor_nombre` es el **nombre de pila** del agente (cada empresa puede cambiarlo por «Soporte» o el nombre de la empresa); nunca su nombre completo ni su id.

### 11.4 Abrir un ticket

`POST …/tickets` con `Idempotency-Key = argos:portal:ticket:<uuid>`. `application/json`, o `multipart/form-data` con la parte `datos` (el mismo JSON) y hasta 3 partes `archivos` (§11.6).

```json
{
  "asunto": "Sin internet desde la mañana",
  "descripcion": "La luz LOS del módem está en rojo.",
  "categoria_id": 3,
  "servicio_id": 4410
}
```

- `asunto` de 3 a 200 caracteres; `descripcion` de 1 a 20 000 (primer mensaje público del cliente); `categoria_id` opcional (una de `GET …/categorias`; otra: `422 validacion`, `rule: "categoria"`); `servicio_id` opcional (debe ser del cliente según la instantánea; si no, `422 validacion`, `rule: "servicio"`).
- Iris crea el ticket con origen `portal_argos`, prioridad media, contacto (nombre y teléfono) y zona de la ficha de Argos, y lo liga a una falla general abierta como cualquier otro ticket. Emite `ticket.creado`.
- `201` con el ticket (§11.3). Un reintento con la misma clave: el mismo ticket (`201`).

### 11.5 Responder y confirmar

- `POST …/tickets/{ticket_id}/responder` con `Idempotency-Key = argos:portal:respuesta:<uuid>`; JSON o `multipart/form-data` (§11.6): `{ "cuerpo": "…", "version": 4 }` (`cuerpo` de 1 a 20 000; `version` opcional: si viene y cambió, `409 conflicto_version`). Un ticket `en_espera` se reanuda y uno `resuelto` se reabre (como cuando el cliente responde por correo); `cerrado` responde `422 transicion_invalida`. Responde `200 { ticket, mensaje }`. Emite `ticket.mensaje_cliente` con canal `portal_argos` y, si cambió el estado, `ticket.estado_cambiado`.
- `POST …/tickets/{ticket_id}/confirmar-solucion` con `Idempotency-Key = argos:portal:confirmar:<ticket_id>:<version>`: `{ "version": 5 }` (obligatoria). Solo `resuelto` → `cerrado`; si no, `422 transicion_invalida`. Responde `200` con el ticket.

### 11.6 Adjuntos

- **Subir:** solo dentro de `POST …/tickets` y `POST …/responder` en `multipart/form-data`. Hasta **3** archivos por mensaje, **10 MB** cada uno; tipos `image/jpeg`, `image/png` y `application/pdf`, comprobados por el contenido (no por el nombre). Desde 0.14.2, **sin `image/heic` ni `image/webp`**: el portal de Argos convierte cualquier otra foto a JPEG en el navegador antes de subir (calidad 0,85, lado mayor ≤ 2560 px, sin EXIF) y, si no puede leerla, pide otra; un HEIC o WebP que llegue a Iris responde `422 mime_no_permitido`. Fuera de eso: `413 tamano_excedido`, `422 mime_no_permitido` o `422 demasiados_adjuntos`, y no se crea nada. Iris quita los metadatos EXIF de las fotos. Igual que los comprobantes de §7.2: Argos sube los bytes y nadie descarga URLs del otro (sin SSRF).
- La idempotencia incluye los archivos: un reintento manda exactamente los mismos bytes.
- **Al guardar:** Iris lee las dimensiones del encabezado (SOF, IHDR) **antes** de decodificar, con un tope de 40 MP (`422 imagen_demasiado_grande`), y recodifica JPEG y PNG sin guardar ningún metadato ni bloque del original; una imagen que no se puede recodificar responde `422 mime_no_permitido`.
- **Descargar:** `GET …/tickets/{ticket_id}/adjuntos/{adjunto_id}` devuelve el archivo solo si es de un mensaje **público** de ese ticket del cliente; si no, `404 no_encontrado`. Cabeceras: `Content-Type` real, `Content-Disposition: attachment` con el nombre saneado en `filename*` (sin `/`, `\`, `"`, CR ni LF; hasta 120 caracteres), `X-Content-Type-Options: nosniff` y `Content-Security-Policy: sandbox`. Argos lo sirve al cliente desde su propio servidor con las mismas cabeceras; un PDF nunca se muestra embebido en un panel.

### 11.7 Solicitudes de servicio

En Iris una solicitud es un **prospecto** de ventas (seguimiento comercial; §10.4, punto 1): un asesor la revisa, cotiza y la convierte en orden. Origen `portal_argos`; el cliente de Argos ya es conocido, así que «ganar» nunca crea otro cliente en Argos.

`POST …/solicitudes` con `Idempotency-Key = argos:portal:solicitud:<uuid>`:

```json
{
  "tipo": "traslado",
  "servicio_id": 4410,
  "direccion": "5a avenida 10-20, zona 1, Mixco",
  "coordenadas": "14.6349,-90.5069",
  "plan_interes_id": 12,
  "comentario": "Me mudo el 1 de diciembre",
  "telefono_contacto": "+502 5512-3344"
}
```

| Campo | Regla |
| --- | --- |
| `tipo` | `alta` (servicio nuevo), `cambio_plan`, `traslado` u `otro` |
| `servicio_id` | Obligatorio en `cambio_plan` y `traslado` (`422 servicio_requerido`); debe ser del cliente (`422 validacion`, `rule: "servicio"`). Opcional en `otro`; no va en `alta` |
| `direccion` | Obligatoria en `alta` y `traslado` (1 a 300). En `cambio_plan` se toma la del servicio |
| `coordenadas` | Opcional, `"lat,lng"` como en §3.2 |
| `plan_interes_id` | Opcional, un plan **activo** de Argos (`GET /planes`; los planes son globales, sin empresa: `catalogo.cambiado` de un plan lleva `empresa_id: null`); otro o inactivo: `422 validacion`, `rule: "plan"` |
| `comentario` | Opcional, hasta 2000 |
| `telefono_contacto` | Opcional, con la regla de teléfonos de §3.2; si falta, el de la ficha |

- Responde `201` con la solicitud. Una abierta del mismo tipo y servicio (`alta`: del mismo tipo) responde `409 solicitud_abierta` con `extensions.solicitud_id`.
- Iris la crea con nombre, correo y teléfono de la ficha de Argos y la deja sin vendedor para que la tome ventas (o la asigna por zona si la empresa lo configuró). Emite `prospecto.creado` (origen `portal_argos`).

**Solicitud** (lo que devuelven las rutas):

```json
{
  "id": 731,
  "numero": "PR-000731",
  "tipo": "traslado",
  "servicio_id": 4410,
  "estado_cliente": "en_revision",
  "comentario": "Me mudo el 1 de diciembre",
  "creado_en": "2026-11-21T16:02:00Z",
  "actualizado_en": "2026-11-21T17:40:12Z"
}
```

`estado_cliente`: `recibida`, `en_revision`, `cotizada`, `en_proceso`, `completada` o `no_procede`. Nunca el vendedor, las actividades, la factibilidad, la cotización ni el motivo de pérdida.

**Qué hace Iris al ganar** (informativo para Argos):

| `tipo` | En Argos | Orden en Iris |
| --- | --- | --- |
| `alta` | Solo `POST /servicios` (§3.2) con el `cliente_id` conocido, `pendiente_instalacion` | Instalación |
| `cambio_plan` | Nada por API en 0.9.0: una persona lo aplica en Argos (Iris deja la tarea con «Abrir en Argos»; A34 después) | `cambio_equipo` si hace falta |
| `traslado` | Igual que `cambio_plan` | Visita de traslado |
| `otro` | Nada | La decide el asesor |

### 11.8 Lo que Iris nunca hace por esta vía

- Mostrar o aceptar datos de otro cliente, de otra empresa o internos (§11.1).
- Crear clientes ni servicios en Argos fuera de «ganar» (§3.2).
- Cambiar el Wi-Fi del cliente: eso es del portal de Argos y queda fuera del contrato (§12 cubre solo los pedidos del personal de Iris).

### 11.9 Portal de soporte de Iris (`/soporte`, ya existe)

Queda como respaldo (y para Argos < 0.9.0): con el permiso `portal.emitir` del token de §1.2, `POST https://<iris>/api/v1/portal/tickets-acceso { clienteId, empresaId, nombre? }` devuelve `201 { url: "https://<iris>/soporte/entrar#<ticket>", expiraEn }`, válida 60 s y de un solo uso, a la que Argos redirige al cliente. El ticket va en el fragmento y nunca en la consulta ni en logs. Errores: `403 empresa_no_permitida`, `422 empresa_no_coincide`, `422 cliente_no_existe`. La sesión del cliente en Iris dura 2 h. Los topes de §11.2 son los mismos para los dos portales.

### 11.10 Tickets pedidos por un contacto (0.12.1, servicio a cargo, A46)

Cuando quien entra al portal de Argos es un **contacto** de otro cliente (§3.8, A46: por ejemplo, José a cargo del servicio de su mamá), las rutas de §11 funcionan así.

- **Cabecera en todas las rutas.** Con contrato ≥ 0.12.1, toda ruta de §11 lleva `X-Iris-Contacto` (forma de §7.1): `<contacto_id>` cuando la sesión es de un contacto, o el valor literal `titular`. Si falta: `400 cabecera_iris_requerida`.
- **Quién recibe una sesión de contacto.** Argos solo la abre si el contacto existe y tiene `reportar` o `consultar` sobre ese servicio (como §7.1), y manda además `X-Iris-Contacto-Version: <version>`: la `version` del contacto en `core.contactos_cliente` (§3.5), un entero positivo (`^[1-9]\d{0,9}$`); mal formada, `422 validacion` (`field: X-Iris-Contacto-Version`).
- **Caché de Iris.** Iris aplica la regla con su caché de contactos (la que llena `cliente.contactos_cambiados`, §2.3). Si la versión que tiene es menor que `X-Iris-Contacto-Version`, o el contacto no está en la caché, lo vuelve a leer con `GET /clientes/:id/contactos` **antes de responder**. Si Argos no responde: `503 argos_no_disponible`; nunca resuelve con la caché vieja.
- **Servicio.** Con `<contacto_id>`, `servicio_id` es obligatorio (en la lista, `?servicio_id=`; al crear, en el cuerpo). Si falta: `422 validacion` con `field: servicio_id`. El contacto debe ser de ese cliente y el servicio debe estar en sus `servicios[]`; si no, `403 servicio_fuera_del_contacto`. Los tickets sin `servicio_id` nunca se le muestran.
- **Qué ve el contacto:**
    - **sus propios tickets** (los que abrió él, `creado_por_contacto`) completos, como el titular;
    - **los demás tickets de ese servicio**, solo con `numero`, `estado`, `categoria`, `afectado_por_incidente` y fechas: sin asunto, sin mensajes de nadie y sin adjuntos.
- **Qué puede hacer:** crear, responder y confirmar exigen `reportar` (si no, `403 permiso_de_contacto`) y solo valen sobre **sus propios tickets**. Los adjuntos (§11.6) solo se ven en los tickets que el contacto ve completos. Las solicitudes de servicio (§11.7) responden `403 solo_titular`.
- **Topes propios.** Con `<contacto_id>`, el contacto tiene sus propios topes de §11.2, además de los del cliente.
- **Al crear** con `<contacto_id>`, Iris guarda `creado_por_contacto`; el dueño lo ve como «Abierto por <nombre del contacto> (a cargo)». El nombre sale de la caché, nunca el teléfono.
- **Contacto quitado** (`cliente.contactos_cambiados` con `accion: quitado`): sus tickets siguen visibles para el dueño y el contacto deja de verlos.
- `GET …/tickets` acepta además `?servicio_id=` como filtro opcional con `titular`.

## 12. Equipos del cliente y activación desde la orden (0.9.0, bloque E)

### 12.1 Principios

| Regla | Detalle |
| --- | --- |
| Dueño de los equipos | Argos es el único que habla con SmartOLT (ONU/ONT de fibra) y con el ACS (TR-069: routers, CPE y radios que lo soporten). **Iris nunca guarda credenciales de SmartOLT ni del ACS** y nunca los llama: todo lo pide a Argos por las rutas de esta sección. El detalle de SmartOLT/TR-069 (perfiles, VLAN por plan y por router/OLT, IPAM) es de Argos. |
| Tecnologías | **FTTH** (ONT por SmartOLT, identificada por su serie; planta PON opcional) y **wireless** (CPE o radio, identificado por su MAC; sin serie de ONT ni puerto PON; gestionado por TR-069 solo si lo soporta). Un router TR-069 del cliente puede ir sobre cualquiera de las dos. |
| Quién lo pide | En Iris, según la empresa o la plantilla: el técnico desde la orden (también sin señal) o un agente desde el panel, aun con la orden ya cerrada. El asistente de IA de Iris puede consultar el equipo y cambiar el Wi-Fi solo si la empresa lo habilitó y le dio los permisos `servicios.ver_equipo` / `servicios.cambiar_wifi`, con el cliente verificado en esa conversación (§12.4); el asistente nunca pide ni recibe la clave (el cliente la escribe en un formulario de Iris) y nunca aprovisiona. El cliente cambia su Wi-Fi desde el portal de Argos, fuera de este contrato (Argos le pide volver a confirmar su contraseña o su identidad aunque ya tenga sesión). |
| Asíncrono | Aprovisionar y cambiar el Wi-Fi responden `202` y el resultado llega por webhook; Iris consulta si el webhook no llega. |
| Secretos | La clave Wi-Fi viaja solo en el cuerpo de la petición. Nunca en webhooks, respuestas, auditoría, eventos, logs (también los de peticiones HTTP) ni en el historial de un chat: en los dos lados queda `***`. En Iris, también `***` en `integracion.pendientes` y sus eventos (`payload_anterior`, `payload_nuevo`) y en la auditoría de la operación sin conexión; la guarda cifrada solo hasta el `2xx` de Argos y luego la descarta; su registro de idempotencia guarda solo la huella, nunca el cuerpo. El teléfono la borra de su cola al recibir la confirmación del servidor. El mensaje al cliente no la repite. |
| Idempotencia | §3.1. Claves `iris:aprovisionar:<orden_id>:<intento>`, `iris:wifi:orden:<orden_id>:<uuid>`, `iris:wifi:ticket:<ticket_id>:<uuid>` e `iris:wifi:conversacion:<conversacion_id>:<uuid>`. |
| Capacidad | Argos informa por empresa y tecnología qué puede hacer (`capacidades.aprovisionamiento`, §4.3) y, desde 0.14.3, por función (`capacidades.equipos`). Sin `aprovisionamiento`, Iris activa al cerrar como en §3; sin la marca de `equipos`, no ofrece esa función (§4.3). |
| Facturación | El mensual empieza cuando el servicio queda activo. El cobro de la instalación es una factura única aparte (`POST /facturas/cargo-unico` con una línea del concepto con `codigo_sistema: "instalacion"` (§4.3) y el precio que pone Iris, §3 y A36), que registra un agente o sale de un formulario de la orden. |

### 12.2 Aprovisionar y activar (A32)

`POST /api/v1/servicios/:id/aprovisionar` (`servicios.aprovisionar`), `Idempotency-Key` obligatoria. Solo servicios `pendiente_instalacion` (A1).

FTTH:

```json
{
  "equipo": { "tipo": "ont", "serie": "ZTEGC8A1B2C3", "mac": null, "unidad_id": null, "modelo": "F670L" },
  "planta": { "olt": "OLT-Centro", "slot": 1, "pon": 7, "caja": "CJ-118", "puerto": 4 },
  "wifi": { "ssid": "Familia Pérez", "clave": "********" },
  "tecnico": { "id_externo": "57" },
  "origen_tipo": "iris_orden",
  "origen_id": "9123"
}
```

Wireless:

```json
{
  "equipo": { "tipo": "cpe_wireless", "serie": null, "mac": "AA:BB:CC:11:22:33", "unidad_id": 812, "modelo": "LiteBeam 5AC" },
  "wireless": { "ap": "AP-Cerro-Norte", "sector": "S2" },
  "wifi": null,
  "tecnico": { "id_externo": "57" },
  "origen_tipo": "iris_orden",
  "origen_id": "9124"
}
```

- `equipo.tipo`: `ont` (exige `serie`), `cpe_wireless` (exige `mac`; `serie` opcional) o `router_tr069` (`mac` o `serie`, la que use el ACS). `unidad_id` opcional si Iris ya lo conoce del inventario. Si el equipo es una unidad del inventario de Argos, debe estar en una bodega permitida (abajo) o ya asignada al servicio (`409 unidad_en_otra_bodega`); si no está asignada, Argos la asigna en comodato al servicio en la misma operación. Con la orden cerrada (lo pide un agente desde el panel), Iris manda `tecnico` = el `tecnico_id` de `orden.cerrada` **si tiene vínculo I1 en la empresa del servicio**, validado en el servidor de Iris; si no, va sin `tecnico`. Argos busca la unidad por `unidad_id`, `serie` o `mac`. Con `tecnico`, Argos comprueba que su `id_externo` sea un técnico vinculado de esa empresa (si no, `403 actor_desconocido`) y que la unidad esté en su bodega personal (o en la de su cuadrilla de esa empresa) o ya asignada al servicio. Sin `tecnico` (desde 0.14.3), la unidad tiene que estar ya asignada al servicio. Una unidad del inventario en cualquier otra bodega, también de otra empresa, responde `409 unidad_en_otra_bodega` sin decir de qué bodega es. Un equipo que no está en el inventario sigue permitido y no genera comodato.
- `planta` (A9) solo con `ont`; `wireless` (`ap`, `sector`, opcionales) solo con `cpe_wireless`. Otra combinación: `422 equipo_invalido`.
- `wifi` opcional: SSID de 1 a 32 caracteres, clave de 8 a 63 (`422 wifi_invalido`). Con un equipo que Argos no gestiona (wireless sin TR-069), `wifi` responde `422 wifi_no_disponible` y no se aprovisiona nada: Iris lo reenvía sin `wifi`.
- **Respuesta** `202 { aprovisionamiento_id, estado: "en_proceso" }`. Si ya terminó (reintento con la misma clave), `200` con el estado final.
- **Errores** inmediatos: `409 servicio_no_pendiente`, `409 aprovisionamiento_en_curso` (con `extensions.aprovisionamiento_id` y, desde 0.14.3, `extensions.iniciado_por`: `argos` | `iris`; nunca datos del equipo), `409 unidad_en_otra_bodega`, `422 equipo_invalido`, `422 wifi_invalido`, `422 wifi_no_disponible`, `422 aprovisionamiento_no_disponible` (la empresa no tiene esa tecnología configurada); desde 0.10.0 también `422 plan_no_permitido` (§3.4.5).

`GET /api/v1/aprovisionamientos/:id` (`servicios.aprovisionar`) → `200 { aprovisionamiento_id, servicio_id, empresa_id, origen_tipo, origen_id, estado, pasos[], causa, reintentable, detalle, servicio_estado, equipo, senal, creado_en, actualizado_en }` y, desde 0.14.3, `iniciado_por` (`argos` | `iris`; recortada para un aprovisionamiento ajeno, abajo).

- `estado`: `en_proceso`, `listo` o `fallido`.
- `pasos[]`: `{ paso: ip | equipo | wifi | activacion, estado: pendiente | hecho | fallido | no_aplica, en }`. Wireless sin TR-069: `equipo` y `wifi` vienen `no_aplica`.
- `equipo`: `{ tipo, serie, mac, unidad_id, gestionado }` (`gestionado`: Argos lo controla por SmartOLT o el ACS).
- `causa` (con `fallido`): `equipo_no_encontrado` (la ONT no aparece en la OLT, o el CPE/router no contactó al ACS), `equipo_en_uso` (la serie o la MAC ya están en otro servicio), `sin_puerto`, `perfil_sin_mapear` (el plan o el router/OLT/AP no tienen perfil), `sin_ip_disponible`, `olt_sin_respuesta`, `acs_sin_respuesta`, `servicio_no_pendiente`, `otro`. `reintentable` dice si tiene sentido reintentar sin cambiar nada; `detalle` es texto para la persona, sin secretos.
- Con `listo`, el servicio queda `activo` y **empieza el cobro mensual**. Argos emite además sus eventos de siempre (`servicio.estado_cambiado`, `servicio.instalacion_activada`). El cobro de la instalación no sale de aquí: es una factura única (§3).

Webhooks Argos → Iris (§2.3): `instalacion.aprovisionada` e `instalacion.fallida`, uno final por aprovisionamiento. Iris los deduplica por `X-Webhook-Id` y los relaciona por `aprovisionamiento_id` o por `idempotency_key`. Si no llega en 10 min, Iris consulta el `GET`.

**Aprovisionamiento ajeno (0.14.3).** Si `extensions.aprovisionamiento_id` es de un intento de Iris de esa misma orden (por ejemplo, el del técnico sin conexión y el de un agente), no es ajeno: Iris sigue ese intento y descarta el nuevo. Si la orden tiene otro intento de Iris enviado y sin respuesta, Iris reintenta primero ese intento con su misma clave y solo después decide si el 409 es ajeno. Con `iniciado_por: iris` de otra orden, o con `argos`, es ajeno: con `409 aprovisionamiento_en_curso`, Iris marca su intento como `fallido` con esa causa y **no sigue** el aprovisionamiento en curso como si fuera suyo (no lo relaciona con su orden ni toma su resultado como el de su intento). Si quiere mostrar cómo terminó, lo consulta con `GET /aprovisionamientos/:aprovisionamiento_id` (mismo permiso `servicios.aprovisionar`), solo para mostrarlo; un nuevo intento de Iris es otra clave. `GET /aprovisionamientos/:id` responde solo aprovisionamientos de servicios dentro de las empresas del token; fuera de ellas, el mismo `404` que uno que no existe. Para un aprovisionamiento que no inició ese conector, Argos responde solo `{ aprovisionamiento_id, servicio_id, empresa_id, origen_tipo, origen_id: null, iniciado_por, estado, causa, actualizado_en }`, sin `equipo`, `pasos`, `detalle` ni `senal`. Iris comprueba que `servicio_id` y `empresa_id` de la respuesta sean los de su orden antes de mostrar nada, y muestra solo `estado`, `causa`, `iniciado_por` y `actualizado_en`: nunca copia `equipo`, `serie` ni `mac` a la orden ni a `orden.cerrada`, ni se los pasa al asistente de IA. Si lo inició Argos, `origen_tipo: argos_panel` y `origen_id: null`. Argos no manda `instalacion.*` a Iris por un aprovisionamiento que no inició Iris; un `instalacion.*` cuyo `aprovisionamiento_id` e `idempotency_key` no son de un intento de Iris se registra y se descarta. Si el servicio queda `activo` por un aprovisionamiento ajeno, la orden muestra «activado fuera de la orden» (`orden.cerrada.activacion` con `estado: activado_fuera` y `aprovisionamiento_id: null`; Iris confirma el estado `activo` leyendo Argos antes de permitirlo, no solo con la instantánea) y se puede cerrar sin supervisor, sin mandar `activar-instalacion` y sin comodato de una unidad que Argos ya asignó al servicio (si llega, Argos lo responde como hecho, sin duplicarlo).

**En Iris** (informativo para Argos):

- Permisos de Iris: `ordenes.activar_servicio` y `servicios.cambiar_wifi`; un técnico los usa **solo sobre sus órdenes en curso** (asignadas a él y no cerradas); con la orden cerrada, solo un agente.
- La activación la pide el técnico desde la orden o un agente desde el panel. Cada empresa (o plantilla) elige el modo: al cerrar como hoy (§3); **exigir la activación lista** para cerrar, salvo un supervisor con motivo; o **permitir cerrar** con «activación pendiente por agente».
- El técnico la pide con la operación sin conexión `orden.activar_servicio` (`contratos/sync.md`): se guarda en el teléfono y se envía al volver la señal. Un agente, desde el panel, también con la orden ya cerrada. Un `fallido` no es un rechazo de la operación: se ve la causa y «Reintentar» es otro intento (otra clave).
- Con «exigir lista», la instalación no se cierra como exitosa sin la activación `lista` (salvo el supervisor). Con «pendiente por agente», se cierra sin ella y un agente la termina desde el panel, con la orden ya cerrada. En los dos casos, el cierre **no** manda `activar-instalacion` ni el comodato del equipo que se aprovisiona (§3). `orden.cerrada` lleva `activacion { modo, estado, aprovisionamiento_id, cerrada_sin_activacion_motivo }` y `equipos[].aprovisionado`.

### 12.3 Estado del equipo (A33)

`GET /api/v1/servicios/:id/equipo` (`servicios.ver_equipo`) → `200`:

```json
{
  "servicio_id": 4410,
  "tecnologia": "ftth",
  "tipo": "ont",
  "serie": "ZTEGC8A1B2C3",
  "mac": null,
  "modelo": "F670L",
  "en_linea": true,
  "senal": { "rx_dbm": -19.8, "tx_dbm": 2.1 },
  "firmware": "V9.0.10P2N3",
  "ultimo_contacto": "2026-11-21T15:58:02Z",
  "fuente": "smartolt",
  "leido_en": "2026-11-21T15:58:05Z"
}
```

- `tecnologia`: `ftth` o `wireless`. `tipo`: `ont`, `cpe_wireless` o `router_tr069`.
- `senal`: con `ont`, `{ rx_dbm, tx_dbm }`; con `cpe_wireless` gestionado, `{ rssi_dbm, snr_db }` si el equipo los informa; si no, `null`. `fuente`: `smartolt` o `acs`. Argos puede responder de su caché si `leido_en` tiene menos de 60 s.
- Errores: `409 equipo_sin_gestion` (por ejemplo, wireless sin TR-069), `503 equipo_sin_respuesta` (Iris no reintenta solo: muestra «no se pudo leer» con «Reintentar»).
- Nunca trae la clave Wi-Fi ni credenciales del equipo.
- Con `X-Iris-Actor: asistente_ia` (§7.1), Argos responde **solo** `servicio_id`, `tecnologia`, `en_linea`, `senal`, `ultimo_contacto` y `leido_en`: sin `serie`, `mac`, `modelo`, `firmware`, IP ni datos de red. Iris tampoco le pasa esos campos al modelo aunque los tenga.

### 12.4 Cambiar el Wi-Fi pedido desde Iris (A35)

`POST /api/v1/servicios/:id/wifi` (`servicios.cambiar_wifi`), `Idempotency-Key` obligatoria, con las cabeceras de §7.1 (`X-Iris-Actor` y, desde una conversación, `X-Iris-Conversacion`): `{ "ssid": "…", "clave": "…", "origen_tipo": "iris_orden" | "iris_ticket" | "iris_conversacion", "origen_id": "…" }` (al menos uno de `ssid` y `clave`).

- `202 { solicitud_id, estado: "en_proceso" }`. Resultado por el webhook `equipo.wifi_resuelto` (`aplicado` o `fallido` con `causa`: `equipo_sin_gestion`, `equipo_sin_respuesta`, `rechazado_por_equipo`, `otro`).
- Errores inmediatos: `409 equipo_sin_gestion`, `422 wifi_invalido`.
- Sirve para FTTH (ONT por SmartOLT o router por TR-069) y para wireless gestionado por TR-069.
- **`verificacion` (obligatoria con cualquier actor):** `{ "metodo": "…", "verificado_en": "<RFC 3339>", "cliente_id": <id de Argos> }`, cómo se comprobó que quien pide es el titular. Argos responde `422 verificacion_requerida` si falta, si el `metodo` no vale para ese actor o si `cliente_id` no es el cliente del servicio.
    - **Asistente de IA** (`X-Iris-Actor: asistente_ia`, con `X-Iris-Conversacion`): `metodo` = `codigo_telefono` o `codigo_correo` (código de un solo uso que Iris manda al teléfono o al correo **registrados en Argos**: solo los del cliente y confirmados, §7.5, nunca los de contactos) o `datos_titular` (solo si en esa conversación coincidió el `documento` del desafío de §7.5; `nombre` y `codigo_cliente` nunca bastan, ni juntos; un cliente `cf` sin documento, solo los códigos). Es el nivel que pide el portal de Argos. Más de **10 min** desde `verificado_en`: `422 verificacion_requerida`. En Iris, la verificación vale para **un solo cambio** y caduca si cambia el cliente asociado a la conversación. Iris solo lo manda si la empresa habilitó la acción y el asistente tiene `servicios.cambiar_wifi`.
    - **Persona** (`usuario:<id>`): además de los anteriores, `presencial` (el técnico en sitio, en su orden en curso) o `llamada_verificada` (soporte confirmó al titular con el desafío de §7.5). Argos lo guarda en la auditoría.
- **El asistente nunca pide ni recibe la clave.** Cuando el cliente verificado quiere cambiar el Wi-Fi, Iris le manda un **enlace de un solo uso** (10 min, ligado a la conversación, al servicio y a esa verificación) a un formulario de Iris donde escribe el SSID y la clave; desde ahí sale esta llamada (con `X-Iris-Actor: asistente_ia` y `origen_tipo: iris_conversacion`). Una clave escrita en el chat se reemplaza por `***` antes de llegar al modelo y al historial, y **no se usa**: el asistente le pide usar el enlace. El mensaje de confirmación no la repite.
- **Límites y aviso:** como máximo **3 cambios por servicio al día**, sumando todos los actores (`429 demasiados_cambios_wifi` con `Retry-After`). Después de `aplicado`, Argos avisa al titular por sus contactos registrados («Se cambió el Wi-Fi de tu servicio; si no fuiste tú, comunícate»).
- Argos audita el cambio con el actor de §7.1 (canal `ia` para el asistente), la `verificacion` completa y la clave como `***`.
- Permisos de Iris (informativo): `servicios.cambiar_wifi`; un técnico solo sobre sus órdenes en curso.
- `GET /servicios/:id/equipo` (§12.3) y esta ruta exigen permisos separados en el token de Iris: `servicios.ver_equipo` y `servicios.cambiar_wifi`.

### 12.5 Mensaje de bienvenida al activar (A37, B16)

Al quedar activo un servicio, el cliente recibe la bienvenida con sus datos y las cuentas para pagar. Argos decide cuándo (al activarse, por cualquier vía: `activar-instalacion` o `instalacion.aprovisionada`) y por qué canales (WhatsApp por Iris, §6; correo y Telegram por su `notify`). La orden de Iris la **sugiere y la valida**: muestra si salió y ofrece reenviarla. No bloquea el cierre de la orden salvo que la empresa lo configure en Iris.

- **Plantilla `bienvenida_servicio`** (§6.3): variables de Argos `nombre`, `plan`, `precio_mensual`, `moneda` y `codigo_cliente`. Las **cuentas para pagar las inserta Iris** con su instantánea de §7.8 (solo activas, de la empresa del servicio, en `GTQ` y con `titular`). Argos **nunca** manda números de cuenta en las variables. Sin cuentas válidas, Iris usa la variante sin cuentas y el aviso queda con `detalle: sin_cuentas`. Con un Iris < 0.9.0, o desde 0.14.3 con `capacidades.equipos.bienvenida: false` para la empresa, Argos sigue pidiendo `bienvenida`; Iris acepta cualquiera de las dos y no rechaza por la marca.
- **Validación de las variables** (Iris, antes de encolar): `nombre` hasta 60 caracteres y `plan` hasta 80, sin saltos de línea, sin URLs ni dominios y sin secuencias de 4 o más dígitos (la lista blanca de §6.3); `codigo_cliente` `^[A-Za-z0-9-]{1,30}$`; `precio_mensual` decimal con dos decimales; `moneda` ISO 4217. Fuera de forma: `422 validacion` (con `errors[].field`) y el aviso no sale.
- **Cuentas recién cambiadas:** una cuenta creada o editada en Argos (`cuenta_bancaria.cambiada`) no entra en la bienvenida durante **24 h**, o hasta que la confirme un admin de Iris.
- **`origen` en §6.2** (opcional, solo para `bienvenida_servicio`): `{ "tipo": "iris_orden", "id": "<orden_id>" }`, cuando la activación vino de una orden de Iris (`origen_tipo`/`origen_id` de `activar-instalacion` o de `aprovisionar`); `null` (o ausente) si la activación fue desde el panel de Argos. Iris lo usa para mostrar el envío en la orden. Fuera de forma: `422 origen_invalido`. Desde 0.14.3, sin `origen` Iris relaciona el envío con una orden solo si hay **exactamente una** orden de instalación de ese servicio y de esa empresa, no anulada, abierta o cerrada como exitosa en los **7 días** anteriores a la recepción del aviso en Iris (`creado_en` del aviso de Iris), cuyo `cliente_id` sea el titular actual del servicio; si no, queda sin relacionar e Iris no inventa el origen. Esa relación se muestra como «relacionada por servicio», no cuenta para `cierre_exige_bienvenida`, no se manda a Argos como `origen` y no habilita «Reenviar» con ese `origen_id`.
- **Resultado de todos los canales:** webhook `servicio.bienvenida_cambiada` (§2.3), con el estado de cada canal (`whatsapp`, `correo`, `telegram`: `pendiente`, `enviada`, `fallida` u `omitida`, más `motivo`). En WhatsApp, Argos reenvía lo que Iris le informó con `aviso.entregado` / `aviso.fallido` (§6.6).
- **Reenviar:** `POST /api/v1/servicios/:id/bienvenida/reenviar` (`servicios.reenviar_bienvenida`), con `Idempotency-Key = iris:bienvenida:<orden_id>:<n>`, cuerpo `{ "origen_tipo": "iris_orden", "origen_id": "…", "canales": ["whatsapp", "correo", "telegram"] }` (`canales` opcional: sin él, todos los que Argos tenga configurados). Argos la vuelve a disparar por sus canales con las mismas reglas (permisos de contacto, límites y horario de silencio de §6.5). `202 { estado: "en_cola" }`. Errores: `409 servicio_no_activo`; `429 bienvenida_reciente` antes de 10 min del último envío; `429 demasiados_reenvios` después de **3 reenvíos por servicio en el día** (los dos con `Retry-After`).
- **Lo que hace Iris con eso** (informativo): bloque «Bienvenida» en la orden, botón «Reenviar bienvenida» si no salió o falló, bandeja «Bienvenidas sin enviar», y el ajuste por empresa o plantilla `cierre_exige_bienvenida` (apagado de fábrica). Con ese ajuste y el modo «exigir la activación lista», el cierre exige la bienvenida enviada por algún canal, o no enviada porque el cliente no acepta avisos (`sin_permiso`, `baja`).

## 13. Tipo de servicio, corte manual y recordatorios (0.15.0, propuesta aceptada con ajustes)

**Estado: propuesta de Argos, aceptada por Iris con ajustes (26 nov 2026), pendiente de la PR espejo.** La línea de versión sigue en 0.14.3 hasta que se fusionen los dos lados; hasta entonces nada de esta sección se envía ni se espera. Es aditiva sobre la 0.14.3, con tres bloques independientes (§4.6). Los ajustes de Iris y las decisiones del líder están en §10.14.

**Marcas y relectura (G1 a G3).** Las marcas de los tres bloques (`tipo_servicio`, `corte_manual`, `mensajeria[]`) se agregan a las `capacidades` de §4.3. Argos las lee solo de `GET /integraciones/argosisp/capacidades`, nunca de `…/estado`. `mensajeria[]` trae solo las empresas que están en el alcance del conector. Argos las relee al activar la integración, en la revisión periódica de versión y ante un `422 canal_no_permitido` o un aviso `auto` que vuelve `bloqueado` con `canal_no_disponible`. **Iris aún no expone `GET …/capacidades` (B16 de la 0.14.2); B33, B34 y B35 dependen de esa ruta.**

### 13.1 Bloque A: tipo de servicio (A53 / B33)

- `GET /api/v1/servicios/:id` y la lista (`GET /servicios`, `GET /servicios?cliente_id=`) suman `tipo_servicio` (`internet` por defecto, `cable`, `iptv`, `mixto`) y `componentes[]` (los de `internet`, `cable`, `iptv` que incluye; en los tipos simples, el mismo tipo). La primera sincronización (§2.5) y cada relectura de `GET /servicios` traen los dos campos. `router_id` y `router_nombre` pueden ser `null` en `cable`. `GET /planes` suma los mismos dos campos y el filtro `tipo_servicio`: `GET /planes?tipo_servicio=cable` lista los planes de cable. En los planes de cable, `bajada_kbps` y `subida_kbps` pueden ser `null`.
- **Qué tiene router y velocidad (A1).** Un servicio tiene router y velocidad si sus `componentes` incluyen `internet` o `iptv`. Un `tipo_servicio` desconocido se trata como `internet`, salvo que `router_id` sea `null`: en ese caso no tiene router.
- **Rutas con un servicio de cable (A3).** `POST /servicios/:id/reservar-ip`, `POST /servicios/:id/aprovisionar`, `GET /planes?router_id=` y `POST /servicios/:id/cambiar-plan` responden `422 tipo_sin_router` y no hacen ningún cambio. **Desde 0.15.1** `cambiar-plan` deja de estar en esta lista: cruza tipos según §13.4.
- Webhooks (solo a conectores ≥ 0.15.0): `servicio.creado` con `tipo_servicio` y `componentes[]`; `servicio.config_cambiada` con `tipo_servicio` en `campos[]`; `catalogo.cambiado` de plan con `tipo_servicio` y `componentes` en `campos[]`. **Si cambia el tipo de un plan o de un servicio, cada servicio afectado emite `servicio.config_cambiada` con `tipo_servicio` en `campos[]` (A5); `catalogo.cambiado` solo no alcanza.** Todo se relee con los GET de arriba.
- **Qué hace Iris:** guarda ambos campos en la instantánea (lista blanca); muestra el tipo en la ficha y en la orden; para un `cable` no pide router ni velocidad, ni ofrece «velocidad contratada» ni cambio de router; tolera `router_id` null en todo lo que hoy lo espera. Un servicio `iptv` se trata como un servicio de datos. **La factibilidad de un servicio de cable es `indeterminado`, con motivo `tipo_sin_router`, hasta que Argos exponga la planta CATV (A4).**
- Marca: `capacidades.tipo_servicio = true`.

### 13.2 Bloque B: corte manual de respaldo (A54 / B34)

El corte y la reconexión del CATV se hacen por SmartOLT (`disable_catv`, `enable_catv`). **Solo** cuando eso no es posible, Argos pide a Iris que alguien lo haga en campo o en la OLT: el servicio no tiene ONU registrada, su instancia de SmartOLT no está operativa, la orden agotó sus reintentos o la ONU no admite CATV.

- **Argos → Iris**, webhooks de §2.3 `servicio.corte_manual_solicitado` y `servicio.corte_manual_cancelado`. Cuerpo del payload: `solicitud_id` (entero de Argos, estable; es la clave de idempotencia en Iris), `version` (entero creciente por `solicitud_id`), `solicitada_en`, `servicio_id`, `empresa_id`, `cliente_id`, `componente` (`cable`), `accion` (`corte` | `reconexion`; también en `cancelado`), `causa` (`sin_onu` | `instancia_no_operativa` | `smartolt_fallo` | `catv_no_soportado`). Sin nombre, teléfono ni dirección: Iris los toma de su instantánea. A lo más una solicitud abierta por servicio y acción.
- **Iris → Argos**, webhook de §6.6 `corte_manual.estado_cambiado`: `solicitud_id`, `empresa_id`, `version`, `estado` (`en_curso` | `resuelta` | `cancelada` | `no_realizada`), `motivo_codigo` (solo con `no_realizada`; lista cerrada: `sin_acceso`, `equipo_no_encontrado`, `otro`), `referencia` (texto o null; el número de la orden de Iris), `referencia_tipo` (`orden` | `ticket`, o null), `ocurrido_en`. El campo `ticket_numero` de la propuesta original se reemplaza por `referencia` y `referencia_tipo`. Argos lo deduplica por `X-Webhook-Id` y guarda el estado y la referencia para mostrarlos en la ficha del servicio («Corte pendiente en Iris …»).
- **Orden de los mensajes (B3).** Cada `solicitud_id` lleva una `version` creciente. Argos solo aplica una versión mayor que la que tiene, y los estados terminales (`resuelta`, `cancelada`, `no_realizada`) no retroceden. Son dos contadores independientes por `solicitud_id`: el de Argos va en `servicio.corte_manual_solicitado` y `servicio.corte_manual_cancelado`, y el de Iris en `corte_manual.estado_cambiado`. Cada lado solo compara entre sí las versiones del otro.
- **Qué hace Iris:** al recibir `solicitado`, abre (idempotente por `solicitud_id`) una **orden de trabajo nueva** de tipo `corte_catv` o `reconexion_catv` para el servicio, con la `causa` visible; avisa `en_curso` al asignarla, `resuelta` al cerrarla con el corte o la reconexión hecha, y `no_realizada` si el técnico no pudo, con su `motivo_codigo`. La reconexión siempre tiene prioridad alta en Iris (B5).
- **Cancelaciones fuera de orden (B4).**
    - `cancelado` llega con la orden sin hacer: Iris la cancela y avisa `cancelada`.
    - `cancelado` llega tarde y el corte ya se hizo: Iris informa `resuelta` (no `cancelada`) y Argos pide la reconexión con una solicitud nueva de `reconexion`.
    - `cancelado` llega antes que `solicitado`: Iris la guarda y no abre nada; cuando llega `solicitado` con una versión menor o igual, no abre la orden.
    - Si Argos reactiva el servicio con un corte `resuelta`, llega una solicitud nueva de `reconexion`.
- **Alcance y errores (B9).** Si la empresa está fuera del alcance del conector o el servicio es desconocido, Iris guarda la solicitud, no abre nada y no devuelve error (responde `2xx`). Si a las 24 h de `solicitada_en` no hubo `en_curso`, Argos la muestra como `sin_respuesta`. Ese estado existe solo en Argos (Iris nunca lo envía ni lo recibe) y no es terminal: lo reemplaza un `en_curso` o un estado final que llegue después. Un nuevo intento después de `no_realizada` es una solicitud nueva, con otro `solicitud_id`.
- **Qué no hace Iris.**
    - La orden de corte **no aparece en el portal de Argos** (§11) ni genera eventos `ticket.*` hacia Argos (B6).
    - Iris no avisa al cliente por su cuenta; esos avisos los pide Argos (B6).
    - **Cerrar la orden no llama a `activar-instalacion` ni a retiro.** El resultado viaja solo en `corte_manual.estado_cambiado` (B7).
    - En un servicio `mixto` solo se toca el componente `cable` (B8).
- **Sin Iris o con contrato anterior:** Argos deja la solicitud `sin_iris`, la muestra en la ficha del servicio y genera una alerta interna. No hay cola de cortes en Argos.
- Marca: `capacidades.corte_manual = true`. No hay permisos nuevos en el token de Iris: el webhook usa el receptor existente.

### 13.3 Bloque C: recordatorios entregados por Iris (A55 / B35)

Argos decide **qué y cuándo** (sus plantillas de recordatorio: vencimiento, corte próximo, promesa por vencer) y Iris **entrega** por el canal que la empresa tenga declarado. Hoy Iris entrega solo por WhatsApp: no tiene Telegram ni correo al cliente; lo que no declara lo sigue enviando Argos.

- **Qué declara Iris (C1, C8).** `capacidades.mensajeria` (por empresa): `[{ empresa_id, entrega_auto: boolean, canales: [{ canal, codigos: string[] }] }]`. `canales` lista **solo** los canales que Iris implementa y tiene configurados para esa empresa, y `codigos` son los de §6.3 aprobados y usables **en ese canal**. `entrega_auto` es `true` solo si la empresa tiene al menos un canal declarado (C12). Un código que no está en `codigos` del canal no se pide. Argos lo combina con las plantillas de §6.7 (A52) para mostrar en su panel qué pasos pueden salir.
- **Canal (C1, C2, C10).** `POST /api/v1/mensajeria/avisos` (§6.2) acepta `canal_preferido: "auto"` y `canales_permitidos[]`. `canales_permitidos` es la **intersección** del paso con los canales que Iris declara para la empresa; los canales que Iris no declara los sigue enviando Argos. Si la intersección queda vacía, no se usa `auto`. Responden `422 canal_no_permitido`: `canales_permitidos` enviado sin `auto`, una lista vacía y valores repetidos; solo la ausencia de la lista significa «todos los declarados». Los avisos de Argos nunca salen por WhatsApp QR (§6.1).
- **Un canal por aviso (C2).** Con `auto`, Iris usa un solo canal por aviso, en el orden de la empresa. Pasa al canal siguiente solo antes del primer envío y solo por causas del canal (sin permiso, sin contacto o canal caído). Un fallo después del envío es final. Hay un único webhook final (`aviso.entregado` o `aviso.fallido`), con el canal real. Con `auto`, Argos **no** manda ese aviso por su correo ni Telegram, para que no salga dos veces. **Desde 0.15.1:** si el canal deja de servir después del `202` y antes del primer envío, y no queda otro canal declarado, Iris emite `aviso.bloqueado` (§6.6) y Argos lo reenvía por sus canales como en C12.
- **Un `envio_id`, un canal (C14).** Un mismo `envio_id` nunca se reenvía con otro `canal_preferido`; si cambia el canal, el `envio_id` es nuevo (y la `Idempotency-Key` también).
- **Contactos (C13).** Con `auto`, el correo va solo al titular. Los contactos con `avisos` (§3.5) reciben solo por WhatsApp, con el permiso de cada número.
- **Bajas y permisos (C3, decisión del líder, opción b).** Los avisos de cobro por correo y por Telegram son transaccionales: no piden alta, pero respetan la baja. `contacto.baja` admite `correo` (enlace de baja y `List-Unsubscribe`) y `telegram` (`/stop`), y Argos respeta esas bajas también cuando envía él (§6.4).
- **Telegram (C4, decisión del líder).** Telegram sigue en Argos. El `chat_id` es por bot. Iris declarará `telegram` solo cuando se acuerde, en otra versión, si Iris usa el bot de la empresa (con su token cargado en la configuración de Iris, nunca en el contrato) o si los clientes se vuelven a vincular.
- **Códigos nuevos** (§6.3): `corte_proximo` (`monto`, `moneda`, `fecha_corte`), `promesa_por_vencer` (`monto`, `moneda`, `vence_en`), `debito_fallido` (`monto`, `moneda` y `reintenta_el`, **opcional**: se omite cuando no hay valor y nunca se manda vacío, porque Meta rechaza parámetros vacíos; C6), `debito_desactivado` (sin variables), `suscripcion_suspendida` y `suscripcion_reactivada` (`concepto`).
- **`concepto` (C7).** Opcional en `factura_por_vencer` y `factura_vencida`: nombre del producto de una suscripción, de lista cerrada del catálogo de Argos; nunca texto libre. Máximo 60 caracteres, sin saltos de línea ni URL; si no cumple, `422 variable_invalida`. Nunca lleva datos de tarjeta ni del débito.
- **WhatsApp (C11).** Sale del número de avisos de la empresa (o del número prestado; referencia informativa: contrato propio de Iris `.claude/integration/contratos/conversaciones.md` §15.3 «Prestar un número a otras empresas», que no se espeja), con plantillas de la cuenta de WhatsApp Business dueña del número (con préstamo, la de la empresa que presta). Meta le cobra a esa cuenta; Iris no factura mensajes y el reparto entre empresas queda fuera del contrato. Los códigos nuevos van como `utility`. Si Meta pasa uno a `marketing` y la empresa no lo permite, ese código sale de `codigos` y Argos usa su texto local.
- **Cuerpo.** Suma `suscripcion_id` (opcional) y `recordatorio: { disparador, orden }` (opcional, solo informativo para la bandeja). La `Idempotency-Key` no cambia: `argos:aviso:<plantilla>:<envio_id>`. Argos sigue sin mandar el número ni el correo del cliente.
- **Entrega (C5).** `aviso.entregado` trae el `canal` real y Argos deja el envío `enviado` con ese canal. En correo, `entregado_en` es la aceptación del SMTP de destino y `leido_en` es siempre `null`; el motivo de `aviso.fallido` por un correo inválido es `destino_invalido`. `bloqueado` sigue como hoy: Argos no reintenta, **salvo** el caso de C12.
- **`canal_no_disponible` con `auto` (C12).** Si un aviso `auto` vuelve `bloqueado` con `canal_no_disponible`, Argos relee `capacidades` y reenvía por sus propios canales con otro `envio_id`.
- **Vencimiento de cada código dentro del horario de silencio (C9).**

| Código | Vence |
| --- | --- |
| `corte_proximo` | `fecha_corte` |
| `promesa_por_vencer` | `vence_en` |
| `debito_fallido` | `reintenta_el`, o a las 72 h si no viene |
| `factura_por_vencer` | `vence_en` (sin cambios desde la 0.6.0) |
| `factura_vencida` y los demás | A las 72 h |

  Un aviso que vence se descarta con `aviso.fallido` y `motivo = vencido`. El silencio y el tope diario aplican a todos los canales.
- **Reglas de §6.5 sin cambios**, y las aplica Iris.
- **Argos sin esta marca de Iris** (o con Iris < 0.15.0) sigue como hoy (§4.6).

### 13.4 Cambiar el tipo de servicio (0.15.1, A56 / B36)

**Estado: propuesta de Iris por pedido del PM, con el diseño de Argos (§10.15); pendiente de la PR espejo.** Parche aditivo sobre la 0.15.0. Cada lado lo usa solo si el otro informó un contrato ≥ 0.15.1; con una versión anterior, el cambio de tipo lo hace una persona en Argos.

- **Servicio pendiente (`pendiente_instalacion`): `cambiar-plan` (A40, §3.4.5).** Acepta un plan de otro tipo. Si el plan nuevo agrega `internet` o `iptv`, el cuerpo suma `router_id` (sin él, `422 router_requerido`). Si quita `internet`, Argos libera la IP reservada. Sin factura, como hasta ahora.
- **Servicio activo: `POST /servicios/:id/cambiar-tipo`** (`servicios.cambiar_tipo`), `Idempotency-Key = iris:tipo:<orden_id>:<cambio_id>`, cuerpo `{ plan_id, router_id?, motivo, motivo_texto?, version, origen_tipo: "iris_orden", origen_id }`. `motivo`: `agregar_internet`, `agregar_cable`, `quitar_internet`, `quitar_cable` u `otro` (con `motivo_texto`). Solo con el servicio `activo` (`422 servicio_no_activo`). Responde `200` con el servicio. Según el cambio:
    - **Agrega `internet` o `iptv`:** exige `router_id` (`422 router_requerido`). Argos reserva la IP en ese router, aprovisiona y emite `asignacion.cambiada`. Si el aprovisionamiento no termina en la misma llamada, sigue las reglas y los estados de §12.2.
    - **Quita `internet`:** Argos libera la IP, desconecta la sesión por CoA y borra el `/ppp secret`. Recuperar un router en comodato sigue §3.10 (Iris abre la orden de retiro si hace falta).
    - **Agrega `cable`:** Argos enciende el CATV por SmartOLT. Si no puede, pide la reconexión manual de §13.2.
    - **Quita `cable`:** Argos corta el CATV por SmartOLT. Si no puede, pide el corte manual de §13.2.
    - **Factura:** Argos ajusta el periodo en curso con la misma regla de prorrateo que el cambio de plan de su panel (nota de crédito o cargo proporcional). Iris no calcula montos.
    - **Errores:** `422 router_requerido`, `422 servicio_no_activo`, `422 plan_no_permitido` (plan inactivo, de otro `modo` o no asignado al router), `409 conflicto_version`. No cambia el estado del servicio, así que no emite `servicio.estado_cambiado`.
- **Webhook:** los dos casos emiten `servicio.config_cambiada` con `tipo_servicio`, `componentes`, `plan` y `router` en `campos[]` (`origen_tipo`/`origen_id` de la orden). Iris relee `GET /servicios/:id`.
- **`422 tipo_sin_router`** queda solo para `reservar-ip`, `aprovisionar` y `GET /planes?router_id=` mientras el servicio no tenga `internet` ni `iptv`.
- **Planes que ofrece Iris:** `GET /planes?tipo_servicio=&router_id=&modo=` (§13.1 y §3.4.5); Iris muestra el precio del plan nuevo antes de confirmar.
- **Qué hace Iris (B36):** «Cambiar tipo de servicio» en la ficha abre una orden `cambio_tipo` con el plan destino. Cuando se agrega internet o cable, la orden lleva visita: el técnico configura el equipo y, con internet, elige el router. Al cerrarla, Iris llama a `cambiar-tipo` (o a `cambiar-plan` si el servicio está pendiente) desde su cola de pendientes. Un `422` se muestra al supervisor y no se reintenta.
- **Permiso:** `servicios.cambiar_tipo` es nuevo; Argos migra los tokens que ya existen.
