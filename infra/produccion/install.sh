#!/usr/bin/env bash
# Iris production installer and manager (docs/runbooks/install.md).
#
# Install (Ubuntu/Debian with Docker), from the latest GitHub Release:
#
#   curl -fsSLO https://github.com/HCDestroyer/iris/releases/latest/download/install.sh
#   curl -fsSLO https://github.com/HCDestroyer/iris/releases/latest/download/install.sh.sha256
#   sha256sum -c install.sh.sha256 && sudo bash install.sh
#
# It never builds anything: it downloads the release package (production docker-compose.yml and
# scripts), verifies its sha256, writes <dir>/.env (mode 600) with generated secrets, pulls the
# images from ghcr.io, runs the migrations, starts Iris and waits until it is healthy. Running it
# again is safe: existing settings and secrets are kept. After the install, the same script is
# available as `irisctl`.
#
# Usage: install.sh [command] [options]
#
# Commands:
#   install          install or reconfigure (default)
#   upgrade          back up the database, then move to a newer version (--version X.Y.Z or latest)
#   status           installed and latest version, containers and health
#   logs [service]   follow the logs (api, worker, web, postgres, minio...)
#   stop             stop every container (data is kept)
#   start            start everything again and wait until it is healthy
#   backup           encrypted database backup now (infra/respaldo/respaldar.sh)
#   create-admin     create an administrator; the password is printed once
#   compose ARGS     run docker compose on the installation (e.g. irisctl compose ps)
#   version          print the installed version
#
# Install options (anything missing is asked for when there is a terminal):
#   --url URL              public address of the panel, e.g. https://iris.example.com (IRIS_BASE_URL)
#   --argos-origin URL     ArgosISP origin(s) allowed to connect, comma separated (IRIS_ARGOS_ORIGENES)
#   --horus-origin URL     Horus origin(s) allowed to connect, comma separated (IRIS_HORUS_ORIGENES)
#   --web-port N           panel port on 127.0.0.1, the reverse proxy target (IRIS_WEB_PUERTO; 3000)
#   --api-port N           API port on 127.0.0.1, for local checks (IRIS_API_PUERTO; 8080)
#   --version X.Y.Z        version to install (default: latest release)
#   --dir PATH             install folder (default /opt/iris)
#   --from-lab ENV_FILE    migrate a `make dev-app` lab install: reuse its .env, database and
#                          attachments (auto-detected when the lab containers exist)
#   --keep-db-password     with --from-lab: keep the lab database password (default: replace the
#                          dev default "iris" with a generated one)
#   --with-osrm            also run the OSRM routing service (needs infra/osrm/preparar.sh data)
#   --admin-email EMAIL    create the first administrator with this email
#   --admin-name NAME      name of that administrator (default "Administrator")
#   --no-admin             do not offer to create an administrator
#   -y, --yes              never ask; fail if something required is missing
#
# Upgrade options: --version X.Y.Z, --skip-backup (not recommended), -y/--yes.
#
# Environment (tests and offline servers): IRIS_DIR, IRIS_REPO (HCDestroyer/iris), IRIS_SOURCE
# (local folder with iris-<v>.tar.gz, its .sha256 and a `latest` file, instead of GitHub),
# IRIS_SKIP_PULL=1 (images already loaded), IRIS_WAIT_S (seconds to wait for health, 300).
set -Eeuo pipefail

# Everything runs inside main(), called on the last line, so bash reads the whole file before running
# anything (safe with `curl ... | bash`). Commands that could read stdin get </dev/null; questions
# read /dev/tty.
main() {

REPO="${IRIS_REPO:-HCDestroyer/iris}"
SOURCE="${IRIS_SOURCE:-}"
DIR="${IRIS_DIR:-/opt/iris}"
WAIT_S="${IRIS_WAIT_S:-300}"
COMPOSE_MIN=2.24
KEEP_RELEASES=3

log() { printf '\033[1m==>\033[0m %s\n' "$*" >&2; }
warn() { printf '\033[33mwarning:\033[0m %s\n' "$*" >&2; }
die() { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }
usage() { sed -n '2,/^set -E/{/^#/p}' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit "${1:-0}"; }

# --- arguments -------------------------------------------------------------------------------
CMD=install
case "${1:-}" in
    install | upgrade | status | logs | stop | start | backup | create-admin | compose | version) CMD="$1"; shift ;;
    help | -h | --help) usage 0 ;;
    '' | -*) ;;
    *) die "unknown command: $1 (see --help)" ;;
esac

URL="" ARGOS="" HORUS="" WEB_PORT="" API_PORT="" VERSION="" FROM_LAB="" ADMIN_EMAIL="" ADMIN_NAME=""
SET_ARGOS=false SET_HORUS=false YES=false NO_ADMIN=false WITH_OSRM=false SKIP_BACKUP=false
KEEP_DB_PASSWORD=false
COMPOSE_ARGS=()
if [ "$CMD" = compose ] || [ "$CMD" = logs ]; then
    COMPOSE_ARGS=("$@")
    set --
