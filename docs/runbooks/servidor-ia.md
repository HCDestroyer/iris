# Servidor de IA («Iris Voz»): instalar, enlazar y operar

Un servidor aparte, con GPU, donde vive Ollama. Iris lo usa como proveedor del asistente de chat por
un canal autenticado; Ollama nunca queda expuesto a la red. Contrato: `.claude/integration/contratos/voz.md`
§4 (en especial §4.7 a §4.12). El paquete es independiente de Iris: tiene su propia versión
(etiquetas `voz-vX.Y.Z`) y **nunca abre conexiones hacia Iris**.

Esta primera entrega no trae audio (STT, TTS ni llamadas): solo el modelo de lenguaje.

## 1. Qué hace falta

- Un servidor Ubuntu o Debian con **GPU NVIDIA**, el controlador y `nvidia-container-toolkit`
  (`nvidia-ctk runtime configure --runtime=docker`).
- Docker con el plugin compose 2.24 o más nuevo.
- **Reloj sincronizado por NTP** (las peticiones van firmadas con la hora; más de 30 s de desfase se
  rechaza con `reloj_desfasado`).
- **Sin intercambio a disco** (o cifrado): lo que el modelo tiene en memoria no debe llegar al disco.
- Que Iris alcance el puerto de control (8443/TCP). Es lo único que hay que abrir; no hace falta
  abrir nada de Ollama. Salida a internet solo para bajar imágenes y modelos.

Las cifras de hardware (VRAM, simultáneas) no están medidas todavía: ver §7.

## 2. Instalar

En el servidor de IA:

```sh
V=0.1.0
base=https://github.com/HCDestroyer/iris/releases/download/voz-v$V
curl -fsSLO $base/install-voz.sh
curl -fsSLO $base/install-voz.sh.sigstore.json
# Verifica el instalador ANTES de ejecutarlo (cosign 2.4 o más nuevo, de una fuente tuya):
cosign verify-blob --new-bundle-format --bundle install-voz.sh.sigstore.json \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity https://github.com/HCDestroyer/iris/.github/workflows/voz-release.yml@refs/tags/voz-v$V \
  install-voz.sh
sudo bash install-voz.sh --version $V
```

El instalador:

1. Comprueba Docker, la GPU (`nvidia-smi` y el runtime de NVIDIA), el reloj y el intercambio. Sin
   GPU se detiene (`--no-gpu` existe solo para pruebas).
2. Baja el paquete y **verifica su firma sin red**: bundle de Sigstore, raíz de confianza embebida
   en el propio instalador y prueba de inclusión en el registro de transparencia, que es
   obligatoria. Comprueba que lo firmó el workflow `voz-release.yml` de este repositorio en la
   etiqueta `voz-v<versión>` y que el manifiesto dice esa misma versión. Si algo no cuadra **no
   instala nada**. El binario de cosign se baja fijado por sha256 (o se le da con
   `IRIS_VOZ_COSIGN=/ruta/cosign` si el servidor no tiene salida a GitHub; la suma se comprueba igual).
3. Arranca los contenedores (imágenes fijadas por digest), baja los modelos por defecto del
   manifiesto (comprobados por digest) y los permite.
4. Muestra la **huella del certificado** del servidor. Anótala: se compara en el panel.

Servidor sin internet: copia `iris-voz-<v>.tar.gz`, su `.sigstore.json` y un archivo `latest` con la
versión a una carpeta y usa `IRIS_VOZ_SOURCE=/esa/carpeta`. La firma se verifica igual.

## 3. Enlazar con Iris

1. Panel de Iris → Administración → Integraciones → **Servidor de IA** → «Enlazar servidor de IA».
   Escribe la dirección (`https://<servidor>:8443`). El panel muestra **una vez** un código de un
   solo uso (`voz-…`, 160 bits) que vale 15 minutos.
2. En el servidor: `sudo iris-voz pair` y escribe el código cuando lo pide (no se ve al teclear; no
   se acepta como argumento). Eso abre la ventana de enlace durante 15 minutos.
3. En el panel: «Conectar». Iris y el servidor se demuestran mutuamente que conocen el código, cada
   uno atado al certificado del otro; el código no viaja.
4. El panel muestra la huella del certificado que vio Iris. **Compárala con la que imprimió el
   servidor** (`sudo iris-voz status` la repite). Si coincide, «Activar». Si no coincide, no actives:
   hay alguien en medio.

Al activar, Iris cambia de inmediato el secreto del canal por uno aleatorio. Hasta activar, Iris no
manda nada más al servidor. Un intento con un código equivocado no cierra la ventana (hay límite de
frecuencia); la ventana se cierra con el primer enlace correcto o a los 15 minutos.

## 4. Modelos

```sh
sudo iris-voz model pull qwen2.5:7b    # descarga; ese contenedor solo llega al registro de modelos
sudo iris-voz model allow qwen2.5:7b   # deja que Iris lo use
sudo iris-voz model list
sudo iris-voz model revoke NOMBRE    # Iris deja de poder usarlo
sudo iris-voz model remove NOMBRE
```

