# notificaciones

Push a la app de campo (Android, `gt.horus.iris.campo`) directo por **Firebase Cloud Messaging HTTP
v1**, sin el servicio de push de Expo. Contrato con la app: `.claude/integration/contratos/push.md`.
Operación: `docs/runbooks/notificaciones-push.md`.

## Qué hace

- **API** (`cmd/iris`): `POST /api/v1/dispositivos` (idempotente por token y empresa; el token FCM
  nunca se devuelve), `GET /api/v1/dispositivos`, `POST /api/v1/dispositivos/{id}/quitar` y
  `GET|POST /api/v1/yo/notificaciones` (preferencias de la cuenta con `version` y 409). Al cerrar
  sesión (`DELETE /sesion`) se quitan los dispositivos de esa sesión en la misma transacción (gancho
  `identidad.AlCerrarSesion`). `/api/v1/salud` informa `notificacionesPush`.
- **Worker** (`cmd/worker`): un suscriptor del outbox convierte los eventos en **envíos** (uno por
  evento y dispositivo, `notificaciones.envios`) y encola un job de River `notificaciones.enviar`
  por envío (único por argumentos). El job:
  1. vuelve a comprobar todo con los datos de ese momento (`identidad.Revalidar` de la sesión o el
     token del dispositivo, empresa, preferencias, permisos y miembros del chat o de la bandeja,
     técnico de la orden, No molestar);
  2. arma el mensaje (solo datos) y lo manda a FCM con el token OAuth de la cuenta de servicio
     (JWT RS256 propio, guardado en memoria hasta un minuto antes de vencer);
  3. 200 → `enviado`; 429/5xx o sin red → reintento con espera exponencial (15 s … 30 min, 8
     intentos) y luego `fallido`/`agotado`; otro 4xx → `fallido` sin reintento; `UNREGISTERED`, o
     `INVALID_ARGUMENT` / `SENDER_ID_MISMATCH` con el detalle en `message.token` → además quita el
     dispositivo (un error de configuración no borra los dispositivos).
- El token OAuth se pide siempre a `https://oauth2.googleapis.com/token` (un `token_uri` distinto en
  el JSON se rechaza) y el cliente no sigue redirecciones. Una cuenta de otro proyecto deja el push
  en `error`. Hasta 10 dispositivos por usuario y empresa.
- **Purga** cada hora: dispositivos cuya sesión o token ya no vale (o usuario inactivo) y envíos de
  más de 30 días.

Sin `IRIS_FCM_PROYECTO` e `IRIS_FCM_CREDENCIALES` el push queda apagado: no se crean envíos, los
dispositivos y las preferencias se guardan y `/salud` dice `sin_configurar`.

Privacidad: los envíos solo guardan referencias (ids y banderas); el texto se lee al enviar y no va
al log ni a la auditoría. La auditoría del dispositivo lleva `fin_fcm` (últimos 6 caracteres).

## Avisos del panel y correo del sistema (2.1, `contratos/conversaciones.md` §16.9)

- **Campana** (`avisos.go`, API principal): `GET /api/v1/yo/avisos` (cursor, `soloNoLeidos`),
  `GET /api/v1/yo/avisos/resumen` (`noLeidos`), `POST /api/v1/yo/avisos/{id}/leer` y
  `POST /api/v1/yo/avisos/leer-todos`; solo los del usuario en la empresa activa. Leer no audita ni
  emite (es el estado personal de la campana).
- **Tope del asistente:** el worker consume `asistente.tope_alcanzado` (también con el push apagado) y,
  para cada persona activa con `mensajeria.asistente_configurar` en esa empresa (asignada o con
  `empresas.todas` sin límite), crea un aviso y un correo. Uno por usuario y clave
  `asistente_tope:<empresa>:<mes>:<80|100>:<tope>`: una sola vez por umbral, mes y valor del tope
  aunque el evento llegue dos veces; si suben el tope y lo vuelven a cruzar, avisa de nuevo. El
  `enlace` de un aviso solo puede ser una ruta interna del panel (CHECK en la tabla).
- **Correo del sistema** (`correo.go`): un job `notificaciones.enviar_correo` por correo (6 intentos
  con espera exponencial), con el cliente SMTP de los buzones (`IRIS_CORREO_REDES_PERMITIDAS`,
  `IRIS_CORREO_CA`). En cada envío se lee por dónde sale (sin reiniciar): el ajuste de la **empresa**
  del correo → el **global** → `IRIS_SMTP_*` del **entorno** (respaldo) → ninguno. Un ajuste apagado
  se salta. Sin ninguno, el correo queda `omitido` (`sin_correo_sistema`) sin error.
- **Correo del sistema desde el panel** (`correo_sistema.go`, `rutas_correo.go`; contrato
  `.claude/integration/contratos/correo-sistema.md`): `GET /api/v1/correo-sistema/{ambito}` y
  `POST …/guardar`, `…/probar`, `…/activar`, `…/desactivar`, con `{ambito}` = `global` o el id de la
  empresa. Permisos `correo_sistema.ver` y `correo_sistema.configurar` (de fábrica solo `admin`); el
  global exige operar sin límite de empresa (`empresas.todas`) y el de una empresa, estar operando en
  ella.
  - La contraseña se cifra con `IRIS_CLAVE_CIFRADO` atada a su fila (entra en `iris clave rotar`),
    nunca se devuelve (`tieneContrasena`) y queda como `***` en la auditoría. La guardada solo se usa
    hacia el mismo servidor, puerto y usuario: si cambian, hay que volver a escribirla.
  - Guarda de red de los buzones en la validación, en la prueba y en cada envío; puertos 25, 465,
    587 y 2525; TLS verificado. `seguridad: ninguna` solo hacia un servidor dentro de
    `IRIS_CORREO_REDES_PERMITIDAS` y sin usuario ni contraseña (lo exige también `soporte/correo` y
    un CHECK de la tabla).
  - «Probar» manda un correo de asunto y cuerpo fijos a una sola dirección, sin guardar: 5 por
    usuario y 20 por instalación cada hora (429), y responde una causa de lista cerrada en lenguaje
    llano, nunca la respuesta cruda del servidor.
  - Remitente, nombre, usuario, host y destino se rechazan con CR, LF u otros caracteres de control.
- Tablas `notificaciones.avisos`, `notificaciones.correos`, `notificaciones.correo_ajustes` (una fila
  por ámbito) y `notificaciones.correo_pruebas` (tope y última prueba; sin host, destino ni
  credenciales); la purga de cada hora borra los de más de 180 días.

## Eventos

- **Emite:** `dispositivo.registrado`, `dispositivo.quitado`, `notificaciones.preferencias_cambiadas`
  (payloads en `contratos/eventos.md`, «Notificaciones push»); `notificaciones.avisos_creados`,
  `notificaciones.correo_enviado` y `notificaciones.correo_fallido` (avisos del panel).
- **Consume:** `chat.mensaje_enviado`, `chat.mencion` (cuando el chat tenga menciones),
  `conversacion.mencion`, `conversacion.aviso_sin_atender`, `orden.asignada`, `orden.programada` y
  `asistente.tope_alcanzado` (avisos del panel).

## Tablas (`notificaciones`)

`dispositivos` (token, hash y final; sesión o token de API; empresa), `preferencias` (una por
usuario, con `version`) y `envios` (único por evento y dispositivo, y por `clave` para unir los
eventos de una misma acción: asignar y reprogramar una orden a la vez da un solo push).
