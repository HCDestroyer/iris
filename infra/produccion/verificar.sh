#!/usr/bin/env bash
# Checks the production install files without Docker images: `docker compose config` of the
# production compose with the env template (and the osrm profile), and that package.sh builds a
# package whose tarball holds what install.sh expects. Used by `make lint-infra` and CI.
set -euo pipefail

aqui="$(cd "$(dirname "$0")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

sed -e "s|^IRIS_DIR=.*|IRIS_DIR=$tmp|" -e 's|^IRIS_VERSION=.*|IRIS_VERSION=0.0.0|' \
    -e 's|^IRIS_BASE_URL=.*|IRIS_BASE_URL=https://iris.example.com|' \
    -e 's|^IRIS_PG_CONTRASENA=.*|IRIS_PG_CONTRASENA=verificar|' \
    -e 's|^IRIS_S3_SECRET_KEY=.*|IRIS_S3_SECRET_KEY=verificar-s3|' "$aqui/.env.example" > "$tmp/.env"
docker compose --env-file "$tmp/.env" -f "$aqui/docker-compose.yml" --profile osrm config --quiet
docker compose --env-file "$tmp/.env" -f "$aqui/docker-compose.yml" -f "$aqui/docker-compose.admin-ports.yml" --profile osrm config --quiet
echo "compose de producción: ok"

# La mensajería borra del almacén la foto de un mensaje del chat eliminado: debe ver el mismo
# almacenamiento de adjuntos que la API (variables IRIS_ADJUNTOS_ALMACEN, IRIS_ADJUNTOS_DIR e
# IRIS_S3_*, y los mismos volúmenes), en producción y en dev.
mismo_almacen() { # <descripción> <argumentos de docker compose…>
    local desc="$1"; shift
    local dif
    dif="$(docker compose "$@" config --format json | jq -r '
        def almacen($s): {env: (.services[$s].environment // {} | with_entries(select(.key | test("^IRIS_(ADJUNTOS_ALMACEN|ADJUNTOS_DIR|S3_)")))),
                          vol: ([.services[$s].volumes // [] | .[] | .target] | sort)};
        . as $c | ["mensajeria", "worker"][] as $s | select(($c | almacen($s)) != ($c | almacen("api"))) | $s')"
    if [ -n "$dif" ]; then
        echo "$desc: estos servicios no tienen el mismo almacenamiento de adjuntos que la API: $dif" >&2
        exit 1
    fi
    echo "$desc: mensajería y worker con el almacén de adjuntos de la API"
}
mismo_almacen "compose de producción" --env-file "$tmp/.env" -f "$aqui/docker-compose.yml"

# Notificaciones push: la API (estado en /salud) y el worker (envío) montan de solo lectura la carpeta
# de secretos con el JSON de la cuenta de servicio de Firebase, y el .env de ejemplo trae IRIS_FCM_*
# vacías (push apagado por defecto; docs/runbooks/notificaciones-push.md).
secretos="$(docker compose --env-file "$tmp/.env" -f "$aqui/docker-compose.yml" config --format json | jq -r '
    . as $c | ["api", "worker"][] as $s
    | select([$c.services[$s].volumes // [] | .[] | select(.target == "/run/iris/secrets" and .read_only == true)] | length != 1) | $s')"
if [ -n "$secretos" ]; then
    echo "compose de producción: sin /run/iris/secrets de solo lectura en: $secretos" >&2
    exit 1
fi
for v in IRIS_FCM_PROYECTO IRIS_FCM_CREDENCIALES; do
    grep -qE "^$v=$" "$aqui/.env.example" || { echo "$v debe estar vacía en infra/produccion/.env.example" >&2; exit 1; }
done
echo "compose de producción: secretos de push montados de solo lectura en api y worker"
mismo_almacen "compose de dev" -f "$aqui/../docker-compose.yml" --env-file "$aqui/../../.env.example" --profile app

"$aqui/package.sh" 0.0.0 "$tmp/dist" > /dev/null
(cd "$tmp/dist" && sha256sum -c --quiet iris-0.0.0.tar.gz.sha256 install.sh.sha256)
for f in docker-compose.yml docker-compose.admin-ports.yml env.example install.sh README.md VERSION MIGRATION backup/respaldar.sh backup/restaurar.sh; do
    tar -tzf "$tmp/dist/iris-0.0.0.tar.gz" "iris-0.0.0/$f" > /dev/null
done
bash "$tmp/dist/install.sh" --help > /dev/null
echo "paquete de instalación: ok"
