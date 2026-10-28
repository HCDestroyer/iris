# nucleo

Piezas comunes de la API: errores RFC 9457 (`Problema`), transacciones (`EnTx`), paginación por
cursor, `version` → 409, `Idempotency-Key`, cifrado de secretos y el catálogo único de permisos
(`permisos.go`). Subpaquetes:

- `contexto`: `Actor`, `Canal`, `RequestID` y permisos efectivos con alcance (zona/cuadrilla).
- `identidad`: usuarios (argon2id), sesiones con cookie + CSRF, TOTP, roles, tokens `iris_pat_`,
  límites de intentos (login: 10 fallos por correo → 15 min con 429; MFA: 5 fallos → sesión
  pendiente revocada).
- `servidor`: chi + huma, middlewares (request id, log, pánicos, identidad, idempotencia),
  `/salud` y `/listo`.
- `pruebas`: Postgres real para las pruebas de integración.

**Idempotencia:** claves por credencial (`token:<id>` para tokens, `<tipo>:<usuario>` para
sesiones), vigentes 24 h. Las respuestas con secretos (`POST /tokens`, `POST /yo/mfa`) no se
guardan: repetir la clave da 409 `idempotencia_no_repetible`. Una clave abandonada
`en_proceso` se retoma tras `IRIS_IDEMPOTENCIA_RECLAMO` (2 min).

**Eventos que emite** (`identidad`, con `empresa_id` null): `usuario.creado`,
`usuario.actualizado`, `usuario.desactivado`, `token.creado`, `token.revocado` (payloads en
`.claude/integration/contratos/eventos.md`). **No consume eventos.**
