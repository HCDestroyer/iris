# pendientes (integración)

Cola de acciones de Iris hacia Argos (`integracion.pendientes`; contrato `argosisp.md` §0 y §3).

## Qué hace

- **Suscriptor de `orden.cerrada`** (actor sistema, idempotente por la Idempotency-Key única):
  arma las llamadas del §3 — instalación exitosa → `activar-instalacion` + un comodato por equipo
  instalado; retiro exitoso → devolución (o pérdida si `condicion = perdido`) por equipo y
  `retirar` si `retirar_servicio`; cambio de equipo → devoluciones y luego asignaciones
  (`depende_de`); materiales → un consumo con `origen_tipo = iris_orden`; cargo único si la orden
  lo trae. Si falta un dato, la acción nace `fallido_negocio` con `datos_incompletos` (nunca se
  adivina). Si nunca hubo conector Argos (modo sin conector) no encola: log warn y
  `integracion.pendiente_omitido`; con un conector registrado pero apagado sí encola y el worker
  reintenta («sin conector Argos activo»). **Cierre repetido** (misma Idempotency-Key): un
  incompleto que ahora trae los datos se corrige y vuelve a `pendiente`
  (`integracion.pendiente_corregido`); mismos datos, nada; otros datos en cualquier otro estado
  (hecho, en curso, pendiente, agotado…) no se pisan: se marca `conflicto =
  cierre_repetido_con_otros_datos` con el payload nuevo (`integracion.pendiente_conflicto`, filtro
  `?conflicto=true` en la lista) para corregir a mano en Argos. Cantidades y montos ≤ 0 →
  `cantidad_no_positiva` / `monto_no_positivo` (datos incompletos). **Dependencias:** si una queda
  `fallido_negocio` o `agotado`, sus dependientes pasan a `bloqueado` (`depende_de_<id>_fallido`,
  `integracion.pendiente_bloqueado`); cuando la dependencia termina `hecho` (tras reintentarla)
  vuelven a la cola (`integracion.pendiente_desbloqueado`). Payload
  esperado: `contratos/eventos.md`, «`orden.cerrada`: lo que la cola espera de campo».
- **Job** `integracion.pendientes_procesar` (River, cola `integracion`, cada minuto y al encolar):
  procesa todo lo vencido cuyas dependencias estén hechas, por el cliente anti-SSRF del conector y
  con `Idempotency-Key: iris:<operacion>:<id>`. 2xx → `hecho` (guarda la respuesta); 4xx de
  negocio (cualquier 409 salvo `idempotencia_en_curso`) → `fallido_negocio` (detail de Argos sin credenciales); red, 5xx, 401/403, 408, 409
  `idempotencia_en_curso`, 425, 429 → reintento a 1 min,
  5 min, 30 min, 2 h y 12 h; el 6.º fallo → `agotado`. Un `en_curso` de más de 15 min (worker
  caído) se retoma. Reloj inyectable (`Config.Reloj`).
- **API para otros módulos** (comercial, campo; sin `pendientesbd`): `pendientes.Encolar(ctx, t,
  Accion{…}, aviso)` en la transacción del caso de uso (idempotente por la clave; `Desde` = el
  reloj de quien encola) y `pendientes.PorClave` → `Registro` (estado, cuerpo, respuesta).
- **Respuesta guardada (lista blanca):** del cuerpo del primer 2xx de Argos se guardan **solo**
  los campos escalares de `CamposRespuesta` — `id`, `codigo`, `codigo_externo`, `cliente_id`,
  `servicio_id`, `estado`, `empresa_id` (y sus formas camelCase) — en la raíz o dentro de
  `{ data: {...} }`. Todo lo demás (credenciales PPP, claves WiFi, tokens, datos personales,
  objetos y listas) se descarta. Un cuerpo de más de 64 KiB, no JSON o sin ninguno de esos campos
  queda null. Si alguien necesita otro campo, se agrega a la lista (y aquí).
  `CampoDeRespuesta` / `IDDeRespuesta` la leen. La API la muestra en `Pendiente.respuesta`.
