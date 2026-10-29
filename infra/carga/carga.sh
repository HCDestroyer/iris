#!/usr/bin/env bash
# Prueba de carga de Iris de punta a punta (docs/runbooks/carga.md). La corre `make carga` y
# `make carga-corta`; también el workflow .github/workflows/carga.yml.
#
#   infra/carga/carga.sh              construye, levanta, siembra, corre k6 y verifica
#   infra/carga/carga.sh limpiar      baja el compose de carga y borra su volumen y .salida
#
# Variables (con su valor por defecto):
#   CARGA_DURACION=15m CARGA_TECNICOS=50 CARGA_PANEL=20   escenarios de k6
#   CARGA_ABIERTOS=10000 CARGA_RESUELTOS=50000            tickets sembrados
#   CARGA_ORDENES_POR_TECNICO=8
#   CARGA_CONSTRUIR=1        0 = usar iris/api:carga e iris/worker:carga ya construidas
#   CARGA_MANTENER=0         1 = dejar el compose arriba al terminar (para medir a mano)
#   IRIS_CARGA_API_PUERTO=3505 IRIS_CARGA_PG_PUERTO=3515
#   K6_IMAGEN=grafana/k6:1.3.0
#   IRIS_BUILD_CA_EXTRA      CA extra para construir detrás de un proxy que intercepta TLS
#   CARGA_BUILD_RED          red del build (p. ej. host, si el proxy solo escucha en localhost)
#
# Resultados en infra/carga/.salida/ (ignorada por git): resumen.txt y resumen.json de k6,
# verificacion.txt (duplicados y cron de SLA) y pg_top.txt (consultas más caras según
# pg_stat_statements). Sale distinto de 0 si k6 cruzó un umbral o hubo duplicados.

set -euo pipefail

RAIZ="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DIR="$RAIZ/infra/carga"
SALIDA="$DIR/.salida"
ENV_FILE="${ENV_FILE:-$RAIZ/.env.example}"
export IRIS_CARGA_API_PUERTO="${IRIS_CARGA_API_PUERTO:-3505}"
export IRIS_CARGA_PG_PUERTO="${IRIS_CARGA_PG_PUERTO:-3515}"
export IRIS_CARGA_TAG="${IRIS_CARGA_TAG:-carga}"
K6_IMAGEN="${K6_IMAGEN:-grafana/k6:1.3.0}"
EMPRESA="${CARGA_EMPRESA:-9001}"
compose=(docker compose -p iris-carga -f "$RAIZ/infra/docker-compose.yml" -f "$DIR/compose.yml"
    --env-file "$ENV_FILE" --profile app)

log() { printf '\n[carga %s] %s\n' "$(date -u +%H:%M:%S)" "$*"; }
psql_carga() { "${compose[@]}" exec -T postgres psql -U iris -d iris -X -q -v ON_ERROR_STOP=1 "$@"; }

if [[ "${1:-}" == "limpiar" ]]; then
    "${compose[@]}" down -v --remove-orphans
    rm -rf "$SALIDA"
    exit 0
fi

command -v node >/dev/null || { echo "carga: falta node (22) para la siembra" >&2; exit 1; }
mkdir -p "$SALIDA"
rm -f "$SALIDA"/resumen.* "$SALIDA"/verificacion.txt "$SALIDA"/pg_top.txt

if [[ "${CARGA_CONSTRUIR:-1}" == "1" ]]; then
    log "construyendo iris/api:$IRIS_CARGA_TAG e iris/worker:$IRIS_CARGA_TAG"
    extra=()
    [[ -n "${IRIS_BUILD_CA_EXTRA:-}" ]] && extra+=(--secret "id=ca_extra,src=$IRIS_BUILD_CA_EXTRA")
    [[ -n "${CARGA_BUILD_RED:-}" ]] && extra+=(--network "$CARGA_BUILD_RED")
    for t in api worker; do
        docker build -q -f "$RAIZ/apps/api/Dockerfile" --target "$t" "${extra[@]}" \
            --build-arg VERSION=carga -t "iris/$t:$IRIS_CARGA_TAG" "$RAIZ" >/dev/null
    done
fi

log "levantando postgres, redis, migrar y api (proyecto iris-carga)"
# El worker se detiene durante la siembra: su cron de SLA arranca con la prueba.
"${compose[@]}" stop worker >/dev/null 2>&1 || true
"${compose[@]}" up -d --wait postgres redis api

# Administrador y contraseña de esta corrida: al azar, solo en memoria (nunca en el repo).
CONTRASENA="$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')"
ADMIN="admin-$(date -u +%Y%m%d%H%M%S)-$RANDOM@carga.local"
printf '%s\n' "$CONTRASENA" | "${compose[@]}" run --rm -T --no-deps api admin crear \
    --correo "$ADMIN" --nombre "Administrador de carga" --contrasena-stdin >/dev/null

