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
  secreto }, contrato, receptor_argos: true }`, donde `token` es un `iris_pat_` nuevo del usuario de integración de Argos (§1.2,
  `identidad.EmitirTokenCanje`, 15 min hasta activar, solo a orígenes de `IRIS_ARGOS_ORIGENES`; ver `conector/README.md`); Argos responde `{ token: "atk_…",
  webhook, conector_id }`). El receptor `webhook: { url, secreto }` de Argos es obligatorio desde
  el contrato 0.6.2: se guarda (`webhook_salida_url` y el secreto cifrado) para los webhooks de la
  mensajería (§6.6); si falta, su host no es el de la base del canje (el `<base>` del enlace o
  `api=` del botón, nunca el panel) o es inválido, el canje es `422 webhook_invalido` y el conector
  no se guarda. `receptor_argos` va siempre `true` en el canje: la petición sale antes de conocer
  el receptor (llega en la respuesta), pero esta versión de Iris lo exige. `POST
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
  (contrato 0.3.1, confirmados con el 404), `argos.servicio_restaurado` y `argos.router_restaurado`;
  `argos.comprobante_resuelto` y `usuario.argos_abierto` (contrato 0.7.0);
  `argos.sincronizacion_iniciada`, `argos.sincronizacion_terminada` y `argos.certificado_cambiado`
  (contrato 0.8.0, abajo).
  Detalle en
  `.claude/integration/contratos/eventos.md`.
- **Consume (webhooks de Argos, §2.3):** `cliente.actualizado`, `servicio.creado`,
  `servicio.config_cambiada`, `asignacion.cambiada` y `servicio.estado_cambiado` → refrescan la
  instantánea del cliente si ya está en ella (si no, «ignorado»); `servicio.eliminado` y
  `router.eliminado` (arriba); `comprobante.aprobado` y `comprobante.rechazado` →
  `argos.comprobante_resuelto`; `promesa.creada`, `promesa.cumplida`, `promesa.incumplida` y
  `promesa.cancelada` → `argos.promesa_cambiada` (con `motivo_reconexion` de 0.8.0, sin monto). Los demás se
  guardan como «ignorado» (Fase 2).
- **Reconexión `sin_personal` (0.8.0 §7.3):** `SoportaSinPersonal(contrato)` y
  `Modulo.ReconexionSinPersonal` (conector activo con contrato ≥ 0.8.0); `PromesaArgos.MotivoReconexion`.

## Jobs (cmd/worker)

`integracion.argos_procesar_entrante` (uno por entrante, hasta 12 intentos) e
`integracion.argos_sincronizar_catalogos` (cada 6 h y al activar),
`integracion.argos_primera_sincronizacion` (al activar un conector que nunca se sincronizó o con
«Sincronizar»; único por conector mientras no termina, 12 intentos) e
`integracion.argos_informar_sincronizacion` (uno por cambio de etapa o de estado).

## Primera sincronización y estado de la conexión (contrato 0.8.0 §1.5 y §2.5, B7)

- **Cuándo:** al activar un conector que nunca tuvo una (`AlActivar`), siempre después de
  `PUT /integraciones/iris/contrato` (el job lo llama primero: confirma el canje y relee el contrato).
- **Qué lee y cómo** (`sincronizacion.go`): empresas (del canje), zonas, planes (solo se cuentan),
  routers, cuentas bancarias, bancos, clientes y servicios, una petición a la vez, `limit=200` y como
  máximo 5 por segundo (`RitmoSincronizacion`); 429 con `Retry-After` se espera. Progreso por
  recurso en `integracion.argos_sincronizacion_etapas` (estado, procesados, omitidos, último cursor,
  error de hasta 300 caracteres sin datos de clientes). Un corte deja la etapa en curso y River
  reintenta desde el cursor; en el último intento queda `con_errores` y se reanuda a mano.
- **Instantáneas:** clientes con `GuardarClienteLista` (una fila nueva nace sin empresas: nadie la ve
  hasta que llegan sus servicios), servicios por la lista blanca de `AServicio`, sin revivir lápidas,
  solo si su cliente está en la instantánea; las empresas del cliente se recalculan de sus servicios.
- **Con Argos ≥ 0.8.0** (`contrato_remoto`): `excluir_retirados=true`, cuentas y bancos sin cabeceras
  de Iris (`FijarCargasBancarias` deja enchufar la instantánea de B5; sin ella solo se cuentan) e
  informe con `PUT /integraciones/iris/sincronizacion` (409 `sincronizacion_vieja` → `descartado`,
  sin reintento; 5xx → reintento). Con uno anterior: listas completas, cuentas y bancos
  `con_errores` «Argos anterior a 0.8.0» y sin informe.
- **Acciones:** `GET /integraciones/argosisp/estado` (`integraciones.ver`, pantalla de estado) y
  `POST /integraciones/conectores/{id}/sincronizar` (`integraciones.sincronizar`: reanuda la en
  curso, reabre la `con_errores` con su mismo id o lanza otra).
