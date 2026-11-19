Versión: 0.8.1 (19 nov 2026) · Iris ↔ ArgosISP

# Contrato de integración Iris ↔ ArgosISP (copia de ArgosISP)

## Gobierno

- Este texto es la fuente de verdad compartida. Vive con el mismo contenido en `HCDestroyer/iris` (`.claude/integration/contratos/argosisp.md`) y en `HCDestroyer/argos-isp` (`.claude/integration/contratos/iris.md`). La línea de versión identifica la copia al día.
- Cada sistema toca solo su código. Lo que uno necesita del otro se pide en §5, y el dueño lo implementa en su repositorio.
- Cambiar el contrato requiere dos PRs enlazadas, una por repositorio. La versión sigue semver.
- Se reutilizan los mecanismos ya probados del contrato Argos ↔ Horus (`argos-isp/.claude/integration/contratos/horus.md` §1): códigos de conexión, webhooks firmados, enlace de conexión y autologin por ticket. Aquí solo se escriben las diferencias.
- **La 0.8.0 es final: acordada con Argos y decidida por el PM (18 nov 2026).** El líder de Argos revisó el borrador contra su código (`argos-isp`, rama `lider/iris-072`, commit `f857fb74`, `.claude/integration/revision-iris-080.md`; Argos 1.17.0, contrato 0.7.1) y este texto incluye sus respuestas. El PM adoptó las propuestas de §10.2. Argos lo implementó en su PR borrador `argos-isp#249` y lo publica todo junto en su versión 1.18.0; en Iris está pendiente de implementar. La copia de `argos-isp` sigue en 0.7.1 hasta que se mergeen las dos PRs enlazadas (§10.1). Lo poco que sigue abierto está en §10.1.
- **0.8.1 (19 nov 2026): cambio aditivo sobre la 0.8.0, aceptado por el líder de Argos.** Agrega `buscar_red` a la búsqueda `q` de `GET /clientes` y `GET /servicios` (§2.1, pedido A29). Argos lo publica en su versión 1.21.0. Nada más cambia respecto de la 0.8.0.

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

