# campo

Órdenes de trabajo (instalación, visita, retiro, cambio de equipo, mantenimiento) con su máquina
de estados, checklist por plantilla, materiales, equipos, firma y adjuntos, y la API de
sincronización de la app de campo (`.claude/integration/contratos/sync.md`). Schema `campo`.
Campo **no llama a Argos**: al cerrar emite `orden.cerrada` y el núcleo (I1) encola los pendientes.

## Qué hace

- **Catálogo** (semilla, editable por SQL): `tipos_orden` (duración, plantilla por defecto,
  `exige_instalar`/`exige_retirar`), `plantillas_checklist` (`requiere_firma`, `tipo_codigo`:
  instalación de fibra o inalámbrica, visita técnica, retiro, cambio de equipo, mantenimiento) y
  `plantilla_items` (`si_no`, `texto`, `numero`, `foto`, `medicion`; obligatorio). `GET /tipos-orden`.
- **Estados:** `pendiente → programada → en_camino → en_sitio → cerrada | fallida | cancelada`,
  con acciones explícitas `POST /api/v1/ordenes/{id}/<acción>` y permiso `ordenes.<acción>`:
  `asignar` (pendiente/programada/en_camino; sin técnico vuelve a pendiente, otro técnico en camino
  la deja programada), `programar` (pendiente → programada, técnico + inicio; fin = inicio +
  duración del tipo), `reprogramar` (programada/en_camino/fallida → programada, con motivo),
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
- **Cierre:** checklist obligatorio, equipos del tipo (instalación ≥1 instalado; retiro ≥1
  retirado con condición `buena|danada|perdido`; cambio de equipo ambos) y firma si la plantilla
  la pide → si no, 422 `cierre_incompleto` con `errors[]` (`checklist.<código>`,
  `equipos.instalar|retirar`, `firma`). Luego los adjuntos (interfaz `Adjuntos`, ver abajo) → 422
  `faltan_adjuntos` con `errors[].field = "adjuntos.<clave>"`.
- **Desde un ticket:** `POST /ordenes` con `ticketId` (exige ver el ticket): hereda empresa,
  cliente, servicio, zona, cuadrilla y contacto, y deja nota interna en el ticket; también al
  fallar, cancelar o cerrar la orden.
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
  (pendientes, programadas hasta ese día —también atrasadas—, en camino y en sitio) con
  checklist, notas, materiales, equipos, firma, adjuntos, instantánea de cliente y servicio y
  `comodato` (`disponible`, `motivo`: `sin_conector|argos_no_disponible|sin_servicio`);
  `existencias` de sus bodegas (igual, `sin_bodega`); `ruta` ausente (fase 3); `cursor` tomado
  antes de leer (puede repetir, nunca saltar). El inventario se lee de Argos fuera de la
  transacción (interfaz `Inventario`, nil sin conector).
- `GET /campo/cambios?cursor=&limit=`: feed por técnico (`campo.orden_cambios`, escrito con un
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

- **Emite** (detalle en `contratos/eventos.md`): `orden.creada`, `orden.asignada`,
  `orden.programada` (del catálogo de agenda; lo emite campo hasta la fase 3),
  `orden.estado_cambiada`, `orden.cerrada`. Empresa del sobre = la de la orden.
- **Consume:** nada todavía (`argos.servicio_retirado` → orden de retiro: pendiente).

## Desarrollo

`go run ./cmd/iris campo semilla [--correo tecnico@iris.local] [--empresa N] [--ordenes 3]`: crea
(o reutiliza) un técnico y órdenes del día programadas para él, por los casos de uso.
