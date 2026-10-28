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

Tipos: `orden.iniciar_camino`, `orden.llegar`, `checklist.marcar`, `nota.agregar`, `material.usar`, `equipo.instalar`, `equipo.retirar`, `firma.capturar`, `adjunto.registrar`, `orden.cerrar`, `orden.fallar`, `orden.pausar`, `orden.reanudar`, `orden.agregar_formulario` y `orden.quitar_formulario`.

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
| `orden.cerrar`, `orden.fallar`, `orden.llegar` u `orden.iniciar_camino` con la orden pausada | `rechazada` con `orden_pausada` (422): primero `orden.reanudar`. Fallar desde pausada no se admite (decisión: la pausa se cierra con su duración y el técnico confirma la falla en curso). |

Una orden pausada sigue en el paquete y conserva su cita y su lugar en la ruta; admite checklist, notas, materiales, equipos, firma y adjuntos. La hora de cada operación nunca queda antes del inicio de la pausa en curso.

## 4. Adjuntos

- **Subida:** `POST /api/v1/adjuntos` (`{ id, entidad: "orden", entidadId, tipo, mime, tamano, sha256, claveChecklist?, lat?, lon?, tomadoEn? }`, camelCase) es idempotente por `id` y devuelve el adjunto con `urlSubida` (también en `Location`): una URL de subida en partes reanudable con el núcleo de tus 1.0 (`HEAD` → `Upload-Offset`/`Upload-Length`; `PATCH` con `Tus-Resumable: 1.0.0`, `Upload-Offset` y `Content-Type: application/offset+octet-stream` → 204 con el nuevo `Upload-Offset`). Lo recibido se guarda aunque se corte la señal. Al completar se verifican tamaño, SHA-256 y tipo; si no coinciden, el adjunto queda `rechazado` (422 `sha256_distinto`/`mime_distinto`, 413 `tamano_excedido`; `HEAD` responde 410) y hay que registrarlo con otro `id`. Errores definitivos del alta: 422 `mime_no_permitido`, 413 `tamano_excedido`, 422 `entidad_no_adjuntable`. `adjunto.registrar` referencia el `id` (se acepta aunque la subida no haya terminado); el cierre cuenta solo los adjuntos `completo`.
- **Fotos:** se comprimen en el teléfono a un máximo de 1600 px y unos 300 KB, con EXIF de hora y GPS.
- **Firma:** se guarda como SVG y PNG, con el nombre y el documento de quien firma.