fi
need_value() { [ $# -ge 2 ] && [ -n "$2" ] || die "$1 needs a value"; }
while [ $# -gt 0 ]; do
    case "$1" in
        --url) need_value "$@"; URL="$2"; shift 2 ;;
        --argos-origin) need_value "$@"; ARGOS="$2"; SET_ARGOS=true; shift 2 ;;
        --horus-origin) need_value "$@"; HORUS="$2"; SET_HORUS=true; shift 2 ;;
        --web-port) need_value "$@"; WEB_PORT="$2"; shift 2 ;;
        --api-port) need_value "$@"; API_PORT="$2"; shift 2 ;;
        --version) need_value "$@"; VERSION="${2#v}"; shift 2 ;;
        --dir) need_value "$@"; DIR="$2"; shift 2 ;;
        --from-lab) need_value "$@"; FROM_LAB="$2"; shift 2 ;;
        --keep-db-password) KEEP_DB_PASSWORD=true; shift ;;
        --with-osrm) WITH_OSRM=true; shift ;;
        --admin-email | --email) need_value "$@"; ADMIN_EMAIL="$2"; shift 2 ;;
        --admin-name | --name) need_value "$@"; ADMIN_NAME="$2"; shift 2 ;;
        --no-admin) NO_ADMIN=true; shift ;;
        --skip-backup) SKIP_BACKUP=true; shift ;;
        -y | --yes) YES=true; shift ;;
        -h | --help) usage 0 ;;
        *) die "unknown option: $1 (see --help)" ;;
    esac
done
case "$DIR" in /*) ;; *) DIR="$(pwd)/$DIR" ;; esac
DIR="${DIR%/}"
ENV_FILE="$DIR/.env"

# --- helpers -----------------------------------------------------------------------------------
has_tty() { ! $YES && { : < /dev/tty; } 2> /dev/null; }

ask() { # ask "question" [default] → answer on stdout
    local q="$1" def="${2:-}" a=""
    has_tty || { printf '%s' "$def"; return 0; }
    if [ -n "$def" ]; then printf '%s [%s]: ' "$q" "$def" > /dev/tty; else printf '%s: ' "$q" > /dev/tty; fi
    read -r a < /dev/tty || true
    printf '%s' "${a:-$def}"
}

confirm() { # confirm "question" default(y|n)
    local def="${2:-y}" a
    if ! has_tty; then [ "$def" = y ]; return; fi
    a="$(ask "$1 [$([ "$def" = y ] && echo Y/n || echo y/N)]" "")"
    case "${a,,}" in y | yes) return 0 ;; n | no) return 1 ;; *) [ "$def" = y ] ;; esac
}

rand_alnum() { # letters and digits only (safe in URLs, SQL and .env files)
    local n="${1:-32}" s=""
    while [ "${#s}" -lt "$n" ]; do
        s="$s$(head -c 64 /dev/urandom | base64 | tr -dc 'A-Za-z0-9')"
    done
    printf '%s' "${s:0:$n}"
}

version_ge() { [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" = "$2" ]; }
is_semver() { [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; }

# Value of KEY in an env file (last occurrence, surrounding quotes removed); empty if missing.
env_get() {
    local key="$1" file="${2:-$ENV_FILE}" line
    [ -f "$file" ] || return 0
    line="$(grep -E "^${key}=" "$file" | tail -n 1 || true)"
    line="${line#*=}"
    line="${line%\"}"; line="${line#\"}"
    line="${line%\'}"; line="${line#\'}"
    printf '%s' "$line"
}
env_has() { grep -qE "^$1=" "${2:-$ENV_FILE}" 2> /dev/null; }

# Sets KEY=VALUE in the .env, in place (keeps order and comments), appending it if missing.
env_set() {
    local key="$1" value="$2" tmp
    tmp="$(mktemp "$DIR/.env.XXXXXX")"
    chmod 600 "$tmp"
    if env_has "$key"; then
        K="$key" V="$value" awk 'BEGIN { k = ENVIRON["K"]; v = ENVIRON["V"] }
            index($0, k "=") == 1 { if (!done) { print k "=" v; done = 1 } ; next } { print }' "$ENV_FILE" > "$tmp"
    else
        cat "$ENV_FILE" > "$tmp"
        printf '%s=%s\n' "$key" "$value" >> "$tmp"
    fi
    mv -f "$tmp" "$ENV_FILE"
}

# Appends the keys of a newer template that the .env does not have yet (with the template value).
env_add_missing() {
    local template="$1" key added=0
    [ -f "$template" ] || return 0
    while IFS= read -r key; do
        env_has "$key" && continue
        [ "$added" = 1 ] || printf '\n# Added by install.sh %s\n' "$(date -u +%Y-%m-%d)" >> "$ENV_FILE"
        added=1
        printf '%s=%s\n' "$key" "$(env_get "$key" "$template")" >> "$ENV_FILE"
    done < <(grep -oE '^[A-Z][A-Z0-9_]*=' "$template" | tr -d '=')
}

compose() {
    local profiles=()
    [ "$(env_get IRIS_OSRM)" = true ] && profiles=(--profile osrm)
    docker compose --env-file "$ENV_FILE" -f "$DIR/current/docker-compose.yml" "${profiles[@]}" "$@"
}

require_install() {
    [ -f "$ENV_FILE" ] || die "Iris is not installed in $DIR (no .env). Install with: sudo bash install.sh"
    [ -f "$DIR/current/docker-compose.yml" ] || die "$DIR/current/docker-compose.yml is missing (broken install?)"
    check_docker
}

check_docker() {
    command -v docker > /dev/null || die "Docker is not installed. Install Docker Engine and the compose plugin first: https://docs.docker.com/engine/install/ubuntu/"
    docker info > /dev/null 2>&1 || die "Docker is installed but not answering (is it running? are you root or in the docker group?)"
    local v
    v="$(docker compose version --short 2> /dev/null || true)"
    [ -n "$v" ] || die "Docker Compose v2 is missing (apt-get install docker-compose-plugin)"
    version_ge "${v#v}" "$COMPOSE_MIN" || die "Docker Compose $COMPOSE_MIN or newer is required (found $v)"
}

lock() {
    mkdir -p "$DIR"
    command -v flock > /dev/null || return 0
    exec 9> "$DIR/.lock"
    flock -n 9 || die "another install.sh/irisctl operation is running in $DIR"
}

valid_url() { [[ "$1" =~ ^https?://[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?(:[0-9]{1,5})?$ ]]; }
valid_origins() {
    local o
    [ -n "$1" ] || return 0
    for o in ${1//,/ }; do valid_url "$o" || return 1; done
}
valid_port() { [[ "$1" =~ ^[0-9]{1,5}$ ]] && [ "$1" -ge 1 ] && [ "$1" -le 65535 ]; }

# Ports published by containers of the iris compose project (they belong to us: not a conflict).
iris_ports() {
    docker ps --filter label=com.docker.compose.project=iris --format '{{.Ports}}' 2> /dev/null |
        tr ',' '\n' | sed -n 's/.*:\([0-9][0-9]*\)->.*/\1/p' | sort -u
}

