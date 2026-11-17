Versión: 1.0.0 (17 nov 2026) · Correo del sistema configurable desde el panel

# Correo del sistema

El correo del sistema es el SMTP con el que Iris manda avisos a **sus usuarios** (hoy: el aviso del
tope del asistente de IA; `notificaciones.correo_enviado` / `notificaciones.correo_fallido`). No es el
correo de los buzones de soporte, que tiene su propio SMTP por buzón.

Hasta ahora solo se configuraba con `IRIS_SMTP_*` en el `.env`. Desde esta versión se configura en el
panel (Configuración › Correo del sistema), con «probar antes de guardar». El `.env` queda como
respaldo para las instalaciones que ya lo usan.

Lo implementa I1 en `internal/notificaciones`. Estado: **implementado** (API y worker); falta la pantalla del panel (§6).

## 1. Modelo

Tabla `notificaciones.correo_ajustes`, una fila por ámbito:

- **Global** (`empresa_id` null): el de la instalación.
- **Por empresa** (opcional): reemplaza al global para los correos de esa empresa.

| Campo | Tipo | Notas |
| --- | --- | --- |
| `host` | texto, 1–253 | Nombre DNS o IP. Sin esquema, ruta ni puerto. |
| `puerto` | entero | Solo `25`, `465`, `587` o `2525`. |
| `seguridad` | `starttls` \| `tls` \| `ninguna` | `ninguna` (sin cifrado) solo si el host resuelve dentro de `IRIS_CORREO_REDES_PERMITIDAS` (un relay interno) **y sin usuario ni contraseña**: las credenciales nunca viajan en claro. Si no, 422 (`sin_cifrado_solo_red_interna`, `sin_cifrado_sin_credenciales`). |
| `usuario` | texto, 0–254 | Vacío = sin autenticación (y la contraseña guardada se borra). Con usuario hace falta contraseña. |
| `contrasena` | secreto | Cifrada en reposo con el cifrador de la instalación (`IRIS_CLAVE_CIFRADO`, el mismo mecanismo que la clave del asistente, con datos asociados a la fila). **Nunca se devuelve.** |
| `remitenteNombre` | texto, 0–80 | Sin CR, LF ni caracteres de control. |
| `remitenteCorreo` | correo | Una sola dirección válida, sin CR ni LF. |
| `activo` | boolean | Apagado, el ámbito se salta (ver §2). |
| `version` | entero | Para el 409. |

## 2. Precedencia

Para cada correo, Iris usa el primero que exista y esté **activo**:

1. el ajuste de la **empresa** del correo;
2. el ajuste **global**;
3. `IRIS_SMTP_*` del **entorno** (respaldo; no se edita desde el panel);
4. **ninguno**: no hay correo, el correo queda `omitido` (`sin_correo_sistema`) y el aviso sigue
   saliendo por la campana.

Un ajuste apagado no «bloquea» el siguiente: apagar el de la empresa hace que se use el global. El
worker lee el ajuste vigente en cada envío (sin reiniciar): un cambio vale para el correo siguiente.

## 3. Endpoints (`/api/v1`)

El ámbito va en la ruta: `global` o el id de la empresa. `{ambito}` = `global` | `<empresaId>`.

| Método y ruta | Permiso | Qué hace |
| --- | --- | --- |
| `GET /correo-sistema/{ambito}` | `correo_sistema.ver` | El ajuste guardado de ese ámbito (o los valores vacíos si no hay) y de dónde sale el correo hoy. |
| `POST /correo-sistema/{ambito}/guardar` | `correo_sistema.configurar` | Crea o reemplaza el ajuste. `version` (0 al crear) e `Idempotency-Key`. |
| `POST /correo-sistema/{ambito}/probar` | `correo_sistema.configurar` | Manda un correo de prueba con los valores del formulario, **sin guardar**. |
| `POST /correo-sistema/{ambito}/activar` | `correo_sistema.configurar` | `{ version }`. |
| `POST /correo-sistema/{ambito}/desactivar` | `correo_sistema.configurar` | `{ version }`. |

El ámbito de una empresa exige operar en esa empresa; `global` exige el permiso sin límite de empresa.

### 3.1 `GET`