- **Dependientes con datos de su dependencia:** el cuerpo lleva marcadores
  `{"$iris_ref": {"dependencia": i, "campo": "id"}}` (`pendientes.Ref`; `i` = índice en
  `DependeDe`), validados al encolar; cada dependencia debe existir, sin repetir, y ser de la
  misma empresa (si no, error de programación). Nace `por_resolver` con `proximo_intento =
  9999-12-31` (`NuncaVence`) y se resuelve **una sola vez** cuando todas sus dependencias quedan
  hechas (o al encolar, si ya lo están): el cuerpo final queda en `payload` y no cambia (mismos
  bytes en cada intento, contrato §3.1), `proximo_intento` vuelve a «ahora»; la plantilla queda
  en su columna. Las dependencias se leen con candado compartido (`FOR SHARE`): si una termina
  en otra transacción mientras se encola, se espera y se resuelve igual. Red de seguridad: cada
  corrida del job resuelve los `por_resolver` cuyas dependencias ya están hechas. Si la
  respuesta no trae el campo: `fallido_negocio`, `datos_incompletos`, `respuesta_sin_<campo>`
  (nunca se adivina) y bloquea a sus dependientes.
- **Verificar identidad (`Accion.Verificar`):** campos del cuerpo (de la lista blanca) que la
  respuesta del 2xx, si los trae, debe repetir igual — p. ej. `codigo_externo` en `POST /clientes`.
  Si no coinciden: `fallido_negocio` con `respuesta_inconsistente: …` (se guarda la respuesta para
  revisarla), **no** resuelve a sus dependientes (quedan bloqueados y nunca salen hacia Argos) y
  avisa (`integracion.pendiente_fallido`). Un campo que la respuesta no trae no se compara.
- **Despliegue (migración `20261028233355_integracion_pendientes_respuesta`):** detén o reemplaza
  el worker **antes** de `iris migrar`. Candado por si no: un pendiente por resolver nunca vence
  (`NuncaVence`), así que un worker de antes (cuyo `TomarVencido` no mira `por_resolver`) no lo
  toma ni manda el marcador crudo a Argos; el worker nuevo lo resuelve y lo pone al día.
- **409 por type (contrato 0.3.1 §3.1):** de los 409, solo `idempotencia_en_curso` se reintenta;
  los demás (`servicio_no_pendiente`, `user_ppp_duplicado`…, también sin type o con
  `about:blank`) y el 422 `idempotency_key_reutilizada` son de negocio (`pendientes.EsNegocio`,
  que también usa comercial).
- **`activar-instalacion` (§3.1):** el cuerpo no lleva `version` (una leída al encolar quedaría
  vieja y el reintento nunca avanzaría); solo `origen_tipo` y `origen_id`. Si el servicio ya no
  está `pendiente_instalacion`, Argos responde `409 servicio_no_pendiente`: `fallido_negocio`
  sin reintento, `integracion.pendiente_fallido` (aviso al supervisor con el `detail`, que trae
  el estado actual) y la orden queda «cerrada con pendiente en Argos»; sus comodatos dependientes
  no se tocan (no dependen de la activación).
- `GET /api/v1/integraciones/pendientes?entidad=&entidadId=&estado=&estadoDistintoDe=&conflicto=` (`integraciones.ver_pendientes`;
  `estadoDistintoDe=hecho` trae lo que aún no llegó a Argos en una lectura)
  y `POST /api/v1/integraciones/pendientes/{id}/reintentar { version }` (`integraciones.reintentar`,
  auditado).
