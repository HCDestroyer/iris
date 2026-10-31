# campo

Órdenes de trabajo (instalación, visita, retiro, cambio de equipo, mantenimiento) con su máquina
de estados, checklist por plantilla, materiales, equipos, firma y adjuntos, y la API de
sincronización de la app de campo (`.claude/integration/contratos/sync.md`). Schema `campo`.
Campo **no llama a Argos**: al cerrar emite `orden.cerrada` y el núcleo (I1) encola los pendientes.

## Qué hace

- **Catálogo:** `tipos_orden` (semilla, por SQL: duración, plantilla por defecto,
  `exige_instalar`/`exige_retirar`) y **plantillas de formularios administrables**
  (`plantillas.go`, abajo). `GET /tipos-orden` trae las plantillas activas visibles de cada tipo.
- **Plantillas** (`plantillas_checklist` + `plantilla_versiones` + `plantilla_items`):
  `GET /plantillas` · `GET /plantillas/{id}?numeroVersion=` (`plantillas.ver`), `POST /plantillas`,
  `POST /plantillas/{id}/editar|activar|desactivar` (`plantillas.crear|editar|activar|desactivar`,
  con el permiso global). Ítems `si_no`, `texto`, `numero`, `foto`, `medicion` y `seleccion` (con
  `opciones`), en orden y con `obligatorio`; `requiereFirma` por plantilla; `tipo` opcional (null =
  cualquier tipo). **Versiones:** editar reemplaza la definición y crea la versión N+1 si la
  plantilla estuvo activa alguna vez (ya viajó en el catálogo de los teléfonos) o si la versión
  está en alguna orden (las órdenes conservan la suya y lo respondido no cambia); solo un
  borrador (`borrador: true` al crear; nunca activado) se reescribe. **Empresa:** una plantilla es de la empresa activa o común
  (`comun`, empresa null; la semilla es común); con empresas registradas, las comunes solo las crea
  o modifica quien puede tocar lo común (`empresas.todas` global, el sistema o sin conector; si no,
  403). Una orden usa plantillas comunes o de su empresa. La plantilla por defecto de un tipo no se
  desactiva (422 `plantilla_por_defecto`); desactivar no toca los formularios ya agregados.
- **Formularios de la orden** (`orden_formularios`, `formularios.go`): al crear, la plantilla de la
  orden (la del tipo o `plantillaId`) entra como primer formulario (origen `tipo`) en su versión
  actual. Despacho agrega (`POST /ordenes/{id}/formularios`, `ordenes.asignar_formulario`) y quita
  (`POST /ordenes/{id}/formularios/{fid}/quitar`, solo sin respuestas: 422
  `formulario_con_respuestas`; el del tipo nunca: 422 `formulario_del_tipo`). El técnico solo fija
  la versión actual o la vigente a la hora de la operación (otra: 422 `versionId`). El técnico agrega y quita (solo los suyos) sin señal con
  `orden.agregar_formulario` / `orden.quitar_formulario` del lote (`ordenes.registrar`, orden propia
  y abierta). Una plantilla una sola vez por orden (409 `formulario_duplicado`). Las marcas del
  checklist quedan ligadas al formulario (`orden_checklist.formulario_id`); un ítem identifica su
  formulario (por versión), y por `codigo` hace falta `formularioId` si se repite.
- **Estados:** `pendiente → programada → en_camino → en_sitio → cerrada | fallida | cancelada`,
  y `pausada` (desde en camino o en sitio; ver «Pausa»),
  con acciones explícitas `POST /api/v1/ordenes/{id}/<acción>` y permiso `ordenes.<acción>`:
  `asignar` (pendiente/programada/en_camino; sin técnico vuelve a pendiente, otro técnico en camino
  la deja programada; con técnico nuevo en una orden programada la agenda revalida la cita),
  `programar` (pendiente → programada, técnico + inicio; fin = inicio + duración del tipo; ventana
  opcional), `reprogramar` (programada/en_camino/pausada/fallida → programada, con motivo). Programar y
  reprogramar **delegan en agenda** (`agenda.ReservarCita`: turno, ausencia, solape, capacidad y
  ventana; 422 con el motivo) y es agenda quien emite `orden.programada`; la bodega por defecto
  del técnico nuevo sale de su ficha de agenda. Cancelar, reabrir o quitar el técnico liberan la
  cita.
  `iniciar-camino`, `llegar` (programada o en_camino → en_sitio), `cerrar` (en_sitio), `fallar`
  (programada/en_camino/en_sitio, con `motivoFalla`), `cancelar` (no final, con motivo), `reabrir`
  (fallida o cancelada → pendiente; una cerrada no se reabre: ya tuvo efectos en Argos).
  Fuera de su estado, 422 `transicion_invalida`; con `version` vieja, 409.
