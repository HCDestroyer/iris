#!/usr/bin/env bash
# Ensayo de respaldo y restauración (CI y a mano): respalda la base de origen con una clave age
# efímera, la restaura en una base nueva y compara la cantidad exacta de filas de cada tabla
# (incluida la de migraciones de goose). Falla si algo difiere o si el origen está vacío.
#
#   infra/respaldo/ensayo.sh <url de origen> <nombre de la base de destino>
#
# La base de destino se borra y se crea en el mismo servidor que el origen (el usuario necesita
# CREATEDB). Requiere age, age-keygen, psql, pg_dump y pg_restore.
set -euo pipefail

[ $# -eq 2 ] || { echo "uso: $0 <postgres://… de origen> <base de destino>" >&2; exit 2; }
origen="$1"
base_destino="$2"
case "$base_destino" in *[!a-z0-9_]*) echo "Nombre de base inválido: $base_destino" >&2; exit 2 ;; esac

aqui="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# URL de destino: la del origen con otro nombre de base (conservando ?parámetros).
destino="$(printf '%s' "$origen" | sed -E "s#^(postgres(ql)?://[^/]+/)[^?]*#\1$base_destino#")"
psql "$origen" -Xqc "drop database if exists $base_destino" -c "create database $base_destino"

age-keygen -o "$tmp/identidad.txt" 2>/dev/null
age-keygen -y "$tmp/identidad.txt" >"$tmp/destinatarios.txt"

export IRIS_DB_URL="$origen" IRIS_RESPALDO_DESTINATARIOS="$tmp/destinatarios.txt" \
    IRIS_RESPALDO_DIR="$tmp/salida" IRIS_RESPALDO_DIAS=0 IRIS_RESPALDO_IDENTIDAD="$tmp/identidad.txt"
"$aqui/respaldar.sh"
respaldo="$(ls "$tmp"/salida/*.dump.age)"

# El respaldo no se puede leer sin la clave privada (ni es un dump en claro).
if pg_restore --list "$respaldo" >/dev/null 2>&1; then
    echo "El respaldo NO está cifrado." >&2
    exit 1
fi

"$aqui/restaurar.sh" "$respaldo" "$destino"

# Conteo exacto por tabla (no las estimaciones de pg_stat): una consulta por tabla.
conteos() {
    local url="$1"
    psql "$url" -XAtq -F ' ' <<'SQL'
select format('select %L, count(*) from %I.%I;', schemaname || '.' || tablename, schemaname, tablename)
from pg_tables
where schemaname not in ('pg_catalog', 'information_schema')
order by schemaname, tablename
\gexec
SQL
}
conteos "$origen" >"$tmp/origen.txt"
conteos "$destino" >"$tmp/destino.txt"

tablas="$(wc -l <"$tmp/origen.txt")"
filas="$(awk '{s += $2} END {print s + 0}' "$tmp/origen.txt")"
if [ "$tablas" -eq 0 ] || [ "$filas" -eq 0 ]; then
    echo "El origen no tiene tablas o filas: el ensayo no prueba nada." >&2
    exit 1
fi
if ! diff -u "$tmp/origen.txt" "$tmp/destino.txt"; then
    echo "Los conteos difieren entre origen y restauración." >&2
    exit 1
fi
echo "Ensayo correcto: $tablas tablas y $filas filas iguales en origen y restauración ($base_destino)."
