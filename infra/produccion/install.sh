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
# again is safe: existing settings and secrets are kept, and an interrupted install or lab migration
# resumes where it stopped. After the install, the same script is available as `irisctl`.
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
#   backup           database backup now (encrypted with age; infra/respaldo/respaldar.sh)
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
#   --version X.Y.Z        version to install (default: latest release with an install package)
#   --dir PATH             install folder (default /opt/iris)
#   --from-lab ENV_FILE    migrate a `make dev-app` lab install: reuse its .env, database and
#                          attachments (auto-detected when the lab containers exist)
#   --keep-db-password     with a lab: keep the lab database password (default: replace the dev
#                          default "iris" with a generated one)
#   --with-osrm            also run the OSRM routing service (needs infra/osrm/preparar.sh data)
#   --admin-email EMAIL    create the first administrator with this email
#   --admin-name NAME      name of that administrator (default "Administrator")
#   --no-admin             do not offer to create an administrator
#   --skip-backup          with a lab: migrate without a backup first (not recommended)
#   -y, --yes              never ask; fail if something required is missing
#
# Upgrade options: --version X.Y.Z, --skip-backup (not recommended), -y/--yes.
#
# Environment (tests and offline servers): IRIS_DIR, IRIS_REPO (HCDestroyer/iris), IRIS_SOURCE
# (local folder with iris-<v>.tar.gz, its .sha256 and a `latest` file, instead of GitHub),
# IRIS_SKIP_PULL=1 (images already loaded), IRIS_WAIT_S (seconds to wait for health, 300),
# IRIS_AGE_URL (where to download the age binary when apt has no package).
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
DOCKER_SAFE_LOCALHOST=28
KEEP_RELEASES=3
# age for encrypted backups, when the distribution has no package (Ubuntu 20.04): official release
# binaries, pinned by sha256.
AGE_VERSION=v1.2.1
age_sha256() {
    case "$1" in
        amd64) echo 7df45a6cc87d4da11cc03a539a7470c15b1041ab2b396af088fe9990f7c79d50 ;;
        arm64) echo 57fd79a7ece5fe501f351b9dd51a82fbee1ea8db65a8839db17f5c080245e99f ;;
    esac
}
AGE_URL="${IRIS_AGE_URL:-https://github.com/FiloSottile/age/releases/download/$AGE_VERSION}"
CURL=(curl -fsSL --proto '=https' --tlsv1.2 --retry 3)

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
need_value() { if [ $# -lt 2 ] || [ -z "$2" ]; then die "$1 needs a value"; fi; }
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
[ "$DIR" != "" ] || die "--dir cannot be /"
ENV_FILE="$DIR/.env"
# Marker of an install or lab migration that has not finished: "fresh" or "lab <lab .env>".
PENDING="$DIR/.install-pending"
# age installed by this script (when the distribution has no package) lives here.
PATH="$DIR/bin:$PATH"

# --- helpers -----------------------------------------------------------------------------------
has_tty() { ! $YES && { : < /dev/tty; } 2> /dev/null; }
# Where secrets shown once go: the terminal if there is one (not logs or pipes), else stdout.
to_user() { if { : > /dev/tty; } 2> /dev/null; then cat > /dev/tty; else cat; fi; }

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
    local hint=y/N
    if [ "$def" = y ]; then hint=Y/n; fi
    a="$(ask "$1 [$hint]" "")"
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

# docker compose on the release folder $1 (current by default), with the optional admin ports file.
compose_at() {
    local rel="$1" args=(--env-file "$ENV_FILE" -f "$1/docker-compose.yml")
    shift
    if [ "$(env_get IRIS_PUERTOS_ADMIN)" = true ] && [ -f "$rel/docker-compose.admin-ports.yml" ]; then
        args+=(-f "$rel/docker-compose.admin-ports.yml")
    fi
    [ "$(env_get IRIS_OSRM)" != true ] || args+=(--profile osrm)
    docker compose "${args[@]}" "$@"
}
compose() { compose_at "$DIR/current" "$@"; }

require_install() {
    [ -f "$ENV_FILE" ] || die "Iris is not installed in $DIR (no .env). Install with: sudo bash install.sh"
    [ -f "$DIR/current/docker-compose.yml" ] || die "$DIR/current/docker-compose.yml is missing (broken install?)"
    if [ -f "$PENDING" ] && [ "$CMD" != install ]; then
        case "$CMD" in
            upgrade | create-admin | start)
                die "the install in $DIR has not finished: run install.sh again to resume it, or discard it (docs/runbooks/install.md, \"Discard an unfinished install\")" ;;
            *) warn "the install in $DIR has not finished (run install.sh again to resume it)" ;;
        esac
    fi
    check_docker
}