check_ports() {
    command -v ss > /dev/null || { warn "ss not found: skipping the port check"; return 0; }
    local ours var p
    ours=" $(iris_ports | tr '\n' ' ') "
    for var in IRIS_WEB_PUERTO IRIS_API_PUERTO IRIS_PG_PUERTO IRIS_MINIO_PUERTO IRIS_MINIO_CONSOLA_PUERTO; do
        p="$(env_get "$var")"
        [ -n "$p" ] || continue
        case "$ours" in *" $p "*) continue ;; esac
        if ss -Hltn "sport = :$p" 2> /dev/null | grep -q .; then
            die "port $p ($var) is already in use on this server. Pick another one: edit $ENV_FILE (or use --web-port/--api-port) and run install.sh again"
        fi
    done
}

# --- release package -----------------------------------------------------------------------------
latest_version() {
    local v
    if [ -n "$SOURCE" ]; then
        v="$(tr -d '[:space:]' < "$SOURCE/latest" 2> /dev/null || true)"
    else
        v="$(curl -fsSL --retry 3 -H 'Accept: application/vnd.github+json' \
            "https://api.github.com/repos/$REPO/releases/latest" < /dev/null |
            sed -n 's/^ *"tag_name": *"v\{0,1\}\([^"]*\)".*/\1/p' | head -n1 || true)"
    fi
    printf '%s' "$v"
}

# Downloads, verifies and unpacks iris-<v>.tar.gz into $DIR/releases/<v> (if not already there).
fetch_release() {
    local v="$1" tmp pkg
    if [ -f "$DIR/releases/$v/docker-compose.yml" ] && [ -f "$DIR/releases/$v/.verified" ]; then
        return 0
    fi
    tmp="$(mktemp -d)"
    pkg="iris-$v.tar.gz"
    log "downloading the Iris $v package"
    if [ -n "$SOURCE" ]; then
        cp "$SOURCE/$pkg" "$SOURCE/$pkg.sha256" "$tmp/" 2> /dev/null || { rm -rf "$tmp"; die "$pkg is not in $SOURCE"; }
    else
        local base="https://github.com/$REPO/releases/download/v$v"
        curl -fsSL --retry 3 -o "$tmp/$pkg" "$base/$pkg" < /dev/null || { rm -rf "$tmp"; die "could not download $base/$pkg (does release v$v have the install package?)"; }
        curl -fsSL --retry 3 -o "$tmp/$pkg.sha256" "$base/$pkg.sha256" < /dev/null || { rm -rf "$tmp"; die "could not download $base/$pkg.sha256"; }
    fi
    (cd "$tmp" && sha256sum -c --quiet "$pkg.sha256") || { rm -rf "$tmp"; die "the sha256 of $pkg does not match: refusing to install it"; }
    tar -xzf "$tmp/$pkg" -C "$tmp"
    [ -f "$tmp/iris-$v/docker-compose.yml" ] || { rm -rf "$tmp"; die "$pkg does not contain iris-$v/docker-compose.yml"; }
    mkdir -p "$DIR/releases"
    rm -rf "${DIR:?}/releases/$v"
    mv "$tmp/iris-$v" "$DIR/releases/$v"
    : > "$DIR/releases/$v/.verified"
    rm -rf "$tmp"
}

use_release() {
    ln -sfn "releases/$1" "$DIR/current"
    if [ "$(id -u)" = 0 ] && [ -d /usr/local/bin ] && [ "$DIR" = /opt/iris ] || [ "${IRIS_LINK_CLI:-0}" = 1 ]; then
        ln -sfn "$DIR/current/install.sh" /usr/local/bin/irisctl 2> /dev/null || warn "could not create /usr/local/bin/irisctl"
    fi
}

