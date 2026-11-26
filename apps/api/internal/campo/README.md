# campo

Órdenes de trabajo (instalación, visita, retiro, cambio de equipo, mantenimiento) con su máquina
de estados, checklist por plantilla, materiales, equipos, firma y adjuntos, y la API de
sincronización de la app de campo (`.claude/integration/contratos/sync.md`). Schema `campo`.
Campo **no llama a Argos**: al cerrar emite `orden.cerrada` y el núcleo (I1) encola los pendientes.

## Qué hace

- **Filtro de cliente en `GET /ordenes`** (contrato con Argos, A7): `cliente_id` y `servicio_id`
  son alias de `clienteId` y `servicioId` (422 `validacion` si llegan los dos con valores
  distintos). Con `ordenes.ver`, un token (de integración, como el de Argos, o de API de una
  persona) sin `clienteId` recibe 422 `filtro_cliente_requerido`, salvo con
  `ordenes.listar_todos`. Un token nunca supera los permisos de su dueño: solo puede llevarlo el
  token de un admin (que tiene todos) o de un usuario con un rol a medida que lo incluya. Los
  tokens personales de supervisor, despacho, lectura, etc. que listaban sin filtro ahora reciben
  422: deben filtrar por cliente o pedir a un admin un rol con ese permiso. Con
  `ordenes.ver_propias` (técnico) la lista ya es solo suya y no lo exige. El panel no cambia. Un
  parámetro de consulta desconocido responde 422 `validacion` (regla `desconocido`).
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
- **Notas editables** (`notas.go`, `ordenes.editar_nota` y `ordenes.eliminar_nota`, permiso
  `ordenes.registrar`): `POST /ordenes/{id}/notas/{nota}/editar { version, texto }` y
  `/eliminar { version }`. Solo su autor, o despacho y supervisión con `ordenes.asignar` que cubra
  la orden (403 `nota_ajena`). Cada nota lleva `version` (409 `conflicto_version`); eliminar es
  lógico (`eliminada_en`, `eliminada_por`: sale de la orden y queda en la auditoría con su diff) y
  repetirlo no cambia nada; editar una eliminada, 409 `nota_eliminada`. En cualquier estado. Desde
  el lote, `nota.editar` / `nota.eliminar` (`{ notaId, versionNota }`, contratos/sync.md). No
  emiten evento: las notas no tienen consumidores fuera de Iris (el feed avisa al teléfono por la
  versión de la orden).
- **Trabajo** (`ordenes.registrar`): `checklist`, `notas`, `materiales`, `equipos-instalados`,
  `equipos-retirados`, `firma`, `adjuntos`. Checklist, materiales, equipos y firma con la orden en
  curso; notas y adjuntos en cualquier estado. Cada uno sube la versión. Notas, materiales y
  equipos usan como id el de la operación del lote (uuid v7 del teléfono). El checklist guarda la
  marca vigente (gana la más reciente por `ocurrido_en`) y el historial de todas. MAC normalizada
  a `AA:BB:CC:DD:EE:FF`; equipo repetido 422 `equipo_duplicado`. El SVG de la firma nunca sale
  por la API ni en la auditoría.
- **Existencias del paquete:** cada línea trae `codigo` (el `producto_sku` de Argos) y cada bodega
  su `nombre` (`bodega_nombre`), para buscar por código y decir «De tu bodega: Bodega Norte» en la
  app. La descripción sale de `producto_nombre` (ExistenciaInventario de Argos) o, en versiones
  anteriores, de `descripcion` / `nombre`.
