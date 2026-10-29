#!/usr/bin/env bash
# Prueba de punta a punta Iris ↔ ArgosISP (docs/runbooks/e2e-argos.md). No corre en el CI de las PR.
#
#   infra/e2e/argos.sh              levanta Argos (v1.3.0) e Iris, corre los pasos y apaga todo
#   E2E_DEJAR=1 infra/e2e/argos.sh  igual, pero deja los procesos arriba para mirar
#   infra/e2e/argos.sh parar        apaga lo que haya quedado arriba
#   infra/e2e/argos.sh limpiar      parar + borra las bases y la carpeta de trabajo
#
# Sin Docker: Argos corre desde su código (etiqueta de la versión, solo lectura) con node, su propia
# base en el Postgres de E2E_PG y un Redis propio. Iris se compila del árbol actual. Necesita: git,
# curl, node 22 y pnpm 10, go, psql, redis-server y python3.
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RAIZ="$(cd "$DIR/../.." && pwd)"
ARGOS_VERSION="${ARGOS_VERSION:-1.3.0}"
ARGOS_REPO="${ARGOS_REPO:-https://github.com/HCDestroyer/argos-isp}"
TRABAJO="${E2E_TRABAJO:-$DIR/.trabajo}"
ARGOS_DIR="${E2E_ARGOS_DIR:-$TRABAJO/argos-isp}"
PG="${E2E_PG:-postgres://iris:iris@localhost:55432}"
BASE_IRIS="${E2E_BASE_IRIS:-iris_e2e}"
BASE_ARGOS="${E2E_BASE_ARGOS:-argos_e2e}"
PUERTO_ARGOS="${E2E_PUERTO_ARGOS:-3520}"
PUERTO_IRIS="${E2E_PUERTO_IRIS:-3521}"
PUERTO_SMTP="${E2E_PUERTO_SMTP:-3528}"
PUERTO_REDIS="${E2E_PUERTO_REDIS:-3529}"
# Árbol de Iris que se compila (por defecto, este); otro para probar una rama sin mergear.
IRIS_API_DIR="${E2E_IRIS_API_DIR:-$RAIZ/apps/api}"

log() { printf '\n[e2e %s] %s\n' "$(date -u +%H:%M:%S)" "$*"; }
falla() { printf '[e2e] ERROR: %s\n' "$*" >&2; exit 1; }

pid_de() { cat "$TRABAJO/pids/$1" 2>/dev/null || true; }
arrancar() { # nombre, carpeta, comando…
    local nombre="$1" carpeta="$2"
    shift 2
    mkdir -p "$TRABAJO/pids" "$TRABAJO/logs"
    (cd "$carpeta" && exec "$@" >>"$TRABAJO/logs/$nombre.log" 2>&1) &
    echo $! >"$TRABAJO/pids/$nombre"
}
detener() {
    local p
    p="$(pid_de "$1")"
    if [[ -n "$p" ]] && kill -0 "$p" 2>/dev/null; then
        kill "$p" 2>/dev/null || true
        for _ in $(seq 1 30); do kill -0 "$p" 2>/dev/null || break; sleep 0.2; done
        kill -9 "$p" 2>/dev/null || true
    fi
    rm -f "$TRABAJO/pids/$1"
}
esperar_http() { # url, segundos
    for _ in $(seq 1 "$2"); do
        [[ "$(curl -s -o /dev/null -w '%{http_code}' "$1" || true)" == 200 ]] && return 0
        sleep 1
    done
    return 1
}
parar_todo() {
    for n in iris-worker iris-api argos-notify argos-iris argos-relay argos-api smtp; do detener "$n"; done
    if [[ -f "$TRABAJO/pids/redis" ]]; then
        redis-cli -p "$PUERTO_REDIS" shutdown nosave >/dev/null 2>&1 || true
        rm -f "$TRABAJO/pids/redis"
    fi
}
# shellcheck source=/dev/null
argos_env() { set -a; . "$TRABAJO/argos.env"; set +a; }
arrancar_argos_api() {
    argos_env
    arrancar argos-api "$ARGOS_DIR/apps/api" node --import=ts-node-maintained/register/esm bin/server.ts
    esperar_http "http://127.0.0.1:$PUERTO_ARGOS/health" 90 || falla "la API de Argos no arrancó (logs/argos-api.log)"
}

case "${1:-}" in
    parar) parar_todo; exit 0 ;;
    limpiar)
        parar_todo
        psql "$PG/postgres" -qc "DROP DATABASE IF EXISTS $BASE_IRIS WITH (FORCE)" -c "DROP DATABASE IF EXISTS $BASE_ARGOS WITH (FORCE)"
        rm -rf "$TRABAJO"
        exit 0 ;;
    parar-argos-api) detener argos-api; exit 0 ;;
    arrancar-argos-api) arrancar_argos_api; exit 0 ;;
    "") ;;
    *) falla "orden desconocida: $1 (parar, limpiar)" ;;