- **Certificado TLS de Argos:** `conector.ConCapturaTLS` anota el certificado hoja en
  `InformarContrato` (al activar y cada 6 h); `integraciones.registrar_certificado` lo guarda en
  `integracion.argos_certificados` y, si cambió sin reconectar, emite `argos.certificado_cambiado`
  (campana `argos_certificado` a quien tiene `integraciones.conectar`). Nunca bloquea ni marca el
  conector. Un certificado que vuelve después de otro (A → B → A) también cuenta como cambio. Ojo: si
  Argos alterna entre dos certificados (dos réplicas detrás de un balanceador, o una renovación a
  medias), cada alternancia avisa; la clave del aviso (huella anterior y nueva) lo acota a un aviso por
  par, no por llamada. Si pasa, revisa que todas las réplicas de Argos sirvan el mismo certificado.
- **Defensas** (revisión de seguridad): zonas, routers y servicios de una empresa fuera del alcance
  del canje (`empresaEnAlcance`) no se guardan y cuentan como omitidos; un cursor de más de 512
  caracteres o una página con más filas que `limit` es una respuesta ilegible; volver a lanzar una
  sincronización que terminó completa hace menos de 10 minutos responde 409 `sincronizacion_reciente`
  (salvo que Argos haya cambiado de contrato desde entonces). Un servicio sin empresa (0 o null) también
  se omite.
- **Qué ve quien abre el estado:** `GET …/estado` exige `integraciones.ver` y es de la instalación,
  no de la empresa activa: un supervisor acotado a una empresa ve los nombres de todas las empresas del
  alcance del conector y los conteos globales de las instantáneas (clientes, servicios, zonas,
  routers) y de los pendientes. Nunca datos de clientes, tokens ni secretos.
- **Vía de recuperación** (S5, `nucleo/identidad/crear_token.go`): el token a mano del usuario de
  integración de un conector exige `integraciones.conectar` y `empresas.todas` sin límite, vence
  (30 días por defecto, 90 como máximo), queda marcado (`tokens_api.via = 'recuperacion'`, auditoría
  `via: recuperacion`) y se revoca al confirmarse un canje nuevo del mismo sistema (con los `cli` de
  `iris integracion crear`, de todos los usuarios con el rol de ese sistema; ver `nucleo/README.md`).

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
  `GET /integraciones/iris/estado`; un 404/405 es Argos 1.5.x (no es error): la versión y el contrato
  guardados se borran (una vuelta atrás de Argos deja a Iris en `bodega_id`). El periódico
  `integracion.argos_revisar_contrato` (al arrancar y cada 6 h) lo encola siempre para el conector
  activo: es el job «de una vez» de los conectores ya activos y detecta una actualización o una
  vuelta atrás de Argos. `Conector.capacidades` trae
  `inventario_por_tecnico` con Argos ≥ 1.6.0; `InventarioPorTecnico` lo consulta la cola.
- **Diagnóstico de la versión (2.1).** Cada corrida de `informar_contrato` guarda en el conector
  `version_estado` (`ok`, `ruta_no_existe`, `sin_permiso`, `no_responde`, `error`), `version_detalle`
  (español, con la URL base usada y el status HTTP, sin secretos) y `version_consultada_en`
  (`Conector.versionEstado` / `versionDetalle`). Un 404/405 en HTML (`ErrorRemoto.HTML`: la URL del
  panel de Nuxt en vez de la de la API) es `ruta_no_existe` con «La dirección <base> no responde como
  la API de Argos: revisa que sea la de la API, no la del panel» y **no borra** una versión ≥ 1.6.0
  guardada; un 401/403 en el PUT ya no corta el job: se lee `GET …/estado` y queda `sin_permiso`; la
  vuelta atrás real (404 problem+json o 405) borra la versión como antes; Argos caído deja
  `no_responde` y River reintenta. Un cambio de `version_estado` emite `conector.actualizado`.
  `POST /integraciones/conectores/{id}/revisar-version` (`integraciones.revisar_version`, solo admin,
  `Idempotency-Key`) encola el job enseguida y responde 202 con el conector (`revisar_version.go`).
- **Contrato 0.6.2.** `ContratoIris = "0.6.2"`. El PUT manda `{ contrato, receptor_argos }`, con
  `receptor_argos: true` si el conector tiene guardados la URL y el secreto del receptor de Argos y
  `false` si no (conectores canjeados antes de B1: Argos marca `requiere_reconectar` y la mensajería
  deja sus webhooks `omitido`).
