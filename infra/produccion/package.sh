#!/usr/bin/env bash
# Builds the Iris install package of a version: what install.sh and `irisctl upgrade` download from
# the GitHub Release (release.yml attaches it).
#
#   infra/produccion/package.sh <version> <output folder>
#
# Output:
#   iris-<version>.tar.gz          folder iris-<version>/ with docker-compose.yml,
#                                  docker-compose.admin-ports.yml, env.example, install.sh,
#                                  README.md, VERSION, MIGRATION (newest goose migration of the
#                                  version) and backup/ (respaldar.sh, restaurar.sh)
#   iris-<version>.tar.gz.sha256
#   install.sh and install.sh.sha256   the installer on its own, for the one-liner
#
# MIGRATION comes from the tag v<version> when it exists (else from the working tree, with a
# warning). With IRIS_PIN_DIGESTS=1 (release.yml) the iris-api/worker/web images of <version> are
# resolved on the registry (IRIS_REGISTRY, ghcr.io/hcdestroyer) and written as tag@sha256:digest;
# without it they stay ${IRIS_VERSION} (local tests). The tarball is reproducible (fixed order,
# dates and owners): the same commit and digests give the same sum.
set -euo pipefail

[ $# -eq 2 ] || { sed -n '2,21p' "$0" | sed 's/^# \{0,1\}//'; exit 2; }
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
install -m 0644 "$aqui/docker-compose.admin-ports.yml" "$d/docker-compose.admin-ports.yml"
install -m 0644 "$aqui/.env.example" "$d/env.example"
install -m 0644 "$aqui/README.md" "$d/README.md"
install -m 0755 "$aqui/install.sh" "$d/install.sh"
install -m 0755 "$raiz/infra/respaldo/respaldar.sh" "$raiz/infra/respaldo/restaurar.sh" "$d/backup/"
printf '%s\n' "$version" > "$d/VERSION"

# Newest goose migration of the version (install.sh refuses a database that is ahead of it).
mig_dir=apps/api/migraciones
if git -C "$raiz" rev-parse -q --verify "refs/tags/v$version" > /dev/null 2>&1; then
    migracion="$(git -C "$raiz" ls-tree --name-only "v$version" -- "$mig_dir/" | sed -n 's|.*/\([0-9]\{1,\}\)_.*\.sql$|\1|p' | sort -n | tail -n1)"
else
    echo "warning: no tag v$version: MIGRATION taken from the working tree" >&2
    migracion="$(find "$raiz/$mig_dir" -maxdepth 1 -name '*.sql' -printf '%f\n' | sed -n 's|^\([0-9]\{1,\}\)_.*|\1|p' | sort -n | tail -n1)"
fi
[[ "$migracion" =~ ^[0-9]+$ ]] || { echo "could not find the migrations of $version" >&2; exit 1; }
printf '%s\n' "$migracion" > "$d/MIGRATION"

if [ "${IRIS_PIN_DIGESTS:-0}" = 1 ]; then
    registro="${IRIS_REGISTRY:-ghcr.io/hcdestroyer}"
    for img in iris-api iris-worker iris-web; do
        digest="$(docker buildx imagetools inspect "$registro/$img:$version" --format '{{json .Manifest}}' | jq -r .digest)"
        [[ "$digest" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo "could not resolve $registro/$img:$version (got '$digest')" >&2; exit 1; }
        sed -i "s|/$img:\${IRIS_VERSION:?IRIS_VERSION is required}|/$img:$version@$digest|" "$d/docker-compose.yml"
        echo "$img:$version@$digest"
    done
    if grep -qF ":\${IRIS_VERSION" "$d/docker-compose.yml"; then
        echo "an iris-* image was left unpinned" >&2
        exit 1
    fi
fi

paquete="iris-$version.tar.gz"
tar --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner -C "$tmp" -cf - "iris-$version" |
    gzip -n -9 > "$salida/$paquete"
install -m 0755 "$aqui/install.sh" "$salida/install.sh"
(cd "$salida" && sha256sum "$paquete" > "$paquete.sha256" && sha256sum install.sh > install.sh.sha256)
echo "ready: $salida/$paquete ($(cut -d' ' -f1 "$salida/$paquete.sha256"))"
