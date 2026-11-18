Versión: 0.8.0 (borrador, sin espejo; 18 nov 2026) · Iris ↔ ArgosISP

# Contrato de integración Iris ↔ ArgosISP (copia de ArgosISP)

## Gobierno

- Este texto es la fuente de verdad compartida. Vive con el mismo contenido en `HCDestroyer/iris` (`.claude/integration/contratos/argosisp.md`) y en `HCDestroyer/argos-isp` (`.claude/integration/contratos/iris.md`). La línea de versión identifica la copia al día.
- Cada sistema toca solo su código. Lo que uno necesita del otro se pide en §5, y el dueño lo implementa en su repositorio.
- Cambiar el contrato requiere dos PRs enlazadas, una por repositorio. La versión sigue semver.
- Se reutilizan los mecanismos ya probados del contrato Argos ↔ Horus (`argos-isp/.claude/integration/contratos/horus.md` §1): códigos de conexión, webhooks firmados, enlace de conexión y autologin por ticket. Aquí solo se escriben las diferencias.
- **La 0.8.0 es un borrador de Iris y todavía no tiene espejo.** La copia de `argos-isp` sigue en 0.7.1 hasta que el líder de Argos revise este texto contra su código y se abran las dos PRs enlazadas. Lo marcado «0.8.0, borrador» no obliga a ningún lado. Lo que falta por decidir está en §10.

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
- **0.8.0 (borrador):** no pide permisos nuevos en Argos. Las cuentas bancarias (§7.8) usan `cuentas_bancarias.ver`, los datos del servicio (§2.4) usan `servicios.ver`, y la primera sincronización (§2.5) usa las lecturas de arriba. Si Argos quiere un permiso aparte para algún campo, está en §10.1.

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
    - Iris muestra el resumen y el conector queda **apagado** hasta que el admin pulse «Activar», como hoy.
6. **`panel_url` en el canje.** Las dos variantes del canje responden además `panel_url` (`PANEL_URL_PUBLICA` de Argos, por ejemplo `https://argos.kns.gt`). Iris guarda por separado `panel_url`, para «Ver en Argos» y para volver a autorizar, y `api_url`, el `api` del paso 5 o el origen del enlace de §1.3.

### 1.5 Conexión de un paso (0.8.0, borrador)

**Estado: propuesta conjunta, sin implementar.** No reemplaza a §1.3 ni a §1.4: los usa. «Un paso» significa que, después de aprobar en Argos y activar en Iris, nadie copia URLs, secretos ni tokens, ni lanza cargas a mano. La 0.8.0 agrega tres cosas: (a) la primera sincronización automática (§2.5), (b) el receptor de Iris guardado en el mismo canje y (c) una pantalla de estado en cada panel (§2.5).

**Requisitos de seguridad (los fija Argos; no son negociables).** Cualquier implementación de esta sección los cumple todos; si uno no se puede cumplir, esa parte no se hace.

| # | Requisito |
| --- | --- |
| S1 | El código de conexión es de **un solo uso** y de **vida corta** (15 min, §1.3). Un código nuevo anula los anteriores sin usar. |
| S2 | El código viaja en el **fragmento** de la URL (`#…`), **nunca en la query**: no queda en logs de servidor ni en `Referer` (§1.3, §1.4 paso 4). |
| S3 | El canje es **servidor a servidor y por HTTPS**. El navegador nunca ve el token ni los secretos. |
| S4 | Si la conexión nace del botón (§1.4), **PKCE S256 es obligatorio**: sin el `code_verifier` correcto, `401 codigo_invalido` y el código queda gastado. |
| S5 | El token que recibe Iris (`atk_`) tiene **alcance acotado a empresas** (§1.1), **nunca es de administrador** y **nunca se muestra en un panel**, ni en Argos ni en Iris: se guarda cifrado y de solo escritura. Lo mismo vale para el `iris_pat_` que recibe Argos. |
| S6 | Los secretos (tokens y secretos de webhook) **se rotan y no se reutilizan**: cada canje genera secretos nuevos en los dos lados y revoca los anteriores con las reglas de reconexión de §1.3 (0.6.3). Ningún secreto de un canje anterior vuelve a valer. |
| S7 | La conexión **la aprueba en Argos un usuario con `iris.configurar`**, con sesión propia y viendo el origen de Iris y las empresas del alcance (§1.4 paso 3). Nada la aprueba solo. |
| S8 | **La vinculación de usuarios nunca va en el setup.** Sigue siendo por persona, con el correo y la contraseña de Argos de cada quien y la pantalla de confirmación de 0.7.1 (§1.3, A24). Nada en bloque, nada por correo, nada por coincidencia de nombres. La primera sincronización no trae usuarios de Argos ni crea vínculos. |

**Capa extra: comparar la huella TLS (propuesta; a Argos le parece bien).** Es el patrón que Iris ya usa con su Servidor de IA (`voz.md` §4.2 y §4.8). La pantalla de aprobación de Argos y la de Iris muestran la huella SHA-256 del certificado TLS que cada lado vio del otro al canjear, y el admin las compara. Iris **fija** la huella del certificado de la API de Argos para ese conector. Si después cambia sin una reconexión, Iris marca el conector «certificado cambiado», deja de enviar escrituras (quedan en `integracion.pendientes`) y pide confirmar. Es una capa **además** de la validación normal del certificado contra las CA públicas, no en su lugar. Cómo viaja la huella, dónde se muestra y qué pasa en una renovación normal del certificado está en §10.1.

**(b) El receptor de Iris en el mismo canje.** Este contrato remite al de Argos ↔ Horus para el cuerpo del canje y nunca lo escribió aquí. Iris ya manda hoy este cuerpo en el canje de Argos (`POST /api/v1/integraciones/iris/conexion/canjear`), y la 0.8.0 lo deja por escrito:

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
- Con la 0.8.0, Argos guarda `base_url`, `token` y `webhook` **en la misma transacción del canje** y no pide configurarlos aparte en su panel. En una reconexión se aplican con las reglas de §1.3 (0.6.3): quedan pendientes hasta la primera llamada del `atk_` nuevo.
- Argos valida que el host de `webhook.url` sea el de `base_url` y, si la conexión nació del botón, que sea el origen `iris` que aprobó el admin (§1.4). Si no, responde `422` y el código queda gastado. Es la misma regla que Iris aplica al receptor de Argos (`422 webhook_invalido`, 0.6.2).
- Qué parte de esto ya hace Argos y qué parte se sigue configurando a mano está en §10.1.

**Qué queda igual.** El conector nace **apagado** en Iris hasta que un admin pulsa «Activar». El canje sin uso vence a las 24 h. La reconexión es la de 0.6.3. `PUT /integraciones/iris/contrato` sigue siendo la primera llamada autenticada. La primera sincronización (§2.5) empieza al activar, no al canjear.

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
| Cuentas bancarias de la empresa (0.8.0, borrador) | `GET /api/v1/cuentas-bancarias` y `GET /api/v1/cuentas-bancarias/:id` (`cuentas_bancarias.ver`), y `GET /api/v1/bancos`. Detalle en §7.8 |
| Red, precio y ubicación del servicio (0.8.0, borrador) | `GET /api/v1/servicios/:id` y la lista: los campos de §2.4 |

### 2.2 Instantánea en Iris

Iris guarda en `integracion.argos_clientes` y `integracion.argos_servicios` solo lo que muestra: nombre, código, teléfono, `email`, dirección, coordenadas, `referencia` (cliente y servicio), contacto en sitio (`contacto_nombre`, `contacto_telefono`), zona, router, plan, estado y `empresa_id`. Cada fila lleva `sincronizado_en` y se refresca por webhook (§2.3) o al abrir la ficha si tiene más de 24 h.

**0.8.0 (borrador).** La instantánea del servicio suma los datos de red y el precio de §2.4, con permisos propios en Iris. Se agrega una instantánea de las cuentas bancarias (§7.8). Con la primera sincronización (§2.5), las instantáneas se llenan al activar el conector y no solo al abrir cada ficha.

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
| `promesa.creada` / `promesa.cumplida` / `promesa.incumplida` / `promesa.cancelada` (0.6.0): forma en §7.4. Solo a conectores con contrato ≥ 0.6.0. Desde 0.8.0 (borrador) el payload suma `motivo_reconexion` | Actualiza la promesa o la reconexión temporal de la conversación y explica al cliente. |
| `cuenta_bancaria.cambiada` (0.8.0, borrador; propuesta de Argos): cuenta_bancaria_id, empresa_id, campos[] (solo nombres, nunca valores), activa. Solo a conectores con contrato ≥ 0.8.0 | Relee `GET /cuentas-bancarias/:id` y refresca la instantánea de la cuenta (§7.8). Una cuenta con `activa: false` deja de ofrecerse; nunca se borra. |

