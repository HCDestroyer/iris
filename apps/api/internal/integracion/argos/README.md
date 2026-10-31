# integracion/argos

Conector de **lectura** con ArgosISP (`.claude/integration/contratos/argosisp.md` §1–§2).
Escrituras en Argos y cola de `pendientes`: Fase 2.

## Qué hace

La conexión, los webhooks, el cliente HTTP y la protección contra SSRF son la base común de
`integracion/conector` (este módulo crea su `Nucleo`, le agrega Argos y publica también las rutas
comunes de conectores); aquí queda lo propio de Argos.

- **Enlace de conexión (§1.3), en los dos sentidos.** `POST /integraciones/argosisp/conectar
  { enlace }` canjea un enlace de Argos (Iris llama a `POST /api/v1/integraciones/iris/conexion/canjear`
  de Argos con el cuerpo de Horus §1.4: `{ codigo, sistema: "iris", base_url, token, webhook: { url,
  secreto } }`, donde `token` es un `iris_pat_` nuevo del usuario de integración de Argos (§1.2,
  `identidad.EmitirTokenCanje`, 15 min hasta activar, solo a orígenes de `IRIS_ARGOS_ORIGENES`; ver `conector/README.md`); Argos responde `{ token: "atk_…",
  webhook, conector_id }`). `POST
  /integraciones/argosisp/conexion/enlaces` genera un enlace de Iris (15 min, un solo uso) que Argos
  canjea en `POST /integraciones/argosisp/conexion/canjear` (sin credenciales, autoriza el código).
  El token `atk_…` y el secreto del webhook se guardan cifrados (AES-256-GCM con
  `IRIS_CLAVE_CIFRADO`, atados a la fila) y nunca salen por la API ni en la auditoría (`***`).
- **Activar / desactivar** (`POST /integraciones/conectores/{id}/activar|desactivar`, con
  `version`): nace apagado; uno activo por sistema. Activar encola la carga de zonas y routers.
  El canje registra en `nucleo.empresas` las empresas que cubre el token, si las informa (Argos 1.3.0
  no las manda: se registran con `POST /api/v1/empresas`). Desde el contrato 0.3.1 el canje trae
  `alcance_global` y `empresas[] { id, nombre, nit }`: el alcance queda en el conector
  (`alcanceGlobal` en la API; ausente si Argos no lo informó) y las empresas se registran con su
  nombre comercial y NIT (`empresa.creada`, auditada); de una existente de origen `argos` solo se
  completa el nombre que sigue siendo el marcador «Empresa <id>» y el NIT vacío
  (`empresa.actualizada` con `campos[]`, auditada); una de origen `manual` no se toca.
- **Argos 1.3.0** (prueba e2e, `infra/e2e/argos.sh`): las coordenadas llegan como texto `"lat,lng"`
  y routers y zonas traen `empresa_sugerida_id` (0 = sin empresa: lo usan todas). La carga de
  catálogos al activar tiene su propia clave de unicidad (`motivo: activacion`).
- **Cliente HTTP** (`cliente.go`, sobre `conector.Cliente`): timeout por llamada (`IRIS_ARGOS_TIMEOUT`), 3 intentos con
  espera exponencial en lecturas ante red, 429 y 5xx, sin seguir redirecciones, problem+json
  mapeado (`Problema`): 404 → 404, 401/403 → 502 `argos_sin_permiso`, caída → 503
  `argos_no_disponible`, otro 4xx → 502 `argos_rechazo` (resumen con status y `type`, nunca el
  `detail` crudo). Lee JSON en snake_case o camelCase, envuelto o no en `{ data }`.
- **Resolución de IP interna** (`resolver_interno.go`): `ResolverIPInterno(ctx, ip, empresa)` para
  otros jobs (hallazgos de Horus), sin HTTP ni permisos; el motivo cuando no resuelve
  (`conector_argos_apagado`, `ip_sin_cliente`, `argos_no_disponible`, `otra_empresa`…).
- **SSRF** (`conector/red.go`): todo el tráfico hacia Argos pasa por un `net.Dialer` cuyo `Control`
  bloquea la IP ya resuelta si es loopback, privada, de enlace local, CGNAT, no especificada,
  multicast u otra reservada (422 `destino_no_permitido`), salvo `IRIS_ARGOS_REDES_PERMITIDAS`.
  Sin proxy del entorno.