check_docker() {
    command -v docker > /dev/null || die "Docker is not installed. Install Docker Engine and the compose plugin first: https://docs.docker.com/engine/install/ubuntu/"
    docker info > /dev/null 2>&1 || die "Docker is installed but not answering (is it running? are you root or in the docker group?)"
    local v e
    v="$(docker compose version --short 2> /dev/null || true)"
    [ -n "$v" ] || die "Docker Compose v2 is missing (apt-get install docker-compose-plugin)"
    version_ge "${v#v}" "$COMPOSE_MIN" || die "Docker Compose $COMPOSE_MIN or newer is required (found $v)"
    e="$(docker version --format '{{.Server.Version}}' 2> /dev/null || true)"
    if [ -n "$e" ] && ! version_ge "$e" "$DOCKER_SAFE_LOCALHOST"; then
        warn "Docker Engine $e: before 28.0, ports published on 127.0.0.1 can be reached by other machines on the same network segment. Upgrade Docker Engine to 28 or newer"
    fi
}

# One lock per Docker host (the compose project is always "iris"), outside the install folder, in a
# directory only root can write. The commands that change the install (install, upgrade, start,
# backup) need root, so every one of them takes this same lock.
lock() {
    local d=/run/iris
    [ "$(id -u)" = 0 ] || die "this command changes the install: run it as root (sudo)"
    command -v flock > /dev/null || die "flock is required (util-linux)"
    install -d -m 700 -o root -g root "$d"
    if [ -L "$d" ] || [ "$(stat -c '%u %a' "$d")" != "0 700" ]; then
        die "$d must be a directory owned by root with mode 700"
    fi
    exec 9> "$d/install.lock"
    flock -n 9 || die "another install.sh/irisctl operation is running on this server"
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
    local ours var p vars=(IRIS_WEB_PUERTO IRIS_API_PUERTO)
    if [ "$(env_get IRIS_PUERTOS_ADMIN)" = true ]; then
        vars+=(IRIS_PG_PUERTO IRIS_MINIO_PUERTO IRIS_MINIO_CONSOLA_PUERTO)
        [ "$(env_get IRIS_OSRM)" != true ] || vars+=(IRIS_OSRM_PUERTO)
    fi
    ours=" $(iris_ports | tr '\n' ' ') "
    for var in "${vars[@]}"; do
        p="$(env_get "$var")"
        [ -n "$p" ] || continue
        case "$ours" in *" $p "*) continue ;; esac
        if ss -Hltn "sport = :$p" 2> /dev/null | grep -q .; then
            die "port $p ($var) is already in use on this server. Pick another one: edit $ENV_FILE (or use --web-port/--api-port) and run install.sh again"
        fi
    done
}

# --- release package -----------------------------------------------------------------------------
# Latest version that has an install package (a release whose package is not uploaded yet, or that
# predates the installer, is skipped).
latest_version() {
    local v
    if [ -n "$SOURCE" ]; then
        v="$(tr -d '[:space:]' < "$SOURCE/latest" 2> /dev/null || true)"
    else
        v="$("${CURL[@]}" -H 'Accept: application/vnd.github+json' \
            "https://api.github.com/repos/$REPO/releases?per_page=30" < /dev/null 2> /dev/null |
            grep -oE '"browser_download_url": *"[^"]*/releases/download/v[0-9]+\.[0-9]+\.[0-9]+/iris-[0-9]+\.[0-9]+\.[0-9]+\.tar\.gz"' |
            sed -E 's/.*\/iris-([0-9]+\.[0-9]+\.[0-9]+)\.tar\.gz"$/\1/' | sort -V | tail -n1 || true)"
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
        "${CURL[@]}" -o "$tmp/$pkg" "$base/$pkg" < /dev/null || { rm -rf "$tmp"; die "could not download $base/$pkg (does release v$v have the install package?)"; }
        "${CURL[@]}" -o "$tmp/$pkg.sha256" "$base/$pkg.sha256" < /dev/null || { rm -rf "$tmp"; die "could not download $base/$pkg.sha256"; }
    fi
    (cd "$tmp" && sha256sum -c --quiet "$pkg.sha256") || { rm -rf "$tmp"; die "the sha256 of $pkg does not match: refusing to install it"; }
    # Build provenance (release.yml attests every package): proves it was built by $REPO's workflow.
    if [ -n "$SOURCE" ]; then
        warn "local package ($SOURCE): no provenance check, sha256 only"
    elif command -v gh > /dev/null && gh auth status > /dev/null 2>&1 < /dev/null; then
        local err
        # Only release.yml of $REPO, run from main (on push and on dispatch), signs packages.
        if ! err="$(gh attestation verify "$tmp/$pkg" --repo "$REPO" \
            --signer-workflow "$REPO/.github/workflows/release.yml" --source-ref refs/heads/main \
            < /dev/null 2>&1 > /dev/null)"; then
            rm -rf "$tmp"
            die "gh attestation verify failed for $pkg, refusing to install it. gh said: ${err:-no output}
  (a network, rate-limit or login problem also ends here: fix it and run again)"
        fi
        log "provenance of $pkg verified (built by $REPO's release workflow from main)"
    else
        warn "GitHub CLI (gh) not installed or not logged in$([ "$(id -u)" = 0 ] && echo " as root (sudo gh auth login)"): checking $pkg by sha256 only. To check its provenance by hand, on any machine with gh: download $pkg from https://github.com/$REPO/releases/tag/v$v and run: gh attestation verify $pkg --repo $REPO --signer-workflow $REPO/.github/workflows/release.yml --source-ref refs/heads/main (docs/runbooks/install.md)"
    fi
    tar -xzf "$tmp/$pkg" -C "$tmp" --no-same-owner
    [ -f "$tmp/iris-$v/docker-compose.yml" ] || { rm -rf "$tmp"; die "$pkg does not contain iris-$v/docker-compose.yml"; }
    mkdir -p "$DIR/releases"
    rm -rf "${DIR:?}/releases/$v"
    mv "$tmp/iris-$v" "$DIR/releases/$v"
    : > "$DIR/releases/$v/.verified"
    rm -rf "$tmp"
}

