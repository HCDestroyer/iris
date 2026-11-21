Versión: 0.10.0 (21 nov 2026) · Iris ↔ ArgosISP

# Contrato de integración Iris ↔ ArgosISP (copia de ArgosISP)

## Gobierno

- Este texto es la fuente de verdad compartida. Vive con el mismo contenido en `HCDestroyer/iris` (`.claude/integration/contratos/argosisp.md`) y en `HCDestroyer/argos-isp` (`.claude/integration/contratos/iris.md`). La línea de versión identifica la copia al día.
- Cada sistema toca solo su código. Lo que uno necesita del otro se pide en §5, y el dueño lo implementa en su repositorio.
- Cambiar el contrato requiere dos PRs enlazadas, una por repositorio. La versión sigue semver.
- Se reutilizan los mecanismos ya probados del contrato Argos ↔ Horus (`argos-isp/.claude/integration/contratos/horus.md` §1): códigos de conexión, webhooks firmados, enlace de conexión y autologin por ticket. Aquí solo se escriben las diferencias.
- **La 0.8.0 es final: acordada con Argos y decidida por el PM (18 nov 2026).** El líder de Argos revisó el borrador contra su código (`argos-isp`, rama `lider/iris-072`, commit `f857fb74`, `.claude/integration/revision-iris-080.md`; Argos 1.17.0, contrato 0.7.1) y este texto incluye sus respuestas. El PM adoptó las propuestas de §10.2. Argos lo implementó en `argos-isp#249` y lo publicó todo junto en su versión 1.18.0; en Iris está implementada (Iris 0.33.0, que informa contrato 0.8.1). La 0.8.0 ya está espejada en `argos-isp` (§10.1). Lo poco que sigue abierto está en §10.1.
- **0.8.1 (19 nov 2026): cambio aditivo sobre la 0.8.0, aceptado por el líder de Argos.** Agrega `buscar_red` a la búsqueda `q` de `GET /clientes` y `GET /servicios` (§2.1, pedido A29). Argos lo publica en su versión 1.21.0. Espejo: PRs enlazadas `iris#222` y `argos-isp#258`. Nada más cambia respecto de la 0.8.0.
- **0.9.0 (21 nov 2026): aditiva sobre la 0.8.1; acordada con Argos y decidida por el PM (21 nov 2026); pendiente de implementar en Argos e Iris.** Pedido del PM por el líder de Argos; propuesta de Iris, revisada por Argos contra su código (1.23.0) y aprobada sin objeciones, con los ajustes que este texto ya incluye. Dos bloques que pueden publicarse por separado, cada uno con su propia marca en `capacidades` (§4.3): **P**, el portal del cliente de Argos con tickets y solicitudes de servicio de Iris (§11); **E**, equipos del cliente y activación desde la orden de Iris, con Argos como único dueño de SmartOLT y del ACS (§12). Decisiones del PM (detalle en §10.4): una solicitud de servicio la atiende ventas en Iris (prospecto) y la convierte en orden; un cliente identificado solo por su IP abre tickets y ve la lista, nada más; el cambio de Wi-Fi que hace el cliente desde su portal es solo de Argos (con reautenticación) y queda fuera del contrato; Iris nunca guarda credenciales de SmartOLT ni del ACS; la activación la pide el técnico desde la orden o un agente desde el panel de Iris, para FTTH y wireless, y cada empresa elige si se puede cerrar la orden sin ella; el mensual empieza con el servicio activo y la instalación se cobra como factura única que registra un agente o un formulario de la orden; el asistente de IA puede consultar el equipo y cambiar el Wi-Fi solo si la empresa lo habilita, con permisos propios y el cliente verificado.
- **0.10.0 (21 nov 2026): aditiva sobre la 0.9.0. Propuesta de Argos con decisiones del PM (21 nov 2026, §10.5) y ajustes de Iris marcados en §10.5; pendiente de que Argos acepte esos ajustes y de implementar en los dos lados.** De la venta a la instalación (§3.4): servicio pendiente sin IP (A38), cambio de titular (A39) y de plan (A40) antes de activar, identidad del titular registrada en Argos (A41), datos del titular y del lugar completados en la instalación (A43); y contactos del cliente con su rol y sus permisos (A42, §3.5).

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
- **0.9.0, cobro de la instalación (§3, A36):** sin permisos nuevos. El cargo único sigue con `facturas.emitir_cargo_unico` (arriba), más la lectura `GET /conceptos-cargo` del catálogo de conceptos. El token de Iris **no** tiene `facturas.cargo_sin_costo`: Iris nunca pide cargos en Q0.
- **0.10.0 (§3.4):** `servicios.reservar_ip`, `servicios.cambiar_titular`, `servicios.cambiar_plan_pendiente`, `clientes.registrar_identidad` y `servicios.completar_datos`. Son permisos nuevos (Argos migra los tokens que ya existen) y solo valen sobre servicios `pendiente_instalacion` con una orden de Iris en curso. El token de Iris **no** recibe un permiso general para editar clientes ni `clientes.ver_identidad`: sube la identidad y no la puede leer.
- **0.10.0, contactos (§3.5, A42):** `contactos.buscar` y `contactos.gestionar` (nuevos, con migración de tokens). Con `contactos.gestionar` el token de Iris nunca crea ni edita un titular (§3.5).

El usuario técnico Iris solo tiene alcance por empresas (o global), nunca por zona ni router: una lista completa y vacía significa sin servicios. Argos lo hace cumplir: un token, un rol o un correo que le darían alcance por zona o router responde `422 usuario_tecnico_solo_empresas`, y el canje nunca emite un token así.

### 1.2 Argos → Iris (Argos lee de Iris)

Token `iris_pat_<32B base62>` con `tickets.ver`, `tickets.crear`, `ordenes.ver`, `sso.emitir` y `portal.emitir`. Argos lo usa para mostrar los tickets y las órdenes de un cliente en la ficha de su panel (A7: `GET /api/v1/tickets?clienteId=`; desde 0.9.0 Iris acepta también `cliente_id` como alias) y para el acceso al portal de soporte de Iris (§11.9).

**0.6.0:** el mismo token suma `mensajeria.enviar` y `mensajeria.ver` para usar el servicio de mensajería de §6. Son permisos de la integración `integracion_argos`; un usuario de Iris no los recibe por rol.

**0.9.0:** el portal del cliente de Argos (§11) usa un **token aparte**, `token_portal` (otro `iris_pat_` del mismo usuario de integración, recortado **solo** a `portal_argos.ver_tickets`, `portal_argos.crear_ticket`, `portal_argos.responder_ticket`, `portal_argos.confirmar_solucion`, `portal_argos.solicitar_servicio` y `portal_argos.ver_solicitudes`). Lo usa únicamente el servidor del portal de Argos; el token de arriba queda en el panel y no tiene `portal_argos.*`. Iris lo entrega en el cuerpo del canje (§1.5, campo `token_portal`) y, para un conector ya conectado, en `PUT /integraciones/iris/contrato` cuando informa contrato ≥ 0.9.0; se rota con cada canje y Argos lo guarda cifrado y de solo escritura (S5, S6). Son permisos del rol `integracion_argos`: ningún rol de persona de Iris los recibe, y aunque un admin los tenga, las rutas de §11 exigen el `token_portal` de un conector Argos (`403 solo_integracion_argos`).

