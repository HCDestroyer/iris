# Desarrollo local

Cómo levantar Iris en tu máquina. Todo lo de infraestructura corre en Docker Compose
(`infra/docker-compose.yml`); la API (Go) y el panel (Next.js) pueden correr en tu máquina o en el
perfil `app` del compose.

## Requisitos

- Docker con Compose v2 (daemon corriendo).
- Go 1.24 (`apps/api`), Node 22 (`.nvmrc`) y pnpm 10 (`corepack enable` toma la versión de
  `packageManager`).
- `make`, `curl`, `openssl` (certificados de Mailpit) y, para `scripts/integrar.sh`,
  `psql`/`createdb`/`dropdb`.
- Opcional: `golangci-lint` v2 y `shellcheck` (los usa `make lint`; sin `shellcheck`, `make lint`
  avisa y sigue: CI sí lo corre). `sqlc` y `goose` no hacen falta: el `Makefile` los corre con
  `go run` en versiones fijadas. Para respaldos, `age` y el cliente de PostgreSQL 16.

## 1. Variables

```sh
cp .env.example .env
```

Los valores por defecto sirven para el compose local. Si ya tienes Postgres o Redis en 5432/6379,
cambia `IRIS_PG_PUERTO` / `IRIS_REDIS_PUERTO` y ajusta `IRIS_DB_URL` / `IRIS_REDIS_URL`. Nunca
pongas secretos reales en `.env.example`.

## 2. Servicios de infraestructura

```sh
make dev          # corre antes infra/mailpit/certificados.sh (idempotente)
```

| Servicio | Para qué | En tu máquina |
| --- | --- | --- |
| postgres 16 | base de datos (`iris`) | `localhost:5432` |
| redis 7 | caché (sin persistencia) | `localhost:6379` |
| minio | adjuntos S3; `minio-init` crea el bucket `iris-adjuntos` | API `:9000`, consola `http://localhost:9001` |
| mailpit | SMTP de prueba **con STARTTLS obligatorio** | SMTP `:1025`, UI `http://localhost:8025` |
| osrm | rutas por calle (Guatemala, perfil car, MLD) | `http://localhost:5000` |

### Mailpit con TLS

Iris nunca habla con un servidor de correo en claro ni sin verificar su certificado, así que
Mailpit exige STARTTLS en `:1025` con un certificado de una CA local de desarrollo:

- `infra/mailpit/certificados.sh` (lo corre `make dev`) deja en `infra/mailpit/certs/` (ignorado por
  git) `ca.pem`, `mailpit.pem` y `mailpit.key` (SAN `mailpit`, `localhost`, `127.0.0.1`). Con
  `--forzar` los vuelve a crear.
- En el perfil `app`, el compose monta `ca.pem` en api y worker como `IRIS_CORREO_CA` y permite su
  propia red (`IRIS_RED_COMPOSE`) en `IRIS_CORREO_REDES_PERMITIDAS`. Un buzón de prueba usa
  `smtp: { host: "mailpit", puerto: 1025, tls: "starttls", usuario: "x", contrasena: "x" }`
  (Mailpit acepta cualquier usuario).
- Con la API en tu máquina: `IRIS_CORREO_CA=$PWD/infra/mailpit/certs/ca.pem`,
  `IRIS_CORREO_REDES_PERMITIDAS=127.0.0.1/32` y el buzón con `host: "localhost"`.
- Mailpit no tiene IMAP: «Probar» un buzón contra él da SMTP correcto e IMAP «conexion». Para
  probar la entrada hace falta un IMAP con TLS (Dovecot o GreenMail) que no está en el compose.
- Comprobar a mano: `openssl s_client -starttls smtp -connect localhost:1025 -CAfile
  infra/mailpit/certs/ca.pem -verify_return_error`.

MinIO usa por defecto el fork comunitario `pgsty/minio` (la imagen oficial `minio/minio` ya no se
publica en Docker Hub); `IRIS_MINIO_IMAGEN` e `IRIS_MC_IMAGEN` permiten cambiarla.

### OSRM (una vez)

`osrm` necesita el extracto preprocesado en su volumen; sin él termina con el aviso
«OSRM sin datos». Prepáralo una vez (unos minutos y ~2 GB de RAM):

```sh
infra/osrm/preparar.sh            # descarga guatemala-latest.osm.pbf de Geofabrik y lo preprocesa
docker compose -f infra/docker-compose.yml --env-file .env up -d osrm
curl 'http://localhost:5000/route/v1/driving/-90.5133,14.6349;-90.7295,14.5586?overview=false'
```

Para actualizar el mapa: `infra/osrm/preparar.sh --forzar`. Otro extracto: `IRIS_OSRM_PBF_URL`.

## 3. Migraciones y API

```sh
export IRIS_DB_URL=postgres://iris:iris@localhost:5432/iris?sslmode=disable
make migrar                       # iris migrar: goose (embebidas), River y permisos
cd apps/api && go run ./cmd/iris  # API en :8080 (PORT)
cd apps/api && go run ./cmd/worker
cd apps/api && go run ./cmd/iris admin crear --correo admin@iris.local --nombre Admin
```

