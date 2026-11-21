#!/usr/bin/env bash
# Smoke test of the Iris phone's TURN relay (service turn, profile telefono) with the production
# compose: it starts with the secret from the Docker secret and stays up, the secret is not in its
# environment, and the entrypoint refuses a port below 1024 and a short secret. Needs Docker and
# the host network (CI, job Infra). Contract: .claude/integration/contratos/voz.md §3.12 (E4c).
set -euo pipefail

aqui="$(cd "$(dirname "$0")" && pwd)"
tmp="$(mktemp -d)"
proyecto="iris-prueba-turn-$$"
secreto="$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')"

dc() { docker compose -p "$proyecto" --env-file "$tmp/.env" -f "$aqui/docker-compose.yml" --profile telefono "$@"; }
limpiar() {
    dc down -v --remove-orphans > /dev/null 2>&1 || true
    rm -rf -- "$tmp"
}
trap limpiar EXIT
falla() {
    echo "probar-turn: $*" >&2
    dc logs turn >&2 || true
    exit 1
}

# Uncommon ports so it does not collide with anything on the runner.
entorno() { # <puerto> <secreto>
    sed -e "s|^IRIS_DIR=.*|IRIS_DIR=$tmp|" -e 's|^IRIS_VERSION=.*|IRIS_VERSION=0.0.0|' \
        -e 's|^IRIS_BASE_URL=.*|IRIS_BASE_URL=https://iris.example.com|' \
        -e 's|^IRIS_PG_CONTRASENA=.*|IRIS_PG_CONTRASENA=probar|' \
        -e 's|^IRIS_S3_SECRET_KEY=.*|IRIS_S3_SECRET_KEY=probar-s3|' \
        -e 's|^IRIS_TELEFONO=.*|IRIS_TELEFONO=true|' \
        -e 's|^IRIS_TURN_RELAY_IP=.*|IRIS_TURN_RELAY_IP=127.0.0.1|' \
        -e 's|^IRIS_TURN_ISSABEL_IP=.*|IRIS_TURN_ISSABEL_IP=127.0.0.1|' \
        -e "s|^IRIS_TURN_PUERTO=.*|IRIS_TURN_PUERTO=$1|" \
        -e 's|^IRIS_TURN_PUERTO_MIN=.*|IRIS_TURN_PUERTO_MIN=49600|' \
        -e 's|^IRIS_TURN_PUERTO_MAX=.*|IRIS_TURN_PUERTO_MAX=49610|' "$aqui/.env.example" > "$tmp/.env"
    mkdir -p "$tmp/secrets"
    # In production install.sh writes it root:65534 0640; here the runner owns it, so 0644.
    printf '%s' "$2" > "$tmp/secrets/turn-secreto"
    chmod 644 "$tmp/secrets/turn-secreto"
}

# 1. Starts and stays up.
entorno 34781 "$secreto"
dc up -d turn
sleep 8
id="$(dc ps -q turn)"
[ -n "$id" ] || falla "the turn container does not exist"
[ "$(docker inspect -f '{{.State.Running}} {{.RestartCount}}' "$id")" = "true 0" ] || falla "turn did not stay up"
# PID 1 is turnserver itself (exec), as 65534 and without any capability.
estado="$(docker exec "$id" cat /proc/1/status)" || falla "cannot read the process status"
printf '%s\n' "$estado" | grep -qE '^Name:[[:space:]]+turnserver$' || falla "PID 1 is not turnserver: $estado"
printf '%s\n' "$estado" | grep -qE '^Uid:[[:space:]]+65534[[:space:]]+65534[[:space:]]+65534[[:space:]]+65534$' || falla "turnserver is not uid 65534: $estado"
printf '%s\n' "$estado" | grep -qE '^CapEff:[[:space:]]+0+$' || falla "turnserver has capabilities: $estado"
if docker inspect -f '{{json .Config.Env}} {{json .Config.Cmd}} {{json .Config.Entrypoint}}' "$id" | grep -qF -- "$secreto"; then
    falla "the secret is in the container's environment or command"
fi
if dc logs turn 2>&1 | grep -qF -- "$secreto"; then
    falla "the secret is in the logs"
fi
dc down -v > /dev/null 2>&1
echo "turn: starts and stays up, secret only in the Docker secret"

# 2. The entrypoint refuses a privileged port and a short secret (exit 1, never starts coturn).
rechaza() { # <descripción> <puerto> <secreto> <texto esperado>
    entorno "$2" "$3"
    local out
    if out="$(dc run --rm --no-deps turn 2>&1)"; then
        falla "$1: it started"
    fi
    printf '%s' "$out" | grep -qF -- "$4" || falla "$1: unexpected output: $out"
    echo "turn: refuses $1"
}
rechaza "port 80" 80 "$secreto" "must be between 1024 and 65535"
rechaza "a short secret" 34781 "corto" "16+ chars"
rechaza "a secret with spaces" 34781 "dos palabras con espacios largas" "spaces or line breaks"