```json
{
  "ambito": "global",
  "empresaId": null,
  "configurado": true,
  "host": "smtp.proveedor.example",
  "puerto": 587,
  "seguridad": "starttls",
  "usuario": "avisos@isp.example",
  "tieneContrasena": true,
  "remitenteNombre": "Iris",
  "remitenteCorreo": "no-responder@isp.example",
  "activo": true,
  "version": 3,
  "actualizadoEn": "2026-11-16T18:00:00Z",
  "origen": "global",
  "origenDetalle": "Los correos salen por el ajuste global de la instalación.",
  "enUso": true,
  "entorno": { "configurado": true, "host": "localhost", "remitenteCorreo": "no-responder@iris.example" },
  "ultimaPrueba": { "estado": "ok", "en": "2026-11-16T17:59:00Z", "causa": null }
}
```

- `origen`: `empresa` | `global` | `entorno` | `ninguno`: por dónde saldría **hoy** un correo de ese
  ámbito (§2). `origenDetalle` es la misma idea en una frase, lista para mostrar.
- `enUso`: el ajuste guardado de **este** ámbito es el que se usa de verdad hoy. En el ámbito de una
  empresa con su ajuste apagado (o sin ajuste), `enUso` es `false` y `origen` dice cuál se usa
  (`global`, `entorno` o `ninguno`).
- Sin ajuste guardado: `configurado: false`, `version: 0` y los valores por defecto del formulario
  (`puerto` 587, `seguridad` `starttls`, `activo` true).
- `ultimaPrueba`: la última prueba de ese ámbito; **ausente** si nunca se probó.
- `entorno`: si hay `IRIS_SMTP_*`, para que el panel pueda decir «hoy se usa el del servidor»; nunca
  el usuario ni la contraseña.
- `tieneContrasena`: lo único que se sabe de la contraseña.

### 3.2 `guardar`

```json
{
  "version": 3,
  "host": "smtp.proveedor.example",
  "puerto": 587,
  "seguridad": "starttls",
  "usuario": "avisos@isp.example",
  "contrasena": "…",
  "remitenteNombre": "Iris",
  "remitenteCorreo": "no-responder@isp.example",
  "activo": true
}
```

`contrasena`: ausente = se conserva la guardada; `""` = se borra; un valor = la reemplaza. Responde la
forma del `GET`. Guardar **no** prueba la conexión (para eso está `probar`), pero sí valida el host
contra la guarda de red (§4).

La contraseña guardada solo vale para **el mismo servidor, puerto y usuario**: si cambia alguno y no
viene `contrasena`, 422 `contrasena_requerida` (con `errors[].field = "contrasena"`): hay que volver
a escribirla. Así la guardada nunca viaja a otro servidor.

Errores: 409 `conflicto_version`; 422 `validacion` (campo en `errors[].field`), `contrasena_requerida`,
`destino_no_permitido`, `sin_cifrado_solo_red_interna`, `sin_cifrado_sin_credenciales`,
`cifrado_no_configurado` (falta `IRIS_CLAVE_CIFRADO`).

`activar` y `desactivar`: `{ "version": n }`; responden la forma del `GET`. Sin ajuste guardado, 404.

### 3.3 `probar`

Mismos campos que `guardar` (sin `version` ni `activo`) más `para` (una sola dirección). Si no viene
`contrasena`, usa la guardada de ese ámbito, **solo si el servidor, el puerto y el usuario son los
guardados** (si no, 422 `contrasena_requerida`: la prueba no sirve para mandar la contraseña guardada
a otro servidor). Manda un correo de **asunto y cuerpo fijos** («Correo de prueba de Iris») y, si
llegó a intentar el envío, responde siempre 200 con el resultado:

```json
{ "estado": "ok", "causa": null, "detalle": "El correo de prueba salió. Revisa la bandeja de ana@isp.example." }
```

`estado`: `ok` | `error`. `causa` (lista cerrada) y `detalle` (texto llano, en español, sin la
respuesta cruda del servidor):

| `causa` | Qué se le dice a la persona |
| --- | --- |
| `no_conecta` | No se pudo conectar: revisa el servidor y el puerto. |
| `destino_no_permitido` | Ese servidor no se puede usar desde Iris (red interna o no permitida). |
| `certificado` | El certificado del servidor no es válido para ese nombre. |
| `seguridad` | El servidor no acepta la seguridad elegida (prueba STARTTLS o TLS). |
| `credenciales` | El usuario o la contraseña no son correctos. |
| `remitente_rechazado` | El servidor no acepta ese remitente. |
| `destinatario_rechazado` | El servidor no acepta esa dirección de destino. |
| `tiempo_agotado` | El servidor no respondió a tiempo. |
| `otro` | El servidor rechazó el envío. |