Desde Iris solo se **listan** los modelos permitidos: Iris no puede bajar, borrar ni crear modelos.
Cada empresa elige en los ajustes de su asistente el proveedor «Servidor de IA» y uno de esos
modelos. Cuántas peticiones a la vez admite el servidor se calcula con la VRAM y el mayor de los
modelos permitidos (`IRIS_VOZ_LLM_SIMULTANEAS` en `/opt/iris-voz/.env` lo fija a mano).

## 5. Operación

| Qué | Cómo |
| --- | --- |
| Estado | `sudo iris-voz status`, y el tablero del panel |
| Logs | `sudo iris-voz logs puerta` (ruta, tamaño, duración y código; **nunca** prompts ni respuestas) |
| Actualizar | `sudo iris-voz upgrade` (misma verificación de firma; nunca baja de versión) |
| Rotar credenciales | Desde el panel («Rotar credenciales»; «Revocar ahora» sin convivencia) |
| Renovar el certificado | `sudo iris-voz renew-cert`: la huella nueva se anuncia a Iris por el canal vigente y Iris la fija sola. Nunca se acepta una huella nueva con un clic |
| Se perdió el certificado (reinstalación) | `sudo iris-voz renew-cert --no-channel` y enlazar de nuevo con código |
| Iris comprometido | `sudo iris-voz unpair` en el servidor y enlazar de nuevo: el token y la clave del certificado de cliente viven los dos en Iris y caen juntos |
| El registro cambió de almacén y `model pull` falla | El proxy de descarga solo admite hosts exactos y registra el rechazado (`sudo iris-voz logs proxy-descarga`). Revisa que sea del registro de modelos y añádelo a `IRIS_VOZ_DESCARGA_HOSTS` en `/opt/iris-voz/.env` |
| Limitar por IP | `sudo iris-voz ip restrict` (solo la IP de Iris vista al enlazar; apagado de fábrica) y `ip release` |

Si el servidor se cae, el asistente de las empresas que lo usan no queda colgado: las sugerencias
fallan al instante con «Reintentar» y el modo autónomo pasa la conversación a una persona. Cuando
vuelve, se usa de nuevo sin tocar nada.

## 6. Qué ve quien comprometa este servidor

Lee los prompts del chat de las empresas que usan «Servidor de IA» mientras pasan (mensajes, nombre
de pila, plan, estado del servicio, fallas, fragmentos de manuales y resultados de herramientas) y
puede falsificar las respuestas del modelo. **El documento, el NIT y los montos nunca viajan.** No
puede leer la base de Iris ni llamar a Iris, Meta o Argos, ni ver nada de las empresas que usan otro
proveedor. Detalle: `voz.md` §4.11.

## 7. Qué está probado y qué no

Probado en CI, sin GPU: el protocolo completo con la puerta real y un Ollama falso (emparejamiento,
mTLS, firma, repetición, rotación, renovación de certificado, reserialización, topes, caída); el
paquete levantado con Docker Compose con un Ollama falso y con un **Ollama real en CPU** y un modelo
diminuto (descarga por el proxy, digest, chat); y la firma sin clave con verificación sin red.

**No probado, porque necesita una GPU real** (queda para la primera instalación):

- El runtime de NVIDIA en el compose y la lectura de `nvidia-smi` dentro del contenedor `medidor`.
- VRAM por modelo y por petición simultánea, y por tanto el cálculo de `maxSimultaneas` (hoy es una
  estimación: el mayor modelo permitido con un 20 % de margen y 2 GB por plaza adicional).
- Latencia real y arranque en frío.

Anota los resultados de la primera instalación en `voz.md` §4.9.

## 8. Publicar una versión (mantenedores)

1. Sube `version` en `infra/voz/manifiesto.json` (y `irisMin`/`irisMax` si cambia la compatibilidad)
   y mergea a `main`.
2. Crea la etiqueta `voz-vX.Y.Z` sobre ese commit. El workflow `voz-release.yml` publica la imagen,
   arma el paquete, lo firma con cosign sin clave y lo verifica sin red antes de crear la release.
3. Los jobs `firmar` y `publicar` esperan una aprobación en el entorno `voz-release`: nada se firma
   (ni queda en el registro de transparencia) sin ella. Si el entorno no existe o no tiene revisores
   obligatorios, el workflow falla antes de construir.

**Antes de la primera versión hay que preparar el repositorio** (lo gestiona el líder con el dueño;
no lo hace ningún agente):

- Una **regla de etiquetas** que proteja `voz-v*`: solo quien publica puede crearlas, sin borrado ni
  reescritura. La identidad que acepta el instalador es ese workflow en esa etiqueta.
- El **entorno `voz-release`** (Settings → Environments) con revisores obligatorios, con «impedir
  autoaprobación» activado, sin permitir que los administradores se salten la regla y limitado a
  las etiquetas `voz-v*`.

El workflow comprueba primero el entorno y después corre tres jobs con permisos mínimos:
`construir` (publica la imagen y exporta las sumas), `firmar` (solo `id-token: write`; no hace
checkout ni ejecuta nada del repositorio, y comprueba las sumas antes de firmar) y `publicar`
(verifica sin red y crea la release). Todas las acciones van fijadas por SHA.