log "siembra por la API (empresa, zonas, cuadrillas, técnicos, órdenes del día, usuarios del panel)"
CARGA_API="http://127.0.0.1:$IRIS_CARGA_API_PUERTO" CARGA_SALIDA="$SALIDA" CARGA_EMPRESA="$EMPRESA" \
CARGA_ADMIN_CORREO="$ADMIN" CARGA_ADMIN_CONTRASENA="$CONTRASENA" CARGA_CONTRASENA="$CONTRASENA" \
CARGA_TECNICOS="${CARGA_TECNICOS_SIEMBRA:-50}" CARGA_ORDENES_POR_TECNICO="${CARGA_ORDENES_POR_TECNICO:-8}" \
    node "$DIR/siembra/api.mjs"

log "siembra de tickets por SQL"
lista() { node -e 'const u=require(process.argv[1]); const v=eval(process.argv[2]); console.log("{"+v.join(",")+"}")' "$SALIDA/usuarios.json" "$1"; }
psql_carga -v empresa="$EMPRESA" -v abiertos="${CARGA_ABIERTOS:-10000}" -v resueltos="${CARGA_RESUELTOS:-50000}" \
    -v soporte="$(lista 'u.soporte.map(x=>x.id)')" -v zonas="$(lista 'u.zonas')" -v cuadrillas="$(lista 'u.cuadrillas')" \
    -f - <"$DIR/siembra/tickets.sql"

psql_carga -c 'CREATE EXTENSION IF NOT EXISTS pg_stat_statements' -c 'SELECT pg_stat_statements_reset()' >/dev/null
DESDE="$(psql_carga -At -c "SELECT to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"')")"

log "worker arriba (relay del outbox y cron de SLA cada minuto)"
"${compose[@]}" up -d worker

log "k6 ($K6_IMAGEN): ${CARGA_DURACION:-15m}, ${CARGA_TECNICOS:-50} técnicos, ${CARGA_PANEL:-20} del panel"
# La contraseña va a k6 por un archivo temporal (no por -e: se vería en `ps` y en `docker inspect`).
K6_ENV="$(mktemp)"
chmod 600 "$K6_ENV"
trap 'rm -f "$K6_ENV"' EXIT
printf 'CARGA_CONTRASENA=%s\n' "$CONTRASENA" >"$K6_ENV"
set +e
docker run --rm --env-file "$K6_ENV" --network host --user "$(id -u):$(id -g)" -v "$DIR:/carga" -w /carga/escenarios \
    -e CARGA_API="http://127.0.0.1:$IRIS_CARGA_API_PUERTO" \
    -e CARGA_USUARIOS=/carga/.salida/usuarios.json -e CARGA_RESUMEN_DIR=/carga/.salida \
    -e CARGA_DURACION="${CARGA_DURACION:-15m}" -e CARGA_TECNICOS="${CARGA_TECNICOS:-50}" -e CARGA_PANEL="${CARGA_PANEL:-20}" \
    -e CARGA_INTERVALO_SYNC -e CARGA_INTERVALO_PAQUETE -e CARGA_REENVIO -e K6_NO_USAGE_REPORT=true \
    "$K6_IMAGEN" run --quiet carga.js
k6_salida=$?
set -e

# Consultas más caras de la corrida (antes de verificar, para no mezclar las de verificar.sql).
psql_carga -P pager=off -c "
    SELECT calls, round(mean_exec_time::numeric, 1) AS ms_medio, round(max_exec_time::numeric, 1) AS ms_max,
           round(total_exec_time::numeric) AS ms_total, rows, left(regexp_replace(query, '\s+', ' ', 'g'), 220) AS consulta
    FROM pg_stat_statements
    WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
    ORDER BY total_exec_time DESC LIMIT 30" >"$SALIDA/pg_top.txt"

log "verificando la base (duplicados, cron de SLA)"
set +e
psql_carga -v desde="$DESDE" -f - <"$DIR/verificar.sql" 2>&1 | tee "$SALIDA/verificacion.txt"
verif_salida=${PIPESTATUS[0]}
set -e

log "consultas más caras (pg_stat_statements) en $SALIDA/pg_top.txt"
head -n 12 "$SALIDA/pg_top.txt" | cut -c1-200

if [[ "${CARGA_MANTENER:-0}" != "1" ]]; then
    "${compose[@]}" stop >/dev/null 2>&1
fi

if [[ $k6_salida -ne 0 || $verif_salida -ne 0 ]]; then
    log "FALLA: k6 salió con $k6_salida (99 = umbral cruzado) y la verificación con $verif_salida"
    exit 1
fi
log "OK: umbrales de k6 y verificación de la base en verde"