**Confirmar un borrado (0.3.2).** Antes de quitar una instantánea por `servicio.eliminado` o `router.eliminado`, Iris puede confirmar con `GET /api/v1/servicios/:id` o `GET /api/v1/routers/:id`. Solo cuenta como borrado un `404` `application/problem+json` con `type` `urn:argosisp:problema:no_encontrado`. Un servicio de una empresa fuera del alcance del usuario técnico responde `403` (`type` `urn:argosisp:problema:empresa_no_permitida`, o el 403 genérico del ACL), nunca 404, y no es un borrado; los routers no se filtran por empresa (§2.1). Cualquier otra respuesta (5xx, timeout, 401) se reintenta con espera exponencial.

### 2.4 Más datos del servicio (0.8.0, borrador)

**Lo que Argos ya entrega (1.17.0).** `GET /api/v1/servicios/:id` y la lista (`GET /api/v1/servicios`) devuelven `ipv4`, `prefijo_enlace_v6`, `prefijo_delegado_v6`, `direccion`, `referencia`, `coordenadas`, `router_id`, `plan_id`, `costo_override`, `tipo_conexion`, `user_ppp` (**nunca la clave PPP**) y `planta` (el objeto de A9). Los cambios llegan por `servicio.datos_cambiados` (dirección, coordenadas, referencia, contacto) y `servicio.config_cambiada` (plan, IPs, prefijo, router), que ya están en §2.3.

**Lo que Argos propone agregar en 0.8.0 (A27).**

