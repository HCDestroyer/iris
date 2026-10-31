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
- Para campo y el panel: `pendientes.DeEntidad(ctx, q, "orden", id)` y `ConPendienteEnArgos(ps)`
  («cerrada con pendiente en Argos»).

## Eventos

Consume `orden.cerrada`. Emite `integracion.pendiente_hecho`, `_fallido`, `_agotado`,
`_reintentado`, `_omitido`, `_corregido`, `_conflicto`, `_bloqueado` y `_desbloqueado` (contratos/eventos.md, «Pendientes hacia Argos»).

## Contrato 0.5.0: `tecnico`, modalidades y cargos

- Con Argos ≥ 1.6.0 (`argos.Modulo.InventarioPorTecnico`) y `tecnico { id_externo }` en
  `orden.cerrada`, consumo, comodato y devolución mandan `tecnico` en vez de `bodega_id` (una
  bodega explícita distinta de la de la orden sigue con `bodega_id`). El pendiente guarda
  `tecnico_usuario_id`. Sin vínculo o con Argos < 1.6.0, todo como antes. Un cierre repetido que
  solo cambia `bodega_id` ↔ `tecnico` no pisa lo encolado (su huella) ni marca conflicto.
- Consumo con `lineas[]` y solo consumibles: un material `comodato` se omite
  (`integracion.pendiente_omitido`, `material_comodato`). El cargo de un producto
  `consumible_cobro` de la orden no se encola (`cargo_cubierto_por_consumo`); el de un producto de
  modalidad desconocida espera al consumo, y un `409 cargo_duplicado` cuenta como hecho (conciliado,
  `codigo_error = cargo_duplicado`).
- La respuesta del consumo guarda `cargos[] { id, producto_id, monto }` y `factura_id` (lista blanca);
  `Pendiente.cargos` / `facturaId` en la API (la orden los ve en sus pendientes).
- `codigo_error` (type de Argos) en cada fallo; `Pendiente.codigoError` y `corregible`
  (`tecnico_no_encontrado`, `tecnico_sin_bodega`, `tecnico_ambiguo`, `empresa_requerida`). El
  reintento a mano de un corregible manda el vínculo actual del técnico (si cambió, corrige el
  cuerpo y emite `integracion.pendiente_corregido`; sin vínculo, 422 `tecnico_sin_vincular`).
  `producto_es_comodato`, `cobro_sin_cliente` y `existencia_insuficiente` son de negocio (no se
  reintentan solos; a mano, sí).