- **Lecturas** (`GET /clientes?q=`, `GET /clientes/{id}`, `GET /clientes/resolver-ip?ip=`),
  permiso `clientes.ver` (+ `clientes.estado_cuenta` para el estado de cuenta). Auditadas
  (`clientes.buscar`, `clientes.ver`, `clientes.resolver_ip`). Sin conector activo: 503
  `conector_apagado`. Sin transacción abierta durante la llamada HTTP (dos casos de uso cortos).
  Filtradas por la **empresa activa**: lo de otra empresa (o sin empresa) se descarta o es 404, y
  no se pide el estado de cuenta de un cliente ajeno. Sin empresas registradas: 422
  `empresas_no_registradas`.
- **Búsqueda con servicios** (`GET /clientes?q=&servicios=true`, selector de cliente del panel):
  exige `q` de 3 caracteres o más (422) y solo los primeros 8 resultados
  (`TopeServiciosBusqueda`) traen `servicios` (los de la empresa activa; sin zona, heredan la del
  cliente; con el convertidor único `AServicio`). Se leen en Argos con los cupos por conector y una
  caché de 5 min por cliente compartida con la verificación de empresas (solo se pide lo que no
  está en caché). Cada lectura (también la verificación de clientes desconocidos de la búsqueda)
  se cobra a la cuota de verificación de `refrescos.go` (usuario, conector y empresa) y respeta la
  caché negativa y `fallo_en`; agotada la cuota, el resto queda sin leer. Si Argos no responde, de
  la instantánea. `servicios: null` = no se leyeron. Es solo informativo: el panel
  toma el servicio elegido de la ficha (`GET /clientes/{id}`, que guarda la instantánea). Sin el
  parámetro no hay llamadas extra.
- **Instantáneas** (`argos_clientes`, `argos_servicios`, `argos_zonas`, `argos_routers`): solo lo
  que se muestra, con `empresa_id` y `sincronizado_en`. La ficha se refresca si tiene más de 24 h;
  si Argos no responde se devuelve la última con `stale: true` (y la búsqueda cae a las
  instantáneas locales). El estado de cuenta nunca se guarda.
- **Webhooks** (`POST /integraciones/argosisp/webhook/{conector_id}`): firma HMAC-SHA256
  (`X-Webhook-Signature: v1=<hex>` sobre `"<timestamp>.<cuerpo>"`) verificada antes que la
  ventana `IRIS_WEBHOOK_VENTANA`; guardado en `integracion.entrantes` deduplicado por el `id` del
  cuerpo (firmado, obligatorio e igual a `X-Webhook-Id`) y job de River en la misma transacción.
  `id` texto o número; `ocurrido_en` no RFC 3339 → hora de recepción. El job
  (`ProcesarEntrante`) es idempotente. Los 401 anónimos (y los del canje) no se auditan: se
  cuentan (`RechazosSinAuditar`) y van al log con muestreo.

## Contrato 0.3.1

- **`cliente.actualizado` con `campos[]`** (`nombre`, `tipo`, `codigo`, `nit_cui`,
  `tipo_documento_fiscal`, `telefono`, `movil`, `direccion`, `coordenadas`, `zona_id`): refresca
  la instantánea con cualquier campo: desde los datos coherentes la instantánea guarda también
  `tipo`, `nit_cui`, `tipo_documento_fiscal` y `movil` (antes esos se omitían con
  `campos_sin_efecto`).
