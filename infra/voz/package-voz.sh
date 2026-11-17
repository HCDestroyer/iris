#!/usr/bin/env bash
# Builds the Iris Voz install package of a version: what install-voz.sh and `iris-voz upgrade`
# download from the GitHub Release voz-v<version> (voz-release.yml signs and attaches it).
#
#   infra/voz/package-voz.sh <version> <output folder>
#
# Output:
#   iris-voz-<version>.tar.gz   folder iris-voz-<version>/ with docker-compose.yml,
#                               docker-compose.gpu.yml, docker-compose.descarga.yml, install-voz.sh,
#                               manifiesto.json, README.md and VERSION
#   install-voz.sh              the installer on its own
#   manifiesto.json             the manifest on its own
#   *.sha256                    sums (the signature, not the sum, is what the installer trusts)
#
# <version> must be the one in manifiesto.json. With IRIS_PIN_DIGESTS=1 (voz-release.yml) the
# iris-voz image is written as tag@sha256:digest (IRIS_VOZ_DIGEST, or resolved on IRIS_REGISTRY);
# without it the compose keeps ${IRIS_VOZ_VERSION} (local tests). The tarball is reproducible.
set -euo pipefail

[ $# -eq 2 ] || { sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'; exit 2; }
version="${1#v}"
salida="$2"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "invalid version (MAJOR.MINOR.PATCH): $version" >&2; exit 2; }

aqui="$(cd "$(dirname "$0")" && pwd)"
en_manifiesto="$(sed -n 's/^ *"version": *"\([^"]*\)".*/\1/p' "$aqui/manifiesto.json" | head -n1)"
[ "$en_manifiesto" = "$version" ] || { echo "manifiesto.json says version $en_manifiesto, not $version" >&2; exit 1; }

mkdir -p "$salida"
salida="$(cd "$salida" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

d="$tmp/iris-voz-$version"
mkdir -p "$d"
for f in docker-compose.yml docker-compose.gpu.yml docker-compose.descarga.yml manifiesto.json README.md; do
    install -m 0644 "$aqui/$f" "$d/$f"
done
install -m 0755 "$aqui/install-voz.sh" "$d/install-voz.sh"
printf '%s\n' "$version" > "$d/VERSION"

if [ "${IRIS_PIN_DIGESTS:-0}" = 1 ]; then
    registro="${IRIS_REGISTRY:-ghcr.io/hcdestroyer}"
    digest="${IRIS_VOZ_DIGEST:-}"
    if [ -z "$digest" ]; then
        digest="$(docker buildx imagetools inspect "$registro/iris-voz:$version" --format '{{json .Manifest}}' | jq -r .digest)"
    fi
    [[ "$digest" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo "could not resolve $registro/iris-voz:$version (got '$digest')" >&2; exit 1; }
    for f in docker-compose.yml docker-compose.gpu.yml docker-compose.descarga.yml; do
        sed -i "s|/iris-voz:\${IRIS_VOZ_VERSION:?IRIS_VOZ_VERSION is required}|/iris-voz:$version@$digest|" "$d/$f"
    done
    echo "iris-voz:$version@$digest"
    if grep -qF ":\${IRIS_VOZ_VERSION" "$d"/docker-compose*.yml; then
        echo "an iris-voz image was left unpinned" >&2
        exit 1
    fi
fi
# Every image of the package is pinned by digest (the iris-voz one, with IRIS_PIN_DIGESTS).
if grep -hE '^\s*image:' "$d"/docker-compose*.yml | grep -v 'IRIS_VOZ_VERSION' | grep -qv '@sha256:'; then
    echo "an image is not pinned by digest" >&2
    exit 1
fi

paquete="iris-voz-$version.tar.gz"
tar --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner -C "$tmp" -cf - "iris-voz-$version" |
    gzip -n -9 > "$salida/$paquete"
install -m 0755 "$aqui/install-voz.sh" "$salida/install-voz.sh"
install -m 0644 "$aqui/manifiesto.json" "$salida/manifiesto.json"
(cd "$salida" && sha256sum "$paquete" > "$paquete.sha256" && sha256sum install-voz.sh > install-voz.sh.sha256)
echo "ready: $salida/$paquete ($(cut -d' ' -f1 "$salida/$paquete.sha256"))"
