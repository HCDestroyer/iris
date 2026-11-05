# integracion/conector

Base común de los conectores con otros sistemas (ArgosISP y Horus Flow), con los mecanismos del
contrato Argos ↔ Horus §1 que reutilizan los dos. Cada sistema se agrega al `Nucleo` con su
`Sistema` (nombre, prefijo del token, ruta del canje remoto, redes permitidas, ganchos) y recibe
una `Conexion` con sus casos de uso. El núcleo lo crea `argos.Nuevo`; `horus.Nuevo` se agrega a él.

## Qué hace

- **Enlace de conexión en los dos sentidos** (`conexion.go`) bajo `/api/v1/integraciones/<sistema>/`:
  `conectar` (enlace del otro sistema: Iris canjea en `Sistema.RutaCanje` con el cuerpo de Horus §1.4,
  `{ codigo, sistema, base_url, token, webhook: { url, secreto } }`, más `contrato` y, con
  `Sistema.ExigirReceptor` (Argos, contrato 0.6.2), `receptor_argos: true`; con `ExigirReceptor`, un
  canje cuya respuesta no trae `webhook` es 422 `webhook_invalido` (Horus no entrega receptor y sigue
  igual); el `iris_pat_` lo emite
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
  `autorizacion`. `api` debe ser un origen sin ruta (422 `validacion`) y el canje va al origen exacto que
  coincidió con la lista. Iniciar otra en la misma sesión anula las anteriores sin usar; el job periódico
  `integracion.autorizaciones_purga` (único por periodo, cada 10 min) borra el `code_verifier` de las vencidas
  y las filas vencidas hace más de un día. `panel_url` siempre se guarda como origen.
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
- **Token del otro sistema en un canje no completado** (`revocar_canje.go`, contrato Iris ↔ ArgosISP
  0.6.3 §1.3; solo sistemas con `Sistema.RutaRevocarCanje`, hoy Argos): si el otro sistema respondió 200
  al canje (Iris tiene su `atk_`) pero Iris no lo completa (422 `webhook_invalido`, `conector_id`
  inválido, error al guardar, pánico), el mismo `defer` que revoca el `iris_pat_` encola
  `integracion.revocar_canje_remoto` en una transacción propia (la del canje se deshizo). El job lleva
  sistema, id de conector reservado, base del canje y el token **cifrado** con la clave de los conectores,
  atado al conector y a la base (`DatosTokenCanje`): nunca en claro en `river.river_job` ni en el log, y
  si alguien cambia `base_url` en el job, no descifra y el job se cancela sin llamar a nadie. Único
  por sistema y conector. Llama a `POST <base>/api/v1<RutaRevocarCanje>` con el token como Bearer y sin
  cuerpo, por el cliente con protección contra SSRF: 204 o 401 terminan; 403 u otro 4xx (también
  destino bloqueado o token ilegible) quedan cancelados en el log sin reintento; 5xx, 429 y sin respuesta
  se reintentan, máximo `IntentosRevocarCanje` (10): el 429 espera su `Retry-After` si viene (hasta 6 h,
  `NextRetry` del worker) y si no, como los demás, la espera de River. Un canje que no respondió 200 no
  tiene token y no encola nada; Iris no repite un canje perdido por timeout (el `atk_` sin uso vence solo
  a las 24 h). Lo registra `Nucleo.RegistrarRevocarCanje` (desde `argos.RegistrarWorkers`).
- **Rutas comunes por id** (`rutas.go`, las publica `argos.Registrar`): `GET /integraciones/conectores`,
  `POST /integraciones/conectores/{id}/activar|desactivar` (uno activo por sistema: activar uno
  reemplaza al activo, que queda apagado con `conector.desactivado` motivo «reemplazado…» y su token
  revocado, en la misma transacción; así reconectar no corta la integración; `Sistema.AlActivar` corre
  en la misma transacción). Con `Sistema.ConfirmarReconexion` (Argos 0.6.3, `reconexion.go`) el token
  del activo reemplazado **no** se revoca al activar: el conector nuevo guarda `reemplaza_conector_id` y
  `reemplaza_token_id`, y los webhooks firmados con el secreto del anterior (misma base) se aceptan y se
  guardan en el nuevo (si ese `X-Webhook-Id` ya está en el viejo o en el nuevo, es un duplicado). Con
  `revocarAnterior: true` en `activar` (ante una filtración) el token del reemplazado se revoca en el
  acto y no queda nada diferido. La primera respuesta 2xx de una llamada con el token nuevo (cualquiera del
  conector activo; el cliente avisa una vez) lo confirma: `integraciones.confirmar_reconexion` revoca
  ese token y limpia las columnas. Si nunca llega, el job `integracion.vencer_reemplazo_conector`
  (programado a 24 h, único por conector reemplazado y nuevo) lo revoca (`integraciones.vencer_reconexion`;
  no si el anterior volvió a activarse). Desactivar el nuevo antes de confirmar revoca también el del
  anterior. Horus sigue revocando al activar. Ni el canje ni la activación llaman al otro sistema con el token nuevo:
  todas las llamadas salen del conector **activo** (`Activo`, `PorID` exige `activo`), así que el
  token nuevo se usa por primera vez cuando el conector nuevo ya es el activo (con Argos 0.6.3, eso
  confirma la reconexión allá),
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