La API lee las variables del entorno (no carga `.env` sola): `set -a; . ./.env; set +a` antes de
correrla. Para MFA, conectores y buzones hace falta `IRIS_CLAVE_CIFRADO` (`openssl rand -base64 32`).

## 4. Panel web

```sh
pnpm install
pnpm --filter ./apps/web dev      # http://localhost:3000
```

## 5. Todo en contenedores (perfil `app`)

```sh
make dev-app                      # construye api, worker y web, migra (servicio migrar) y levanta todo
make logs                         # sigue los logs
make parar                        # detiene (los volúmenes se conservan)
docker compose -f infra/docker-compose.yml --env-file .env --profile app run --rm api \
    admin crear --correo admin@iris.local --nombre Admin
```

- `migrar` corre `iris migrar` y termina; `api` y `worker` esperan a que salga bien
  (`service_completed_successfully`). Si una migración falla, ni la API ni el worker arrancan:
  `make logs` muestra el error de `migrar`.
- api, worker y migrar cargan `.env.example` y luego `.env` (`env_file`); el compose solo
  reemplaza lo que cambia en su red (ver el encabezado de `.env.example`).
- Panel en `http://localhost:3000` (proxy de `/api` hacia `api:8080`); API directa en
  `http://localhost:8080/api/v1/salud`. Cookies `Secure` funcionan en `http://localhost`; con otro
  host por HTTP, `IRIS_COOKIE_SEGURA=false`.
- Detrás de un proxy que intercepta TLS, el build necesita su CA: `infra/imagenes/README.md`.

Borrar datos locales: `docker compose -f infra/docker-compose.yml down -v` (pierdes la base, los
adjuntos y el OSRM preprocesado).

## 6. Verificar antes de subir

```sh
make lint        # golangci-lint + eslint + shellcheck (si está) + docker compose config
make typecheck   # go build/vet + tsc (también comprueba los permisos que usa el panel)
IRIS_DB_URL=postgres://iris:iris@localhost:5432/iris_test_<agente>?sslmode=disable make test
make contratos   # make openapi + pnpm contratos:build; commitea packages/contratos si cambió
make e2e         # Playwright (construye el panel; IRIS_E2E_SIN_BUILD=1 reutiliza el build)
make imagenes    # construye iris/api, iris/worker e iris/web como CI (necesita Docker)
```

Permisos del panel: `menu.ts`, `RequierePermiso` y `puede()` solo aceptan el tipo `Permiso`, que
es el enum del OpenAPI generado de `internal/nucleo/permisos.go`. Con `packages/contratos` al día
(job `contratos` de CI), `make typecheck` falla si el panel usa un permiso que no existe.

Los objetivos agregados dicen qué omitieron (por ejemplo, Go mientras no exista `apps/api`); los de
Go (`openapi`, `sqlc`, `migrar`, `lint-go`, `test-go`) fallan con un mensaje si falta `apps/api`.

Cada agente prueba contra su propia base (`iris_test_<agente>`) y su puerto (`PORT=35<n>`); nunca la
de otro.

## 7. Traer `main` a tu rama

```sh
IRIS_DB=iris_test_<agente>_integrar scripts/integrar.sh agente/<agente>          # solo verifica
IRIS_DB=iris_test_<agente>_integrar scripts/integrar.sh agente/<agente> --push   # y sube si pasa
```

Mezcla `origin/main` en un worktree temporal, une los `.claude/integration/*.md`, regenera
lockfile, `go.sum` y contratos, recrea la base de pruebas y corre `make lint`, `make typecheck` y
`make test`. Variables de conexión: `PGHOST`, `PGPORT`, `PGUSER`, `PGPASSWORD`.

## Problemas comunes

- **`port is already allocated`:** otro Postgres/Redis usa el puerto; cambia `IRIS_*_PUERTO`.
- **`osrm` reiniciándose:** faltan datos; corre `infra/osrm/preparar.sh`.
- **`minio-init` falla:** MinIO no arrancó en 60 s; revisa `make logs` y que
  `IRIS_S3_SECRET_KEY` tenga al menos 8 caracteres.
- **mailpit no arranca o monta un directorio:** faltan los certificados; corre
  `infra/mailpit/certificados.sh` (o `make dev`).
- **La API termina con `IRIS_CORREO_CA: …`:** igual que el anterior (el compose monta
  `infra/mailpit/certs/ca.pem`).
- **`additional privileges requested: --allow=fs.read=…`:** `IRIS_BUILD_CA_EXTRA` apunta fuera del
  repo; cópiala a `infra/imagenes/ca-extra.pem` (ver `infra/imagenes/README.md`).
- **Build de imágenes con `x509: certificate signed by unknown authority`:** estás detrás de un proxy
  que intercepta TLS; mismo arreglo que el anterior.