- **`servicio.eliminado` y `router.eliminado`** (`eliminados.go`). La lápida es difícil de
  deshacer, así que no se cree al payload:
  - el id sale solo de `servicio_id` / `router_id` (sin ellos, «ignorado», `sin_id`);
  - si la instantánea tiene la fila, su empresa y su cliente mandan; si el payload no coincide se
    anota la discrepancia (`empresa_no_coincide`, `cliente_no_coincide`) y la eliminación se
    aplica igual con los datos de la instantánea (lápida `de_instantanea`);
  - Iris confirma con `GET /servicios/:id` o `GET /routers/:id`: solo un 404
    `application/problem+json` con type `urn:argosisp:problema:no_encontrado` confirma
    (`conector.EsNoEncontradoConfirmado`; no se reintenta). Otro 404 (HTML de un proxy, ruta
    inexistente) o el 403 `empresa_no_permitida` (existe fuera del alcance; Argos nunca responde
    404 en ese caso) son errores y el job se reintenta. Si Argos aún lo devuelve, «ignorado»
    (`no_confirmado`);
  - **no** se filtra por el alcance que informó el canje: es una foto (Argos pudo ampliarlo
    después) y Argos solo confirma dentro del alcance real del token;
  - candados `pg_advisory_xact_lock` por (tipo, id), tomados todos al principio y en orden de id
    (`bloquear`): la eliminación con los de la ficha de su cliente, `guardarFicha` antes de tocar
    ninguna fila, la carga de catálogos antes de guardar routers. Sin interbloqueos;
  - confirmado, en una transacción: lápida en `integracion.argos_eliminados` (última copia,
    `de_instantanea`, `conector_id`, `eliminado_en` de Argos y `confirmado_en` = `clock_timestamp()`
    de Postgres antes de pedir el 404), fuera de `argos_servicios` / `argos_routers`, evento interno
    y, del servicio, la ficha del cliente releída;
  - un evento atrasado del servicio (`servicio.*`, `asignacion.cambiada`) queda «ignorado»
    (`servicio_eliminado`) sin leer Argos. Una lista (ficha, catálogos) nunca guarda un id con
    lápida: lo confirma con un GET por id y solo un 200 pedido después de `confirmado_en` (los dos
    con el reloj de Postgres) lo **resucita**: borra la lápida, guarda la instantánea, anota
    `resucitado_<tipo>_<id>` en la auditoría, deja un log de advertencia y emite
    `argos.servicio_restaurado` / `argos.router_restaurado` con la empresa del servicio o router
    (`Tx.EmitirEnEmpresa`: la acción auditada y el entrante conservan la suya). Ese GET va con un
    solo intento y 5 s de tope. Una nueva confirmación sobre una lápida existente la pone al día
    (`confirmado_en` = la más reciente, `GREATEST`), así que un GET 200 anterior a ella no
    resucita; el evento interno sale solo la primera vez (`RETURNING (xmax = 0)`);
  - un cliente cuyos servicios se eliminaron no queda «sin servicios» (visible en todas las
    empresas): las empresas de sus lápidas `de_instantanea` son la pista (`empresasYPista`); una
    lápida armada solo con el payload no cuenta.
- **«Eliminado en Argos»:** la ficha (`GET /clientes/{id}`) trae `serviciosEliminados` (filtrados
  por la empresa activa como `servicios`); un ticket u orden cuyo `servicioId` esté ahí lo muestra
  eliminado. Para otros módulos: `argos.ServiciosEliminados(ctx, q, ids, empresa)` (empresa
  obligatoria) o, solo para jobs del sistema, `argos.ServiciosEliminadosTodasLasEmpresas`. Órdenes y
  tickets conservan su propia copia (dirección, coordenadas, contacto).
- **Routers (§2.1):** `coordenadas` `"lat,lng"` o null → `lat`/`lon` de `argos_routers`, que usa
  la factibilidad (`RoutersConCoordenadas`; sin coordenadas no cuenta).
- **Pendiente:** si `asignacion.cambiada` (ips vacías) llega antes que `servicio.eliminado`, la
  ficha ya lo quitó como ausente y la lápida queda sin código ni plan.

## Eventos

- **Emite:** `conector.creado`, `conector.activado`, `conector.desactivado`,
  `argos.servicio_retirado` (gancho para la orden de retiro de I2; solo si Argos confirma el
  estado al releer `GET /servicios/:id`), `argos.servicio_eliminado` y `argos.router_eliminado`
  (contrato 0.3.1, confirmados con el 404), `argos.servicio_restaurado` y `argos.router_restaurado`.
  Detalle en
  `.claude/integration/contratos/eventos.md`.
