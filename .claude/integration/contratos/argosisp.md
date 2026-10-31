Versión: 0.5.0 (borrador) · Iris ↔ ArgosISP

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
| Auditoría | Argos audita las acciones del usuario técnico «Iris» con `canal = integracion` y la referencia de la orden (`origen_tipo = 'iris_orden'`, `origen_id`). Iris audita sus lecturas de datos personales. |

## 1. Conexión, firma y autologin

### 1.1 Iris → Argos (Iris lee y escribe en Argos)

Iris guarda, cifrado y de solo escritura, un token de Argos `atk_…` del usuario técnico «Iris» (rol creado por el seed de Argos), con estos permisos:

- **Lectura:** `clientes.ver`, `clientes.estado_cuenta`, `servicios.ver`, `planes.ver`, `routers.ver`, `zonas.ver`, `ip.resolver`, `inventario.ver`.
- **Instalación y retiro:** `servicios.activar_instalacion` (pedido A1), `servicios.retirar`, `inventario.asignar_comodato`, `inventario.devolver_comodato`, `inventario.declarar_perdida_comodato`, `inventario.consumir`, `inventario.recibir_traslado` (0.5.0) y `facturas.emitir_cargo_unico`.
- **Comercial:** `clientes.crear` y `servicios.crear`.
- **Autologin:** `sso.emitir`.

El usuario técnico Iris solo tiene alcance por empresas (o global), nunca por zona ni router: una lista completa y vacía significa sin servicios. Argos lo hace cumplir: un token, un rol o un correo que le darían alcance por zona o router responde `422 usuario_tecnico_solo_empresas`, y el canje nunca emite un token así.

### 1.2 Argos → Iris (Argos lee de Iris)

Token `iris_pat_<32B base62>` con `tickets.ver`, `tickets.crear`, `ordenes.ver` y `sso.emitir`. Argos lo usa para mostrar los tickets y las órdenes de un cliente en su ficha y para abrir tickets desde el portal del cliente.

### 1.3 Webhooks, enlace de conexión y autologin

Igual que en el contrato Argos ↔ Horus, §1.3–§1.5:

- **Webhooks:** cabeceras `X-Webhook-Id`, `X-Webhook-Timestamp` y `X-Webhook-Signature: v1=…`, con HMAC-SHA256. El receptor genera el secreto. Reintentos a 1 min, 5 min, 30 min, 2 h y 12 h.
- **Enlace de conexión:** `https://<A>/conectar#<codigo>`, válido 15 min y de un solo uso. El canje es servidor a servidor:
    - en Argos, `POST /api/v1/integraciones/iris/conexion/canjear`;
    - en Iris, `POST /api/v1/integraciones/argosisp/conexion/canjear`.
    - `sistema` vale `"iris"` o `"argosisp"`. El conector queda apagado hasta que un administrador lo active.
    - El canje de Argos responde `{ token, webhook: { url, secreto }, conector_id, alcance_global, empresas[] }`. `empresas[]` son las empresas activas del alcance del usuario técnico «Iris» al canjear, en orden de id, cada una `{ id, nombre, nit }` (`nombre` = nombre comercial; `nit` sin guiones, o `null` si la empresa aún no lo tiene). `alcance_global: true` significa que Iris verá también las empresas que se creen después; `false`, solo las listadas.
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
- `GET /inventario/traslados/:id` devuelve la orden con estado, líneas y recepción.

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
- Argos solo envía los webhooks nuevos de 0.5.0 a conectores con contrato ≥ 0.5.0. Así un Iris anterior no los recibe como desconocidos.
- Argos 1.5.x no conoce `tecnico`: lo ignora y, sin `bodega_id`, responde `422 bodega_id required`. Iris consulta `version_argos` antes de mandar `tecnico`.
- `bodega_id` sigue válido en todo 0.x.

## 4. Versionado

Semver. Esta versión 0.5.0 es un borrador hasta que los dos lados lo aprueben; desde 1.0.0, solo cambios compatibles en versiones menores.

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

### 5.2 ArgosISP solicita a Iris

(vacía)
