# Rotación de `IRIS_CLAVE_CIFRADO`

`IRIS_CLAVE_CIFRADO` (32 bytes en base64) cifra con AES-256-GCM los secretos que Iris guarda en la
base. La leen la **API y el worker** (los dos deben tener siempre la misma).

| Qué | Dónde | Dato asociado |
| --- | --- | --- |
| Secreto TOTP (MFA) de cada usuario | `nucleo.usuarios.totp_secreto` | `usuario:<id>:totp` |
| Token de Argos (`atk_…`) / Horus (`hf_pat_…`) y secreto de su webhook | `integracion.conectores.token_cifrado`, `secreto_webhook_cifrado` | `conector:<id>:token` y `conector:<id>:secreto_webhook` |
| Contraseñas IMAP y SMTP de los buzones | `soporte.buzones.imap_contrasena`, `smtp_contrasena` | `soporte.buzon:<id>:imap\|smtp` |

No dependen de la clave: contraseñas de usuarios (argon2id), sesiones, tokens `iris_pat_`, tickets
del portal y códigos de conexión (hashes), ni el resto de los datos.

## Qué pasa al cambiarla

Hoy **no hay re-cifrado**: Iris solo conoce una clave. Con una clave nueva, todo lo de la tabla
queda ilegible:

- **MFA:** los usuarios con MFA activo no pueden completar el login (el código no se puede
  verificar). Todavía no existe el reinicio de MFA por un administrador (pendiente de I1).
- **Conectores Argos y Horus:** las llamadas fallan y los webhooks entrantes no se pueden validar
  (el worker no descifra el secreto); activar un conector responde 500. Hay que **volver a
  conectar** cada uno (enlace de conexión nuevo, `POST /api/v1/integraciones/<sistema>/conexion/enlaces`,
  y canje en Argos/Horus), lo que entrega un token y un secreto nuevos.
- **Buzones:** la lectura IMAP y el envío SMTP fallan. Hay que **volver a escribir las contraseñas**
  de cada buzón (Administración → Buzones → Editar, IMAP y SMTP) y luego «Probar».

## Cuándo rotar

Solo si la clave se filtró (o se sospecha). Si lo que se filtró fue un respaldo de la base sin la
clave, no hace falta: sin la clave los secretos no se leen.

## Procedimiento (con lo que existe hoy)

1. Avisa: los usuarios con MFA no podrán entrar hasta que se reinicie su MFA, y las integraciones
   y buzones se cortan hasta reconectarlos.
2. Respaldo previo ([respaldos](respaldos.md)) y guarda la clave **anterior** junto a él (sirve
   para restaurar ese respaldo).
3. Revoca del lado de Argos y Horus los tokens que Iris tenía (si la clave se filtró, esos tokens
   también hay que darlos por filtrados) y cambia las contraseñas de los buzones en el servidor de
   correo.
4. Genera la nueva: `openssl rand -base64 32`. Ponla en el gestor de secretos y en el `.env` (o el
   secreto del entorno) de **api y worker**.
5. Reinicia: `docker compose -f infra/docker-compose.yml --env-file .env --profile app up -d api worker`.
6. Reconecta Argos y Horus, reescribe las contraseñas de cada buzón y pruébalos.
7. MFA: los usuarios afectados lo vuelven a configurar cuando exista el reinicio por administrador.

## Pedido abierto (I1)

Una orden `iris clave rotar` que lea la clave anterior (p. ej. `IRIS_CLAVE_CIFRADO_ANTERIOR`) y
re-cifre en una transacción todas las columnas de la tabla, auditada. Con ella, la rotación sería
transparente (sin reconectar ni reescribir) y solo haría falta el paso 3 si la clave se filtró. Y el
reinicio de MFA de otro usuario por un administrador.
