# Plantillas de WhatsApp (servidor ↔ panel)

Contrato interno de Iris (no es espejo de otro sistema). Servidor: `apps/api/internal/mensajeria/plantillas_wa_*.go`,
tablas `mensajeria.plantillas_whatsapp*`. Panel: `/administracion/plantillas-whatsapp` (la ruta
`/administracion/plantillas` es de los formularios de campo), con entrada desde «Números de WhatsApp».
Maqueta aprobada por el PM (pantallas a lista, b nueva, c detalle, d sugeridas, e errores).

## 1. Decisiones del PM

1. **Quién.** Crear, editar, borrar y enviar a Meta: solo administración (`plantillas_whatsapp.crear`,
   `.editar`, `.borrar`, en `admin`). Ver y sincronizar: también `supervisor` y `soporte`
   (`plantillas_whatsapp.ver`, `.sincronizar`). `plantillas.*` sigue siendo de los formularios de campo.
   Solo personas con sesión: un token de API o una integración recibe 403 (sin el permiso) o 404.
2. **Por cuenta.** Las plantillas viven por cuenta de WhatsApp (WABA), no por número. La empresa dueña
   de la cuenta decide en qué **usos** se aplica cada una (`avisos`, `conversaciones`, `asistente`) y qué
   **códigos** del catálogo de Iris atiende (`codigos[]`: una plantilla puede atender varios; cada
   código, una sola plantilla de la cuenta). Los números prestados (§15.3 de `conversaciones.md`) usan
   las de la dueña, como hoy.
   **Una cuenta es de una sola empresa** (`mensajeria.plantillas_whatsapp_cuentas.empresa_id`): la
   primera que registra un número con esa WABA. Otra empresa no puede registrar ni pasar un número a
   esa WABA (409 `waba_de_otra_empresa`, solo si la WABA es nueva o cambia); si la dueña ya no tiene
   números activos en la cuenta (uno desconectado por Meta sigue contando), pasa a la nueva y las plantillas de la anterior quedan archivadas
   (`DELETED`, `borradaPor = sistema`, con sus códigos y usos en el historial y el evento). Activar un
   número solo reclama una cuenta sin dueña; nunca la cambia. La WABA de un número manual se comprueba en Meta con el token
   del número antes de abrir la transacción (`GET /{waba}/phone_numbers` debe traer su phone number
   ID; si no, 422 `waba_no_coincide`). Un número QR no cambia de WABA por el formulario (422
   `numero_qr`: se reconecta con QR). Un número de otra empresa que ya estaba en la cuenta (de antes)
   sigue con su mapa propio. Todas las lecturas filtran por la empresa activa y la dueña.
3. **Borradas en Meta.** Lo que Meta borra NO se borra en Iris: queda `estado = DELETED`,
   `borradaPor = meta`, con su historial, su código y sus usos; si se usaba, la lista la marca
   `atender = borrada_en_uso` para elegir otra. Borrar desde Iris pide confirmación (panel), la borra en
   Meta por su id (solo ese idioma) y, si estuvo aprobada, el nombre queda bloqueado 30 días
   (`nombreLibreEn`). Borrada desde Iris suelta su código y sus usos.
4. **Sincronizar.** En vivo por el webhook (`message_template_status_update`, `template_category_update`
   y `message_template_quality_update`), una relectura cada noche (job de River
   `mensajeria.sincronizar_plantillas`, único por cuenta) y el botón «Sincronizar con Meta».
5. **Marketing.** Permitido, configurable por empresa y **apagado de fábrica**
   (`POST /plantillas-whatsapp/ajustes { marketingPermitido }`). Sin activarlo, crear una de marketing o
   ponerle un uso responde 422 `marketing_no_permitido`. El panel avisa que Meta cobra más. Si Meta
   reclasifica una de servicio a marketing, queda `categoriaAnterior` y `atender = reclasificada`
   (`categoriaProxima` y `reclasificacion_proxima` cuando Meta avisa que lo hará).

## 2. Contrato con Argos (sin cambios)