- **Contrato 0.6.3.** `ContratoIris = "0.6.3"`.
  - **Canje no completado:** si Argos respondió 200 al canje y Iris no lo completa, el job
    `integracion.revocar_canje_remoto` llama a `POST /integraciones/iris/conexion/revocar` con ese
    `atk_` (detalle en `conector/README.md`; Horus no tiene esa ruta y sigue igual).
  - **Reconexión sin corte:** Argos deja vivo el `atk_` anterior hasta la primera llamada autenticada
    con el nuevo. En Iris el canje nuevo crea otro conector apagado y el anterior sigue activo con su
    `atk_`. La activación los cambia en una sola transacción y encola el PUT del contrato, sin llamar a
    Argos. Toda llamada sale del conector activo, así que la primera con el `atk_` nuevo ocurre con el
    conector nuevo ya activo (`TestReconexionSinCorte`). Esa primera llamada es el PUT del contrato o la
    carga de catálogos, porque la activación encola los dos, o una llamada que ya estaba en curso
    (pendientes, panel). El contrato acepta cualquiera.
  - **El `iris_pat_` anterior no se corta:** al activar no se revoca. Argos sigue leyendo de Iris con
    él y sus webhooks firmados con el secreto anterior se aceptan (se guardan en el conector nuevo).
    Se revoca con la primera respuesta 2xx de una llamada con el `atk_` nuevo, o a las 24 h (job), o
    al desactivar el nuevo (`conector/README.md`, `reconexion.go`).
- **Contrato 0.6.4.** `ContratoIris = "0.6.4"` (A22): cada elemento de `bodegas[]` de
  `GET /inventario/tecnicos` trae `nombre`. Iris lo pasa como `bodegas[].nombre` en su selector; con
  un Argos anterior llega vacío y el panel muestra «Bodega <id>». Es compatible: nada más cambia.
- **Contrato 0.7.0** (`ContratoIris = "0.7.0"`, pedido B3): ver «Contrato 0.7.0» abajo.
- **Técnicos de Argos** (`tecnicos.go`). `GET /api/v1/integraciones/argosisp/tecnicos`
  (`agenda.vincular_argos`) es el proxy paginado de `GET /inventario/tecnicos` para el selector
  del vínculo; marca `enEmpresa` y `vinculadoA`. Con `?sinVincular=true` salta los que ya tiene
  una ficha (de cualquier empresa: el vínculo es único) y lee de Argos hasta 10 páginas para llenar
  `limit`; si una página falla después de leer otras, entrega lo leído con esa página como
  `nextCursor`. Lo usan el formulario del técnico (selector de bodega por nombre) y la vista
  «Técnicos de Argos sin ficha». Si el conector está apagado: 503 `conector_apagado`; si Argos no
  responde: 502/503 del conector (la UI muestra «Reintentar» y deja el número de bodega a mano solo
  en ese caso). `BuscarTecnico` (lo usa agenda al vincular)
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

## Contrato 0.7.0: aprobar comprobantes desde Iris y autologin Iris → Argos (pedido B3)

- **Capacidad** (`contrato.go`): `Conector.capacidades` trae `aprobar_comprobantes` cuando el conector
  informa contrato ≥ 0.7.0 (`SoportaAprobarComprobantes`, `ApruebaComprobantes`). Sin ella, aprobar,
  rechazar y abrir Argos responden 422 `argos_sin_aprobacion` sin llamar a Argos.
- **Aprobar y rechazar** (`comprobantes.go`, §7.7): `AprobarComprobante`, `RechazarComprobante` y
  `ObtenerComprobante`, con el patrón de `AtencionIris` (`X-Iris-Actor: usuario:<id>`,
  `X-Iris-Conversacion`, `X-Empresa-Id`) e `Idempotency-Key` = `iris:aprobar:<comprobante_id>` /
  `iris:rechazar:<comprobante_id>`. El cuerpo es un struct (un reintento manda los mismos bytes).
  Un 409 `idempotencia_en_curso` se reintenta con espera corta (`EsperasEnCurso`). Son sincrónicas:
  nunca van por `integracion.pendientes`. La acción, los permisos y la traducción de errores viven en
  `internal/mensajeria` (`boletas_decidir.go`). `ErrorRemoto.Codigo` trae el `code` del problem+json
  de Argos (o el último segmento de su `type`). `Comprobante.VerificadoIrisActor` (contrato 0.7.1,
  opcional) es quien lo decidió desde Iris; `IrisActor` es quien registró la boleta.
- **Abrir Argos** (`abrir_argos.go`, §1.3): `POST /api/v1/integraciones/argosisp/abrir
  { destino?: { tipo, idExterno } }` (`integraciones.abrir_argos`; solo una persona con sesión del
  panel: 403 `solo_personas`). Pide el ticket a `POST /integraciones/iris/sso/tickets` con el token del
  conector y `usuario.id_externo` = el id del usuario de Iris en texto (el mismo de `X-Iris-Actor`);
  `correo` y `nombre` son informativos. Devuelve `{ url, expiraEn }` solo si la URL es
  `http(s)://<host>/sso/entrar#<ticket>` con el host de `conector.URLPublicaDe` (URL pública a mano,
  `panel_url` o `base_url`: lo mismo que entrega `GET /integraciones/enlaces`, que acepta
  `clientes.ver` o `integraciones.abrir_argos`); nunca un host libre. Si no, 502 `argos_rechazo`. Sin `destino` es «Vincular mi usuario de Argos». La URL lleva el ticket: en la
  auditoría va `url: "***"`, el evento `usuario.argos_abierto` no la lleva y la respuesta se marca
  `NoRepetible` (no queda en `nucleo.idempotencia`). Tope: 10 por minuto por persona
  (`TopeAbrirArgos`, contado en `integracion.abrir_argos_cuotas`: vale con varias réplicas): 429
  `demasiados_intentos`. En una sesión abierta por autologin exige re-autenticación
  (`auditoria.RequiereReautenticacion`).
  Errores: 409 `conector_apagado` e `iris_apagado`, 422 `argos_sin_aprobacion`, 502
  `argos_no_disponible`. Iris no guarda si el vínculo existe: solo Argos lo sabe.
