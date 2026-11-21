# Servidor de IA («Iris Voz»): instalar, enlazar y operar

Un servidor aparte, con GPU, donde vive Ollama. Iris lo usa como proveedor del asistente de chat por
un canal autenticado; Ollama nunca queda expuesto a la red. Contrato: `.claude/integration/contratos/voz.md`
§4 (en especial §4.7 a §4.12). El paquete es independiente de Iris: tiene su propia versión
(etiquetas `voz-vX.Y.Z`) y **nunca abre conexiones hacia Iris**.

Esta primera entrega no trae audio (STT, TTS ni llamadas): solo el modelo de lenguaje.

## 1. Qué hace falta

- Un servidor Ubuntu, Debian, RHEL, Rocky Linux o AlmaLinux con **GPU NVIDIA** y su controlador
  (`nvidia-smi` funciona). El controlador lo instalas tú porque pide reiniciar; `setup-voz.sh` dice
  cómo si falta.
- Docker con el plugin compose 2.24 o más nuevo y `nvidia-container-toolkit`
  (`nvidia-ctk runtime configure --runtime=docker`). Si faltan, `setup-voz.sh` los instala de los
  repositorios oficiales (preguntando antes).
- **Reloj sincronizado por NTP** (las peticiones van firmadas con la hora; más de 30 s de desfase se
  rechaza con `reloj_desfasado`).
- **Sin intercambio a disco** (o cifrado): lo que el modelo tiene en memoria no debe llegar al disco.
- Que Iris alcance el puerto de control (8443/TCP). Es lo único que hay que abrir; no hace falta
  abrir nada de Ollama. Salida a internet solo para bajar imágenes y modelos.

Las cifras de hardware (VRAM, simultáneas) no están medidas todavía: ver §7.

## 2. Instalar

### Con tres líneas (lo recomendado)

En el servidor de IA, con un usuario que pueda usar `sudo`, copia y pega:

```sh
curl -fsSLO https://github.com/HCDestroyer/iris/releases/download/voz-v0.1.1/setup-voz.sh
echo "bde9c117f9f0b61c470d493f8fdfe39fb7100bb377a65e3bcb5932ecf65fdb00  setup-voz.sh" | sha256sum -c -
sudo bash setup-voz.sh
```

La primera línea baja el script; la segunda comprueba que es **exactamente** el que se revisó en
este repositorio (la suma está escrita aquí, no en la release; `infra/voz/verificar.sh` comprueba
que coincide con `setup-voz.sh`). Si dice algo distinto de `setup-voz.sh: OK`, **no sigas**. La
tercera lo ejecuta.

`setup-voz.sh` hace todo, con un mensaje en cada paso, y se detiene explicando qué hacer si algo
falta:

1. Comprueba que corre como administrador, la distribución (Ubuntu, Debian, RHEL, Rocky o Alma), la
   arquitectura (x86_64 o arm64), el reloj (si no está sincronizado ofrece activar NTP), el
   intercambio a disco (ofrece apagarlo) y que el puerto 8443 esté libre (o que ya sea de Iris Voz).
2. Comprueba la tarjeta: si `nvidia-smi` no funciona, se detiene y explica cómo instalar el
   controlador; después de reiniciar se vuelve a correr `sudo bash setup-voz.sh`.
3. Si faltan Docker (con compose 2.24 o más nuevo) o el NVIDIA Container Toolkit, pregunta
   `¿Instalar …? [s/N]` y los instala de sus repositorios oficiales (`download.docker.com` y
   `nvidia.github.io`), con la llave GPG de cada uno comprobada por su huella; configura el runtime de
   NVIDIA y reinicia Docker.
4. Usa el `cosign` del sistema si es 2.4 o más nuevo; si no, baja el oficial de sigstore con versión
   y sha256 fijos dentro del script (si la suma no coincide, se detiene).
5. Elige la última versión `voz-vX.Y.Z` publicada (ni borrador ni prerelease), o la de
   `--version X.Y.Z`. Nunca una anterior a la de la release que trae el script (`VOZ_MIN`).
6. Baja `install-voz.sh` y su firma a una carpeta temporal propia, comprueba que lo firmó el
   workflow `voz-release.yml` en la etiqueta `voz-vX.Y.Z` y **solo entonces** lo ejecuta (abajo, lo
   que hace el instalador). Al final recuerda los pasos del §3.

Correrlo otra vez no rompe nada: lo que ya está instalado se deja, y si hay una versión más nueva
la instala. Opciones: `sudo bash setup-voz.sh --version X.Y.Z --port N …`