Argos sigue pidiendo avisos por **código** (`argosisp.md` §6.2 y §6.3). El código de Iris se resuelve
así: el número de avisos de la empresa → su cuenta (WABA) → la plantilla de la cuenta con ese
código en `codigos`, de la misma empresa, aprobada (`APPROVED` o `FLAGGED`), sin pausa vigente, con
el uso `avisos` y, si es de marketing, con marketing activado. Si no hay, el
aviso queda `bloqueado / canal_no_disponible` (como hoy). Un número **sin** cuenta (`wabaId` vacío)
sigue usando su mapa propio `mensajeria.canales.plantillas`. `GET /mensajeria/plantillas` (Argos)
responde igual, ahora calculado con las plantillas de la cuenta.

Las conversaciones (iniciar, responder fuera de la ventana) usan las del uso `conversaciones`; las
boletas, cualquiera con su código. Una plantilla con datos del cliente de Argos (`cliente_nombre`,
`cliente_codigo`, `plan`) solo se manda en una conversación con el cliente **confirmado**; si no,
422 `cliente_sin_confirmar` (el agente confirma el cliente o elige otra plantilla). Lo mismo para lo
automático: el aviso de boleta fuera de la ventana no sale (`sin_aviso`) y un mensaje en cola que
llegaría sin esos datos falla con el motivo `cliente_sin_confirmar` en `conversacion.mensaje_fallido` (nunca sale con datos vacíos).

## 3. Endpoints (`/api/v1/mensajeria/plantillas-whatsapp`)

| Método y ruta | Permiso | Qué hace |
| --- | --- | --- |
| `GET /` | ver | Lista `{ data, nextCursor, conteos }`. Filtros: `wabaId`, `grupo` (`activas`, `revision`, `atender`), `categoria`, `idioma`, `buscar`, `borradas`, `cursor`, `limit`. Las borradas en Meta que se usaban salen siempre. |
| `GET /datos` | ver | Diccionario de datos amigables (§6). |
| `GET /cuentas` | ver | Cuentas de la empresa: `wabaId`, `numeros[]`, `nombres[]`, `sincronizadaEn`, `resumen` (última sincronización) y `error` (`{codigo, en}`, null si salió bien). |
| `POST /cuentas/{waba}/sincronizar` | sincronizar | Encola la relectura (job único por cuenta) y responde **202** `{ wabaId, sincronizadaEn }`. El panel vuelve a leer `GET /cuentas` para ver el `resumen`. Si la cuenta se sincronizó hace menos de 2 minutos: 429 `sincronizada_hace_poco` con `Retry-After` y `disponibleEn`. |
| `GET /sugeridas?wabaId=` | ver | Las 7 de fábrica con la que ya la cubre (`plantilla`) y `faltan`. |
| `POST /sugeridas/{clave}/crear` | crear | `{ wabaId, idioma?, pie?, enviar? }` → 201 `PlantillaCreadaPWA` (con `errorEnvio` si se pidió enviar y Meta no la aceptó; queda borrador). |
| `GET /ajustes`, `POST /ajustes` | ver / editar | `{ marketingPermitido, version }` (version 0 = de fábrica). |
| `POST /validar` | ver | Lista «Antes de enviar a Meta» sin guardar: `{ ok, formato, reglas[] }` (§4). |
| `POST /` | crear | Crea un **borrador** (`enviar: true` además lo manda; `errorEnvio` si falla). `nombre` vacío = se propone desde `etiqueta`. |
| `GET /{id}` | ver | Ficha con `historial[]` (100 últimas) y `prestatarias[]`. |
| `POST /{id}/actualizar` | editar | Edita un borrador (`version`). 409 `no_es_borrador` si ya está en Meta. |
| `POST /{id}/enviar` | crear | Manda el borrador a Meta (§4). Queda `PENDING`; el resultado llega por el webhook. |
| `POST /{id}/editar` | editar | «Corregir y volver a enviar» una `APPROVED`, `REJECTED` o `PAUSED`. Aprobada: 1 cada 24 h y 10 en 30 días (422 `limite_ediciones` con `disponibleEn`), sin cambiar la categoría (422 `categoria_aprobada`). Queda `PENDING`. |
| `POST /{id}/usos` | editar | `{ usos[], codigos[], variables?[{nombre, dato}], version }`. Si otra plantilla de la cuenta tenía alguno de esos códigos, lo suelta. 422 `datos_no_disponibles` si el aviso del código no trae algún dato. |
| `POST /{id}/borrar` | borrar | Borra en Meta por `hsm_id` + `name` (un borrador, solo en Iris). 422 `deshabilitada_no_se_borra`; 409 `sin_sincronizar` (migrada sin id de Meta). |
| `POST /{id}/reanudar` | editar | `POST /{template_id}/unpause` en Meta. |