- **Bodega:** sin `bodegaId`, la bodega de la orden (la del técnico). Desde el lote (B26, contrato
  0.11.0 §3.9), `bodegaId` es la bodega personal del técnico o la de su cuadrilla actual en la empresa
  de la orden (`integracion.argos_bodegas_cuadrilla`, que se llena al leer «Mi bodega» o con los
  traslados); si no, 403 `no_es_miembro` antes de mirar existencias. Al quitarlo de la cuadrilla, sus
  lotes con esa bodega se cortan desde ese momento. Nunca se cambia de bodega en silencio. Desde el
  panel, otra bodega solo la indica quien tiene `ordenes.asignar` con alcance (despacho, supervisor), y
  debe existir en la empresa según Argos (`Inventario.BodegaDeEmpresa`; sin conector, 422
  `bodega_invalida`).
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
  (que no se marca desactualizada: la cita no se mueve) y admite notas y adjuntos (evidencia). Desde
  la app de campo (el lote) no admite trabajo: checklist, formularios, materiales, equipos y firma
  responden 422 `orden_pausada` (`sinTrabajoEnPausa`; despacho sí corrige desde el panel). No se cierra, falla,
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

## Equipos del cliente (B27, contrato con Argos 0.12.0 §3.10)

- **`equipo.cambiar`** (orden `cambio_equipo`, `ordenes.cambiar_equipo`): la unidad que sale
  (`saleUnidadId`, en comodato del servicio) y la que entra (`entra`), motivo, destino de la que sale
  (por defecto `revision`; `funciona` no vale con `danado` o `garantia`), foto y cobro propuesto
  opcionales (sin monto). Cuenta como equipo instalado y retirado para el cierre.
- **`equipo.no_recuperado`** (orden `retiro`, `ordenes.no_recuperado`): solo en sitio; motivo, foto de la
  orden (obligatoria con `se_nego` y `equipo_perdido`), ubicación y nota; un intento por unidad al día
  en Iris (429 `demasiados_intentos`). Cuenta como equipo retirado para el cierre.
- La fecha del teléfono no puede ser anterior a la asignación de la orden; `recibido_en` es la hora
  del servidor. Las notas (≤ 500) se ven en la orden, pero nunca van en eventos ni en la auditoría
  (`***`).
- `orden.cerrada` suma, por equipo, `motivo`, `foto_ref`, `fecha` y `recibido_en_iris`; en el cambio,
  `sale_unidad_id`, `entra`, `destino_sale` y `cobro_propuesto { motivo }`; en el intento, `ubicacion`.
  Los materiales suman `fecha` y `recibido_en_iris`.
- **Estado e historial de cada equipo** (`GET /ordenes/{id}/equipos/estado`, `ordenes.ver` u
  `ordenes.ver_propias`): las unidades del servicio de la orden y las que la orden instaló, retiró,
  cambió o no recuperó, con el estado que avisó Argos y sus últimos 20 movimientos, sin costos
  (instantánea de `integracion/argos`, llenada por `inventario.unidad_estado_cambiado` e
  `inventario.equipo_cambiado`). Vacío si Argos no avisó nada (contrato < 0.12.0).

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
  - `tarea.crear`, `tarea.completar` y `tarea.anular` (tareas propias de agenda, sin orden: se
    agrupan por tarea; `tareas.go`). El paquete y los cambios traen `tareas` y `tomarLibres`.

## Órdenes libres y ajustes de la app (`libres.go`, `campo.ajustes`)

- `GET/POST /campo/ajustes` (`campo.configurar`, admin, sin límite de zona): ajustes de la app por
  empresa, con el patrón de los umbrales de factibilidad (una fila por empresa, `version`).
  `tecnicosTomanLibres` (apagado por defecto). Emite `campo.ajustes_cambiados`.
- **Regla de qué es libre para quién** (decisión del líder): una orden con `cuadrilla_id` solo es
  libre para los técnicos de ESA cuadrilla (despacho ya la encaminó); sin cuadrilla, vale la zona
  (las que atiende su cuadrilla, más el alcance del permiso si lo tiene limitado), y al tomarla
  queda con la cuadrilla del técnico.
- `GET /campo/ordenes-libres` (`ordenes.tomar`, técnico): pendientes y sin técnico según esa regla, por
  distancia en línea recta a su última posición (`campo.posiciones_ultimas`) o por fecha; cursor y
  `total` (contador de la hoja del «+»). Ajuste apagado: 403 `libres_apagado`.