- **Consume (webhooks de Argos, §2.3):** `cliente.actualizado`, `servicio.creado`,
  `servicio.config_cambiada`, `asignacion.cambiada` y `servicio.estado_cambiado` → refrescan la
  instantánea del cliente si ya está en ella (si no, «ignorado»); `servicio.eliminado` y
  `router.eliminado` (arriba). Los demás se guardan como «ignorado» (Fase 2).

## Jobs (cmd/worker)

`integracion.argos_procesar_entrante` (uno por entrante, hasta 12 intentos) e
`integracion.argos_sincronizar_catalogos` (cada 6 h y al activar).

## Empresa del cliente

En Argos el cliente no tiene empresa: la tienen sus servicios (`multiempresa.md` de Argos). La
instantánea guarda `empresas` (las de sus servicios) y `sin_servicios`; `empresa_id` queda como la
primera.

- **Servicios:** se leen con `GET /servicios?cliente_id=&empresas=todas`, el consolidado de todas
  las empresas. Sin `todas`, Argos filtra por el alcance del token; si Argos no da el consolidado
  (403: el token no tiene alcance global), se leen los del alcance y la lista no es concluyente.
- **Visible en una empresa:** si alguno de sus servicios es de ella, o si no tiene servicios. «Sin
  servicios» solo cuenta si lo dice la lista consolidada (una vacía por alcance deja al cliente
  fuera). A un cliente sin servicios lo ve cualquier empresa (paridad con Argos), pero su estado de
  cuenta y su acceso al portal solo los obtiene quien tiene `empresas.todas` global.
- **Alta desde Iris:** un cliente recién creado desde un prospecto ganado, aún sin servicio, tiene
  como pista la empresa del prospecto (el pendiente `crear_cliente`): solo se ve en ella.
- **Ficha:** solo muestra los servicios de la empresa activa (con `empresas.todas`, todos) y pide el
  estado de cuenta de esa empresa (`?empresa_id=` y `X-Empresa-Id`). Un servicio sin empresa no hereda
  la del cliente y no se atribuye a nadie (resolver IP lo descarta).
- **Búsqueda:** la empresa activa va en `X-Empresa-Id`. Los clientes que Iris no conoce (ni en la
  instantánea ni en una caché de 5 min) se verifican en Argos: a lo sumo 20 por búsqueda (los demás
  quedan fuera) y 10 a la vez por conector; falla cerrado.

## Datos coherentes (pedido 5 del PM)

Lo que Iris muestra del cliente y del servicio coincide con Argos 1.4.0 y se ve igual en todas
partes (ficha, ticket, orden, comercial y app de campo).

- **Instantánea completa** (migración `integracion_argos_datos_coherentes`): del cliente, además,
  `tipo`, `tipo_documento_fiscal`, `nit_cui` (sin guiones, como Argos), `movil`, `correo` (`email`
  de Argos) y `zona_nombre` (Argos lo manda en cada servicio; sin servicios, del catálogo de zonas);
  del servicio, `user_ppp` y `tipo_conexion`. La IP sale de `ipv4` (Argos 1.4.0 no manda `ip`:
  antes quedaba vacía y la resolución local por IP nunca acertaba). En Argos el servicio no tiene
  `zona_id`: toma la de su cliente.
- **Estado del cliente:** Argos no tiene estado de cliente. `cliente.estado` es el resumen de sus
  servicios visibles (el más grave: suspendido > limitado > activo > pendiente_instalacion >
  retirado), el mismo criterio que Argos en `GET /clientes/:id/dependientes`.
- **Formato para mostrar** (`formato/`, funciones puras, solo espacios y tabuladores ASCII y mayúsculas a–z para dar lo mismo que su espejo en
  `apps/web/lib/datos/telefono.ts`; los dos prueban `formato/testdata/telefonos.json`):
  - teléfonos: un campo de Argos puede traer varios números («5512-3344 / 2233-4455»); cada uno sale
    en `telefonos[]` con `original`, `e164` (+50255123344, null si no se pudo interpretar) y
    `mostrar` (+502 5512 3344; el original si es de otro país o no se interpreta);
  - documento: `nitCuiMostrar` (NIT 1234567-8, CUI 1234 56789 0101, CF);
  - coordenadas: 6 decimales y el 0,0 como «sin coordenadas».