prune_releases() {
    local cur keep=0 v
    cur="$(env_get IRIS_VERSION)"
    for v in $(find "$DIR/releases" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' 2> /dev/null | sort -rV); do
        [ "$v" = "$cur" ] && continue
        keep=$((keep + 1))
        [ "$keep" -le "$KEEP_RELEASES" ] || rm -rf "${DIR:?}/releases/$v"
    done
}

# --- backups -------------------------------------------------------------------------------------
ensure_age() {
    command -v age > /dev/null && command -v age-keygen > /dev/null && return 0
    if [ "$(id -u)" = 0 ] && command -v apt-get > /dev/null; then
        log "installing age (backup encryption)"
        DEBIAN_FRONTEND=noninteractive apt-get install -y -qq age < /dev/null > /dev/null 2>&1 ||
            { apt-get update -qq < /dev/null > /dev/null 2>&1 && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq age < /dev/null > /dev/null 2>&1; } || true
    fi
    command -v age > /dev/null && command -v age-keygen > /dev/null
}

# Creates the age key pair for backups if the .env has no recipients file. Prints nothing if it
# already exists; sets NEW_BACKUP_KEY=true when it creates one.
NEW_BACKUP_KEY=false
LAB_MOVED=false
ensure_backup_keys() {
    local r
    r="$(env_get IRIS_RESPALDO_DESTINATARIOS)"
    [ -n "$r" ] && [ -s "$r" ] && return 0
    if ! ensure_age; then
        warn "age is not installed: backups (and the backup before each upgrade) are disabled until you install it (apt-get install age) and run install.sh again"
        return 0
    fi
    mkdir -p "$DIR/backup-keys"
    chmod 700 "$DIR/backup-keys"
    if [ ! -s "$DIR/backup-keys/identity.txt" ]; then
        (umask 077 && age-keygen -o "$DIR/backup-keys/identity.txt" 2> /dev/null)
        NEW_BACKUP_KEY=true
    fi
    age-keygen -y "$DIR/backup-keys/identity.txt" > "$DIR/backup-keys/recipients.txt"
    env_set IRIS_RESPALDO_DESTINATARIOS "$DIR/backup-keys/recipients.txt"
    env_set IRIS_RESPALDO_IDENTIDAD "$DIR/backup-keys/identity.txt"
}

do_backup() {
    local r
    r="$(env_get IRIS_RESPALDO_DESTINATARIOS)"
    [ -n "$r" ] && [ -s "$r" ] || die "no backup key configured (IRIS_RESPALDO_DESTINATARIOS). Run install.sh again to create one, or use --skip-backup"
    command -v age > /dev/null || die "age is not installed (apt-get install age)"
    [ -x "$DIR/current/backup/respaldar.sh" ] || die "$DIR/current/backup/respaldar.sh is missing"
    mkdir -p "$(env_get IRIS_RESPALDO_DIR)"
    log "backing up the database"
    ENV_FILE="$ENV_FILE" IRIS_COMPOSE_ARCHIVO="$DIR/current/docker-compose.yml" \
        "$DIR/current/backup/respaldar.sh" --compose < /dev/null
}

# --- start and health ----------------------------------------------------------------------------
wait_healthy() {
    local api web end
    api="$(env_get IRIS_API_PUERTO)"; api="${api:-8080}"
    web="$(env_get IRIS_WEB_PUERTO)"; web="${web:-3000}"
    end=$((SECONDS + WAIT_S))
    log "waiting for /api/v1/salud (up to ${WAIT_S}s)"
    while [ "$SECONDS" -lt "$end" ]; do
        if curl -fsS --max-time 5 "http://127.0.0.1:$api/api/v1/salud" > /dev/null 2>&1 < /dev/null &&
            curl -fsS --max-time 5 "http://127.0.0.1:$web/api/v1/salud" > /dev/null 2>&1 < /dev/null; then
            log "Iris is healthy: $(curl -fsS --max-time 5 "http://127.0.0.1:$api/api/v1/salud" < /dev/null)"
            return 0
        fi
        sleep 3
    done
    return 1
}

start_all() {
    compose up -d --remove-orphans < /dev/null || return 1
    # The lab's osrm container is not an orphan (the service exists under a profile): remove it when
    # OSRM is off. Its data volume is kept.
    if [ "$(env_get IRIS_OSRM)" != true ]; then
        docker compose --env-file "$ENV_FILE" -f "$DIR/current/docker-compose.yml" --profile osrm \
            rm -sf osrm < /dev/null > /dev/null 2>&1 || true
    fi
    compose up -d --wait --wait-timeout "$WAIT_S" api worker web < /dev/null || return 1
    wait_healthy
}

create_admin() {
    local email="$1" name="${2:-Administrator}" pw out
    [[ "$email" =~ ^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$ ]] || die "invalid email: '$email'"
    pw="$(rand_alnum 24)"
    if ! out="$(printf '%s\n' "$pw" | compose run --rm --no-deps -T api admin crear \
        --correo "$email" --nombre "$name" --contrasena-stdin 2>&1)"; then
        printf '%s\n' "$out" | tail -n 5 >&2
        warn "could not create the administrator $email (does it already exist?)"
        return 1
    fi
    cat << EOF

  First administrator created. This password is shown only once: write it down now.

    User:      $email
    Password:  $pw

  Sign in, change the password and turn on MFA (panel → your profile).

EOF
}

# --- lab migration --------------------------------------------------------------------------------
# The .env of a `make dev-app` install whose containers (project iris) are on this server.
# Only containers started from infra/docker-compose.yml (a lab), never from a production install.
detect_lab_env() {
    local files envs f
    while IFS=$'\t' read -r files envs; do
        case ",$files," in *"/infra/docker-compose.yml,"*) ;; *) continue ;; esac
        f="$(printf '%s' "$envs" | tr ',' '\n' | grep -E '/\.env$' | head -n1 || true)"
        [ -n "$f" ] && [ -f "$f" ] && { printf '%s' "$f"; return 0; }
    done < <(docker ps -a --filter label=com.docker.compose.project=iris --format \
        '{{.Label "com.docker.compose.project.config_files"}}	{{.Label "com.docker.compose.project.environment_file"}}' 2> /dev/null)
    return 0
}

