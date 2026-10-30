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
echo "compose de producción: ok"

"$aqui/package.sh" 0.0.0 "$tmp/dist" > /dev/null
(cd "$tmp/dist" && sha256sum -c --quiet iris-0.0.0.tar.gz.sha256 install.sh.sha256)
for f in docker-compose.yml env.example install.sh README.md VERSION backup/respaldar.sh backup/restaurar.sh; do
    tar -tzf "$tmp/dist/iris-0.0.0.tar.gz" "iris-0.0.0/$f" > /dev/null
done
bash "$tmp/dist/install.sh" --help > /dev/null
echo "paquete de instalación: ok"