- **Trabajo** (`ordenes.registrar`): `checklist`, `notas`, `materiales`, `equipos-instalados`,
  `equipos-retirados`, `firma`, `adjuntos`. Checklist, materiales, equipos y firma con la orden en
  curso; notas y adjuntos en cualquier estado. Cada uno sube la versión. Notas, materiales y
  equipos usan como id el de la operación del lote (uuid v7 del teléfono). El checklist guarda la
  marca vigente (gana la más reciente por `ocurrido_en`) y el historial de todas. MAC normalizada
  a `AA:BB:CC:DD:EE:FF`; equipo repetido 422 `equipo_duplicado`. El SVG de la firma nunca sale
  por la API ni en la auditoría.
- **Bodega:** materiales siempre de la bodega de la orden (la del técnico). Otra bodega solo la
  indica quien tiene `ordenes.asignar` con alcance (despacho, supervisor) desde el panel, y debe
  existir en la empresa según Argos (`Inventario.BodegaDeEmpresa`; sin conector, 422
  `bodega_invalida`). Desde el lote, `bodegaId` se rechaza.
- **Equipos:** se instala solo en instalación y cambio de equipo, se retira solo en retiro y cambio
  de equipo (422 `equipo_no_admitido`). Con conector, lo retirado debe estar en el comodato del
  servicio (por unidad o serie; 422 `equipo_no_en_comodato`) y queda `validado`; sin conector o con
  Argos caído se acepta como no verificado (`equipos_validados: false` en `orden.cerrada`).
- **Pausa** (`pausas.go`): `POST /ordenes/{id}/pausar { motivo, nota?, version }`
  (`ordenes.pausar`; motivos `esperando_material`, `cliente_ausente`, `clima`, `acceso`, `otro` con
  nota) desde en camino o en sitio, y `POST /ordenes/{id}/reanudar { version }` (`ordenes.reanudar`)
  al estado de antes. Cada pausa queda en `orden_pausas` (inicio, fin, motivo, nota, quién la abrió
  y la cerró, cómo terminó: `reanudada`, `cancelada` o `reprogramada`); el total de las terminadas
  en `ordenes.pausada_seg` (`tiempoPausadoSeg`). Una pausa no empieza antes del fin de la anterior
  (se ajusta con `aviso: pausa_ajustada`) y lo pausado nunca supera lo transcurrido desde que salió. Sigue abierta: conserva la cita (agenda: ocupa,
  cuenta para la capacidad y sale `pausada` en el calendario), no sale del paquete ni de la ruta
  (que no se marca desactualizada: la cita no se mueve) y admite evidencias. No se cierra, falla,
  llega, sale ni reasigna sin reanudar (422 `orden_pausada`); cancelar y reprogramar cierran la
  pausa. **Fallar desde pausada no se admite** (decisión: se reanuda y se falla en curso; así la
  duración de la pausa queda cerrada y la falla la confirma el técnico en camino o en sitio).
- **Cierre:** los obligatorios de **todos** los formularios, equipos del tipo (instalación ≥1
  instalado; retiro ≥1 retirado con condición `buena|danada|perdido`; cambio de equipo ambos) y la
  firma si algún formulario la pide → si no, 422 `cierre_incompleto` con `errors[]`
  (`formularios.<id>.checklist.<código>` con «Falta: etiqueta (formulario)»,
  `equipos.instalar|retirar`, `firma`). Luego los adjuntos (interfaz `Adjuntos`, ver abajo) → 422
  `faltan_adjuntos` con `errors[].field = "adjuntos.<clave>"`.
- **Desde un ticket:** `POST /ordenes` con `ticketId` (exige ver el ticket): hereda empresa,
  cliente, servicio, zona, cuadrilla y contacto, y deja nota interna en el ticket; también al
  fallar, cancelar o cerrar la orden.