**0.10.0:** sin cambios en el token de Argos en Iris.

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
| `instalacion.aprovisionada` / `instalacion.fallida` (0.9.0, §12.2): aprovisionamiento_id, servicio_id, empresa_id, cliente_id, origen_tipo (`iris_orden`), origen_id, idempotency_key; aprovisionada: servicio_estado, equipo `{ tipo, serie, mac, unidad_id }`, senal `{ rx_dbm, tx_dbm }` (o null), activado_en; fallida: causa (§12.2), reintentable, detalle (texto sin secretos). Nunca la clave Wi-Fi ni credenciales. Solo a conectores con contrato ≥ 0.9.0 | Actualiza la activación de la orden; con `aprovisionada`, deja la orden lista para cerrar. |
| `factura.emitida` / `factura.anulada` / `pago.registrado` / `pago.anulado` (0.9.0, A36): solo de facturas con `origen_tipo = iris_orden` y a conectores con contrato ≥ 0.9.0. Factura: factura_id, numero, empresa_id, cliente_id, servicio_id, origen_tipo, origen_id, estado, total, saldo, moneda. Pago: pago_id, factura_id, origen_tipo, origen_id, empresa_id, monto, fecha. Son eventos que Argos ya tiene (§6.3); hoy solo envía a Iris `comprobante.*` y `promesa.*` | Actualiza el cobro de la orden: «facturado», «pagado», «anulado» o, con `pago.anulado`, vuelve a «facturado». |
| `servicio.bienvenida_cambiada` (0.9.0, §12.5): servicio_id, empresa_id, cliente_id, origen_tipo (`iris_orden`, o null si la activación fue desde el panel de Argos), origen_id (o null), canales[] (`{ canal: whatsapp \| correo \| telegram, estado: pendiente \| enviada \| fallida \| omitida, motivo (o null), aviso_id (o null) }`; en WhatsApp, Argos reenvía lo que Iris le informó con `aviso.entregado` / `aviso.fallido`, §6.6, y `aviso_id` es el de Iris), reenvio (bool). Uno por cambio de estado, deduplicado por `X-Webhook-Id`. Nunca el texto ni las cuentas. Solo a conectores con contrato ≥ 0.9.0 | Actualiza el bloque «Bienvenida» de la orden. |
| `equipo.wifi_resuelto` (0.9.0, §12.4): solicitud_id, servicio_id, empresa_id, origen_tipo (`iris_orden` \| `iris_ticket` \| `iris_conversacion`), origen_id, idempotency_key, estado (`aplicado` \| `fallido`), causa (o null). Nunca el SSID ni la clave | Muestra el resultado en la orden o el ticket. |
| `servicio.titular_cambiado` (0.10.0, §3.4.4): servicio_id, empresa_id, de_cliente_id, a_cliente_id, existente (bool), posible_duplicado (bool), de_cliente_eliminado (bool), motivo, origen_tipo (`iris_orden` o null), origen_id (o null). Nunca el documento ni la identidad. Solo a conectores con contrato ≥ 0.10.0 | Mueve el servicio y la orden al titular en la instantánea; si `de_cliente_eliminado`, quita el cliente anterior y pasa el prospecto al nuevo. |
| `cliente.identidad_registrada` (0.10.0, §3.4.3): cliente_id, empresa_id, documento_tipo, metodo. Nunca el número ni la foto. Solo a conectores con contrato ≥ 0.10.0 | Marca la identidad del titular como registrada en la orden y borra la foto del depósito de tránsito. |
| `cliente.contactos_cambiados` (0.10.0, §3.5): cliente_id, empresa_id, contacto_id, accion (`creado` \| `editado` \| `quitado`), campos[] (solo nombres: `nombre`, `relacion`, `servicios`, `permisos`, `telefonos`). Nunca los números. Solo a conectores con contrato ≥ 0.10.0 | Refresca la caché de contactos de ese cliente (relee `GET /clientes/:id/contactos`). |
| `servicio.config_cambiada` y `catalogo.cambiado` (0.10.0, cambios sin forma nueva): `servicio.config_cambiada` lleva además `origen_tipo`/`origen_id` cuando el cambio de plan vino de Iris (§3.4.5); `catalogo.cambiado` de plan suma `modo` y `routers` en `campos[]` | Refresca la instantánea; con `origen_tipo = iris_orden`, no avisa de nuevo al vendedor. |

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
| Visita o instalación cobrada | `POST /facturas/cargo-unico` (ya existe). Desde 0.9.0, con el cuerpo real de Argos (fila siguiente): una línea con el `concepto_id` y el `precio_unitario` que fija la plantilla de Iris | `iris:cargo:<orden_id>` |
| Técnico o agente pulsa «Activar servicio» en una instalación (0.9.0, §12) | `POST /servicios/:id/aprovisionar` | `iris:aprovisionar:<orden_id>:<intento>` |
| Técnico confirma que firma la misma persona de la venta (0.10.0, §3.4.3) | `POST /clientes/:id/identidad` (multipart) | `iris:identidad:<orden_id>:<version>` |
| Técnico cambia el titular antes de activar (0.10.0, §3.4.4) | `POST /servicios/:id/cambiar-titular` (multipart, con la identidad) | `iris:titular:<orden_id>:<cambio_id>` |
| Técnico completa los datos del titular o del lugar (0.10.0, §3.4.6) | `POST /servicios/:id/completar-datos` | `iris:datos:<orden_id>:<n>` |
| Técnico cambia el plan antes de activar (0.10.0, §3.4.5) | `POST /servicios/:id/cambiar-plan` | `iris:plan:<orden_id>:<cambio_id>` |
| Técnico reserva la IP en sitio (0.10.0, §3.4.1) | `POST /servicios/:id/reservar-ip` | `iris:reservar_ip:<orden_id>` |
| Cobro de la instalación registrado por un agente o por el formulario de la orden (0.9.0, A36) | `POST /facturas/cargo-unico` con el cuerpo real de Argos: `{ servicio_id, cliente_id?, vence_en?, lineas: [{ concepto_id, precio_unitario, descripcion?, cantidad?, descuentos? }], origen_tipo: "iris_orden", origen_id }`, con `X-Iris-Actor` (§7.1: quien lo registró). No hay `concepto_id`, `monto` ni `descripcion` en la raíz. `concepto_id` es de `GET /conceptos-cargo` (por defecto «Instalación», sembrado por empresa); los conceptos **no tienen precio**: `precio_unitario` (string decimal, bruto con IVA, **> 0**, obligatorio) lo pone Iris, de su plantilla o del formulario de la orden. **Iris nunca manda un cargo en Q0** (exigiría `motivo_sin_costo_id`, `motivo_sin_costo_texto` y `facturas.cargo_sin_costo`, que su token no tiene). Responde `201 CargoUnicoEmitido` sin cambios: Iris lee `factura.id`, `numero`, `estado`, `total` y `moneda` (`documento = bonificacion` es una respuesta posible de Argos, pero Iris nunca la pide). Sale en el momento (no espera al cierre), por la cola de pendientes si Argos falla. El precio que trae la plantilla o el formulario se manda tal cual; cambiarlo exige en Iris el permiso aparte `ordenes.cobro_editar_monto`, con motivo auditado | `iris:cargo:<orden_id>:<cargo_id>` |

**`tecnico` (0.5.0):** ver §3.3.

**0.9.0:** el mensual empieza cuando el servicio queda activo; la instalación se cobra solo como factura única (fila anterior). Cada cobro de la orden tiene su `cargo_id` de Iris y su clave `iris:cargo:<orden_id>:<cargo_id>`; si la instalación ya se cobró así, el cierre **no** manda el cargo de instalación de la plantilla (`iris:cargo:<orden_id>`). Después de un `factura.anulada`, un cobro nuevo lleva **otro** `cargo_id` (nunca se reusa la clave). Si la orden tiene una activación `lista` o pendiente para un agente (§12), el cierre **no** manda `activar-instalacion` ni el `POST /servicios/:id/equipos` del equipo aprovisionado: Argos ya activó el servicio y asignó ese equipo en comodato. Un `409 servicio_no_pendiente` de una orden con activación `lista` es conciliado, no error.

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
- **0.8.1:** un Argos anterior a 1.21.0 no conoce `buscar_red` y lo ignora. Iris lo manda igual (§2.1, §4.2) y usa `version_argos` solo para registrar si tuvo efecto.

### 3.4 De la venta a la instalación (0.10.0)

El cliente no siempre da todos sus datos al vender, y en la instalación puede cambiar el titular (por ejemplo, pidió el servicio un menor y queda de titular un adulto) o el plan. Todo esto pasa **solo mientras el servicio está `pendiente_instalacion`**; después de activar, un cambio de titular es un traspaso y un cambio de plan es A34, fuera de esta sección. Decisiones del PM en §10.5.

**Principios.**

| Regla | Detalle |
| --- | --- |
| Quién | El técnico, en el paso «Confirmar titular y plan» de la orden de instalación de Iris (app de campo, también sin señal), o un agente o supervisor desde el panel. Sin aprobación previa: queda auditado y ventas lo ve después. En Iris el técnico solo puede sobre **sus órdenes en curso** (asignadas a él, en sitio y no cerradas). |
| Orden | Iris aplica en Argos, en este orden y con dependencias en su cola de pendientes: cambiar el titular (A39, que incluye su identidad) o registrar la identidad del mismo titular (A41) → completar los datos del titular y del lugar (A43) → cambiar el plan (A40) → activar (`activar-instalacion` o `aprovisionar`, §12). Si una falla, las siguientes no salen (§3.4.8). |
| Actor | Todas las llamadas llevan `X-Iris-Actor: usuario:<id>` (§7.1) e `Idempotency-Key`. Argos audita con ese actor y canal `api`. El asistente de IA nunca las usa. |
| Identidad | La foto del documento es del cliente y **vive en Argos** (A41). Iris solo la tiene en tránsito (§3.4.8). El número de documento nunca va en eventos ni logs de ninguno de los dos lados. |

