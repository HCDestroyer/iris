# nucleo

Piezas comunes de la API: errores RFC 9457 (`Problema`), transacciones (`EnTx`), paginación por
cursor, `version` → 409, `Idempotency-Key`, cifrado de secretos y el catálogo único de permisos
(`permisos.go`). **IP del cliente** (`ip.go`): `IPCliente` resuelve `X-Forwarded-For` solo desde
`IRIS_PROXIES_CONFIABLES`; el servidor reescribe `RemoteAddr` una vez por petición y todos leen la IP con
`HostDe(RemoteAddr)`. Subpaquetes:

- `contexto`: `Actor` (también el **actor cliente** del portal: `ActorCliente`, con `ClienteID` y
  `EmpresaID` de Argos), `Canal`, `RequestID`, permisos efectivos con alcance (zona/cuadrilla) y
  empresa activa (`EmpresaDe`, `EmpresasDe`).
- `identidad`: usuarios (argon2id), sesiones con cookie + CSRF, TOTP, roles, tokens `iris_pat_`,
  límites de intentos (login: 10 fallos por correo → 15 min con 429; MFA: 5 fallos → sesión
  pendiente revocada; el job `nucleo.intentos_login_purga` del worker borra cada hora, por lotes,
  los intentos sin bloqueo vigente cuyo último fallo tiene más de max(ventana, bloqueo, 24 h),
  con el corte calculado por `now()` de la base), empresas (`nucleo.empresas`, membresía `nucleo.usuarios_empresas`) y la
  empresa activa por petición (`MiddlewareEmpresa`, cabecera `X-Iris-Empresa`). **Portal de
  soporte** (`portal.go`): Argos emite un acceso de un solo uso (`POST /portal/tickets-acceso`,
  permiso `portal.emitir`, 60 s), el canje (`POST /portal/sesion`) abre una sesión de actor
  cliente (cookie `iris_portal` + CSRF `iris_portal_csrf`, 2 h, solo válida en `/api/v1/portal/…`;
  sin límite por IP; freno global: pasados 1000 canjes fallidos por minuto los fallidos dan 429; un ticket válido entra siempre), `GET`/`DELETE
  /portal/sesion`. Consola: `iris portal acceso --cliente <id> --empresa <id>`. El job
  `nucleo.portal_purga` (worker, cada hora) borra las sesiones vencidas o revocadas y los tickets
  vencidos sin sesiones 30 días después. **Integraciones**
  (`integracion.go`): roles base `integracion_argos` / `integracion_horus` y
  `iris integracion crear --sistema argosisp|horus --empresa …` (usuario + token).
- `clave`: rotación de `IRIS_CLAVE_CIFRADO` (`iris clave rotar [--simular] [--ignorar-ilegibles]`):
  re-cifra TOTP, conectores y buzones con la clave nueva en una transacción auditada (`clave.rotar`,
  actor sistema). `Cifrador` acepta la anterior (`IRIS_CLAVE_CIFRADO_ANTERIOR`) para descifrar
  durante la transición. Runbook: `docs/runbooks/rotar-clave-cifrado.md`.
- `servidor`: chi + huma, middlewares (request id, log, pánicos, identidad, idempotencia),
  `/salud` y `/listo`.
- `pruebas`: Postgres real para las pruebas de integración.

**Idempotencia:** el hash de la petición es un HMAC-SHA256 con clave derivada de
`IRIS_CLAVE_CIFRADO` (HKDF, `iris:idempotencia`), nunca un hash plano del cuerpo (puede llevar
contraseñas); una fila de una clave ya desconocida da 422 `idempotencia_no_verificable`. Las vencidas las borra el job `nucleo.idempotencia_purga` (worker, cada hora). Claves por credencial (`token:<id>` para tokens, `<tipo>:<usuario>` para
sesiones, `cliente:<empresa>:<cliente>` para el actor cliente), vigentes 24 h. Las respuestas con secretos (`POST /tokens`, `POST /yo/mfa`) no se
guardan: repetir la clave da 409 `idempotencia_no_repetible`. Una clave abandonada
`en_proceso` se retoma tras `IRIS_IDEMPOTENCIA_RECLAMO` (2 min).

**Eventos que emite** (`identidad`, con `empresa_id` null): `usuario.creado`,
`usuario.actualizado`, `usuario.desactivado`, `usuario.mfa_reiniciado` (`POST /usuarios/{id}/reiniciar-mfa`,
solo admin, nunca el propio: borra el TOTP, pone una contraseña temporal que hay que cambiar con
`POST /yo/contrasena` antes de cualquier otra cosa, cierra sesiones y revoca tokens; responde el `Usuario` con `sesionesCerradas` y `tokensRevocados`), `token.creado`, `token.revocado`; y, con la
empresa afectada, `empresa.creada`, `empresa.actualizada`, `usuario.empresa_asignada`,
`usuario.empresa_quitada`, `empresa.datos_adoptados` (payloads en `.claude/integration/contratos/eventos.md`). El portal no
emite eventos (como el inicio de sesión del panel): sus accesos y sesiones quedan en
`auditoria.acciones` con el actor cliente. **No consume eventos.**