- **Ticket y orden de la mano** (`ticket_orden.go`, acciones compuestas: corren anidados los casos
  de siempre, cada uno con su permiso, auditoría y eventos, en una sola transacción):
  - `POST /tickets/{id}/crear-orden` (`tickets.crear_orden`; exige además `ordenes.crear`,
    `ordenes.asignar` y, con `inicio`, `ordenes.programar`): crea la orden ligada (visita por
    defecto; descripción = asunto del ticket si va vacía), la asigna al técnico y/o la cuadrilla
    (al menos uno: 422 `validacion`, regla `asignacion_requerida` en `tecnicoId` y `cuadrillaId`) y
    con fecha la programa. La cuadrilla del ticket **no** se hereda en silencio: va en el cuerpo
    (el panel la propone). Todo o nada: si la agenda rechaza la cita no queda orden. Emite
    `orden.creada`, `orden.asignada` (también solo con cuadrilla) y, con fecha,
    `orden.programada` y `orden.estado_cambiada`.
  - `POST /ordenes/{id}/igualar-asignacion` (`ordenes.igualar_asignacion`): `desde = ticket` (la
    orden toma responsable y cuadrilla del ticket con `ordenes.asignar`) o `desde = orden` (el
    ticket toma técnico y cuadrilla de la orden con `tickets.asignar`). Lleva `version` y
    `versionTicket` (409 si cualquiera cambió). 422 `sin_ticket`, `ticket_sin_asignar`,
    `orden_sin_asignar`, `sin_cambios`. Responde `{ orden, ticket }`. Ojo: el rol base `tecnico`
    no tiene `tickets.ver`, así que soporte rechaza asignarle un ticket (422 en `usuarioId`).
  - **Al cerrar** una orden con ticket: nota interna en el ticket y, si sigue abierto, no le
    quedan otras órdenes abiertas (ni cerradas ni canceladas) y no tiene ya una propuesta de
    Horus, **propuesta de resolver** con `origen = orden` (soporte emite
    `ticket.resolucion_propuesta`). Las otras órdenes se cuentan con el ticket ya bloqueado
    (`FOR UPDATE`): dos cierres a la vez se serializan y el último propone. El ticket nunca se
    resuelve solo. Si después se crea o se reabre una orden del ticket, la propuesta de origen
    `orden` se descarta (con nota).
  - **Bloqueos:** siempre orden → ticket (`FOR UPDATE`) → candado del feed del técnico
    (`alimentarFeed`). Cerrar, fallar, cancelar y reabrir toman el ticket de la orden justo
    después de `tomar` (`bloquearTicket`); crear con ticket y `crear-orden` lo toman al leerlo
    (`soporte.TicketParaOrden`). Un 40P01/40001 responde 409 `concurrencia` en las acciones de la
    orden, al crear y al igualar.
  - `crear-orden` solo con cuadrilla también exige `ordenes.asignar` (403). Una
    `Idempotency-Key` que ya creó la orden de otro ticket: 422 `idempotencia_reutilizada`.
- **Zona y cuadrilla:** del catálogo de agenda (llaves foráneas). La indicada debe existir, estar
  activa y ser de la empresa (422 en `zonaId`/`cuadrillaId`); la de la instantánea de Argos se
  copia al catálogo si falta.
- **Origen** (`ordenes.origen`): `panel`, `api`, `sistema` o `argos` (retiro automático).
- **Cargo único (visita cobrada):** `tipos_orden.cobrar` con `cargo_producto_id` y/o `cargo_monto`
  (> 0) y `cargo_descripcion`; la plantilla manda si define `cobrar` (true con sus propios valores o
  los del tipo; false = no se cobra). Se configura por SQL, p. ej.
  `UPDATE campo.tipos_orden SET cobrar = true, cargo_monto = 150 WHERE codigo = 'visita'`. Al
  cerrar, `orden.cerrada` lleva `cargo: { producto_id?, monto?, descripcion }` (o null) y la cola de
  I1 lo manda a Argos (`POST /facturas/cargo-unico`) con cualquier resultado.
