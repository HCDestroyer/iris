# Notificaciones push a la app de campo (FCM)

Iris avisa al teléfono del técnico (app Android `gt.horus.iris.campo`) aunque la app esté cerrada:
mensajes del chat del equipo, menciones, órdenes asignadas o reprogramadas y «¿Alguien la atiende?»
de la bandeja. El **worker** manda directo a **Firebase Cloud Messaging HTTP v1** con una cuenta de
servicio de Google: no pasa por el servicio de push de Expo ni por ningún otro tercero. Contrato con
la app: `.claude/integration/contratos/push.md`. Código: `apps/api/internal/notificaciones`.

El push es **opcional**. Sin configurar, Iris funciona igual: los teléfonos se registran, las
preferencias se guardan y la app muestra «Notificaciones push: sin configurar».

## Qué se necesita

| Variable | Qué es |
| --- | --- |
| `IRIS_FCM_PROYECTO` | Id del proyecto de Firebase (p. ej. `iris-campo-isp`) |
| `IRIS_FCM_CREDENCIALES` | **Ruta** del JSON de la cuenta de servicio, montado como archivo de solo lectura (nunca su contenido en el `.env` ni el archivo en el repositorio) |

Las leen la API (para informar el estado en `/api/v1/salud`) y el worker (para enviar).

## 1. Crear el proyecto y la cuenta de servicio (una vez por ISP)

1. En <https://console.firebase.google.com> crea un proyecto (o usa el del ISP). Anota su **id**
   (Configuración del proyecto → General → «Id. del proyecto»).
2. Agrega una app **Android** con el paquete `gt.horus.iris.campo`. Descarga su
   `google-services.json`: es para **compilar la app** (lo pide el equipo de la app, ver
   `docs/runbooks/movil-apk.md`), no para el servidor.
3. Verifica que esté habilitada la **Firebase Cloud Messaging API (V1)** (Configuración del proyecto
   → Cloud Messaging). La API heredada no se usa.
4. Configuración del proyecto → **Cuentas de servicio** → «Generar nueva clave privada». Se descarga
   un JSON con `"type": "service_account"`. Es un secreto: quien lo tenga puede mandar push a los
   teléfonos del ISP.
   - Mejor aún: en Google Cloud → IAM, crea una cuenta de servicio solo para Iris con el rol
     **«Firebase Cloud Messaging API Admin»** (`roles/firebasecloudmessaging.admin`) y genera su clave;
     la que crea Firebase por defecto tiene más permisos de los necesarios.
5. Copia el JSON al servidor por un canal seguro (`scp`) y bórralo de tu equipo.

## 2. Producción (`install.sh` / `irisctl`)

```sh
sudo irisctl install --fcm-credentials /root/fcm-iris.json      # el id del proyecto sale del JSON
# o, si el id es otro:  --fcm-project iris-campo-isp
sudo shred -u /root/fcm-iris.json
sudo irisctl status        # «Push notifications: on (project …)»
```

El instalador:

- copia el JSON a `/opt/iris/secrets/fcm-service-account.json` con dueño `root:65532` y modo `0640`
  (la carpeta, `0750`): lo leen solo root y los contenedores (usuario `nonroot` de la imagen
  distroless, uid/gid 65532);
- monta `/opt/iris/secrets` **solo en api y worker** (no en mensajería ni en migrar), en
  `/run/iris/secrets` **de solo lectura** (`docker-compose.yml`, ancla `x-api-volumes-push`;
  `verificar.sh` lo comprueba);
- escribe en `/opt/iris/.env` `IRIS_FCM_PROYECTO=<id>` e
  `IRIS_FCM_CREDENCIALES=/run/iris/secrets/fcm-service-account.json` (la ruta **dentro** del
  contenedor) y reinicia.

**Rotar la clave:** genera una nueva en Firebase, vuelve a correr `irisctl install --fcm-credentials
<nuevo>` y luego borra la vieja en la consola de Firebase (Cuentas de servicio → administrar claves).
El worker toma la nueva al reiniciar (el instalador reinicia).

**Apagar:** `irisctl install --no-fcm` vacía las dos variables (el archivo queda; bórralo a mano si
ya no se usa).

A mano (sin el instalador): deja el archivo en `/opt/iris/secrets/` con esos permisos, pon las dos
variables en el `.env` y `irisctl start`.

## 3. Desarrollo y laboratorio (`make dev-app`)

1. Copia el JSON a `infra/secretos/fcm.json` (la carpeta está fuera de git salvo su `.gitkeep`).
2. En el `.env` de la raíz: `IRIS_FCM_PROYECTO=<id>` e
   `IRIS_FCM_CREDENCIALES=/run/iris/secrets/fcm.json`.
3. `make dev-app`. Para el API fuera de Docker (`go run ./cmd/worker`), la ruta es la del archivo
   en tu equipo.

Un proyecto de Firebase de pruebas, distinto del de producción, evita mandar push a teléfonos reales.

## 4. Comprobar

```sh
curl -s http://127.0.0.1:8080/api/v1/salud
# {"estado":"ok","version":"…","notificacionesPush":"configurado"}
```

| `notificacionesPush` | Qué pasa | Qué hacer |
| --- | --- | --- |
| `configurado` | El worker manda | — |
| `sin_configurar` | Las dos variables están vacías | Paso 2 |
| `error` | Falta una de las dos, el archivo no se puede leer, no es una cuenta de servicio, su `project_id` no es `IRIS_FCM_PROYECTO` o su `token_uri` no es el de Google (`https://oauth2.googleapis.com/token`) | `irisctl logs api` o `worker`: «notificaciones push apagadas: la configuración no sirve», con el motivo (nunca el contenido del archivo) |