esac

for h in git curl node pnpm go psql redis-server redis-cli python3 openssl sha256sum; do
    command -v "$h" >/dev/null || falla "falta $h"
done
psql "$PG/postgres" -Atc 'select 1' >/dev/null || falla "no hay Postgres en $PG"
mkdir -p "$TRABAJO"
parar_todo
if [[ "${E2E_DEJAR:-}" != 1 ]]; then trap parar_todo EXIT; fi

# 1. Argos: la release publicada (verificada) y su código en la misma etiqueta ----------------------
log "Argos v$ARGOS_VERSION: release y sha256"
REL="$TRABAJO/release"
mkdir -p "$REL"
for f in "argosisp-$ARGOS_VERSION.tar.gz" "argosisp-$ARGOS_VERSION.tar.gz.sha256"; do
    [[ -s "$REL/$f" ]] || curl -fsSL -o "$REL/$f" "$ARGOS_REPO/releases/download/v$ARGOS_VERSION/$f"
done
(cd "$REL" && sha256sum -c "argosisp-$ARGOS_VERSION.tar.gz.sha256") || falla "sha256 de la release no coincide"
VERSION_REL="$(tar xzf "$REL/argosisp-$ARGOS_VERSION.tar.gz" -O "argosisp-$ARGOS_VERSION/VERSION")"
[[ "$VERSION_REL" == "$ARGOS_VERSION" ]] || falla "la release dice VERSION=$VERSION_REL"
# La release es un despliegue con Docker (imágenes de GHCR); sin Docker se corre su código.
if [[ ! -d "$ARGOS_DIR/.git" ]]; then
    git clone -q --depth 1 --branch "v$ARGOS_VERSION" "$ARGOS_REPO" "$ARGOS_DIR"
fi
ETIQUETA="$(git -C "$ARGOS_DIR" describe --tags --exact-match 2>/dev/null || true)"
[[ "$ETIQUETA" == "v$ARGOS_VERSION" ]] || falla "$ARGOS_DIR no está en v$ARGOS_VERSION ($ETIQUETA)"
[[ -z "$(git -C "$ARGOS_DIR" status --porcelain)" ]] || falla "$ARGOS_DIR tiene cambios: no se toca el código de Argos"
log "Argos: dependencias (pnpm, solo @argosisp/api)"
(cd "$ARGOS_DIR" && pnpm install --frozen-lockfile --filter '@argosisp/api...' >"$TRABAJO/pnpm.log" 2>&1) || falla "pnpm install (pnpm.log)"

# 2. Bases, Redis y sumidero SMTP ------------------------------------------------------------------
log "bases $BASE_ARGOS y $BASE_IRIS (se recrean), Redis :$PUERTO_REDIS y SMTP :$PUERTO_SMTP"
psql "$PG/postgres" -qc "DROP DATABASE IF EXISTS $BASE_IRIS WITH (FORCE)" -c "DROP DATABASE IF EXISTS $BASE_ARGOS WITH (FORCE)" \
    -c "CREATE DATABASE $BASE_IRIS" -c "CREATE DATABASE $BASE_ARGOS"
redis-server --port "$PUERTO_REDIS" --bind 127.0.0.1 --daemonize yes --save "" --dir "$TRABAJO" --logfile "$TRABAJO/redis.log"
mkdir -p "$TRABAJO/pids" && echo redis >"$TRABAJO/pids/redis"
rm -rf "$TRABAJO/correos"
arrancar smtp "$DIR" python3 -I "$DIR/smtp_sumidero.py" "$PUERTO_SMTP" "$TRABAJO/correos"