- **Webhooks** (`webhook_070.go`): `comprobante.aprobado` y `comprobante.rechazado` (§7.2) emiten
  `argos.comprobante_resuelto` como **aviso**, con lo mínimo (comprobante, empresa, estado, canal,
  pago y entrante; sin monto, motivo ni causa). Mensajería marca la boleta y relee
  `GET /comprobantes/:id` antes de aplicar nada: lo que cambia el estado y lo que se le dice al cliente
  sale de esa lectura, nunca del payload.

## Contrato 0.8.1: lo que Iris informa

- `ContratoIris = "0.8.1"` desde que B4 (reconexión `sin_personal`), B5 (cuentas bancarias) y B7 (conexión de
  un paso) están en Iris; B6 y `buscar_red` (0.8.1 §2.1) ya estaban. Iris lo manda en el canje y en
  `PUT /integraciones/iris/contrato`. Cada función nueva sigue dependiendo del contrato que informó Argos
  (§4.1): con un Argos de contrato < 0.8.0 se comporta como en 0.7.1 y el conector queda en `ok`.

## Contrato 0.14.0: carga de vínculos y permiso de la verificación (§4.5, §6.6)

Detrás de la versión que informa Iris (`Config.ContratoIris`, por defecto `ContratoIris`): con 0.14.0
informado y aceptado por un Argos que lo habla, una vez por conector, `InformarContrato` suma
`adjuntos.verificar_integracion` al `iris_pat_` vigente (si tiene lista de permisos) y pide a agenda la carga
inicial de vínculos (`Config.Vinculos` / `UsarVinculos`). El job `integracion.argos_cerrar_carga_vinculos`
(cada minuto) cierra la carga con `PUT /integraciones/iris/contrato { contrato, receptor_argos,
vinculos_cargados }` cuando todos sus vinculados tuvieron el `2xx` (los omitidos no cuentan; uno agotado la
deja abierta). Columnas: `integracion.conectores.vinculos_carga_id`, `vinculos_carga_n`,
`vinculos_cargados_en`. Con un vinculado de la carga `agotado`, la carga no se cierra y se emite
`argos.carga_vinculos_detenida` una vez por episodio (`vinculos_carga_avisada_en`; aviso de campana al
admin).

**Reconexión (A-1).** Al activar un conector de la misma instancia de Argos que otro
(`integracion.misma_instancia_argos`: `base_url` normalizada —host en minúsculas, sin punto final ni puerto
por defecto; se guarda así desde el canje, y la migración normalizó las ya guardadas; el receptor de
webhooks que manda Argos se compara y se guarda igual— y, si los dos lo traen, el mismo `remoto_id`), `migrarVinculos`
pasa al nuevo, en la transacción de la activación, los `agenda.tecnicos.argos_conector_id` y el
`conector_origen` de los webhooks `tecnico.argos_*` aún sin entregar. Lo entregado y la carga cerrada no se
heredan: el conector nuevo corre su carga inicial al informar 0.14.0 (reemite los vinculados con versión
mayor) y, hasta cerrarla, `depende_de` espera. Un evento que seguía en el outbox con el conector viejo se
programa hacia el activo de la misma instancia (mensajeria).

## Contrato 0.7.1 (A24)

- Iris informó `0.7.1` hasta la 0.8.1. Es aditivo: con un Argos 1.15.x (que solo conoce 0.7.0) todo sigue igual. Iris informa
  0.7.1, Argos responde 0.7.0, el conector queda en `versionEstado: ok` con sus capacidades y el panel solo
  muestra las dos versiones (`contratoInformado` y `contratoRemoto`). Ninguna capacidad depende de 0.7.1.
- `Comprobante.VerificadoIrisActor` (`verificado_iris_actor`): quién aprobó o rechazó desde Iris. `IrisActor` es
  quien registró. Con Argos 1.15.x no viene: queda vacío y la decisión no se atribuye a nadie de Iris.
- §7.1: `X-Iris-Actor` y `X-Iris-Conversacion` se validan con las expresiones del contrato antes de llamar
  (`ErrCabecerasIris`); lo que Iris arma (`usuario:<id>`, `asistente_ia`, `iris:conversacion:<id>`) cumple.