- `POST /ordenes/{id}/tomar` (`ordenes.tomar`): reutiliza `asignar` (la lógica de
  `ordenes.asignar`) con el técnico = el actor y la bodega de su ficha, bajo el candado de la orden.
  Primero el estado y la regla: fuera de su cuadrilla o zona, o en un estado que no sea abierto
  (pendiente, programada, en camino), el mismo 404 que una orden inexistente, sin número ni nombre
  (no se enumeran órdenes ni técnicos con ids secuenciales). Abierta y con otro técnico: 409
  `orden_ya_tomada` con `tomadaPor { id, nombre }` (problem+json). Ya suya: 200 sin cambios. Requiere conexión (no va por el lote). Emite
  `orden.asignada` con `origen = tomar` y `motivo = tomada`.

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

- **Emite** (detalle en `contratos/eventos.md`): `orden.creada` (con `origen`), `orden.asignada`
  (con `origen`: `asignar`, `tomar`, `programar`, `reprogramar`, `crear`), `campo.ajustes_cambiados`,
  `orden.estado_cambiada`, `orden.pausada`, `orden.reanudada`, `orden.formulario_agregado`,
  `orden.formulario_quitado`, `orden.cerrada` (con `cargo`), y `plantilla.creada`,
  `plantilla.actualizada`, `plantilla.activada`, `plantilla.desactivada`. `orden.programada` lo
  emite agenda en la misma transacción. Empresa del sobre = la de la orden (o de la plantilla).
- **Consume:** `argos.servicio_retirado` (orden de retiro automática).

## Paso «Confirmar titular y plan» (`titular.go`; contrato argosisp.md 0.10.0 §3.4, B18 y B19)

- Operaciones del lote `orden.registrar_identidad` (A41), `orden.cambiar_titular` (A39),
  `orden.completar_datos` (A43) y `orden.cambiar_plan` (A40), en una instalación en sitio del
  técnico (reglas en `contratos/sync.md` §3b). Permisos `ordenes.cambiar_titular`,
  `ordenes.completar_datos` y `ordenes.cambiar_plan` (técnico, despacho, supervisor) y
  `ordenes.validar_identidad` (supervisor): `POST /ordenes/{id}/validar-identidad`, solo para activar.
- Cada acción encola su llamada en `integracion.pendientes` (`pendientes.EncolarPaso`, con
  `X-Iris-Actor`) en cadena identidad → datos → plan; la activación y el cobro, al cerrar, esperan a
  todos. Lo sensible (documento, teléfono, correo, NIT/CUI y fotos) va solo al depósito de tránsito
  cifrado (`integracion.datos_transito`): la auditoría lo guarda como `***` (`auditoria:"sensible"`),
  y no va en la orden (`campo.orden_titular` guarda quién firma, tipo de documento, nombre, contadores
  y la validación), en los eventos, en los logs ni en el payload del pendiente.
- Una instalación de una venta (con prospecto) no cierra con éxito sin la identidad registrada o
  validada (`identidad_requerida`). `GET /ordenes/{id}/titular` da el estado de cada paso en Argos.
- **Emite** `orden.identidad_registrada`, `orden.cambio_titular_registrado`, `orden.datos_completados`,
  `orden.cambio_plan_registrado` y `orden.identidad_validada` (nunca el número ni datos de contacto).
