#!/usr/bin/env bash
# Descarga el extracto de Guatemala (Geofabrik) y lo preprocesa para OSRM (perfil car, MLD)
# dentro del volumen `osrm` del compose, que luego sirve el servicio `osrm`.
#
#   infra/osrm/preparar.sh             # descarga y preprocesa (no hace nada si ya está listo)
#   infra/osrm/preparar.sh --forzar    # vuelve a descargar y preprocesar (extracto nuevo)
#
# Variables: IRIS_OSRM_PBF_URL (entorno o ENV_FILE; por defecto Guatemala en Geofabrik) y ENV_FILE
# (por defecto .env o, si no existe, .env.example). Requiere Docker con daemon y curl.
# El preprocesado de Guatemala tarda unos minutos y necesita ~2 GB de RAM.
set -euo pipefail

raiz="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$raiz"
forzar=0
[ "${1:-}" = "--forzar" ] && forzar=1

env_file="${ENV_FILE:-$([ -f .env ] && echo .env || echo .env.example)}"
url="${IRIS_OSRM_PBF_URL:-$(sed -n 's/^IRIS_OSRM_PBF_URL=//p' "$env_file" 2>/dev/null | tail -1)}"
url="${url:-https://download.geofabrik.de/central-america/guatemala-latest.osm.pbf}"
pbf="$(basename "$url")"          # guatemala-latest.osm.pbf
# Nombre fijo en el volumen, sea cual sea el extracto: el servicio osrm sirve /data/iris.osrm.
base=iris
compose=(docker compose -f infra/docker-compose.yml --env-file "$env_file")

if ! docker info >/dev/null 2>&1; then
    echo "Docker no responde (¿daemon detenido?). OSRM necesita Docker para preprocesar." >&2
    exit 1
fi

# Preprocesar con su misma imagen y su mismo volumen.
en_volumen() {
    "${compose[@]}" run --rm --no-deps -T --entrypoint /bin/sh osrm -c "$1"
}

if [ "$forzar" = 0 ] && en_volumen "test -f /data/$base.osrm.mldgr"; then
    echo "OSRM ya tiene el extracto preprocesado (/data/$base.osrm). Usa --forzar para actualizar el extracto."
    exit 0
fi

descarga="$(mktemp -d "${TMPDIR:-/tmp}/iris-osrm-XXXXXX")"
trap 'rm -rf "$descarga"' EXIT

echo "== Descargando $url"
curl -fL --retry 3 --retry-delay 5 -o "$descarga/$pbf" "$url"
if curl -fsSL -o "$descarga/$pbf.md5" "$url.md5"; then
    (cd "$descarga" && md5sum -c "$pbf.md5")
else
    echo "Aviso: no se pudo descargar $url.md5; se omite la verificación." >&2
fi

echo "== Copiando el extracto al volumen osrm"
"${compose[@]}" run --rm --no-deps -T -v "$descarga:/descarga:ro" --entrypoint /bin/sh osrm \
    -c "rm -f /data/$base.osrm* && cp /descarga/$pbf /data/$base.osm.pbf"

echo "== osrm-extract (perfil car)"
en_volumen "osrm-extract -p /opt/car.lua /data/$base.osm.pbf"
echo "== osrm-partition (MLD)"
en_volumen "osrm-partition /data/$base.osrm"
echo "== osrm-customize (MLD)"
en_volumen "osrm-customize /data/$base.osrm"
# El .pbf ya no hace falta para servir; se borra para no duplicar espacio en el volumen.
en_volumen "rm -f /data/$base.osm.pbf"

echo "Listo. Levanta el servicio: docker compose -f infra/docker-compose.yml --env-file $env_file up -d osrm"
echo "Prueba (Ciudad de Guatemala → Antigua): curl 'http://localhost:${IRIS_OSRM_PUERTO:-5000}/route/v1/driving/-90.5133,14.6349;-90.7295,14.5586?overview=false'"