- §1.3: `vinculacion.instalacion` es del canje del ticket en el panel de Argos; Iris no manda ni recibe nada
  nuevo. Sigue mandando `usuario.correo` y `usuario.nombre` en el ticket para la pantalla de confirmación.


## Contrato 0.8.0, bloque C: datos del servicio con permisos (§2.4, B6)

- **Lista blanca.** `ServicioArgos` (`tipos.go`) es la lista escrita de lo que se lee de Argos: plan,
  zona, router, estado, `precio_mensual` y `moneda` (Argos ≥ 1.18.0), tipo de conexión, `user_ppp`,
  `ipv4`, `prefijo_enlace_v6`, `prefijo_delegado_v6`, `planta` (seis claves texto o null), dirección,
  referencia, coordenadas y contacto en sitio. `mac`, `ipv6_duid`, `ipv6_modo`, `local_address`,
  `servidor_pppoe_id`, `excepciones`, `costo_override`, `suspension_manual_motivo` y `externo` no
  tienen campo: el decodificador los descarta y nunca se guardan, registran ni salen por la API.
- **Precio.** `precio_mensual` (numeric) y `moneda` van juntos o son null (Argos anterior a 1.18.0: no
  se muestra ni se calcula; nunca «Q0»). Un precio NaN, infinito, negativo o fuera de numeric(12,2)
  queda null. Se refresca con la ficha (más de 24 h), con `servicio.config_cambiada` (cualquier
  `campos[]`, también `precio`) y con `catalogo.cambiado` de un plan con `precio` en `campos[]`, que
  encola `ArgsPreciosPlan` (job único por conector y plan): relee `GET /servicios?plan_id=`
  (paginado) y actualiza solo los servicios de ese plan que Iris ya tiene. «Lista demasiado larga»
  (más de 50 páginas) cancela el job sin reintentos.
- **Permisos (`visibilidad.go`).** `servicios.ver_red` (router, IP, prefijos, `user_ppp`, tipo de
  conexión, planta) y `servicios.ver_ubicacion` (dirección, referencia, coordenadas, contacto en
  sitio); todo exige además `clientes.ver`. Sin el permiso **el servidor omite** el campo
  (`omitempty`): ficha, búsqueda con servicios, copia de ticket u orden (`CopiaDe`: sin ubicación, la
  dirección que aplica vuelve a ser la del cliente), ficha de la conversación (`ServiciosVisibles`,
  sin servicios si falta `clientes.ver`; una conversación sin empresa, habiendo empresas, no trae al
  cliente), paquete y cambios de la app de campo, y `GET /clientes/resolver-ip` (exige
  `servicios.ver_red`; fuera de su alcance, 404 como una IP sin cliente). Un servicio muestra el dato
  si el permiso es global, o con alcance de la zona del servicio, o el servicio es de «sus órdenes»;
  y, si sus órdenes están acotadas (`ordenes.ver_propias`, u `ordenes.ver` con alcance de zona o
  cuadrilla, sin `ordenes.ver` global), solo los servicios de sus órdenes. «Sus órdenes»: de la
  empresa activa, abiertas o terminadas (cerrada, fallida) hace menos de 24 h, nunca canceladas;
  asignadas a él o en una zona o cuadrilla de su `ordenes.ver` (criterio de `campo/comun.go`). Una
  orden reasignada deja de ser suya. Plan, zona, estado y precio los ve cualquiera con `clientes.ver`.
- **Límite conocido de `servicios.ver_ubicacion`.** Quita la dirección y las coordenadas **del
  servicio**, pero un rol sin él puede seguir viendo una dirección por otros caminos que no son de
  este permiso: la dirección propia de la orden (su copia, con los permisos de la orden, §2.4) y la
  dirección del cliente (domicilio en la ficha del cliente, con `clientes.ver`). Si el servicio se
  instaló en el domicilio del cliente, esa dirección se ve. Es una decisión del contrato, no un
  descuido; cambiarla es otro pedido.
- **Búsqueda por IP.** `GET /clientes?q=` con una IP, un prefijo o un trozo de IP, sin
  `servicios.ver_red` global, responde lista vacía sin llamar a Argos (sería un oráculo de la red).
  **`user_ppp` y prefijos (contrato 0.8.1 §2.1, A29): resuelto con Argos ≥ 1.21.0.** Sin
  `servicios.ver_red` global (también con alcance solo por zona), `BuscarClientes` manda
  `buscar_red=false` siempre, sin depender de la versión de Argos (falla cerrado), y Argos ≥ 1.21.0
  deja fuera de `q` la IPv4 vigente, los prefijos IPv6 y el `user_ppp` de los servicios. La decisión
  es del caso de uso (`lecturas.go`); el cliente HTTP solo agrega el parámetro. Con
  `servicios.ver_red` global no se manda. Un Argos anterior (o que no informó versión) lo ignora:
  sigue el **límite conocido** de antes (un texto con el usuario PPPoE todavía puede encontrar al
  cliente). La auditoría de `clientes.buscar` anota `buscar_red`: `efectivo` (Argos ≥ 1.21.0 según
  `version_remota`, con `SoportaBuscarSinRed`), `ignorado` (Argos anterior o sin versión) o
  `no_enviado` (con `ver_red` global).