# 3. Argos -----------------------------------------------------------------------------------------
PGHOST_PUERTO="${PG#*://}"; PGHOST_PUERTO="${PGHOST_PUERTO#*@}"
PG_USUARIO="${PG#*://}"; PG_USUARIO="${PG_USUARIO%%@*}"
cat >"$TRABAJO/argos.env" <<EOF
NODE_ENV=development
PORT=$PUERTO_ARGOS
HOST=127.0.0.1
LOG_LEVEL=warn
APP_KEY=$(openssl rand -hex 24)
INTERNAL_TOKEN=$(openssl rand -hex 24)
AAA_PPP_KEY=$(openssl rand -base64 32)
DB_HOST=${PGHOST_PUERTO%%:*}
DB_PORT=${PGHOST_PUERTO##*:}
DB_USER=${PG_USUARIO%%:*}
DB_PASSWORD=${PG_USUARIO#*:}
DB_DATABASE=$BASE_ARGOS
REDIS_HOST=127.0.0.1
REDIS_PORT=$PUERTO_REDIS
COLAS_PREFIJO=argose2e
SMTP_HOST=127.0.0.1
SMTP_PORT=$PUERTO_SMTP
SMTP_SECURE=false
MAIL_FROM="Argos E2E <no-responder@argos-e2e.test>"
API_PUBLIC_URL=http://127.0.0.1:$PUERTO_ARGOS
IRIS_PERMITIR_HTTP=true
SEED_ADMIN_EMAIL=admin@argos-e2e.test
SEED_ADMIN_PASSWORD=admin-e2e-12345
EOF
chmod 600 "$TRABAJO/argos.env"
log "Argos: migraciones y seed"
argos_env
(cd "$ARGOS_DIR/apps/api" && node ace migration:run --force && node ace db:seed) >"$TRABAJO/logs/argos-migrar.log" 2>&1 \
    || falla "migraciones de Argos (logs/argos-migrar.log)"
log "Argos: API :$PUERTO_ARGOS, relay, iris:worker y notify:worker"
arrancar_argos_api
arrancar argos-relay "$ARGOS_DIR/apps/api" node ace eventos:relay
arrancar argos-iris "$ARGOS_DIR/apps/api" node ace iris:worker
arrancar argos-notify "$ARGOS_DIR/apps/api" node ace notify:worker

# 4. Iris ------------------------------------------------------------------------------------------
log "Iris: compilar ($IRIS_API_DIR), migrar y levantar :$PUERTO_IRIS"
mkdir -p "$TRABAJO/bin" "$TRABAJO/adjuntos"
(cd "$IRIS_API_DIR" && go build -o "$TRABAJO/bin/iris" ./cmd/iris && go build -o "$TRABAJO/bin/iris-worker" ./cmd/worker)
cat >"$TRABAJO/iris.env" <<EOF
IRIS_DB_URL=$PG/$BASE_IRIS?sslmode=disable
PORT=$PUERTO_IRIS
IRIS_LOG_NIVEL=info
IRIS_COOKIE_SEGURA=false
IRIS_CLAVE_CIFRADO=$(openssl rand -base64 32)
IRIS_BASE_URL=http://127.0.0.1:$PUERTO_IRIS
IRIS_CONECTORES_PERMITIR_HTTP=true
IRIS_ARGOS_REDES_PERMITIDAS=127.0.0.1/32
IRIS_ADJUNTOS_ALMACEN=local
IRIS_ADJUNTOS_DIR=$TRABAJO/adjuntos
IRIS_RELAY_INTERVALO=1s
EOF
chmod 600 "$TRABAJO/iris.env"
set -a
# shellcheck source=/dev/null
. "$TRABAJO/iris.env"
set +a
"$TRABAJO/bin/iris" migrar >"$TRABAJO/logs/iris-migrar.log" 2>&1 || falla "iris migrar (logs/iris-migrar.log)"
echo "admin-iris-e2e-123" | "$TRABAJO/bin/iris" admin crear --correo admin@iris-e2e.test --nombre "Admin E2E" --contrasena-stdin >/dev/null
arrancar iris-api "$TRABAJO" "$TRABAJO/bin/iris" servir
arrancar iris-worker "$TRABAJO" "$TRABAJO/bin/iris-worker"
esperar_http "http://127.0.0.1:$PUERTO_IRIS/api/v1/salud" 30 || falla "Iris no arrancó (logs/iris-api.log)"

# 5. Pasos -----------------------------------------------------------------------------------------
log "pasos (infra/e2e/argos_e2e.py)"
export E2E_ARGOS_URL="http://127.0.0.1:$PUERTO_ARGOS" E2E_IRIS_URL="http://127.0.0.1:$PUERTO_IRIS"
export E2E_ARGOS_ADMIN=admin@argos-e2e.test E2E_ARGOS_CLAVE=admin-e2e-12345
export E2E_IRIS_ADMIN=admin@iris-e2e.test E2E_IRIS_CLAVE=admin-iris-e2e-123
export E2E_IRIS_DB="$PG/$BASE_IRIS" E2E_ARGOS_DB="$PG/$BASE_ARGOS" E2E_CORREOS="$TRABAJO/correos"
export E2E_ARGOS_PARAR="'$DIR/argos.sh' parar-argos-api" E2E_ARGOS_ARRANCAR="'$DIR/argos.sh' arrancar-argos-api"
# Secreto del receptor de Iris guardado en Argos (cifrado con AAA_PPP_KEY): firma y deduplicación.
export E2E_SECRETO_CMD="psql '$PG/$BASE_ARGOS' -Atc 'select webhook_salida_secreto_cifrado from integraciones.iris_conector' | \
AAA_PPP_KEY='$AAA_PPP_KEY' node '$DIR/secreto_argos.mjs'"
set +e
python3 -I "$DIR/argos_e2e.py" | tee "$TRABAJO/reporte.txt"
salida=${PIPESTATUS[0]}
set -e
log "reporte en $TRABAJO/reporte.txt; logs en $TRABAJO/logs"
exit "$salida"
