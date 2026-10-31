# Contrato de sincronización sin conexión (app de campo)

Este contrato lo comparten la API (I1 e I2) y `packages/sync` (I4). La PWA y la futura app de Expo cumplen el mismo.

## 1. Qué guarda el teléfono

- **Paquete del día:** `GET /api/v1/campo/paquete?fecha=` devuelve las órdenes asignadas del técnico con:
    - la instantánea del cliente y del servicio;
    - sus formularios (cada uno con la plantilla en la versión con que se agregó) y el checklist de todos;
    - los equipos en comodato;
    - las existencias de su bodega;
    - la ruta publicada;
    - un `cursor` de cambios;
    - el catálogo de plantillas activas que el técnico puede agregar (`plantillas`, versión actual).
- **Cambios desde el servidor:** `GET /api/v1/campo/cambios?cursor=` devuelve lo que cambió desde el cursor: órdenes nuevas, reasignadas, canceladas o reprogramadas, y el catálogo de plantillas como está ahora (reemplaza el del paquete).

Todo se guarda en IndexedDB y caduca a los 7 días sin sincronizar.

## 2. Operaciones

Cada acción del técnico es una operación con `id` (UUID v7 generado en el teléfono), `tipo`, `orden_id`, `version_base` (la versión de la orden que veía) y `ocurrido_en` (hora del teléfono, más el desfase medido en el último contacto).

Tipos: `orden.iniciar_camino`, `orden.llegar`, `checklist.marcar`, `nota.agregar`, `material.usar`, `equipo.instalar`, `equipo.retirar`, `firma.capturar`, `adjunto.registrar`, `orden.cerrar`, `orden.fallar`, `orden.pausar`, `orden.reanudar`, `orden.agregar_formulario` y `orden.quitar_formulario`; y `traslado.recibir`, que no es de una orden (abajo).

- `traslado.recibir` `{ trasladoId, lineas?, firmaAdjuntoId, recibidoEn }` (contrato `argosisp.md` 0.5.0 §3.3; permiso `campo.recibir_traslado`): el técnico recibe una orden de traslado de Argos hacia su bodega. **Sin `ordenId` ni `versionBase`**; idempotente por el `id` de la operación; el servidor la agrupa por traslado (un rechazo bloquea las siguientes de ese traslado, nunca una orden). `firmaAdjuntoId` es un adjunto tipo `firma` en PNG (≤ 200 KB, de 50×20 a 1000×400 px) de la entidad `traslado` con `entidadId` = el traslado (`POST /adjuntos`); puede no haber terminado de subir. `lineas[]` (`{ lineaId, cantidadRecibida, seriesFaltantes? }`, cada serie de 1 a 100 caracteres) solo si faltó algo. `recibidoEn` = hora del teléfono (no futura ni anterior al `enviado_en` del traslado, con 5 min de tolerancia). Solo el técnico al que va el traslado según Iris registra, ve y manda su firma. Iris lo sabe por el webhook `inventario.traslado_enviado` o, si aún no llegó (o llegó sin técnico), lo confirma con Argos (`GET /inventario/traslados/:id`) al registrar la firma o al recibir la operación, y lo guarda (abajo). Aplicada, el resultado trae `recepcion { trasladoId, pendienteId, estado }` y el servidor encola la recepción hacia Argos (`Idempotency-Key: iris:recepcion:<traslado_id>`), que sale cuando la firma termina de subir. El estado sigue en `GET /campo/cambios` (`recepciones[]`: `esperando_firma`, `enviando`, `recibida`, `rechazada` con `codigo`, `agotada`). `@iris/sync` trae el tipo (`OperacionTrasladoCable`, `operacionRecibirTraslado`) y el esquema zod (`esquemaRecibirTraslado`); el motor de órdenes no la encola (es de I4).

- `orden.pausar` `{ motivo: esperando_material | cliente_ausente | clima | acceso | otro, nota? }` (con `otro`, nota obligatoria) y `orden.reanudar` `{}`: desde `en_camino` o `en_sitio`; reanudar vuelve al estado de antes.
- `orden.agregar_formulario` `{ plantillaId, versionId? }`: el `id` de la operación es el id del formulario (el teléfono lo conoce sin esperar). `versionId` = la versión del catálogo que vio el teléfono. Solo plantillas activas, comunes o de la empresa de la orden, en una orden propia y abierta.
- `orden.quitar_formulario` `{ formularioId }`: solo un formulario que agregó el mismo técnico y sin respuestas.
- `checklist.marcar` acepta `formularioId` (opcional con `itemId`; obligatorio con `codigo` si el código se repite en varios formularios).

