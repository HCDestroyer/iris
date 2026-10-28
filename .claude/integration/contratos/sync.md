# Contrato de sincronización sin conexión (app de campo)

Este contrato lo comparten la API (I1 e I2) y `packages/sync` (I4). La PWA y la futura app de Expo cumplen el mismo.

## 1. Qué guarda el teléfono

- **Paquete del día:** `GET /api/v1/campo/paquete?fecha=` devuelve las órdenes asignadas del técnico con:
    - la instantánea del cliente y del servicio;
    - el checklist de la plantilla;
    - los equipos en comodato;
    - las existencias de su bodega;
    - la ruta publicada;
    - un `cursor` de cambios.
- **Cambios desde el servidor:** `GET /api/v1/campo/cambios?cursor=` devuelve lo que cambió desde el cursor: órdenes nuevas, reasignadas, canceladas o reprogramadas.

Todo se guarda en IndexedDB y caduca a los 7 días sin sincronizar.

## 2. Operaciones

Cada acción del técnico es una operación con `id` (UUID v7 generado en el teléfono), `tipo`, `orden_id`, `version_base` (la versión de la orden que veía) y `ocurrido_en` (hora del teléfono, más el desfase medido en el último contacto).

Tipos: `orden.iniciar_camino`, `orden.llegar`, `checklist.marcar`, `nota.agregar`, `material.usar`, `equipo.instalar`, `equipo.retirar`, `firma.capturar`, `adjunto.registrar`, `orden.cerrar` y `orden.fallar`.

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

## 4. Adjuntos

- **Subida:** `POST /api/v1/adjuntos` (`{ id, entidad: "orden", entidadId, tipo, mime, tamano, sha256, claveChecklist?, lat?, lon?, tomadoEn? }`, camelCase) es idempotente por `id` y devuelve el adjunto con `urlSubida` (también en `Location`): una URL de subida en partes reanudable con el núcleo de tus 1.0 (`HEAD` → `Upload-Offset`/`Upload-Length`; `PATCH` con `Tus-Resumable: 1.0.0`, `Upload-Offset` y `Content-Type: application/offset+octet-stream` → 204 con el nuevo `Upload-Offset`). Lo recibido se guarda aunque se corte la señal. Al completar se verifican tamaño, SHA-256 y tipo; si no coinciden, el adjunto queda `rechazado` (422 `sha256_distinto`/`mime_distinto`, 413 `tamano_excedido`; `HEAD` responde 410) y hay que registrarlo con otro `id`. Errores definitivos del alta: 422 `mime_no_permitido`, 413 `tamano_excedido`, 422 `entidad_no_adjuntable`. `adjunto.registrar` referencia el `id` (se acepta aunque la subida no haya terminado); el cierre cuenta solo los adjuntos `completo`.
- **Fotos:** se comprimen en el teléfono a un máximo de 1600 px y unos 300 KB, con EXIF de hora y GPS.
- **Firma:** se guarda como SVG y PNG, con el nombre y el documento de quien firma.