- **Retiro automático:** suscriptor `campo.retiro` de `argos.servicio_retirado` (en `cmd/worker`):
  lee el comodato del servicio en Argos fuera de la transacción; con equipos y sin otra orden de
  retiro abierta del servicio (no cerrada ni cancelada; una fallida cuenta) crea una de retiro con
  `origen = argos`, los equipos en la descripción y la dirección de la instantánea (si no hay, «Por
  confirmar»). Idempotente: candado por servicio + `Idempotency-Key`
  `argos.servicio_retirado:<entrante_id>`. Sin conector o de otra empresa: no crea (log); Argos
  caído: error y el relay reintenta; regla de negocio (422): log, sin reintento.
- **Instantánea de Argos:** con `clienteId`/`servicioId` en `integracion.argos_*`, valida la
  empresa (422 `empresa_no_coincide`) y que el servicio sea del cliente, y completa dirección,
  coordenadas, zona y contacto. Sin instantánea acepta los ids.
- **Permisos y alcance:** `ordenes.ver` según alcance (zona, cuadrilla o asignada al actor);
  `ordenes.ver_propias` (técnico) solo las asignadas a él. Fuera de lo visible, 404; visible sin
  alcance para la acción, 403 `fuera_de_alcance`. El técnico asignado debe ver la orden y operar en
  su empresa (422 `tecnico_fuera_de_empresa`).
- **Empresa activa:** como soporte (`filtroEmpresa`): activa, sin conector (`null`) o sistema.
  `campo.ordenes` se registra para la adopción.
- **Firma de un intento fallido:** reabrir o reprogramar una orden fallida la invalida
  (`firmas.invalidada_en`, queda como evidencia): el nuevo cierre pide otra.
- **Idempotencia:** `crear` guarda la `Idempotency-Key` (por credencial); las demás acciones la
  toleran con `version` y su estado; el lote, por id de operación.

## App de campo (contratos/sync.md; claves camelCase)

- `GET /campo/paquete?fecha=YYYY-MM-DD` (`campo.sincronizar`): órdenes del técnico del actor
  (pendientes, programadas hasta ese día —también atrasadas—, en camino, en sitio y pausadas) con
  sus formularios y el checklist de todos, sus pausas, notas, materiales, equipos, firma,
  adjuntos, instantánea de cliente y servicio y
  `comodato` (`disponible`, `motivo`: `sin_conector|argos_no_disponible|sin_servicio`);
  `existencias` de sus bodegas (igual, `sin_bodega`); `cita` de la agenda (inicio, fin, ventana);
  `ruta` publicada del día (agenda: `ordenIds` y `paradas` con la llegada estimada, solo las
  órdenes del paquete; `desactualizada` si una cambió después) y `ordenes` en ese orden (las que
  no están en la ruta, después); `cursor` tomado
  antes de leer (puede repetir, nunca saltar). El inventario se lee de Argos fuera de la
  transacción (interfaz `Inventario`, nil sin conector). `plantillas`: el catálogo activo (comunes
  y de la empresa, versión actual) para `orden.agregar_formulario`.
- `GET /campo/cambios?cursor=&limit=`: trae además `ruta` (la publicada de hoy, como está ahora) y
  `plantillas` (el catálogo como está ahora: reemplaza el del paquete).
  Feed por técnico (`campo.orden_cambios`, escrito con un
  candado por técnico para que sus ids sigan el orden de COMMIT). Una entrada por orden (`nueva`,
  `actualizada`, `reprogramada`, `cancelada`, `reasignada` sin `orden`), `cursor` y `hayMas`.
