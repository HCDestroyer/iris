# soporte

Tickets de soporte con SLA por categoría y prioridad, calendario laboral y feriados de Guatemala,
conversación (mensajes públicos y notas internas), historial y cron de SLA. Schema `soporte`.

## Qué hace

- **Acciones** (`POST /api/v1/tickets/{id}/<acción>`, permiso `tickets.<acción>`; nunca PATCH):
  `crear` (`POST /tickets`), `asignar`, `responder`, `nota-interna`, `escalar`,
  `poner-en-espera`, `reanudar`, `resolver`, `reabrir`, `cerrar`, `fusionar`. Lecturas con
  `tickets.ver`: `GET /tickets` (cursor y filtros), `/tickets/{id}`, `/tickets/{id}/mensajes`,
  `/tickets/{id}/eventos`, `/categorias`, `/slas`. Las notas internas (y el motivo de una fusión
  en el historial) solo salen con `tickets.ver_notas` y nunca por el canal `portal`.
- **Estados:** `nuevo → abierto` (asignar o responder) `⇄ en_espera` (poner en espera / reanudar)
  `→ resuelto` (desde nuevo, abierto o en espera) `→ abierto` (reabrir) `→ cerrado` (desde
  resuelto; desde nuevo, abierto o en espera con motivo; fusionar). Cerrado es final. Una acción
  fuera de su estado responde 422 `transicion_invalida`; con `version` vieja, 409.
- **SLA:** el ticket copia al crearse los objetivos (minutos de respuesta y de resolución) del SLA
  de su categoría, o del SLA por defecto, para su prioridad. Con calendario cuentan solo los
  tramos laborales fuera de feriados (hora de `America/Guatemala`); sin calendario, 24/7.
  `en_espera` pausa la resolución (no la respuesta); el tiempo en `resuelto` también es pausa si se
  reabre. Escalar la prioridad recalcula desde el inicio los relojes que no se cumplieron (una
  primera respuesta ya dada conserva su objetivo y vencimiento). Riesgo = 80 %.
- **Cron** (`TrabajadorSLA`, River, cada minuto, único por minuto): procesa todo lo pendiente
  hasta ahora y emite `ticket.sla_en_riesgo` y `ticket.sla_vencido` una sola vez por ticket,
  reloj (`respuesta`/`resolucion`), umbral y vencimiento (`soporte.ticket_sla_alertas`): si el
  vencimiento se corre (reanudar, reabrir, escalar), el nuevo plazo vuelve a alertar.
- **Alcance:** las listas y acciones respetan el alcance del permiso (zona o cuadrilla del ticket,
  o asignado al actor) y la empresa activa. Fuera del alcance de lectura, 404; visible pero sin
  alcance para la acción, 403 `fuera_de_alcance`.
- **Empresa activa** (middleware de I1): con empresa activa, solo sus tickets (otro id = 404) y
  se crea en ella (otra en el cuerpo: 422 `empresa_no_coincide`). **Sin conector** (sin empresas
  registradas) solo los de `empresa_id` null, y se crea con null. El actor sistema ve todas y,
  con empresas registradas, indica una registrada al crear. Al asignar, el responsable debe
  operar en la empresa del ticket (422 `asignado_fuera_de_empresa`). `soporte.tickets`,
  `ticket_mensajes` y `ticket_eventos` se registran para la adopción
  (`POST /empresas/{id}/adoptar-sin-empresa`).
- **Origen:** `correo`, `telegram`, `horus` y `argos` solo los declara una integración o el
  sistema; `portal`, solo el canal portal (422 `origen_no_permitido`).
- **Asignar:** el responsable es una persona activa con `tickets.ver` y alcance sobre el ticket
  (global, su zona o la cuadrilla asignada); si no, 422 en `usuarioId`. La cuadrilla es del
  catálogo de agenda: existe, activa y de la empresa del ticket (si no, 422 en `cuadrillaId`).