| Campo | Tipo | Detalle |
| --- | --- | --- |
| `precio_mensual` | string decimal | Lo que el cliente paga al mes por ese servicio, **ya calculado por Argos** (hoy sería `costo_override ?? precio del plan`). Iris nunca lo calcula ni lo deduce de `costo_override`. |
| `moneda` | string | La moneda de `precio_mensual` (por ejemplo `GTQ`). |
| `router_nombre` | string | El nombre del router de `router_id`, para no cruzarlo con el catálogo. |

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
  "planta": { "caja": "C-014", "puerto": 3 }
}
```

El ejemplo solo muestra los campos de esta sección; el resto del objeto no cambia. Los valores de `tipo_conexion` y la forma exacta de las IP y los prefijos son de Argos y están por confirmar (§10.1). La **caja o el nodo** no están en el servicio, salvo la referencia `planta` de A9, que es solo de lectura (§10.1).

**Datos sensibles (clasificación de Argos).** Son sensibles la IP y los prefijos, las coordenadas y la dirección (datos personales) y `user_ppp`: Iris les pone permiso propio. El precio y el nombre del plan no son sensibles.

**Qué entra a la instantánea de Iris** (`integracion.argos_servicios`, §2.2):

| Dato | En la instantánea | Quién lo ve en Iris |
| --- | --- | --- |
| `plan_id`, nombre del plan, `estado`, `precio_mensual`, `moneda` | sí | Cualquiera con `clientes.ver` |
| `router_id`, `router_nombre`, `tipo_conexion`, `ipv4`, `prefijo_enlace_v6`, `prefijo_delegado_v6`, `user_ppp`, `planta` | sí | Solo con `servicios.ver_red` |
| `direccion`, `referencia`, `coordenadas`, contacto en sitio | sí (ya estaban) | Solo con `servicios.ver_ubicacion` |
| `costo_override` | **no** | Nadie: Iris usa `precio_mensual` y no guarda el dato del que sale |
| Clave PPP | **no** (Argos nunca la entrega) | Nadie |

- La instantánea se refresca como en §2.2: por `servicio.config_cambiada` y `servicio.datos_cambiados`, y al abrir la ficha si tiene más de 24 h. Falta saber qué evento avisa cuando `precio_mensual` cambia sin que cambie el servicio, por ejemplo al subir el precio del plan (§10.1).
- Sin el permiso, la API de Iris **no devuelve** el campo. No lo manda vacío ni lo oculta la pantalla: el filtro es del servidor.

**Permisos nuevos en Iris (PROPUESTA del líder de Iris; los roles, pendientes del PM, §10.2).** Hoy Iris no tiene permisos `servicios.*`: todo lo del servicio se ve con `clientes.ver`, que tienen todos los roles base de persona.

| Permiso propuesto | Qué abre | Roles base propuestos | Roles que no |
| --- | --- | --- | --- |
| `servicios.ver_red` | IP, prefijos, `user_ppp`, `tipo_conexion`, router y `planta` del servicio | `admin`, `supervisor`, `soporte` y `tecnico` (este solo en los servicios de sus órdenes, con el alcance de `ordenes.ver_propias`) | `despacho`, `ventas`, `caja`, `lectura` |
| `servicios.ver_ubicacion` | Dirección, `referencia`, coordenadas y contacto en sitio del servicio | `admin`, `supervisor`, `despacho`, `soporte`, `ventas` y `tecnico` (solo en sus órdenes) | `caja`, `lectura` |

- Las órdenes de trabajo guardan su propia copia de la dirección y las coordenadas para que el técnico llegue. Esa copia se sigue viendo con los permisos de la orden y no cambia.
- Es un **cambio de comportamiento dentro de Iris**: `caja` y `lectura` dejarían de ver la dirección y las coordenadas del servicio, que hoy ven con `clientes.ver`. Por eso los roles van a §10.2.
- Los usuarios de tipo integración (`integracion_argos`, `integracion_horus`) no reciben ninguno de los dos.
- Cada lectura de estos datos por una persona se audita en Iris como lectura de datos personales (§0), sin el valor.

**Qué recibe el asistente de IA (PROPUESTA del líder de Iris, §10.2).** Amplía `conversaciones.md` §17.9 sin relajarla:

- **Nunca viajan al proveedor de IA:** IP, prefijos, `user_ppp`, coordenadas, la dirección, el texto libre de `referencia`, el contacto en sitio, `planta` ni el nombre del router. El asistente no tiene `servicios.ver_red` ni `servicios.ver_ubicacion`.
- **Sí viajan:** el nombre del plan, `precio_mensual` con `moneda`, el estado del servicio y una **etiqueta corta** que arma Iris para distinguir los servicios de un mismo cliente.
- **Etiqueta corta:** `Servicio <n> · <plan> · <zona>`. `<n>` es el orden del servicio dentro del cliente (1, 2, 3…, por id) y `<zona>` es el nombre de la zona del catálogo. No lleva calle, número de casa, coordenadas ni códigos internos. Si dos servicios quedan con la misma etiqueta, el asistente no adivina: pasa a una persona.
- Los ids de servicio siguen sin ir en los argumentos de las herramientas: los pone el servidor (§17.9).

**Cómo se distinguen los servicios de un mismo cliente en el panel.** Cada servicio va en una línea: código del servicio, plan, `precio_mensual` y estado (con icono, texto y color). La dirección corta se ve solo con `servicios.ver_ubicacion`, y la IP y el router solo con `servicios.ver_red`. Sin esos permisos se ve la misma etiqueta corta que usa el asistente. Vale para la ficha del cliente, el selector de servicio de un ticket, una orden o una boleta, y el panel lateral de la conversación.

### 2.5 Primera sincronización y estado de la conexión (0.8.0, borrador)

**Estado: propuesta conjunta, sin implementar.** Hoy Iris carga los catálogos (zonas, planes, routers) con un job y trae clientes y servicios a demanda, al abrir cada ficha. Con la 0.8.0, al **activar** un conector recién canjeado (§1.5), Iris hace sola una primera sincronización completa.

**Qué trae y en qué orden.** Todo con lecturas que ya existen y con el token de §1.1. No hay rutas nuevas.

| Paso | Recurso | Lectura en Argos |
| --- | --- | --- |
| 1 | Empresas | `empresas[]` y `alcance_global` de la respuesta del canje (§1.3) |
| 2 | Zonas | `GET /api/v1/zonas` |
| 3 | Planes | `GET /api/v1/planes` |
| 4 | Routers | `GET /api/v1/routers` |
| 5 | Cuentas bancarias y bancos | `GET /api/v1/cuentas-bancarias` (activas e inactivas) y `GET /api/v1/bancos` (§7.8) |
| 6 | Clientes | `GET /api/v1/clientes`, paginado por cursor |
| 7 | Servicios | `GET /api/v1/servicios`, paginado por cursor |

- **Progreso.** Iris guarda por conector y por recurso: estado (`pendiente`, `en_curso`, `completo`, `fallido`), cuántos lleva, el último cursor, cuándo empezó y terminó, y el último error. La pantalla de estado lo muestra mientras corre.
- **Reanudable.** Si se corta (reinicio, Argos caído, token revocado), sigue desde el último cursor guardado de ese recurso y no vuelve a empezar. Es un job único por conector: lanzarlo dos veces no lo duplica.
- **Idempotente.** Cada fila se escribe como instantánea con su `sincronizado_en`. Un webhook que llega durante la carga se aplica normal. Entre la carga y un webhook gana la lectura más reciente de Argos. Una lista nunca revive una lápida de `servicio.eliminado` o `router.eliminado` (§2.3).
- **Sin escrituras.** La primera sincronización solo lee de Argos. No crea clientes, tickets, órdenes ni usuarios, y **no trae usuarios de Argos ni vincula a nadie** (§1.5, S8).
- **Carga.** Iris pagina con el `limit` que Argos permita y respeta `429` y `Retry-After`. El ritmo máximo, y si las listas de clientes y servicios se pueden recorrer completas sin filtro, están por confirmar (§10.1).
- **Alcance.** Si la primera carga trae todos los clientes o solo los que tienen un servicio no retirado lo decide el PM (§10.2). Lo que no se cargue sigue llegando a demanda, como hoy.
- **Sin Argos.** Iris sigue funcionando con lo que tenga (§0). Una sincronización a medias no bloquea nada, y la pantalla dice qué falta.

**(c) Pantalla de estado, en los dos paneles.** Cada lado muestra lo que **él** sabe. Ninguno inventa el estado del otro.

| Qué muestra | Panel de Iris (Administración → Integraciones → Argos) | Panel de Argos (Integraciones → Iris) |
| --- | --- | --- |
| Conexión | Conector activo o apagado, desde cuándo y quién lo activó | Lo que `GET /integraciones/iris` ya devuelve: `estado_canje`, `canje_vence_en`, `desconectado_en`, `motivo_desconexion`, `reconexion_pendiente_desde`, `reconexion_vence_en` y `requiere_reconectar` |
| Versión de contrato de cada lado | La propia y la de Argos (`contrato` y `version_argos` del canje y de `GET /integraciones/iris/estado`) | La propia y la que Iris informó con `PUT /integraciones/iris/contrato` |
| Qué se sincronizó y cuándo | Por recurso: cuántos, último `sincronizado_en` y estado de la primera carga | Lo que Iris le informe (ver abajo) |
| Qué falló | Último error por recurso, webhooks entrantes rechazados y pendientes hacia Argos (`integracion.pendientes`) | Webhooks a Iris fallidos o en reintento, y el último `2xx` de Iris |
| Empresas | Las del alcance y `alcance_global` | Las del usuario técnico Iris |

- Con versiones de contrato distintas, las dos pantallas dicen qué funciones quedan apagadas por eso (§4.1), no solo los números de versión.
- Ninguna de las dos pantallas muestra tokens ni secretos (§1.5, S5): solo si existen y cuándo se generaron.
- **No está definido cómo se entera Argos del avance de la sincronización de Iris:** hoy no hay un campo ni una ruta para eso (§10.1). Hasta que se acuerde, el panel de Argos muestra solo lo suyo.

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

Al cerrar órdenes, Iris nunca registra pagos ni asientos. Solo pide a Argos el cargo único, y Argos factura y contabiliza. El único pago que nace de una llamada de Iris es el de un comprobante aprobado por un usuario de Argos vinculado (§7.7), y también lo registra Argos.

### 3.1 Idempotencia en Argos

- Con la misma `Idempotency-Key`, la misma ruta y el mismo cuerpo, Argos devuelve el `2xx` original (mismo status, cuerpo e id) con `Idempotent-Replayed: true` y no crea nada.
- La huella es el sha256 de método + URL completa con query + el cuerpo JSON **crudo**: el orden de las claves y los campos extra cuentan. Un reintento manda exactamente los mismos bytes.
- Misma clave con otro cuerpo u otra ruta: `422 idempotency_key_reutilizada` (de negocio, no se reintenta). Primera petición aún en curso: `409 idempotencia_en_curso` (se reintenta con espera). Un `4xx` o `5xx` no guarda la clave.
- En sentido contrario (Argos → Iris, §6), Iris responde `409` con un `type` que termina en `idempotencia_en_curso` (mensajería) o `idempotencia_en_proceso` (resto de su API). Argos reintenta cualquiera de los dos con la misma clave y los mismos bytes; otro `409` es de negocio y no se reintenta (0.6.2).
- El alcance de la clave es el actor (el usuario técnico «Iris»). Argos la conserva al menos 30 días.
- En aprobar y rechazar comprobantes (§7.7, 0.7.0) la huella incluye además `X-Iris-Actor`: la misma clave con otro actor responde `422 idempotency_key_reutilizada` y nunca un replay.
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
- **0.8.0 (borrador):** qué hace cada lado cuando el otro no la habla está en §4.1. No hay mecanismos nuevos: se usan `contrato`, `version_argos` y `PUT /integraciones/iris/contrato`.

## 4. Versionado

Semver. Esta versión 0.8.0 es un borrador **sin espejo** (ver «Gobierno»); la última versión con espejo es la 0.7.1. Desde 1.0.0, solo cambios compatibles en versiones menores.

- **0.8.0** (18 nov 2026, **borrador de Iris, sin espejo**; pendiente de la revisión de Argos y de las decisiones del PM de §10.2): cuatro bloques, todos aditivos sobre 0.7.1. **A, reconexión temporal sin personal de caja** (era la 0.7.2; lado Argos en su rama `lider/iris-072`): §7.3: `motivo` acepta `fuera_de_horario` | `sin_personal` (otro valor, `422` con `errors[0].field = "motivo"`); `reglas_promesa.reconexion_temporal.vigencia` = `horas` (por defecto) | `medianoche`, con el vencimiento real siempre en `hasta`; el criterio de `sin_personal` es de Iris y queda auditado en Argos. §7.4 y §2.3: `motivo_reconexion` en la promesa (`GET /promesas`) y en los webhooks `promesa.*`. **B, cuentas bancarias de Argos como única fuente:** §7.8 (nueva): `titular` y `tipo` en la cuenta, webhook `cuenta_bancaria.cambiada`, instantánea de lectura en Iris y fin de las cuentas escritas a mano (lista «a migrar» en §10.3). **C, más datos del servicio:** §2.4 (nueva): `precio_mensual`, `moneda` y `router_nombre`; qué entra a la instantánea, los permisos propuestos `servicios.ver_red` y `servicios.ver_ubicacion` en Iris y qué recibe el asistente de IA. **D, conexión de un paso:** §1.5 (nueva): los requisitos de seguridad de Argos (S1–S8), el cuerpo del canje por escrito con el receptor de Iris, y la comparación de la huella TLS como capa extra; §2.5 (nueva): primera sincronización automática, con progreso y reanudable, y pantalla de estado en los dos paneles. §4.1 (nueva): compatibilidad y errores. §5: pedidos A25–A28 y B4–B7. §10 (nueva): lo que falta confirmar con Argos y con el PM. Es compatible: no cambia ningún campo ni ruta de 0.7.1; un Argos 1.17.x sin estos cambios sigue funcionando con Iris, y al revés (§4.1). Requiere la primera versión de Argos que la implemente (después de 1.17.0).
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

### 4.1 Compatibilidad de la 0.8.0 (borrador)

La 0.8.0 es aditiva sobre la 0.7.1. Un Argos 1.17.x sin estos cambios sigue funcionando con un Iris 0.8.0, y un Iris 0.7.1 sigue funcionando con un Argos que ya los tenga.

**Cómo sabe cada lado qué soporta el otro.** No hay mecanismos nuevos: es el de «Versión y compatibilidad» de §3.3.

- **Iris sabe de Argos** por `contrato` y `version_argos`, que vienen en la respuesta del canje y en `GET /integraciones/iris/estado`. Iris los relee al activar el conector y en su revisión periódica de versión.
- **Argos sabe de Iris** por `contrato` en el cuerpo del canje y por `PUT /integraciones/iris/contrato { contrato, receptor_argos }`, que Iris manda al activar y cada vez que cambia de versión.
- Cada lado usa una función de la 0.8.0 **solo si el otro informó un contrato ≥ 0.8.0**. Con menos, se comporta como en 0.7.1.

| Función 0.8.0 | Iris 0.8.0 con un Argos sin ella (contrato < 0.8.0) | Argos 0.8.0 con Iris 0.7.1 |
| --- | --- | --- |
| `motivo: "sin_personal"` (§7.3) | Iris no lo manda: en horario hábil responde su `422 reconexion_no_corresponde` y sigue una persona, como hoy | Iris solo manda `fuera_de_horario`; Argos lo atiende igual |
| `vigencia: "medianoche"` (§7.3) | No aplica: es de la plantilla de Argos | Iris 0.7.1 ya lee `hasta` de la respuesta y lo pasa a la plantilla `reconexion_temporal`; no asume horas |
| `motivo_reconexion` en la promesa (§7.4) | El campo no viene: Iris lo trata como `null` y usa el motivo que guardó al pedirla | Iris ignora el campo que no conoce |
| `titular` y `tipo` de la cuenta (§7.8) | No vienen: Iris muestra banco, número, alias y moneda, y no le ofrece al cliente una cuenta sin `titular` (§7.8) | Iris ignora los campos que no conoce |
| Webhook `cuenta_bancaria.cambiada` (§2.3) | No llega: Iris refresca la instantánea de cuentas al usarla si tiene más de 24 h, y siempre relee la cuenta antes de registrar una boleta | Argos no lo envía a conectores con contrato < 0.8.0 |
| `precio_mensual`, `moneda`, `router_nombre` (§2.4) | No vienen: Iris no muestra el precio del servicio ni lo calcula, y el asistente no lo dice. El nombre del router sale del catálogo, como hoy | Iris ignora los campos que no conoce |
| Receptor de Iris en el canje (§1.5) | Iris ya lo manda. Si Argos no lo guarda solo, se configura aparte como hoy | Sin cambio |
| Primera sincronización (§2.5) | Funciona igual, porque usa lecturas que ya existen. Sin `titular` en las cuentas ni precios | No aplica: Iris 0.7.1 no la hace |
| Pantalla de estado (§2.5) | Iris muestra la suya con lo que tiene | Argos muestra la suya; lo de Iris, «no informado» |
| Huella TLS (§1.5) | Iris la fija por su cuenta. Sin la pantalla de Argos, el admin no tiene con qué compararla y queda «sin comparar» | Sin cambio |

- Argos solo envía el webhook nuevo (`cuenta_bancaria.cambiada`) a conectores con contrato ≥ 0.8.0, igual que hizo con los de 0.5.0 y 0.6.0. Los campos nuevos en respuestas y en webhooks que ya existen (`motivo_reconexion`) pueden ir siempre: quien no los conoce los ignora.
- Ningún campo ni ruta de 0.7.1 cambia de forma ni de significado. `motivo: "fuera_de_horario"` y `vigencia: "horas"` siguen siendo los valores por defecto.
- Los permisos `servicios.ver_red` y `servicios.ver_ubicacion` son internos de Iris y no dependen de la versión de Argos.

**Errores de la 0.8.0.** No hay códigos de error nuevos acordados. Estos son los que tocan las secciones nuevas. Todos son de negocio y no se reintentan.

| Dónde | Status | Código o regla | Cuándo |
| --- | --- | --- | --- |
| `POST /comprobantes/:id/reconexion-temporal` (§7.3) | 422 | validación, con `errors[0].field = "motivo"` | `motivo` distinto de `fuera_de_horario` y `sin_personal` |
| `POST /comprobantes/:id/reconexion-temporal` (§7.3) | 422 | `reconexion_no_permitida`, con `errors[0].rule` | Las reglas de 0.7.1 (`no_habilitada`, `cupo_agotado`, `comprobante_no_pendiente`, `comprobante_no_cubre`, `servicio_no_cortado`, `suspension_manual`, `promesa_abierta`, `servicio_de_otro_cliente`), con cualquiera de los dos motivos |
| Iris, `conversaciones.pedir_reconexion` (§7.3) | 422 | `reconexion_no_corresponde` | En horario hábil con alguien de caja disponible, con `sin_personal` apagado en Iris, o con un Argos que no lo soporta. Lo responde Iris sin llamar a Argos |
| `GET /cuentas-bancarias` y `GET /cuentas-bancarias/:id` (§7.8) | 403 | `empresa_no_permitida` | Cuenta de una empresa fuera del alcance del usuario técnico Iris |
| `POST /comprobantes` (§7.2, §7.8) | 422 | `cuenta_de_otra_empresa` | Sin cambio. Con una cuenta `activa: false`, por confirmar (§10.1) |
| Canje (§1.5) | 401 | `codigo_invalido` | `code_verifier` que no coincide; el código queda gastado (S1, S4) |
| Canje (§1.5) | 410 | `enlace_vencido` | Repetir un canje cuyo token ya se revocó |
| Canje (§1.5), en Iris | 422 | `webhook_invalido` | Falta el receptor de Argos o su host no es el de la base del canje |
| Canje (§1.5), en Argos | 422 | por definir (§10.1) | El host del receptor de Iris no es el de `base_url` ni el origen aprobado |
| Rutas de §1.3 y §7.7 con el conector apagado | 409 | `iris_apagado` | Sin cambio |

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
| A22 | **Nombre de la bodega del técnico** (0.6.4, §3.3): `nombre` en cada elemento de `bodegas[]` de `GET /inventario/tecnicos`, para que Iris elija la bodega de una lista sin teclear números. | listo en Argos |
| A23 | **Aprobar y rechazar comprobantes desde Iris** (0.7.0, §7.7): `POST /comprobantes/:id/aprobar` y `POST /comprobantes/:id/rechazar` con el token de Iris, solo `canal = iris`, autorizadas por el usuario de Argos vinculado al `X-Iris-Actor` (los cinco `403 actor_*`), con el actor en la huella de idempotencia y la auditoría a nombre del usuario vinculado. Permisos `comprobantes.aprobar` y `comprobantes.rechazar` en el token de Iris, con migración de los que existen. `destino` opcional en `POST /integraciones/iris/sso/tickets` (§1.3). Pedido del PM. | en curso en Argos |
| A24 | **Quién decidió el comprobante e instalación en el vínculo** (0.7.1): `verificado_iris_actor` en el `Comprobante` (`GET /comprobantes/:id`, la lista y la respuesta de aprobar y rechazar, §7.7) y en los webhooks `comprobante.aprobado` / `comprobante.rechazado` (§7.2), con `null` cuando decide caja en el panel. `instalacion` (string o `null`) en `vinculacion` del canje del autologin Iris → Argos y la pantalla de confirmación del vínculo con el sistema, el nombre y el correo del usuario de Iris, la instalación y el aviso, antes de pedir el correo y la contraseña (§1.3). | en curso en Argos |
| A25 | **Reconexión temporal sin personal de caja** (0.8.0, borrador, §7.3): `motivo: "sin_personal"` en `POST /comprobantes/:id/reconexion-temporal`, `vigencia: "horas" \| "medianoche"` en la plantilla y `motivo_reconexion` en la promesa y en los webhooks `promesa.*`. | en la rama `lider/iris-072` de Argos; sin espejo |
| A26 | **Cuentas bancarias como única fuente** (0.8.0, borrador, §7.8): `titular` y `tipo` (`monetaria` \| `ahorro`) en `GET /cuentas-bancarias`, y el webhook `cuenta_bancaria.cambiada`. Las desactivadas vienen con `activa: false` y nunca se borran. | propuesto por Argos; por confirmar (§10.1) |
| A27 | **Más datos del servicio** (0.8.0, borrador, §2.4): `precio_mensual`, `moneda` y `router_nombre` en `GET /servicios/:id` y en la lista. Caja o nodo, por confirmar. | propuesto por Argos; por confirmar (§10.1) |
| A28 | **Conexión de un paso, lado Argos** (0.8.0, borrador, §1.5 y §2.5): guardar en el mismo canje el receptor de Iris; pantalla de estado de la integración en el panel de Argos; mostrar la huella TLS en la pantalla de aprobación. Con los requisitos S1–S8. | propuesta conjunta; por confirmar (§10.1) |

### 5.2 ArgosISP solicita a Iris

| # | Pedido | Estado |
| --- | --- | --- |
| B1 | **Servicio de mensajería** (0.6.0, §6): API de avisos, plantillas, permisos de contacto y webhooks de entrega. Lo ofrece Iris aunque Argos no lo pidió; Argos lo adopta como canal WhatsApp (A21) y pide además las plantillas `factura_vencida` y `servicio_reactivado`. | en curso en Iris |
| B2 | **Autologin a una conversación** (0.6.0, §7.2): destino `conversacion` en `POST <iris>/api/v1/integraciones/argosisp/sso/tickets`, con el id de `X-Iris-Conversacion`, para el botón «Ver conversación en Iris» de la cola de caja. | pendiente en Iris (cuando exista la pantalla de conversaciones) |
| B3 | **Aprobar y rechazar desde la bandeja** (0.7.0, §7.7): emitir el ticket de autologin Iris → Argos (§1.3) con el botón «Vincular mi usuario de Argos»; los botones «Aprobar y registrar pago» y «Rechazar» en la boleta de la conversación, solo para una persona y nunca para el asistente de IA; y el manejo de los cinco `403` (`actor_sin_vinculo`, `actor_inactivo`, `actor_sin_permiso`, `actor_sin_empresa`, `actor_ia_no_permitido`). | listo en Iris |
| B4 | **Criterio de `sin_personal`** (0.8.0, borrador, §7.3): Iris la pide solo en horario hábil y sin nadie de caja disponible, apagada de fábrica y con verificación fuerte; le dice al cliente la fecha y la hora exactas de `hasta`. | propuesto por el líder de Iris; pendiente del PM (§10.2) |
| B5 | **Iris sin cuentas bancarias propias** (0.8.0, borrador, §7.8): instantánea de lectura, solo cuentas activas de la empresa de la conversación y migración de §10.3. | pendiente en Iris |
| B6 | **Permisos para los datos sensibles del servicio** (0.8.0, borrador, §2.4): `servicios.ver_red` y `servicios.ver_ubicacion` en Iris, y el asistente de IA sin IP, `user_ppp` ni coordenadas. | propuesto por el líder de Iris; pendiente del PM (§10.2) |
| B7 | **Conexión de un paso, lado Iris** (0.8.0, borrador, §1.5 y §2.5): primera sincronización automática, con progreso y reanudable; pantalla de estado en el panel de Iris; fijar y comparar la huella TLS de Argos. | pendiente en Iris |

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
| `reconexion_temporal` | `hasta` | Iris, al recibir el 201 de §7.3. Desde 0.8.0 (borrador), Iris escribe en `hasta` la fecha y la hora exactas en hora de Guatemala, tomadas del `hasta` de Argos | Argos no avisa por su cuenta (§7.3) |
| `promesa_aceptada` | `vence_en`, `monto` | Iris, al recibir el 201 de §7.4 con `estado = activa` | Argos no avisa por su cuenta de una promesa creada por Iris (§7.4) |

En `boleta_rechazada` y `boleta_falta_dato`, `motivo` es un texto fijo de Iris según la `causa` del rechazo. Iris solo le reenvía al cliente el motivo libre (el de `POST /comprobantes/:id/rechazar` o el `motivo_rechazo` que escribe caja) si, tras normalizarlo (Unicode NFKC y sin caracteres de formato), contiene únicamente letras del español, espacios y los signos `,;:¿?¡!()`. Cualquier dígito o punto lo deja fuera (salvo el punto final de la frase) y el cliente recibe solo el texto fijo de la causa. El motivo completo se guarda y se envía a Argos sin cambios.

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

Los clientes mandan por chat o llamada la foto de su boleta de depósito o piden una promesa de pago. Los atiende un asistente de IA de Iris o una persona. **Iris nunca registra un pago, una promesa ni una reconexión por su cuenta: siempre lo pide a Argos, y Argos decide.** Desde 0.7.0, cuando la boleta la atiende una persona, esa persona puede aprobarla o rechazarla desde la conversación (§7.7). También ahí decide Argos: Iris solo transmite la decisión de un usuario de Argos vinculado, que es quien tiene el permiso. El asistente de IA nunca puede.

En Argos todo esto ya existe para el portal del cliente y el panel; Iris usa las mismas piezas:

| Lo que pide Iris | Pieza de Argos | Estado en Argos |
| --- | --- | --- |
| Boleta de pago | **Comprobante de depósito** (`comprobantes.registrar`): `pendiente` → `aprobado` (crea el pago) o `rechazado` | Existe, con canal `iris` (PR #217) |
| Aprobar o rechazar la boleta (una persona, 0.7.0) | Las acciones de caja `comprobantes.aprobar` y `comprobantes.rechazar`, ahora también con el token de Iris y a nombre del usuario de Argos vinculado (§7.7) | En curso (A23) |
| Promesa de pago | **Promesa** (`promesas.crear`, `POST /promesas`), con reglas por plantilla y por canal (`ia`, `operador`) | Existe |
| Política de promesas | **Simulador** (`POST /promesas/simular`) | Existe |
| Reconexión temporal | **Promesa de tipo `reconexion_temporal`** ligada a un comprobante pendiente | Existe (PR #217) |
| Reconexión temporal sin personal de caja (0.8.0, borrador) | La misma promesa, con `motivo: "sin_personal"` (§7.3) | En la rama `lider/iris-072` de Argos (A25) |
| Cuentas donde deposita el cliente (0.8.0, borrador) | **Cuentas bancarias** de la empresa (`cuentas_bancarias.ver`), única fuente (§7.8) | Existe en 1.17.0; `titular`, `tipo` y el evento, propuestos (A26) |
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
| Actor | Cada llamada de §7 lleva `X-Iris-Actor` = `asistente_ia` \| `usuario:<id en Iris>` y `X-Iris-Conversacion` = id de la conversación. La forma exacta (0.7.1) es `^(asistente_ia\|usuario:[A-Za-z0-9_-]{1,64})$` para `X-Iris-Actor` y `^[A-Za-z0-9._:-]{1,128}$` para `X-Iris-Conversacion`. Fuera de formato, o si faltan en una ruta de §7: `400 cabecera_iris_requerida`. Argos audita con actor `token` del usuario técnico Iris y guarda las dos cabeceras en la fila de auditoría, donde se pueden buscar. El canal de la auditoría es `ia` con `asistente_ia` y `api` con `usuario:<id>`. Excepción (0.7.0): en aprobar y rechazar (§7.7) el actor de la auditoría es el usuario de Argos vinculado a `usuario:<id>`, y el token de Iris queda como «vía». |
| Empresa | Cada documento es de una empresa: la del servicio (promesas) o la de la cuenta bancaria (comprobantes). Si Iris manda `empresa_id` y no coincide: `422 empresa_no_coincide`. Fuera del alcance del usuario técnico Iris: `403 empresa_no_permitida`. |
| Idempotencia | Todas las escrituras de §7 llevan `Idempotency-Key` (obligatoria: `400 idempotency_key_requerida`) con las reglas de §3.1. En §7.7 la huella incluye además `X-Iris-Actor`. |

### 7.2 Comprobantes de pago (A16)

**Leer las cuentas.** El cliente deposita en una cuenta bancaria de la empresa. Iris lee las cuentas con `GET /api/v1/cuentas-bancarias?activa=true` (`cuentas_bancarias.ver`; solo las de moneda `GTQ` reciben depósitos) y los bancos de origen con `GET /api/v1/bancos`. El asistente le pregunta al cliente a cuál depositó si la foto no lo dice. Desde 0.8.0 (borrador) este catálogo es además la única fuente de las cuentas que Iris le ofrece al cliente (§7.8).

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

### 7.3 Reconexión temporal (A17; ampliada en 0.8.0, borrador)

Cuando un cliente cortado manda su comprobante y nadie de caja puede revisarlo pronto, Iris puede pedir que Argos lo reconecte mientras se revisa. En Argos es una **promesa de pago** de tipo `reconexion_temporal`, ligada al comprobante: `evaluarEstadoServicio` mantiene el servicio activo mientras esté activa, igual que cualquier promesa. Nunca es un cambio de estado directo.

`POST /api/v1/comprobantes/:id/reconexion-temporal` (`comprobantes.reconexion_temporal`) con `{ servicio_id?, motivo }` e `Idempotency-Key = iris:reconexion:<comprobante_id>`. `servicio_id` es obligatorio solo si el comprobante no tiene servicio.

- **`motivo`** = `fuera_de_horario` | `sin_personal`. `sin_personal` es nuevo en 0.8.0 (borrador, A25). Cualquier otro valor responde `422` con `errors[0].field = "motivo"`.

    | `motivo` | Qué significa | Desde |
    | --- | --- | --- |
    | `fuera_de_horario` | El comprobante llegó fuera del horario hábil de la empresa | 0.6.0 |
    | `sin_personal` | Es horario hábil, pero no hay nadie de caja que pueda revisarlo ahora | 0.8.0 (borrador) |

- Las reglas y los `422` son **los mismos con los dos motivos** (plantilla, cupo, cubre lo vencido…). Con cualquiera de los dos, el comprobante queda `pendiente` en la cola de caja.
- Respuesta `201 { promesa_id, reconexion_id, hasta, servicio: { id, estado } }`. `reconexion_id` = `promesa_id` (alias para Iris); `hasta` = vencimiento de la promesa.

```json
{ "servicio_id": 4410, "motivo": "sin_personal" }
```

```json
{
  "promesa_id": 9012,
  "reconexion_id": 9012,
  "hasta": "2026-11-19T06:00:00Z",
  "servicio": { "id": 4410, "estado": "activo" }
}
```

El ejemplo es una reconexión pedida el 18 nov a las 15:20 de Guatemala con `vigencia: "medianoche"`: `hasta` es la medianoche de Guatemala. El formato exacto de `hasta` está por confirmar (§10.1).

- Política (la fija Argos en la plantilla de facturación, bloque `reconexion_temporal` de `reglas_promesa`, exceptuable por servicio). Valores aprobados por el PM (5 nov 2026):
    - apagada por defecto (`permitida: false`) hasta que cada empresa la active;
    - a lo sumo **una por periodo de facturación y servicio** (`max_por_periodo: 1`), aparte del cupo de promesas normales;
    - solo si el comprobante está `pendiente`, es del mismo cliente y su `monto` cubre **todo lo vencido** del servicio;
    - dura `horas_max` (propuesta: 24 h) desde que se crea, con `vigencia: "horas"`.
- **Vigencia (0.8.0, borrador).** `reglas_promesa.reconexion_temporal.vigencia` = `horas` (por defecto) | `medianoche`.
    - `horas`: como hasta hoy, dura `horas_max` desde que se crea.
    - `medianoche`: dura hasta la siguiente medianoche de Guatemala. Si faltan menos de 60 minutos para esa medianoche, dura hasta la medianoche siguiente (menos de 25 h en total). `horas_max` no se usa.
    - **El vencimiento real llega siempre en `hasta`.** Iris no asume horas ni calcula la medianoche: lee `hasta`.
- `422 reconexion_no_permitida`, con la razón en `errors[0].rule`: `no_habilitada`, `cupo_agotado`, `comprobante_no_pendiente`, `comprobante_no_cubre`, `servicio_no_cortado` (el servicio ya está activo), `suspension_manual` (lo suspendió un operador: solo él lo reactiva), `promesa_abierta` (ya tiene una promesa activa o pendiente), `servicio_de_otro_cliente`. Es de negocio y no se reintenta.
- **Cómo termina** (webhooks `promesa.*` de §7.4, con `tipo = reconexion_temporal`). Aquí «caja» es quien aprueba o rechaza el comprobante: en el panel o desde Iris (§7.7). No cambia en 0.8.0 ni depende del motivo:
    - caja aprueba el comprobante y el pago salda lo vencido → `promesa.cumplida`;
    - caja lo rechaza → la promesa se cancela al instante → `promesa.cancelada` con `motivo = comprobante_rechazado`, y el servicio se re-evalúa: vuelve a cortarse (si la plantilla tiene hora de corte y aún no llega, espera a esa hora, como cualquier corte);
    - llega `hasta` con el comprobante aún pendiente → `promesa.cancelada` con `motivo = vencida_sin_revisar` y re-evaluación; el comprobante sigue en la cola de caja. No cuenta como promesa incumplida en el historial del cliente: la demora fue de caja;
    - caja aprueba pero el pago no salda lo vencido → sigue activa hasta `hasta` y entonces `promesa.incumplida`, como cualquier promesa.
- **`motivo_reconexion` (0.8.0, borrador).** La promesa guarda con qué motivo se pidió: `motivo_reconexion` = `fuera_de_horario` | `sin_personal` | `null`. Viene en `GET /promesas` y en los cuatro webhooks `promesa.*` (§7.4). No es el `motivo` de `promesa.cancelada`, que dice por qué terminó.
- Iris nunca pide una reconexión temporal sin un comprobante registrado. Argos no avisa al cliente por su cuenta: Iris le explica en los cuatro casos.

**`sin_personal`: Argos confía en Iris (0.8.0, borrador).** Argos no tiene un horario de caja propio, así que no puede comprobar si había o no personal. El criterio es **de Iris**, y cada petición queda auditada en Argos con `X-Iris-Actor`, el canal y el motivo (§7.1). Las barreras de Argos son las mismas de siempre y no dependen de Iris:

- la plantilla debe tener la reconexión habilitada (viene apagada de fábrica);
- el cupo por periodo;
- el comprobante debe cubrir todo lo vencido;
- sin suspensión manual y sin otra promesa abierta.

**Del lado de Iris (criterio PROPUESTO por el líder, pendiente de confirmación del PM; §10.2).** Amplía `conversaciones.md` §17.8, que hoy solo pide la reconexión fuera de horario.

- **Cuándo pide `sin_personal`.** Solo cuando se cumplen las dos cosas: es **horario hábil** de la empresa (el mismo horario con el que Iris decide hoy `fuera_de_horario`) y **ningún usuario con rol `caja` está en estado disponible en esa empresa**. Si hay al menos una persona de caja disponible, Iris no la pide: responde su `422 reconexion_no_corresponde` y la boleta la revisa esa persona, como hoy.
- **Cuándo pide `fuera_de_horario`.** Igual que hoy: fuera del horario hábil. Iris nunca manda los dos; el motivo sale del reloj y de la disponibilidad, no de lo que diga el cliente ni el modelo.
- **Apagado de fábrica.** `sin_personal` tiene su propio interruptor en Iris, apagado por defecto y aparte del de la reconexión fuera de horario. Además, Iris solo lo manda si Argos informó contrato ≥ 0.8.0 (§4.1). Con cualquiera de las dos cosas en falta, se comporta como en 0.7.1.
- **Verificación fuerte.** La exige siempre, sin opción, igual que la reconexión fuera de horario de hoy (`conversaciones.md` §17.8).
- **Un comprobante registrado.** Como hoy: una boleta registrada en Argos en esa conversación.
- **Qué le dice el asistente al cliente.** Dos cosas, siempre: la **fecha y la hora exactas** de `hasta` en hora de Guatemala (por ejemplo, «su servicio queda activo hasta las 12:00 de la noche de hoy, miércoles 18 de noviembre»), y que **su comprobante queda pendiente de revisión**: la reconexión no es la confirmación del pago. El texto lo arma Iris con el `hasta` de Argos; el modelo no escribe la hora.
- **Evento.** `reconexion.solicitada` suma el motivo `sin_personal` a su campo `motivo` (hoy solo `fuera_de_horario`). Es un cambio propuesto en `eventos.md`.
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
3. **Webhooks Argos → Iris** (§2.3), para promesas de cualquier canal dentro del alcance; solo a conectores con contrato ≥ 0.6.0. Payload común: promesa_id, empresa_id, cliente_id, servicio_id, tipo (`promesa` \| `reconexion_temporal`), canal (`portal` \| `ia` \| `operador` \| `supervisor`), comprobante_id (o null), vence, monto. Desde 0.8.0 (borrador) suma `motivo_reconexion`: `fuera_de_horario` \| `sin_personal` \| `null` (`null` cuando no es una reconexión temporal); el mismo campo viene en la promesa de `GET /promesas`. Un Argos anterior no lo manda, e Iris lo trata como `null`.

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

### 7.8 Cuentas bancarias: Argos es la única fuente (0.8.0, borrador)

Las cuentas donde los clientes depositan son un catálogo **de Argos**, por empresa: son las mismas con las que se registra el comprobante (§7.2). Desde la 0.8.0, Iris no tiene cuentas propias ni textos con números de cuenta escritos a mano. Lo que el agente o el asistente le dice al cliente sale de este catálogo.

**Lo que existe hoy en Argos (1.17.0).** `GET /api/v1/cuentas-bancarias` y `GET /api/v1/cuentas-bancarias/:id`, con el permiso `cuentas_bancarias.ver` que el token de Iris tiene desde 0.6.0 (§1.1). El catálogo de bancos es `GET /api/v1/bancos`. Campos de la cuenta: `id`, `empresa_id`, `banco_id`, `banco_nombre`, `numero`, `alias`, `moneda`, `activa` y `version`. Hoy **no** existen `titular`, `tipo` ni un evento de cambio.

**Lo que Argos propone agregar en 0.8.0 (A26).**

- `titular` (string): a nombre de quién está la cuenta.
- `tipo`: `monetaria` | `ahorro`.
- Webhook `cuenta_bancaria.cambiada` (§2.3), solo con nombres de campos, nunca valores.
- Una cuenta que se deja de usar queda con `activa: false`. **Nunca se borra**: así una boleta vieja siempre encuentra su cuenta.

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
  "version": 4
}
```