- `--version X.Y.Z` (también `voz-vX.Y.Z`), `--port N`, `--listen IP`, `--dir RUTA`, `--no-models`,
  `--allow-swap` (solo con intercambio cifrado), `--allow-unsynced-clock`.
- `--yes`: no pregunta y **acepta todo lo que preguntaría**: agregar los repositorios e instalar
  Docker y el NVIDIA Container Toolkit, activar NTP, apagar el intercambio y comentarlo en
  `/etc/fstab` (con copia en `/etc/fstab.antes-de-iris-voz`), y que `nvidia-ctk` reescriba
  `/etc/docker/daemon.json` y reinicie Docker, **lo que detiene un momento los demás contenedores**
  del servidor. Úsalo solo en un servidor dedicado a Iris Voz.

La dirección es la de una release `voz-v*` concreta: `releases/latest/download/…` **no sirve**,
porque «latest» es siempre la release de Iris, no la de Iris Voz. El script instala siempre la
última publicada. Cuando cambia `setup-voz.sh`, se actualizan la versión de la URL y la suma de
arriba (lo exige `verificar.sh`).

### Atajo de una línea (con un riesgo)

```sh
curl -fsSL https://github.com/HCDestroyer/iris/releases/download/voz-v0.1.1/setup-voz.sh | sudo bash
```

Hace lo mismo, pero **ejecuta como administrador lo que llegue de esa dirección sin comprobarlo
antes**. Si alguien lograra cambiar el archivo publicado en la release (por ejemplo, con acceso a la
cuenta de GitHub del repositorio), esta línea correría su versión con todos los permisos del
servidor. Las tres líneas de arriba lo evitan: la suma no coincidiría y no se ejecutaría nada. Con la
tubería, las opciones van después de `bash -s --` (`… | sudo bash -s -- --version 0.1.1`).

### Verificar con la firma (opcional)

Para comprobar además quién lo publicó, con cosign 2.4 o más nuevo de una fuente tuya:

```sh
V=0.1.1
base=https://github.com/HCDestroyer/iris/releases/download/voz-v$V
curl -fsSLO $base/setup-voz.sh
curl -fsSLO $base/setup-voz.sh.sigstore.json
cosign verify-blob --new-bundle-format --bundle setup-voz.sh.sigstore.json \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity https://github.com/HCDestroyer/iris/.github/workflows/voz-release.yml@refs/tags/voz-v$V \
  setup-voz.sh
sudo bash setup-voz.sh
```

La firma prueba que lo publicó `voz-release.yml` en esa etiqueta, con la aprobación del entorno
`voz-release` (§8). La release también trae `setup-voz.sh.sha256`, pero esa suma viene del mismo
sitio que el script: solo detecta una descarga rota.

### A mano

Lo mismo sin `setup-voz.sh`, con Docker, `nvidia-container-toolkit` y cosign ya instalados:

```sh
V=0.1.1
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

El instalador (`install-voz.sh`, también cuando lo llama `setup-voz.sh`):

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

1. Sube `version` en `infra/voz/manifiesto.json` (y `irisMin`/`irisMax` si cambia la compatibilidad),
   `VOZ_MIN` en `infra/voz/setup-voz.sh` a esa misma versión y, en el §2, la versión de las URLs de
   `setup-voz.sh` y su sha256 (`sha256sum infra/voz/setup-voz.sh`); `verificar.sh` falla si algo no
   cuadra. Mergea a `main`. Hasta que el PM apruebe la release, la URL del §2 aún no existe.
2. **La etiqueta y la construcción salen solas.** El workflow `voz-etiqueta.yml` crea la etiqueta
   `voz-vX.Y.Z` sobre ese commit y eso dispara `voz-release.yml`, que publica la imagen y arma el
   paquete. Si la etiqueta ya existe con el mismo manifiesto no hace nada; si existe con otro
   manifiesto, o la versión no es mayor que la última, falla (hay que subir `version`).
3. **El PM aprueba firmar y publicar.** Los jobs `firmar` y `publicar` esperan una aprobación en el
   entorno `voz-release`: nada se firma (ni queda en el registro de transparencia) sin ella. Con la
   aprobación, firma con cosign sin clave, verifica sin red y crea la release. Si el entorno no
   existe o no tiene revisores obligatorios, o el commit de la etiqueta no está en `main`, el
   workflow falla antes de construir.

Si `voz-etiqueta.yml` falló (por ejemplo, antes de configurar la App), se relanza desde Actions →
«Etiqueta de Iris Voz» → *Run workflow* sobre `main`. La etiqueta también se puede crear a mano.

**Antes de la primera versión hay que preparar el repositorio** (lo hace el dueño, una sola vez; no
lo hace ningún agente):

1. **Una GitHub App que crea las etiquetas** (GitHub → tu foto → Settings → Developer settings →
   GitHub Apps → *New GitHub App*):
   - Nombre, por ejemplo «Iris Voz etiquetas»; *Homepage URL*: la del repositorio.
   - *Webhook*: desmarcar *Active*.
   - *Repository permissions* → *Contents*: **Read and write**. Nada más.
   - *Where can this GitHub App be installed?*: **Only on this account**. Crear.
   - Anotar el **App ID** (arriba en la página de la App) y, abajo, *Generate a private key*: baja
     un archivo `.pem`.
   - *Install App* (menú izquierdo) → instalar en tu cuenta → **Only select repositories** → `iris`.
2. **El entorno `voz-etiqueta`** (repositorio → Settings → Environments → *New environment*):
   - *Deployment branches and tags* → *Selected branches and tags* → agregar la regla de rama
     `main` (solo `main`). Sin revisores: corre solo.
   - *Environment secrets*: `VOZ_APP_ID` = el App ID, y `VOZ_APP_KEY` = el contenido completo del
     `.pem` (abrirlo con un editor de texto y pegar todo, con las líneas `BEGIN`/`END`). Después,
     borrar el `.pem` del equipo.
   - **Nunca** en Settings → Secrets and variables → Actions → *Repository secrets*: ahí los leería
     cualquier workflow de cualquier rama. `voz-etiqueta.yml` se niega a usar la llave si el entorno
     no está limitado exactamente a `main` (y avisa, porque `GITHUB_TOKEN` no puede listar los
     secretos del repositorio, de que hay que comprobar a mano que no estén ahí).
3. **La regla de etiquetas `voz-v*`** (Settings → Rules → Rulesets, la de «Iris Voz»): restringe
   crear, actualizar y borrar. En *Bypass list* → *Add bypass* → la App «Iris Voz etiquetas»
   (además de quien ya publica). **No** agregar la app «GitHub Actions»: eso dejaría etiquetar a
   cualquier workflow de cualquier rama.
4. **El entorno `voz-release`** con revisores obligatorios, con «impedir autoaprobación» activado,
   sin permitir que los administradores se salten la regla y limitado a las etiquetas `voz-v*`.
5. **`main` sigue protegida** contra push directo y contra force-push (solo entra por PR con CI en
   verde). Todo lo anterior descansa en eso: lo que llega a `main` puede etiquetarse solo.

Por qué así: la identidad que acepta el instalador es `voz-release.yml` en la etiqueta `voz-vX.Y.Z`,
y esa identidad no depende del contenido del workflow. Por eso quien pueda crear una etiqueta
`voz-v*` en un commit cualquiera podría firmar sin aprobación con un `voz-release.yml` modificado. La
llave de la App solo llega a un job del entorno `voz-etiqueta`, que solo corre en `main`: quien empuja
una rama no puede crear la etiqueta; solo lo que ya entró a `main` (con revisión y CI) llega a
etiquetarse, y firmarlo sigue necesitando la aprobación del PM.

La comprobación «el commit de la etiqueta está en `main`» de `voz-release.yml` evita errores honestos
(una etiqueta hecha a mano sobre otra rama), pero **no es una barrera de seguridad**: vive en el mismo
archivo que corre en la etiqueta y quien controle ese commit la puede quitar. Las barreras son la
regla de etiquetas `voz-v*`, que la llave de la App solo esté en el entorno `voz-etiqueta` (limitado a
`main`) y que `main` esté protegida.

El workflow comprueba primero el commit y el entorno y después corre tres jobs con permisos mínimos:
`construir` (publica la imagen y exporta las sumas), `firmar` (solo `id-token: write`; no hace
checkout ni ejecuta nada del repositorio, y comprueba las sumas antes de firmar) y `publicar`
(verifica sin red y crea la release). Todas las acciones van fijadas por SHA.

Cada release `voz-vX.Y.Z` publica, firmados igual (sha256 y bundle de Sigstore): el paquete
`iris-voz-X.Y.Z.tar.gz`, `install-voz.sh`, `setup-voz.sh` (la instalación de un paso del §2) y
`manifiesto.json`. `infra/voz/verificar.sh` comprueba que `setup-voz.sh` fija el mismo cosign y la
misma identidad que `install-voz.sh`, y `infra/voz/prueba-setup.sh` lo prueba con comandos falsos
(sin GPU, sin Docker, cosign alterado, firma inválida, versión inválida…).