- **Idempotencia:** `crear`, `responder` y `nota-interna` guardan la `Idempotency-Key` (por
  credencial, índice único). Si la clave se reclama y la petición se repite, devuelven lo que
  ya se creó en vez de duplicarlo; las demás acciones lo toleran con `version` y su estado.
- **Argos:** guarda `cliente_id`, `servicio_id` y `zona_id` (opcionales) y, sin conector, los
  datos de contacto capturados a mano. No llama a Argos. `zona_id` y `cuadrilla_id` tienen llave
  foránea al catálogo de agenda: la zona indicada debe existir, estar activa y ser de la empresa
  (422 en `zonaId`); la del portal sale de la ficha de Argos y se copia al catálogo si falta.

## Entrada por correo (IMAP) y respuesta por SMTP

Paquete de protocolo en `correo/` (IMAP con go-imap v2, MIME con go-message, SMTP con
`net/smtp`); la lógica, en `buzones.go` y `correo_*.go`.

- **Buzones** (`soporte.buzones`, `/api/v1/buzones`): `GET` lista y detalle (`buzones.ver`), `POST`
  crear (`buzones.crear`, nace inactivo), `POST /{id}/editar` (`buzones.editar`, con `version`),
  `/activar` y `/desactivar` (`buzones.activar`/`desactivar`) y `/probar` (`buzones.editar`).
  Las acciones de escritura y `probar` exigen el permiso con **alcance global** (un supervisor de
  zona recibe 403); ver y listar, con cualquier alcance. `probar` lee y descifra en una
  transacción corta, conecta fuera de toda transacción con un plazo total de 20 s (no se renueva
  por paso) y audita el resultado en otra transacción corta. Empresa: la activa (null sin conector), como los tickets. Solo TLS (`tls` implícito o `starttls`
  obligatorio; si el servidor no ofrece STARTTLS se corta antes del LOGIN) con el certificado
  verificado. Contraseñas IMAP y SMTP cifradas con `IRIS_CLAVE_CIFRADO` (AES-GCM, atadas al
  buzón); nunca salen en respuestas, logs, errores ni auditoría (`***`). Editar host, puerto,
  TLS o usuario exige volver a escribir la contraseña. SSRF: IP interna escrita → 422
  `destino_no_permitido`; al conectar, `argos.DestinoPermitido` sobre la IP resuelta, salvo
  `IRIS_CORREO_REDES_PERMITIDAS`.
- **Lectura** (`soporte.programar_buzones` cada minuto → `soporte.leer_buzon`, único por buzón):
  UID mayores que el guardado con la misma UIDVALIDITY (la primera vez, o si cambió, solo los no
  leídos). Cada correo se guarda en `correos_entrantes` (único por buzón y `Message-ID`) y avanza
  `ultimo_uid` en la misma transacción; solo después se marca leído (o se mueve a la carpeta de
  procesados). Más grande que `IRIS_CORREO_TAMANO_MAX`: solo cabeceras. Errores resumidos en
  `buzones.ultimo_error`.
- **Límites MIME:** con más de 50 `boundary=`/`multipart/` en el crudo no se analiza el cuerpo
  (solo cabeceras); el recorrido se corta a las 100 partes, 10 niveles de anidamiento o 2 MiB de
  cuerpo analizado, y respeta el plazo (10 s por correo). Si se supera un límite, el correo se
  guarda igual con `truncado = true` y `limite` = `tamano`, `mime_anidado`, `partes`,
  `profundidad`, `bytes` o `tiempo` (el cursor avanza: un correo hostil no traba el buzón) y el
  ticket lleva una nota que lo explica, con lo que se alcanzó a leer.
- **Verificación del remitente** (`correo/autenticacion.go`): el `From` lo escribe cualquiera.
  Cada buzón tiene `authservId` (el authserv-id que pone en `Authentication-Results` el servidor
  que recibe para él, RFC 8601); vacío = no se confía en ninguna cabecera y **nada queda
  verificado**. Solo cuenta el primer bloque contiguo, desde arriba, de cabeceras con ese id (las
  de más abajo las pudo escribir el remitente; el servidor debe quitar las que lleguen con su id).
  Verificado = `dmarc=pass` (con `header.from` = dominio del From, si viene) o `dkim=pass` con
  `header.d` (o `header.i`) alineado al dominio del From (igual o dominio padre con al menos un
  punto). SPF solo no basta. Se guarda en `correos_entrantes.verificado` y `autenticacion`.