Webhook Argos → Iris, con el cuerpo de §2.3:

```json
{
  "id": "<id estable del webhook>",
  "evento": "cuenta_bancaria.cambiada",
  "ocurrido_en": "2026-11-18T21:20:00Z",
  "payload": { "cuenta_bancaria_id": 7, "empresa_id": 3, "campos": ["titular", "activa"], "activa": false }
}
```

**Del lado de Iris.**

- **Instantánea de lectura.** Iris guarda las cuentas en una tabla de instantánea (propuesta: `integracion.argos_cuentas_bancarias`) con los campos de arriba y `sincronizado_en`. Se llena en la primera sincronización (§2.5), con activas e inactivas, y se refresca con `cuenta_bancaria.cambiada`: al recibirlo, Iris relee `GET /cuentas-bancarias/:id` y guarda lo que diga Argos, no el payload. Sin el webhook (Argos anterior a 0.8.0), se refresca al usarla si tiene más de 24 h. Nunca se edita en Iris.
- **Sin cuentas propias.** Iris no tiene pantalla, tabla ni ajuste para crear o editar cuentas. Lo que hay que migrar está en §10.3.
- **Solo activas y solo de la empresa.** El agente y el asistente ofrecen únicamente cuentas con `activa: true`, de la **empresa de la conversación** y en la moneda que recibe depósitos (`GTQ`, §7.2). Una conversación sin empresa asignada no ofrece ninguna. El filtro es del servidor de Iris, no de la pantalla.
- **Antes de registrar una boleta,** Iris relee la cuenta elegida en Argos (`GET /cuentas-bancarias/:id`). Si no es de la empresa, no manda el comprobante y lo dice. Una instantánea vieja nunca decide a dónde se registra un pago. Qué hacer con una cuenta que se desactivó después del depósito está por confirmar (§10.1).
- **Sin `titular`** (Argos anterior a 0.8.0, o cuenta sin el dato), Iris no le ofrece esa cuenta al cliente por chat ni por voz. La cuenta sigue sirviendo para registrar una boleta que ya se depositó. Decirle a un cliente dónde depositar sin decirle a nombre de quién facilita un fraude.
- **Cómo se le dice al cliente.** Con un texto fijo de Iris que lleva banco, tipo, número, titular y moneda.
    - Con una **persona**: un botón «Enviar cuentas» en la conversación inserta ese texto. La persona no lo escribe a mano.
    - Con el **asistente de IA** (PROPUESTA del líder de Iris, §10.2): el modelo no recibe los números ni los titulares. Escribe una marca y el servidor de Iris la reemplaza por el texto fijo al enviar, igual que ya hace con el saldo (`conversaciones.md` §17.9). Así el número no viaja al proveedor de IA y el modelo no puede cambiar un dígito.
