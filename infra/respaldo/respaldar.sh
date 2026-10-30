#!/usr/bin/env bash
# Respaldo cifrado de la base de Iris: pg_dump (formato custom, todos los schemas: nucleo, soporte,
# campo, integracion, auditoria, river…) cifrado con age hacia una o más claves públicas.
#
#   infra/respaldo/respaldar.sh               # pg_dump local contra IRIS_DB_URL
#   infra/respaldo/respaldar.sh --compose     # pg_dump dentro del contenedor postgres del compose
#
# Variables (entorno o ENV_FILE, por defecto .env o .env.example):
#   IRIS_DB_URL                    base a respaldar (sin --compose)
#   IRIS_RESPALDO_DESTINATARIOS    archivo con las claves públicas age (age1…), una por línea
#   IRIS_RESPALDO_DIR              carpeta de salida (./infra/respaldo/salida)
#   IRIS_RESPALDO_DIAS             días que se conservan los respaldos (14; 0 = no borrar)
#   IRIS_COMPOSE_ARCHIVO           compose de --compose (por defecto infra/docker-compose.yml; la
#                                  instalación de producción usa <dir>/current/docker-compose.yml)
#
# Por qué age con claves públicas: el servidor que respalda solo tiene la clave pública y no puede
# leer los respaldos; la clave privada (IRIS_RESPALDO_IDENTIDAD) vive fuera, solo para restaurar.
# age es un binario sin estado (sin llavero ni agente como gpg) y no pide frase por terminal, así
# que corre en cron y en CI. Deja <base>-<fecha UTC>.dump.age y su .sha256. Docs:
# docs/runbooks/respaldos.md.
set -euo pipefail

raiz="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
compose=0
[ "${1:-}" = "--compose" ] && compose=1

env_file="${ENV_FILE:-$([ -f "$raiz/.env" ] && echo "$raiz/.env" || echo "$raiz/.env.example")}"
# Lee una variable del entorno o, si no está, de ENV_FILE.
leer() {
    local v="${!1:-}"
    [ -n "$v" ] || v="$(sed -n "s/^$1=//p" "$env_file" 2>/dev/null | tail -1)"
    printf '%s' "${v:-${2:-}}"
}

destinatarios="$(leer IRIS_RESPALDO_DESTINATARIOS)"
dir="$(leer IRIS_RESPALDO_DIR "$raiz/infra/respaldo/salida")"
dias="$(leer IRIS_RESPALDO_DIAS 14)"
case "$dir" in /*) ;; *) dir="$raiz/${dir#./}" ;; esac

command -v age >/dev/null || { echo "Falta age (apt install age / brew install age)." >&2; exit 1; }
if [ -z "$destinatarios" ] || [ ! -s "$destinatarios" ]; then
    echo "IRIS_RESPALDO_DESTINATARIOS debe apuntar a un archivo con claves públicas age (age-keygen -y)." >&2
    exit 1
fi
if grep -q 'AGE-SECRET-KEY' "$destinatarios"; then
    echo "IRIS_RESPALDO_DESTINATARIOS tiene una clave PRIVADA: ahí va solo la pública (age-keygen -y)." >&2
    exit 1
fi

if [ "$compose" = 1 ]; then
    base="$(leer IRIS_PG_BASE iris)"
    usuario="$(leer IRIS_PG_USUARIO iris)"
    volcar() {
        docker compose -f "${IRIS_COMPOSE_ARCHIVO:-$raiz/infra/docker-compose.yml}" --env-file "$env_file" exec -T postgres \
            pg_dump --format=custom --compress=6 --username "$usuario" --dbname "$base"
    }
else
    url="$(leer IRIS_DB_URL)"
    [ -n "$url" ] || { echo "Falta IRIS_DB_URL." >&2; exit 1; }
    command -v pg_dump >/dev/null || { echo "Falta pg_dump (cliente de PostgreSQL 16)." >&2; exit 1; }
    base="$(psql "$url" -XAtqc 'select current_database()')"
    volcar() { pg_dump --format=custom --compress=6 --dbname "$url"; }
fi

mkdir -p "$dir"
fecha="$(date -u +%Y%m%dT%H%M%SZ)"
nombre="$base-$fecha.dump.age"
parcial="$dir/.$nombre.parcial"
trap 'rm -f "$parcial"' EXIT

# pipefail: si pg_dump falla, falla el respaldo (no queda un archivo cortado con nombre final).
volcar | age --encrypt --recipients-file "$destinatarios" --output "$parcial"
mv "$parcial" "$dir/$nombre"
(cd "$dir" && sha256sum "$nombre" >"$nombre.sha256")
echo "Respaldo: $dir/$nombre ($(du -h "$dir/$nombre" | cut -f1))"

if [ "$dias" -gt 0 ] 2>/dev/null; then
    # También los volcados sin cifrar del instalador de producción (*.dump, cuando no hay age).
    find "$dir" -maxdepth 1 -type f \( -name '*.dump.age' -o -name '*.dump.age.sha256' -o -name '*.dump' -o -name '*.dump.sha256' \) \
        -mtime +"$dias" -print -delete | sed 's/^/Borrado (más de '"$dias"' días): /'
fi