- **Conversaciones sin empresa.** Una conversación con `empresa_id` null creada antes de registrar la
  primera empresa no muestra al cliente (podría ser de otra empresa) hasta que se adopten sus datos
  con `empresas.adoptar`.
- **App de campo.** `packages/sync` aplica la misma ventana: una orden que sale del paquete, reasignada,
  cancelada o terminada hace más de 24 h pierde en el teléfono (IndexedDB o SQLite) el router, la IP,
  la dirección y las coordenadas del servicio, aunque siga guardada por su cola.
- **Roles.** Base: `ver_red` a admin, supervisor, soporte y técnico; `ver_ubicacion` a admin,
  supervisor, despacho, soporte, ventas y técnico (caja y lectura dejan de ver la ubicación; los
  roles de integración, ninguno). La migración `20261119024424` da los dos a los **roles propios**
  que tenían `clientes.ver`, una sola vez, con una fila `roles.permisos_agregados_por_migracion` en
  `auditoria.acciones` y un `rol.actualizado` (`permisos_agregados[]`) en el outbox por rol, en la
  misma transacción (el admin se los quita si quiere).
- **Auditoría.** La lectura (`clientes.ver`, `clientes.buscar`) anota `servicios_ver_red` y
  `servicios_ver_ubicacion` con los ids de los servicios cuyos datos salieron; nunca los valores.
- **Número del servicio.** `numero` (1, 2, 3…, por id, en la lista de la empresa activa): «Servicio 2»
  en el panel y en el asistente.
- No emite ni consume eventos nuevos (la migración emite `rol.actualizado`, que ya existe).

## Cuentas bancarias (contrato 0.8.0 §7.8, B5)

Argos es la única fuente de las cuentas donde depositan los clientes. Iris guarda una instantánea de
lectura (`integracion.argos_cuentas_bancarias`: `id`, `empresa_id`, `banco_id`, `banco_nombre`,
`numero`, `alias`, `titular`, `tipo`, `moneda`, `activa`, `sincronizado_en`; nunca los campos
contables) y nunca la edita (`cuentas_bancarias.go`).

- **Carga completa**: en la primera sincronización (paso `cuentas_bancarias`, enganchado con
  `FijarCargasBancarias(CargaCuentasBancarias, nil)`; los bancos solo se cuentan) y cada 6 h
  (`CargarCuentasBancarias`, job `ArgsSincronizarCuentas`, único por minuto y motivo). `GET /cuentas-bancarias` sin `activa` (activas e
  inactivas) y sin las cabeceras de §7.1, que son opcionales desde la 0.8.0. Con un Argos que
  informa un contrato anterior la lista exige cabeceras de una conversación: el job no la pide
  (`omitido: argos_anterior_080`). Lo que la lista no trae (fuera del alcance) se quita.
- **Al usarla** (`CuentasDeEmpresa`): si la última lista de la empresa (`argos_cuentas_lecturas`)
  tiene más de 24 h, la relee primero (con Argos < 0.8.0, con las cabeceras de la conversación y su
  empresa). Si Argos no responde y es vieja: `ErrCuentasViejas` y no se ofrecen.
- **Antes de registrar una boleta** (`ReleerCuentaBancaria`): `GET /cuentas-bancarias/:id`, que
  refresca la fila (un 403 `empresa_no_permitida` la quita). Una instantánea vieja nunca decide.
- **Webhook `cuenta_bancaria.cambiada`** (solo a conectores ≥ 0.8.0): guardado y deduplicado como
  los demás; el job relee `GET /cuentas-bancarias/:id` y guarda lo que dice Argos, no el payload.
  404 confirmado: omitido `cuenta_no_encontrada`; 403: se quita (`cuenta_fuera_de_alcance`); caído:
  el job se reintenta.
- `EsCuentaInactiva`: el 422 `validacion` con `rule: "inactiva"` de `POST /comprobantes`.
- **Emite** `argos.cuenta_bancaria_cambiada` (cuenta, empresa, `campos[]`, `activa`, `creada`,
  `entrante_id` o null): con el webhook siempre; con una relectura, solo si la cuenta cambió. Nunca
  el número ni el titular. **Consume** `cuenta_bancaria.cambiada` de Argos.

## Contrato 0.10.0: código y nombre del servicio (A45 / B24)

- La instantánea guarda `nombre` del servicio (`integracion.argos_servicios.nombre`): el decodificador
  lo lee de `GET /servicios` (lista blanca), lo deja en una línea y hasta 60 caracteres
  (`NombreServicio`). Es texto del cliente: la API lo devuelve en `Servicio.nombre` (ausente si no
  tiene) y el panel lo muestra escapado («S-000412 · Casa»). Nunca va a los logs ni al proveedor de IA
  (`ServiciosClienteAsistente` no lo lee).
