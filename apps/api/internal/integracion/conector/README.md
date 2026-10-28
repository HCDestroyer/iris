# integracion/conector

Base común de los conectores con otros sistemas (ArgosISP y Horus Flow), con los mecanismos del
contrato Argos ↔ Horus §1 que reutilizan los dos. Cada sistema se agrega al `Nucleo` con su
`Sistema` (nombre, prefijo del token, ruta del canje remoto, redes permitidas, ganchos) y recibe
una `Conexion` con sus casos de uso. El núcleo lo crea `argos.Nuevo`; `horus.Nuevo` se agrega a él.

## Qué hace

- **Enlace de conexión en los dos sentidos** (`conexion.go`) bajo `/api/v1/integraciones/<sistema>/`:
  `conectar` (enlace del otro sistema: Iris canjea en `Sistema.RutaCanje`), `conexion/enlaces`
  (enlace de Iris, 15 min, un solo uso) y `conexion/canjear` (lo llama el otro sistema; 401
  `codigo_invalido`; `sistema` debe ser el de la ruta). Token y secreto del webhook cifrados con
  `IRIS_CLAVE_CIFRADO`, atados a la fila; nunca salen por la API ni en la auditoría. Nace apagado.
- **Rutas comunes por id** (`rutas.go`, las publica `argos.Registrar`): `GET /integraciones/conectores`,
  `POST /integraciones/conectores/{id}/activar|desactivar` (uno activo por sistema, 409
  `otro_conector_activo`; `Sistema.AlActivar` corre en la misma transacción),
  `POST /integraciones/conectores/{id}/url-publica` (`integraciones.conectar`) y
  `GET /integraciones/enlaces` (cualquier autenticado: URL pública de cada sistema activo, para
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
