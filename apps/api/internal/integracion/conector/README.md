# integracion/conector

Base común de los conectores con otros sistemas (ArgosISP y Horus Flow), con los mecanismos del
contrato Argos ↔ Horus §1 que reutilizan los dos. Cada sistema se agrega al `Nucleo` con su
`Sistema` (nombre, prefijo del token, ruta del canje remoto, redes permitidas, ganchos) y recibe
una `Conexion` con sus casos de uso. El núcleo lo crea `argos.Nuevo`; `horus.Nuevo` se agrega a él.

## Qué hace

- **Enlace de conexión en los dos sentidos** (`conexion.go`) bajo `/api/v1/integraciones/<sistema>/`:
  `conectar` (enlace del otro sistema: Iris canjea en `Sistema.RutaCanje` con el cuerpo de Horus §1.4,
  `{ codigo, sistema, base_url, token, webhook: { url, secreto } }`; el `iris_pat_` lo emite
  `Tokens` (identidad, fijado con `Nucleo.FijarTokens`; sin él, 422 `emisor_token_no_configurado`)),
  `conexion/previsualizar` (origen del enlace y si está permitido, sin llamar afuera), `conexion/enlaces`
  (enlace de Iris, 15 min, un solo uso) y `conexion/canjear` (lo llama el otro sistema; 401
  `codigo_invalido`; `sistema` debe ser el de la ruta). Token y secreto del webhook cifrados con
  `IRIS_CLAVE_CIFRADO`, atados a la fila; nunca salen por la API ni en la auditoría. Nace apagado.
- **Conectar con un botón** (`autorizacion.go`, contrato Iris ↔ ArgosISP 0.4.0 §1.4; solo sistemas con
  `Sistema.RutaAutorizar`, hoy Argos): `POST …/autorizaciones` (`integraciones.autorizar`, permiso
  `integraciones.conectar`, solo con sesión del panel: 422 `sesion_requerida`) normaliza el panel a
  `https://host[:puerto]`, guarda en `integracion.autorizaciones` el SHA-256 del `state` (32 bytes, 10 min, un
  uso, ligado a sesión y usuario) y el `code_verifier` cifrado y atado a la fila (rotado por `iris clave rotar`;
  se borra al usarse), y devuelve la URL de autorizar con el `code_challenge` S256 (respuesta no repetible por
  `Idempotency-Key`). `POST …/autorizaciones/completar` (`integraciones.completar_autorizacion`) verifica y gasta
  el `state` (422 `state_invalido` si no existe, ya se usó o es de otra sesión —esta última sin gastarlo—;
  422 `state_vencido`), con `error` responde 422 `rechazado`, exige `api` en `Sistema.Origenes` (sin la
  excepción del conector activo; 422 `origen_no_permitido` / `origenes_no_configurados`) y canjea con
  `code_verifier` por el mismo camino que el enlace (`canjearYGuardar`: token con vencimiento, revocado si
  falla). Un 401 o `codigo_invalido` de Argos es 422 `codigo_invalido`. El conector nace apagado con origen
  `autorizacion`.
- **Panel y API por separado** (0.4.0): `panel_url` del canje (en los dos flujos; si falta en §1.4, el panel
  que escribió el administrador) va en `integracion.conectores.panel_url`; la API sigue en `base_url`. La API
  expone `panelUrl`, `apiUrl` y `urlPublica` (la configurada a mano, si no `panelUrl`, si no la API), que es la
  que usan los enlaces «Ver en Argos».
- **Token entregado en el canje** (revisión de seguridad de la PR #50): solo para orígenes de
  `Sistema.Origenes` (`IRIS_ARGOS_ORIGENES`, `IRIS_HORUS_ORIGENES`, obligatoria también con
  `PermitirHTTP`, que solo permite http) o el del conector **activo** del sistema (uno apagado o un
  intento sin activar no hereda confianza); 422 `origenes_no_configurados` / `origen_no_permitido`,
  también en el receptor `conexion/canjear`. Exige `empresas.todas` global (403) y se emite con el actor real.
  Nace con vencimiento de 15 min (`VigenciaTokenCanje`); al activar el conector se confirma (sin
  vencimiento; vencido: 422 `token_entregado_vencido`). Queda en `integracion.conectores.token_entregado_id`
  y se revoca si el canje no se completa (`defer`, también ante un pánico), al canjear otro intento
  (solo los de conectores no activos), al activar otro del mismo sistema (el activo y los intentos
  sobrantes) y al desactivar. Si la revocación falla, el job `integracion.revocar_token_entregado`
  (único por token) la reintenta. `conector_id` de la respuesta: entero positivo o `[A-Za-z0-9_-]{1,64}`.
- **Rutas comunes por id** (`rutas.go`, las publica `argos.Registrar`): `GET /integraciones/conectores`,
  `POST /integraciones/conectores/{id}/activar|desactivar` (uno activo por sistema: activar uno
  reemplaza al activo, que queda apagado con `conector.desactivado` motivo «reemplazado…» y su token
  revocado, en la misma transacción; así reconectar no corta la integración; `Sistema.AlActivar` corre
  en la misma transacción),
  `POST /integraciones/conectores/{id}/url-publica` (`integraciones.conectar`) y
  `GET /integraciones/enlaces` (`clientes.ver`, sin empresa activa: URL pública de cada sistema activo, para
  «Ver en Argos/Horus»).
- **Webhooks** (`webhook.go`): `POST /integraciones/<sistema>/webhook/{conector_id}`, máximo 1 MiB,
  HMAC-SHA256 en tiempo constante sobre el cuerpo crudo, firma antes que ventana, `id` del cuerpo
  firmado, obligatorio e igual a `X-Webhook-Id`, deduplicación por él, `integracion.entrantes` y el
  job del sistema (`Sistema.TrabajoEntrante`) en la misma transacción. Los 401 anónimos no se
  auditan (se cuentan y van al log con muestreo). `TomarEntrante` / `MarcarEntrante` ayudan al job
  de cada sistema a ser idempotente.
- **Cliente HTTP** (`cliente.go`) y **SSRF** (`red.go`): `net.Dialer` que bloquea IPs internas ya
  resueltas (salvo las redes permitidas del sistema), sin proxy ni redirecciones; problem+json
  mapeado a `<codigo>_no_disponible|_sin_permiso|_rechazo` sin filtrar el `detail` del otro lado.

## Eventos

Emite `conector.creado`, `conector.activado`, `conector.desactivado` y `conector.actualizado`
(URL pública), con `sistema`. No consume eventos.
