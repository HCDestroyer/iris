Versión: 0.4.0 (borrador) · Iris ↔ ArgosISP

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
| Mínimo privilegio | El token de Iris en Argos lleva solo los permisos de §1.2, con alcance de empresas acotado. |
| Idempotencia | Toda escritura de Iris en Argos lleva `Idempotency-Key = iris:<operacion>:<id de la orden u operación>`. Los webhooks llevan un `id` estable y el receptor deduplica. |
| Auditoría | Argos audita las acciones del usuario técnico «Iris» con `canal = integracion` y la referencia de la orden (`origen_tipo = 'iris_orden'`, `origen_id`). Iris audita sus lecturas de datos personales. |

## 1. Conexión, firma y autologin

### 1.1 Iris → Argos (Iris lee y escribe en Argos)

Iris guarda, cifrado y de solo escritura, un token de Argos `atk_…` del usuario técnico «Iris» (rol creado por el seed de Argos), con estos permisos:

- **Lectura:** `clientes.ver`, `clientes.estado_cuenta`, `servicios.ver`, `planes.ver`, `routers.ver`, `zonas.ver`, `ip.resolver`, `inventario.ver`.
- **Instalación y retiro:** `servicios.activar_instalacion` (pedido A1), `servicios.retirar`, `inventario.asignar_comodato`, `inventario.devolver_comodato`, `inventario.declarar_perdida_comodato`, `inventario.consumir` y `facturas.emitir_cargo_unico`.
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
| Existencias de la bodega del técnico | `GET /api/v1/inventario/existencias?bodega_id=` |

### 2.2 Instantánea en Iris

Iris guarda en `integracion.argos_clientes` y `integracion.argos_servicios` solo lo que muestra: nombre, código, teléfono, dirección, coordenadas, zona, router, plan, estado y `empresa_id`. Cada fila lleva `sincronizado_en` y se refresca por webhook (§2.3) o al abrir la ficha si tiene más de 24 h.

### 2.3 Webhooks Argos → Iris

`POST https://<iris>/api/v1/integraciones/argosisp/webhook/{conector_id}`. Cuerpo: `{ id, evento, ocurrido_en, payload }`, con `empresa_id` en el payload.

| Evento | Efecto en Iris |
| --- | --- |
| `cliente.actualizado`: cliente_id, empresa_id, empresas[], campos[] (los que cambiaron, solo nombres, nunca valores: `nombre`, `tipo`, `codigo`, `nit_cui`, `tipo_documento_fiscal`, `telefono`, `movil`, `direccion`, `coordenadas`, `zona_id`) | Refresca la instantánea del cliente. |
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
| Instalación cerrada como exitosa | `POST /servicios/:id/activar-instalacion` (pedido A1), sin `version` (§3.1), con `planta` si la orden la trae (pedido A9) y, por cada equipo instalado, `POST /servicios/:id/equipos` con `{ unidad_id }` o `{ serie }` | `iris:instalacion:<orden_id>` · `iris:comodato:<orden_id>:<serie>` |
| Materiales usados en una orden | `POST /inventario/consumos` con `{ bodega_id: bodega del técnico, lineas[], servicio_id, origen_tipo: 'iris_orden', origen_id }` | `iris:consumo:<orden_id>` |
| Retiro con equipo recuperado | `POST /servicios/:id/equipos/:unidad_id/devolver` con `{ bodega_id, condicion }` | `iris:devolucion:<orden_id>:<unidad_id>` |
| Retiro sin equipo (perdido) | `POST /servicios/:id/equipos/:unidad_id/perdida` | `iris:perdida:<orden_id>:<unidad_id>` |
| Retiro del servicio pedido por el cliente | `POST /servicios/:id/retirar` | `iris:retiro:<orden_id>` |
| Cambio de equipo | Una devolución y una asignación, en ese orden | Las dos de arriba |
| Prospecto ganado | `POST /clientes` y luego `POST /servicios` con `estado_inicial: pendiente_instalacion` (pedido A1), según §3.2 | `iris:cliente:<prospecto_id>` · `iris:servicio:<prospecto_id>` |
| Visita o instalación cobrada | `POST /facturas/cargo-unico` (ya existe), con el producto si aplica | `iris:cargo:<orden_id>` |

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
3. Iris guarda el `cliente_id` y el `servicio_id` de cada `2xx`; si los perdiera, repite la llamada con la misma clave (§3.1) para recuperarlos.

## 4. Versionado

Semver. Esta versión 0.4.0 es un borrador hasta que los dos lados lo aprueben; desde 1.0.0, solo cambios compatibles en versiones menores.

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