# Keys of the lab .env that never move to production: they point to Mailpit, to localhost or to the
# build, or the production compose and installer set them.
LAB_SKIP=" IRIS_DIR IRIS_VERSION IRIS_REGISTRY IRIS_DB_URL IRIS_REDIS_URL IRIS_S3_ENDPOINT IRIS_S3_PATH_STYLE
 IRIS_ADJUNTOS_ALMACEN IRIS_ADJUNTOS_DIR IRIS_OSRM_URL IRIS_OSRM_PBF_URL IRIS_SMTP_HOST IRIS_SMTP_PORT IRIS_SMTP_TLS
 IRIS_SMTP_USUARIO IRIS_SMTP_CONTRASENA IRIS_SMTP_REMITENTE IRIS_CORREO_CA IRIS_CORREO_REDES_PERMITIDAS
 IRIS_MAILPIT_SMTP_PUERTO IRIS_MAILPIT_UI_PUERTO IRIS_REDIS_PUERTO PORT IRIS_PUERTO IRIS_IMAGEN_TAG
 IRIS_BUILD_CA_EXTRA IRIS_API_URL IRIS_RESPALDO_DIR IRIS_RESPALDO_DESTINATARIOS IRIS_RESPALDO_IDENTIDAD "

import_lab_env() {
    local lab="$1" example key value root
    root="$(cd "$(dirname "$lab")" && pwd)"
    example="$root/.env.example"
    log "importing settings from the lab install ($lab)"
    # Values the lab took from .env.example or from the dev compose defaults when its .env had none:
    # the database and MinIO volumes were created with them.
    lab_value() {
        local v
        if env_has "$1" "$lab"; then v="$(env_get "$1" "$lab")"; else v="$(env_get "$1" "$example")"; fi
        printf '%s' "${v:-$2}"
    }
    for key in IRIS_PG_USUARIO:iris IRIS_PG_CONTRASENA:iris IRIS_PG_BASE:iris IRIS_S3_ACCESS_KEY:iris \
        IRIS_S3_SECRET_KEY:iris-dev-solo-local IRIS_S3_BUCKET:iris-adjuntos IRIS_S3_REGION:us-east-1 \
        IRIS_RED_COMPOSE:172.28.10.0/24 IRIS_WEB_IP:172.28.10.10 IRIS_WEB_PUERTO:3000 IRIS_API_PUERTO:8080 \
        IRIS_PG_PUERTO:5432 IRIS_MINIO_PUERTO:9000 IRIS_MINIO_CONSOLA_PUERTO:9001 IRIS_OSRM_PUERTO:5000 \
        IRIS_MINIO_IMAGEN:pgsty/minio:RELEASE.2026-08-04T00-00-00Z IRIS_MC_IMAGEN:pgsty/mc:RELEASE.2026-09-16T00-00-00Z; do
        env_set "${key%%:*}" "$(lab_value "${key%%:*}" "${key#*:}")"
    done
    # Everything else the lab .env sets explicitly (encryption key, origins, networks, tuning...).
    while IFS= read -r key; do
        case "$LAB_SKIP" in *" $key "*) continue ;; esac
        case "$key" in IRIS_* ) ;; *) continue ;; esac
        value="$(env_get "$key" "$lab")"
        [ -n "$value" ] || continue
        env_set "$key" "$value"
    done < <(grep -oE '^[A-Z][A-Z0-9_]*=' "$lab" | tr -d '=' | awk '!seen[$0]++')
    # The dev default (localhost) is not a public address: ask for the real one.
    case "$(env_get IRIS_BASE_URL)" in http://localhost* | http://127.0.0.1*) env_set IRIS_BASE_URL "" ;; esac
    # Mail networks: the lab allowed the compose network for Mailpit; keep only something else.
    value="$(env_get IRIS_CORREO_REDES_PERMITIDAS "$lab")"
    [ -z "$value" ] || [ "$value" = "$(env_get IRIS_RED_COMPOSE)" ] || env_set IRIS_CORREO_REDES_PERMITIDAS "$value"
    # Backup keys of the lab (relative paths are relative to the repo root).
    for key in IRIS_RESPALDO_DESTINATARIOS IRIS_RESPALDO_IDENTIDAD; do
        value="$(env_get "$key" "$lab")"
        [ -n "$value" ] || continue
        case "$value" in /*) ;; *) value="$root/${value#./}" ;; esac
        [ -s "$value" ] && env_set "$key" "$value"
    done
    if docker volume inspect iris_osrm > /dev/null 2>&1 && ! $WITH_OSRM; then
        warn "the lab has an osrm volume; OSRM stays off. To use it: install.sh --with-osrm"
    fi
}

# Replaces the dev default database password ("iris") once postgres runs under the new compose.
rotate_db_password() {
    local user db new
    user="$(env_get IRIS_PG_USUARIO)"; db="$(env_get IRIS_PG_BASE)"
    new="$(rand_alnum 32)"
    log "replacing the lab database password (dev default) with a generated one"
    printf 'ALTER ROLE "%s" PASSWORD '\''%s'\'';\n' "$user" "$new" |
        compose exec -T postgres psql -X -q -v ON_ERROR_STOP=1 -U "$user" -d "$db" > /dev/null
    env_set IRIS_PG_CONTRASENA "$new"
}

# --- commands -------------------------------------------------------------------------------------
cmd_install() {
    check_docker
    for t in curl tar sha256sum awk; do command -v "$t" > /dev/null || die "$t is required"; done
    mkdir -p "$DIR"
    chmod 755 "$DIR"
    lock

    local mode=fresh
    # A `make dev-app` git checkout in the install folder itself (e.g. the lab cloned in /opt/iris):
    # move it aside to <dir>-lab and migrate from there.
    if [ ! -e "$DIR/current" ] && { [ -d "$DIR/.git" ] || [ -f "$DIR/infra/docker-compose.yml" ]; }; then
        [ ! -e "$DIR-lab" ] || die "$DIR is a git checkout and $DIR-lab already exists: use --dir to pick another install folder"
        log "$DIR is a source checkout (lab install). It will be moved to $DIR-lab and Iris installed in $DIR"
        confirm "Move $DIR to $DIR-lab and migrate it?" y || die "cancelled (use --dir to install somewhere else)"
        exec 9>&-
        mv "$DIR" "$DIR-lab"
        rm -f "$DIR-lab/.lock"
        mkdir -p "$DIR"
        lock
        [ -n "$FROM_LAB" ] || FROM_LAB="$DIR-lab/.env"
        case "$FROM_LAB" in "$DIR"/*) FROM_LAB="$DIR-lab/${FROM_LAB#"$DIR"/}" ;; esac
        [ -f "$FROM_LAB" ] || die "$FROM_LAB not found (the lab .env)"
        LAB_MOVED=true
    fi
    if [ -f "$ENV_FILE" ]; then
        mode=existing
        [ -z "$FROM_LAB" ] || die "Iris is already installed in $DIR: --from-lab is only for the first install"
    else
        # One Iris per Docker host: the compose project and its volumes are always named iris.
        local other
        other="$(docker ps -a --filter label=com.docker.compose.project=iris \
            --format '{{.Label "com.docker.compose.project.config_files"}}' 2> /dev/null |
            tr ',' '\n' | grep -E '/current/docker-compose\.yml$' | grep -vF "$DIR/" | head -n1 || true)"
        [ -z "$other" ] || die "Iris is already installed on this server (${other%/current/docker-compose.yml}); only one install per Docker host"
        [ -n "$FROM_LAB" ] || FROM_LAB="$(detect_lab_env || true)"
        if [ -n "$FROM_LAB" ]; then
            [ -f "$FROM_LAB" ] || die "--from-lab: $FROM_LAB does not exist"
            FROM_LAB="$(cd "$(dirname "$FROM_LAB")" && pwd)/$(basename "$FROM_LAB")"
            mode=lab
            log "found a lab install (make dev-app) that uses $FROM_LAB"
            log "it becomes the production install: same database and attachments, images from GHCR, no Mailpit"
            $LAB_MOVED || confirm "Migrate it now?" y || die "cancelled. To install somewhere else, remove or rename the lab first"
        elif docker volume inspect iris_pgdata > /dev/null 2>&1; then
            die "the Docker volume iris_pgdata already exists but its .env was not found. If it is a make dev-app lab install, run: install.sh --from-lab /path/to/iris/.env"
        fi
    fi

    if [ "$mode" = fresh ] && [ -z "$URL" ] && ! has_tty; then
        die "--url is required when there is no terminal to ask (e.g. --url https://iris.example.com)"
    fi

    # Version
    if [ "$mode" = existing ]; then
        local cur
        cur="$(env_get IRIS_VERSION)"
        if [ -n "$VERSION" ] && [ "$VERSION" != "$cur" ]; then
            die "Iris $cur is installed. To change version: irisctl upgrade --version $VERSION"
        fi
        VERSION="$cur"
    fi
    [ -n "$VERSION" ] || VERSION="$(latest_version)"
    is_semver "$VERSION" || die "could not tell which version to install (got '$VERSION'); use --version X.Y.Z"
    log "Iris $VERSION in $DIR ($mode)"

    fetch_release "$VERSION"
    use_release "$VERSION"
    mkdir -p "$DIR/certs" "$DIR/backups"

    # .env
    if [ "$mode" != existing ]; then
        (umask 077 && cp "$DIR/current/env.example" "$ENV_FILE.new")
        mv -f "$ENV_FILE.new" "$ENV_FILE"
        env_set IRIS_DIR "$DIR"
        env_set IRIS_RESPALDO_DIR "$DIR/backups"
        [ -z "${IRIS_REGISTRY:-}" ] || env_set IRIS_REGISTRY "$IRIS_REGISTRY"
        [ "$mode" != lab ] || import_lab_env "$FROM_LAB"
    else
        env_add_missing "$DIR/current/env.example"
    fi
    chmod 600 "$ENV_FILE"
    env_set IRIS_VERSION "$VERSION"
    env_set IRIS_DIR "$DIR"

    # Answers: flags first, then the current value, then a question.
    local v
    [ -n "$URL" ] || URL="$(env_get IRIS_BASE_URL)"
    [ -n "$URL" ] || URL="$(ask 'Public address of Iris (e.g. https://iris.example.com)')"
    URL="${URL%/}"
    valid_url "$URL" || die "invalid public address: '$URL' (https://host[:port], no path)"
    case "$URL" in http://*) warn "$URL is not HTTPS: put a reverse proxy with a certificate in front (secure cookies need HTTPS)" ;; esac
    env_set IRIS_BASE_URL "$URL"

    if ! $SET_ARGOS && [ "$mode" != existing ] && [ -z "$(env_get IRIS_ARGOS_ORIGENES)" ]; then
        ARGOS="$(ask 'ArgosISP origin allowed to connect (e.g. https://api.argos.example.com; empty = none yet)')"
        SET_ARGOS=true
    fi
    if $SET_ARGOS; then
        valid_origins "$ARGOS" || die "invalid Argos origin(s): '$ARGOS'"
        env_set IRIS_ARGOS_ORIGENES "$ARGOS"
    fi
    if ! $SET_HORUS && [ "$mode" != existing ] && [ -z "$(env_get IRIS_HORUS_ORIGENES)" ]; then
        HORUS="$(ask 'Horus origin allowed to connect (empty = none yet)')"
        SET_HORUS=true
    fi
    if $SET_HORUS; then
        valid_origins "$HORUS" || die "invalid Horus origin(s): '$HORUS'"
        env_set IRIS_HORUS_ORIGENES "$HORUS"
    fi

    if [ -z "$WEB_PORT" ] && [ "$mode" = fresh ]; then
        WEB_PORT="$(ask 'Panel port on 127.0.0.1 (your reverse proxy points here)' "$(env_get IRIS_WEB_PUERTO)")"
    fi
    if [ -n "$WEB_PORT" ]; then valid_port "$WEB_PORT" || die "invalid web port: $WEB_PORT"; env_set IRIS_WEB_PUERTO "$WEB_PORT"; fi
    if [ -n "$API_PORT" ]; then valid_port "$API_PORT" || die "invalid API port: $API_PORT"; env_set IRIS_API_PUERTO "$API_PORT"; fi
    [ "$(env_get IRIS_WEB_PUERTO)" != "$(env_get IRIS_API_PUERTO)" ] || die "the web and API ports must differ"

    if $WITH_OSRM; then
        env_set IRIS_OSRM true
        env_set IRIS_OSRM_URL http://osrm:5000
    fi

    # Secrets: only the missing ones.
    local new_key=false
    if [ -z "$(env_get IRIS_CLAVE_CIFRADO)" ]; then
        env_set IRIS_CLAVE_CIFRADO "$(head -c 32 /dev/urandom | base64 | tr -d '\n')"
        new_key=true
    fi
    [ -n "$(env_get IRIS_PG_CONTRASENA)" ] || env_set IRIS_PG_CONTRASENA "$(rand_alnum 32)"
    [ -n "$(env_get IRIS_S3_SECRET_KEY)" ] || env_set IRIS_S3_SECRET_KEY "$(rand_alnum 32)"
    ensure_backup_keys
    chmod 600 "$ENV_FILE"

    check_ports
    compose config --quiet < /dev/null || die "the compose file of $VERSION is not valid with $ENV_FILE"
    if [ "${IRIS_SKIP_PULL:-0}" != 1 ]; then
        log "pulling the images of Iris $VERSION"
        compose pull --quiet --ignore-buildable < /dev/null || die "could not pull the images (is ghcr.io reachable?)"
    fi

    if [ "$mode" = lab ]; then
        local r
        r="$(env_get IRIS_RESPALDO_DESTINATARIOS)"
        if [ -n "$r" ] && [ -s "$r" ]; then
            compose up -d --wait postgres < /dev/null
            do_backup || die "the backup of the lab database failed: nothing was changed in the lab containers' data"
        else
            warn "no backup taken before the migration (age is missing)"
        fi
        case "$(env_get IRIS_S3_SECRET_KEY)" in iris-dev-solo-local)
            warn "MinIO still uses the lab default secret (only reachable from 127.0.0.1). Changing it is a manual step: docs/runbooks/install.md" ;;
        esac
    fi

    log "starting Iris (migrations run first)"
    start_all || die "Iris did not become healthy. Look at: irisctl logs   (settings: $ENV_FILE)$([ "$mode" = lab ] && echo "; to go back to the lab: docs/runbooks/install.md, \"Back to the lab\"")"
    # Only once the migrated install is healthy (until here, going back to the lab needs no change).
    if [ "$mode" = lab ] && [ "$(env_get IRIS_PG_CONTRASENA)" = iris ] && ! $KEEP_DB_PASSWORD; then
        rotate_db_password
        start_all || die "Iris did not become healthy after changing the database password. Look at: irisctl logs"
    fi
    prune_releases

    # First administrator (not offered when migrating: the lab already has users).
    if [ -n "$ADMIN_EMAIL" ]; then
        create_admin "$ADMIN_EMAIL" "${ADMIN_NAME:-Administrator}" || true
    elif ! $NO_ADMIN && [ "$mode" = fresh ] && has_tty && confirm "Create the first administrator now?" y; then
        ADMIN_EMAIL="$(ask 'Administrator email')"
        ADMIN_NAME="$(ask 'Administrator name' Administrator)"
        create_admin "$ADMIN_EMAIL" "$ADMIN_NAME" || true
    fi

    print_summary "$new_key"
}

print_summary() {
    local new_key="$1" web url host
    web="$(env_get IRIS_WEB_PUERTO)"
    url="$(env_get IRIS_BASE_URL)"
    host="${url#*://}"; host="${host%%:*}"
    cat << EOF

Iris $(env_get IRIS_VERSION) is running.

  Panel (local):   http://127.0.0.1:$web
  Public address:  $url
  Settings:        $ENV_FILE (mode 600)
  Manage:          $( [ -e /usr/local/bin/irisctl ] && echo irisctl || echo "$DIR/current/install.sh" ) status | logs | backup | upgrade | stop | start

EOF
    if [ "$new_key" = true ]; then
        cat << EOF
  IMPORTANT: back up $ENV_FILE OUTSIDE this server now. IRIS_CLAVE_CIFRADO encrypts MFA secrets,
  connector tokens and mailbox passwords: if it is lost, they cannot be recovered.

EOF
    fi
    if [ "$NEW_BACKUP_KEY" = true ]; then
        cat << EOF
  Backups are encrypted with age. Copy the private key $DIR/backup-keys/identity.txt
  OUTSIDE this server (password manager or offline copy): without it, backups cannot be restored.

EOF
    fi
    cat << EOF
Reverse proxy (HTTPS in front of the panel; only the panel port is needed):

  Apache / Virtualmin (Server Configuration → Edit Directives, inside the :443 VirtualHost of $host;
  needs mod_proxy, mod_proxy_http and mod_headers):
      ProxyPreserveHost On
      ProxyPass        / http://127.0.0.1:$web/
      ProxyPassReverse / http://127.0.0.1:$web/
      RequestHeader set X-Forwarded-Proto "https"

  nginx (inside the server { listen 443 ssl; server_name $host; } block):
      client_max_body_size 30m;
      location / {
          proxy_pass http://127.0.0.1:$web;
          proxy_set_header Host \$host;
          proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
          proxy_set_header X-Forwarded-Proto https;
      }

  Nginx Proxy Manager: the panel port listens on 127.0.0.1 only, which NPM cannot reach from its
  container or from another machine. Point the NPM Proxy Host $host (with an SSL certificate and
  "Force SSL") to the Apache/nginx of this server, and let that one proxy to 127.0.0.1:$web as
  above. Details: docs/runbooks/install.md, "Reverse proxy".

Check:
  curl -fsS http://127.0.0.1:$(env_get IRIS_API_PUERTO)/api/v1/salud      # {"estado":"ok",...}
  curl -fsSI $url/entrar                         # HTTP 200 through the proxy

EOF
}

cmd_upgrade() {
    require_install
    lock
    local cur target
    cur="$(env_get IRIS_VERSION)"
    target="${VERSION:-$(latest_version)}"
    is_semver "$target" || die "could not tell which version to install (got '$target'); use --version X.Y.Z"
    if [ "$target" = "$cur" ]; then
        log "Iris $cur is already installed"
        return 0
    fi
    version_ge "$target" "$cur" || die "$target is older than the installed $cur: migrations only move forward (restore a backup instead: docs/runbooks/respaldos.md)"
    log "upgrading Iris $cur → $target"
    confirm "Continue?" y || die "cancelled"

    fetch_release "$target"
    if [ "${IRIS_SKIP_PULL:-0}" != 1 ]; then
        log "pulling the images of Iris $target"
        IRIS_VERSION="$target" docker compose --env-file "$ENV_FILE" -f "$DIR/releases/$target/docker-compose.yml" \
            pull --quiet migrar api worker web < /dev/null || die "could not pull the images of $target (nothing changed)"
    fi
    if $SKIP_BACKUP; then
        warn "--skip-backup: upgrading without a backup"
    else
        do_backup || die "the backup failed: nothing changed (fix it, or use --skip-backup at your own risk)"
    fi

    use_release "$target"
    env_add_missing "$DIR/current/env.example"
    env_set IRIS_VERSION "$target"
    if ! start_all; then
        cat >&2 << EOF

error: Iris $target did not become healthy. Look at: irisctl logs
The database may already be migrated to $target. To go back to $cur:
  1. restore the backup taken above (latest file in $(env_get IRIS_RESPALDO_DIR); docs/runbooks/respaldos.md)
  2. sed -i 's/^IRIS_VERSION=.*/IRIS_VERSION=$cur/' $ENV_FILE && ln -sfn releases/$cur $DIR/current
  3. irisctl start
