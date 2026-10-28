# Rotación de `IRIS_CLAVE_CIFRADO`

`IRIS_CLAVE_CIFRADO` (32 bytes en base64) cifra con AES-256-GCM los secretos que Iris guarda en la
base. La leen la **API y el worker** (los dos deben tener siempre la misma).

| Qué | Dónde | Dato asociado |
| --- | --- | --- |
| Secreto TOTP (MFA) de cada usuario | `nucleo.usuarios.totp_secreto` | `usuario:<id>:totp` |
| Token de Argos (`atk_…`) / Horus (`hf_pat_…`) y secreto de su webhook | `integracion.conectores.token_cifrado`, `secreto_webhook_cifrado` | `conector:<id>:token` y `conector:<id>:secreto_webhook` |
| Contraseñas IMAP y SMTP de los buzones | `soporte.buzones.imap_contrasena`, `smtp_contrasena` | `soporte.buzon:<id>:imap\|smtp` |

No dependen de la clave: contraseñas de usuarios (argon2id), sesiones, tokens `iris_pat_`, tickets
del portal y códigos de conexión (hashes), ni el resto de los datos. La lista vive en
`apps/api/internal/nucleo/clave`, y una prueba exige que toda columna `bytea` nueva quede
clasificada (cifrada y rotada, o no cifrada).

## Cuándo rotar

Si la clave se filtró (o se sospecha), o por política periódica. Si lo que se filtró fue un
respaldo de la base sin la clave, no hace falta: sin la clave los secretos no se leen.

## Cómo funciona

- Durante la rotación, API y worker arrancan con **las dos claves**: `IRIS_CLAVE_CIFRADO` = la
  nueva e `IRIS_CLAVE_CIFRADO_ANTERIOR` = la anterior. Cifran siempre con la nueva y, al descifrar,
  prueban la nueva y luego la anterior. Nada se cae a mitad de la rotación.
- **`iris clave rotar`** vuelve a cifrar con la nueva todo lo que sigue con la anterior, en **una
  sola transacción** (con candado sobre las filas). Queda en `auditoria.acciones` como acción
  `clave.rotar` del actor `sistema`, canal `consola`, con los conteos por columna y los ids
  ilegibles; nunca valores ni claves. No sube `version` ni `updated_at` (el valor no cambia).
- Es **idempotente**: lo que ya está con la nueva se cuenta como «ya» y no se toca. Correrla otra
  vez es seguro.
- **`--simular`** cuenta (total, por rotar, ya rotadas, ilegibles) sin escribir.
- Si algún valor no se descifra con **ninguna** de las dos claves, la rotación **falla sin escribir
  nada** (422 `cifrados_ilegibles`, con columna e id de cada uno). Con **`--ignorar-ilegibles`**
  rota lo demás y deja esos como están (ver «Ilegibles»).

## Procedimiento

1. **Respaldo** previo ([respaldos](respaldos.md)). Guarda la clave **anterior** junto a él: sirve
   para restaurar ese respaldo.
2. Genera la nueva: `openssl rand -base64 32`. Ponla en el gestor de secretos.
3. En el `.env` (o el secreto del entorno) de **api y worker**:

   ```sh
   IRIS_CLAVE_CIFRADO=<nueva>
   IRIS_CLAVE_CIFRADO_ANTERIOR=<anterior>
   ```

4. Reinicia: `docker compose -f infra/docker-compose.yml --env-file .env --profile app up -d api worker`.
   Desde aquí, todo lo que se escribe va con la nueva y lo viejo se sigue leyendo.
5. Simula, con la imagen de la API (trae la orden):

   ```sh
   docker compose -f infra/docker-compose.yml --env-file .env --profile app run --rm --no-deps api clave rotar --simular
   ```

   Revisa la tabla: `rotadas` es lo que se va a re-cifrar e `ilegibles` debe ser 0.
6. Rota: la misma orden sin `--simular`. Termina con `Re-cifrados: N`. Si responde
   `cifrados_ilegibles`, no escribió nada: ver «Ilegibles».
7. Comprueba: córrela otra vez con `--simular`. Todas las columnas deben tener `rotadas` en 0.
8. Quita `IRIS_CLAVE_CIFRADO_ANTERIOR` del `.env` de api y worker y reinícialos (paso 4).
9. Si la clave se filtró, los secretos que protegía pudieron leerse:
    - Revoca del lado de Argos y Horus los tokens que Iris tenía y reconecta cada conector (enlace
      nuevo, `POST /api/v1/integraciones/<sistema>/conexion/enlaces`).
    - Cambia las contraseñas de los buzones en el servidor de correo y reescríbelas en Iris
      (Administración → Buzones → Editar, y «Probar»).
    - Si se sospecha que alguien leyó los TOTP, reinicia el MFA de los usuarios (abajo).

## Ilegibles

Un valor ilegible no abre ni con la nueva ni con la anterior (se cifró con otra clave más vieja,
o se copió de otra fila). Opciones:

- Si existe la clave con que se cifró, rota primero con ella como anterior.
- Si no, corre con `--ignorar-ilegibles` y luego, según la columna:
    - **TOTP:** un administrador reinicia el MFA del usuario (abajo).
    - **Conector:** reconéctalo (enlace nuevo y canje en Argos o Horus).
    - **Buzón:** reescribe sus contraseñas y pulsa «Probar».

Si la clave anterior se perdió, todo lo de la tabla es ilegible: aplica lo anterior a cada fila.

## Reinicio de MFA por un administrador

`POST /api/v1/usuarios/{id}/reiniciar-mfa { version, motivo }` (permiso `usuarios.reiniciar_mfa`,
rol `admin`):

- Borra el secreto TOTP (activo o a medio configurar) y cierra todas las sesiones del usuario. Sus
  tokens `iris_pat_` siguen valiendo.
- El usuario entra solo con su contraseña y vuelve a configurar el MFA.
- Queda auditado y emite `usuario.mfa_reiniciado`.
- No sirve para el propio usuario (lo hace otro admin) y respeta el alcance por empresas, como
  editar o desactivar.

Iris no tiene códigos de respaldo del MFA: el reinicio es la vía para quien perdió el teléfono.