- `POST /sync/lote`: `{ dispositivoId, plataforma?, enviadoEn?, cursor?, operaciones[] }` →
  `{ resultados[], cursor, servidorEn, desfaseMs }`. Ver `.claude/integration/i2.md`.
  - Cada operación corre con el caso de uso de su acción en su propia transacción (audita como esa
    acción, canal `campo`, con `operacion_id` en la entrada auditada) y se reclama en
    `operaciones_sync` en la misma transacción: una aplicada responde `duplicada` con su resultado
    original (también con dos lotes a la vez); una rechazada se reevalúa al reenviarla.
  - Rechazo: bloquea las siguientes de la misma orden (`bloqueada_por_rechazo`) salvo
    `orden_reasignada` y `orden_cancelada`, que se evalúan una por una (tras la cancelación, notas
    y adjuntos siguen entrando). `orden {version, estado}` también en los rechazos si el técnico
    ve o tuvo la orden.
  - Reloj: `desfaseMs = servidor − enviadoEn`; cada `ocurridoEn` se corrige con él y se acota a
    `[max(ahora − 7 días, hora del estado de la orden), ahora]`; la hora original queda en
    `operaciones_sync.ocurrido_en_telefono`.
  - `datos` se valida con el esquema (OpenAPI) de la entrada de la acción: mismas reglas que el panel
    (`errors[].field = "datos.<campo>"`). NUL en un texto → 422. Cuerpo hasta 4 MiB.
  - Un `dispositivoId` es de un usuario: el de otro da 409 `dispositivo_ajeno`. `campo.dispositivos` guarda plataforma, último contacto y desfase.
  - Material por encima de la existencia de la bodega (según Argos): se acepta con
    `aviso: existencia_insuficiente` y `excede_existencia` en `orden.cerrada`.
  - `orden.pausar`, `orden.reanudar`, `orden.agregar_formulario` (id del formulario = id de la
    operación; con la versión que vio el teléfono → `aviso: version_anterior`) y
    `orden.quitar_formulario`; sus conflictos, en `contratos/sync.md` §3.

## Ubicación en vivo (`campo.posiciones`)

- `POST /api/v1/campo/posiciones` (`campo.registrar_posiciones`, rol técnico): solo un actor
  usuario con sesión de la app de campo (`X-Iris-Canal: campo`; un token o el panel → 403
  `requiere_sesion_campo`). `{ dispositivoId?, enviadoEn?, puntos: [{ lat, lon, precision?,
  velocidad?, rumbo?, registradaEn }] }` (1–500; rango validado, 422). `enviadoEn` corrige el
  desfase del reloj. Puntos de más de 24 h (`vieja`), más de 2 min adelantados (`futura`) o a menos
  del intervalo mínimo del anterior del lote (`frecuencia`) se descartan y se cuentan en `motivos`.
  El intervalo mínimo se mide también contra lo ya guardado del técnico (candado por técnico): un
  punto a menos del intervalo de uno guardado es `frecuencia`; con la misma hora, `duplicada`.
  Límite de lotes por minuto por técnico → 429 `demasiadas_posiciones` con `Retry-After`; el
  contador se escribe fuera de la transacción de la acción, así que un lote rechazado (422) también
  gasta cupo. **Sin auditoría por fila** (ni el lote aceptado ni el 429) y sin eventos; los demás
  rechazos se auditan con la entrada resumida (`{dispositivoId, enviadoEn, puntos: n}`, nunca las
  coordenadas).
- Tabla particionada por mes UTC (`campo.posiciones_YYYYMM`, creadas al vuelo y por el job);
  `campo.posiciones_ultimas` (la última por técnico y empresa) y `campo.posiciones_limite`.
- `GET /api/v1/campo/posiciones/ultimas?tecnicoId=&cuadrillaId=&zonaId=&maxAntiguedadMin=`
  (`campo.ver_posiciones`: despacho, supervisor): de la empresa activa y según el alcance (cuadrilla
  del técnico o zonas que atiende su cuadrilla).
- Job `campo.mantener_posiciones` (worker, cada 6 h): crea las particiones del mes actual y los dos
  siguientes y borra las que terminaron antes de ahora − retención (`IRIS_POSICIONES_RETENCION_DIAS`).
- Variables: `IRIS_POSICIONES_MAX_POR_LOTE` (500), `IRIS_POSICIONES_LOTES_POR_MINUTO` (6),
  `IRIS_POSICIONES_INTERVALO_MIN` (5s), `IRIS_POSICIONES_RETENCION_DIAS` (90).

## Enlaces con I1 (conectados)

- **Adjuntos** (`enlaces.go`): `campo.RegistrarAdjuntos()` (en `cmd/iris` y `cmd/worker`) registra
  la entidad `orden` en `nucleo/adjuntos` con `AccesoAdjuntos` (ver = la orden es visible; subir =
  además `ordenes.registrar` con alcance; si no, 404). `Config.Adjuntos` es `AdjuntosNucleo` por
  defecto: `Faltantes` → `adjuntos.Faltantes` (solo adjuntos completos, verificados por sha256;
  claves `foto:<código del ítem>` y `firma`: el teléfono da de alta la foto con
  `claveChecklist` = código del ítem y la firma PNG con `tipo: firma`) y `AdjuntoDeOrden` →
  `adjuntos.DeEntidad` (en `adjunto.registrar` y en los valores de los ítems foto del checklist;
  basta con el alta). `SinAdjuntosObligatorios` queda solo para pruebas que lo pidan.