use_release() {
    ln -sfn "releases/$1" "$DIR/current"
    if { [ "$(id -u)" = 0 ] && [ -d /usr/local/bin ] && [ "$DIR" = /opt/iris ]; } || [ "${IRIS_LINK_CLI:-0}" = 1 ]; then
        ln -sfn "$DIR/current/install.sh" /usr/local/bin/irisctl 2> /dev/null || warn "could not create /usr/local/bin/irisctl"
    fi
}

prune_releases() {
    local cur keep=0 v
    cur="$(env_get IRIS_VERSION)"
    while IFS= read -r v; do
        is_semver "$v" || continue
        [ "$v" = "$cur" ] && continue
        keep=$((keep + 1))
        [ "$keep" -le "$KEEP_RELEASES" ] || rm -rf "${DIR:?}/releases/${v:?}"
    done < <(find "$DIR/releases" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' 2> /dev/null | sort -rV)
}

# --- backups -------------------------------------------------------------------------------------
# age from the distribution, or else the official binary (pinned sha256) into $DIR/bin.
ensure_age() {
    command -v age > /dev/null && command -v age-keygen > /dev/null && return 0
    if [ "$(id -u)" = 0 ] && command -v apt-get > /dev/null; then
        log "installing age (backup encryption)"
        DEBIAN_FRONTEND=noninteractive apt-get install -y -qq age < /dev/null > /dev/null 2>&1 ||
            { apt-get update -qq < /dev/null > /dev/null 2>&1 && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq age < /dev/null > /dev/null 2>&1; } || true
    fi
    command -v age > /dev/null && command -v age-keygen > /dev/null && return 0

    local arch tmp file
    case "$(uname -m)" in x86_64 | amd64) arch=amd64 ;; aarch64 | arm64) arch=arm64 ;; *) return 1 ;; esac
    file="age-$AGE_VERSION-linux-$arch.tar.gz"
    log "the distribution has no age package: downloading age $AGE_VERSION ($arch) from its GitHub release"
    tmp="$(mktemp -d)"
    if "${CURL[@]}" -o "$tmp/$file" "$AGE_URL/$file" < /dev/null 2> /dev/null &&
        printf '%s  %s\n' "$(age_sha256 "$arch")" "$tmp/$file" | sha256sum -c --quiet - 2> /dev/null &&
        tar -xzf "$tmp/$file" -C "$tmp" --no-same-owner age/age age/age-keygen; then
        mkdir -p "$DIR/bin"
        install -m 0755 "$tmp/age/age" "$tmp/age/age-keygen" "$DIR/bin/"
    else
        warn "could not download or verify $AGE_URL/$file"
    fi
    rm -rf "$tmp"
    command -v age > /dev/null && command -v age-keygen > /dev/null
}

# Creates the age key pair for backups if the .env has no recipients file. Sets NEW_BACKUP_KEY=true
# when it creates one. Without age, backups fall back to unencrypted dumps (do_backup).
NEW_BACKUP_KEY=false
ensure_backup_keys() {
    local r
    r="$(env_get IRIS_RESPALDO_DESTINATARIOS)"
    [ -n "$r" ] && [ -s "$r" ] && return 0
    ensure_age || return 0
    mkdir -p "$DIR/backup-keys"
    chmod 700 "$DIR/backup-keys"
    if [ ! -s "$DIR/backup-keys/identity.txt" ] && [ ! -s "$DIR/backup-keys/recipients.txt" ]; then
        (umask 077 && age-keygen -o "$DIR/backup-keys/identity.txt" 2> /dev/null)
        age-keygen -y "$DIR/backup-keys/identity.txt" > "$DIR/backup-keys/recipients.txt"
        NEW_BACKUP_KEY=true
        env_set IRIS_RESPALDO_IDENTIDAD "$DIR/backup-keys/identity.txt"
    elif [ ! -s "$DIR/backup-keys/recipients.txt" ]; then
        age-keygen -y "$DIR/backup-keys/identity.txt" > "$DIR/backup-keys/recipients.txt"
    fi
    env_set IRIS_RESPALDO_DESTINATARIOS "$DIR/backup-keys/recipients.txt"
}

