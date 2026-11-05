Versión: 0.6.3 (5 nov 2026, borrador) · Iris ↔ ArgosISP

# Contrato de integración Iris ↔ ArgosISP (copia de ArgosISP)

## Gobierno

- Este texto es la fuente de verdad compartida. Vive con el mismo contenido en `HCDestroyer/iris` (`.claude/integration/contratos/argosisp.md`) y en `HCDestroyer/argos-isp` (`.claude/integration/contratos/iris.md`). La línea de versión identifica la copia al día.
- Cada sistema toca solo su código. Lo que uno necesita del otro se pide en §5, y el dueño lo implementa en su repositorio.
- Cambiar el contrato requiere dos PRs enlazadas, una por repositorio. La versión sigue semver.
- Se reutilizan los mecanismos ya probados del contrato Argos ↔ Horus (`argos-isp/.claude/integration/contratos/horus.md` §1): códigos de conexión, webhooks firmados, enlace de conexión y autologin por ticket. Aquí solo se escriben las diferencias.

## 0. Principios

| Regla | Detalle |
| --- | --- |
| Fuente de verdad | Argos es dueño de clientes, servicios, planes, routers, zonas, facturación e inventario. Iris es dueño de tickets, órdenes, agenda, rutas, prospectos y evidencias. |
| Independencia | Iris funciona sin Argos: con el conector apagado, los tickets y órdenes usan los datos capturados a mano. Argos nunca depende de Iris para AAA, facturación ni corte. |
| Degradación | Si Argos no responde, Iris muestra la última instantánea marcada `stale`. Las acciones hacia Argos quedan en cola de pendientes y se reintentan (1 min, 5 min, 30 min, 2 h, 12 h; después, alerta al supervisor). |
| Mínimo privilegio | El token de Iris en Argos lleva solo los permisos de §1.1, con alcance de empresas acotado. |
| Idempotencia | Toda escritura de Iris en Argos lleva `Idempotency-Key = iris:<operacion>:<id de la orden u operación>`. Los webhooks llevan un `id` estable y el receptor deduplica. |
| Auditoría | Argos audita las acciones del usuario técnico «Iris» con actor `token` y la referencia de la orden (`origen_tipo = 'iris_orden'`, `origen_id`). El canal es el de §7.1: `ia` con `X-Iris-Actor: asistente_ia` y `api` en el resto (`usuario:<id>` y las llamadas sin `X-Iris-Actor`). Los webhooks de §6.6 quedan con canal `webhook`. Iris audita sus lecturas de datos personales. |

## 1. Conexión, firma y autologin

### 1.1 Iris → Argos (Iris lee y escribe en Argos)

Iris guarda, cifrado y de solo escritura, un token de Argos `atk_…` del usuario técnico «Iris» (rol creado por el seed de Argos), con estos permisos:

- **Lectura:** `clientes.ver`, `clientes.estado_cuenta`, `servicios.ver`, `planes.ver`, `routers.ver`, `zonas.ver`, `ip.resolver`, `inventario.ver`.
- **Instalación y retiro:** `servicios.activar_instalacion` (pedido A1), `servicios.retirar`, `inventario.asignar_comodato`, `inventario.devolver_comodato`, `inventario.declarar_perdida_comodato`, `inventario.consumir`, `inventario.recibir_traslado` (0.5.0) y `facturas.emitir_cargo_unico`.
- **Comercial:** `clientes.crear` y `servicios.crear`.
- **Autologin:** `sso.emitir`.
- **Pagos y atención (0.6.0, §7, con los nombres de §9.1):** `comprobantes.registrar` (+ `comprobantes.ver` y `cuentas_bancarias.ver`), `comprobantes.reconexion_temporal`, `promesas.ver` (simulador, §7.4), `promesas.crear` y `clientes.verificar_identidad`. La búsqueda por teléfono (§7.6) es el filtro `telefono` de `GET /clientes`, con `clientes.ver`.

El usuario técnico Iris solo tiene alcance por empresas (o global), nunca por zona ni router: una lista completa y vacía significa sin servicios. Argos lo hace cumplir: un token, un rol o un correo que le darían alcance por zona o router responde `422 usuario_tecnico_solo_empresas`, y el canje nunca emite un token así.

### 1.2 Argos → Iris (Argos lee de Iris)

Token `iris_pat_<32B base62>` con `tickets.ver`, `tickets.crear`, `ordenes.ver` y `sso.emitir`. Argos lo usa para mostrar los tickets y las órdenes de un cliente en su ficha y para abrir tickets desde el portal del cliente.

**0.6.0:** el mismo token suma `mensajeria.enviar` y `mensajeria.ver` para usar el servicio de mensajería de §6. Son permisos de la integración `integracion_argos`; un usuario de Iris no los recibe por rol.

### 1.3 Webhooks, enlace de conexión y autologin

Igual que en el contrato Argos ↔ Horus, §1.3–§1.5:

- **Webhooks:** cabeceras `X-Webhook-Id`, `X-Webhook-Timestamp` y `X-Webhook-Signature: v1=…`, con HMAC-SHA256. El receptor genera el secreto. Reintentos a 1 min, 5 min, 30 min, 2 h y 12 h.
- **Enlace de conexión:** `https://<A>/conectar#<codigo>`, válido 15 min y de un solo uso. El canje es servidor a servidor:
    - en Argos, `POST /api/v1/integraciones/iris/conexion/canjear`;
    - en Iris, `POST /api/v1/integraciones/argosisp/conexion/canjear`.
    - `sistema` vale `"iris"` o `"argosisp"`. El conector queda apagado hasta que un administrador lo active.
    - El canje de Argos responde `{ token, webhook: { url, secreto }, conector_id, alcance_global, empresas[] }`. `empresas[]` son las empresas activas del alcance del usuario técnico «Iris» al canjear, en orden de id, cada una `{ id, nombre, nit }` (`nombre` = nombre comercial; `nit` sin guiones, o `null` si la empresa aún no lo tiene). `alcance_global: true` significa que Iris verá también las empresas que se creen después; `false`, solo las listadas.
    - El host de `webhook.url` es siempre el de la base que Iris usó para canjear: el `<base>` del enlace (§1.3) o `api=` del botón (§1.4). Nunca el del panel: en producción la API y el panel pueden estar en dominios distintos. Si falta `webhook` o su host es otro, Iris rechaza el canje con `422 webhook_invalido` (0.6.2).
    - **Canje rechazado o incompleto (0.6.3).** Cuando Argos responde `200` al canje pero Iris no lo completa por cualquier motivo (`422 webhook_invalido`, un error al guardar, un pánico), Iris llama a `POST /api/v1/integraciones/iris/conexion/revocar` con el `token` (`atk_`) de esa respuesta, sin cuerpo, y lo reintenta en segundo plano (solo `5xx` y timeouts). Argos revoca ese token. Si es el de una reconexión pendiente, la descarta y el conector sigue igual. Si es el del conector, lo deja **desconectado**: apagado, sin el `iris_pat_` ni los secretos de webhooks, sin borrar el historial. Responde `204`. Repetirlo con el mismo token responde `401`, porque el token ya no autentica. Para Iris, `204` y `401` significan lo mismo: el token ya no sirve y no se reintenta. Con otra credencial, `403`. Si Iris no recibió la respuesta del canje (timeout), puede repetir la misma petición mientras el código no venza para obtener el token y revocarlo. Repetir un canje cuyo token ya se revocó responde `410 enlace_vencido`.
    - **Canje sin uso (0.6.3).** El canje queda pendiente de confirmar hasta la primera llamada autenticada con su `atk_`: cualquiera, sea `PUT /integraciones/iris/contrato`, una lectura o una escritura. Si pasan 24 h sin ninguna, Argos revoca el token y desconecta el conector solo; en una reconexión solo descarta el canje nuevo (punto siguiente). Un Iris que acepta el canje usa el token enseguida, al activar su conector, así que no nota nada. Los conectores anteriores a 0.6.3 cuentan como usados. `GET /integraciones/iris` (panel de Argos) muestra `estado_canje` (`pendiente`, `confirmado`, `desconectado` o `null` sin enlace), `canje_vence_en`, `desconectado_en` y `motivo_desconexion` (`revocado_por_iris` o `canje_sin_uso`). Volver a conectar es otro canje.
    - **Reconexión (0.6.3).** Si el conector de Argos ya está en uso (lo usó el `atk_` de un canje anterior, o se configuró a mano), un canje nuevo no lo reemplaza enseguida: queda pendiente hasta la primera llamada autenticada con su `atk_`. Iris hace esa llamada (el PUT del contrato) solo cuando el conector nuevo está activo de su lado. Hasta entonces, el conector anterior sigue igual: su `atk_`, la URL de Iris, el `iris_pat_`, los dos secretos de webhooks, `contrato` y `receptor_argos`. En la primera llamada del `atk_` nuevo, antes de atenderla, Argos aplica todo lo que trajo el canje nuevo y revoca el `atk_` anterior; el encendido del conector no cambia. Si el `atk_` nuevo se revoca o vence sin uso en 24 h, Argos lo descarta y el conector anterior queda vivo y sin cambios. Un tercer canje antes de confirmar reemplaza al pendiente, cuyo `atk_` se revoca. Si el conector no está en uso (no existe, está desconectado o su canje nunca se usó), el canje lo reemplaza al momento y revoca el `atk_` anterior. Así, de los canjes nunca quedan más de dos `atk_` vivos, el que está en uso y uno pendiente, y el pendiente vence solo. `GET /integraciones/iris` muestra `reconexion_pendiente_desde` y `reconexion_vence_en`.
- **Autologin:** `POST /api/v1/<prefijo>/sso/tickets` devuelve una URL `/sso/entrar#<ticket>`, válida 60 s y de un solo uso, con vinculación de usuarios y sin crearlos nunca.
    - Destinos en Argos: `cliente` y `servicio`.
    - Destinos en Iris: `ticket`, `orden` y `prospecto`.
    - Autologin desde Argos («Ver en Iris»): el cuerpo de `POST <iris>/api/v1/integraciones/argosisp/sso/tickets` es `{ usuario: { id_externo, correo, nombre, correo_verificado }, destino }`. `usuario.correo_verificado` (boolean): Argos hoy siempre manda `false` (no verifica correos); con `false` o ausente, Iris no vincula por su cuenta y el vínculo lo hace un admin de Iris.

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
    - Iris muestra el resumen y el conector queda **apagado** hasta que el admin pulse «Activar», como hoy.
6. **`panel_url` en el canje.** Las dos variantes del canje responden además `panel_url` (`PANEL_URL_PUBLICA` de Argos, por ejemplo `https://argos.kns.gt`). Iris guarda por separado `panel_url`, para «Ver en Argos» y para volver a autorizar, y `api_url`, el `api` del paso 5 o el origen del enlace de §1.3.

## 2. Iris consume de Argos

### 2.1 Búsqueda y ficha

| Uso en Iris | Llamada a Argos (API existente) |
| --- | --- |
| Buscar cliente al crear un ticket u orden | `GET /api/v1/clientes?q=&limit=` |
| Ficha del cliente con sus servicios | `GET /api/v1/clientes/:id` y `GET /api/v1/servicios?cliente_id=` |
| Estado de cuenta resumido (en la ficha del ticket) | `GET /api/v1/clientes/:id/estado-cuenta`, solo el saldo y las facturas vencidas (requiere `clientes.estado_cuenta`) |
| Cliente por IP (ticket desde Horus o desde el portal) | `POST /api/v1/integraciones/iris/resolver` con la IP en el cuerpo (pedido A8; hoy solo existe `GET /ip/{ip}` y el resolver del conector Horus) |
| Planes para cotizar | `GET /api/v1/planes?activo=true&router_id=<router sugerido>` (planes globales de la instalación más los implementados en ese router; `precio` como string decimal; sin `empresa_id`) |
| Routers, zonas y coordenadas para factibilidad | `GET /api/v1/routers` (cada router trae `coordenadas`: `"lat,lng"` en grados decimales, sin espacios, o `null` si no está ubicado; el mismo formato que `coordenadas` de clientes y servicios) y `GET /api/v1/zonas` |
| Equipos en comodato de un servicio | `GET /api/v1/servicios/:id/equipos` |
| Bodega y kardex del técnico (0.5.0) | `GET /api/v1/inventario/mi-bodega?id_externo=<usuario de Argos>` (o `?correo=`): `{ bodega, existencias[], unidades[], traslados_pendientes[], kardex }` (últimos 50; más con `GET /api/v1/inventario/movimientos?bodega_id=` y cursor). Sin bodega activa: `404 tecnico_sin_bodega`. `GET /api/v1/inventario/existencias?bodega_id=` sigue igual |
| Ubicación y contacto en sitio (0.5.0) | `Cliente.referencia`; `Servicio.referencia`, `Servicio.contacto_nombre` y `Servicio.contacto_telefono` (null si no hay) |

### 2.2 Instantánea en Iris

Iris guarda en `integracion.argos_clientes` y `integracion.argos_servicios` solo lo que muestra: nombre, código, teléfono, `email`, dirección, coordenadas, `referencia` (cliente y servicio), contacto en sitio (`contacto_nombre`, `contacto_telefono`), zona, router, plan, estado y `empresa_id`. Cada fila lleva `sincronizado_en` y se refresca por webhook (§2.3) o al abrir la ficha si tiene más de 24 h.

### 2.3 Webhooks Argos → Iris

