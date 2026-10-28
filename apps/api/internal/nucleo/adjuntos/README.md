# adjuntos (núcleo)

Evidencias de órdenes y tickets (fotos, firmas, documentos) con subida reanudable (núcleo de tus
1.0). Tablas `nucleo.adjuntos` y `nucleo.adjuntos_partes` (partes en curso). Vive en `nucleo`
porque lo usan varios módulos: cada uno registra su entidad.

## Qué hace

- `POST /api/v1/adjuntos` (`adjuntos.subir`): alta idempotente por el UUID del cliente; MIME y
  tamaño configurables (`IRIS_ADJUNTOS_MIME`, `IRIS_ADJUNTOS_TAMANO_MAX`); la entidad la valida su
  módulo y su empresa debe ser la activa (404 si no). Devuelve `urlSubida`.
- `HEAD`/`PATCH /api/v1/adjuntos/{id}/subida` (`adjuntos.subir`): tus 1.0 (sin la extensión
  creation: el recurso lo crea el alta). Las partes se guardan en la base de a 256 KiB a medida que
  llegan (un corte no pierde lo recibido y cualquier réplica de la API retoma). Al completar:
  tamaño, SHA-256 y firma del contenido (imágenes y PDF por `http.DetectContentType`; SVG debe ser
  XML con `<svg`; nunca HTML) → almacenamiento → `completo`. Si no coincide → `rechazado`.
- Cuotas en el alta (429 `demasiadas_subidas` con `Retry-After`): subidas sin terminar por
  usuario (`IRIS_ADJUNTOS_MAX_EN_CURSO_USUARIO`, 50) y sus bytes declarados
  (`IRIS_ADJUNTOS_MAX_BYTES_USUARIO`, 500 MiB), y por entidad (`IRIS_ADJUNTOS_MAX_EN_CURSO_ENTIDAD`,
  30). Repetir un alta no cuenta.
- Job `nucleo.adjuntos_abandonados` (worker, cada hora, único): lo que sigue `pendiente`/`subiendo`
  sin actividad más de `IRIS_ADJUNTOS_ABANDONO` (48 h) pasa a `rechazado` con motivo
  `abandonado` y se borran sus partes.
- `GET /api/v1/adjuntos/{id}` (`adjuntos.ver`, auditado; plazo de escritura de 10 min): descarga con `nosniff`, CSP `sandbox`,
  `inline` solo para JPEG/PNG/WebP/GIF verificados y `attachment` para lo demás (SVG, PDF).
- `GET /api/v1/adjuntos?entidad=&entidadId=` (`adjuntos.ver`): lista.
- Almacenamiento: `Disco` (local, sin path traversal) o `S3` (PUT/GET/DELETE con firma AWS SigV4
  propia, sin SDK). `IRIS_ADJUNTOS_ALMACEN`, `IRIS_ADJUNTOS_DIR`, `IRIS_S3_*`.

## Para otros módulos

```go
// En el init() o al armar el módulo dueño (campo: "orden"; soporte: "ticket"):
adjuntos.RegistrarEntidadAdjuntable("orden", func(ctx context.Context, q adjuntos.DBTX, id string, uso adjuntos.Uso) (int64, error) {
    // 404 (nucleo.NoEncontrado) si no existe o el actor no puede usarla para ese uso
    // (UsoSubir: su técnico, no cerrada…; UsoVer: quien puede ver la orden); devuelve su empresa (0 = null).
})
// Al cerrar: lo que falta (solo cuentan los adjuntos «completo»).
faltan, err := adjuntos.Faltantes(ctx, t, "orden", ordenID, []adjuntos.Requisito{{Tipo: "firma"}, {Tipo: "foto", ClaveChecklist: "antena"}})
// Validar la operación adjunto.registrar del lote: el adjunto existe y es de esa orden.
a, err := adjuntos.DeEntidad(ctx, t, id, "orden", ordenID)
```

## Eventos

Emite `adjunto.registrado`, `adjunto.completado` y `adjunto.rechazado` (contratos/eventos.md,
«Adjuntos»). No consume eventos.