# Database backup: encrypted with age (respaldar.sh) when possible; otherwise an unencrypted
# pg_dump -Fc readable only by root, with a loud warning. Never silently skipped.
do_backup() {
    local r dir user db ts out
    ensure_backup_keys
    dir="$(env_get IRIS_RESPALDO_DIR)"; dir="${dir:-$DIR/backups}"
    mkdir -p "$dir"
    chmod 700 "$dir"
    r="$(env_get IRIS_RESPALDO_DESTINATARIOS)"
    if [ -n "$r" ] && [ -s "$r" ] && command -v age > /dev/null; then
        [ -x "$DIR/current/backup/respaldar.sh" ] || die "$DIR/current/backup/respaldar.sh is missing"
        log "backing up the database (encrypted with age)"
        ENV_FILE="$ENV_FILE" IRIS_COMPOSE_ARCHIVO="$DIR/current/docker-compose.yml" IRIS_RESPALDO_DIR="$dir" \
            "$DIR/current/backup/respaldar.sh" --compose < /dev/null
        return
    fi
    user="$(env_get IRIS_PG_USUARIO)"; db="$(env_get IRIS_PG_BASE)"
    ts="$(date -u +%Y%m%dT%H%M%SZ)"
    out="$dir/${db:-iris}-$ts.dump"
    warn "age is not available: this backup is NOT encrypted ($out, readable only by $(id -un)). Install age and run install.sh again to encrypt the next ones"
    (
        umask 077
        compose exec -T postgres pg_dump --format=custom --compress=6 --username "${user:-iris}" --dbname "${db:-iris}" \
            < /dev/null > "$out.partial" || { rm -f "$out.partial"; exit 1; }
        mv "$out.partial" "$out"
        (cd "$dir" && sha256sum "$(basename "$out")" > "$(basename "$out").sha256")
    ) || return 1
    log "Backup: $out"
    local days
    days="$(env_get IRIS_RESPALDO_DIAS)"
    if [[ "${days:-14}" =~ ^[0-9]+$ ]] && [ "${days:-14}" -gt 0 ]; then
        # Only the files this function writes: <db>-<YYYYMMDDTHHMMSSZ>.dump and its .sha256.
        if [[ "${db:-iris}" =~ ^[A-Za-z0-9_-]+$ ]]; then
            find "$dir" -maxdepth 1 -type f -regextype posix-extended \
                -regex ".*/${db:-iris}-[0-9]{8}T[0-9]{6}Z\.dump(\.sha256)?" -mtime +"${days:-14}" -delete
        fi
    fi
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
    to_user << EOF

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
        # A checkout this script already moved from $DIR to $DIR-lab (an earlier, aborted run).
        case "$f" in "$DIR"/*) [ -f "$f" ] || f="$DIR-lab/${f#"$DIR"/}" ;; esac
        [ -n "$f" ] && [ -f "$f" ] && { printf '%s' "$f"; return 0; }
    done < <(docker ps -a --filter label=com.docker.compose.project=iris --format \
        '{{.Label "com.docker.compose.project.config_files"}}	{{.Label "com.docker.compose.project.environment_file"}}' 2> /dev/null)
    return 0
}

# Keys of the lab .env that never move to production: they point to Mailpit, to localhost or to the
# build, or the production compose, its template and the installer set them.
LAB_SKIP=" IRIS_DIR IRIS_VERSION IRIS_REGISTRY IRIS_DB_URL IRIS_REDIS_URL IRIS_S3_ENDPOINT IRIS_S3_PATH_STYLE
 IRIS_ADJUNTOS_ALMACEN IRIS_ADJUNTOS_DIR IRIS_OSRM_URL IRIS_OSRM_PBF_URL IRIS_SMTP_HOST IRIS_SMTP_PORT IRIS_SMTP_TLS
 IRIS_SMTP_USUARIO IRIS_SMTP_CONTRASENA IRIS_SMTP_REMITENTE IRIS_CORREO_CA IRIS_CORREO_REDES_PERMITIDAS
 IRIS_MAILPIT_SMTP_PUERTO IRIS_MAILPIT_UI_PUERTO IRIS_REDIS_PUERTO PORT IRIS_PUERTO IRIS_IMAGEN_TAG
 IRIS_BUILD_CA_EXTRA IRIS_API_URL IRIS_RESPALDO_DIR IRIS_RESPALDO_DESTINATARIOS IRIS_RESPALDO_IDENTIDAD
 IRIS_MINIO_IMAGEN IRIS_MC_IMAGEN IRIS_PUERTOS_ADMIN IRIS_OSRM IRIS_PG_CONTRASENA_NUEVA "

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
        IRIS_PG_PUERTO:5432 IRIS_MINIO_PUERTO:9000 IRIS_MINIO_CONSOLA_PUERTO:9001 IRIS_OSRM_PUERTO:5000; do
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
    # Backup keys of the lab (relative paths are relative to the repo root): copied into the install,
    # so nothing points into the lab checkout.
    local src
    for key in IRIS_RESPALDO_DESTINATARIOS:recipients.txt IRIS_RESPALDO_IDENTIDAD:identity.txt; do
        src="$(env_get "${key%%:*}" "$lab")"
        [ -n "$src" ] || continue
        case "$src" in /*) ;; *) src="$root/${src#./}" ;; esac
        [ -s "$src" ] || continue
        mkdir -p "$DIR/backup-keys"
        chmod 700 "$DIR/backup-keys"
        install -m 0600 "$src" "$DIR/backup-keys/${key#*:}"
        env_set "${key%%:*}" "$DIR/backup-keys/${key#*:}"
    done
    if docker volume inspect iris_osrm > /dev/null 2>&1 && ! $WITH_OSRM; then
        warn "the lab has an osrm volume; OSRM stays off. To use it: install.sh --with-osrm"
    fi
    # MinIO root credentials: the production compose passes them in a URL (MC_HOST_local), so they
    # must be letters and digits; the lab default secret is public. MinIO takes its root credentials
    # from the environment, so new ones apply when minio starts under the production compose.
    if ! [[ "$(env_get IRIS_S3_ACCESS_KEY)" =~ ^[A-Za-z0-9]{3,}$ ]]; then
        log "the lab MinIO user has characters the production compose cannot use: replacing it with 'iris'"
        env_set IRIS_S3_ACCESS_KEY iris
    fi
    if [ "$(env_get IRIS_S3_SECRET_KEY)" = iris-dev-solo-local ] || ! [[ "$(env_get IRIS_S3_SECRET_KEY)" =~ ^[A-Za-z0-9]{8,}$ ]]; then
        log "replacing the lab MinIO secret (public default, or not letters and digits only) with a generated one"
        env_set IRIS_S3_SECRET_KEY "$(rand_alnum 32)"
    fi
}

# MinIO credentials go in a URL (MC_HOST_local): letters and digits only.
check_s3_credentials() {
    [[ "$(env_get IRIS_S3_ACCESS_KEY)" =~ ^[A-Za-z0-9]{3,}$ ]] ||
        die "IRIS_S3_ACCESS_KEY in $ENV_FILE must be 3+ letters and digits only"
    [[ "$(env_get IRIS_S3_SECRET_KEY)" =~ ^[A-Za-z0-9]{8,}$ ]] ||
        die "IRIS_S3_SECRET_KEY in $ENV_FILE must be 8+ letters and digits only (no / + = or symbols). Changing it is safe: MinIO takes it from the .env when it starts"
}

# Value of a lab setting: its .env, else the repo's .env.example, else the dev compose default.
lab_get() {
    local key="$1" def="$2" lab="$3" v
    if env_has "$key" "$lab"; then v="$(env_get "$key" "$lab")"; else v="$(env_get "$key" "$(dirname "$lab")/.env.example")"; fi
    printf '%s' "${v:-$def}"
}

# Refuses to run a release whose migrations are older than the database (it would run old code on a
# newer schema). MIGRATION in the package is the newest goose version of that release. $1 runs psql
# (arguments follow), $2 the user, $3 the database.
schema_ahead() {
    local run="$1" user="$2" name="$3" rel db
    [ -f "$DIR/releases/$VERSION/MIGRATION" ] || { warn "this package does not declare its schema version: skipping the check"; return 1; }
    rel="$(tr -dc '0-9' < "$DIR/releases/$VERSION/MIGRATION")"
    db="$("$run" psql -XAtq -U "$user" -d "$name" -c \
        "select coalesce(max(version_id), 0) from goose_db_version" < /dev/null 2> /dev/null | tr -dc '0-9' || true)"
    db="${db:-0}"
    [ "$db" -gt "${rel:-0}" ] || return 1
    SCHEMA_DB="$db" SCHEMA_REL="$rel"
    return 0
}
SCHEMA_DB="" SCHEMA_REL=""
schema_die() {
    die "the database is at migration $SCHEMA_DB, newer than Iris $VERSION (up to $SCHEMA_REL). Nothing was migrated. Run install.sh again with a version at least as new as the lab: install.sh --version X.Y.Z"
}
# Before anything is written: the lab's own postgres container (started if it was stopped).
LAB_PG=""
lab_psql() { docker exec -i "$LAB_PG" "$@"; }
# If this check started the lab's postgres, it stops it again however it ends (the lab is left as
# it was found).
stop_lab_pg() { docker stop "$LAB_PG" > /dev/null 2>&1 || true; }
check_schema_lab() {
    local lab="$1" n err
    LAB_PG="$(docker ps -aq --filter label=com.docker.compose.project=iris --filter label=com.docker.compose.service=postgres | head -n1)"
    if [ -z "$LAB_PG" ]; then
        warn "the lab postgres container was not found: the schema is checked later, before migrating"
        return 0
    fi
    local started=false
    if [ "$(docker inspect -f '{{.State.Running}}' "$LAB_PG")" != true ]; then
        if ! err="$(docker start "$LAB_PG" 2>&1 > /dev/null)"; then
            die "could not start the lab database container ($(docker inspect -f '{{.Name}}' "$LAB_PG" | tr -d /)) to check its schema: $err"
        fi
        started=true
        trap stop_lab_pg EXIT
    fi
    for n in $(seq 1 30); do
        docker exec "$LAB_PG" pg_isready -q > /dev/null 2>&1 && break
        [ "$n" -lt 30 ] || die "the lab database did not start (docker logs $LAB_PG)"
        sleep 2
    done
    if schema_ahead lab_psql "$(lab_get IRIS_PG_USUARIO iris "$lab")" "$(lab_get IRIS_PG_BASE iris "$lab")"; then
        schema_die
    fi
    if $started; then
        stop_lab_pg
        trap - EXIT
    fi
}
# Again under the production compose, right before migrating (also when resuming).
compose_psql() { compose exec -T postgres "$@"; }
check_schema() {
    if schema_ahead compose_psql "$(env_get IRIS_PG_USUARIO)" "$(env_get IRIS_PG_BASE)"; then
        schema_die
    fi
}

# Marker: "<fresh|lab> <prepared|migrating> <lab .env>". prepared = nothing migrated yet (the
# version can still change); migrating = the migrations may have run.
set_phase() { printf '%s %s %s\n' "$1" "$2" "$3" > "$PENDING"; }

# SCRAM-SHA-256 verifier of a password (what Postgres stores), so the plain password never reaches
# the server or its logs. Computed with node in the panel image of the release (already pulled; no
# extra image, no network). Password on stdin.
scram_verifier() {
    local img
    img="$(compose config --images < /dev/null | grep -E '/iris-web[:@]' | head -n1)"
    [ -n "$img" ] || die "could not find the panel image to compute the password verifier"
    docker run --rm -i --network none --entrypoint node "$img" -e '
        const c = require("crypto");
        let pw = "";
        process.stdin.on("data", d => pw += d).on("end", () => {
            pw = pw.replace(/\r?\n$/, "");
            const salt = c.randomBytes(16), it = 4096;
            const salted = c.pbkdf2Sync(pw, salt, it, 32, "sha256");
            const hmac = (k, s) => c.createHmac("sha256", k).update(s).digest();
            const stored = c.createHash("sha256").update(hmac(salted, "Client Key")).digest();
            const server = hmac(salted, "Server Key");
            process.stdout.write(`SCRAM-SHA-256$${it}:${salt.toString("base64")}$${stored.toString("base64")}:${server.toString("base64")}`);
        });'
}

# Replaces the dev default database password ("iris"). The new password is saved in the .env
# (IRIS_PG_CONTRASENA_NUEVA) before ALTER ROLE, so an interruption can be resumed.
rotate_db_password() {
    local user db new verifier
    user="$(env_get IRIS_PG_USUARIO)"; db="$(env_get IRIS_PG_BASE)"
    new="$(env_get IRIS_PG_CONTRASENA_NUEVA)"
    if [ -z "$new" ]; then
        new="$(rand_alnum 32)"
        env_set IRIS_PG_CONTRASENA_NUEVA "$new"
    fi
    log "setting the generated database password (replaces the lab default)"
    verifier="$(printf '%s\n' "$new" | scram_verifier)"
    [[ "$verifier" =~ ^SCRAM-SHA-256\$4096:[A-Za-z0-9+/=]+\$[A-Za-z0-9+/=]+:[A-Za-z0-9+/=]+$ ]] || die "could not compute the password verifier"
    printf "SET log_statement = 'none'; SET log_min_error_statement = 'panic'; ALTER ROLE \"%s\" PASSWORD '%s';\n" "$user" "$verifier" |
        compose exec -T postgres psql -X -q -v ON_ERROR_STOP=1 -U "$user" -d "$db" > /dev/null
    env_set IRIS_PG_CONTRASENA "$new"
    env_set IRIS_PG_CONTRASENA_NUEVA ""
}

# --- commands -------------------------------------------------------------------------------------
cmd_install() {
    check_docker
    for t in curl tar sha256sum awk; do command -v "$t" > /dev/null || die "$t is required"; done
    lock

    # A `make dev-app` git checkout in the install folder itself (the lab cloned in /opt/iris): move
    # it aside to <dir>-lab before creating anything in it, and migrate from there.
    local lab_moved=false
    if [ -d "$DIR" ] && [ ! -e "$DIR/current" ] && { [ -e "$DIR/.git" ] || [ -f "$DIR/infra/docker-compose.yml" ]; }; then
        [ ! -e "$DIR-lab" ] || die "$DIR is a git checkout and $DIR-lab already exists: use --dir to pick another install folder"
        log "$DIR is a source checkout (lab install). It will be moved to $DIR-lab and Iris installed in $DIR"
        confirm "Move $DIR to $DIR-lab and migrate it?" y || die "cancelled (use --dir to install somewhere else)"
        mv "$DIR" "$DIR-lab"
        [ -n "$FROM_LAB" ] || FROM_LAB="$DIR-lab/.env"
        case "$FROM_LAB" in "$DIR"/*) FROM_LAB="$DIR-lab/${FROM_LAB#"$DIR"/}" ;; esac
        [ -f "$FROM_LAB" ] || die "$FROM_LAB not found (the lab .env)"
        lab_moved=true
    fi
    # As root, never trust a folder another user owns (it could plant files or links).
    if [ -e "$DIR" ] && [ "$(id -u)" = 0 ] && [ "$(stat -c %u "$DIR")" != 0 ]; then
        die "$DIR is owned by $(stat -c %U "$DIR"), not root: move it away (or chown -R root:root it) and run install.sh again"
    fi
    mkdir -p "$DIR"
    chmod 755 "$DIR"

    local mode=fresh resume=false phase=prepared
    if [ -f "$ENV_FILE" ] && [ -f "$PENDING" ]; then
        # An install or lab migration that stopped half way: resume it (same .env, same steps).
        read -r mode phase FROM_LAB < "$PENDING" || true
        case "$mode $phase" in "fresh prepared" | "fresh migrating" | "lab prepared" | "lab migrating") ;; *) die "unreadable $PENDING" ;; esac
        resume=true
        if [ "$mode" = lab ]; then log "resuming the unfinished lab migration in $DIR"; else log "resuming the unfinished install in $DIR"; fi
    elif [ -f "$ENV_FILE" ]; then
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
            $lab_moved || confirm "Migrate it now?" y || die "cancelled. To install somewhere else, remove or rename the lab first"
        elif docker volume inspect iris_pgdata > /dev/null 2>&1; then
            die "the Docker volume iris_pgdata already exists but its .env was not found. If it is a make dev-app lab install, run: install.sh --from-lab /path/to/iris/.env"
        fi
    fi

    if [ "$mode" = fresh ] && [ -z "$URL" ] && ! has_tty && [ -z "$(env_get IRIS_BASE_URL)" ]; then
        die "--url is required when there is no terminal to ask (e.g. --url https://iris.example.com)"
    fi

    # Version. A resumed install can still change it while nothing has been migrated.
    if [ "$mode" = existing ] || $resume; then
        local cur
        cur="$(env_get IRIS_VERSION)"
        if [ -n "$VERSION" ] && [ "$VERSION" != "$cur" ]; then
            if $resume && [ "$phase" = prepared ]; then
                log "nothing was migrated yet: switching the unfinished install from $cur to $VERSION"
            elif $resume; then
                die "Iris $cur is being installed and its migrations may have run: finish it (run install.sh again), then irisctl upgrade --version $VERSION"
            else
                die "Iris $cur is installed. To change version: irisctl upgrade --version $VERSION"
            fi
        else
            VERSION="$cur"
        fi
    fi
    [ -n "$VERSION" ] || VERSION="$(latest_version)"
    is_semver "$VERSION" || die "could not tell which version to install (got '$VERSION'); use --version X.Y.Z"
    log "Iris $VERSION in $DIR ($mode)"

    fetch_release "$VERSION"
    # Before writing anything for a lab: is the release at least as new as the lab database?
    if [ "$mode" = lab ] && ! $resume; then
        check_schema_lab "$FROM_LAB"
    fi
    use_release "$VERSION"
    mkdir -p "$DIR/certs"

    # .env. The pending marker goes first: until the end, a re-run resumes this install.
    if [ "$mode" != existing ] && ! $resume; then
        set_phase "$mode" prepared "$FROM_LAB"
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
    local ask_more=false
    { [ "$mode" != existing ] && ! $resume; } && ask_more=true
    [ -n "$URL" ] || URL="$(env_get IRIS_BASE_URL)"
    [ -n "$URL" ] || URL="$(ask 'Public address of Iris (e.g. https://iris.example.com)')"
    URL="${URL%/}"
    valid_url "$URL" || die "invalid public address: '$URL' (https://host[:port], no path)"
    case "$URL" in http://*) warn "$URL is not HTTPS: put a reverse proxy with a certificate in front (secure cookies need HTTPS)" ;; esac
    env_set IRIS_BASE_URL "$URL"

    if ! $SET_ARGOS && $ask_more && [ -z "$(env_get IRIS_ARGOS_ORIGENES)" ]; then
        ARGOS="$(ask 'ArgosISP origin allowed to connect (e.g. https://api.argos.example.com; empty = none yet)')"
        SET_ARGOS=true
    fi
    if $SET_ARGOS; then
        valid_origins "$ARGOS" || die "invalid Argos origin(s): '$ARGOS'"
        env_set IRIS_ARGOS_ORIGENES "$ARGOS"
    fi
    if ! $SET_HORUS && $ask_more && [ -z "$(env_get IRIS_HORUS_ORIGENES)" ]; then
        HORUS="$(ask 'Horus origin allowed to connect (empty = none yet)')"
        SET_HORUS=true
    fi
    if $SET_HORUS; then
        valid_origins "$HORUS" || die "invalid Horus origin(s): '$HORUS'"
        env_set IRIS_HORUS_ORIGENES "$HORUS"
    fi

    if [ -z "$WEB_PORT" ] && $ask_more && [ "$mode" = fresh ]; then
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
    check_s3_credentials
    ensure_backup_keys
    chmod 600 "$ENV_FILE"

    check_ports
    compose config --quiet < /dev/null || die "the compose file of $VERSION is not valid with $ENV_FILE"
    if [ "${IRIS_SKIP_PULL:-0}" != 1 ]; then
        log "pulling the images of Iris $VERSION"
        compose pull --quiet --ignore-buildable < /dev/null || die "could not pull the images (is ghcr.io reachable?)"
    fi

    if [ "$mode" = lab ]; then
        compose up -d --wait postgres < /dev/null || die "the lab database did not start under the production compose"
        check_schema
        if $SKIP_BACKUP; then
            warn "--skip-backup: migrating the lab database WITHOUT a backup"
        else
            do_backup || die "the backup of the lab database failed. No migration has run: the database is as the lab left it. Fix the problem and run install.sh again (it resumes the migration)"
        fi
    fi

    # A password change interrupted after ALTER ROLE: finish it before anything connects.
    if [ -n "$(env_get IRIS_PG_CONTRASENA_NUEVA)" ]; then
        compose up -d --wait postgres < /dev/null || die "the database did not start"
        rotate_db_password
    fi

    [ "$mode" = existing ] || set_phase "$mode" migrating "$FROM_LAB"
    log "starting Iris (migrations run first)"
    if ! start_all; then
        [ "$mode" != lab ] || warn "the migrations may have run already. To go back to the lab you need the backup above: docs/runbooks/install.md, \"Back to the lab\""
        die "Iris did not become healthy. Look at: irisctl logs   (settings: $ENV_FILE). Run install.sh again to resume, or discard it: docs/runbooks/install.md, \"Discard an unfinished install\""
    fi
    # Lab defaults that are public (in the repository): replaced once the migrated install is healthy.
    if [ "$mode" = lab ]; then
        if ! $KEEP_DB_PASSWORD && [ "$(env_get IRIS_PG_CONTRASENA)" = iris ]; then
            rotate_db_password
            start_all || die "Iris did not become healthy after changing the database password. Look at: irisctl logs (run install.sh again to resume)"
        fi
    fi
    prune_releases
    rm -f "$PENDING"

    # First administrator (not offered when migrating: the lab already has users).
    if [ -n "$ADMIN_EMAIL" ]; then
        create_admin "$ADMIN_EMAIL" "${ADMIN_NAME:-Administrator}" || true
    elif ! $NO_ADMIN && [ "$mode" = fresh ] && has_tty && confirm "Create the first administrator now?" y; then
        ADMIN_EMAIL="$(ask 'Administrator email')"
        ADMIN_NAME="$(ask 'Administrator name' Administrator)"
        create_admin "$ADMIN_EMAIL" "$ADMIN_NAME" || true
    fi

    print_summary "$new_key"
    offer_identity_removal
}

# The private backup key is needed only to restore: show it once and offer to remove it from the
# server once it is safely copied elsewhere.
offer_identity_removal() {
    local id
    $NEW_BACKUP_KEY || return 0
    id="$DIR/backup-keys/identity.txt"
    [ -s "$id" ] || return 0
    if ! has_tty; then
        warn "the private backup key is in $id (mode 600). Copy it off this server; you can delete it afterwards"
        return 0
    fi
    {
        printf '\n  Private key of the backups (%s) — copy it to a password manager now:\n\n' "$id"
        sed 's/^/    /' "$id"
        printf '\n'
    } > /dev/tty
    if confirm "Have you copied it? Delete it from this server now (backups only need the public key)?" n; then
        rm -f "$id"
        env_set IRIS_RESPALDO_IDENTIDAD ""
        log "deleted $id: to restore a backup, use your copy (IRIS_RESPALDO_IDENTIDAD=<file>)"
    else
        warn "the private backup key stays in $id (mode 600). Copy it off this server; you can delete it afterwards"
    fi
}

print_summary() {
    local new_key="$1" web url host manage="$DIR/current/install.sh"
    if [ -e /usr/local/bin/irisctl ]; then manage=irisctl; fi
    web="$(env_get IRIS_WEB_PUERTO)"
    url="$(env_get IRIS_BASE_URL)"
    host="${url#*://}"; host="${host%%:*}"
    cat << EOF

Iris $(env_get IRIS_VERSION) is running.

  Panel (local):   http://127.0.0.1:$web
  Public address:  $url
  Settings:        $ENV_FILE (mode 600)
  Manage:          $manage status | logs | backup | upgrade | stop | start

EOF
    if [ "$new_key" = true ]; then
        cat << EOF
  IMPORTANT: back up $ENV_FILE OUTSIDE this server now. IRIS_CLAVE_CIFRADO encrypts MFA secrets,
  connector tokens and mailbox passwords: if it is lost, they cannot be recovered.

EOF
    fi
    if ! command -v age > /dev/null; then
        cat << EOF
  WARNING: age is not installed, so backups are NOT encrypted. Install it (apt-get install age, or
  the binary from https://github.com/FiloSottile/age/releases) and run install.sh again.

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
    check_s3_credentials
    confirm "Continue?" y || die "cancelled"

    fetch_release "$target"
    IRIS_VERSION="$target" compose_at "$DIR/releases/$target" config --quiet < /dev/null ||
        die "the compose file of $target is not valid with $ENV_FILE (nothing changed)"
    if [ "${IRIS_SKIP_PULL:-0}" != 1 ]; then
        log "pulling the images of Iris $target"
        IRIS_VERSION="$target" compose_at "$DIR/releases/$target" pull --quiet < /dev/null ||
            die "could not pull the images of $target (nothing changed)"
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
    last="$(find "$(env_get IRIS_RESPALDO_DIR)" -maxdepth 1 \( -name '*.dump.age' -o -name '*.dump' \) -printf '%T@ %p\n' 2> /dev/null | sort -rn | head -n1 | cut -d' ' -f2- || true)"
    echo "Last backup: ${last:-none}"
}

case "$CMD" in
    install) cmd_install ;;
    upgrade) cmd_upgrade ;;
    status) cmd_status ;;
    logs) require_install; compose logs -f --tail 200 "${COMPOSE_ARGS[@]}" ;;
    stop) require_install; compose stop < /dev/null; log "Iris stopped (data kept). Start again with: irisctl start" ;;
    start) require_install; lock; start_all || die "Iris did not become healthy. Look at: irisctl logs" ;;
    backup) require_install; lock; do_backup ;;
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
