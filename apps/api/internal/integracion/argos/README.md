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
  cada resultado trae `servicios` (los de la empresa activa) leídos en Argos con los mismos cupos
  y tope que la verificación de empresas (20 clientes); si Argos no responde, de la instantánea.
  Un cliente cuyos servicios no se pudieron leer queda con `servicios: null` (el panel abre la
  ficha). Sin el parámetro, `servicios` es null y no hay llamadas extra.
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
  la instantánea (teléfono, dirección, coordenadas `"lat,lng"` y zona incluidos). Si todos los que
  cambiaron son de los que la instantánea no guarda (`tipo`, `nit_cui`, `tipo_documento_fiscal`,
  `movil`), no relee el cliente («ignorado», `omitido: campos_sin_efecto`). Sin `campos[]` o con
  uno desconocido, refresca.
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