- `codigo` ya se guardaba. Con el formato de A45 (`S-000412`) el asistente lo usa en la etiqueta corta
  (`<codigo> · «plan» · «zona»`, §10.5 ajuste ag); con otra forma sigue «Servicio n».

## Contratos 0.10.0 y 0.10.1: contactos del cliente (A42, A46 / B22, B25)

- **Caché de lectura** (`contactos.go`): `integracion.argos_contactos` (rol, `servicios[]`, permisos,
  `persona_cliente_id`, `version`), `argos_contacto_telefonos` (solo números E.164, con sus dígitos para
  buscar) y `argos_contactos_leidos` (cuándo se leyeron completos). Argos es el dueño: nada se edita a mano.
  Es una sola tabla y una sola escritura para el portal (§11.10) y la atención: `guardarContactos` (consulta
  `GuardarContacto`) guarda todas las columnas, cambia los teléfonos solo si escribió la fila y **nunca baja
  la `version`** (una lectura vieja que llega tarde no pisa una más nueva). Cada lectura toma la hora de la
  base **antes** de pedir a Argos y la guarda en `leido_en`: con la misma versión, solo escribe si empezó
  después del último cambio de la fila, así que un `quitado` por webhook (que no sube la versión) llegado
  mientras se leía no se revive ni recupera permisos. Un contacto quitado (con `clock_timestamp()`) solo
  vuelve con una lectura que empezó después del quitado, aunque una anterior traiga una versión mayor
  (Argos lo editó y después lo quitó mientras se leía). `argos_contactos_leidos`
  se marca con esa misma hora de inicio (nunca retrocede). Sin `permisos` de Argos, todos en
  `false` (lo seguro; los valores por defecto de la base también son `false`).
- **`quitado` (estado propio de Iris, no del contrato):** un contacto que Argos quitó (`accion: quitado`), que
  ya no viene en la lista del cliente o cuyo cliente ya no existe (`404`) **no se borra**: se marca
  `quitado = true` con `leido_en`, y conserva su `version`. Ninguna lectura lo usa (portal, rol en la
  conversación, avisos de cobro, consentimiento: todas filtran `NOT quitado`, y las de la mensajería además
  `coalesce(empresa_id, 0)` de la empresa). Si vuelve a venir de Argos con la misma versión o una mayor, deja
  de estar quitado.
- `RefrescarContactos` lee `GET /clientes/:id/contactos` si la caché tiene más de 24 h (auditado como
  `clientes.sincronizar_contactos`); `BuscarContactos` llama `GET /contactos/buscar?telefono=`. Las dos
  exigen `X-Iris-Actor` de quien atiende; con Argos < 0.10.0 no llaman a nada.
- **Webhook `cliente.contactos_cambiados`:** solo de clientes que Iris conoce. Un solo manejador
  (`aplicarContactos`) para el portal y la atención. Con `quitado`, el contacto queda quitado al momento, sin
  leer Argos; si no, Iris relee con `X-Iris-Actor: sistema` (0.14.2: solo en esa ruta, nunca con
  `X-Iris-Conversacion` ni `X-Iris-Contacto`). Si Argos no responde o no lo acepta, el webhook falla y se
  reintenta (la caché queda como estaba). Emite `argos.contactos_cambiados`, sin números ni nombres.
- **`X-Iris-Contacto` (§7.1):** con Argos ≥ 0.10.1 va en toda ruta de §7 con `X-Iris-Conversacion`:
  `titular` o el `contacto_id` (`AtencionIris.Contacto`). Con uno anterior no se manda. El estado de
  cuenta acepta `?servicio_id=` (`EstadoCuentaServicio`) para un contacto con servicios acotados.
- **Con Argos caído la caché no vence:** `RefrescarContactos` no toca nada si Argos no responde (solo un
  `404` deja quitados los contactos del cliente); la mensajería sigue con los contactos que tenía, por viejos que sean, y el rol
  sigue fallando cerrado si el contacto o su número ya no están. Cada relectura que cambia contactos o
  números emite `argos.contactos_cambiados` para que la mensajería revise sus conversaciones.

## Contrato 0.9.0, bloque P: portal del cliente de Argos (`portal.go`, B8 y B28)

- `SesionPortal` resuelve cada petición de §11: solo el `token_portal` de un conector Argos (403
  `solo_integracion_argos`; 409 `argos_apagado`), `X-Iris-Empresa` y `X-Argos-Portal-Sesion` (400
  `cabecera_requerida`), `X-Iris-Contacto` con contrato ≥ 0.12.1 (400 `cabecera_iris_requerida`), tope de
  600 peticiones por minuto por conector (`integracion.portal_argos_minutos`, 429 `demasiadas_peticiones`)
  y el cliente por la instantánea o con `GET /clientes/:id` (422 `cliente_no_existe` / `empresa_no_coincide`;
  si Argos no responde, la sesión queda sin validar y solo `POST …/tickets` la acepta).
