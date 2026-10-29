# Despliegue en laboratorio con Docker Compose

El laboratorio usa el mismo `infra/docker-compose.yml` que desarrollo, con el perfil `app` y un `.env`
propio fuera de git. No es producción: Postgres, MinIO y Mailpit corren en la misma máquina y los
puertos se publican solo en `127.0.0.1` (pon un proxy inverso con HTTPS delante del panel).

## 1. Máquina

- Linux con Docker Engine y Compose v2, 4 GB de RAM (8 GB si preparas OSRM ahí), 20 GB de disco.
- `git`, `make`, `openssl`, `curl`; para respaldos, `age` y el cliente de PostgreSQL 16 (o
  `respaldar.sh --compose`, que usa el `pg_dump` del contenedor).
- Un proxy inverso (Caddy, nginx) con certificado para el nombre del lab, hacia `127.0.0.1:3000`.

## 2. Código y variables

```sh
git clone https://github.com/HCDestroyer/iris.git && cd iris
git checkout v0.1.0            # o la rama/etiqueta que se ensaya
cp .env.example .env
chmod 600 .env
```

Cambia en `.env` como mínimo:

| Variable | Valor en lab |
| --- | --- |
| `IRIS_BASE_URL` | `https://iris-lab.<dominio>` (el origen del proxy inverso; arma webhooks, enlaces de conexión y del portal) |
| `IRIS_CLAVE_CIFRADO` | `openssl rand -base64 32`, guardada también en el gestor de secretos: perderla deja ilegibles MFA, conectores y buzones ([rotación](rotar-clave-cifrado.md)) |
| `IRIS_PG_CONTRASENA`, `IRIS_S3_SECRET_KEY` | contraseñas aleatorias (MinIO: ≥ 8 caracteres) |
| `IRIS_COOKIE_SEGURA` | `true` (detrás de HTTPS) |
| `IRIS_PROXIES_CONFIABLES` | **vacía**: el compose confía solo en la IP fija del panel (`IRIS_WEB_IP`, `172.28.10.10/32`). Si la pones, solo IPs `/32` de proxies reales: nunca una red entera ni el gateway de la red de Docker (ver «IP del cliente») |
| `IRIS_WEB_IP` | `172.28.10.10` salvo que cambies `IRIS_RED_COMPOSE` (debe estar dentro de esa red y no ser el `.1`) |
| `IRIS_ARGOS_ORIGENES`, `IRIS_HORUS_ORIGENES` | el origen de la API de Argos y de Horus de lab (`https://argos-lab.<dominio>`). **Obligatorias:** al conectar con un enlace, Iris entrega un token suyo al origen del enlace y solo lo hace a estos orígenes (o al de un conector que ya existe); vacías, el canje responde 422 |
| `IRIS_ARGOS_REDES_PERMITIDAS`, `IRIS_HORUS_REDES_PERMITIDAS` | la red interna donde viven Argos y Horus de lab (CIDR); vacía si están en IP pública |
| `IRIS_CONECTORES_PERMITIR_HTTP` | `false` salvo que Argos/Horus de lab no tengan HTTPS |
| `IRIS_CORREO_REDES_PERMITIDAS` | vacía para usar solo Mailpit (red del compose); la red de tu servidor de correo de lab si es interna |
| `IRIS_RESPALDO_DESTINATARIOS` | archivo con la clave pública age ([respaldos](respaldos.md)) |

### IP del cliente

La API guarda la IP del cliente en sesiones, portal, encuesta y logs. Solo cree `X-Forwarded-For`
si la conexión viene de `IRIS_PROXIES_CONFIABLES`, y en el compose eso es solo el panel (`web`,
IP fija `IRIS_WEB_IP`). **Nunca pongas la red del compose ni su gateway (`172.28.10.1`):** con un
puerto publicado, Docker hace NAT y todo lo que llega de fuera (también tu proxy inverso del host)
entra con la IP del gateway, que podría mandar el `X-Forwarded-For` que quisiera. En producción va
solo la IP `/32` del proxy real.

Los puertos de `api` (8080) y `web` (3000) se publican solo en `127.0.0.1`: desde fuera solo se
llega por el proxy inverso del host. El de la API sirve para `curl` de comprobación; el panel le
habla por la red interna (`http://api:8080`).