- Revisión de seguridad (#273): solo el técnico asignado (`propia`); `titular_no_coincide` deja la
  orden **en revisión** (`campo.orden_titular.en_revision`, la pone la cola) y solo un supervisor la
  levanta (`POST /ordenes/{id}/levantar-revision-titular`, `orden.revision_titular_levantada`); A41
  nunca reemplaza a un A39 rechazado; lo agotado no se reemplaza; cancelar o fallar detiene los
  pasos (`pendientes.DetenerPasos`) y reabrir los reinicia; el cierre exige la identidad en toda
  instalación con el servicio `pendiente_instalacion` (o sin instantánea); las fotos pasan por
  `image.DecodeConfig` (hasta 8000 × 8000); `GET /titular` se audita y nunca muestra
  `titular_no_coincide`.
- Configuración: `campo.Config.Titular` (cifrador, conector de Argos y aviso a la cola).
- **Titular empresa** (contrato 0.14.1 §3.4.4): con `titular.tipo: empresa`, el cambio de titular
  (A39) va con `cliente.tipo: empresa` (razón social, `tipo_documento_fiscal: nit` y `nit_cui`, sin
  documento ni `mayor_de_edad`) y `representante` (nombre, DPI o pasaporte y `mayor_de_edad`; su
  número solo al depósito de tránsito); con un cliente empresa, la identidad (A41) es la del
  representante. El tipo no depende del modo de la venta (el plan va aparte). Con Argos < 0.14.1, 422
  `titular_empresa_no_disponible` (`titularEmpresaDisponible` en `GET /ordenes/{id}/titular`).
  `titular_empresa_en_revision` deja la orden en revisión como `titular_no_coincide`. En la
  auditoría (`Representante.MarshalJSON`) y en los pendientes, `representante.*` es `***` salvo
  `documento_tipo`. La orden guarda `tipo_titular` (`campo.orden_titular`).
- **Desde el panel**: `POST /ordenes/{id}/completar-datos` y `POST /ordenes/{id}/cambiar-plan`
  (despacho y supervisor, con `version`), en una orden no terminada; mismas reglas y cola. El nombre
  del titular no se cambia desde el panel (422 `nombre_solo_en_sitio`): va ligado a la identidad.
- **B20, firma del contrato regenerado** (`firma_contrato.go`): `orden.firmar_contrato` del lote
  (técnico asignado, orden en sitio) firma el contrato que comercial regeneró al aceptar Argos un
  cambio de plan o de titular (`contratoPorFirmar` en `GET /ordenes/{id}/titular`). Comercial arma
  el PDF firmado antes (`campo.Config.Contratos`, `ConectarContratos`) y lo marca firmado en la
  transacción de la operación; emite `contrato.firmado` con `orden_id`.
- **B21, planes permitidos** (`planes_orden.go`): el paquete y los cambios traen `planes` en cada
  instalación con el servicio `pendiente_instalacion` (`campo.Config.Planes` =
  `argos.Modulo.PlanesPermitidos`, `GET /planes?router_id=&modo=`; uno por router y modo).
- **B17** (`titular_argos.go`): `argos.titular_cambiado` pasa las órdenes abiertas del servicio al
  titular nuevo con `guardar` (versión, feed y diff) y emite `orden.titular_movido`, solo si la
  instantánea (`integracion.argos_servicios.cliente_id`, leída con candado) dice que el servicio es
  de `a_cliente_id`: un aviso atrasado no devuelve las órdenes.

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
  `orden.datos_argos_cambiados`. Cada aviso nuevo mueve `en` (creciente), aunque repita campos; solo el reproceso del mismo webhook
  (`datos_argos_entrante_id`) no lo mueve.
  Despacho lo atiende con `POST /ordenes/{id}/aplicar-datos-argos { version, avisoEn, campos? }` (`ordenes.aplicar_datos_argos`: copia de la instantánea actual la dirección,
  las coordenadas, la referencia y el contacto —`campos` = direccion, coordenadas, referencia,
  contacto; sin ellos, todo— con la regla de la creación; si la instantánea del servicio ya no es del
  cliente o de la empresa de la orden, 422 `instantanea_no_coincide` (al cliente solo se recurre sin
  servicio o sin su instantánea); quita de la marca solo lo aplicado y sube la versión; emite
  `orden.datos_argos_aplicados`) o `POST /ordenes/{id}/descartar-datos-argos { version, avisoEn, motivo? }`
  (`ordenes.descartar_datos_argos`: solo quita la marca; emite `orden.datos_argos_descartados`). Orden
  abierta con aviso (422 `sin_aviso_argos`, `transicion_invalida`), 422 `sin_instantanea_argos` o
  `instantanea_no_coincide`, 409 por `version` o `aviso_argos_cambiado` (`avisoEn` ≠ `datosArgosCambiados.en`:
  Argos avisó otro campo después de leer); auditadas con el diff por campo; el técnico la recibe actualizada. La orden guarda
  también la `referencia` de Argos (cómo llegar) al crearse.

## Órdenes del asistente de IA (`contratos/conversaciones.md` §17.10)

- `origen: "asistente"` (valor nuevo de `campo.ordenes.origen` y de `orden.creada`): la orden la creó
  el asistente de IA de mensajería al agendar una visita, con el actor `asistente_ia`. Si la ventana
  que eligió el cliente ya no tenía espacio, la orden queda `pendiente` (sin programar) con una
  descripción que lo dice: la programa despacho.
- `OrdenesAbiertasDeCliente` y `OrdenesDelAsistenteRecientes` (`visitas_asistente.go`): las órdenes
  vivas de un cliente (con su programación y su ventana) y las que el asistente creó en 24 h, para sus
  topes. La migración del `origen` va sin transacción (`NOT VALID`, `VALIDATE` y luego quita la
  restricción vieja); su Down no toca las filas `asistente`: deja la restricción anterior sin validar.
- **Después de un Down de `20261116180516`:** una restricción `NOT VALID` no revisa las filas viejas,
  pero sí cada `INSERT` y cada `UPDATE`. Las órdenes con `origen = 'asistente'` siguen ahí y se pueden
  leer, pero **no se pueden editar** (asignar, programar, cerrar, cancelar: cualquier `UPDATE` de la fila
  falla con `ordenes_origen_check`) hasta volver a subir la migración. Si hay que quedarse abajo, hay
  que decidir a mano qué origen llevan esas órdenes (p. ej. `sistema`) antes de tocarlas.


## Equipos del cliente y activación desde la orden (contrato 0.9.0 §12, B12, B15 y B16)

- **Activar el servicio** (`activacion.go`): `orden.activar_servicio` del lote o `POST /ordenes/{id}/activar-servicio`
  (`ordenes.activar_servicio`; el técnico en sitio y en su orden, un agente también con la orden cerrada). Cada
  intento es una fila de `campo.activaciones` con `iris:aprovisionar:<orden>:<intento>`; la llamada sale por la
  cola de pendientes (`aprovisionar`), detrás de los pasos vivos del titular, con la clave Wi-Fi solo en el
  depósito cifrado. **Nunca dos activaciones:** la orden se toma con `FOR UPDATE`, hay un solo intento vivo
  (`pendiente`, `en_proceso`, `listo`) por orden y por servicio (índices únicos), y si el cierre ya mandó
  `activar-instalacion` no se activa otra vez (409 `activacion_al_cerrar`). Resultado: el 202 de la cola, el
  webhook `instalacion.*` (`argos.instalacion_resuelta`) o, a los 10 min, `GET /aprovisionamientos/:id`
  (`campo.consultar_activaciones`, cada 2 min); un intento resuelto no cambia. Un agotado sigue vivo hasta
  reintentarse o resolverse a mano en la cola (nunca dos aprovisionamientos a la vez). Estado:
  `GET /ordenes/{id}/activacion`.
- **Modo** por empresa (`POST /campo/ajustes`: `activacionModo`, `cierreExigeBienvenida`) o plantilla: `al_cerrar`
  (de fábrica), `exigir_lista` (el cierre exitoso exige el intento listo o un supervisor con
  `ordenes.cerrar_sin_activacion` y `sinActivacionMotivo`; con `cierre_exige_bienvenida`, además la bienvenida) o
  `pendiente_agente`. Sin la capacidad de Argos (contrato ≥ 0.9.0 y `capacidades.aprovisionamiento`) el modo
  efectivo es `al_cerrar`. `orden.cerrada` lleva `activacion` y `equipos[].aprovisionado`: la cola no manda
  `activar-instalacion` ni el comodato del equipo aprovisionado.
- **Equipo y Wi-Fi** (`equipo.go`): `GET /ordenes/{id}/equipo` (`servicios.ver_equipo`; 409 `equipo_sin_gestion`,
  503 `equipo_sin_respuesta`) y `POST|GET /ordenes/{id}/wifi` (`servicios.cambiar_wifi`; `integracion/equipos`).
- **Cobro de la instalación** (`cobro.go`, B15): `POST /ordenes/{id}/cobros` (`ordenes.registrar_cobro`; cambiar el
  precio de la plantilla exige `ordenes.cobro_editar_monto` y motivo; nunca Q0), `iris:cargo:<orden>:<cargo_id>`
  con `X-Iris-Actor`; un cobro vivo por orden. Sigue `argos.factura_cambiada` (facturado, pagado, anulado).
  **0.14.3:** el cobro guarda `es_instalacion` (su concepto es el de `codigo_sistema = "instalacion"`, leído de
  `GET /conceptos-cargo` fuera de la transacción; si no se puede leer, cuenta como instalación). Solo la línea de
  instalación exige `cobro_equipo` (422 `capacidad_no_disponible`) y choca con el cargo del cierre vigente con esa
  línea (409 `cobro_al_cerrar`). El cierre manda `cargo.lineas[]` si el tipo o la plantilla tienen
  `cargo_concepto_id` (y precio); la cola quita la línea de instalación si hay un cobro de A36 vigente (con la orden
  bloqueada, como aquí). Estado `por_confirmar` (422 tras un envío sin respuesta; vigente) hasta que un supervisor
  confirme en la cola; la orden lo muestra en su historial (`tipo = cobro`: `por_confirmar`, `reemplazado`,
  `reemplazo_rechazado`, `sin_reemplazo`, `revivir_rechazado`, `factura_sin_cobro`, `factura_por_anular`). Un cobro
  fallido no pasa a facturado en silencio si la orden ya tiene otro vigente (aviso `revivir_rechazado`). `factura.*` y
  `pago.*` solo se aplican al cobro con ese `factura_id`; si no hay, quedan como aviso. El reenvío de la bienvenida no
  manda la orden como `origen` si la bienvenida solo llegó relacionada por servicio; la bienvenida sin origen se
  relaciona por la fecha de cierre de la instalación respecto de la llegada del aviso (7 días).
- **Bienvenida** (`bienvenida.go`, B16): `GET /ordenes/{id}/bienvenida`, `POST /ordenes/{id}/bienvenida/reenviar`
  (`ordenes.reenviar_bienvenida`, `iris:bienvenida:<orden>:<n>`) y la bandeja `GET /ordenes/bienvenidas-sin-enviar`;
  el estado por canal llega con `argos.bienvenida_cambiada`.
- Eventos: emite `orden.activacion_pedida`, `orden.activacion_cambiada`, `orden.cobro_registrado`,
  `orden.cobro_cambiado`, `orden.bienvenida_cambiada` y `orden.bienvenida_reenviada`; consume
  `integracion.pendiente_hecho`, `_fallido`, `_resuelto_manual`, `argos.instalacion_resuelta`,
  `argos.factura_cambiada` y `argos.bienvenida_cambiada`.

**Contrato 0.14.3:** cada función del bloque E (estado del equipo, Wi-Fi —también el inicial al activar—, bienvenida y
cobro) se ofrece solo con su marca en `capacidades.equipos` de la empresa (sin el campo, como en la 0.14.2; una empresa
sin fila, todo false): si no, 422 `capacidad_no_disponible`. Un 422 `capacidad_no_disponible` de Argos deja el pendiente
terminal y hace releer el estado. Con «exigir lista» y el servicio ya `activo` en la instantánea de Argos, el cierre sale
con `activacion.estado: activado_fuera`, sin supervisor ni comodato. Una bienvenida sin origen se relaciona solo con la
única instalación del servicio y del titular de los últimos 7 días, marcada `relacionadaPorServicio`, y no cuenta para
`cierre_exige_bienvenida`.

## Tipo de servicio (contrato argosisp.md 0.15.0 §13.1, B33)

El servicio del paquete (`ServicioCampo`) trae `tipoServicio` y `sinRouter`. Un servicio sin router (cable, regla A1)
no ofrece cambiar el plan por router (`planes.motivo: tipo_sin_router`, sin llamar a `GET /planes?router_id=`) ni
«Activar servicio» (`noDisponible: tipo_sin_router`; la activación sale al cerrar). Pruebas: `tipo_servicio_test.go`.

## Corte manual de respaldo del cable (contrato argosisp.md 0.15.0 §13.2, B34; `corte_manual.go`)

- **Consume** `argos.corte_manual` (`SuscribirCortes` en cmd/worker). La solicitud vive en `campo.cortes_manuales` (clave
  `solicitud_id`; una `version` menor o igual que la guardada no cambia nada). `solicitado` abre una orden nueva
  `corte_catv` o `reconexion_catv` (`origen = argos`, idempotente por `argos.corte_manual:<solicitud_id>`; la reconexión
  con `prioridad = alta`), con la causa en la descripción y en `corteManual` del detalle. Con `descarte` (empresa fuera
  del alcance o servicio desconocido) se guarda sin abrir nada.
- **Cancelaciones fuera de orden (B4):** con la orden `pendiente` o `programada`, se cancela y se informa `cancelada`; con el
  técnico en camino, en sitio o pausado, queda `cancelacionPedida` y decide el cierre; con el corte ya `resuelta`, se
  vuelve a informar `resuelta`; un `cancelado` antes del `solicitado` se guarda (`cancelada_antes`) y el solicitado de una
  versión menor o igual ya no abre nada.
- **Emite** `corte_manual.estado_cambiado` (contador `version` propio) desde `avisarCorte`, en la transacción de cada cambio
  de la orden: `en_curso` al asignar, y al cerrar según el ítem `resultado` del checklist de cierre (plantillas comunes
  `corte_catv` y `reconexion_catv`): «Hecho» → `resuelta`; «Sin acceso», «Equipo no encontrado», «Otro motivo» →
  `no_realizada` con su `motivo_codigo`; fallar → `no_realizada` (`sin_acceso` u `otro`). Los terminales no retroceden.
- **No hace (B6–B8):** la orden no tiene ticket (ni `ticket.*` ni portal de Argos), no manda encuesta ni avisos al
  cliente y su cierre no encola `activar-instalacion` ni retiro. En un servicio mixto solo se toca el cable (lo dice la
  orden).
- **Seguridad (revisión de #297):** la clave es `(conector_id, solicitud_id)`. Un aviso con `descarte`, o con otra empresa,
  servicio o acción que la solicitud guardada, solo se anota (`ignorado`) y no toca nada; la empresa de la auditoría es la
  de la solicitud. Los candados van orden → solicitud, como el resto de campo. Las plantillas del sistema `corte_catv` y
  `reconexion_catv` no cambian de tipo ni pierden el ítem `resultado` (422 `plantilla_corte_protegida`). El Down de la
  migración falla si ya hay órdenes, citas, capacidades, plantillas o checklists de estos tipos.
- **Riesgo conocido:** un corte hecho sin señal con la orden ya cancelada en el servidor (Argos canceló antes de que el
  técnico saliera) solo queda en las notas y adjuntos de la orden: el lote recibe `orden_cancelada` y a Argos no se le
  informa `resuelta` (`contratos/sync.md`).
- Pruebas: `corte_manual_test.go` (solicitado, asignado y cerrado por `POST /sync/lote`; reconexión con prioridad alta;
  no realizada; cancelaciones fuera de orden; descarte).