Los 422 de validación son los de `guardar`. Tope: 429 `demasiadas_pruebas` con `Retry-After` (§4).
Las pruebas fallidas también cuentan. No lleva `Idempotency-Key`: cada llamada es una prueba.

## 4. Seguridad

- **Permisos** `correo_sistema.ver` y `correo_sistema.configurar`; de fábrica solo los tiene `admin`.
  `configurar` es sensible (MFA y re-autenticación en una sesión abierta por el autologin); `ver` no,
  como todos los permisos de ver, y nunca muestra la contraseña.
- **Alcance:** `global` exige operar sin límite de empresa (`empresas.todas`, o una instalación sin
  empresas): si no, 403. El ámbito de una empresa exige estar operando en ella (`X-Iris-Empresa`): si
  no, 404. Solo personas (un token de integración: 404).
- **Guarda de red** (la de los buzones, `soporte/correo`): el host se resuelve y se conecta a esa
  misma IP. Nunca loopback, link-local ni metadata; las redes privadas, solo las de
  `IRIS_CORREO_REDES_PERMITIDAS`. Se valida al guardar, al probar y en cada envío.
- **Puertos:** solo 25, 465, 587 y 2525 (422 con cualquier otro).
- **TLS verificado** contra las raíces del sistema más `IRIS_CORREO_CA` (como hoy). `ninguna` solo
  dentro de las redes permitidas y sin credenciales: lo exigen la validación, el cliente SMTP (mira
  la IP a la que de verdad se conectó) y un CHECK de la tabla.
- **La prueba no es un escáner ni un relay:** un solo destinatario, cuerpo y asunto fijos, la misma
  guarda de red y los mismos puertos, 5 pruebas por usuario y hora (429 con `Retry-After`) y 20 por
  instalación y hora. Las causas son de lista cerrada: no distinguen «puerto cerrado» de «filtrado».
- **Contraseña:** `***` en `auditoria.acciones` y `auditoria.cambios`; nunca en eventos, logs ni
  respuestas. Entra en la rotación de `IRIS_CLAVE_CIFRADO` (`iris clave rotar`).
- **Inyección de cabeceras:** `remitenteNombre`, `remitenteCorreo`, `usuario`, `host` y `para` se
  rechazan con CR, LF u otros caracteres de control; los correos se validan como una sola dirección.

## 5. Eventos (outbox)

| Evento | Payload | Cuándo |
| --- | --- | --- |
| `correo_sistema.ajustes_cambiados` | `empresa_id` (o null = global), `campos[]`, `activo` | Guardar, activar o desactivar. `campos[]` puede traer `contrasena` (que cambió), nunca su valor. |
| `correo_sistema.probado` | `empresa_id` (o null), `estado` (`ok` \| `error`), `causa` (o null), `usuario_id` | Cada prueba. Nunca el host, el destinatario ni credenciales. |

## 6. Para el panel

- Configuración › **Correo del sistema**: formulario en pantalla propia (más de 3 campos), con «Probar»
  (pide la dirección de destino; por defecto el correo de quien prueba) antes de «Guardar».
- Arriba, una línea con `origen`: «Hoy los correos salen por: el ajuste de esta empresa / el ajuste
  global / la configuración del servidor (.env) / no hay correo configurado».
- La contraseña nunca viene: con `tieneContrasena` el campo se muestra como «guardada» y solo se
  manda si la persona escribe una nueva.
- Los 422 traen `errors[].field` con el nombre del campo del formulario.

## 7. Decisiones (cerradas con el líder, 16 nov 2026)

1. Ámbito en la ruta (`global` o el id de la empresa).
2. Un ajuste apagado no bloquea al siguiente, y el `GET` dice cuál se usa de verdad (`origen`, `enUso`).
3. `guardar` no prueba la conexión; `probar` no guarda.
4. Puertos 25, 465, 587 y 2525.
5. `seguridad: ninguna` solo con el host dentro de `IRIS_CORREO_REDES_PERMITIDAS` y nunca con usuario
   ni contraseña.
6. Tope de pruebas: 5 por usuario y 20 por instalación cada hora.
7. Causas de lista cerrada, en lenguaje llano.
8. Dos permisos (`ver`, `configurar`), de fábrica solo `admin`.
9. Del `.env` el `GET` solo muestra el host y el remitente.
