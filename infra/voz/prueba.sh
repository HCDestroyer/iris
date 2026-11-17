#!/usr/bin/env bash
# End-to-end test of the Iris Voz package with Docker Compose and no GPU (CI, voz.yml). Run as root
# from the repository root, with Go in PATH:
#
#   infra/voz/prueba.sh falso   Ollama falso (cmd/voz-ollama-falso): instalar, enlazar, chat y desenlazar
#   infra/voz/prueba.sh real    real Ollama on CPU with a tiny model: also the model download
#                               through the registry-only proxy and its digest check
#
# It builds the images locally and installs the package through the functions of install-voz.sh
# (sourced), skipping only the download: the signature check has its own test (prueba-firma.sh).
set -euo pipefail

modo="${1:-falso}"
aqui="$(cd "$(dirname "$0")" && pwd)"
raiz="$(cd "$aqui/../.." && pwd)"
[ "$(id -u)" = 0 ] || { echo "run as root" >&2; exit 1; }

# shellcheck source=infra/voz/install-voz.sh
. "$aqui/install-voz.sh"

VERSION="$(manifest_get "$aqui/manifiesto.json" version)"
DIR="$(mktemp -d)"
PUERTO=18443
export IRIS_VOZ_SKIP_PULL=1
MODELO=modelo-a:latest
if [ "$modo" = falso ]; then
    export IRIS_VOZ_COMPOSE_EXTRA="$aqui/docker-compose.prueba.yml"
else
    # Tiny model for CPU; digest of its manifest on registry.ollama.ai.
    MODELO=smollm2:135m
    MODELO_DIGEST=sha256:9077fe9d2ae1a4a41a868836b56b8163731a8fe16621397028c2c76f838c6907
fi

limpiar() {
    estado=$?
    if [ "$estado" != 0 ]; then
        dc --profile descarga ps -a || true
        dc --profile descarga logs --tail 80 || true
    fi
    dc --profile descarga down -v --remove-orphans > /dev/null 2>&1 || true
    rm -rf "$DIR"
    exit "$estado"
}
trap limpiar EXIT

log "Building the images"
docker build -q -f "$aqui/Dockerfile" --build-arg VERSION="$VERSION" -t "ghcr.io/hcdestroyer/iris-voz:$VERSION" "$raiz" > /dev/null
[ "$modo" != falso ] || docker build -q -f "$aqui/Dockerfile" --target ollama-falso -t iris-voz-ollama-falso:prueba "$raiz" > /dev/null

log "Installing the package into $DIR"
"$aqui/package-voz.sh" "$VERSION" "$DIR/dist" > /dev/null
mkdir -p "$DIR/releases"
tar -xzf "$DIR/dist/iris-voz-$VERSION.tar.gz" -C "$DIR/releases"
mv "$DIR/releases/iris-voz-$VERSION" "$DIR/releases/$VERSION"
env_set IRIS_VOZ_PUERTO "$PUERTO"
env_set IRIS_VOZ_ESCUCHA 127.0.0.1
env_set IRIS_VOZ_GPU 0
BIN="$DIR/bin/iris-voz"
mkdir -p "$DIR/bin"
activate_release "$VERSION"
cmp -s "$BIN" "$DIR/current/install-voz.sh" || die "install did not leave the iris-voz wrapper"
# An upgrade activates a release again: the wrapper of the old version must be replaced.
printf '#!/bin/sh\necho old wrapper\n' > "$BIN"
activate_release "$VERSION"
cmp -s "$BIN" "$DIR/current/install-voz.sh" || die "upgrade did not replace the iris-voz wrapper"
start

log "Hardening of the running containers"
[ "$(docker inspect -f '{{.HostConfig.ReadonlyRootfs}}' iris-voz-puerta-1)" = true ] || die "puerta: root filesystem is writable"
[ "$(docker inspect -f '{{.Config.User}}' iris-voz-puerta-1)" = 65532:65532 ] || die "puerta: not the unprivileged user"
[ "$(docker inspect -f '{{.HostConfig.ReadonlyRootfs}}' iris-voz-llm-1)" = true ] || die "llm: root filesystem is writable"
[ -z "$(docker port iris-voz-llm-1)" ] || die "llm publishes a port"
[ "$(docker network inspect -f '{{.Internal}}' iris-voz_interna)" = true ] || die "the internal network has a way out"
publicados="$(docker ps --filter label=com.docker.compose.project=iris-voz --format '{{.Ports}}' | grep -o '[0-9.]*:[0-9]*->' | sort -u)"
[ "$publicados" = "127.0.0.1:$PUERTO->" ] || die "published ports: $publicados"
# Ollama is not reachable from the host: only the control port is.
if curl -fsS -m 3 "http://127.0.0.1:11434/api/tags" > /dev/null 2>&1; then die "Ollama is exposed on the host"; fi

if [ "$modo" = real ]; then
    log "A model with the wrong digest is removed"
    if (model_pull "$MODELO" "sha256:$(printf '0%.0s' $(seq 1 64))") 2> "$DIR/err"; then die "a wrong digest was accepted"; fi
    grep -q "no coincide con el digest" "$DIR/err" || { cat "$DIR/err" >&2; die "unexpected failure of the digest check"; }
    if puerta model digest "$MODELO" > /dev/null 2>&1 < /dev/null; then die "the model with the wrong digest was kept"; fi
    model_pull "$MODELO" "$MODELO_DIGEST"
    log "The download container reaches nothing but the model registry"
    respuesta="$(dc --profile descarga run --rm --entrypoint /bin/bash descarga -c \
        'exec 3<>/dev/tcp/proxy-descarga/3128 && printf "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n" >&3 && head -n1 <&3' \
        < /dev/null 2> /dev/null || true)"
    case "$respuesta" in *" 403 "*) ;; *) die "the download proxy did not refuse example.com: '$respuesta'" ;; esac
    # ...and has no direct route to the internet.
    if dc --profile descarga run --rm --entrypoint /bin/bash descarga -c 'exec 3<>/dev/tcp/1.1.1.1/443' < /dev/null 2> /dev/null; then
        die "the download container has a direct route to the internet"
    fi
    dc --profile descarga rm -sf proxy-descarga > /dev/null 2>&1 || true
fi
puerta model allow "$MODELO" < /dev/null

log "Pairing, chat and unpairing through the real control port"
real=0
[ "$modo" != real ] || real=1
(cd "$raiz/apps/api" && IRIS_VOZ_URL="https://127.0.0.1:$PUERTO" IRIS_VOZ_CLI="$aqui/install-voz.sh" IRIS_VOZ_DIR="$DIR" \
    IRIS_VOZ_MODELO="$MODELO" IRIS_VOZ_REAL="$real" go test -tags vozcompose -count=1 -run '^TestCompose$' ./internal/voz/)

log "The logs of the puerta carry no prompt"
if dc logs puerta 2> /dev/null | grep -q "Di hola"; then die "a prompt reached the logs of the puerta"; fi
log "ok ($modo)"