`POST https://<iris>/api/v1/integraciones/argosisp/webhook/{conector_id}`. Cuerpo: `{ id, evento, ocurrido_en, payload }`, con `empresa_id` en el payload.

| Evento | Efecto en Iris |
| --- | --- |
| `cliente.actualizado`: cliente_id, empresa_id, empresas[], campos[] (los que cambiaron, solo nombres, nunca valores: `nombre`, `tipo`, `codigo`, `nit_cui`, `tipo_documento_fiscal`, `telefono`, `movil`, `direccion`, `coordenadas`, `zona_id`; desde 0.5.0 también `email` y `referencia`) | Refresca la instantánea del cliente. |
| `servicio.datos_cambiados` (0.5.0): servicio_id, empresa_id, empresas[] (= [empresa_id], igual que en `cliente.actualizado`), cliente_id, campos[] (`direccion`, `coordenadas`, `referencia`, `contacto_nombre`, `contacto_telefono`; solo nombres). No lleva cambios de red (esos van en `servicio.config_cambiada`) | Refresca la instantánea del servicio. Iris decide qué hace con sus órdenes abiertas. |
| `catalogo.cambiado` (0.5.0): catalogo (`zona` \| `plan` \| `router`), id, empresa_id (empresa sugerida de la zona o del router; null = compartido, llega a cualquier alcance; en `plan` siempre null), campos[]: `nombre` (los tres), `activo` (desactivar o reactivar, los tres) y `coordenadas` (router) | Refresca sus catálogos locales. |
| `inventario.traslado_enviado` / `inventario.traslado_recibido` / `inventario.traslado_cancelado` (0.5.0): traslado_id, empresa_id, bodega_destino_id, tecnico (`{ id_externo, correo }` o null si el destino no es la bodega de un técnico), estado, lineas[] (`{ producto_id, cantidad, series[] }`), enviado_en; recibido: recibido_en, recibido_por, metodo_recepcion, origen (`iris` \| `panel`); cancelado: cancelado_en, motivo. Solo a conectores con contrato ≥ 0.5.0 (§3.3) | Enviado: muestra la orden por recibir en la app de campo del técnico (antes puede confirmar con `GET /inventario/traslados/:id`). Recibido o cancelado: la cierra. |
| `servicio.creado`, `servicio.config_cambiada`, `asignacion.cambiada` | Refresca la instantánea del servicio. |
| `servicio.estado_cambiado` | Refresca el estado. Con `a = retirado`, propone una orden de retiro si el servicio tiene equipos en comodato. |
| `inventario.equipo_por_recuperar` | Crea una orden de retiro (`origen = argos`), si no hay una abierta para esa unidad. |
| `servicio.caidas_repetidas` (pedido A6): servicio_id, empresa_id, cliente_id, router_id, sesion_id, sesiones, desde, hasta, umbral, ventana_minutos, causas. Por defecto 5 sesiones terminadas en 120 min, a lo sumo una vez por servicio y ventana; no cuentan los cortes que manda Argos ni los reinicios del NAS | Sugiere un ticket proactivo. |
| `servicio.eliminado` (0.3.1): servicio_id, empresa_id, cliente_id, router_id. Solo servicios que nunca se usaron (sin facturas, pagos, sesiones ni otro historial); en Argos la fila ya no existe. Llega junto con un `asignacion.cambiada` de ips vacías. Solo si `empresa_id` está en el alcance del usuario técnico | Quita la instantánea del servicio y refresca la lista de servicios del cliente (`cliente_id`). Órdenes y tickets que lo referencian conservan su copia y lo muestran «eliminado en Argos». Un evento atrasado de ese servicio no recrea la instantánea; `GET /servicios/:id` responde 404 (esperado, no se reintenta; ver «Confirmar un borrado» abajo). |
| `router.eliminado` (0.3.1): router_id, empresa_id (empresa sugerida del router, o null = router compartido, que llega a cualquier alcance; con empresa, solo si está en el alcance). Solo routers sin historial | Quita el router de factibilidad (router o nodo sugerido); una factibilidad ya evaluada conserva el id. `GET /routers/:id` responde 404 (ver «Confirmar un borrado» abajo). |
| `comprobante.aprobado` / `comprobante.rechazado` (0.6.0): forma en §7.2. Solo a conectores con contrato ≥ 0.6.0 | Resuelve la boleta en la conversación y avisa al cliente (§6.3). |
| `promesa.creada` / `promesa.cumplida` / `promesa.incumplida` / `promesa.cancelada` (0.6.0): forma en §7.4. Solo a conectores con contrato ≥ 0.6.0 | Actualiza la promesa o la reconexión temporal de la conversación y explica al cliente. |

**Confirmar un borrado (0.3.2).** Antes de quitar una instantánea por `servicio.eliminado` o `router.eliminado`, Iris puede confirmar con `GET /api/v1/servicios/:id` o `GET /api/v1/routers/:id`. Solo cuenta como borrado un `404` `application/problem+json` con `type` `urn:argosisp:problema:no_encontrado`. Un servicio de una empresa fuera del alcance del usuario técnico responde `403` (`type` `urn:argosisp:problema:empresa_no_permitida`, o el 403 genérico del ACL), nunca 404, y no es un borrado; los routers no se filtran por empresa (§2.1). Cualquier otra respuesta (5xx, timeout, 401) se reintenta con espera exponencial.

## 3. Iris escribe en Argos (al cerrar órdenes y ganar prospectos)

Todas las llamadas llevan `Idempotency-Key`. Cada una queda en `integracion.pendientes` hasta su `2xx`. Un `4xx` de negocio no se reintenta: la orden queda «cerrada con pendiente en Argos» y se avisa al supervisor con el `detail` del error.