#### 3.4.1 Servicio pendiente sin IP (A38)

- `POST /servicios` con `estado_inicial: pendiente_instalacion` **no asigna** IPv4 ni prefijos IPv6. El IPAM asigna con el router final al activar: en `POST /servicios/:id/activar-instalacion` o en `POST /servicios/:id/aprovisionar` (§12.2). `user_ppp` y la contraseña PPP se generan al crear, como hoy.
- **Reservar en sitio** (para conexiones que necesitan la IP en el equipo antes de activar: IP fija o amarre IP-MAC): `POST /servicios/:id/reservar-ip` (`servicios.reservar_ip`), `Idempotency-Key = iris:reservar_ip:<orden_id>`, cuerpo `{ router_id?, origen_tipo: "iris_orden", origen_id }`. Solo en `pendiente_instalacion` (`422 servicio_no_pendiente`). Responde `200 { ipv4, prefijo_v6 }` (o `null`); una segunda llamada devuelve la misma reserva. La activación usa esa reserva; si el servicio se retira sin activar, se libera. Con PPPoE no hace falta.
- Sin IP libre en el router: `422 sin_ip_disponible`, en `reservar-ip`, en `activar-instalacion` y como `causa` de `aprovisionar`. Es de negocio y no se reintenta solo.
- El servicio pendiente ya no lleva IP en `GET /servicios/:id` ni en la primera sincronización (§2.5). Al asignarse, Argos emite `asignacion.cambiada` (ya existe).

#### 3.4.2 El titular: qué pide la orden

**¿Quién firma?** «La misma persona de la venta» (A41 sobre el cliente del servicio) u «Otra persona» (A39: Argos busca o crea el cliente por documento y le pasa el servicio). Para activar, el titular necesita:

| Dato | Regla |
| --- | --- |
| Tipo de documento | `dpi` o `pasaporte`. En modo empresarial: `nit` de la empresa y un **representante** con `dpi` |
| Número | DPI: 13 dígitos con la verificación de departamento y municipio. Pasaporte: alfanumérico de 5 a 20. NIT: con dígito verificador (`K` permitido) |
| Nombre completo | Tal como aparece en el documento (2 a 200) |
| Mayor de edad | Casilla obligatoria «El titular es mayor de edad» (`mayor_de_edad: true`); **sin** fecha de nacimiento |
| Teléfono | 8 dígitos de Guatemala (regla de §3.2) y `whatsapp` (bool) |
| Correo | Opcional |
| Facturación | `cf` o `nit` (con dígito verificador) |
| Relación con quien pidió el servicio | Opcional: `titular`, `familiar` u `otro` |
| Validación de identidad | Foto del documento: **frente obligatorio**, reverso opcional; el técnico marca «Coincide con el titular» (`coincide: true`); la firma del titular va en el contrato (en Iris) |

**Lugar del servicio:** dirección escrita, referencia, coordenadas GPS con su precisión en metros y foto de fachada opcional (la foto de fachada queda en Iris como adjunto de la orden; no viaja a Argos).

#### 3.4.3 Registrar la identidad del titular (A41)

`POST /api/v1/clientes/:id/identidad` (`clientes.registrar_identidad`), `multipart/form-data`, con `X-Iris-Actor` e `Idempotency-Key = iris:identidad:<orden_id>:<version>`:

| Parte | Regla |
| --- | --- |
| `frente` | Obligatoria. JPEG o PNG, hasta 5 MB |
| `reverso` | Opcional, igual |
| `documento_tipo` | `dpi`, `pasaporte` (o `dpi` del representante en empresarial) |
| `documento_numero` | Con las reglas de §3.4.2 |
| `nombre` | Como aparece en el documento |
| `mayor_de_edad` | `true` (otro valor: `422 validacion`) |
| `coincide` | `true`: el técnico comprobó que la foto es de quien está presente |
| `verificado_en` | RFC 3339, hora del teléfono corregida |
| `metodo` | `foto_documento` |
| `origen_tipo`, `origen_id` | `iris_orden` y la orden |

- Solo para el cliente de un servicio `pendiente_instalacion` con una orden de Iris en curso (`422 cliente_sin_instalacion`): el token de Iris no sube identidades de cualquier cliente.
- Argos la guarda **cifrada en reposo**. Solo la ve quien tiene `clientes.ver_identidad` en Argos; cada vista se audita; nunca sale por el portal, webhooks, eventos, logs ni MCP. Iris no la puede leer de vuelta.
- Reemplazarla deja la versión anterior; todas se borran **12 meses después de retirado el cliente**.
- `201 { identidad_id, version }`. **Idempotente por `Idempotency-Key`** (§3.1): misma clave y mismos bytes (las partes del multipart incluidas) devuelven el `201` original con `Idempotent-Replayed: true` y no registran otra versión; misma clave con otro cuerpo, `422 idempotency_key_reutilizada`. Emite `cliente.identidad_registrada { cliente_id, empresa_id, documento_tipo, metodo }` (§2.3), sin número ni foto.

#### 3.4.4 Cambiar el titular antes de activar (A39)

`POST /servicios/:id/cambiar-titular` (`servicios.cambiar_titular`), `multipart/form-data`, `Idempotency-Key = iris:titular:<orden_id>:<cambio_id>`: la parte `datos` (JSON) y las partes `frente` y `reverso?` de A41.

```json
{
  "cliente": { "nombre": "Ana Pérez", "documento_tipo": "dpi", "documento_numero": "1234567890101", "mayor_de_edad": true, "telefono": "+502 5512-3344", "whatsapp": true, "email": null, "facturacion": { "tipo": "cf", "nit": null }, "relacion": "familiar" },
  "identidad": { "coincide": true, "verificado_en": "2026-11-21T16:02:00Z", "metodo": "foto_documento" },
  "motivo": "El servicio lo pidió un menor; queda de titular su madre",
  "version": 7,
  "origen_tipo": "iris_orden",
  "origen_id": "9123"
}
```

- **Quién es el titular lo decide Argos.** Iris **nunca** manda un `cliente_id`. Argos compara en su lado, en la empresa del servicio:
    - **Mismo `documento_tipo` y `documento_numero` y mismo nombre normalizado** (sin tildes, sin distinguir mayúsculas y sin espacios dobles): usa el cliente existente, registra en él la identidad de A41 como **versión nueva** y le avisa por sus contactos registrados («Se agregó un servicio a tu nombre»). La respuesta solo dice `titular: { cliente_id, existente: true }`, sin ningún otro dato de ese cliente.
    - **Mismo documento y otro nombre:** `422 titular_no_coincide`, sin datos del otro cliente, y no cambia nada. La orden queda en revisión de un supervisor.
    - **Ningún cliente con ese documento:** crea uno nuevo (`titular: { cliente_id, existente: false }`). Solo marca `posible_duplicado: true` cuando coinciden el nombre o el teléfono y el documento no, para que una persona de Argos lo revise.
- La identidad que registra (A41) es la `evidencia` del cambio: no hay adjuntos de Iris.
- Solo en `pendiente_instalacion` (`422 servicio_no_pendiente`). `version` del servicio (`409 conflicto_version`). `motivo` de 10 a 500 caracteres. Sin `frente` o sin `coincide: true`: `422 evidencia_requerida`.
- Argos mueve el servicio y sus credenciales PPP al titular. Las facturas ya emitidas no se mueven (si hiciera falta, las rehace una persona en Argos); por eso Iris manda el cobro de la instalación (§3, A36) **después** de confirmar el titular. El cliente anterior, si se queda sin servicios ni movimientos, se **elimina**; si tiene historial, queda como está.
- Responde `200 { servicio_id, titular: { cliente_id, existente }, posible_duplicado, identidad_id, de_cliente_id, de_cliente_eliminado }` y emite `servicio.titular_cambiado` y `cliente.identidad_registrada` (§2.3).

#### 3.4.5 Cambiar el plan antes de activar (A40)