- **Dónde queda el número de cuenta.** En la instantánea, en el mensaje que se le envió al cliente (es parte de la conversación) y en la lectura de Argos. No va en ningún evento de Iris ni en su auditoría: ahí va solo `cuenta_bancaria_id`, como hoy en la boleta. El webhook `cuenta_bancaria.cambiada` tampoco lo trae.
- **Si Argos no responde.** Para ofrecer cuentas, Iris usa la instantánea marcada `stale` (§0) si tiene menos de 24 h; con más, no ofrece cuentas y pasa a una persona. Para registrar la boleta, la relectura es obligatoria: sin Argos, la boleta queda en cola como hoy.

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

## 10. Por confirmar de la 0.8.0 (borrador)

Mientras esta sección tenga puntos abiertos, la 0.8.0 sigue siendo borrador y no se copia al repositorio de Argos. Cada punto se cierra moviendo la respuesta a su sección y borrándolo de aquí.

### 10.1 POR CONFIRMAR con Argos

**Bloque A: reconexión temporal (§7.3, §7.4).**

1. Formato de `hasta` en la respuesta `201` y en la promesa: ¿es un instante RFC 3339 en UTC? El ejemplo de §7.3 lo supone.
2. Con `vigencia: "medianoche"`, ¿«medianoche de Guatemala» son las 00:00 del día siguiente (`06:00:00Z`) o el último instante del día (`05:59:59Z`)? Iris le dice al cliente la hora que salga de `hasta`.
3. El código exacto del `422` por un `motivo` desconocido (¿`validacion`, como en §3.2?) y el `rule` de `errors[0]`.
4. ¿Un Argos 1.17.x, sin la 0.8.0, responde ese mismo `422` a `sin_personal`? Iris no lo manda sin contrato ≥ 0.8.0; es para saber qué mostrar si pasa.
5. ¿`motivo_reconexion` viene también en la respuesta `201` de `POST /comprobantes/:id/reconexion-temporal` y en la `Promesa` de `POST /promesas`, o solo en `GET /promesas` y los webhooks? ¿Vale `null` en toda promesa con `tipo = promesa`?
6. ¿El cupo `max_por_periodo` es uno solo para los dos motivos, o cada motivo tiene el suyo?
7. ¿La auditoría de Argos permite filtrar por motivo, para revisar cuántas reconexiones `sin_personal` pidió Iris?
8. ¿Argos quiere que Iris mande algún dato más para auditar el criterio de `sin_personal` (por ejemplo, cuántas personas de caja había y cuántas disponibles)? Hoy no hay campo para eso y este borrador no lo agrega.