| Hecho en Iris | Llamada a Argos | Idempotency-Key |
| --- | --- | --- |
| Instalación cerrada como exitosa | `POST /servicios/:id/activar-instalacion` (pedido A1), sin `version` (§3.1), con `planta` si la orden la trae (pedido A9) y, por cada equipo con serie, `POST /servicios/:id/equipos` con `{ unidad_id }` o `{ serie }` y, desde 0.5.0, `tecnico` (§3.3): la unidad sale de la bodega del técnico | `iris:instalacion:<orden_id>` · `iris:comodato:<orden_id>:<serie>` |
| Consumibles usados en una orden | `POST /inventario/consumos` con `{ tecnico, lineas[], servicio_id, origen_tipo: 'iris_orden', origen_id }` (0.5.0; `bodega_id` explícita sigue valiendo en todo 0.x y manda sobre `tecnico`). Solo productos `consumible_gasto` o `consumible_cobro`; una línea `comodato` responde `422 producto_es_comodato` (los equipos van por `POST /servicios/:id/equipos`). Detalle en §3.3 | `iris:consumo:<orden_id>` |
| Técnico recibe una orden de traslado (0.5.0) | `POST /inventario/traslados/:id/recibir`, cuerpo y errores en §3.3 | `iris:recepcion:<traslado_id>` |
| Retiro con equipo recuperado | `POST /servicios/:id/equipos/:unidad_id/devolver` con `{ tecnico | bodega_id, condicion }` (0.5.0: con `tecnico`, a la bodega activa del técnico en la empresa del servicio) | `iris:devolucion:<orden_id>:<unidad_id>` |
| Retiro sin equipo (perdido) | `POST /servicios/:id/equipos/:unidad_id/perdida` | `iris:perdida:<orden_id>:<unidad_id>` |
| Retiro del servicio pedido por el cliente | `POST /servicios/:id/retirar` | `iris:retiro:<orden_id>` |
| Cambio de equipo | Una devolución y una asignación, en ese orden | Las dos de arriba |
| Prospecto ganado | `POST /clientes` y luego `POST /servicios` con `estado_inicial: pendiente_instalacion` (pedido A1), según §3.2 | `iris:cliente:<prospecto_id>` · `iris:servicio:<prospecto_id>` |
| Visita o instalación cobrada | `POST /facturas/cargo-unico` (ya existe), con el producto si aplica | `iris:cargo:<orden_id>` |

**`tecnico` (0.5.0):** ver §3.3.

Iris nunca registra pagos ni asientos. Solo pide a Argos el cargo único, y Argos factura y contabiliza.

### 3.1 Idempotencia en Argos

- Con la misma `Idempotency-Key`, la misma ruta y el mismo cuerpo, Argos devuelve el `2xx` original (mismo status, cuerpo e id) con `Idempotent-Replayed: true` y no crea nada.
- La huella es el sha256 de método + URL completa con query + el cuerpo JSON **crudo**: el orden de las claves y los campos extra cuentan. Un reintento manda exactamente los mismos bytes.
- Misma clave con otro cuerpo u otra ruta: `422 idempotency_key_reutilizada` (de negocio, no se reintenta). Primera petición aún en curso: `409 idempotencia_en_curso` (se reintenta con espera). Un `4xx` o `5xx` no guarda la clave.
- En sentido contrario (Argos → Iris, §6), Iris responde `409` con un `type` que termina en `idempotencia_en_curso` (mensajería) o `idempotencia_en_proceso` (resto de su API). Argos reintenta cualquiera de los dos con la misma clave y los mismos bytes; otro `409` es de negocio y no se reintenta (0.6.2).
- El alcance de la clave es el actor (el usuario técnico «Iris»). Argos la conserva al menos 30 días.
- En `activar-instalacion` la cabecera es obligatoria (`400 idempotency_key_requerida`); en el resto de §3, opcional, pero Iris siempre la manda.
- En `activar-instalacion`, `version` es opcional e Iris no la manda: una versión leída al encolar queda vieja y el reintento nunca avanzaría. Si llega y no coincide, `409 conflicto_version`. Sin ella, Argos se protege con la máquina de estados (solo sale de `pendiente_instalacion`) y con la `Idempotency-Key`.
- Una activación con otra clave (o cuya respuesta ya no está guardada) sobre un servicio que ya no está `pendiente_instalacion` responde `409 servicio_no_pendiente` (`type: urn:argosisp:problema:servicio_no_pendiente`, `detail` con el estado actual). Es de negocio y no se reintenta: la orden queda «cerrada con pendiente en Argos» y se avisa al supervisor. De los 409, solo `idempotencia_en_curso` se reintenta.

### 3.2 Alta desde un prospecto ganado

1. `POST /clientes`: `nombre` (2–200) obligatorio; opcionales `tipo` (`persona` | `empresa`), `tipo_documento_fiscal`, `nit_cui` (Argos valida el dígito verificador), `nombre_fiscal`, `direccion_fiscal`, `email`, `telefono`, `movil`, `direccion`, `coordenadas` (`"lat,lng"`), `zona_id`, `canal_preferido`, `codigo_externo` (Iris manda `iris:prospecto:<id>`). Sin `empresa_id`: el cliente es de la instalación. Argos genera el `codigo`.
2. `POST /servicios`: `cliente_id`, `router_id` y `plan_id` obligatorios; Iris manda además `empresa_id` (la del prospecto), `estado_inicial: pendiente_instalacion`, `direccion` y `coordenadas`. Sin `origen_tipo`/`origen_id` (van solo en `activar-instalacion`). Si no llega `user_ppp`, Argos lo genera; `409 user_ppp_duplicado` es de negocio. La respuesta nunca trae la clave PPP.
3. **0.5.0:** `POST /clientes` acepta `referencia` (≤ 300), y `POST /servicios`, `referencia`, `contacto_nombre` (≤ 200) y `contacto_telefono`. `telefono`, `movil` y `contacto_telefono` se validan en el alta y en la edición (Guatemala, 8 dígitos que empiezan en 2–7, con `+502` opcional; o internacional `+` y 8–15 dígitos), y Argos los guarda normalizados (`+502 5512-3344`). Si no es válido: `422 validacion` con `rule: 'telefono'` en el campo; es de negocio y no se reintenta.
4. Iris guarda el `cliente_id` y el `servicio_id` de cada `2xx`; si los perdiera, repite la llamada con la misma clave (§3.1) para recuperarlos.

### 3.3 Inventario del técnico (0.5.0)

Detalle de Argos en su contrato de inventario (Fase D). Aquí va lo que Iris necesita.

**Técnico.** `tecnico` es `{ id_externo }` o `{ correo }`.
- `id_externo` es el id del usuario de Argos. Iris lo obtiene de `GET /inventario/tecnicos` y guarda su propio vínculo, que enlaza un admin. No depende del vínculo de SSO.
- Si llegan los dos, manda `id_externo`.
- `correo`: Argos no verifica correos (§1.3). Solo se acepta si coincide con un único usuario activo, sin distinguir mayúsculas. Si coincide con más de uno: `422 tecnico_ambiguo`.
- La bodega es la bodega `tecnico` activa de ese usuario en la empresa de la operación: la del servicio. Sin servicio, la de `X-Empresa-Id`; sin ella y con varias posibles, `422 empresa_requerida`.

**Lecturas.**
- `GET /inventario/tecnicos?cursor=&limit=` (`inventario.ver`) → `{ data: [{ id_externo, correo, nombre, bodegas: [{ id, empresa_id, activa }] }], nextCursor }`. Solo usuarios con al menos una bodega `tecnico`, dentro del alcance de empresas del usuario técnico Iris.
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
- **Forma:** `lineas[]` (o, como hasta hoy, un solo producto arriba). Responde `MovimientoInventario` (el de la primera línea) más `movimientos[]`, `cargos[]` y `factura_id` (o null).
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