- **Dirección del cliente ≠ dirección de instalación:** `cliente.direccion` es el domicilio;
  `servicio.direccion`, la de instalación (vacía si Argos no tiene una propia: entonces aplica la del
  cliente; Iris no la inventa).
- **Copias en tickets y órdenes** (`copias.go`, `integracion.argos_copias`, solo inserción): al
  crear un ticket o una orden con cliente de Argos, el caso de uso que lo crea guarda en su
  transacción **solo lo operativo** (nombre, código, dirección del cliente y de instalación,
  teléfono principal, servicio, plan, IP, router, zona; nunca documento fiscal, correo ni
  empresas) con la empresa de la entidad (`CopiarDatos`). No se copia un cliente que la instantánea
  pone en otra empresa, y al leer (`CopiaDe`) se descarta si la empresa ya no coincide. El detalle
  (`GET /tickets/{id}`, `GET /ordenes/{id}`) la devuelve en `datosArgos` solo a quien tiene
  `clientes.ver`. Si Argos elimina al cliente (404 al leerlo o webhook), sus copias se anonimizan
  (solo ids, `anonimizada: true`). Una copia de una fila anterior a los datos coherentes va con
  `completa: false`. Tickets y órdenes anteriores a la migración no tienen copia.
- **Cliente y servicio de la empresa** (revisión de seguridad): `ValidarClienteEnEmpresa` en
  `crearTicket` (panel, API, portal, correo, Horus) y `CrearOrden`: con instantánea, el cliente
  debe ser de la empresa (`enEmpresa`) y el servicio, de ese cliente y de esa empresa (422
  `empresa_no_coincide` o 422 `servicioId`). Horus con un cliente de otra empresa abre el ticket
  sin cliente (`otra_empresa`). Antes de la transacción, `ConInstantanea` (envoltura de
  `CrearTicket` y `CrearOrden` para usuarios, tokens e integraciones con el permiso) llama a
  `AsegurarInstantanea`: valida primero con la instantánea y solo relee de Argos si no basta y
  tiene más de 30 s, con la protección de la ficha (singleflight, cuota por usuario y conector →
  429 `cuota_argos`, espera tras una caída). Lo leído se valida antes de guardarlo (un cliente de
  otra empresa no entra a la instantánea; auditado como `clientes.sincronizar`); un 404 borra la
  instantánea y anonimiza las copias. Con Argos caído y un servicio pedido, 503
  `argos_no_disponible`. Cliente inexistente, de otra empresa o servicio ajeno dan el mismo 422
  `empresa_no_coincide` (sin oráculo de enumeración). El sistema (retiro automático, alta
  comercial) no consulta.
- **Convertidor único:** `AServicio(ServicioArgos, zonaCliente)` → `Servicio` (instantánea y
  lecturas en vivo).
- **Protección de Argos** (`refrescos.go`): una sola lectura de ficha en curso por (conector,
  cliente) (`singleflight`); solo el líder, el que llama a Argos, cobra. Toda lectura que llama a
  Argos cobra la cuota del usuario (30/min), también las de instantáneas vencidas o inexistentes.
  Las lecturas a pedido cobran además una cuota propia según el uso, global por conector y por
  (conector, empresa): «Refrescar» 60/20 por minuto y verificación de altas 300/100 (agotar una no
  bloquea la otra ni a otras empresas). Pasada una cuota: «Refrescar» devuelve la instantánea con
  `refrescoOmitido: cuota`, una vencida se sirve `stale` y sin instantánea es 429 `cuota_argos`.
  Tras una caída con instantánea (`fallo_en`), 1 min sin volver a llamar; sin instantánea, un 404
  (10 s) o una caída (1 min) quedan en una caché negativa en memoria que respetan la ficha,
  «Refrescar» y la verificación, y que se descarta si desde entonces hubo noticia del cliente en la
  base (webhook `cliente.creado`/`cliente.actualizado` procesado o pendiente `crear_cliente` hecho,
  en cualquier proceso). Un usuario tiene además 5 «Refrescar» por minuto, para no agotar los de su
  empresa. Las ventanas de cuota vencidas se desalojan (nunca las vigentes). Con
  `?refrescar=true`, `refrescoOmitido` dice por qué no se llamó (`cuota`, `reciente`,
  `en_espera`). «Refrescar» de un cliente que la instantánea pone en otra empresa no llama a
  Argos (404).