EOF
        exit 1
    fi
    prune_releases
    log "Iris $target is installed"
}

cmd_status() {
    require_install
    local cur latest api
    cur="$(env_get IRIS_VERSION)"
    latest="$(latest_version 2> /dev/null || true)"
    api="$(env_get IRIS_API_PUERTO)"
    echo "Installed: $cur   Latest release: ${latest:-unknown}$([ -n "$latest" ] && [ "$latest" != "$cur" ] && version_ge "$latest" "$cur" && echo '  (irisctl upgrade)')"
    echo "Folder:    $DIR   Public address: $(env_get IRIS_BASE_URL)"
    echo
    compose ps -a --format 'table {{.Service}}\t{{.Status}}\t{{.Ports}}' < /dev/null || true
    echo
    if curl -fsS --max-time 5 "http://127.0.0.1:${api:-8080}/api/v1/salud" < /dev/null; then echo; else echo "API health: NOT answering on 127.0.0.1:${api:-8080}"; fi
    local last
    last="$(find "$(env_get IRIS_RESPALDO_DIR)" -maxdepth 1 -name '*.dump.age' -printf '%T@ %p\n' 2> /dev/null | sort -rn | head -n1 | cut -d' ' -f2- || true)"
    echo "Last backup: ${last:-none}"
}

case "$CMD" in
    install) cmd_install ;;
    upgrade) cmd_upgrade ;;
    status) cmd_status ;;
    logs) require_install; compose logs -f --tail 200 "${COMPOSE_ARGS[@]}" ;;
    stop) require_install; compose stop < /dev/null; log "Iris stopped (data kept). Start again with: irisctl start" ;;
    start) require_install; lock; start_all || die "Iris did not become healthy. Look at: irisctl logs" ;;
    backup) require_install; do_backup ;;
    create-admin)
        require_install
        [ -n "$ADMIN_EMAIL" ] || ADMIN_EMAIL="$(ask 'Administrator email')"
        [ -n "$ADMIN_NAME" ] || ADMIN_NAME="$(ask 'Administrator name' Administrator)"
        create_admin "$ADMIN_EMAIL" "$ADMIN_NAME"
        ;;
    compose) require_install; compose "${COMPOSE_ARGS[@]}" ;;
    version) require_install; env_get IRIS_VERSION; echo ;;
esac
}

main "$@"