Toda acción que cambia algo acepta `Idempotency-Key` y lleva `version` (409 `conflicto_version`).

### Errores propios

| Código | Status | Cuándo |
| --- | --- | --- |
| `nombre_en_uso` | 409 | Mismo nombre e idioma en la cuenta (en Iris o Meta 100/2388024). Trae `sugerencia`. |
| `nombre_bloqueado` | 409 | Una aprobada con ese nombre se borró hace menos de 30 días. `sugerencia` y `disponibleEn`. |
| `reglas_meta` | 422 | Falla una regla de §4; cada problema va en `errors[]` (`field` = dónde, `rule` = la regla). |
| `marketing_no_permitido` | 422 | §1.5. |
| `limite_ediciones` | 422 | `disponibleEn`. |
| `meta_sin_permiso` | 424 | El token no tiene `whatsapp_business_management` (o Meta responde 200/10/3). Se comprueba con `GET /me/permissions` antes de crear, editar o leer. La cuenta guarda el error. |
| `meta_token_invalido` | 424 | Meta 190. La cuenta guarda el error. |
| `meta_no_responde` | 502 | Red, plazo (15 s) o 5xx. No cambia nada. La cuenta guarda el error. |
| `meta_limite` | 429 | Meta pide esperar (4, 17, 32, 613, 80007, 80008, 130429). |
| `meta_rechazo` | 422 | Otro rechazo de Meta: `detail` lleva su `error_user_msg` recortado (sin él, un texto genérico; nunca el `message` técnico). |
| `cuenta_no_encontrada` | 404 | Ningún número activo de la empresa es de esa WABA (o la cuenta es de otra empresa). |
| `waba_de_otra_empresa` | 409 | Formulario de números: la WABA ya es de otra empresa con números. |
| `waba_no_coincide` | 422 | Formulario de números: Meta dice que el phone number ID no es de esa WABA. |
| `sincronizada_hace_poco` | 429 | Botón «Sincronizar» antes de 2 minutos; `Retry-After`. |
| `cliente_sin_confirmar` | 422 | Plantilla con datos del cliente en una conversación sin cliente confirmado. |
| `cuenta_sin_token`, `sin_app_meta` | 422 | Sin token guardado; encabezado con archivo sin app ID en «Conexión con Meta». |

## 4. Reglas de Meta revisadas antes de enviar

`POST /validar` y `/enviar` (y `/editar`) revisan: nombre `^[a-z0-9_]+$` ≤ 512 y libre en la cuenta e
idioma; cuerpo obligatorio ≤ 1024 (en verificación lo pone Meta); encabezado texto ≤ 60 con un dato,
o imagen/video/documento con archivo de ejemplo (subido antes como adjunto, entidad
`plantilla_whatsapp`, id = WABA; Iris lo sube a Meta por la carga reanudable) o ubicación; pie ≤ 60
sin datos; botones ≤ 10 con texto ≤ 25 (página ≤ 2, URL ≤ 2000 y el dato solo al final; llamar ≤ 1,
teléfono ≤ 20; copiar código ≤ 1, ejemplo ≤ 20); respuestas rápidas juntas; cada dato con ejemplo;
datos con nombre (`{{cliente_nombre}}`) o por posición (`{{1}}`…, en orden), sin mezclar; el mensaje
no empieza ni termina con un dato y no hay dos datos pegados; verificación sin enlaces, archivos ni
emojis y ≤ 15 datos; marketing activado. Lo que Iris no puede saber antes (calidad, 100 por hora,
250/6 000 por cuenta) lo dice Meta al enviar.

## 5. Sincronización y webhook