**Bloque B: cuentas bancarias (§7.8).**

9. Los nombres finales `titular` y `tipo` (`monetaria` | `ahorro`), si pueden venir `null` en cuentas viejas y sus límites de largo.
10. Los nombres posibles en `campos[]` de `cuenta_bancaria.cambiada`. ¿Se emite también al **crear** una cuenta? ¿El `activa` del payload es el valor después del cambio?
11. ¿`GET /cuentas-bancarias` sin `activa=true` devuelve también las inactivas? ¿Pagina con `cursor` y `limit` como las demás listas?
12. `GET /cuentas-bancarias/:id` de una cuenta que no existe: ¿responde `404` con `urn:argosisp:problema:no_encontrado`, como en §2.3?
13. `POST /comprobantes` con una `cuenta_bancaria_id` que está `activa: false`: ¿qué responde? El cliente pudo depositar antes de que se desactivara.
14. **Desajuste a revisar.** El cliente de Iris lee hoy de `GET /cuentas-bancarias` los campos `nombre` y `banco` (`apps/api/internal/integracion/argos/conversaciones.go:146-153`), y Argos dice que los campos son `alias`, `banco_id` y `banco_nombre`. ¿Argos devuelve además `nombre` y un objeto `banco`, o Iris está leyendo campos que no existen? Si es lo segundo, es un fallo de Iris que se corrige con la migración de §10.3.
15. ¿Solo las cuentas en `GTQ` reciben depósitos (§7.2), o Argos piensa abrir otras monedas? Iris filtra por `GTQ` en su código.
16. ¿El catálogo de bancos (`GET /bancos`) cambia? Si cambia, ¿lo avisa algún evento, o Iris lo relee cada 24 h?