- **Auditoría de la búsqueda:** una búsqueda con 4 dígitos o más (teléfono, NIT, CUI) queda con los
  dígitos ocultos salvo los dos últimos (`****-**44`).
- **Resolución local por IP:** ignora servicios retirados.
- **Refresco:** por webhook (`cliente.actualizado` con cualquier `campos[]`,
  `servicio.creado`, `servicio.config_cambiada`, `asignacion.cambiada`,
  `servicio.estado_cambiado`), al abrir la ficha si tiene más de 24 h o es de una versión anterior
  de los datos (`version_datos` < `VersionDatos`) y con «Refrescar» (`GET /clientes/{id}?refrescar=true`,
  que no vuelve a pedir a Argos si la instantánea es de hace menos de 30 s). Argos 1.4.0 no avisa
  cambios de la dirección o las coordenadas de un servicio ni del correo del cliente: hasta que lo
  haga (pedidos en `.claude/integration/i1.md`), se ven al vencer las 24 h o con «Refrescar».

## Contrato 0.5.0 (Argos ≥ 1.6.0): inventario del técnico

- **Versión y contrato** (`contrato.go`). Iris manda `contrato: "0.5.0"` en el canje (enlace §1.3
  y botón §1.4) y guarda `version_argos` / `contrato` en el conector (`version_remota`,
  `contrato_remoto`, `contrato_informado`, `version_consultada_en`). Al activar (y al reconectar)
  se encola `integracion.argos_informar_contrato`: `PUT /integraciones/iris/contrato` y
  `GET /integraciones/iris/estado`; un 404/405 es Argos 1.5.x (no es error). El periódico
  `integracion.argos_revisar_contrato` (al arrancar y cada 6 h) lo encola para el conector activo
  si falta informar el contrato o la versión es desconocida o < 1.6.0: es el job «de una vez» de
  los conectores ya activos y detecta una actualización de Argos. `Conector.capacidades` trae
  `inventario_por_tecnico` con Argos ≥ 1.6.0; `InventarioPorTecnico` lo consulta la cola.
- **Técnicos de Argos** (`tecnicos.go`). `GET /api/v1/integraciones/argosisp/tecnicos`
  (`agenda.vincular_argos`) es el proxy paginado de `GET /inventario/tecnicos` para el selector
  del vínculo; marca `enEmpresa` y `vinculadoA`. `BuscarTecnico` (lo usa agenda al vincular)
  recorre las páginas. `IDExterno` acepta texto o número y se guarda como texto.
- **Instantánea**: `referencia` del cliente; `referencia`, `contacto_nombre` y
  `contacto_telefono` del servicio (`contactoTelefonos` normalizados en la API).
- **Webhooks** (`webhook_050.go`):
  - `servicio.datos_cambiados` refresca la ficha (como `servicio.config_cambiada`) y emite
    `argos.datos_cambiados` (campos con prefijo `servicio.`); `cliente.actualizado` con campos que
    copian las órdenes (dirección, coordenadas, referencia, contacto, teléfonos, nombre) también
    (`cliente.`). Campo marca sus órdenes abiertas sin pisarlas.
  - `catalogo.cambiado`: relee el elemento (zona en `GET /zonas`, router con `GET /routers/:id`,
    plan en `GET /planes`) y actualiza `argos_zonas` / `argos_routers` (`activo` nuevo), los
    nombres copiados en servicios y clientes y la zona espejo de agenda (`nombre`, `activa`). Si ya
    no aparece y el aviso es de `activo`, queda inactivo.
  - `inventario.traslado_enviado|recibido|cancelado`: modelo de lectura
    `integracion.argos_traslados` (un estado final no vuelve a `enviado`) y `argos.traslado_cambiado`
    con el técnico de Iris del vínculo (`usuario_id`).
- **Errores** del inventario por técnico: 422 `argos_sin_inventario_tecnico` (Argos < 1.6.0),
  `tecnico_no_encontrado`, `tecnico_sin_bodega`.