**Estado: acordada con Argos y decidida por el PM; implementada en Argos (PR #249, sale en 1.18.0); pendiente de implementar en Iris.** No reemplaza a §1.3 ni a §1.4: los usa. «Un paso» significa que, después de aprobar en Argos y activar en Iris, nadie copia URLs, secretos ni tokens, ni lanza cargas a mano. La 0.8.0 agrega cuatro cosas: (a) la primera sincronización automática (§2.5); (b) dos validaciones nuevas del receptor de Iris en el canje de Argos; (c) con el botón, el conector de Argos se enciende solo al confirmarse el canje; y (d) una pantalla de estado en cada panel (§2.5).

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
| Estado de cuenta resumido (en la ficha del ticket) | `GET /api/v1/clientes/:id/estado-cuenta`, solo el saldo y las facturas vencidas (requiere `clientes.estado_cuenta`) |
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
- **Uso de Iris:** manda `buscar_red=false` cuando el actor de Iris no tiene `servicios.ver_red` con alcance global (§2.4), para que una búsqueda no sirva para encontrar a un cliente por sus datos de red. Con `servicios.ver_red` global no lo manda.
- **Compatibilidad:** un Argos anterior a 1.21.0 ignora el parámetro y sigue buscando también en los datos de red. Iris lo manda solo si Argos informó `version_argos` ≥ 1.21.0 (§3.3, §4.2). Con un Argos anterior, Iris corta las búsquedas que parecen una IP sin `servicios.ver_red` global (lista vacía, sin llamar a Argos) y un `q` con un `user_ppp` todavía puede encontrar al cliente: es el límite conocido de la 0.8.0.

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
| `inventario.traslado_enviado` / `inventario.traslado_recibido` / `inventario.traslado_cancelado` (0.5.0): traslado_id, empresa_id, bodega_destino_id, tecnico (`{ id_externo, correo }` o null si el destino no es la bodega de un técnico), estado, lineas[] (`{ producto_id, cantidad, series[] }`), enviado_en; recibido: recibido_en, recibido_por, metodo_recepcion, origen (`iris` \| `panel`); cancelado: cancelado_en, motivo. Solo a conectores con contrato ≥ 0.5.0 (§3.3) | Enviado: muestra la orden por recibir en la app de campo del técnico (antes puede confirmar con `GET /inventario/traslados/:id`). Recibido o cancelado: la cierra. |
| `servicio.creado`, `servicio.config_cambiada`, `asignacion.cambiada`. Los `campos[]` de `servicio.config_cambiada` (solo nombres, nunca valores) son: `plan`, `ips`, `prefijo_v6`, `router`, `tipo_conexion`, `mac`, `amarre_ip_mac`, `empresa`, `servidor_pppoe` y `credenciales` (cambió `user_ppp` o la clave PPP; la clave nunca viaja). Desde 0.8.0 también `precio`: cambió `costo_override`, o empezó o terminó un plan temporal con otro precio (hoy ese cambio no emite nada). `asignacion.cambiada` trae `ipv4[]`, una lista | Refresca la instantánea del servicio releyendo `GET /servicios/:id`. |
| `servicio.estado_cambiado` | Refresca el estado. Con `a = retirado`, propone una orden de retiro si el servicio tiene equipos en comodato. |
| `inventario.equipo_por_recuperar` | Crea una orden de retiro (`origen = argos`), si no hay una abierta para esa unidad. |
| `servicio.caidas_repetidas` (pedido A6): servicio_id, empresa_id, cliente_id, router_id, sesion_id, sesiones, desde, hasta, umbral, ventana_minutos, causas. Por defecto 5 sesiones terminadas en 120 min, a lo sumo una vez por servicio y ventana; no cuentan los cortes que manda Argos ni los reinicios del NAS | Sugiere un ticket proactivo. |
| `servicio.eliminado` (0.3.1): servicio_id, empresa_id, cliente_id, router_id. Solo servicios que nunca se usaron (sin facturas, pagos, sesiones ni otro historial); en Argos la fila ya no existe. Llega junto con un `asignacion.cambiada` de ips vacías. Solo si `empresa_id` está en el alcance del usuario técnico | Quita la instantánea del servicio y refresca la lista de servicios del cliente (`cliente_id`). Órdenes y tickets que lo referencian conservan su copia y lo muestran «eliminado en Argos». Un evento atrasado de ese servicio no recrea la instantánea; `GET /servicios/:id` responde 404 (esperado, no se reintenta; ver «Confirmar un borrado» abajo). |
| `router.eliminado` (0.3.1): router_id, empresa_id (empresa sugerida del router, o null = router compartido, que llega a cualquier alcance; con empresa, solo si está en el alcance). Solo routers sin historial | Quita el router de factibilidad (router o nodo sugerido); una factibilidad ya evaluada conserva el id. `GET /routers/:id` responde 404 (ver «Confirmar un borrado» abajo). |
| `comprobante.aprobado` / `comprobante.rechazado` (0.6.0): forma en §7.2. Solo a conectores con contrato ≥ 0.6.0 | Resuelve la boleta en la conversación y avisa al cliente (§6.3). |
| `promesa.creada` / `promesa.cumplida` / `promesa.incumplida` / `promesa.cancelada` (0.6.0): forma en §7.4. Solo a conectores con contrato ≥ 0.6.0. Desde 0.8.0 el payload suma `motivo_reconexion`, sin filtro de versión | Actualiza la promesa o la reconexión temporal de la conversación y explica al cliente. |
| `cuenta_bancaria.cambiada` (0.8.0; hoy Argos no emite ningún evento de cuentas): cuenta_bancaria_id, empresa_id, campos[] (solo nombres, nunca valores: `banco_id`, `numero`, `alias`, `titular`, `tipo`, `moneda`, `activa`), activa (el valor **después** del cambio), creada (boolean). Se emite al editar y **también al crear** (`creada: true`, con todos los campos guardados en `campos[]`). `empresa_id` no se edita. Solo a conectores con contrato ≥ 0.8.0, con el alcance de la empresa de la cuenta | Relee `GET /cuentas-bancarias/:id` y refresca la instantánea de la cuenta (§7.8). Una cuenta con `activa: false` deja de ofrecerse; nunca se borra. |

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
- **Etiqueta corta:** `Servicio <n> · <plan> · <zona>`. `<n>` es el orden del servicio dentro del cliente (1, 2, 3…, por id) y `<zona>` es el nombre de la zona del catálogo. No lleva calle, número de casa, coordenadas ni códigos internos. Si dos servicios quedan con la misma etiqueta, el asistente no adivina: pasa a una persona.
- Los ids de servicio siguen sin ir en los argumentos de las herramientas: los pone el servidor (§17.9).

**Cómo se distinguen los servicios de un mismo cliente en el panel.** Cada servicio va en una línea: código del servicio, plan, `precio_mensual` y estado (con icono, texto y color). La dirección corta se ve solo con `servicios.ver_ubicacion`, y la IP y el router solo con `servicios.ver_red`. Sin esos permisos se ve la misma etiqueta corta que usa el asistente. Vale para la ficha del cliente, el selector de servicio de un ticket, una orden o una boleta, y el panel lateral de la conversación.

### 2.5 Primera sincronización y estado de la conexión (0.8.0)

**Estado: acordada con Argos y decidida por el PM; implementada en Argos (PR #249, sale en 1.18.0); pendiente de implementar en Iris.** Hoy Iris carga los catálogos (zonas, planes, routers) con un job y trae clientes y servicios a demanda, al abrir cada ficha. Con la 0.8.0, al **activar en Iris** un conector recién canjeado (§1.5), Iris hace sola una primera sincronización completa. Antes va siempre la primera llamada autenticada (`PUT /integraciones/iris/contrato`), que confirma el canje. Argos no necesita enterarse antes: se entera por el primer `PUT …/sincronizacion` con `en_curso` (abajo).

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
- **0.8.0:** qué hace cada lado cuando el otro no la habla está en §4.1. No hay mecanismos nuevos: se usan `contrato`, `version_argos` y `PUT /integraciones/iris/contrato`.
- **0.8.1:** un Argos anterior a 1.21.0 no conoce `buscar_red` y lo ignora. Iris consulta `version_argos` antes de mandarlo (§2.1, §4.2).

## 4. Versionado

Semver. Esta versión 0.8.1 es un cambio aditivo sobre la 0.8.0, que es final (ver «Gobierno»); hasta que se mergeen las dos PRs enlazadas, la última versión con espejo es la 0.7.1. Desde 1.0.0, solo cambios compatibles en versiones menores.

- **0.8.1** (19 nov 2026; aceptada por el líder de Argos): A29, búsqueda sin datos de red. §2.1: `buscar_red` opcional (booleano: `true`, `false`, `1` o `0`; por defecto `true`) en `GET /clientes?q=` y `GET /servicios?q=`; con `false`, `q` deja de coincidir por la IPv4 vigente, una IPv6 dentro de un prefijo vigente y el `user_ppp` de los servicios (clientes: nombre, código, código externo, nombre fiscal, email, NIT/CUI y teléfono o móvil; servicios: solo el nombre del cliente); cualquier otro valor responde `422 validacion` (`field: buscar_red`). Iris lo manda en `false` cuando el actor no tiene `servicios.ver_red` global, y solo con Argos ≥ 1.21.0 (§3.3, §4.2). Es compatible: el valor por defecto es el comportamiento de hoy y un Argos anterior ignora el parámetro. Requiere Argos ≥ 1.21.0.
- **0.8.0** (18 nov 2026, **final**; revisada por Argos contra su código, `argos-isp` `f857fb74`; decisiones del PM del 18 nov en §10.2; implementada en Argos en la PR borrador `argos-isp#249`; pendiente de implementar en Iris y de las dos PRs enlazadas): cuatro bloques, todos aditivos sobre 0.7.1, en **una sola versión** que Argos publica como 1.18.0 (decisión del PM; no hay 0.7.2). **A, reconexión temporal sin personal de caja** (lado Argos hecho primero en su rama `lider/iris-072`, hoy en la PR #249): §7.3: `motivo` acepta `fuera_de_horario` | `sin_personal` (otro valor, `422 validacion` con `field = "motivo"` y `rule = "enum"`); `reglas_promesa.reconexion_temporal.vigencia` = `horas` (por defecto) | `medianoche` (las 00:00 del día siguiente en Guatemala), con el vencimiento real siempre en `hasta` (RFC 3339 en UTC con milisegundos), que es el fin de la promesa y no la hora del corte; un solo cupo para los dos motivos; el criterio de `sin_personal` es de Iris y queda auditado en Argos. §7.4 y §2.3: `motivo_reconexion` en la `Promesa` y en los webhooks `promesa.*`, y filtro `motivo_reconexion` en `GET /promesas`. **B, cuentas bancarias de Argos como única fuente:** §7.8 (nueva): `titular` y `tipo` en la cuenta (los dos pueden ser `null`), webhook `cuenta_bancaria.cambiada` (también al crear), `rule: "inactiva"` al registrar un comprobante en una cuenta desactivada, instantánea de lectura en Iris y fin de las cuentas escritas a mano (lista «a migrar» en §10.3). **C, más datos del servicio:** §2.4 (nueva): `precio_mensual` (con IVA, sin adicionales) y `moneda`; `precio` en `campos[]` de `servicio.config_cambiada` y de `catalogo.cambiado` de plan; lista blanca de campos en la instantánea de Iris, los permisos `servicios.ver_red` y `servicios.ver_ubicacion` en Iris y qué recibe el asistente de IA. `router_nombre`, `plan_nombre` y `zona_nombre` ya existían. **D, conexión de un paso:** §1.5 (nueva): los requisitos de seguridad de Argos (S1–S8), el cuerpo del canje por escrito, dos validaciones nuevas del receptor de Iris en el canje de Argos (`422 webhook_invalido` con `rule` `mismo_host` u `origen_aprobado`, y el código queda gastado), el conector de Argos encendido al confirmarse un canje del botón, y el certificado TLS solo como «registrar y avisar»; §2.5 (nueva): primera sincronización automática, con progreso y reanudable, cabeceras de Iris opcionales en `GET /cuentas-bancarias` y `GET /bancos`, la ruta nueva `PUT /integraciones/iris/sincronizacion` (`409 sincronizacion_vieja`) y pantalla de estado en los dos paneles. §4.1 (nueva): compatibilidad y errores. §5: pedidos A25–A28 y B4–B7. `excluir_retirados=true` en `GET /clientes` y `GET /servicios` para la primera carga (§2.5). §10 (nueva): las decisiones del PM, lo que sigue abierto y la migración de cuentas en Iris. Es compatible: no cambia ningún campo ni ruta de 0.7.1; un Argos 1.17.x sin estos cambios sigue funcionando con Iris, y al revés (§4.1). Requiere Argos ≥ 1.18.0.
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
| `buscar_red=false` en `GET /clientes?q=` y `GET /servicios?q=` (§2.1) | Iris no lo manda. Corta las búsquedas que parecen una IP sin `servicios.ver_red` global; un `q` con un `user_ppp` todavía encuentra al cliente (límite conocido). Si llegara a mandarlo, ese Argos lo ignora y busca como hoy | Iris no lo manda: Argos busca como hoy (`buscar_red=true`) |

| Dónde | Status | Código o regla | Cuándo |
| --- | --- | --- | --- |
| `GET /clientes` y `GET /servicios` (§2.1) | 422 | `validacion` (`urn:argosisp:problema:validacion`), con `errors[0].field = "buscar_red"` (nuevo) | `buscar_red` con un valor que no es `true`, `false`, `1` ni `0`. Argos anterior a 1.21.0 ignora el parámetro |

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
| A25 | **Reconexión temporal sin personal de caja** (0.8.0, §7.3): `motivo: "sin_personal"` en `POST /comprobantes/:id/reconexion-temporal`, `vigencia: "horas" \| "medianoche"` en la plantilla y `motivo_reconexion` en la promesa y en los webhooks `promesa.*`. Además: el filtro `motivo_reconexion` en `GET /promesas` y en la lista de promesas del panel, y corregir la descripción de `hasta` en el OpenAPI («según `vigencia`»). | implementado en Argos (PR #249, sale en 1.18.0) |
| A26 | **Cuentas bancarias como única fuente** (0.8.0, §7.8): `titular` (string o `null`, 2–120) y `tipo` (`monetaria` \| `ahorro` o `null`) en la cuenta; el webhook `cuenta_bancaria.cambiada` `{ cuenta_bancaria_id, empresa_id, campos[], activa, creada }`, que se emite **también al crear** (`creada: true`), solo a conectores con contrato ≥ 0.8.0; y `rule: "inactiva"` (hoy `exists`) al registrar un comprobante en una cuenta desactivada. La lista sigue siendo un arreglo sin paginar. Las desactivadas vienen con `activa: false` y nunca se borran (ya es así). | implementado en Argos (PR #249, sale en 1.18.0) |
| A27 | **Más datos del servicio** (0.8.0, §2.4): `precio_mensual` y `moneda` en `GET /servicios/:id` y en la lista; `precio` en `campos[]` de `servicio.config_cambiada` y de `catalogo.cambiado` de plan. `router_nombre` no se pide: ya existe. Caja o nodo: no; sigue siendo `planta`, con OZmap como fuente. Además, `excluir_retirados=true` en `GET /clientes` y `GET /servicios` (§2.5). | implementado en Argos (PR #249, sale en 1.18.0) |
| A28 | **Conexión de un paso, lado Argos** (0.8.0, §1.5 y §2.5). El receptor de Iris ya se guarda en el canje; lo que falta es: (1) validar en el canje el host del receptor y el origen aprobado (`422 webhook_invalido`, `mismo_host` \| `origen_aprobado`, con el código gastado); (2) encender el conector al confirmarse un canje del botón; (3) `PUT /integraciones/iris/sincronizacion` y `sincronizacion` en `GET /integraciones/iris`; (4) las cabeceras de Iris opcionales en `GET /cuentas-bancarias` y `GET /bancos`; (5) la pantalla de estado en el panel de Argos y el texto «Iris copiará los clientes, los servicios y las cuentas bancarias de estas empresas» en la pantalla de aprobación y en la del enlace; (6) rotular la generación manual de tokens como «vía de recuperación» (S5). **Sin huella TLS:** Argos no la muestra. Con los requisitos S1–S8. | implementado en Argos (PR #249, sale en 1.18.0) |
| A29 | **Búsqueda sin datos de red** (0.8.1, §2.1): `buscar_red` opcional (por defecto `true`) en `GET /clientes?q=` y `GET /servicios?q=`; con `false`, `q` no coincide por la IPv4 vigente, una IPv6 dentro de un prefijo vigente ni el `user_ppp` de los servicios (§2.1 dice qué sigue coincidiendo en cada lista); un valor que no sea `true`, `false`, `1` ni `0` responde `422 validacion` (`field: buscar_red`). | implementado en Argos, sale en 1.21.0 |

### 5.2 ArgosISP solicita a Iris

| # | Pedido | Estado |
| --- | --- | --- |
| B1 | **Servicio de mensajería** (0.6.0, §6): API de avisos, plantillas, permisos de contacto y webhooks de entrega. Lo ofrece Iris aunque Argos no lo pidió; Argos lo adopta como canal WhatsApp (A21) y pide además las plantillas `factura_vencida` y `servicio_reactivado`. | en curso en Iris |
| B2 | **Autologin a una conversación** (0.6.0, §7.2): destino `conversacion` en `POST <iris>/api/v1/integraciones/argosisp/sso/tickets`, con el id de `X-Iris-Conversacion`, para el botón «Ver conversación en Iris» de la cola de caja. | pendiente en Iris (cuando exista la pantalla de conversaciones) |
| B3 | **Aprobar y rechazar desde la bandeja** (0.7.0, §7.7): emitir el ticket de autologin Iris → Argos (§1.3) con el botón «Vincular mi usuario de Argos»; los botones «Aprobar y registrar pago» y «Rechazar» en la boleta de la conversación, solo para una persona y nunca para el asistente de IA; y el manejo de los cinco `403` (`actor_sin_vinculo`, `actor_inactivo`, `actor_sin_permiso`, `actor_sin_empresa`, `actor_ia_no_permitido`). | listo en Iris |
| B4 | **Criterio de `sin_personal`** (0.8.0, §7.3): Iris la pide solo en horario hábil y sin nadie de caja disponible, apagada de fábrica, con verificación fuerte y solo con contrato ≥ 0.8.0; le dice al cliente hasta cuándo vale la reconexión con el `hasta` de Argos y **nunca dice «se corta a medianoche»**. Argos no objeta el criterio. | acordado con Argos y decidido por el PM (§10.2, puntos 1, 2 y 12); pendiente de implementar en Iris |
| B5 | **Iris sin cuentas bancarias propias** (0.8.0, §7.8): instantánea de lectura, solo cuentas activas de la empresa de la conversación y migración de §10.3. Iris lee un **arreglo**, usa `alias` y `banco_nombre`, y maneja `rule: "inactiva"`. | pendiente en Iris |
| B6 | **Permisos para los datos sensibles del servicio** (0.8.0, §2.4): `servicios.ver_red` y `servicios.ver_ubicacion` en Iris; el asistente de IA sin IP, `user_ppp`, coordenadas ni dirección; y la **lista blanca** de campos en la instantánea. Argos está de acuerdo. | acordado con Argos y decidido por el PM (§10.2, puntos 4 a 8); pendiente de implementar en Iris |
| B7 | **Conexión de un paso, lado Iris** (0.8.0, §1.5 y §2.5): primera sincronización automática, con progreso y reanudable, al ritmo de §2.5; informar el avance con `PUT /integraciones/iris/sincronizacion`; pantalla de estado en el panel de Iris; y **registrar y avisar** (sin bloquear) si cambia el certificado TLS de la API de Argos. | pendiente en Iris |

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
| `reconexion_temporal` | `hasta` | Iris, al recibir el 201 de §7.3. Desde 0.8.0, Iris escribe en `hasta` hasta cuándo vale la reconexión, en hora de Guatemala y tomado del `hasta` de Argos (§7.3, «Qué le dice el asistente al cliente») | Argos no avisa por su cuenta (§7.3) |
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
| Actor | Cada llamada de §7 lleva `X-Iris-Actor` = `asistente_ia` \| `usuario:<id en Iris>` y `X-Iris-Conversacion` = id de la conversación. La forma exacta (0.7.1) es `^(asistente_ia\|usuario:[A-Za-z0-9_-]{1,64})$` para `X-Iris-Actor` y `^[A-Za-z0-9._:-]{1,128}$` para `X-Iris-Conversacion`. Fuera de formato, o si faltan en una ruta de §7: `400 cabecera_iris_requerida`. Argos audita con actor `token` del usuario técnico Iris y guarda las dos cabeceras en la fila de auditoría, donde se pueden buscar. El canal de la auditoría es `ia` con `asistente_ia` y `api` con `usuario:<id>`. Excepción (0.7.0): en aprobar y rechazar (§7.7) el actor de la auditoría es el usuario de Argos vinculado a `usuario:<id>`, y el token de Iris queda como «vía». |
| Empresa | Cada documento es de una empresa: la del servicio (promesas) o la de la cuenta bancaria (comprobantes). Si Iris manda `empresa_id` y no coincide: `422 empresa_no_coincide`. Fuera del alcance del usuario técnico Iris: `403 empresa_no_permitida`. |
| Idempotencia | Todas las escrituras de §7 llevan `Idempotency-Key` (obligatoria: `400 idempotency_key_requerida`) con las reglas de §3.1. En §7.7 la huella incluye además `X-Iris-Actor`. |

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
- **Evento.** `reconexion.solicitada` suma el motivo `sin_personal` a su campo `motivo` (hoy solo `fuera_de_horario`). Está en `eventos.md`, pendiente de implementar.
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

## 10. Decisiones y cierre de la 0.8.0

El líder de Argos respondió los 32 puntos del borrador contra su código (ver «Gobierno») y sus respuestas están en el cuerpo del contrato, cada una en su sección. El PM adoptó las propuestas el 18 nov 2026 (§10.2). Argos informó que implementó todo lo suyo en su PR borrador `argos-isp#249`, que sale en la 1.18.0. Aquí queda solo lo que sigue abierto (§10.1), las decisiones del PM (§10.2) y la migración de Iris (§10.3).

### 10.1 Lo que sigue abierto con Argos

Argos informó que la PR #249 implementa: la reconexión sin personal con su vigencia; las cuentas bancarias con `titular`, `tipo`, el webhook `cuenta_bancaria.cambiada`, las cabeceras de Iris opcionales y `rule: "inactiva"`; `precio_mensual`, `moneda` y `excluir_retirados=true`; la conexión de un paso con `422 webhook_invalido`; el encendido del conector al confirmar el canje y `PUT /integraciones/iris/sincronizacion`. Con eso se cierra lo que §10.1 tenía sobre `excluir_retirados=true` (el PM eligió «solo activos», §10.2, punto 9). Sigue abierto:

1. **S5 y la vía manual (§1.5).** Argos recomendó la opción (a): S5 vale «en la conexión de un paso» y la vía manual del panel de Argos (un `atk_` que se muestra una vez y pegar un `iris_pat_`) queda como recuperación, solo `admin`, auditada y rotulada así. El texto ya la escribe así. Falta que el líder de Iris lo confirme y decida lo mismo para la configuración manual del conector del lado de Iris.
2. **Revisar la PR #249 contra el contrato.** El aviso de Argos no nombra por separado: el filtro `motivo_reconexion` en `GET /promesas` y en el panel, la descripción de `hasta` en el OpenAPI (A25), la pantalla de estado y el texto «Iris copiará…» en el panel de Argos, el rótulo «vía de recuperación» (A28, puntos 5 y 6), ni la frase vieja de su `eventos.md` que dice que `motivo_reconexion` no se reenvía a Iris. Se comprueba al revisar la PR.
3. **El espejo.** Faltan las dos PRs enlazadas (§12 de `CLAUDE.md`): la de Iris con este texto y la de Argos, que copia el contrato y sube su `contrato` a `0.8.0` en la 1.18.0.

Todo lo demás que el contrato marca como «0.8.0» está implementado en Argos (A25–A28, PR #249) y pendiente de implementar en Iris (B4–B7).

### 10.2 Decidido por el PM (18 nov 2026)

El PM respondió «realiza las propuestas»: lo que estaba propuesto queda adoptado. Ya estaba decidido que la 0.8.0 sale en una sola versión con los cuatro bloques (no hay 0.7.2). Donde no había una propuesta clara, queda un **valor inicial del líder de Iris, que el PM puede cambiar sin tocar el contrato**.

1. **Criterio de `sin_personal` (§7.3).** Iris la pide solo dentro del horario hábil de la empresa y si ningún usuario con rol `caja` de esa empresa está disponible. Valores iniciales del líder de Iris: sin espera de minutos, y si la empresa no tiene a nadie con rol `caja`, no se pide nunca. Siempre con verificación fuerte y apagado de fábrica (punto 2). Argos no objeta el criterio y lo acepta con sus barreras de siempre.
2. **Quién enciende `sin_personal`.** Apagado de fábrica en Iris, con un interruptor aparte del de la reconexión fuera de horario. Lo enciende un admin por empresa.
3. **Vigencia (§7.3).** Valor inicial del líder de Iris: cada empresa la elige en la plantilla de Argos; por defecto, `horas`.
4. **Quién ve la IP y los datos de red (§2.4).** `admin`, `supervisor`, `soporte` y `tecnico` (este solo en sus órdenes). No los ven `despacho`, `ventas`, `caja` ni `lectura`.
5. **Quién ve la dirección y la ubicación (§2.4).** `admin`, `supervisor`, `despacho`, `soporte`, `ventas` y `tecnico` (solo en sus órdenes). `caja` y `lectura` dejan de verlas.
6. **El asistente y las cuentas bancarias (§7.8).** Basta el número registrado: son las cuentas de la empresa, no un dato del cliente. A un número desconocido se las da una persona.
7. **El asistente y el precio (§2.4).** Puede decir el precio mensual con número registrado, sin verificación fuerte. Dice «su plan cuesta Q250.00 al mes», nunca el total de la factura.
8. **Cómo distingue el asistente los servicios (§2.4).** «Servicio 1 · plan · zona», sin dirección.
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