- `POST /api/v1/integraciones/pendientes/{id}/resolver-manual { version, motivo }`
  (`integraciones.resolver_pendiente`, admin y supervisor; auditado): lo que Argos rechazó por negocio
  (`fallido_negocio`, también incompleto) y ya se corrigió directo en Argos queda `resuelto_manual`
  (final, con `resueltoMotivo`, `resueltoPor`, `resueltoEn`): nunca vuelve a salir y sus dependientes
  (el cargo que esperaba al consumo) vuelven a la cola. 422 `pendiente_no_resoluble` desde otro estado
  (lo agotado se reintenta; lo bloqueado espera a su dependencia). `ConPendienteEnArgos` lo cuenta
  como hecho. Un cargo bloqueado no se reintenta solo.
  - **Consumo con cargo del mismo producto:** `cargo` es obligatorio (422 `decision_cargo_requerida`
    sin él). `liberar`: el cargo sale. `resolver_tambien`: queda también `resuelto_manual` (mismo motivo,
    auditado, con su evento) y no se manda. Si Argos ya cobró el `consumible_cobro` al rehacer el consumo
    y el cargo se libera, Argos no lo reconoce como duplicado (su `origen_id` no coincide) y cobra dos
    veces. **El consumo rehecho a mano en Argos debe llevar el `origen_tipo`/`origen_id` de la orden**
    (así un cargo liberado responde `409 cargo_duplicado` y se concilia).
  - **Dependientes que esperaban su respuesta** (marcadores, p. ej. el servicio del alta de un
    prospecto cuyo `POST /clientes` falló): quedan `fallido_negocio` incompletos (`codigoError` =
    `dependencia_resuelta_a_mano`, sin marcadores pendientes) para resolverlos también a mano o
    corregirlos con los datos reales; lo que dependía de ellos sigue bloqueado hasta entonces.
- Para campo y el panel: `pendientes.DeEntidad(ctx, q, "orden", id)` y `ConPendienteEnArgos(ps)`
  («cerrada con pendiente en Argos»).

## Eventos

Consume `orden.cerrada`. Emite `integracion.pendiente_hecho`, `_fallido`, `_agotado`,
`_reintentado`, `_omitido`, `_corregido`, `_conflicto`, `_bloqueado`, `_desbloqueado` y `_resuelto_manual` (contratos/eventos.md, «Pendientes hacia Argos»).

## Contrato 0.5.0: `tecnico`, modalidades y cargos

- Con Argos ≥ 1.6.0 (`argos.Modulo.InventarioPorTecnico`) y `tecnico { id_externo }` en
  `orden.cerrada`, consumo, comodato y devolución mandan `tecnico` en vez de `bodega_id` (una
  bodega explícita distinta de la de la orden sigue con `bodega_id`). El pendiente guarda
  `tecnico_usuario_id`. Sin vínculo o con Argos < 1.6.0, todo como antes. Un cierre repetido que
  solo cambia `bodega_id` ↔ `tecnico` no pisa lo encolado (su huella) ni marca conflicto.
- Consumo con `lineas[]` y solo consumibles: un material `comodato` se omite
  (`integracion.pendiente_omitido`, `material_comodato`). El cargo de la orden nunca se descarta: si
  su producto está en el consumo, espera al consumo, y un `409 cargo_duplicado` (Argos ya lo cobró por
  modalidad) cuenta como hecho (conciliado, `codigo_error = cargo_duplicado`). Si el consumo falla, el
  cargo queda bloqueado hasta que el consumo se reintente y quede hecho, o se resuelva a mano.
- Argos vuelto a 1.5.x: un `422` sobre `bodega_id` («bodega_id required» en `detail`/`title`, o
  `errors[].field = bodega_id`) de un cuerpo con `tecnico` es corregible (`bodega_id_requerida`) y
  encola enseguida la relectura del contrato (`ArgsInformarContrato`). El reintento a mano de ese
  pendiente (o de cualquier corregible sin la capacidad `inventario_por_tecnico`) replanifica consumo
  y devolución con la bodega de la orden (sin ella, 422 `bodega_requerida`) y el comodato sin `tecnico`.