```
POST /api/v1/sync/lote
{ "dispositivo_id": "...", "operaciones": [ { "id": "...", "tipo": "...", "orden_id": "...", "version_base": 3, "ocurrido_en": "...", "datos": { } } ] }
→ 200 { "resultados": [ { "id": "...", "estado": "aplicada" | "duplicada" | "rechazada", "error": { problem+json }?, "orden": { version, estado } } ], "cursor": "..." }
```

- **Orden:** las operaciones se aplican en el orden recibido. Cada una es idempotente por `id` (`duplicada` devuelve el resultado original).
- **Rechazo:** una operación rechazada no bloquea las siguientes de otra orden, pero sí las de la misma orden. El teléfono muestra el rechazo y el técnico decide.

## 3. Conflictos

| Caso | Regla |
| --- | --- |
| La orden fue reasignada a otro técnico | Las operaciones posteriores a la reasignación se rechazan con `orden_reasignada`. Lo ya aplicado se conserva. |
| La orden fue cancelada | Se rechaza con `orden_cancelada`, salvo `nota.agregar` y `adjunto.registrar`, que se aceptan como evidencia. |
| Checklist editado por otro (despacho) | Gana la marca más reciente por `ocurrido_en` y queda el historial de ambas. |
| `orden.cerrar` sin adjuntos obligatorios subidos | `rechazada` con `faltan_adjuntos` y la lista. El teléfono reintenta cuando terminan de subir. |
| Material usado mayor que la existencia de su bodega | Se acepta en Iris (queda la evidencia), pero el consumo en Argos queda pendiente con error y se avisa al supervisor. |
| `orden.agregar_formulario` con una plantilla que se desactivó mientras el técnico estaba sin señal | `rechazada` con `plantilla_inactiva` (422). El teléfono muestra el rechazo; las marcas de ese formulario en el mismo lote quedan `bloqueada_por_rechazo`. El técnico decide (quitarlo del teléfono o pedir a despacho que la active). |
| `orden.agregar_formulario` con una plantilla que se editó (versión nueva) mientras el técnico estaba sin señal | Se aplica con la versión que vio el teléfono (`versionId`: sus marcas son de esos ítems) y `aviso: version_anterior`, **solo si era la vigente a la hora de la operación** (`ocurridoEn`: la última creada hasta entonces). Cualquier otra que no sea la actual → `rechazada` con `validacion` (422, `versionId`). Nunca se cambia de versión en silencio. |
| `orden.agregar_formulario` de una plantilla que la orden ya tiene (despacho la agregó entretanto) | `rechazada` con `formulario_duplicado` (409); el `detail` trae el id del formulario existente. Las marcas posteriores por `itemId` de la misma versión van a ese formulario. |
| Plantilla de otra empresa, o `versionId` que no es de la plantilla | `rechazada` con `validacion` (422, `plantillaId` / `versionId`). |
| `orden.quitar_formulario` de un formulario con respuestas, del tipo, ajeno o ya quitado | `rechazada` con `formulario_con_respuestas` (422), `formulario_del_tipo` (422: el del tipo de orden nunca se quita), `formulario_ajeno` (403: lo agregó despacho u otro técnico) o `formulario_ya_quitado` (409). |
| `checklist.marcar` de un formulario que despacho quitó mientras el técnico estaba sin señal (no tenía respuestas en el servidor) | `rechazada` con `formulario_quitado` (409). Lo marcado sin señal no se aplica: el técnico decide (pedir a despacho que lo vuelva a asignar). |
| `orden.pausar` sobre una orden ya pausada (despacho u otro teléfono la pausó) | `rechazada` con `orden_ya_pausada` (409). |
| `orden.pausar` cuya hora cae antes del fin de la pausa anterior | Se aplica con el inicio ajustado a ese fin y `aviso: pausa_ajustada` (las pausas no se enciman; el tiempo pausado nunca supera lo transcurrido desde que salió). |
| `orden.reanudar` sobre una orden que no está pausada (despacho la reanudó, reprogramó o canceló) | `rechazada` con `orden_no_pausada` (409); si se canceló, `orden_cancelada`. |
| `traslado.recibir` de un traslado que Iris sabe cancelado (webhook `inventario.traslado_cancelado`) | `rechazada` con `traslado_cancelado` (409). Las siguientes del mismo traslado en el lote: `bloqueada_por_rechazo`. |
| `traslado.recibir` (o el `POST /adjuntos` de su firma) de un traslado que Iris aún no conoce, o que conoce sin técnico | Iris lo confirma con Argos (`GET /inventario/traslados/:id`, con la cuota de lecturas del conector y la caché negativa corta: 404 unos segundos, caída un minuto) y lo guarda con la misma validación que el webhook (empresa en el alcance, líneas acotadas). Nunca se encola nada ni se toma la clave `iris:recepcion:<id>` sin saber a qué técnico va (nadie bloquea la recepción de otro con ids futuros). Según Argos: **lo tiene y va a un técnico** → sigue normal (de otro técnico: `traslado_de_otro_tecnico`); **404** → `rechazada` con `traslado_no_encontrado` (404, definitivo: «Descartar»; la firma, 404); **sin técnico** → `rechazada` con `traslado_sin_tecnico` (422, definitivo); **no responde** (caída, 5xx, 429 o cuota agotada) → `traslado_sin_confirmar` (503), que **no es un rechazo**: el servidor no lo guarda (el mismo `id` se vuelve a mandar) y el teléfono espera **solo esa operación** (y lo que sigue de su traslado) con su propia espera exponencial (`CODIGOS_ESPERA_OPERACION` de `@iris/sync`), sin detener el resto de la cola ni el bucle; a las 24 h de la primera espera queda `rechazada` con ese error («Rehacer» o «Descartar»). La firma de ese traslado responde 503 `traslado_sin_confirmar` (el teléfono reintenta la subida). |
| `traslado.recibir` de un traslado de otro técnico (según el webhook) o de otra empresa | `rechazada` con `traslado_de_otro_tecnico` (422). |
| `traslado.recibir` sin vínculo con Argos o con Argos < 1.6.0 | `rechazada` con `tecnico_sin_vincular` (422; un administrador vincula la ficha en Agenda) o `argos_sin_inventario_tecnico` (422). |
| `traslado.recibir` con una firma que no es un adjunto `firma` PNG del traslado | `rechazada` con `firma_invalida` (422). Si la firma se registró pero al terminar de subir no cumple (más de 200 KB, medidas, no es PNG, o se rechazó al subir), la operación ya quedó aplicada: la recepción pasa a `rechazada` con `firma_invalida` en `recepciones[]` (no se llama a Argos); el técnico la manda de nuevo (otra operación, con otra firma): una recepción `rechazada` del mismo técnico se rehace con los datos nuevos (vuelve a `esperando_firma`; `integracion.pendiente_corregido`). Lo mismo vale tras un rechazo de negocio de Argos (Argos no guarda la clave de un 4xx). |
| Dos `traslado.recibir` del mismo traslado (otro teléfono u otra operación) | La del mismo técnico devuelve la recepción existente (`aplicada`, misma `recepcion`), salvo que esté `rechazada` (se rehace, arriba). La del destinatario real cuando la registró otro usuario: si el otro ya no es el destinatario según Iris (Argos reasignó el traslado) o su recepción está `rechazada`, el destinatario la toma (se rehace con su cuerpo y queda como dueño; auditado, `integracion.pendiente_corregido`), salvo que esté hecha o resuelta a mano (`recepcion_existente`, 409) o enviándose ahora: `recepcion_en_envio` (409), que no es un rechazo (no se guarda; el teléfono espera esa operación como `traslado_sin_confirmar`). Si no, `rechazada` con `recepcion_existente` (409). |
| Argos ya la tenía recibida (otra recepción, el panel de Argos) | `409 traslado_ya_recibido` cuenta como hecho: `recepciones[]` la muestra `recibida`. Las diferencias se corrigen con un ajuste en Argos. Un `422 idempotency_key_reutilizada` (la clave ya viajó con otro cuerpo: recepción rehecha o tomada) se confirma con `GET /inventario/traslados/:id`: si Argos la muestra recibida, también cuenta como hecho (conciliado `traslado_ya_recibido`); si no, queda `rechazada`. |
| Argos la rechaza (`traslado_cancelado`, `traslado_de_otro_tecnico`, `lineas_invalidas`, `firma_invalida`, `tecnico_*`) | Error de negocio, no se reintenta solo: `recepciones[]` la muestra `rechazada` con el `codigo` y el `detalle` de Argos; el teléfono la muestra y el técnico decide (los `tecnico_*` los corrige un administrador vinculando la ficha y reintentando a mano). |
| Choque con otra transacción en el servidor (Postgres cortó un deadlock o una serialización) | `rechazada` con `concurrencia` (409), pero **no es un rechazo**: el servidor no lo guarda (el mismo `id` se puede volver a mandar) y responde lo mismo, sin aplicarlas, a las operaciones siguientes de esa orden en el lote. El teléfono las deja en la cola con su `id`, no detiene la orden ni muestra rechazo, y reintenta la pasada con la espera exponencial (`CODIGOS_REINTENTABLES` de `packages/sync`). |
| `orden.cerrar`, `orden.fallar`, `orden.llegar` u `orden.iniciar_camino` con la orden pausada | `rechazada` con `orden_pausada` (422): primero `orden.reanudar`. Fallar desde pausada no se admite (decisión: la pausa se cierra con su duración y el técnico confirma la falla en curso). |