- **Inventario** (`InventarioDeArgos`, sobre `integracion/argos/inventario.go`): comodato con
  `GET /servicios/:id/equipos` (antes `GET /servicios/:id` confirma la empresa), existencias con
  `GET /inventario/existencias?bodega_id=` y la bodega con `GET /bodegas/:id` (supuesto, pendiente
  con Argos). Cliente anti-SSRF del conector activo; lo de otra empresa se descarta o es error.
  Conector apagado → `ErrSinConector`: paquete con `motivo: sin_conector`, retiros sin validar
  (`equipos_validados: false`), bodega distinta rechazada.
- **Pendientes:** `orden.cerrada` lo consume `integracion/pendientes` (I1); probado de punta a
  punta en `integracion_test.go` contra un Argos falso.

## Eventos

- **Emite** (detalle en `contratos/eventos.md`): `orden.creada` (con `origen`), `orden.asignada`,
  `orden.estado_cambiada`, `orden.pausada`, `orden.reanudada`, `orden.formulario_agregado`,
  `orden.formulario_quitado`, `orden.cerrada` (con `cargo`), y `plantilla.creada`,
  `plantilla.actualizada`, `plantilla.activada`, `plantilla.desactivada`. `orden.programada` lo
  emite agenda en la misma transacción. Empresa del sobre = la de la orden (o de la plantilla).
- **Consume:** `argos.servicio_retirado` (orden de retiro automática).

## Desarrollo

`go run ./cmd/iris campo semilla [--correo tecnico@iris.local] [--empresa N] [--ordenes 3]`: crea
(o reutiliza) un técnico (con ficha de agenda y turnos de 07:00 a 19:00 si no la tenía) y órdenes
del día programadas para él en los primeros huecos libres, por los casos de uso.

## Contrato Iris ↔ ArgosISP 0.5.0 (I1)

- `orden.cerrada` lleva `tecnico { id_externo }` (vínculo de la ficha de agenda, o null) y la
  `modalidad` de cada material (leída de las existencias de Argos ≥ 1.6.0 al registrarlo; columna
  `campo.orden_materiales.modalidad`). La cola de I1 decide `tecnico` o `bodega_id`.
- **«Dato de Argos cambió»**: `SuscribirDatosArgos` (worker) escucha `argos.datos_cambiados` y
  marca las órdenes abiertas del servicio o del cliente (`datos_argos_cambiados_en`,
  `datos_argos_campos`, sin subir la versión ni pisar lo copiado; `ordenes.marcar_datos_argos`,
  auditado). La API expone `datosArgosCambiados { en, campos[] }` y emite
  `orden.datos_argos_cambiados`. La marca cambia de `en` (creciente) solo cuando trae un campo nuevo.
  Despacho lo atiende con `POST /ordenes/{id}/aplicar-datos-argos { version, avisoEn, campos? }` (`ordenes.aplicar_datos_argos`: copia de la instantánea actual la dirección,
  las coordenadas, la referencia y el contacto —`campos` = direccion, coordenadas, referencia,
  contacto; sin ellos, todo— con la regla de la creación, solo de la instantánea que sigue siendo del
  cliente y la empresa de la orden; quita de la marca solo lo aplicado y sube la versión; emite
  `orden.datos_argos_aplicados`) o `POST /ordenes/{id}/descartar-datos-argos { version, avisoEn, motivo? }`
  (`ordenes.descartar_datos_argos`: solo quita la marca; emite `orden.datos_argos_descartados`). Orden
  abierta con aviso (422 `sin_aviso_argos`, `transicion_invalida`), 422 `sin_instantanea_argos` o
  `instantanea_no_coincide`, 409 por `version` o `aviso_argos_cambiado` (`avisoEn` ≠ `datosArgosCambiados.en`:
  Argos avisó otro campo después de leer); auditadas con el diff por campo; el técnico la recibe actualizada. La orden guarda
  también la `referencia` de Argos (cómo llegar) al crearse.
