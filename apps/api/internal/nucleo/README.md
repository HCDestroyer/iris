# nucleo

Piezas comunes de la API: errores RFC 9457 (`Problema`), transacciones (`EnTx`), paginación por
cursor, `version` → 409, `Idempotency-Key`, cifrado de secretos y el catálogo único de permisos
(`permisos.go`). **Rutas con secretos** (`rutas_secretas.go`): las rutas públicas con un secreto
en un segmento (hoy `/api/v1/encuestas/publica/{token}` y sus subrutas) se registran como
plantilla (`RutasConSecretoBase` o `RegistrarRutaConSecreto` desde el módulo); `EnmascararRuta`
las deja con `***` y es lo único que debe escribir una ruta en logs, trazas o métricas. El log de
acceso escribe `ruta` (escapada y enmascarada, sin query) y `patron` (el de chi, sin valores).
**IP del cliente** (`ip.go`): `IPCliente` resuelve `X-Forwarded-For` solo desde
`IRIS_PROXIES_CONFIABLES`; el servidor reescribe `RemoteAddr` una vez por petición y todos leen la IP con
`HostDe(RemoteAddr)`.
Subpaquetes:

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
  `nucleo.portal_purga` (worker, cada hora) borra también los tickets de autologin vencidos.
  **Autologin desde Argos** (`sso.go`, «Ver en Iris»): Argos pide `POST /integraciones/argosisp/sso/tickets`
  (`sso.emitir`, solo con el `iris_pat_` entregado al conector activo; 60 s, un uso, SHA-256); la
  página `/sso/entrar` canjea (`POST /sso/canjear`): con vínculo abre la sesión (pendiente de MFA
  si lo tiene; TOTP obligatorio para los privilegiados; sesión de origen `sso`, 8 h, con
  re-autenticación `POST /sesion/reautenticar` para las acciones sensibles), sin vínculo deja la
  cookie `iris_sso_vincular` y, tras un login posterior al canje y la confirmación del usuario,
  `POST /sso/vincular { id_externo }` guarda el vínculo si el correo de Argos es el de la cuenta (si
  no, lo crea un admin). Vínculos: `GET|POST /usuarios/{id}/vinculos-sso` y
  `POST /usuarios/{id}/vinculos-sso/{vinculoId}/borrar`; se borran al desactivar al usuario o
  reiniciar su MFA o su contraseña. El job
  `nucleo.portal_purga` (worker, cada hora) borra las sesiones vencidas o revocadas y los tickets
  vencidos sin sesiones 30 días después. **Integraciones**
  (`integracion.go`): roles base `integracion_argos` / `integracion_horus` y
  `iris integracion crear --sistema argosisp|horus --empresa …` (usuario + token).
- `identidad` · **historial de accesos** (`accesos.go`, tabla `nucleo.accesos`): una fila por
  inicio de sesión (éxito al quedar la sesión completa, también tras el MFA; `contrasena_erronea`,
  `mfa_fallido`, `bloqueado`, `inactivo`) y por uso de un token (como mucho una por token y hora),
  con canal (`panel`, `campo_pwa`, `campo_nativa`, `sso`, `token`), IP, dispositivo resumido y la
  cabecera `X-Iris-App`. Un correo que no existe queda sin usuario y **sin el correo** (no sirve
  para enumerar cuentas; la respuesta del login es la misma). Retención:
  `IRIS_ACCESOS_RETENCION_DIAS` (180, mínimo 7), job `nucleo.accesos_purga` del worker cada hora.
  `GET /accesos`, `GET /usuarios/{id}/accesos` (`accesos.ver`), `GET /usuarios/{id}/sesiones`
  y `POST /usuarios/{id}/sesiones/{sesionId}/cerrar` | `/cerrar-sesiones`
  (`usuarios.cerrar_sesiones`, exige superar al usuario salvo el propio).
  **Actividad de la app de campo** (`actividad_campo.go`): el middleware guarda en
  `nucleo.conexiones_app` la última conexión por usuario y plataforma con `X-Iris-App:
  campo/<versión> (<plataforma>)` y `X-Iris-Pendientes: <n>` (a lo más cada 30 s);
  `GET /actividad-campo` (`campo.ver_actividad`, operativo: empresa activa y alcance) junta eso con
  `campo.dispositivos` (último lote), `campo.operaciones_sync` (rechazadas, 7 días) y
  `campo.posiciones_ultimas`; estado `al_dia` / `atrasado` (pendientes y sin lote en 15 min) /
  `sin_conexion` (nada en 30 min). **Roles propios** (`roles.go`): `POST /roles`,
  `/roles/{id}/editar|activar|desactivar`, `POST /usuarios/{id}/asignar-rol|quitar-rol` con alcance
  por zona o cuadrilla. Los base no se editan ni desactivan (422 `rol_base`) y ningún rol se borra;
  un rol inactivo no da permisos. Dar un permiso sensible (`nucleo.PermisosSensibles`, también por
  `usuarios.crear` y `usuarios.editar`) exige `POST /sesion/reautenticar` en los últimos 15 min
  (403 `reautenticacion_requerida`; desde un token, 403 `requiere_sesion`), salvo el usuario de
  integración con su rol de contrato. `GET /permisos` trae `modulo`, `moduloNombre` y `sensible`.
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
sesiones, `cliente:<empresa>:<cliente>` para el actor cliente), vigentes 24 h. Un rechazo de autorización antes de ejecutar (401/403/empresa por
elegir, `nucleo.AutorizacionFallida`) libera la clave si nada se confirmó. Las respuestas con secretos (`POST /tokens`, `POST /yo/mfa`) no se
guardan: repetir la clave da 409 `idempotencia_no_repetible`. Una clave abandonada
`en_proceso` se retoma tras `IRIS_IDEMPOTENCIA_RECLAMO` (2 min).

**Eventos que emite** (`identidad`, con `empresa_id` null): `usuario.creado`,
`usuario.actualizado`, `usuario.desactivado`, `usuario.mfa_reiniciado` (`POST /usuarios/{id}/reiniciar-mfa`,
solo admin, nunca el propio: borra el TOTP, pone una contraseña temporal que hay que cambiar con
`POST /yo/contrasena` antes de cualquier otra cosa, cierra sesiones y revoca tokens; responde el `Usuario` con `sesionesCerradas` y `tokensRevocados`), `token.creado`, `token.revocado`; y, con la
empresa afectada, `empresa.creada`, `empresa.actualizada`, `usuario.empresa_asignada`,
`usuario.empresa_quitada`, `empresa.datos_adoptados`; y, del autologin desde Argos (`sso.go`),
`usuario.sso_vinculado` y `usuario.sso_desvinculado`; de accesos y roles (`accesos.go`, `roles.go`),
`usuario.sesiones_cerradas`, `usuario.rol_asignado`, `usuario.rol_quitado`, `rol.creado`,
`rol.actualizado`, `rol.activado` y `rol.desactivado` (payloads en `.claude/integration/contratos/eventos.md`). El portal no
emite eventos (como el inicio de sesión del panel): sus accesos y sesiones quedan en
`auditoria.acciones` con el actor cliente. **No consume eventos.**