**Qué se reintenta:** solo `5xx`, timeouts y `409 idempotencia_en_curso` (§3.1).

**Versión y compatibilidad.**
- El canje (§1.3/§1.4) y `GET /integraciones/iris/estado` devuelven `version_argos` (por ejemplo `"1.6.0"`) y `contrato` (por ejemplo `"0.5.0"`).
- El canje acepta `contrato` (la versión que habla Iris), y `PUT /integraciones/iris/contrato { contrato }` la actualiza con el token de Iris.
- **Receptor de Argos (0.6.2).** El canje y el PUT aceptan además `receptor_argos?: boolean`: `true` cuando Iris tiene guardados la `url` y el `secreto` del receptor de §6.6. Con B1, el canje siempre manda `true`. En el PUT, Iris manda `false` para los conectores canjeados antes de B1, que no tienen receptor guardado y a los que Iris no envía `aviso.*` ni `contacto.baja` (quedan `omitido`). Si el PUT no lo trae, Argos conserva el último valor. `GET /integraciones/iris` devuelve `requiere_reconectar` (`true` solo si el último `receptor_argos` es `false`) y `motivo_reconectar` (texto o `null`), y el panel pide volver a conectar.
- Argos solo envía los webhooks nuevos de 0.5.0 a conectores con contrato ≥ 0.5.0. Así un Iris anterior no los recibe como desconocidos.
- Argos 1.5.x no conoce `tecnico`: lo ignora y, sin `bodega_id`, responde `422 bodega_id required`. Iris consulta `version_argos` antes de mandar `tecnico`.
- `bodega_id` sigue válido en todo 0.x.

## 4. Versionado