**Bloque C: datos del servicio (§2.4).**

17. Los nombres finales `precio_mensual`, `moneda` y `router_nombre`, y que vienen en `GET /servicios/:id` **y** en la lista.
18. ¿Qué evento avisa cuando cambia `precio_mensual` sin cambiar el servicio (cambio del precio del plan o de `costo_override`)? ¿`servicio.config_cambiada` con `precio_mensual` en `campos[]`, o `catalogo.cambiado` del plan?
19. ¿`precio_mensual` incluye impuestos, descuentos o cargos adicionales, o es solo el precio base? Es lo que el asistente le va a decir al cliente.
20. Los valores posibles de `tipo_conexion` y la forma de `ipv4`, `prefijo_enlace_v6` y `prefijo_delegado_v6` (con o sin máscara; `null` o cadena vacía cuando no hay).
21. **Caja o nodo.** No está en el servicio. ¿Argos lo va a exponer, y con qué campos? ¿O la única referencia sigue siendo `planta` (A9) y la fuente es OZmap?
22. ¿`user_ppp` cambia alguna vez? Si cambia, ¿lo avisa `servicio.config_cambiada`?
23. ¿El token de Iris necesita un permiso nuevo en Argos para alguno de estos campos, o basta `servicios.ver`?

**Bloque D: conexión de un paso (§1.5, §2.5).**