- `GET /planes?router_id=&modo=residencial|empresarial` devuelve **solo** los planes aplicables: activos, del mismo `modo` y asignados a todos los routers o a ese router. Cada plan suma `modo`. Iris guarda `modo` y los routers en su instantánea (`catalogo.cambiado` de plan suma `modo` y `routers` en `campos[]`).
- Por defecto, la orden propone el plan de la cotización. Si el técnico elige otro, el motivo es obligatorio (`mas_velocidad`, `menos_velocidad` u `otro` con texto) y la orden muestra el precio nuevo antes de confirmar.
- `POST /servicios/:id/cambiar-plan` (`servicios.cambiar_plan_pendiente`), `Idempotency-Key = iris:plan:<orden_id>:<cambio_id>`, cuerpo `{ plan_id, motivo, motivo_texto?, version, origen_tipo: "iris_orden", origen_id }`. Solo en `pendiente_instalacion` (`422 servicio_no_pendiente`). Sin factura ni prorrateo (no hay facturación todavía). Un plan que no sea de los aplicables: `422 plan_no_permitido`.
- Responde `200` con el servicio y emite `servicio.config_cambiada` con `plan` y `precio` en `campos[]` (ya existe; con `origen_tipo`/`origen_id`).
- Argos vuelve a comprobar el plan al activar: si el router final cambió y el plan ya no aplica, `activar-instalacion` y `aprovisionar` responden `422 plan_no_permitido`.

#### 3.4.6 Completar los datos del titular y del lugar (A43)

Argos solo exige nombre y teléfono al crear. Lo que falte lo manda Iris **antes de activar**:

- `POST /servicios/:id/completar-datos` (`servicios.completar_datos`), `Idempotency-Key = iris:datos:<orden_id>:<n>`, cuerpo `{ cliente?: { nombre?, telefono?, whatsapp?, email?, facturacion?: { tipo: cf | nit, nit? }, relacion? }, servicio?: { direccion?, referencia?, coordenadas?, precision_m? }, identidad_id?, version, origen_tipo: "iris_orden", origen_id }`.
- Solo con el servicio en `pendiente_instalacion` (`422 servicio_no_pendiente`) y para el cliente **de ese servicio**. Del cliente, **solo llena** campos vacíos (`422 campo_ya_registrado`); con `identidad_id` de una identidad registrada en esta instalación (A41 o A39), también corrige `nombre` para que coincida con el documento. Los del servicio (dirección, referencia y coordenadas de la instalación) sí se corrigen.
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
| 422 | `titular_no_coincide` | A39: el documento es de otro cliente con otro nombre. En Iris, la operación de la cola queda `rechazada` con este código y la orden pasa a revisión de un supervisor |
| 422 | `campo_ya_registrado` | A43 |
| 413 / 422 | `tamano_excedido` / `mime_no_permitido` | A39, A41 (fotos) |
| 422 | `validacion` | Cualquier campo (`errors[]`): documento, NIT, teléfono, `mayor_de_edad` |

Todos son de negocio y no se reintentan solos (§3.1).

#### 3.4.8 En Iris (informativo para Argos)

- **Sin señal primero.** En el teléfono, la foto del documento vive **solo en la cola local** hasta que el servidor de Iris confirma que la recibió; entonces se borra del teléfono. En el servidor de Iris queda en un **depósito de tránsito cifrado**, fuera de los adjuntos y fuera de la auditoría (`***`), y sale a Argos por `integracion.pendientes` con `Idempotency-Key = iris:identidad:<orden_id>:<version>` (o dentro de A39). Al recibir el `201` (o el `200` de A39) se borra.
- **Si Argos no la recibe:** con los reintentos agotados, Iris borra la foto a los **7 días** y la orden muestra el error para tomarla otra vez. La orden **no activa** sin la identidad registrada en Argos, salvo que un agente la valide desde el panel (permiso `ordenes.validar_identidad`, con motivo, auditado).
- **El número de documento** nunca va en eventos ni logs de Iris; en la auditoría, `***`.
- **El prospecto** queda «ganado, alta pendiente» (`alta.estado = esperando_activacion`) ligado al cliente y al servicio de Argos hasta que el servicio se activa; ahí pasa a `completa`. Si cambia el titular, el prospecto pasa al titular nuevo.
- **La firma** del titular es la del **contrato regenerado** a su nombre, en la orden (un menor no puede quedar como quien firmó).
- **El cambio de plan** crea una revisión nueva de la cotización con el plan nuevo (reemplaza la vigente) y avisa al vendedor. Los planes que el técnico puede elegir son los de `GET /planes?router_id=&modo=`, guardados en el paquete del día.
- **El cobro de la instalación** de la orden (§3) sale después del paso «Confirmar titular y plan», a nombre del titular final y con el plan final.
- **`titular_no_coincide`:** la operación `orden.cambiar_titular` queda `rechazada` en la sincronización con ese código (regla en `contratos/sync.md`), la orden pasa a revisión de un supervisor y la activación no sale. El técnico no ve datos del otro cliente.
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
- `telefonos[]`: `numero` en E.164, `whatsapp` y `principal` (uno por contacto). **Un mismo número puede estar en varios clientes.**
- **Reservado al titular**, siempre con la identidad de A41: cambiar titular, cambiar plan, cambiar el Wi-Fi, traspaso y baja. Ningún permiso de contacto los abre.

**Quién los gestiona.** El personal, con `contactos.gestionar` y `X-Iris-Actor` (§7.1), y el titular desde el portal de Argos.

**Rutas.**

| Método y ruta | Permiso | Qué hace |
| --- | --- | --- |
| `GET /contactos/buscar?telefono=` | `contactos.buscar` | `telefono` en E.164. Responde `[{ cliente_id, empresa_id, contacto_id, nombre, relacion, es_titular, servicios[], permisos }]`, dentro del alcance de empresas del token. **No** devuelve otros números ni documentos. Límite por tasa: 60 por minuto por token (`429 demasiadas_busquedas` con `Retry-After`) |
| `GET /clientes/:id/contactos` | `contactos.buscar` | Los contactos del cliente, con sus teléfonos |
| `POST /clientes/:id/contactos` | `contactos.gestionar` | Crea un contacto. `Idempotency-Key` obligatoria; `201` con el contacto |
| `PUT /contactos/:id` | `contactos.gestionar` | Lo edita, con `version` (`409 conflicto_version`). `Idempotency-Key` obligatoria |
| `DELETE /contactos/:id` | `contactos.gestionar` | Lo quita, con `version`. `Idempotency-Key` obligatoria. El contacto titular no se quita (`422 contacto_titular`) |

- Idempotencia como §3.1. Claves de Iris: `iris:contacto:<cliente_id>:<contacto_iris_id>` al crear, `iris:contacto:<contacto_id>:<version>` al editar o quitar.
- **Desde el token de Iris** (ajuste i de §10.5): un contacto nunca puede ser titular (`relacion: titular` o cambiar `es_titular`: `422 contacto_titular_no_permitido`). Iris no le da permisos por encima de los que tiene el agente que lo crea o edita (lo valida Iris; Argos audita el actor).
- Cada cambio emite `cliente.contactos_cambiados` (§2.3), sin números.
- **Avisos de cobro:** las plantillas de cobro de §6.3 (`factura_por_vencer`, `factura_vencida`, `corte_por_falta_de_pago`) también salen a los contactos con `avisos` encendido del servicio, respetando el permiso de contacto **de cada número** (§6.4, con `telefono` opcional en E.164). Iris toma los números de su caché de contactos, nunca del cuerpo del aviso.
- **§7.6:** `GET /clientes?telefono=` sigue existiendo; para atender por chat o llamada, Iris usa `GET /contactos/buscar`, que dice además el rol y los permisos.

**En Iris** (informativo para Argos):

- Al llegar un mensaje o una llamada, Iris busca el número (en su caché o con `buscar`) y asocia la conversación al cliente **con el rol del contacto**.
- **Varios clientes con el mismo número** (ajuste g de §10.5): el chat pregunta de cuál se trata **sin mostrar datos de ninguno**; solo pide una referencia que el cliente conoce (su número de servicio, o la dirección que él mismo escribe) y la compara. Nunca lista nombres ni direcciones.
- El asistente y los agentes ofrecen solo lo que permiten los `permisos` del contacto; para lo reservado al titular piden su validación (A41 o §7.5).
- Si un contacto de Iris (`contacto_iris`) se une a un cliente, Iris lo crea en Argos con `POST /clientes/:id/contactos`.
- La caché se refresca con `cliente.contactos_cambiados`.

## 4. Versionado

Semver. Esta versión 0.10.0 es aditiva sobre la 0.9.0 (ver «Gobierno»), que está acordada con Argos y decidida por el PM; las dos están pendientes de implementar en los dos lados. La 0.8.1 está espejada (`iris#222` y `argos-isp#258`) y salió en Argos 1.21.0. Desde 1.0.0, solo cambios compatibles en versiones menores.