Con el proxy inverso en el host, el panel recibe la conexión desde el gateway: hasta que el proxy
de Next (I3) decida si cree el `X-Forwarded-For` del proxy inverso, la IP que se guarda es la del
gateway, no la del cliente. Para la IP real, corre el proxy inverso como servicio del compose con IP
fija y agrega su `/32` a `IRIS_PROXIES_CONFIABLES` junto con la de `web`.

`IRIS_DB_URL`, `IRIS_S3_ENDPOINT` y demás direcciones internas las pone el compose (nombres de
servicio); no hace falta tocarlas. La lista completa de lo que lee la API está en
`apps/api/.env.example`.

### Conectar con Argos o Horus

Al conectar con el enlace que genera Argos (`https://<argos>/conectar#<código>`), Iris le entrega un
`iris_pat_` del usuario de integración. Por eso:

- Solo se acepta un enlace cuyo origen esté en `IRIS_ARGOS_ORIGENES` / `IRIS_HORUS_ORIGENES` (o el
  de un conector que ya existe). `POST /api/v1/integraciones/argosisp/conexion/previsualizar
  { enlace }` muestra el origen sin llamar afuera: confírmalo antes de conectar.
- Conecta un administrador con `empresas.todas` sin límite de alcance (el `admin`).
- El token nace con **15 min** de vida: activa el conector en ese plazo. Si vence, activar responde
  422 `token_entregado_vencido` y hay que generar otro enlace en Argos.
- Desactivar el conector, o conectar otro del mismo sistema, revoca el token anterior. Para volver
  a activar uno desactivado, conecta de nuevo.

## 3. Levantar

```sh
make dev-app                   # certificados de Mailpit, build de api/worker/web, migrar, levantar
make logs                      # en otra terminal
docker compose -f infra/docker-compose.yml --env-file .env --profile app run --rm api \
    admin crear --correo admin@<dominio> --nombre "Admin lab"   # imprime la contraseña una vez
```

Orden de arranque: postgres, redis, minio (+ `minio-init`: bucket `iris-adjuntos`) y mailpit sanos
→ `migrar` (`iris migrar`: goose, River y permisos) termina bien → `api` y `worker` → `web`. Si
`migrar` falla, nada de Iris arranca y el error queda en `make logs`.

OSRM (Fase 3, rutas): `infra/osrm/preparar.sh` una vez (descarga Guatemala de Geofabrik, perfil
car, MLD) y `docker compose … up -d osrm`. Sin datos, `osrm` termina con «OSRM sin datos»; el resto
de Iris no lo necesita todavía.

## 4. Comprobar

```sh
curl -fsS http://127.0.0.1:8080/api/v1/salud          # {"estado":"ok","version":"…"}
curl -fsS http://127.0.0.1:3000/api/v1/salud          # lo mismo, a través del proxy del panel
curl -fsSI https://iris-lab.<dominio>/entrar          # 200 por el proxy inverso
docker compose -f infra/docker-compose.yml --env-file .env --profile app ps   # migrar: Exited (0)
```

Luego, en el panel: entrar con el administrador, activar MFA, crear un buzón de prueba contra
Mailpit ([buzones](buzones.md)) y subir una foto a una orden (queda en MinIO, bucket
`iris-adjuntos`).

## 5. Actualizar

```sh
infra/respaldo/respaldar.sh --compose     # siempre antes de migrar
git fetch && git checkout <nueva etiqueta>
make dev-app                              # reconstruye, migra y reinicia api/worker/web
```

Las migraciones solo avanzan: para volver atrás se restaura el respaldo previo
([respaldos](respaldos.md)) con las imágenes de la versión anterior.

## 6. Imágenes publicadas

Al crear una versión, `release.yml` publica `ghcr.io/hcdestroyer/iris-api|iris-worker|iris-web`
con la etiqueta `X.Y.Z`. Para usarlas en lugar de construir: `IRIS_IMAGEN_TAG=X.Y.Z` y cambia
`image:` por la de GHCR en un `docker-compose.override.yml` local; `docker compose pull` y
`up -d --no-build`.