- **Webhook.** Llega al mismo `POST /mensajeria/canales/{id}/webhook` del número (firmado), se guarda
  en `mensajeria.entrantes` (deduplicado por el SHA-256 del cuerpo, como todo lo de Meta) y se procesa
  en el job `mensajeria.procesar_entrante`. Solo cuenta si `entry[].id` es la WABA del número y el
  número es de la empresa dueña de la cuenta. **Orden y repetidos:** con `entry[].time`, un cambio de
  estado más viejo que el último aplicado (`meta_evento_en`, reloj de Meta; nunca `estadoEn`) no se
  aplica y encola la relectura de la cuenta (y la relectura no pisa una fila que recibió un aviso
  mientras se leía: guarda su `meta_evento_en` y su versión antes de leer y la salta si cambió; solo da
  por «Borrada en Meta» una que estaba en esa foto y nadie cambió, nunca un envío hecho durante la
  lectura), y cada aviso se
  aplica una vez por
  `(waba, message_template_id, campo, evento, time)` (`mensajeria.plantillas_whatsapp_avisos_meta`),
  aunque llegue por varios números de la cuenta (se guardan 30 días). `other_info.title`/`description` se recortan. Una
  plantilla que Iris no conoce encola la relectura de la cuenta. `PAUSED` con `FIRST_PAUSE` /
  `SECOND_PAUSE` deja `pausadaHasta` (+3 h / +6 h); `REINSTATED` = `APPROVED`; `UNARCHIVED` relee;
  `REJECTED` guarda `reason` y `rejection_info`.
  **Falta del lado de Meta (líder y PM):** suscribir la app a esos tres campos.
- **Noche.** Entre las 2:00 y las 5:00 (Guatemala) el mantenimiento encola
  `mensajeria.sincronizar_plantillas { waba_id, origen }` (único por `waba_id` cada 2 min) para las
  cuentas no sincronizadas en 20 h (y sin error en ese lapso).
- **Lectura.** `GET /{waba}/message_templates?fields=…&limit=100` siguiendo `paging.cursors.after`
  (nunca la URL `next`). Lo nuevo entra con `origen = meta` y sin usos; lo migrado sin id se empareja
  por nombre e idioma; lo que Meta ya no devuelve queda «Borrada en Meta».

## 6. Datos (variables amigables) y envío

`GET /datos`: `clave`, `etiqueta` («Nombre del cliente»), `origen` («Cliente en Argos · nombre»),
`ejemplo` y `codigos[]` (los avisos que lo traen). De Argos se leen de la instantánea
(`cliente_nombre`, `cliente_codigo`, `plan`, `empresa_nombre`); del aviso, sus variables y los
derivados (`monto_con_moneda` = «Q 250.00», `fecha_corte` = «viernes 28 de noviembre»). Los de
órdenes y verificación aún no los llena ningún envío.

Cada plantilla guarda `parametros[]` (`componente`, `boton`, `nombre`, `dato`): al enviar, cada variable
de Meta recibe su dato (con `parameter_name` si son con nombre). Si falta uno, el envío falla (Meta
rechaza un parámetro vacío). Las migradas sin mapa se mandan como antes (variables del catálogo por
posición).

## 7. Migración

La migración `20261121065851_mensajeria_plantillas_whatsapp` copia `mensajeria.canales.plantillas` de
los números **activos y conectados** con WABA a `mensajeria.plantillas_whatsapp` (`origen = migrada`,
`APPROVED`, `POSITIONAL`): primero fija la dueña de cada cuenta (la empresa de su número de avisos o,
si no, de su número más antiguo) y solo copia lo de los números de la dueña. Una fila por nombre e
idioma; un nombre que atendía varios códigos los sigue atendiendo todos, y cada código queda en una
sola plantilla (primero la del número de avisos). Lo que no cumple las reglas (nombre, idioma o
código inválidos) no aborta la migración: se descarta y se informa con `RAISE NOTICE`. Es repetible
y la columna de los canales no se toca. Qué borra el `Down`: `docs/runbooks/plantillas-whatsapp.md`.

El formulario de «Números de WhatsApp» todavía manda `plantillas`: lo que cambia ahí se refleja en la
cuenta (`origen = canal`), solo los códigos que cambiaron, y exige `plantillas_whatsapp.editar`. Una
plantilla que Iris no conocía entra en revisión (`PENDING`, nunca aprobada sin Meta) y se encola la
relectura de la cuenta (`origen = canal`).

## 8. Eventos

En `eventos.md` («Plantillas de WhatsApp»).