- La respuesta del consumo guarda `cargos[] { id, producto_id, monto }` y `factura_id` (lista blanca);
  `Pendiente.cargos` / `facturaId` en la API (la orden los ve en sus pendientes).
- `codigo_error` (type de Argos) en cada fallo; `Pendiente.codigoError` y `corregible`
  (`tecnico_no_encontrado`, `tecnico_sin_bodega`, `tecnico_ambiguo`, `empresa_requerida`, `bodega_id_requerida`). El
  reintento a mano de un corregible manda el vínculo actual del técnico (si cambió, corrige el
  cuerpo y emite `integracion.pendiente_corregido`; sin vínculo, 422 `tecnico_sin_vincular`).
  `producto_es_comodato`, `cobro_sin_cliente` y `existencia_insuficiente` son de negocio (no se
  reintentan solos; a mano, sí).

## Paso «Confirmar titular y plan» (contrato 0.10.0 §3.4, B18 y B19; `titular.go`, `transito.go`)

- Operaciones `registrar_identidad` (A41, multipart), `cambiar_titular` (A39, multipart con la parte
  `datos`), `completar_datos` (A43) y `cambiar_plan` (A40), con `X-Iris-Actor: usuario:<id>` y las
  claves `iris:identidad:<orden>:<toma>`, `iris:titular:<orden>:<cambio>`, `iris:datos:<orden>:<n>` e
  `iris:plan:<orden>:<cambio>`. Campo las encola con `EncolarPaso`; nacen bloqueadas si un paso
  anterior ya falló.
- Lo sensible va en `integracion.datos_transito` (AES-GCM, datos asociados `transito:<id>`), nunca en
  `payload`; se mezcla con el cuerpo solo al enviar. Se borra al 2xx (`entregado`), al resolverse a mano
  (`resuelto_manual`) o a los 7 días de tomado (`vencido`, en cada pasada de la cola); un paso sin su
  depósito queda `fallido_negocio` con `foto_vencida`. El panel ve `documento_numero`, `telefono`,
  `email` y `nit_cui` como `***` si llegaran al payload.
- La activación y el cargo de una orden cerrada dependen de todos los pasos vivos del titular; el cargo
  va a `titular.cliente_id` de la respuesta del último cambio de titular (`objetosRespuesta`).
- `ReemplazarPaso`: otra toma de la identidad reemplaza a la rechazada (lo que la esperaba pasa a
  esperar a la nueva; la vieja queda `resuelto_manual`). `DarPorValidado`: la validación desde el panel
  sin la foto deja el paso resuelto y libera la activación.
