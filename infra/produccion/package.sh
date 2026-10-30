#!/usr/bin/env bash
# Builds the Iris install package of a version: what install.sh and `irisctl upgrade` download from
# the GitHub Release (release.yml attaches it).
#
#   infra/produccion/package.sh <version> <output folder>
#
# Output:
#   iris-<version>.tar.gz          folder iris-<version>/ with docker-compose.yml, env.example,
#                                  install.sh, README.md, VERSION and backup/ (respaldar.sh,
#                                  restaurar.sh)
#   iris-<version>.tar.gz.sha256
#   install.sh and install.sh.sha256   the installer on its own, for the one-liner
#
# The tarball is reproducible (fixed order, dates and owners): the same commit gives the same sum.
set -euo pipefail

[ $# -eq 2 ] || { sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'; exit 2; }
version="${1#v}"
salida="$2"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "invalid version (MAJOR.MINOR.PATCH): $version" >&2; exit 2; }

aqui="$(cd "$(dirname "$0")" && pwd)"
raiz="$(cd "$aqui/../.." && pwd)"
mkdir -p "$salida"
salida="$(cd "$salida" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

d="$tmp/iris-$version"
mkdir -p "$d/backup"
install -m 0644 "$aqui/docker-compose.yml" "$d/docker-compose.yml"
install -m 0644 "$aqui/.env.example" "$d/env.example"
install -m 0644 "$aqui/README.md" "$d/README.md"
install -m 0755 "$aqui/install.sh" "$d/install.sh"
install -m 0755 "$raiz/infra/respaldo/respaldar.sh" "$raiz/infra/respaldo/restaurar.sh" "$d/backup/"
printf '%s\n' "$version" > "$d/VERSION"

paquete="iris-$version.tar.gz"
tar --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner -C "$tmp" -cf - "iris-$version" |
    gzip -n -9 > "$salida/$paquete"
install -m 0755 "$aqui/install.sh" "$salida/install.sh"
(cd "$salida" && sha256sum "$paquete" > "$paquete.sha256" && sha256sum install.sh > install.sh.sha256)
echo "ready: $salida/$paquete ($(cut -d' ' -f1 "$salida/$paquete.sha256"))"