- **0.10.0** (21 nov 2026; propuesta de Argos, decisiones del PM y ajustes de Iris en §10.5): de la venta a la instalación, §3.4. **A38:** un servicio `pendiente_instalacion` no reserva IP; el IPAM asigna al activar (`activar-instalacion` o `aprovisionar`) con el router final, y `POST /servicios/:id/reservar-ip` la reserva en sitio cuando el equipo la necesita antes de activar. **A39:** `POST /servicios/:id/cambiar-titular` (multipart: datos del titular, identidad, motivo, `version`), solo en pendiente; Argos busca o crea el titular por documento en su lado; el cliente anterior sin historial se elimina; evento `servicio.titular_cambiado`. **A40:** `GET /planes?router_id=&modo=` con solo los aplicables y `POST /servicios/:id/cambiar-plan` en pendiente, con motivo y sin factura, con `servicio.config_cambiada`. **A41:** `POST /clientes/:id/identidad` (foto del documento, número, `coincide`), guardada cifrada en Argos, visible solo con `clientes.ver_identidad`, borrada 12 meses después de retirado el cliente; evento `cliente.identidad_registrada`. **A42:** contactos del cliente en Argos (§3.5): varios por cliente con relación, servicios y permisos (`consultar`, `reportar`, `pagar`, `avisos`), teléfonos E.164 con WhatsApp; `GET /contactos/buscar?telefono=`, `GET/POST /clientes/:id/contactos`, `PUT/DELETE /contactos/:id`; evento `cliente.contactos_cambiados`; permisos `contactos.buscar` y `contactos.gestionar`; los avisos de cobro también salen a los contactos con `avisos`. **A43:** `POST /servicios/:id/completar-datos`, que solo llena campos vacíos del titular del servicio (o corrige el nombre con la identidad) y corrige el lugar. Permisos `servicios.reservar_ip`, `servicios.cambiar_titular`, `servicios.cambiar_plan_pendiente`, `clientes.registrar_identidad`, `servicios.completar_datos`, `contactos.buscar` y `contactos.gestionar`. Pedidos A38–A43 y B17–B22.
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
- `GET /api/v1/integraciones/argosisp/estado` (Iris, ya existe) suma `capacidades: { portal_argos: boolean, solicitudes_servicio: boolean, adjuntos_ticket: boolean }`. Argos muestra cada sección de su portal solo con su marca en `true`.

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
| Alias `cliente_id` en `GET /tickets` | No aplica | Iris anterior lo ignora y devuelve toda la empresa: Argos manda **siempre** `clienteId` |

**Errores nuevos de la 0.9.0.** Todos `application/problem+json` (RFC 9457) con `type` `urn:iris:problema:<codigo>` (rutas de Iris) o `urn:argosisp:problema:<codigo>` (rutas de Argos). Los de negocio no se reintentan; solo `5xx`, timeouts, `429` (con `Retry-After`) y el `409` de idempotencia en curso (§3.1).

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
| §12.2 | 409 | `aprovisionamiento_en_curso` | Hay otro en curso para el servicio (`extensions.aprovisionamiento_id`) |
| §12.2 | 422 | `equipo_invalido` | `ont` sin serie, `cpe_wireless` sin MAC, `planta` con wireless, formato, o el tipo no coincide con la tecnología del servicio |
| §12.2 | 422 | `aprovisionamiento_no_disponible` | La empresa no tiene SmartOLT o ACS configurado |
| §12.2 | 409 | `unidad_en_otra_bodega` | El equipo escaneado está en inventario, pero no en la bodega del técnico |
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