- **Proceso** (`soporte.procesar_correos`, único por buzón, en orden): autorespuestas y rebotes
  (`Auto-Submitted`, `Precedence`, `X-Autoreply`, MAILER-DAEMON/postmaster, `multipart/report`,
  `Return-Path: <>`) y correos del propio buzón se ignoran. Respuesta si `In-Reply-To`/`References`
  apuntan a una respuesta enviada o a un correo ya ligado (mismo buzón), o si el asunto trae
  `[TK-…]` **y** el remitente es el contacto del ticket; el ticket debe ser de la empresa del
  buzón; si estaba fusionado, va al destino; si está cerrado, abre otro que lo menciona. En ese
  ticket (el destino, tras una fusión) **solo el contacto del ticket** (dirección normalizada)
  **con el remitente verificado** entra como mensaje público del cliente (`autor_tipo = cliente`,
  canal `correo`, sin la cita) y cambia el estado: `en_espera → abierto` (reanuda) y `resuelto →
  abierto` (reabre). Cualquier otro (tercero en CC, reenvío, From falsificado o sin verificar)
  queda como **nota interna** «correo de X no verificado / no es el contacto» (`resultado =
  nota_interna`, `motivo = remitente_no_es_contacto | remitente_no_verificado`): no cambia
  estado, contacto ni destinatario, y no emite `ticket.respondido` ni `ticket.estado_cambiado`.
  Si no hay ticket, ticket nuevo (origen `correo`, categoría y prioridad del buzón, remitente como
  contacto); puede entrar sin verificar, pero queda con `contacto.verificado = false` (el panel lo
  muestra). HTML → texto (sin scripts ni estilos), adjuntos solo como metadatos (se listan en el
  mensaje).
- **Salida** (suscriptor de `ticket.respondido` → `soporte.enviar_correo`): mensaje público de un
  agente en un ticket de origen correo → un `correos_salientes` por mensaje (idempotente). El
  destinatario es **siempre el contacto del ticket** (`contacto_correo`), nunca «el último
  remitente» ni otros remitentes ligados; buzón e `In-Reply-To`/`References` del último correo de
  ese contacto que entró como mensaje del cliente. `Re: [TK-…] asunto`, `Message-ID` fijo (un
  reintento sale igual) y sin `Auto-Submitted` (la escribió una persona). Sin SMTP en el buzón o
  sin contacto, no sale.

## Tickets desde Horus (`horus.go`)

Suscriptor `soporte.horus` (en `cmd/worker`, `SuscribirHorus`) de los eventos internos que publica
el conector (I1): `horus.router_estado_cambiado`, `horus.servicio_infra_cambiado`,
`horus.hallazgo_abierto` y `horus.hallazgo_resuelto`. Casos de uso de actor sistema
(`tickets.horus_*`, Acceso `Autenticado` + solo sistema).

- **Idempotencia:** `soporte.horus_eventos`, una fila por `entrante_id` reclamada en la misma
  transacción (`INSERT … ON CONFLICT DO NOTHING`): un evento repetido, o el mismo en dos workers a
  la vez, no hace nada dos veces (el segundo espera el COMMIT del primero). Dos eventos distintos de
  un mismo router se serializan con un advisory lock por tipo, empresa y router; los índices únicos
  parciales `tickets_maestro_router_uq` / `_infra_uq` (un maestro **no cerrado** por router o
  servicio de infraestructura y empresa) lo garantizan en la base.