- `token_portal` (§1.2): un `iris_pat_` hijo del entregado en el canje (`nucleo.tokens_api.token_padre_id`),
  recortado a `portal_argos.*`; vale mientras vale su padre, así que vence, se confirma, se revoca y se
  elimina con él. Se entrega en el canje (`token_portal`) y, a un conector ya conectado sin uno vigente, en
  `PUT /integraciones/iris/contrato`; las dos solo cuando `ContratoIris` ≥ 0.9.0. Los `iris_pat_` que no
  son hijos pierden `portal_argos.*` aunque los dé el rol.
- Caché de contactos (`integracion.argos_contactos`, §11.10): sin números; se relee con
  `GET /clientes/:id/contactos` (con `X-Iris-Actor: sistema`, 0.14.2 §7.1, sin `X-Iris-Conversacion` ni `X-Iris-Contacto`) si falta o si
  `X-Iris-Contacto-Version` es mayor (503 si Argos no responde) y con `cliente.contactos_cambiados`
  (`quitado` marca enseguida, sin leer Argos).
- `GET /integraciones/argosisp/estado` suma `capacidades { portal_argos, solicitudes_servicio,
  adjuntos_ticket }` (§4.3) y sigue siendo solo para personas. Argos las lee con
  `GET /integraciones/argosisp/capacidades` (0.14.2, `capacidades.go`): permiso
  `integraciones.ver_capacidades` del rol `integracion_argos`, solo `{ contrato_iris, capacidades }` del
  conector del token; otro conector o uno apagado, 404; el `token_portal`, 403.

## Contrato 0.13.0: caídas repetidas y falla general (§3.11, A49/A50; B29/B30)

`red.go`. Solo por el webhook firmado (§2.3); se procesan en el job de entrantes y salen como eventos
internos que consume soporte:

- `servicio.caidas_repetidas` → `argos.caidas_repetidas`, si la empresa está en el alcance del conector
  y el servicio es de esa empresa (instantánea o `GET /servicios/:id`). Iris decide con su umbral.
- `red.falla_general_abierta` → `argos.falla_general_abierta` enseguida y **sin leer Argos**: `servicios[]`
  son los que la instantánea tiene en esa empresa, `descartados` los de otra empresa, `por_verificar` los
  ids que Iris no conoce y `paginando` si `cantidad` pasa de los ids del evento (hasta 5000).
- Lo que falta lo lee el job `integracion.argos_verificar_incidente` (`red_verificar.go`, uno por
  entrante; tablas `integracion.argos_incidente_lecturas` y `argos_incidente_servicios`): el resto con
  `GET /incidentes-red/:id/servicios?empresa_id=&cursor=&limit=1000` (un 404 = fuera del alcance; un
  error reintenta solo la página) y cada id desconocido con `GET /servicios/:id`, con un presupuesto de
  300 lecturas por vuelta y tandas de 100, cada una en su transacción, que salen como
  `argos.falla_general_servicios` (válidos, `descartados` y `no_verificados`). Un error de Argos con un
  id cuenta un intento solo para ese id (a los 3, `no_verificado`, aparte de los descartados) y el job
  vuelve en un minuto.
- `red.falla_general_cerrada` → `argos.falla_general_cerrada` (sin servicios).
- Omitidos (quedan en la auditoría): `payload_invalido` (sin `incidente_id`, empresa, `desde`, o con
  un `alcance` fuera de `router|olt|pon|zona`), `empresa_fuera_de_alcance`, `servicio_de_otra_empresa`.
- El conector necesita el permiso `incidentes_red.leer` en Argos (§1.1) para la lectura paginada.

## Cambio de titular (contrato 0.10.0 §3.4.4, B17; 0.14.1)

- **`servicio.titular_cambiado`** (`titular_cambiado.go`): Iris relee al titular nuevo y, si Argos
  confirma que el servicio ya es suyo, refresca la instantánea y emite `argos.titular_cambiado`
  (con `tipo_titular`: `persona` o `empresa`, 0.14.1). La fila del servicio se bloquea
  (`BloquearArgos`, como en las eliminaciones) y guarda `titular_cambiado_en` (el `ocurrido_en` de
  Argos del último cambio aplicado): un aviso atrasado (A→B que llega después de B→C) no reescribe
  el servicio ni emite (`titular: atrasado` en la auditoría). Solo cuentan horas de Argos
  (`integracion.entrantes.ocurrido_origen = argos`, que guarda el conector) y nunca más allá de la
  recepción + 5 minutos; sin hora de Argos no se compara ni se guarda: manda la ficha releída. Comercial y campo, además, solo mueven si
  la instantánea dice que el servicio es de `a_cliente_id`.
- **Titular empresa (0.14.1):** `TitularEmpresaDisponible` dice si el conector activo informa un
  contrato ≥ 0.14.1; si no, campo no ofrece el cambio a una empresa y el estado de la conexión lo
  lista en `funcionesApagadas` (`titular_empresa`).
