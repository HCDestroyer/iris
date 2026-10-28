# I1 · Núcleo de la API

**Carpetas:** `apps/api/cmd/`, `apps/api/internal/{nucleo,auditoria,integracion}`, `apps/api/migraciones/` (las de estos schemas), `apps/api/openapi/`, `packages/contratos` (lo regeneras, nunca a mano).

## Tareas

1. **Esqueleto:**
    - `cmd/iris` con `chi`, configuración por env (`IRIS_*`), `slog` en JSON, `/salud` y `/listo`;
    - errores RFC 9457, paginación por cursor e `Idempotency-Key`.
    - OpenAPI con `huma` v2 sobre `chi`; `make openapi` escribe `packages/contratos/openapi.json`.
2. **Contexto y transacción:** `contexto.Actor`, `Canal`, `RequestID`; `nucleo.EnTx(ctx, func(tx) error)`. Base de casos de uso que audita (`auditoria.acciones`, `auditoria.cambios` con `***` para lo sensible) y escribe en el outbox en la misma transacción. Relay del outbox con River.
3. **Identidad:**
    - Usuarios con argon2id y TOTP opcional; sesiones en cookie `HttpOnly` + `SameSite=Lax` con CSRF por doble envío.
    - Tokens `iris_pat_` guardados como hash.
    - Roles, permisos y alcance (zona, cuadrilla); `permisos.go` es el catálogo único y se publica en el OpenAPI como enum.
    - Seed de roles base: `admin`, `supervisor`, `despacho`, `tecnico`, `soporte`, `ventas` y `lectura`.
4. **Conectores** (`contratos/argosisp.md` y `contratos/horus.md`):
    - enlace de conexión;
    - receptor de webhooks firmados (guarda en `integracion.entrantes` y procesa en un job);
    - cliente HTTP de Argos con reintentos;
    - instantáneas y cola de `pendientes` con espera exponencial;
    - autologin por ticket (emisor y receptor).
5. **Adjuntos:** `POST /adjuntos` con subida reanudable (tus) a disco o S3 compatible (MinIO en dev).

**Pruebas:** Go con Postgres real (`IRIS_DB=iris_test_i1`), con `sqlc` para todo el SQL y migraciones `goose`.
