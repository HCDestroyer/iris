# Desarrollo local

Cómo levantar Iris en tu máquina. Todo lo de infraestructura corre en Docker Compose
(`infra/docker-compose.yml`); la API (Go) y el panel (Next.js) pueden correr en tu máquina o en el
perfil `app` del compose.

## Requisitos

- Docker con Compose v2 (daemon corriendo).
- Go 1.24 (`apps/api`), Node 22 (`.nvmrc`) y pnpm 10 (`corepack enable` toma la versión de
  `packageManager`).
- `make`, `curl`, y para `scripts/integrar.sh`, `psql`/`createdb`/`dropdb`.
- Opcional: `golangci-lint` v2 y `shellcheck` (los usa `make lint`). `sqlc` y `goose` no hacen
  falta: el `Makefile` los corre con `go run` en versiones fijadas.

## 1. Variables

```sh
cp .env.example .env
```

Los valores por defecto sirven para el compose local. Si ya tienes Postgres o Redis en 5432/6379,
cambia `IRIS_PG_PUERTO` / `IRIS_REDIS_PUERTO` y ajusta `IRIS_DB_URL` / `IRIS_REDIS_URL`. Nunca
pongas secretos reales en `.env.example`.

## 2. Servicios de infraestructura

```sh
make dev
```

| Servicio | Para qué | En tu máquina |
| --- | --- | --- |
| postgres 16 | base de datos (`iris`) | `localhost:5432` |
| redis 7 | caché (sin persistencia) | `localhost:6379` |
| minio | adjuntos S3; `minio-init` crea el bucket `iris-adjuntos` | API `:9000`, consola `http://localhost:9001` |
| mailpit | SMTP de prueba | SMTP `:1025`, UI `http://localhost:8025` |
| osrm | rutas por calle (Guatemala, perfil car, MLD) | `http://localhost:5000` |

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
make migrar                       # goose up con apps/api/migraciones
cd apps/api && go run ./cmd/iris  # API en :8080 (PORT)
cd apps/api && go run ./cmd/worker
```

## 4. Panel web

```sh
pnpm install
pnpm --filter ./apps/web dev      # http://localhost:3000
```

## 5. Todo en contenedores (perfil `app`)

```sh
make dev-app                      # construye api, worker y web desde apps/ y levanta todo
make logs                         # sigue los logs
make parar                        # detiene (los volúmenes se conservan)
```

Borrar datos locales: `docker compose -f infra/docker-compose.yml down -v` (pierdes la base, los
adjuntos y el OSRM preprocesado).

## 6. Verificar antes de subir

```sh
make lint        # golangci-lint + eslint + shellcheck + docker compose config
make typecheck   # go build/vet + tsc
IRIS_DB_URL=postgres://iris:iris@localhost:5432/iris_test_<agente>?sslmode=disable make test
make contratos   # make openapi + pnpm contratos:build; commitea packages/contratos si cambió
```

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