Errores nuevos: los de §3.4.7.

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
| A32 | **Aprovisionar desde la orden de Iris** (0.9.0, §12.2): `POST /servicios/:id/aprovisionar` asíncrono para FTTH y wireless (IPAM, autorización de la ONT en SmartOLT por serie, del CPE/radio por MAC o del router por TR-069 con el perfil y la VLAN del plan y del router/OLT/AP, Wi-Fi inicial cuando el equipo es gestionado, y activación con el inicio del cobro mensual), `GET /aprovisionamientos/:id`, webhooks `instalacion.aprovisionada` / `instalacion.fallida` y `capacidades.aprovisionamiento` por empresa. Permiso `servicios.aprovisionar` con migración de tokens. | acordado; pendiente en Argos |
| A33 | **Estado del equipo** (0.9.0, §12.3): `GET /servicios/:id/equipo` con en línea, señal óptica, firmware y último contacto. Permiso `servicios.ver_equipo`. | acordado; pendiente en Argos |
| A34 | **Cambio de plan y traslado por API** (sin forma; para después de 0.9.0): rutas para que Iris aplique en Argos un cambio de plan o de dirección ganado en ventas (§11.7). Mientras no existan, lo hace una persona en Argos. | propuesto (sin forma) |
| A35 | **Cambiar el Wi-Fi pedido desde Iris** (0.9.0, §12.4): `POST /servicios/:id/wifi` asíncrono y webhook `equipo.wifi_resuelto`, para personas y para el asistente de IA (canal `ia`), siempre con `verificacion { metodo, verificado_en, cliente_id }` (asistente: menos de 10 min y solo código o documento; si no, o si el servicio no es de ese cliente, `422 verificacion_requerida`); máximo 3 cambios por servicio al día (`429 demasiados_cambios_wifi`) y aviso al titular por sus contactos registrados después de `aplicado`. Permisos separados `servicios.ver_equipo` y `servicios.cambiar_wifi`. La clave nunca se guarda en claro ni sale en auditoría, eventos, logs ni historial del chat. | acordado; pendiente en Argos |
| A36 | **Webhooks de facturas y pagos del cobro de la orden** (0.9.0, §3): `POST /facturas/cargo-unico` ya existe y acepta `origen_tipo = iris_orden` + `origen_id` (A4), con el permiso `facturas.emitir_cargo_unico`; **su respuesta no cambia**: `201 CargoUnicoEmitido = { documento: 'factura' \| 'bonificacion', factura: FacturaDetalle \| null, bonificacion: Bonificacion \| null }`. Iris lee `factura.id`, `factura.numero`, `factura.estado`, `factura.total` y `factura.moneda`; `documento = bonificacion` (cargo en Q0 sin factura) es una respuesta posible, pero Iris nunca la pide: no manda Q0 ni tiene `facturas.cargo_sin_costo`. Cuerpo: `{ servicio_id, cliente_id?, vence_en?, lineas: [{ concepto_id, precio_unitario, descripcion?, cantidad?, descuentos? }], origen_tipo: "iris_orden", origen_id }`; `concepto_id` de `GET /conceptos-cargo` («Instalación» sembrado por empresa; los conceptos no tienen precio) y `precio_unitario` (> 0, bruto con IVA) lo pone Iris. **Lo nuevo:** enviar a Iris `factura.emitida`, `factura.anulada`, `pago.registrado` y `pago.anulado`, solo de facturas con `origen_tipo = iris_orden`, con `origen_id` en el payload y a conectores con contrato de Iris ≥ 0.9.0 (hoy solo se envían `comprobante.*` y `promesa.*`). | acordado; pendiente en Argos |
| A37 | **Bienvenida al activar, con seguimiento** (0.9.0, §12.5): pedir `bienvenida_servicio` (con `nombre`, `plan`, `precio_mensual`, `moneda`, `codigo_cliente`) en lugar de `bienvenida` cuando Iris informa contrato ≥ 0.9.0, con `origen: { tipo: "iris_orden", id }` si la activación vino de una orden de Iris; el webhook `servicio.bienvenida_cambiada` con el resultado de cada canal (también correo y Telegram); y `POST /servicios/:id/bienvenida/reenviar` (permiso `servicios.reenviar_bienvenida`; máximo 3 por servicio al día, `429 demasiados_reenvios`). | acordado; pendiente en Argos |
| A38 | **Servicio pendiente sin IP** (0.10.0, §3.4.1): `POST /servicios` con `estado_inicial: pendiente_instalacion` no asigna IPv4 ni prefijos; el IPAM asigna con el router final en `activar-instalacion` o `aprovisionar`; `POST /servicios/:id/reservar-ip` para reservarla en sitio (IP fija o amarre); `422 sin_ip_disponible`. Cambia `contratos/instalacion.md` §1 de Argos («un servicio pendiente ya tiene IP asignada»). | propuesto por Argos; ajuste de Iris (reservar en sitio) pendiente de aceptar |
| A39 | **Cambiar el titular antes de activar** (0.10.0, §3.4.4): `POST /servicios/:id/cambiar-titular` en multipart con los datos del titular (§3.4.2), su identidad (A41), `motivo` y `version`. Iris nunca manda `cliente_id`: Argos usa el cliente existente solo si coinciden documento **y** nombre normalizado en la misma empresa (`titular: { cliente_id, existente: true }`, identidad como versión nueva, aviso al titular), responde `422 titular_no_coincide` si coincide el documento y no el nombre, y si no crea uno nuevo (`posible_duplicado` solo con nombre o teléfono iguales y documento distinto); el cliente anterior sin historial se elimina; evento `servicio.titular_cambiado`; permiso `servicios.cambiar_titular`. | acordado con Argos |
| A40 | **Cambiar el plan antes de activar** (0.10.0, §3.4.5): `GET /planes?router_id=&modo=` solo con los aplicables (`modo` en el plan; `modo` y `routers` en `catalogo.cambiado`) y `POST /servicios/:id/cambiar-plan` en pendiente, con motivo (`mas_velocidad`, `menos_velocidad` u `otro`) y sin factura; `422 plan_no_permitido`, también al activar si el router final cambió; permiso `servicios.cambiar_plan_pendiente`. | propuesto por Argos |
| A41 | **Identidad del titular** (0.10.0, §3.4.2 y §3.4.3): `POST /clientes/:id/identidad` en multipart (frente obligatorio, reverso opcional, `documento_tipo`, `documento_numero`, `nombre`, `mayor_de_edad`, `coincide`, `verificado_en`, `metodo: foto_documento`), con `X-Iris-Actor`, solo para el cliente de un servicio pendiente con orden de Iris en curso; guardada cifrada en Argos, visible solo con `clientes.ver_identidad` (cada vista auditada), nunca por portal, webhooks, eventos, logs ni MCP; versiones anteriores conservadas y borradas 12 meses después de retirado el cliente; evento `cliente.identidad_registrada`; permiso `clientes.registrar_identidad`. | propuesto por Argos |
| A42 | **Contactos del cliente** (0.10.0, §3.5): varios contactos por cliente con `relacion`, `servicios[]`, `permisos` (`consultar`, `reportar`, `pagar`, `avisos`) y `telefonos[]` (E.164, WhatsApp, principal); lo reservado al titular siempre con la identidad de A41; `GET /contactos/buscar?telefono=` (sin otros números ni documentos, 60 por minuto), `GET/POST /clientes/:id/contactos`, `PUT/DELETE /contactos/:id` con `version` e idempotentes; `cliente.contactos_cambiados` sin números; avisos de cobro también a los contactos con `avisos`, con el permiso de cada número; permisos `contactos.buscar` y `contactos.gestionar`. | propuesto por Argos, decidido por el PM; ajustes de Iris (g, h, i de §10.5) aceptados por Argos |
| A43 | **Completar los datos en la instalación** (0.10.0, §3.4.6): `POST /servicios/:id/completar-datos`, solo con el servicio pendiente, solo para el titular de ese servicio y solo llenando campos vacíos (`422 campo_ya_registrado`), salvo el `nombre`, que se corrige con una identidad registrada en esa instalación; dirección, referencia y coordenadas de la instalación sí se corrigen; permiso `servicios.completar_datos`. Reemplaza el `PUT /clientes/:id` general de la propuesta de Argos. | ajuste de Iris, aceptado por Argos |

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
| B12 | **Activación desde la orden, lado Iris** (0.9.0, §12): FTTH y wireless; operación sin conexión `orden.activar_servicio` y activación por un agente desde el panel (también con la orden cerrada), estado en la orden, modo configurable por empresa o plantilla (al cerrar como hoy; exigir la activación lista salvo un supervisor con motivo; o cerrar con «activación pendiente por agente»), `orden.cerrada` con `aprovisionado` y sin repetir la activación ni el comodato de ese equipo, estado del equipo y cambio de Wi-Fi en la orden y en el ticket. | acordado; pendiente en Iris |
| B13 | **Origen `portal_argos` en tickets** y canal `portal_argos` en `ticket.mensaje_cliente` (0.9.0, §11.4). | acordado; pendiente en Iris |
| B14 | **Asistente de IA con los equipos** (0.9.0, §12.3 y §12.4): acciones apagadas de fábrica, permisos `servicios.ver_equipo` y `servicios.cambiar_wifi` asignables al asistente, verificación por código al teléfono o correo registrados en Argos o por el documento del titular (§7.5) antes de cambiar el Wi-Fi (para un solo cambio, caduca al cambiar el cliente asociado), el asistente **nunca pide ni recibe la clave**: Iris manda un enlace de un solo uso a un formulario propio (§12.4); una clave escrita en el chat se reemplaza por `***` antes del modelo y no se usa; el modelo no recibe serie, MAC, firmware ni modelo del equipo. | acordado; pendiente en Iris |
| B15 | **Cobro de la instalación desde la orden** (0.9.0, §3): lo registra un agente o sale de un formulario de la orden (concepto y `precio_unitario` > 0 de la plantilla o del formulario; cambiar el precio exige `ordenes.cobro_editar_monto`, con motivo auditado; nunca Q0), sale en el momento por `integracion.pendientes` con `lineas[{ concepto_id, precio_unitario }]`, `X-Iris-Actor` e `Idempotency-Key = iris:cargo:<orden_id>:<cargo_id>`, no repite la instalación al cerrar y la orden sigue la factura (emitida, pagada, anulada). | acordado; pendiente en Iris |
| B16 | **Bienvenida en la orden** (0.9.0, §12.5 y §6.3): plantilla `bienvenida_servicio` con las cuentas que Iris inserta de la instantánea de §7.8 (nunca de Argos ni de texto libre), `origen` opcional en §6.2, estado de la bienvenida en la orden, «Reenviar bienvenida», bandeja «Bienvenidas sin enviar» y el ajuste `cierre_exige_bienvenida` (apagado de fábrica). | acordado; pendiente en Iris |
| B17 | **Alta pendiente hasta activar** (0.10.0, §3.4.8): prospecto «ganado, alta pendiente» (`alta.estado = esperando_activacion`) ligado al cliente y al servicio de Argos hasta la activación; pasa al cliente nuevo con `servicio.titular_cambiado`. | pendiente en Iris |
| B18 | **Paso «Confirmar titular y plan» en la orden** (0.10.0, §3.4.2 y §3.4.8): antes de «Activar»; «¿Quién firma?», datos del titular, lugar del servicio, plan y validación de identidad; operaciones sin conexión `orden.registrar_identidad`, `orden.cambiar_titular`, `orden.completar_datos` y `orden.cambiar_plan` (reglas en `contratos/sync.md`); pendientes en orden con dependencias y activación bloqueada sin identidad registrada o si Argos rechaza; permisos `ordenes.cambiar_titular`, `ordenes.cambiar_plan`, `ordenes.completar_datos` (técnico solo en sus órdenes en curso) y `ordenes.validar_identidad` (agente, para activar sin la identidad registrada, con motivo). | pendiente en Iris |
| B19 | **Foto del documento solo en tránsito** (0.10.0, §3.4.8): en el teléfono, solo en la cola local hasta que el servidor de Iris la confirma; en el servidor, en un depósito de tránsito cifrado, fuera de los adjuntos y de la auditoría (`***`), enviada por `integracion.pendientes` (`iris:identidad:<orden_id>:<version>`) y borrada al `201`, o a los 7 días si se agotan los reintentos (la orden muestra el error). El número del documento nunca en eventos ni logs. La retención y quién la ve son de Argos (A41). | pendiente en Iris |
| B20 | **Cotización y contrato al cambiar** (0.10.0, §3.4.8): revisión nueva de la cotización con el plan nuevo y aviso al vendedor; contrato regenerado a nombre del nuevo titular y firmado en la orden. | pendiente en Iris |
| B21 | **Planes permitidos sin señal y cobro después del titular** (0.10.0, §3.4.8): planes de `GET /planes?router_id=&modo=` en el paquete del día; el cobro de la instalación sale después de confirmar el titular. | pendiente en Iris |
| B22 | **Contactos en la atención** (0.10.0, §3.5): al llegar un mensaje o una llamada, buscar el número y asociar la conversación al cliente con el rol del contacto; con varios clientes, preguntar sin mostrar datos de ninguno; el asistente y los agentes ofrecen solo lo que permiten los `permisos` y piden la validación del titular para lo reservado; un `contacto_iris` unido a un cliente se crea en Argos; caché refrescada por `cliente.contactos_cambiados`; consentimiento por número. | pendiente en Iris |

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
- **`origen` (0.9.0, opcional):** `{ "tipo": "iris_orden", "id": "<orden_id>" }`, solo en `bienvenida_servicio`, cuando la activación vino de una orden de Iris (§12.5); sin `origen` (o `null`) si la activación fue desde el panel de Argos. Otro tipo, o una orden que no es de ese servicio: `422 origen_invalido`. No cambia la `Idempotency-Key`.

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
| `bienvenida_servicio` (0.9.0) | `nombre`, `plan`, `precio_mensual`, `moneda`, `codigo_cliente`; `cuentas` la inserta Iris (§7.8), nunca Argos. Iris valida las variables (§12.5) | Argos | `servicio.instalacion_activada`, con Iris en contrato ≥ 0.9.0 (con uno anterior, `bienvenida`). Detalle en §12.5 |

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

