# clave

Rotación de `IRIS_CLAVE_CIFRADO` (`iris clave rotar`, runbook en
`docs/runbooks/rotar-clave-cifrado.md`).

- **Qué hace:** con `IRIS_CLAVE_CIFRADO` = nueva e `IRIS_CLAVE_CIFRADO_ANTERIOR` = anterior,
  vuelve a cifrar con la nueva lo que está con la anterior en `nucleo.usuarios.totp_secreto`,
  `integracion.conectores.token_cifrado` / `secreto_webhook_cifrado` y
  `soporte.buzones.imap_contrasena` / `smtp_contrasena`, en una transacción con candado de filas.
  Idempotente (lo que ya está con la nueva no se toca), `--simular` (cuenta sin escribir), y falla
  sin escribir si algo no abre con ninguna clave (salvo `--ignorar-ilegibles`).
- **Auditoría:** acción `clave.rotar` del actor sistema (solo la consola; un usuario recibe 403),
  con conteos por columna e ids ilegibles; nunca valores ni claves.
- **Eventos:** no emite ni consume (no es un hecho de negocio).
- **Columnas nuevas:** toda columna cifrada con `nucleo.Cifrador` debe sumarse a `Columnas` y a la
  rotación; `TestTodasLasColumnasCifradasSeRotan` falla con cualquier `bytea` sin clasificar.