- **Orden:** Horus reintenta y puede desordenar. `soporte.horus_estados` guarda el último estado y
  `ocurrido_en` por clave (router o servicio de infraestructura + empresa; hallazgo + empresa). Un
  evento con `ocurrido_en` igual o anterior al guardado queda `obsoleto` y no hace nada: un «caído»
  más viejo que el último «arriba» no abre maestro, y un «arriba» más viejo que la caída que abrió
  el maestro no propone resolver. Si `hallazgo_resuelto` llega antes que un `hallazgo_abierto` más
  viejo, **no se abre ticket** (Horus ya lo dio por resuelto); una apertura posterior al resuelto
  (Horus lo reabrió) sí abre.
- **Caída (`caido`):** sin maestro → abre uno: categoría `incidente_red` (SLA `red_24_7`),
  prioridad alta, origen `horus`, asunto «Caída de router X (#id)», `maestro = router|infra`,
  `router_id` / `infra_id`, nota interna con la hora. Maestro abierto → nota (y se descarta una
  propuesta de resolver pendiente). Maestro **resuelto ≤ 24 h antes de la caída**
  (`VentanaReapertura`, medida con `ocurrido_en` de Horus, no con la hora de proceso) → se reabre
  (`resuelto → abierto`) y se avisa a los hijos de la nueva caída (nota pública, job de abajo). Resuelto hace más → se cierra («nueva caída») y se abre otro que lo
  menciona. Cerrado es final: se abre otro.
- **Vuelve (`arriba`):** nota y **propuesta** de resolver (`tickets.propuesta = resolver`, emite
  `ticket.resolucion_propuesta`); nunca resuelve solo. La propuesta se ve en `Ticket.propuesta` y se
  descarta en cualquier cambio de estado. Sin maestro abierto, nada.
- **Hijos:** al crear un ticket (panel, API, portal; no los de origen `horus`) con cliente y sin
  `ticketPadreId`, si la instantánea de Argos (`integracion.argos_servicios.router_id`) pone su
  servicio (o, sin servicio, todos los servicios del cliente con maestro abierto) en **un solo**
  router con maestro abierto de la misma empresa, el ticket nace hijo (`ticket_padre_id`) con una
  nota interna «Vinculado automáticamente…». Si hay maestro de router y de infraestructura del
  mismo router, gana el de router. Sin instantánea, o con varios routers posibles, no se vincula.
- **Al resolver el maestro** (`tickets.resolver`) se encola en la misma transacción el job
  `soporte.avisar_hijos` (River, único por maestro, aviso y versión; `avisos_hijos.go`), que avisa
  en tandas de 200 (`TandaAvisos`, una transacción cada una) a cada hijo no cerrado: mensaje público
  de Iris con la solución, `ticket.respondido` (si el hijo vino por correo, sale por correo al
  contacto: la salida acepta mensajes públicos de agente o de Iris), `TocarTicket` (sube su
  versión) y historial `aviso_incidente` (en `GET /tickets/{id}/eventos`, en el enum de
  `EventoTicket`). Idempotente por `soporte.avisos_hijos`. Los hijos no
  cambian de estado. Lo mismo con «reabierto» cuando Horus reabre el maestro.
- **Hallazgo abierto:** solo si la categoría tiene regla **activa** en `soporte.reglas_hallazgos`
  (`categoria_horus` → `categoria_id` y prioridad de Iris; la de la empresa gana a la global, aunque
  esté inactiva; la semilla trae `equipo_infectado` y `cpe_firmware_vulnerable` inactivas; se
  administran por SQL por ahora). Un ticket por hallazgo (`tickets_hallazgo_uq`): del cliente que
  resolvió Argos (descripción pública genérica, sin IP) o sin cliente con el motivo
  (`motivo_sin_cliente`) en la descripción. La IP, el resumen y «Ver en Horus» (`url_horus`) van
  en una nota interna y en `Ticket.incidente`.
- **Hallazgo resuelto:** nota y propuesta: `resolver` si está abierto, `cerrar` si ya está
  resuelto; cerrado, solo la nota.
- **Empresa:** la del evento. Sin empresas registradas (sin conector), los tickets van con empresa
  null. Con empresas, un evento sin empresa (o con una no registrada) **no crea nada**: queda en
  `horus_eventos` (`sin_empresa` / `empresa_no_registrada`) y se emite `soporte.horus_descartado`.
  Una regla de negocio que impide crear (p. ej. la categoría de la regla está inactiva) queda como
  `error` con la misma alerta, sin reintentos; un error de base se reintenta (relay).

## Portal del cliente (`portal.go`, `rutas_portal.go`)

Casos con `Acceso: auditoria.Cliente` (actor cliente de la sesión `iris_portal`, canal `portal`),
rutas bajo `/api/v1/portal/` (tag `portal`, seguridad `portal`). Cliente y empresa salen siempre de
la sesión; un ticket de otro cliente o de otra empresa da 404 (en `verificarAcceso`, que también
usan `tomar` y `leer`). El cliente ve un `TicketPortal` reducido (sin asignación, SLA, notas,
historial ni datos de Horus) y solo los mensajes públicos (`MensajePortal`, autor
`cliente|agente|sistema`, sin ids de usuarios).

| Método y ruta | Qué hace |
| --- | --- |
| `GET /portal/tickets?estado=&cursor=&limit=` | Sus tickets, más recientes primero. |
| `GET /portal/tickets/{id}` | Detalle (`afectadoPorIncidente`, `puedeConfirmar`). |
| `GET /portal/tickets/{id}/mensajes` | Mensajes públicos (asc, cursor). |
| `POST /portal/tickets` | `asunto`, `descripcion`, `categoriaId?` (de `GET /portal/categorias`), `servicioId?` (suyo según la instantánea). Origen `portal`, prioridad media, contacto y zona de la ficha de Argos (si no hay, el nombre de la sesión). Se vincula al maestro abierto como cualquier ticket. `Idempotency-Key`. |
| `POST /portal/tickets/{id}/responder` | `cuerpo`, `version?`. Mensaje del cliente: `en_espera → abierto`, `resuelto → abierto` (como el correo); cerrado → 422. Emite `ticket.mensaje_cliente`. `Idempotency-Key`. |
| `POST /portal/tickets/{id}/confirmar-solucion` | `version`. `resuelto → cerrado`; si no, 422 `transicion_invalida`. |
| `GET /portal/categorias` | Activas con `visible_portal` (globales y de su empresa). |

**Topes anti spam** (conteos en la base por `cliente:<empresa>:<cliente>`, con advisory lock por
cliente; configurables): tickets del portal abiertos (`IRIS_PORTAL_TICKETS_ABIERTOS`, 10) y
creados en la última hora (`IRIS_PORTAL_TICKETS_POR_HORA`, 20) → 429 `demasiados_tickets` (una
creación repetida con la misma `Idempotency-Key` no cuenta); mensajes del cliente por el portal en
el último minuto, en todos sus tickets (`IRIS_PORTAL_MENSAJES_POR_MINUTO`, 10; la descripción del
ticket cuenta) → 429 `demasiados_mensajes`. `afectadoPorIncidente` es true solo si el maestro
(padre) sigue en nuevo, abierto o en espera. Un mensaje del cliente que no cambia el estado no
sube la `version` (portal y correo).

Lógica común con el panel en funciones internas: `crearTicket` (también la usa Horus),
`reactivarPorCliente` (también el correo), `tomar`, `cerrar`. Las claves de idempotencia del
cliente son `cliente:<empresa>:<cliente>|<clave>`.

## Rendimiento (prueba de carga de I5)

- **Cron de SLA:** `AlertasSLAPendientes` lee solo los tickets vivos con un reloj en riesgo (los
  índices parciales `tickets_sla_respuesta_idx` / `tickets_sla_resolucion_idx`, BitmapOr) y mira
  las alertas ya emitidas por ticket con la llave primaria de `ticket_sla_alertas`: su costo no
  crece con la historia. Las alertas se emiten en lotes de 200 (`LoteSLAPorDefecto`), cada uno en
  su transacción con su fila de auditoría (`RevisorSLA`); la idempotencia sigue siendo el `INSERT …
  ON CONFLICT DO NOTHING` en la transacción del evento.
- **Búsqueda `q` de la bandeja:** índices GIN de trigramas (`pg_trgm`, `fastupdate = off`) sobre
  `numero`, `asunto` y `contacto_nombre`; la consulta no cambió (`ILIKE '%…%'`).
- `internal/soporte/carga` siembra 60 000 tickets de un año, compara la consulta anterior del cron
  con la nueva (mismo resultado), verifica los planes con `EXPLAIN` y mide.

## Eventos que emite

`ticket.creado`, `ticket.asignado`, `ticket.estado_cambiado`, `ticket.respondido`,
`ticket.escalado`, `ticket.fusionado`, `ticket.sla_en_riesgo`, `ticket.sla_vencido`,
`ticket.resolucion_propuesta`, `ticket.mensaje_cliente`, `soporte.horus_descartado`,
`correo.recibido`, `buzon.creado`, `buzon.actualizado`, `buzon.activado`, `buzon.desactivado`.
Payloads en `.claude/integration/contratos/eventos.md`.

## Eventos que consume

`ticket.respondido` (salida por correo; idempotente por mensaje) y `horus.router_estado_cambiado`,
`horus.servicio_infra_cambiado`, `horus.hallazgo_abierto`, `horus.hallazgo_resuelto` (tickets desde
Horus; idempotentes por `entrante_id`).

## Pruebas

`IRIS_DB=iris_test_i2 go test ./internal/soporte/` (Postgres real): calendario (horario, fin de
semana, feriados y medio día), transiciones válidas e inválidas, permisos y alcance, auditoría y
outbox, `version` → 409, idempotencia por HTTP, pausa del SLA y cron idempotente.
`revision_test.go` cubre los hallazgos de la revisión de la PR #5 y `empresa_test.go` la empresa
activa (aislamiento A/B por HTTP, modo sin conector, asignación y adopción). `correo_test.go`, con
un IMAP en memoria (`imapmemserver`) y un SMTP falso con TLS de una CA de prueba
(`correo_falso_test.go`): ticket nuevo, respuesta por número y por `In-Reply-To`, duplicado por
`Message-ID`, autorespuesta y rebote, HTML, reanudación por UID, otra empresa, contraseña cifrada y
nunca expuesta, TLS exigido (sin STARTTLS no hay LOGIN; CA desconocida), SSRF, envío idempotente y
las rutas HTTP. `correo/mensaje_test.go`: HTML, cita, charsets, cabeceras sin inyección, límites
MIME y verificación con `Authentication-Results`. `revision_correo_test.go` cubre la revisión de
seguridad de la PR #8: secuestro del hilo (tercero y From sin verificar → nota interna; respuesta
al contacto; fusión), MIME anidado profundo (< 1 s, no traba el siguiente), From falsificado,
`probar` sin transacción durante la red y con plazo total, y alcance global de `buzones.*`.
`horus_test.go`: caída con dos webhooks entregados dos veces en paralelo → un maestro; hijos por
servicio, por cliente y por el portal; sin instantánea u otro router, no; arriba → propuesta;
resolver el maestro → aviso público y `ticket.respondido` en cada hijo; reapertura dentro de 24 h y
maestro nuevo después; índice único; servicio de infraestructura; empresa (sin conector, sin
empresa, no registrada); hallazgos con y sin regla, regla de empresa que apaga la global, con y sin
cliente, repetido, error de negocio y resuelto (propuesta de resolver y de cerrar); y el camino real
outbox → relay → suscriptor. `portal_test.go`: por HTTP con el middleware real, crear (ficha,
idempotencia por cliente, categoría y servicio ajenos), mensajes sin notas internas, aislamiento
entre clientes y empresas, sin acceso al panel ni del panel al portal, reanudar, reabrir,
confirmar (409 y 422); y los casos `Cliente` frente a los `ConPermiso`.
`revision_horus_test.go` (revisión de la PR): eventos de Horus fuera de orden (arriba antes que un
caído viejo, arriba más viejo que el maestro, ventana por `ocurrido_en`, hallazgo resuelto antes que
abierto), aviso a los hijos en tandas e idempotente, topes del portal y versión sin cambio.