- **0.10.0 (A42, §3.5):** el permiso de contacto es también **por número**: `POST /api/v1/mensajeria/consentimientos` acepta `telefono` opcional (E.164) para el número de un contacto. Los avisos de cobro a los contactos con `avisos` respetan el de cada número.

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

**0.9.0:** suma tres eventos, **solo a conectores con contrato ≥ 0.9.0** (§4.3). Solo de tickets con `cliente_id` dentro del alcance del conector, de cualquier origen. Nunca llevan el cuerpo de un mensaje, notas internas, un motivo escrito por una persona, ni datos de otro cliente; Argos lee lo que muestra con §11.

| Evento | Payload | Efecto en Argos |
| --- | --- | --- |
| `ticket.estado_cambiado` | ticket_id, numero, empresa_id, cliente_id, servicio_id (o null), origen, de, a, puede_confirmar (`a = resuelto`) | Actualiza el portal y, si corresponde, avisa al cliente (`resuelto`: «revisa y confirma»; `cerrado`) |
| `ticket.respondido` | ticket_id, numero, empresa_id, cliente_id, mensaje_id, autor_tipo (`agente` \| `sistema`), masivo (true en el aviso de una falla general), aviso_por_correo (true si Iris ya lo mandó por correo al contacto del ticket) | Avisa «tienes una respuesta en tu ticket TK-…»; con `aviso_por_correo` no manda correo; con `masivo`, un solo aviso por cliente |
| `solicitud_servicio.estado_cambiado` | solicitud_id, numero, empresa_id, cliente_id, tipo, servicio_id (o null), de, a (`estado_cliente` de §11.7) | Actualiza «Mis solicitudes» y avisa al cliente |

Un mensaje que el cliente escribió desde el portal de Argos (`ticket.mensaje_cliente` con canal `portal_argos`) no vuelve como webhook. Un ticket fusionado con el de **otro** cliente solo llega como `ticket.estado_cambiado` a `cerrado`, sin el destino.

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
- El código por WhatsApp y la revisión de una foto del DPI los hace Iris (la foto la revisa siempre una persona) y no necesitan a Argos.
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

## 10. Decisiones y cierre de la 0.8.0, la 0.9.0 y la 0.10.0

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

**Ajustes de Iris a la propuesta de Argos** (ninguno cambia una decisión del PM; b, d, g, h e i ya aceptados por Argos; a, c, e y f pendientes de que los acepte):

- **a. Reservar la IP en sitio (A38).** Hoy el técnico configura el CPE con la IP del pendiente (`contratos/instalacion.md` §1 de Argos). Sin IP hasta activar, una conexión con IP fija o amarre no se podría configurar antes de cerrar en el modo de fábrica (activar al cerrar, §10.4 punto 6). `reservar-ip` la asigna en la instalación, nunca al ganar.
- **b. Cambio de titular sin `cliente_id` de Iris (A39; acordado con Argos).** El técnico no puede ver ni elegir otros clientes: decide Argos. Usa un cliente existente solo si coinciden el documento **y** el nombre normalizado, sin devolverle a Iris más que su id, y le avisa; si coincide el documento y no el nombre, `422 titular_no_coincide` y revisión de un supervisor. Así un número de documento escrito no basta para pasarle un servicio (y su cobro) a otra persona.
- **c. `de_cliente_eliminado` en el evento (A39).** Sin él, Iris no sabe que el cliente anterior ya no existe.
- **d. `completar-datos` en lugar de `PUT /clientes/:id` (A43).** El token de Iris no debe editar cualquier cliente: solo llena lo que falta del titular del servicio pendiente (y corrige el nombre con la identidad registrada).
- **e. Contrato firmado por el nuevo titular (B20).** La firma que pide el PM es la del contrato regenerado a su nombre: el que firmó el menor no sirve.
- **f. Cobro después del titular (B21).** La factura de la instalación sale a nombre del titular final; las ya emitidas no se mueven.
- **g. Varios clientes con el mismo número (A42, B22).** El chat pregunta de cuál se trata sin mostrar datos de ninguno: solo pide una referencia que el cliente conoce (número de servicio o la dirección que él escribe). Nunca lista nombres ni direcciones.
- **h. `buscar` acotado (A42).** No devuelve otros números ni documentos, y se limita por tasa.
- **i. Contactos desde Iris (A42).** Un contacto creado desde Iris nunca puede ser titular ni recibir permisos por encima de los del agente.

**Valor inicial del líder de Iris** (cambiable sin tocar el contrato): un cambio de plan que sube el precio también pide la firma del titular sobre la cotización nueva, apagado de fábrica (lo enciende la empresa).

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
| `GET /tickets?estado=&cursor=&limit=` | `portal_argos.ver_tickets` | Sus tickets en la empresa, de cualquier origen, más recientes primero. `estado` admite varios separados por coma |
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

- **Subir:** solo dentro de `POST …/tickets` y `POST …/responder` en `multipart/form-data`. Hasta **3** archivos por mensaje, **10 MB** cada uno; tipos `image/jpeg`, `image/png`, `image/webp`, `image/heic` y `application/pdf`, comprobados por el contenido (no por el nombre). Fuera de eso: `413 tamano_excedido`, `422 mime_no_permitido` o `422 demasiados_adjuntos`, y no se crea nada. Iris quita los metadatos EXIF de las fotos. Igual que los comprobantes de §7.2: Argos sube los bytes y nadie descarga URLs del otro (sin SSRF).
- La idempotencia incluye los archivos: un reintento manda exactamente los mismos bytes.
- **Al guardar:** Iris recodifica las imágenes con un tope de píxeles (por ejemplo, 40 MP) y quita los metadatos (EXIF y los de HEIC).
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
| `plan_interes_id` | Opcional, un plan de Argos (`GET /planes`) **de esa empresa**; otro: `422 validacion`, `rule: "plan"` |
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
| Capacidad | Argos informa por empresa y tecnología qué puede hacer (`capacidades.aprovisionamiento`, §4.3). Sin ella, Iris activa al cerrar como en §3. |
| Facturación | El mensual empieza cuando el servicio queda activo. El cobro de la instalación es una factura única aparte (`POST /facturas/cargo-unico` con una línea del concepto «Instalación» y el precio que pone Iris, §3 y A36), que registra un agente o sale de un formulario de la orden. |

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

- `equipo.tipo`: `ont` (exige `serie`), `cpe_wireless` (exige `mac`; `serie` opcional) o `router_tr069` (`mac` o `serie`, la que use el ACS). `unidad_id` opcional si Iris ya lo conoce del inventario. Si el equipo es una unidad del inventario de Argos, debe estar en la bodega del técnico (`409 unidad_en_otra_bodega`) y Argos la asigna en comodato al servicio en la misma operación. Sin `tecnico` (lo pide un agente desde el panel con la orden cerrada), la unidad debe estar en la bodega del técnico de la orden (Iris manda ese técnico) o ya asignada al servicio.
- `planta` (A9) solo con `ont`; `wireless` (`ap`, `sector`, opcionales) solo con `cpe_wireless`. Otra combinación: `422 equipo_invalido`.
- `wifi` opcional: SSID de 1 a 32 caracteres, clave de 8 a 63 (`422 wifi_invalido`). Con un equipo que Argos no gestiona (wireless sin TR-069), `wifi` responde `422 wifi_no_disponible` y no se aprovisiona nada: Iris lo reenvía sin `wifi`.
- **Respuesta** `202 { aprovisionamiento_id, estado: "en_proceso" }`. Si ya terminó (reintento con la misma clave), `200` con el estado final.
- **Errores** inmediatos: `409 servicio_no_pendiente`, `409 aprovisionamiento_en_curso` (con `extensions.aprovisionamiento_id`), `409 unidad_en_otra_bodega`, `422 equipo_invalido`, `422 wifi_invalido`, `422 wifi_no_disponible`, `422 aprovisionamiento_no_disponible` (la empresa no tiene esa tecnología configurada); desde 0.10.0 también `422 plan_no_permitido` (§3.4.5).