Semver. Esta versión 0.6.3 es un borrador hasta que los dos lados lo aprueben; desde 1.0.0, solo cambios compatibles en versiones menores.

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
| A7 | **Ficha del cliente en Argos con sus tickets y órdenes de Iris**, leyendo `GET /api/v1/tickets?cliente_id=` con el token de §1.2, y botón «Ver en Iris». | solicitado |
| A16 | **Comprobantes de pago desde Iris** (0.6.0, §7.2): `POST /comprobantes` (multipart) con la boleta que mandó el cliente, `canal = iris` en la cola de caja que ya existe, y webhooks `comprobante.aprobado` / `comprobante.rechazado` con `causa` y `servicio_estado`. | listo en Argos (1.x, PR #217): `POST /comprobantes` multipart con canal `iris`, filtro `canal=iris` en la cola de caja y webhooks `comprobante.*` con `causa` y `servicio_estado` |
| A17 | **Reconexión temporal** (0.6.0, §7.3): `POST /comprobantes/:id/reconexion-temporal`, como promesa de tipo `reconexion_temporal` con la política de la plantilla, y el webhook `promesa.cancelada`. | listo en Argos (1.x, PR #217): promesa `reconexion_temporal` con la política de la plantilla (apagada por defecto) y `promesa.cancelada` |
| A18 | **Promesas de pago** (0.6.0, §7.4): `POST /promesas/simular`, `POST /promesas` y los webhooks `promesa.*`. | listo en Argos (1.x, PR #217): permisos de Iris, canal `ia` u `operador` según `X-Iris-Actor` y webhooks `promesa.*` |
| A19 | **Verificar identidad sin exponer el documento** (0.6.0, §7.5): `POST /integraciones/iris/verificar-identidad` y `nit_cui` oculto para el usuario técnico Iris. | listo en Argos (1.x, PR #217): `{ coincide }` con 5 intentos fallidos cada 30 min; `nit_cui` sale `***` al usuario técnico Iris (null si el cliente no tiene documento) |
| A20 | **Buscar cliente por teléfono** (0.6.0, §7.6): `GET /clientes?telefono=` con coincidencia exacta sobre el número normalizado y `coincide_en[]`. | listo en Argos (1.x, PR #217): filtro exacto `telefono` con `coincide_en[]`; con el token de Iris exige las cabeceras de §7.1 |
| A21 | **Avisos de Argos por la mensajería de Iris** (0.6.0, §6): canal `whatsapp` de `notify` que llama a §6.2, envío de permisos de contacto desde el panel y el portal, y receptor de webhooks `/integraciones/iris/webhook`. | listo en Argos (1.x, PR #217): canal `whatsapp` de `notify`, consentimientos desde el panel y el portal, receptor `/integraciones/iris/webhook` y consulta `GET /mensajeria/avisos/:id` de los envíos `encolado` con más de 24 h |

### 5.2 ArgosISP solicita a Iris

| # | Pedido | Estado |
| --- | --- | --- |
| B1 | **Servicio de mensajería** (0.6.0, §6): API de avisos, plantillas, permisos de contacto y webhooks de entrega. Lo ofrece Iris aunque Argos no lo pidió; Argos lo adopta como canal WhatsApp (A21) y pide además las plantillas `factura_vencida` y `servicio_reactivado`. | en curso en Iris |
| B2 | **Autologin a una conversación** (0.6.0, §7.2): destino `conversacion` en `POST <iris>/api/v1/integraciones/argosisp/sso/tickets`, con el id de `X-Iris-Conversacion`, para el botón «Ver conversación en Iris» de la cola de caja. | pendiente en Iris (cuando exista la pantalla de conversaciones) |

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
- En Argos, `en_cola` deja el envío `encolado` hasta el webhook de §6.6, y `bloqueado` lo deja `omitido` con el motivo (como un canal apagado: sin reintento ni respaldo). Un `5xx` o un timeout se reintentan con la misma clave, con las reglas de reintento de `notify`.
- En horario de silencio el aviso queda `en_cola` y sale al terminar. Si al terminar el silencio ya no tiene sentido (`vence_en` pasado), se descarta con `aviso.fallido` y `motivo = vencido`.
- `prioridad: "urgente"` (solo `corte_programado`) salta el límite diario, pero nunca el horario de silencio ni la falta de permiso. Argos no la usa en la fase 1.

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
| `reconexion_temporal` | `hasta` | Iris, al recibir el 201 de §7.3 | Argos no avisa por su cuenta (§7.3) |
| `promesa_aceptada` | `vence_en`, `monto` | Iris, al recibir el 201 de §7.4 con `estado = activa` | Argos no avisa por su cuenta de una promesa creada por Iris (§7.4) |

`GET /api/v1/mensajeria/plantillas` lista las vigentes, con sus variables y en qué canales están aprobadas. Una plantilla que Argos pide y no está aprobada responde `bloqueado` con `canal_no_disponible`.

### 6.4 Permisos de contacto

Iris guarda el registro: cliente, canal, estado (`acepta` | `baja`), fecha, origen y quién lo registró. Argos **no** lleva un registro propio: el de Iris es la fuente.

- `POST /api/v1/mensajeria/consentimientos` `{ empresa_id, cliente_id, canal: "whatsapp", estado: "acepta" | "baja", origen: "argos_panel" | "argos_portal", ocurrido_en }` (`empresa_id` obligatorio), con `Idempotency-Key = argos:consentimiento:<empresa_id>:<cliente_id>:<canal>:<ocurrido_en>`: el mismo cliente en dos empresas son dos permisos. Argos lo manda cuando un operador marca en el panel «Acepta avisos por WhatsApp» al crear o editar un cliente (`origen = argos_panel`), o cuando el cliente lo marca en el portal (`origen = argos_portal`). Si el conector está apagado, queda en la cola de envíos a Iris y sale al activarse.
- Cualquier `2xx` significa que quedó en el historial de Iris: `201` si es nuevo y `200` si es un reintento con la misma `Idempotency-Key`. Los dos responden `{ consentimiento_id, estado_vigente }`.
- `estado_vigente` puede diferir de lo que se mandó: gana el más reciente por `ocurrido_en` (por ejemplo, un `acepta` anterior a una baja dada en el chat queda en el historial, pero sigue vigente la `baja`). Argos lo guarda y lo adopta como último estado de contacto del cliente.
- `422` por validación (canal u origen desconocido, cliente fuera de la empresa del conector) no se reintenta. Un `5xx` o un timeout se reintentan con la misma clave.
- Una baja que da el cliente en el chat (palabras `BAJA` o `ALTO`) manda sobre cualquier alta anterior y llega a Argos por el webhook `contacto.baja`.
- Una baja corta los avisos, pero no las respuestas: el cliente puede seguir escribiendo y se le contesta.
- `contacto.baja` en Argos queda en la línea de tiempo del cliente (auditoría) y se muestra en su ficha. Solo afecta al canal que nombra: no apaga el correo ni el Telegram de Argos.

### 6.5 Reglas contra spam (las aplica Iris, nunca Argos)

- Solo a clientes con permiso registrado.
- Máximo 3 avisos automáticos por cliente y por día (configurable en Iris).
- Horario de silencio de 21:00 a 07:00, hora de Guatemala.
- Los envíos masivos van espaciados: 20 por minuto por número, con pausa automática si suben los reportes de spam o baja la calidad del número.
- Ventana de 24 h de WhatsApp: fuera de ella, solo plantillas aprobadas.
- Un mismo hecho se avisa una sola vez por WhatsApp: lo que Iris ya contestó en la conversación (boletas, reconexión, promesas) Argos no lo repite (§6.3).

### 6.6 Webhooks Iris → Argos

A `POST https://<argos>/api/v1/integraciones/iris/webhook` (el receptor que Argos entrega en el canje de §1.3). Solo a conectores con contrato ≥ 0.6.0 y con el receptor guardado; un conector canjeado antes de B1 se vuelve a conectar («Versión y compatibilidad», `receptor_argos`).

- **Cuerpo:** `{ id, evento, ocurrido_en, payload }`.
- **Cabeceras:** `X-Webhook-Id` (= `id`), `X-Webhook-Timestamp` y `X-Webhook-Signature: v1=<hmac-sha256>`, firmadas con el secreto de entrada del canje de §1.3. Los reintentos son los de §1.3. Si `X-Webhook-Id` y `id` no coinciden, Argos responde `400`.
- Argos deduplica por `X-Webhook-Id`.

| Evento | Payload | Efecto en Argos |
| --- | --- | --- |
| `aviso.entregado` | aviso_id, idempotency_key, empresa_id, canal (`whatsapp`), entregado_en, leido_en (o null) | El envío pasa a `enviado` y emite `notificacion.enviada` |
| `aviso.fallido` | aviso_id, idempotency_key, empresa_id, canal (o null), motivo (`vencido` \| `numero_invalido` \| `rechazado_por_canal` \| `agotado`), detalle (o null) | El envío pasa a `fallido` con el motivo y emite `notificacion.enviada` |
| `contacto.baja` | cliente_id, empresa_id, canal, ocurrido_en, origen (`cliente` \| `agente`) | Ver §6.4 |

Argos encuentra su envío por `idempotency_key` (`argos:aviso:<plantilla>:<envio_id>`, §6.2). Llega un solo webhook final por aviso, `aviso.entregado` o `aviso.fallido`. Un `leido_en` posterior no manda otro webhook: solo se ve en el GET.

**Consultar un aviso.** `GET /api/v1/mensajeria/avisos/:id` (permiso `mensajeria.enviar`), por si Argos perdió un webhook. Argos lo consulta para los envíos `encolado` con más de 24 h.

- `:id` es el `aviso_id` del `202` de §6.2. Iris solo muestra los avisos que creó el mismo conector; uno de otro conector o de otra empresa responde `404` problem+json.
- Responde `200 { aviso_id, idempotency_key, empresa_id, cliente_id, servicio_id (o null), plantilla, estado, canal (o null), motivo (o null), detalle (o null), creado_en, entregado_en (o null), leido_en (o null) }`.
- `estado` = `en_cola` | `bloqueado` | `entregado` | `fallido`. `leido_en` es un dato, no un estado. `motivo` sale con `bloqueado` (los motivos de §6.2) y con `fallido` (los de esta tabla).
- En Argos: `entregado` → `enviado`, `fallido` → `fallido`, `bloqueado` → `omitido`; `en_cola` sigue `encolado`.
- Si Argos perdió el `202`, reenvía el `POST` de §6.2 con la misma `Idempotency-Key`: Iris devuelve el mismo `aviso_id` y el estado actual.

## 7. Pagos y atención (0.6.0, fase 1)

Los clientes mandan por chat o llamada la foto de su boleta de depósito o piden una promesa de pago. Los atiende un asistente de IA de Iris o una persona. **Iris nunca registra un pago, una promesa ni una reconexión: siempre lo pide a Argos, y Argos decide.**

En Argos todo esto ya existe para el portal del cliente y el panel; Iris usa las mismas piezas:

| Lo que pide Iris | Pieza de Argos | Estado en Argos |
| --- | --- | --- |
| Boleta de pago | **Comprobante de depósito** (`comprobantes.registrar`): `pendiente` → `aprobado` (crea el pago) o `rechazado` | Existe, con canal `iris` (PR #217) |
| Promesa de pago | **Promesa** (`promesas.crear`, `POST /promesas`), con reglas por plantilla y por canal (`ia`, `operador`) | Existe |
| Política de promesas | **Simulador** (`POST /promesas/simular`) | Existe |
| Reconexión temporal | **Promesa de tipo `reconexion_temporal`** ligada a un comprobante pendiente | Existe (PR #217) |
| Verificar identidad | `clientes.verificar_identidad` | Existe (PR #217) |
| Buscar por teléfono | Filtro `telefono` de `GET /clientes` | Existe (PR #217) |

### 7.1 Principios

| Regla | Detalle |
| --- | --- |
| Argos decide | Argos valida la boleta (una persona de caja en su panel), aplica el pago y fija la política de promesas y de reconexión temporal. |
| Un comprobante no es un pago | Registrar un comprobante no mueve dinero ni toca el libro contable ni el estado del servicio. El pago, su asiento y la reactivación nacen solo cuando caja lo aprueba. |
| Un solo juez del estado | El estado del servicio lo decide solo `evaluarEstadoServicio` de Argos. Ninguna llamada de Iris cambia un estado directo: la reconexión temporal es una promesa, y la promesa activa es lo que mantiene el servicio activo. |
| Identidad primero | Antes de dar el saldo o de reportar un pago, Iris verifica al cliente: número registrado más una prueba (§7.5). Sin verificar, solo responde información general. |
| Datos mínimos | Iris nunca recibe el documento (NIT o CUI) del cliente ni datos de tarjeta (§1.1). El saldo se muestra solo al cliente verificado y no se guarda en el historial del chat. |
| Actor | Cada llamada de §7 lleva `X-Iris-Actor` = `asistente_ia` \| `usuario:<id en Iris>` (id de hasta 64 caracteres `A-Z a-z 0-9 _ -`) y `X-Iris-Conversacion` = id de la conversación (hasta 128 caracteres `A-Z a-z 0-9 . _ : -`). Sin ellas o mal formadas: `400 cabecera_iris_requerida`. Argos audita con actor `token` del usuario técnico Iris y guarda las dos cabeceras en la fila de auditoría, donde se pueden buscar. El canal de la auditoría es `ia` con `asistente_ia` y `api` con `usuario:<id>`. |
| Empresa | Cada documento es de una empresa: la del servicio (promesas) o la de la cuenta bancaria (comprobantes). Si Iris manda `empresa_id` y no coincide: `422 empresa_no_coincide`. Fuera del alcance del usuario técnico Iris: `403 empresa_no_permitida`. |
| Idempotencia | Todas las escrituras de §7 llevan `Idempotency-Key` (obligatoria: `400 idempotency_key_requerida`) con las reglas de §3.1. |

### 7.2 Comprobantes de pago (A16)

**Leer las cuentas.** El cliente deposita en una cuenta bancaria de la empresa. Iris lee las cuentas con `GET /api/v1/cuentas-bancarias?activa=true` (`cuentas_bancarias.ver`; solo las de moneda `GTQ` reciben depósitos) y los bancos de origen con `GET /api/v1/bancos`. El asistente le pregunta al cliente a cuál depositó si la foto no lo dice.

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
- `GET /api/v1/comprobantes/:id` (`comprobantes.ver`) devuelve su estado, por si Iris perdió un webhook.

**Resolución.** Caja aprueba o rechaza en el panel (`comprobantes.aprobar`, `comprobantes.rechazar`); Iris no puede hacerlo. Al aprobar, Argos registra el pago, su asiento y la reactivación en una sola transacción.

Webhooks Argos → Iris (§2.3), para comprobantes de cualquier canal dentro del alcance; solo a conectores con contrato ≥ 0.6.0:

| Evento | Payload |
| --- | --- |
| `comprobante.aprobado` | comprobante_id, empresa_id, cliente_id, servicio_id (o null), canal (`portal` \| `panel` \| `iris`), monto, pago_id, motivo: null, causa: null, servicio_estado (estado del servicio después de aplicar el pago, o null sin servicio) |
| `comprobante.rechazado` | lo mismo, con pago_id: null, motivo (texto de caja) y causa (`no_aparece` \| `monto_distinto` \| `ilegible` \| `falta_dato` \| `duplicado` \| `otro`) |

- `servicio_estado = "activo"` después de un corte es lo que la propuesta llamaba `reconectado`. El cambio de estado llega además, como siempre, en `servicio.estado_cambiado`.
- `causa = falta_dato` es lo que la propuesta llamaba `falta_dato`: el comprobante queda rechazado, la referencia queda libre y el cliente lo vuelve a mandar completo (otro `POST /comprobantes` con otra `Idempotency-Key`). Iris usa `boleta_falta_dato` (§6.3).
- Con `canal = iris`, Argos no avisa al cliente por su cuenta del comprobante ni del pago que nace de él: lo hace Iris en la conversación.

### 7.3 Reconexión temporal (A17)

Cuando un cliente cortado manda su comprobante y nadie de caja puede revisarlo pronto, Iris puede pedir que Argos lo reconecte mientras se revisa. En Argos es una **promesa de pago** de tipo `reconexion_temporal`, ligada al comprobante: `evaluarEstadoServicio` mantiene el servicio activo mientras esté activa, igual que cualquier promesa. Nunca es un cambio de estado directo.

`POST /api/v1/comprobantes/:id/reconexion-temporal` (`comprobantes.reconexion_temporal`) con `{ servicio_id?, motivo: "fuera_de_horario" }` e `Idempotency-Key = iris:reconexion:<comprobante_id>`. `servicio_id` es obligatorio solo si el comprobante no tiene servicio.

- Respuesta `201 { promesa_id, reconexion_id, hasta, servicio: { id, estado } }`. `reconexion_id` = `promesa_id` (alias para Iris); `hasta` = vencimiento de la promesa.
- Política (la fija Argos en la plantilla de facturación, bloque nuevo `reconexion_temporal` de `reglas_promesa`, exceptuable por servicio). Valores aprobados por el PM (5 nov 2026):
    - apagada por defecto (`permitida: false`) hasta que cada empresa la active;
    - a lo sumo **una por periodo de facturación y servicio** (`max_por_periodo: 1`), aparte del cupo de promesas normales;
    - solo si el comprobante está `pendiente`, es del mismo cliente y su `monto` cubre **todo lo vencido** del servicio;
    - dura `horas_max` (propuesta: 24 h) desde que se crea.
- `422 reconexion_no_permitida`, con la razón en `errors[0].rule`: `no_habilitada`, `cupo_agotado`, `comprobante_no_pendiente`, `comprobante_no_cubre`, `servicio_no_cortado` (el servicio ya está activo), `suspension_manual` (lo suspendió un operador: solo él lo reactiva), `promesa_abierta` (ya tiene una promesa activa o pendiente), `servicio_de_otro_cliente`. Es de negocio y no se reintenta.
- **Cómo termina** (webhooks `promesa.*` de §7.4, con `tipo = reconexion_temporal`):
    - caja aprueba el comprobante y el pago salda lo vencido → `promesa.cumplida`;
    - caja lo rechaza → la promesa se cancela al instante → `promesa.cancelada` con `motivo = comprobante_rechazado`, y el servicio se re-evalúa: vuelve a cortarse (si la plantilla tiene hora de corte y aún no llega, espera a esa hora, como cualquier corte);
    - llega `hasta` con el comprobante aún pendiente → `promesa.cancelada` con `motivo = vencida_sin_revisar` y re-evaluación. No cuenta como promesa incumplida en el historial del cliente: la demora fue de caja;
    - caja aprueba pero el pago no salda lo vencido → sigue activa hasta `hasta` y entonces `promesa.incumplida`, como cualquier promesa.
- Iris nunca pide una reconexión temporal sin un comprobante registrado. Argos no avisa al cliente por su cuenta: Iris le explica en los cuatro casos.

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

2. **Crear:** `POST /api/v1/promesas` (`promesas.crear`) `{ servicio_id, vence_en }` con `Idempotency-Key = iris:promesa:<id en Iris>`. `vence_en` es un instante RFC 3339 (Iris manda el final del día prometido en hora de Guatemala). El canal de la promesa lo pone Argos según `X-Iris-Actor`: `ia` para `asistente_ia` y `operador` para `usuario:<id>`; cada uno tiene sus límites en la plantilla.
    - Respuesta **siempre** `201 Promesa`, porque Argos guarda también la promesa rechazada (cuenta para el historial). Iris mira `estado`:
        - `activa`: aceptada; el servicio se re-evalúa en la misma transacción y la respuesta trae `servicio`. Iris usa `promesa_aceptada` (§6.3);
        - `rechazada`: fuera de política; `resultado_evaluacion.regla` y `.motivo` dicen por qué. Iris pasa la conversación a una persona;
        - `pendiente_aprobacion`: la plantilla pide que un supervisor de Argos la apruebe. Iris le dice al cliente que queda en revisión y espera el webhook.
    - `409 promesa_abierta`: el servicio ya tiene una promesa activa o pendiente. `422` si `vence_en` no es futura, o el servicio está retirado, pendiente de instalación o sin plantilla. De negocio, no se reintentan.
    - `monto` en el cuerpo se ignora; `fecha_compromiso` de la propuesta es `vence_en`.
3. **Webhooks Argos → Iris** (§2.3), para promesas de cualquier canal dentro del alcance; solo a conectores con contrato ≥ 0.6.0. Payload común: promesa_id, empresa_id, cliente_id, servicio_id, tipo (`promesa` \| `reconexion_temporal`), canal (`portal` \| `ia` \| `operador` \| `supervisor`), comprobante_id (o null), vence, monto.

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
- El código por WhatsApp y la revisión de una foto del DPI los hace Iris (la foto la revisa siempre una persona) y no necesitan a Argos.
- Iris nunca guarda el `valor`: en la conversación, la transcripción de la llamada, los logs y su auditoría queda como `***` desde que el cliente lo escribe o lo dicta, y el asistente de IA solo ve una marca en su lugar: Iris lo reemplaza antes de pasarle el mensaje al modelo y lo manda a Argos directamente.

### 7.6 Buscar cliente por teléfono (A20)

`GET /api/v1/clientes?telefono=+50255123344` (`clientes.ver`, el permiso que Iris ya tiene). Argos normaliza el número con la regla de §3.2 (`422 validacion` con `rule: 'telefono'` si no es un teléfono válido) y busca coincidencia **exacta** de dígitos sobre `telefono` y `movil` del cliente y `contacto_telefono` de sus servicios no retirados. Los teléfonos viejos guardados sin normalizar también coinciden: se comparan solo los dígitos, con `502` delante para los de 8 dígitos.

- Responde la lista corta habitual (`{ data, nextCursor }`), filtrada por el alcance de empresas del usuario técnico Iris. Cada cliente trae además `coincide_en[]`: `telefono`, `movil` y/o `contacto_servicio`.
- `contacto_servicio` es la persona de contacto en sitio, que puede no ser el titular: Iris no la trata como cliente verificado.
- Iris la usa para reconocer quién llama o escribe. Con más de un resultado, Iris le pide al cliente que diga cuál es su servicio, sin leerle la lista.
- `q` sigue igual: busca por partes del número entre otros campos. `telefono` es otro filtro, exacto.

## 8. Fase 2 (sin forma todavía)

- **Cambios en el equipo del cliente (Wi-Fi):** Argos se conectará con SmartOLT para cambiar el nombre o la contraseña del Wi-Fi, reiniciar la ONU o revisar la potencia. Iris lo pedirá a Argos, nunca directo a SmartOLT, con cliente verificado (§7.5). Mientras tanto, el asistente de Iris solo da las instrucciones de su biblioteca de ayuda.
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

1. **Un comprobante no es un pago** (§7.1): no toca el libro ni el estado hasta que caja lo aprueba.
2. **La reconexión temporal es una promesa** (§7.3): el estado lo sigue decidiendo solo `evaluarEstadoServicio`. Exige un comprobante pendiente que cubra todo lo vencido. Al rechazarse se re-evalúa al instante, pero un corte respeta la hora de corte de la plantilla. Vencida sin revisar no castiga el historial del cliente.
3. **Promesas:** son por servicio, Argos calcula el monto, y `X-Iris-Actor` decide el canal (`ia` u `operador`). Puede quedar `pendiente_aprobacion`.
4. **Identidad:** se verifica con el documento completo, el nombre o el código (PM). Argos deja de entregar `nit_cui` al usuario técnico Iris (§1.1); `nombre` y `codigo_cliente` son pruebas débiles; los intentos se auditan con el valor como `***`.
5. **Teléfono:** `coincide_en[]` distingue al titular del contacto en sitio.
6. **Cabeceras:** `X-Iris-Actor` y `X-Iris-Conversacion` son obligatorias en §7 (`400 cabecera_iris_requerida`). La auditoría de Argos usa canal `ia` o `api`; §0 se corrige (no existe un canal `integracion`).
7. **Archivo:** el comprobante sube como `multipart/form-data` con el archivo, igual que en el portal, en lugar de una URL firmada de 7 días. Argos no descarga nada de Iris (sin riesgo de SSRF) y la copia del archivo queda en Argos desde el primer momento.
8. **Avisos (A21):** Argos adopta la mensajería de Iris solo como su canal WhatsApp; correo y Telegram siguen en Argos. Lo que Iris contestó en la conversación (comprobantes, reconexión y promesas creadas por Iris) Argos no lo repite. `corte_programado` no lo usa Argos. Argos suma las plantillas `factura_vencida` y `servicio_reactivado`, e Iris suma `boleta_recibida`. El registro de permisos de contacto vive solo en Iris.
9. **Webhooks Iris → Argos:** van al receptor `/api/v1/integraciones/iris/webhook` que Argos ya entrega en el canje (activo desde PR #217).
10. **Nuevo pedido B2** a Iris: destino de autologin `conversacion`, para el botón «Ver conversación en Iris» de la cola de caja.