Luego, en la app: «Yo» → «Notificaciones» → permitir, y mandarse un mensaje desde otro usuario del
chat. En la base, cada envío queda en `notificaciones.envios` (`estado`, `motivo`):

```sql
SELECT estado, motivo, count(*) FROM notificaciones.envios
WHERE created_at > now() - interval '1 day' GROUP BY 1, 2 ORDER BY 3 DESC;
```

| `estado` / `motivo` | Significa |
| --- | --- |
| `enviado` | FCM lo aceptó (que el teléfono lo muestre depende de Android y de la app) |
| `omitido` / `preferencia` | El usuario apagó ese tipo |
| `omitido` / `no_molestar` | Cayó en su No molestar y no era urgente (no se manda después) |
| `omitido` / `sin_permiso` | Ya no ve esa conversación u orden (salió de la cuadrilla, cambió de rol o de empresa) |
| `omitido` / `ya_no_aplica` | El mensaje se eliminó, la conversación ya se tomó o la orden pasó a otro técnico |
| `omitido` / `dispositivo_quitado` | Se cerró la sesión del teléfono, se venció o salió de la empresa |
| `fallido` / `agotado: …` | FCM respondió 429/5xx en los 8 intentos (espera de 15 s a 30 min) |
| `fallido` / `token_invalido: …` | FCM dijo `UNREGISTERED` (app desinstalada o datos borrados), o `INVALID_ARGUMENT` / `SENDER_ID_MISMATCH` señalando `message.token`: el dispositivo se quitó |
| `fallido` / `rechazado: …` | Otro 4xx: el dispositivo **no** se quita. Revisa el código (p. ej. `PERMISSION_DENIED` = la cuenta de servicio no tiene el rol o la API V1 no está habilitada; `SENDER_ID_MISMATCH` en todos = el `google-services.json` de la app es de otro proyecto) |

Los envíos se borran a los 30 días. Los registros nunca llevan el token del teléfono ni el texto de
los mensajes.

## 5. Privacidad

- La app recibe un mensaje **solo de datos** y arma la notificación. En la pantalla bloqueada los
  cuatro canales van con visibilidad **privada**: Android muestra «Iris Campo» sin título ni texto
  (la app aún no usa el `publicoTitulo` del mensaje: `expo-notifications` no permite fijar una
  versión pública propia, ver la sección 6). Cada técnico puede endurecerlo en los ajustes del canal.
- Cada usuario elige en «Yo» → «Notificaciones» si el aviso muestra el texto o solo quién escribe.
  Con «solo quién escribe», el texto no sale del servidor.
- Antes de cada envío se vuelven a comprobar la sesión del teléfono, la empresa y los permisos: a
  quien se le quita un rol, sale de una cuadrilla o de la empresa le dejan de llegar los push.

## 6. La app (APK con push)

La app necesita el `google-services.json` del **mismo** proyecto de Firebase (paso 1.2) al compilar.
Es configuración, no una llave (queda dentro del APK), pero es de cada ISP: **nunca se commitea**
(`apps/movil/.gitignore`).

1. Codifícalo en una línea: `base64 -w0 google-services.json` (en macOS, `base64 -i google-services.json`).
2. En GitHub → Settings → Secrets and variables → Actions, crea el secreto de repositorio
   **`GOOGLE_SERVICES_JSON`** con ese texto.
3. Corre «APK de Android» (`movil-apk.yml`). El job `compilar` lo valida (base64, JSON y que traiga
   la app `gt.horus.iris.campo`), lo escribe en `apps/movil/google-services.json` y `app.config.js`
   lo pasa a `android.googleServicesFile`. El resumen del job dice «Push: configurado».
4. **Sin el secreto** el APK se compila igual, **sin push**: el job lo avisa con un *warning* y
   «Push: sin configurar». En el teléfono, «Yo» → «Notificaciones» guarda las preferencias pero el
   teléfono no se registra (no hay token de FCM).

Para compilar en local, copia el archivo a `apps/movil/google-services.json` antes de
`expo prebuild`.

### Qué hace la app

- Al entrar (y al cambiar de empresa, cuando FCM rota el token y una vez al día) registra el
  teléfono con `POST /api/v1/dispositivos` y el token **nativo** de FCM (`getDevicePushTokenAsync`).
  Al cerrar sesión quita sus dispositivos (el servidor también lo hace).
- La primera vez muestra «Que Iris te avise» antes del diálogo de Android; «Ahora no» no vuelve a
  preguntar solo. Si el permiso está bloqueado, «Yo» → «Notificaciones» lo dice con «Activar» (abre
  los ajustes de Android).
- Canales de Android: «Mensajes del equipo» (`chat`), «Menciones» (`mencion`), «Órdenes» (`orden`)
  y «Bandeja de clientes» (`bandeja`).
- Una notificación por conversación u orden: los mensajes de una conversación se apilan
  («Cuadrilla Norte · 2 mensajes»), con «Responder» (respuesta en línea) y «Marcar como leído»; las
  órdenes, con «Ver orden» y «Cómo llegar». Tocar abre la pantalla (`iris://chat/…`,
  `iris://orden/…`; `iris://conversaciones/…` abre la bandeja en el panel web), también con la app
  cerrada: si no hay sesión, después de entrar, y solo si el aviso era para esa persona.
- Con la app al frente no avisa el chat que se está viendo.

**Límites de `expo-notifications`** (sin código nativo propio): no hay `MessagingStyle` (avatar e
iniciales por remitente), ni versión pública con título propio, ni `setWhen`. Hacerlo pide un *config
plugin* con un `FirebaseMessagingService` propio en Kotlin; queda para una siguiente versión, a
validar en un teléfono.