`GET /api/v1/aprovisionamientos/:id` (`servicios.aprovisionar`) → `200 { aprovisionamiento_id, servicio_id, empresa_id, origen_tipo, origen_id, estado, pasos[], causa, reintentable, detalle, servicio_estado, equipo, senal, creado_en, actualizado_en }`.

- `estado`: `en_proceso`, `listo` o `fallido`.
- `pasos[]`: `{ paso: ip | equipo | wifi | activacion, estado: pendiente | hecho | fallido | no_aplica, en }`. Wireless sin TR-069: `equipo` y `wifi` vienen `no_aplica`.
- `equipo`: `{ tipo, serie, mac, unidad_id, gestionado }` (`gestionado`: Argos lo controla por SmartOLT o el ACS).
- `causa` (con `fallido`): `equipo_no_encontrado` (la ONT no aparece en la OLT, o el CPE/router no contactó al ACS), `equipo_en_uso` (la serie o la MAC ya están en otro servicio), `sin_puerto`, `perfil_sin_mapear` (el plan o el router/OLT/AP no tienen perfil), `sin_ip_disponible`, `olt_sin_respuesta`, `acs_sin_respuesta`, `servicio_no_pendiente`, `otro`. `reintentable` dice si tiene sentido reintentar sin cambiar nada; `detalle` es texto para la persona, sin secretos.
- Con `listo`, el servicio queda `activo` y **empieza el cobro mensual**. Argos emite además sus eventos de siempre (`servicio.estado_cambiado`, `servicio.instalacion_activada`). El cobro de la instalación no sale de aquí: es una factura única (§3).

Webhooks Argos → Iris (§2.3): `instalacion.aprovisionada` e `instalacion.fallida`, uno final por aprovisionamiento. Iris los deduplica por `X-Webhook-Id` y los relaciona por `aprovisionamiento_id` o por `idempotency_key`. Si no llega en 10 min, Iris consulta el `GET`.

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
    - **Asistente de IA** (`X-Iris-Actor: asistente_ia`, con `X-Iris-Conversacion`): `metodo` = `codigo_telefono` o `codigo_correo` (código de un solo uso que Iris manda al teléfono o al correo **registrados en Argos**) o `datos_titular` (solo si en esa conversación coincidió el `documento` del desafío de §7.5; `nombre` y `codigo_cliente` nunca bastan, ni juntos; un cliente `cf` sin documento, solo los códigos). Es el nivel que pide el portal de Argos. Más de **10 min** desde `verificado_en`: `422 verificacion_requerida`. En Iris, la verificación vale para **un solo cambio** y caduca si cambia el cliente asociado a la conversación. Iris solo lo manda si la empresa habilitó la acción y el asistente tiene `servicios.cambiar_wifi`.
    - **Persona** (`usuario:<id>`): además de los anteriores, `presencial` (el técnico en sitio, en su orden en curso) o `llamada_verificada` (soporte confirmó al titular con el desafío de §7.5). Argos lo guarda en la auditoría.
- **El asistente nunca pide ni recibe la clave.** Cuando el cliente verificado quiere cambiar el Wi-Fi, Iris le manda un **enlace de un solo uso** (10 min, ligado a la conversación, al servicio y a esa verificación) a un formulario de Iris donde escribe el SSID y la clave; desde ahí sale esta llamada (con `X-Iris-Actor: asistente_ia` y `origen_tipo: iris_conversacion`). Una clave escrita en el chat se reemplaza por `***` antes de llegar al modelo y al historial, y **no se usa**: el asistente le pide usar el enlace. El mensaje de confirmación no la repite.
- **Límites y aviso:** como máximo **3 cambios por servicio al día**, sumando todos los actores (`429 demasiados_cambios_wifi` con `Retry-After`). Después de `aplicado`, Argos avisa al titular por sus contactos registrados («Se cambió el Wi-Fi de tu servicio; si no fuiste tú, comunícate»).
- Argos audita el cambio con el actor de §7.1 (canal `ia` para el asistente), la `verificacion` completa y la clave como `***`.
- Permisos de Iris (informativo): `servicios.cambiar_wifi`; un técnico solo sobre sus órdenes en curso.
- `GET /servicios/:id/equipo` (§12.3) y esta ruta exigen permisos separados en el token de Iris: `servicios.ver_equipo` y `servicios.cambiar_wifi`.

### 12.5 Mensaje de bienvenida al activar (A37, B16)

Al quedar activo un servicio, el cliente recibe la bienvenida con sus datos y las cuentas para pagar. Argos decide cuándo (al activarse, por cualquier vía: `activar-instalacion` o `instalacion.aprovisionada`) y por qué canales (WhatsApp por Iris, §6; correo y Telegram por su `notify`). La orden de Iris la **sugiere y la valida**: muestra si salió y ofrece reenviarla. No bloquea el cierre de la orden salvo que la empresa lo configure en Iris.

- **Plantilla `bienvenida_servicio`** (§6.3): variables de Argos `nombre`, `plan`, `precio_mensual`, `moneda` y `codigo_cliente`. Las **cuentas para pagar las inserta Iris** con su instantánea de §7.8 (solo activas, de la empresa del servicio, en `GTQ` y con `titular`). Argos **nunca** manda números de cuenta en las variables. Sin cuentas válidas, Iris usa la variante sin cuentas y el aviso queda con `detalle: sin_cuentas`. Con un Iris < 0.9.0, Argos sigue pidiendo `bienvenida`.
- **Validación de las variables** (Iris, antes de encolar): `nombre` hasta 60 caracteres y `plan` hasta 80, sin saltos de línea, sin URLs ni dominios y sin secuencias de 4 o más dígitos (la lista blanca de §6.3); `codigo_cliente` `^[A-Za-z0-9-]{1,30}$`; `precio_mensual` decimal con dos decimales; `moneda` ISO 4217. Fuera de forma: `422 validacion` (con `errors[].field`) y el aviso no sale.
- **Cuentas recién cambiadas:** una cuenta creada o editada en Argos (`cuenta_bancaria.cambiada`) no entra en la bienvenida durante **24 h**, o hasta que la confirme un admin de Iris.
- **`origen` en §6.2** (opcional, solo para `bienvenida_servicio`): `{ "tipo": "iris_orden", "id": "<orden_id>" }`, cuando la activación vino de una orden de Iris (`origen_tipo`/`origen_id` de `activar-instalacion` o de `aprovisionar`); `null` (o ausente) si la activación fue desde el panel de Argos. Iris lo usa para mostrar el envío en la orden. Fuera de forma: `422 origen_invalido`.
- **Resultado de todos los canales:** webhook `servicio.bienvenida_cambiada` (§2.3), con el estado de cada canal (`whatsapp`, `correo`, `telegram`: `pendiente`, `enviada`, `fallida` u `omitida`, más `motivo`). En WhatsApp, Argos reenvía lo que Iris le informó con `aviso.entregado` / `aviso.fallido` (§6.6).
- **Reenviar:** `POST /api/v1/servicios/:id/bienvenida/reenviar` (`servicios.reenviar_bienvenida`), con `Idempotency-Key = iris:bienvenida:<orden_id>:<n>`, cuerpo `{ "origen_tipo": "iris_orden", "origen_id": "…", "canales": ["whatsapp", "correo", "telegram"] }` (`canales` opcional: sin él, todos los que Argos tenga configurados). Argos la vuelve a disparar por sus canales con las mismas reglas (permisos de contacto, límites y horario de silencio de §6.5). `202 { estado: "en_cola" }`. Errores: `409 servicio_no_activo`; `429 bienvenida_reciente` antes de 10 min del último envío; `429 demasiados_reenvios` después de **3 reenvíos por servicio en el día** (los dos con `Retry-After`).
- **Lo que hace Iris con eso** (informativo): bloque «Bienvenida» en la orden, botón «Reenviar bienvenida» si no salió o falló, bandeja «Bienvenidas sin enviar», y el ajuste por empresa o plantilla `cierre_exige_bienvenida` (apagado de fábrica). Con ese ajuste y el modo «exigir la activación lista», el cierre exige la bienvenida enviada por algún canal, o no enviada porque el cliente no acepta avisos (`sin_permiso`, `baja`).