- Revisión de seguridad (#273): el consumo también espera a los pasos; cada depósito con su llave de
  datos (`llave_cifrada`, cifrada con IRIS_CLAVE_CIFRADO y borrada con el depósito; como viven en la
  misma fila, el WAL y los respaldos anteriores conservan las dos: ver `docs/runbooks/respaldos.md`; datos asociados
  `transito:<uuid>:<orden>:<empresa>` verificados con la orden y la empresa del pendiente; la rotación
  solo vuelve a cifrar la llave); purga en un job diario propio (`ArgsPurgarTransito`);
  `resuelto_como` distingue a mano, reemplazado, cancelado y validado; un A39 resuelto a mano deja el
  cobro y el consumo incompletos salvo `clienteId` al resolver; resolver un paso del titular exige
  `ordenes.validar_identidad`; `titular_no_coincide` marca la orden en revisión; un paso hecho con la
  orden cancelada o fallida emite `orden.paso_titular_tras_cancelar` (con la orden cerrada ya no, desde
  la verificación de #280: un cambio de plan o de titular de una venta de Iris lo avisa comercial con
  `orden.contrato_sin_firma`, y las órdenes sin venta de Iris no avisan); `motivo` (A39) y
  `motivo_texto` (A40) no se ven en el panel.
- Segunda revisión (#273): un paso del titular de una orden cancelada o fallida nunca sale (se mira
  antes de enviar y al guardar el resultado: lo que no salió bien queda `resuelto_manual` con
  `orden_cancelada` u `orden_fallida` y se borra su depósito); resolver a mano un `cambiar_titular`
  deja su consumo y su cobro incompletos o, con `clienteId`, reescribe el cobro; `clienteId` debe ser
  el titular actual del servicio leído en vivo de Argos en la empresa del pendiente
  (`422 cliente_no_es_titular`; Argos sin responder: `503 no_disponible`, sin guardar nada).

## Contrato 0.9.0: activación, Wi-Fi, bienvenida y cobro (§12, B12, B14–B16; `equipos.go`)

- Operaciones `aprovisionar` (`iris:aprovisionar:<orden>:<intento>`), `cambiar_wifi`
  (`iris:wifi:<origen>:<id>:<uuid>`), `reenviar_bienvenida` (`iris:bienvenida:<orden>:<n>`) y el cobro de la
  instalación como `cargo` (`iris:cargo:<orden>:<cargo_id>`). `AccionConTransito` guarda la clave Wi-Fi en el
  depósito de tránsito (entidad orden, ticket o conversación, con la entidad en los datos asociados) y encola.
  La clave se borra al 2xx y también al rechazo de Argos (`rechazado`); un reintento a mano sin ella queda
  `clave_wifi_descartada`. Un cambio desde una conversación lleva `X-Iris-Conversacion` y `X-Iris-Actor: asistente_ia`.
- `orden.cerrada` con `activacion` (modo distinto de `al_cerrar` o un intento vivo) no planifica
  `activar_instalacion`; un equipo con `aprovisionado` no lleva comodato. Un `409 servicio_no_pendiente` de
  `activar_instalacion` con un intento `listo` en la orden queda hecho (conciliado).
- Lista blanca de la respuesta: también `aprovisionamiento_id`, `solicitud_id`, `causa`, `reintentable`,
  `numero`, `total`, `moneda` y el objeto `factura { id, numero, estado, total, moneda }`.

## Contrato 0.14.3: cobro de la instalación (`cobro.go`, `cierre_orden.go`)

- **Cargo del cierre con `lineas[]`** (cuerpo real de Argos, como A36). La línea de instalación es la del concepto con
  `codigo_sistema = "instalacion"` (`argos.ConceptoInstalacion`, `GET /conceptos-cargo`, leído fuera de la
  transacción) o la de un cobro de A36 vigente. Al encolar, con `campo.ordenes` bloqueada (`FOR UPDATE`, igual que
  el registro de A36), el cargo sale sin ella si hay un cobro de A36 vigente con esa línea o la empresa tiene
  `cobro_equipo: false`; sin líneas no sale (`pendiente_omitido`, `cargo_sin_lineas`). El cuerpo queda fijo.
- **Forma anterior** (`producto_id`, `monto`): solo de cierres emitidos antes; un `consumible_cobro` del consumo no se
  encola (`pendiente_omitido`, `consumible_cobro`).
- **422 `capacidad_no_disponible` en un cargo:** queda `fallido_negocio` y `cargoTerminal` (reintentar: 409
  `cargo_terminal`). Sus demás líneas salen sin la de instalación como un cobro de reemplazo (`reemplazoDe` /
  `reemplazadoPor`, clave `iris:cargo:<orden>:<uuid>`), que nunca genera otro: si también recibe el 422, queda
  fallido y se avisa (log de error, `pendiente_fallido` y el historial de la orden). Si el concepto de instalación no
  se puede leer en ese momento, el intento cuenta como temporal (Argos no guarda la clave en un 4xx).
- **«Cobro por confirmar»:** si el 422 llega después de un envío sin respuesta (`envio_sin_respuesta`: timeout, red o
  5xx), queda `porConfirmar` (vigente), sin reemplazo ni reintento; resolver a mano responde 409
  `cobro_por_confirmar`. `POST /integraciones/pendientes/{id}/confirmar-cobro` (`integraciones.confirmar_cobro`), con
  la orden bloqueada y `version`:
  - `existe` con la «Factura #» del panel de Argos (`facturaId` = `factura_id`) o `serie` + `numeroDte`: Iris lee
    `GET /facturas/origen-iris` (`X-Iris-Actor: usuario:<id>`, Argos ≥ 0.14.3; el token necesita
    `facturas.leer_origen_iris`) y valida empresa, servicio, `iris_orden`, origen, líneas exactas (concepto, precio y
    cantidad), no anulada, `emitida_en` ≥ primer envío − 5 min y `factura_id` único en Iris (índice único) → `hecho` con
    ese `factura_id`. Un 404 `factura_no_encontrada` o un 422 solo rechazan (422 `factura_no_encontrada` /
    `factura_no_coincide`); otros errores de lectura son 503 («Reintentar»). Nunca se compara el `numero`.
  - Factura anterior a la 0.14.3 (líneas con `concepto_id` null) que cumple lo demás: queda como `facturaPorAnular`
    (no liga el cargo ni ocupa el índice único). `POST …/quitar-factura-por-anular` la quita si la lectura de esa
    acción no la ve anulada (si la ve anulada, 409 `factura_anulada`).
  - `existe = false` («no existe», con motivo, cualquier versión de Argos) → fallido y sale el reemplazo. Con una
    factura por anular, solo si la lectura de esa acción la ve anulada (422 `factura_por_anular_vigente`) y, si tenía
    pagos (`saldo < total`), con `pagosAnulados` (422 `pagos_por_confirmar`).
- **Revivir un cargo:** reintentar (y corregir un incompleto al repetir el cierre) un cargo con la línea de instalación
  bloquea la orden y responde 409 `cargo_instalacion_vigente` si hay otro vigente con ella (A36 de instalación o el
  cargo del cierre, incluido uno resuelto a mano). Sin concepto conocido, o en la forma anterior en una instalación, se
  supone que la lleva.
- **Forma anterior en una instalación:** falla cerrado (cuenta como la línea de instalación; con un A36 vigente no sale,
  `cobro_a36_vigente`). Con solo `monto` y el concepto de instalación informado por Argos, se manda como `lineas[]` con
  ese concepto. Un A36 vigente con otro concepto quita esa misma línea del cierre; sin el concepto de instalación
  conocido no se quita nada a ciegas (con un A36 de instalación vigente, el cargo nace por resolver).
- **Vigencia (revisión de 53928a7):** reintentar el cargo de un A36 fallido vuelve su cobro a `pendiente` en la
  misma transacción (409 `cobro_vigente` si hay otro vivo). Cuentan como vigentes todos los cargos de la orden con la
  línea de instalación, salvo fallidos (sin «por confirmar»), con `factura_anulada`, `esperaConsumo` o por resolver.
  Una factura por anular indicada una vez deja `exige_por_anular`: «no existe» espera a que una se lea anulada.
- **Reemplazo sin concepto de instalación:** nace por resolver (fallido incompleto, con todas las líneas).
- **`consumible_cobro` (forma anterior):** el cargo se encola `esperaConsumo` (nunca sale): queda `resuelto_manual`
  (`cobrado_con_consumo`) si el consumo confirma su cobro en `cargos[]`; si no, fallido incompleto, visible.
- **Aprovisionar con Wi-Fi y 422 `capacidad_no_disponible`:** se reenvía sin `wifi` con la clave `<clave>:sin-wifi`; la
  clave cifrada se descarta y el intento de campo sigue con la nueva clave.
- **Activado fuera de la orden:** solo la unidad que devuelve `GET /servicios/:id/equipo` cuenta como aprovisionada; el
  resto lleva comodato.