24. **Receptor de Iris en el canje.** Iris ya manda `base_url`, `token` (`iris_pat_`) y `webhook { url, secreto }` en el cuerpo del canje (`apps/api/internal/integracion/conector/tipos.go:166-178`). ¿Argos ya guarda los tres en ese mismo canje? Si sí, ¿qué es lo que hoy se configura aparte en el panel de Argos? Si no, ¿qué campos toma y cuáles pide a mano?
25. El código del `422` de Argos cuando el host del receptor de Iris no es el de `base_url` ni el origen aprobado.
26. **Listas completas.** ¿`GET /clientes` y `GET /servicios` se pueden recorrer enteras con `cursor`, sin `q` ni `cliente_id`? ¿Cuál es el `limit` máximo? ¿Hay un filtro por estado (para traer solo activos) o por fecha de cambio (para reanudar sin releer todo)?
27. El ritmo que Argos acepta para la primera carga (peticiones por segundo) y si responde `429` con `Retry-After`.
28. **Estado en el panel de Argos.** ¿Cómo se entera Argos del avance de la sincronización de Iris? Hoy no hay campo ni ruta. Opciones a decidir entre los dos: que Iris lo informe junto con `PUT /integraciones/iris/contrato`, una ruta nueva en Iris que Argos lea con su `iris_pat_`, o que el panel de Argos no lo muestre.
29. **Huella TLS.** ¿En qué pantalla de Argos se muestra y de qué certificado (el de la API, el del panel o los dos)? ¿Cómo le llega a Iris para compararla? ¿Qué se hace en una renovación normal del certificado, para que la integración no se corte cada 60 a 90 días: fijar la clave pública en vez del certificado, o aceptar el cambio si la cadena sigue siendo válida y solo avisar?
30. ¿La primera sincronización arranca al **activar** en Iris, como propone este borrador, o Argos necesita enterarse de que empezó?
31. ¿Argos quiere limitar lo que el token de Iris puede leer en bloque? Por ejemplo, que la carga completa de clientes exija que el admin la haya aprobado en la pantalla de §1.4.
32. La versión de Argos que implementaría cada bloque, y si el bloque A sale antes como 0.7.2 (ya está en la rama `lider/iris-072` de Argos) o va todo junto en la 0.8.0.

### 10.2 POR CONFIRMAR con el PM

1. **Criterio de `sin_personal` (§7.3).** Propuesta del líder: Iris la pide solo si, dentro del horario hábil de la empresa, ningún usuario con rol `caja` de esa empresa está disponible. Por decidir: ¿ese es el criterio? ¿Hay que esperar unos minutos sin nadie disponible antes de pedirla? Si la empresa no tiene a nadie con rol `caja`, ¿no se pide nunca o se pide siempre?
2. **Quién enciende `sin_personal`.** Propuesta: apagado de fábrica en Iris, con un interruptor aparte del de la reconexión fuera de horario, que enciende un admin por empresa.
3. **Vigencia hasta medianoche.** Es un ajuste de la plantilla de Argos. ¿El PM quiere `medianoche`? Con «horas», una reconexión pedida a las 10:00 dura hasta las 10:00 del día siguiente.
4. **Qué roles ven la IP y los datos de red (§2.4).** Propuesta: `admin`, `supervisor`, `soporte` y `tecnico` (este solo en sus órdenes). No la ven `despacho`, `ventas`, `caja` ni `lectura`.
5. **Qué roles ven la dirección y la ubicación (§2.4).** Propuesta: `admin`, `supervisor`, `despacho`, `soporte`, `ventas` y `tecnico` (solo en sus órdenes). No la verían `caja` ni `lectura`, que **hoy sí la ven**. ¿Se les quita?
6. **El asistente y las cuentas bancarias (§7.8).** ¿El asistente da las cuentas solo, o siempre con verificación? Tres opciones: a cualquiera que las pida, solo a quien escribe desde un número registrado, o solo con verificación fuerte. Propuesta: basta el número registrado, porque son las cuentas de la empresa y no un dato del cliente; a un número desconocido se las da una persona.
7. **El asistente y el precio (§2.4).** ¿Puede decir el precio mensual del servicio sin verificación fuerte? Propuesta: sí, con número registrado; es lo que ya dice la factura.
8. **Cómo distingue el asistente los servicios (§2.4).** Propuesta: «Servicio 1 · plan · zona», sin dirección. ¿O prefiere que diga una parte de la dirección (por ejemplo, la colonia), aunque viaje al proveedor de IA?
9. **Primera sincronización (§2.5).** ¿Trae todos los clientes o solo los activos? Propuesta: solo los que tienen un servicio no retirado; el resto llega a demanda al buscarlo.
10. **Activar después de conectar (§1.5).** Hoy el conector queda apagado hasta que un admin de Iris pulsa «Activar». ¿Se mantiene ese paso, o la conexión aprobada en Argos lo activa sola? Propuesta: se mantiene.
11. **Textos con números de cuenta que ya existen (§10.3).** Al migrar, ¿Iris solo avisa al admin para que los borre a mano, o además impide guardar una respuesta rápida o un manual con algo que parece un número de cuenta?

### 10.3 A migrar en Iris: cuentas bancarias propias (§7.8)

Revisión del repositorio de Iris (18 nov 2026). **Iris no tiene hoy una tabla ni una pantalla de cuentas bancarias propias:** las lee en vivo de Argos cada vez. Para cumplir §7.8 hay que cambiar esto:

| # | Dónde | Qué hay hoy | Qué cambia |
| --- | --- | --- | --- |
| 1 | `apps/api/internal/integracion/argos/conversaciones.go:145-169` | `CuentaBancariaArgos` y `CuentasBancarias`: lectura en vivo de `GET /cuentas-bancarias?activa=true&limit=100`, sin instantánea ni `sincronizado_en`. Lee `nombre` y `banco`, que no son los campos que Argos declara (§10.1, punto 14). No lee `alias`, `banco_nombre`, `titular`, `tipo` ni `activa` | Llena la instantánea; el modelo usa los campos del contrato |
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
| 12 | `apps/api/migraciones/20261106064327_mensajeria_horarios_manuales.sql:43` (`mensajeria.manuales.cuerpo`) | Texto libre de hasta 50 000 caracteres: puede tener números de cuenta escritos a mano, y los manuales «Para el cliente» viajan al asistente | Revisar los que existan en cada instalación; decisión del PM (§10.2, punto 11) |
| 13 | `apps/api/migraciones/20261106054851_mensajeria_conversaciones.sql:64` (`mensajeria.respuestas_rapidas.texto`) | Texto libre de hasta 4000 caracteres: es donde un agente guardaría «nuestras cuentas son…» | Igual que el 12; el botón «Enviar cuentas» lo reemplaza |
| 14 | `apps/api/migraciones/20261106064327_mensajeria_horarios_manuales.sql:26-29` (`mensajeria.canales.fuera_horario`, clave `mensaje`) | Texto libre de la respuesta de fuera de horario | Igual que el 12 |

No se encontraron cuentas ni números de cuenta en los ajustes de conversaciones (`mensajeria.ajustes_bandeja`), en los ajustes del asistente (`mensajeria.asistente_ajustes`, que no tiene un campo de instrucciones libres) ni en las plantillas de aviso. Los puntos 12 a 14 son campos de texto libre: el repositorio no trae números reales, pero cada instalación puede tenerlos en su base de datos.