Una orden pausada sigue en el paquete y conserva su cita y su lugar en la ruta; admite checklist, notas, materiales, equipos, firma y adjuntos. La hora de cada operación nunca queda antes del inicio de la pausa en curso.

## 4. Adjuntos

- **Subida:** `POST /api/v1/adjuntos` (`{ id, entidad: "orden", entidadId, tipo, mime, tamano, sha256, claveChecklist?, lat?, lon?, tomadoEn? }`, camelCase) es idempotente por `id` y devuelve el adjunto con `urlSubida` (también en `Location`): una URL de subida en partes reanudable con el núcleo de tus 1.0 (`HEAD` → `Upload-Offset`/`Upload-Length`; `PATCH` con `Tus-Resumable: 1.0.0`, `Upload-Offset` y `Content-Type: application/offset+octet-stream` → 204 con el nuevo `Upload-Offset`). Lo recibido se guarda aunque se corte la señal. Al completar se verifican tamaño, SHA-256 y tipo; si no coinciden, el adjunto queda `rechazado` (422 `sha256_distinto`/`mime_distinto`, 413 `tamano_excedido`; `HEAD` responde 410) y hay que registrarlo con otro `id`. Errores definitivos del alta: 422 `mime_no_permitido`, 413 `tamano_excedido`, 422 `entidad_no_adjuntable`. `adjunto.registrar` referencia el `id` (se acepta aunque la subida no haya terminado); el cierre cuenta solo los adjuntos `completo`.
- **Fotos:** se comprimen en el teléfono a un máximo de 1600 px y unos 300 KB, con EXIF de hora y GPS.
- **Firma:** se guarda como SVG y PNG, con el nombre y el documento de quien firma.

## 5. Identificación de la app

Toda petición de la app de campo (PWA y nativa) lleva, además de `X-Iris-Canal: campo`:

- `X-Iris-App: campo/<versión> (<plataforma>)`: versión `[0-9A-Za-z.+_-]{1,40}` y plataforma
  `pwa`, `android` o `ios` (minúsculas; otra plataforma o app no se registra). La API guarda la
  última conexión por usuario y plataforma (`nucleo.conexiones_app`, a lo más cada 30 s, solo con
  `X-Iris-Canal: campo` en `/yo`, `/campo/…`, `/sync/…` y `/adjuntos…`) y usa la plataforma para el canal del historial
  de accesos (`campo_pwa` o `campo_nativa`). Un valor con otro formato se ignora (no es error).
- `X-Iris-Pendientes: <n>` (opcional): operaciones de la cola local sin enviar. Con pendientes y
  sin un `POST /sync/lote` en 15 min, el técnico aparece «atrasado» en Administración → Actividad
  de la app.

`packages/sync` las agrega con `crearTransporteApi(cliente, app)` (y el subidor y las posiciones);
la app nativa pone `X-Iris-App` en su cliente (`apps/movil/src/api/cliente.ts`).
