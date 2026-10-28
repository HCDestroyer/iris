#!/usr/bin/env bash
# Restaura un respaldo de infra/respaldo/respaldar.sh en una base de destino.
#
#   infra/respaldo/restaurar.sh <archivo.dump.age> <url de destino> [--limpiar]
#
# La base de destino debe existir y estar vacía (createdb iris_restaurada). Con --limpiar se
# borran antes los objetos que el respaldo trae (pg_restore --clean --if-exists): úsalo solo
# sabiendo qué base pisas. El destino se pasa siempre a mano (nunca se toma IRIS_DB_URL) para no
# restaurar sobre la base viva por accidente.
#
# Variables: IRIS_RESPALDO_IDENTIDAD (archivo con la clave privada age, AGE-SECRET-KEY-…),
# en el entorno o en ENV_FILE. Requiere age y pg_restore (cliente de PostgreSQL 16).
set -euo pipefail

uso() { echo "uso: $0 <archivo.dump.age> <postgres://… de destino> [--limpiar]" >&2; exit 2; }
[ $# -ge 2 ] || uso
archivo="$1"
destino="$2"
limpiar=0
[ "${3:-}" = "--limpiar" ] && limpiar=1
[ $# -le 3 ] || uso

raiz="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
env_file="${ENV_FILE:-$([ -f "$raiz/.env" ] && echo "$raiz/.env" || echo "$raiz/.env.example")}"
identidad="${IRIS_RESPALDO_IDENTIDAD:-$(sed -n 's/^IRIS_RESPALDO_IDENTIDAD=//p' "$env_file" 2>/dev/null | tail -1)}"

command -v age >/dev/null || { echo "Falta age." >&2; exit 1; }
command -v pg_restore >/dev/null || { echo "Falta pg_restore (cliente de PostgreSQL 16)." >&2; exit 1; }
[ -s "$archivo" ] || { echo "No existe el respaldo $archivo." >&2; exit 1; }
[ -n "$identidad" ] && [ -s "$identidad" ] || {
    echo "IRIS_RESPALDO_IDENTIDAD debe apuntar al archivo con la clave privada age." >&2
    exit 1
}

if [ -f "$archivo.sha256" ]; then
    (cd "$(dirname "$archivo")" && sha256sum --check --quiet "$(basename "$archivo").sha256")
    echo "Suma SHA-256 correcta."
else
    echo "Aviso: no hay $archivo.sha256; se restaura sin verificar la suma." >&2
fi

if [ "$limpiar" = 0 ]; then
    tablas="$(psql "$destino" -XAtqc "select count(*) from pg_tables where schemaname not in ('pg_catalog', 'information_schema')")"
    if [ "$tablas" != 0 ]; then
        echo "La base de destino tiene $tablas tablas. Usa una base vacía o --limpiar." >&2
        exit 1
    fi
    opciones=()
else
    opciones=(--clean --if-exists)
fi

# --no-owner/--no-privileges: el destino puede tener otro usuario dueño. --exit-on-error y
# --single-transaction: o queda todo o nada.
age --decrypt --identity "$identidad" "$archivo" \
    | pg_restore --no-owner --no-privileges --exit-on-error --single-transaction ${opciones[@]+"${opciones[@]}"} --dbname "$destino"
echo "Restaurado $archivo en $(psql "$destino" -XAtqc 'select current_database()')."
