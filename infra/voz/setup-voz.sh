#!/usr/bin/env bash
# Instalación de un paso del Servidor de IA de Iris («Iris Voz»): prepara el servidor y ejecuta el
# instalador firmado (docs/runbooks/servidor-ia.md §2).
#
# Una sola línea, en el servidor con la tarjeta NVIDIA:
#
#   curl -fsSL https://github.com/HCDestroyer/iris/releases/download/voz-v0.1.1/setup-voz.sh | sudo bash
#
# Sirve la URL de cualquier versión voz-v*: el script instala siempre la última publicada (o la de
# --version). Para verificar este script antes de correrlo, mira el runbook (§2, «Verificar este
# script»).
#
# Qué hace, en orden (se detiene con un mensaje claro en cuanto algo falla):
#   1. Comprueba que corre como root, la distribución (Ubuntu, Debian, RHEL, Rocky o Alma), la
#      arquitectura, el reloj (NTP), el intercambio a disco y que el puerto de control esté libre.
#   2. Comprueba la GPU: sin el controlador de NVIDIA se detiene y explica cómo instalarlo (pide
#      reiniciar, por eso no lo instala él).
#   3. Si faltan Docker o el NVIDIA Container Toolkit, PREGUNTA y los instala de sus repositorios
#      oficiales, con su llave GPG comprobada por huella; configura el runtime de NVIDIA en Docker.
#   4. Usa cosign 2.4 o más nuevo; si no hay, baja el oficial de sigstore con versión y sha256 fijos.
#   5. Elige la versión: la última voz-vX.Y.Z publicada o la de --version.
#   6. Baja install-voz.sh y su firma, verifica que la firmó el workflow voz-release.yml de este
#      repositorio en la etiqueta voz-vX.Y.Z y SOLO entonces lo ejecuta.
# Correrlo otra vez no rompe nada: lo que ya está se deja como está.
#
# Uso: setup-voz.sh [opciones]       (con la tubería: curl ... | sudo bash -s -- [opciones])
#
#   --version X.Y.Z           versión de Iris Voz (por defecto, la última voz-v* publicada)
#   -y, --yes                 no preguntar: instala lo que falte sin pedir confirmación
#   --dir RUTA                carpeta de instalación (por defecto /opt/iris-voz)
#   --port N                  puerto de control (por defecto 8443)
#   --listen DIRECCION        dirección en la que escucha el puerto de control
#   --no-models               no bajar los modelos por defecto
#   --allow-swap              seguir aunque haya intercambio a disco (solo si está cifrado)
#   --allow-unsynced-clock    seguir aunque el reloj no esté sincronizado por NTP
#   -h, --help                esta ayuda
set -Eeuo pipefail

# --- Lo fijo (infra/voz/verificar.sh comprueba que coincide con install-voz.sh) ------------------

REPO=HCDestroyer/iris
RELEASES="https://github.com/$REPO/releases/download"
API_RELEASES="https://api.github.com/repos/$REPO/releases"
# Quién debe haber firmado install-voz.sh: el workflow de release en la etiqueta voz-vX.Y.Z.
OIDC_ISSUER=https://token.actions.githubusercontent.com
signer_identity() { printf 'https://github.com/HCDestroyer/iris/.github/workflows/voz-release.yml@refs/tags/voz-v%s' "$1"; }

# cosign oficial de sigstore, fijado por sha256 (el mismo que fija install-voz.sh).
COSIGN_MIN=2.4
COSIGN_VERSION=v2.6.1
cosign_sha256() {
    case "$1" in
        amd64) echo 064954c5d8c7e3b28188eee5b1727b31c411550bc5fefd41aa672d3c761d103a ;;
        arm64) echo 56a16480bdd56ec789abaa65924402f6b92c0041f06885995853c05567b76f34 ;;
    esac
}
COSIGN_URL="https://github.com/sigstore/cosign/releases/download/$COSIGN_VERSION"

# Huellas de las llaves GPG oficiales de los repositorios de paquetes.
DOCKER_GPG_DEB=9DC858229FC7DD38854AE2D88D81803C0EBFCD88  # download.docker.com/linux/{ubuntu,debian}/gpg
DOCKER_GPG_RPM=060A61C51B558A7F742B77AAC52FEB6B621E9F35  # download.docker.com/linux/rhel/gpg
NVIDIA_GPG=C95B321B61E88C1809C4F759DDCAE044F796ECB0      # nvidia.github.io/libnvidia-container/gpgkey
NVIDIA_REPO=https://nvidia.github.io/libnvidia-container

COMPOSE_MIN=2.24
CURL=(curl -fsSL --proto '=https' --tlsv1.2 --retry 3)
# Solo pruebas (infra/voz/prueba-setup.sh): otro /etc/os-release y otra terminal para las preguntas.
OS_RELEASE="${IRIS_VOZ_OS_RELEASE:-/etc/os-release}"
TTY="${IRIS_VOZ_TTY:-/dev/tty}"

# --- Utilidades ----------------------------------------------------------------------------------

paso() { printf '\n\033[1;36m[%s]\033[0m \033[1m%s\033[0m\n' "$1" "$2" >&2; }
log() { printf '  %s\n' "$*" >&2; }
ok() { printf '  \033[32mlisto:\033[0m %s\n' "$*" >&2; }
warn() { printf '  \033[33maviso:\033[0m %s\n' "$*" >&2; }
die() { printf '\n\033[31mNo se pudo seguir:\033[0m %s\n' "$*" >&2; exit 1; }
usage() {
    local src="${BASH_SOURCE[0]:-}"
    if [ -n "$src" ] && [ -r "$src" ]; then
        sed -n '2,/^set -E/{/^#/p}' "$src" | sed 's/^# \{0,1\}//'
    else
        echo "Ayuda: docs/runbooks/servidor-ia.md §2 (o baja setup-voz.sh y corre: bash setup-voz.sh --help)"
    fi
    exit "${1:-0}"
}

version_ge() { [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" = "$2" ]; }
is_semver() { [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; }

# ask "pregunta" → 0 si responde que sí. Con --yes no pregunta. Lee de la terminal, no de la entrada
# (con `curl | sudo bash` la entrada es el propio script).
ask() {
    if $YES; then log "$1 → sí (--yes)"; return 0; fi
    { : < "$TTY"; } 2> /dev/null || die "hace falta responder «$1» y no hay terminal. Vuelve a correrlo con --yes para aceptar"
    local r=""
    printf '  %s [s/N] ' "$1" > "$TTY"
    IFS= read -r r < "$TTY" || true
    case "$r" in s | S | si | sí | Si | Sí | SI | SÍ | y | Y | yes) return 0 ;; *) return 1 ;; esac
}

# fetch_key URL DESTINO HUELLA [dearmor]: baja una llave GPG, comprueba que trae UNA llave principal
# con la huella fijada y solo entonces la deja en DESTINO (0644).
fetch_key() {
    local url="$1" dest="$2" want="$3" k="$TMP/llave" fprs
    "${CURL[@]}" -o "$k" "$url" < /dev/null || die "no se pudo bajar la llave GPG de $url"
    command -v gpg > /dev/null || die "falta gpg para comprobar la llave de $url"
    mkdir -p "$TMP/gnupg" && chmod 0700 "$TMP/gnupg"
    fprs="$(GNUPGHOME="$TMP/gnupg" gpg --batch --show-keys --with-colons "$k" 2> /dev/null |
        awk -F: '$1 == "pub" { p = 1; next } p && $1 == "fpr" { print $10; p = 0 }')" || true
    [ "$fprs" = "$want" ] || die "la llave GPG de $url no es la oficial (huella esperada $want, llegó '${fprs:-ninguna}'). No se instaló nada"
    mkdir -p "$(dirname "$dest")"
    if [ "${4:-}" = dearmor ]; then
        GNUPGHOME="$TMP/gnupg" gpg --batch --yes --dearmor -o "$k.gpg" "$k"
        k="$k.gpg"
    fi
    install -m 0644 "$k" "$dest"
}

# write_file DESTINO < contenido (0644; reemplaza solo si cambió).
write_file() {
    cat > "$TMP/archivo"
    if ! cmp -s "$TMP/archivo" "$1" 2> /dev/null; then install -m 0644 "$TMP/archivo" "$1"; fi
}

# --- 1. El servidor ------------------------------------------------------------------------------

check_root() {
    [ "$(id -u)" = 0 ] || die "hay que correrlo como administrador. Usa:  curl -fsSL <URL de setup-voz.sh> | sudo bash"
}

detect_os() {
    [ -r "$OS_RELEASE" ] || die "no se pudo saber qué Linux es este servidor (falta $OS_RELEASE)"
    local info
    # shellcheck disable=SC1090
    info="$(. "$OS_RELEASE" && printf '%s|%s|%s|%s' "${ID:-}" "${VERSION_ID:-}" "${UBUNTU_CODENAME:-${VERSION_CODENAME:-}}" "${PRETTY_NAME:-${ID:-}}")"
    IFS='|' read -r OS_ID OS_VERSION OS_CODENAME OS_NAME <<< "$info"
    case "$OS_ID" in
        ubuntu | debian)
            FAMILY=deb
            [ -n "$OS_CODENAME" ] || die "no se pudo saber la versión de $OS_NAME (falta VERSION_CODENAME)"
            ;;
        rhel | rocky | almalinux)
            FAMILY=rpm
            [[ "${OS_VERSION%%.*}" =~ ^[0-9]+$ ]] && [ "${OS_VERSION%%.*}" -ge 8 ] ||
                die "$OS_NAME es demasiado viejo: hace falta la versión 8 o más nueva"
            ;;
        *) die "este servidor es «${OS_NAME:-desconocido}». Solo se admite Ubuntu, Debian, RHEL, Rocky Linux o AlmaLinux" ;;
    esac
    ok "$OS_NAME"
}

detect_arch() {
    case "$(uname -m)" in
        x86_64 | amd64) ARCH=amd64 ;;
        aarch64 | arm64) ARCH=arm64 ;;
        *) die "la arquitectura $(uname -m) no está soportada (solo x86_64 y arm64)" ;;
    esac
    ok "arquitectura $ARCH"
}

clock_synced() { [ "$(timedatectl show -p NTPSynchronized --value 2> /dev/null)" = yes ]; }
check_clock() {
    if ! command -v timedatectl > /dev/null; then
        warn "no se puede comprobar el reloj (no hay timedatectl): asegúrate de que NTP está activo"
        return 0
    fi
    if clock_synced; then ok "reloj sincronizado (NTP)"; return 0; fi
    if $ALLOW_CLOCK; then warn "el reloj no está sincronizado (--allow-unsynced-clock)"; return 0; fi
    log "El reloj no está sincronizado por internet (NTP). Iris rechaza las peticiones con más de 30 s de diferencia."
    ask "¿Activar la sincronización automática del reloj?" ||
        die "sin el reloj sincronizado Iris no acepta este servidor. Actívalo (timedatectl set-ntp true) y vuelve a correr el script"
    timedatectl set-ntp true < /dev/null || die "no se pudo activar NTP (timedatectl set-ntp true)"
    local _
    for _ in $(seq 1 30); do
        if clock_synced; then ok "reloj sincronizado (NTP)"; return 0; fi
        sleep 2
    done
    die "se activó NTP pero el reloj aún no se sincroniza. Revisa que el servidor llegue a internet y vuelve a correr el script en unos minutos"
}

check_swap() {
    command -v swapon > /dev/null || return 0
    if [ -z "$(swapon --noheadings 2> /dev/null || true)" ]; then ok "sin intercambio a disco"; return 0; fi
    if $ALLOW_SWAP; then warn "hay intercambio a disco (--allow-swap: debe estar cifrado)"; return 0; fi
    log "Hay intercambio a disco (swap): lo que el modelo tiene en memoria podría acabar en el disco sin cifrar."
    ask "¿Apagar el intercambio a disco (ahora y al reiniciar)?" ||
        die "con intercambio a disco no se instala. Apágalo (swapoff -a) o, si está cifrado, vuelve a correr con --allow-swap"
    swapoff -a < /dev/null || die "no se pudo apagar el intercambio (swapoff -a)"
    if [ -f /etc/fstab ] && grep -qE '^[^#].*[[:space:]]swap[[:space:]]' /etc/fstab; then
        [ -f /etc/fstab.antes-de-iris-voz ] || cp -p /etc/fstab /etc/fstab.antes-de-iris-voz
        sed -i -E 's/^([^#].*[[:space:]]swap[[:space:]].*)$/# (iris-voz) \1/' /etc/fstab
        log "Se comentó el intercambio en /etc/fstab (copia en /etc/fstab.antes-de-iris-voz)."
    fi
    ok "intercambio a disco apagado"
}

# El puerto de control está libre, o ya lo usa Iris Voz (correr el script otra vez).
check_port() {
    local p="${PORT:-8443}"
    if ! command -v ss > /dev/null; then warn "no se puede comprobar el puerto $p (no hay ss)"; return 0; fi
    if [ -z "$(ss -Hltn "sport = :$p" 2> /dev/null || true)" ]; then ok "puerto $p libre"; return 0; fi
    if command -v docker > /dev/null &&
        docker ps --filter label=com.docker.compose.project=iris-voz --format '{{.Ports}}' 2> /dev/null < /dev/null | grep -q ":$p->"; then
        ok "el puerto $p ya lo usa Iris Voz (se reinstala o actualiza)"
        return 0
    fi
    die "el puerto $p ya lo usa otro programa (míralo con: ss -ltnp 'sport = :$p'). Libéralo o elige otro con --port N"
}

# --- 2. GPU --------------------------------------------------------------------------------------

check_gpu() {
    local gpus=""
    if command -v nvidia-smi > /dev/null; then gpus="$(nvidia-smi -L 2> /dev/null < /dev/null | grep -E '^GPU [0-9]+' || true)"; fi
    if [ -n "$gpus" ]; then
        while IFS= read -r g; do ok "$g"; done <<< "$gpus"
        return 0
    fi
    local tarjeta="no se ve ninguna tarjeta NVIDIA"
    if command -v lspci > /dev/null && lspci 2> /dev/null | grep -qi 'nvidia'; then tarjeta="hay una tarjeta NVIDIA, pero falta su controlador"; fi
    local como
    case "$OS_ID" in
        ubuntu) como="sudo apt-get update && sudo ubuntu-drivers install" ;;
        debian) como="activa los componentes «contrib non-free non-free-firmware» en /etc/apt/sources.list y luego: sudo apt-get update && sudo apt-get install -y nvidia-driver firmware-misc-nonfree" ;;
        *) como="sigue la guía oficial de NVIDIA para $OS_NAME: https://docs.nvidia.com/datacenter/tesla/driver-installation-guide/" ;;
    esac
    die "$tarjeta (nvidia-smi no funciona).

  El Servidor de IA necesita una tarjeta NVIDIA con su controlador. Este script no lo instala porque
  hay que reiniciar el servidor después. Hazlo así:

    1. Instala el controlador:
         $como
    2. Reinicia el servidor:  sudo reboot
    3. Comprueba que se ve la tarjeta:  nvidia-smi
    4. Vuelve a correr la misma línea de instalación."
}

# --- 3. Docker y NVIDIA Container Toolkit --------------------------------------------------------

compose_ok() {
    local v
    v="$(docker compose version --short 2> /dev/null < /dev/null || true)"
    [ -n "$v" ] && version_ge "${v#v}" "$COMPOSE_MIN"
}

pkg_installed() {
    if [ "$FAMILY" = deb ]; then dpkg-query -W -f '${Status}' "$1" 2> /dev/null | grep -q 'install ok installed'; else rpm -q "$1" > /dev/null 2>&1; fi
}

docker_repo() {
    if [ "$FAMILY" = deb ]; then
        export DEBIAN_FRONTEND=noninteractive
        apt-get update -q < /dev/null || die "apt-get update falló"
        apt-get install -y -q ca-certificates curl gnupg < /dev/null || die "no se pudieron instalar ca-certificates, curl y gnupg"
        fetch_key "https://download.docker.com/linux/$OS_ID/gpg" /etc/apt/keyrings/docker.asc "$DOCKER_GPG_DEB"
        write_file /etc/apt/sources.list.d/docker.list << EOF
deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/$OS_ID $OS_CODENAME stable
EOF
        apt-get update -q < /dev/null || die "apt-get update falló con el repositorio de Docker"
    else
        fetch_key https://download.docker.com/linux/rhel/gpg /etc/pki/rpm-gpg/docker-ce.asc "$DOCKER_GPG_RPM"
        rpm --import /etc/pki/rpm-gpg/docker-ce.asc < /dev/null
        # shellcheck disable=SC2016 # $basearch y $releasever los resuelve dnf
        write_file /etc/yum.repos.d/docker-ce.repo << 'EOF'
[docker-ce-stable]
name=Docker CE Stable - $basearch
baseurl=https://download.docker.com/linux/rhel/$releasever/$basearch/stable
enabled=1
gpgcheck=1
gpgkey=file:///etc/pki/rpm-gpg/docker-ce.asc
EOF
    fi
}

pkg_install() {
    if [ "$FAMILY" = deb ]; then
        DEBIAN_FRONTEND=noninteractive apt-get install -y -q "$@" < /dev/null
    else
        dnf -y install "$@" < /dev/null
    fi
}

ensure_docker() {
    if command -v docker > /dev/null && compose_ok; then
        :
    elif command -v docker > /dev/null; then
        # Hay Docker sin compose 2.24+: solo se completa si es el Docker oficial (docker-ce).
        pkg_installed docker-ce ||
            die "hay un Docker que no es el oficial (o podman-docker) y le falta docker compose $COMPOSE_MIN o más nuevo. Quítalo (por ejemplo: apt-get remove docker.io / dnf remove podman-docker) y vuelve a correr el script: instalará el Docker oficial"
        ask "¿Instalar o actualizar docker compose (repositorio oficial de Docker)?" ||
            die "sin docker compose $COMPOSE_MIN o más nuevo no se puede instalar Iris Voz"
        docker_repo
        pkg_install docker-compose-plugin || die "no se pudo instalar docker-compose-plugin"
    else
        log "Docker no está instalado."
        ask "¿Instalar Docker desde su repositorio oficial (download.docker.com)?" ||
            die "sin Docker no se puede instalar Iris Voz. Vuelve a correr el script cuando quieras instalarlo"
        docker_repo
        pkg_install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin ||
            die "no se pudo instalar Docker (si $OS_NAME trae podman o runc, quítalos y vuelve a intentar)"
    fi
    if ! docker info > /dev/null 2>&1 < /dev/null; then
        log "Arrancando Docker"
        systemctl enable --now docker < /dev/null || die "Docker no arranca (mira: systemctl status docker)"
    fi
    { command -v docker > /dev/null && compose_ok; } || die "Docker o docker compose $COMPOSE_MIN no quedaron instalados"
    ok "Docker $(docker version --format '{{.Server.Version}}' 2> /dev/null < /dev/null || echo '?') con compose $(docker compose version --short 2> /dev/null < /dev/null)"
}

nvidia_runtime() { docker info --format '{{json .Runtimes}}' 2> /dev/null < /dev/null | grep -q nvidia; }

ensure_nvidia_toolkit() {
    if nvidia_runtime; then ok "Docker ya usa la GPU (runtime de NVIDIA)"; return 0; fi
    if ! command -v nvidia-ctk > /dev/null; then
        log "Falta el NVIDIA Container Toolkit (deja que Docker use la tarjeta)."
        ask "¿Instalar el NVIDIA Container Toolkit desde su repositorio oficial (nvidia.github.io) y reiniciar Docker?" ||
            die "sin el NVIDIA Container Toolkit los contenedores no ven la GPU. Vuelve a correr el script cuando quieras instalarlo"
        if [ "$FAMILY" = deb ]; then
            fetch_key "$NVIDIA_REPO/gpgkey" /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg "$NVIDIA_GPG" dearmor
            # shellcheck disable=SC2016 # $(ARCH) lo resuelve apt
            write_file /etc/apt/sources.list.d/nvidia-container-toolkit.list << 'EOF'
deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://nvidia.github.io/libnvidia-container/stable/deb/$(ARCH) /
EOF
            DEBIAN_FRONTEND=noninteractive apt-get update -q < /dev/null || die "apt-get update falló con el repositorio de NVIDIA"
        else
            fetch_key "$NVIDIA_REPO/gpgkey" /etc/pki/rpm-gpg/nvidia-container-toolkit.asc "$NVIDIA_GPG"
            # shellcheck disable=SC2016 # $basearch lo resuelve dnf
            write_file /etc/yum.repos.d/nvidia-container-toolkit.repo << 'EOF'
[nvidia-container-toolkit]
name=nvidia-container-toolkit
baseurl=https://nvidia.github.io/libnvidia-container/stable/rpm/$basearch
repo_gpgcheck=1
gpgcheck=0
enabled=1
gpgkey=file:///etc/pki/rpm-gpg/nvidia-container-toolkit.asc
EOF
        fi
        pkg_install nvidia-container-toolkit || die "no se pudo instalar nvidia-container-toolkit"
    else
        ask "¿Configurar Docker para que use la GPU (nvidia-ctk) y reiniciarlo?" ||
            die "sin el runtime de NVIDIA los contenedores no ven la GPU"
    fi
    nvidia-ctk runtime configure --runtime=docker < /dev/null > /dev/null || die "falló nvidia-ctk runtime configure --runtime=docker"
    systemctl restart docker < /dev/null || die "Docker no arrancó después de configurar la GPU (mira: systemctl status docker)"
    nvidia_runtime || die "Docker sigue sin el runtime de NVIDIA después de configurarlo"
    ok "Docker usa la GPU (runtime de NVIDIA)"
}

# --- 4. cosign -----------------------------------------------------------------------------------

# Deja en COSIGN un cosign 2.4 o más nuevo. El del sistema si sirve; si no, el oficial fijado por sha256.
ensure_cosign() {
    local v="" want got dir
    if command -v cosign > /dev/null; then
        v="$(cosign version 2> /dev/null < /dev/null | sed -n 's/^GitVersion: *v\{0,1\}\([0-9][0-9.]*\).*/\1/p' | head -n1 || true)"
        if [ -n "$v" ] && version_ge "$v" "$COSIGN_MIN"; then
            COSIGN="$(command -v cosign)"
            ok "cosign $v (del sistema)"
            return 0
        fi
        log "El cosign del sistema (${v:-versión desconocida}) es anterior a $COSIGN_MIN: se usa el oficial $COSIGN_VERSION solo para esta instalación."
    fi
    want="$(cosign_sha256 "$ARCH")"
    dir="$TMP/bin"
    # Si /tmp no deja ejecutar (noexec), se guarda en /usr/local/lib/iris-voz-setup.
    if command -v findmnt > /dev/null && findmnt -no OPTIONS --target "$TMP" 2> /dev/null | tr ',' '\n' | grep -qx noexec; then
        dir=/usr/local/lib/iris-voz-setup
    fi
    mkdir -p "$dir" && chmod 0700 "$dir"
    COSIGN="$dir/cosign"
    if [ -x "$COSIGN" ] && [ "$(sha256sum "$COSIGN" | cut -d' ' -f1)" = "$want" ]; then
        COSIGN_PINNED=true
        ok "cosign $COSIGN_VERSION (sha256 comprobado)"
        return 0
    fi
    log "Bajando cosign $COSIGN_VERSION (sigstore)"
    "${CURL[@]}" -o "$COSIGN.tmp" "$COSIGN_URL/cosign-linux-$ARCH" < /dev/null || die "no se pudo bajar cosign de $COSIGN_URL"
    got="$(sha256sum "$COSIGN.tmp" | cut -d' ' -f1)"
    if [ "$got" != "$want" ]; then
        rm -f "$COSIGN.tmp"
        die "el cosign descargado NO coincide con su sha256 fijado (esperado $want, llegó $got). No se ejecutó nada"
    fi
    chmod 0755 "$COSIGN.tmp"
    mv -f "$COSIGN.tmp" "$COSIGN"
    COSIGN_PINNED=true
    ok "cosign $COSIGN_VERSION (sha256 comprobado)"
}

# --- 5. Versión ----------------------------------------------------------------------------------

# La última voz-vX.Y.Z publicada (las releases de Iris, vX.Y.Z, no cuentan). Sin token.
latest_version() {
    local page body all=""
    for page in 1 2 3 4 5; do
        body="$("${CURL[@]}" -H 'Accept: application/vnd.github+json' "$API_RELEASES?per_page=100&page=$page" < /dev/null)" ||
            die "no se pudo consultar las versiones en GitHub (¿hay salida a internet?). Puedes indicar una con --version X.Y.Z"
        grep -q '"tag_name"' <<< "$body" || break
        all+="$(sed -n 's/.*"tag_name": *"voz-v\([0-9]\{1,\}\.[0-9]\{1,\}\.[0-9]\{1,\}\)".*/\1/p' <<< "$body")"$'\n'
    done
    printf '%s' "$all" | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -n1 || true
}

# --- 6. Instalador firmado -----------------------------------------------------------------------

fetch_installer() { # fetch_installer VERSIÓN
    local f
    for f in install-voz.sh install-voz.sh.sigstore.json; do
        "${CURL[@]}" -o "$TMP/$f" "$RELEASES/voz-v$1/$f" < /dev/null ||
            die "no se pudo bajar $f de la versión $1 (¿existe la release voz-v$1?)"
    done
}

verify_installer() { # verify_installer VERSIÓN
    # Con red: cosign comprueba la firma, el certificado y el registro de transparencia de Sigstore.
    if ! TUF_ROOT="$TMP/tuf" "$COSIGN" verify-blob --new-bundle-format --bundle "$TMP/install-voz.sh.sigstore.json" \
        --certificate-oidc-issuer "$OIDC_ISSUER" --certificate-identity "$(signer_identity "$1")" \
        "$TMP/install-voz.sh" > "$TMP/cosign.log" 2>&1 < /dev/null; then
        sed 's/^/    /' "$TMP/cosign.log" >&2 || true
        die "la firma de install-voz.sh NO es válida para $(signer_identity "$1"). No se ejecutó nada"
    fi
}

# --- Principal -----------------------------------------------------------------------------------

main() {
    VERSION="" YES=false PORT="" LISTEN="" DIR="" NO_MODELS=false ALLOW_SWAP=false ALLOW_CLOCK=false
    COSIGN="" COSIGN_PINNED=false
    while [ $# -gt 0 ]; do
        case "$1" in
            --version) [ $# -ge 2 ] || die "--version necesita un valor (X.Y.Z)"; VERSION="${2#v}"; shift 2 ;;
            -y | --yes) YES=true; shift ;;
            --dir) [ $# -ge 2 ] || die "--dir necesita un valor"; DIR="$2"; shift 2 ;;
            --port) [ $# -ge 2 ] || die "--port necesita un valor"; PORT="$2"; shift 2 ;;
            --listen) [ $# -ge 2 ] || die "--listen necesita un valor"; LISTEN="$2"; shift 2 ;;
            --no-models) NO_MODELS=true; shift ;;
            --allow-swap) ALLOW_SWAP=true; shift ;;
            --allow-unsynced-clock) ALLOW_CLOCK=true; shift ;;
            -h | --help) usage 0 ;;
            *) die "opción desconocida: $1 (mira: setup-voz.sh --help)" ;;
        esac
    done
    VERSION="${VERSION#voz-v}"
    [ -z "$VERSION" ] || is_semver "$VERSION" || die "versión inválida: «$VERSION». Usa el formato X.Y.Z, por ejemplo --version 0.1.1"
    [ -z "$PORT" ] || { [[ "$PORT" =~ ^[0-9]{1,5}$ ]] && [ "$PORT" -ge 1 ] && [ "$PORT" -le 65535 ]; } || die "puerto inválido: $PORT"

    printf '\033[1mInstalación del Servidor de IA de Iris (Iris Voz)\033[0m\n' >&2

    paso 1/6 "Comprobando el servidor"
    check_root
    TMP="$(umask 077 && mktemp -d "${TMPDIR:-/tmp}/iris-voz-setup.XXXXXX")"
    trap 'rm -rf "$TMP"' EXIT
    trap 'exit 130' INT TERM
    detect_os
    detect_arch
    check_clock
    check_swap
    check_port

    paso 2/6 "Comprobando la tarjeta NVIDIA"
    check_gpu

    paso 3/6 "Docker y NVIDIA Container Toolkit"
    ensure_docker
    ensure_nvidia_toolkit

    paso 4/6 "Herramienta de verificación de firmas (cosign)"
    ensure_cosign

    paso 5/6 "Eligiendo la versión"
    if [ -z "$VERSION" ]; then
        VERSION="$(latest_version)"
        is_semver "$VERSION" || die "no se encontró ninguna versión publicada de Iris Voz. Indica una con --version X.Y.Z"
        ok "última versión publicada: $VERSION"
    else
        ok "versión pedida: $VERSION"
    fi

    paso 6/6 "Bajando y verificando el instalador firmado"
    fetch_installer "$VERSION"
    verify_installer "$VERSION"
    ok "firma válida: lo publicó el workflow voz-release.yml en la etiqueta voz-v$VERSION"

    local args=(install --version "$VERSION" --yes)
    [ -z "$DIR" ] || args+=(--dir "$DIR")
    [ -z "$PORT" ] || args+=(--port "$PORT")
    [ -z "$LISTEN" ] || args+=(--listen "$LISTEN")
    ! $NO_MODELS || args+=(--no-models)
    ! $ALLOW_SWAP || args+=(--allow-swap)
    ! $ALLOW_CLOCK || args+=(--allow-unsynced-clock)
    printf '\n\033[1mEjecutando el instalador verificado de Iris Voz %s\033[0m\n' "$VERSION" >&2
    # El cosign fijado se le pasa para que no lo baje otra vez (el instalador vuelve a comprobar su sha256).
    if $COSIGN_PINNED; then
        IRIS_VOZ_COSIGN="$COSIGN" bash "$TMP/install-voz.sh" "${args[@]}" < /dev/null || die "el instalador de Iris Voz falló (los mensajes de arriba dicen por qué). Puedes volver a correr la misma línea"
    else
        bash "$TMP/install-voz.sh" "${args[@]}" < /dev/null || die "el instalador de Iris Voz falló (los mensajes de arriba dicen por qué). Puedes volver a correr la misma línea"
    fi

    cat >&2 << EOF

$(printf '\033[1;32m')Listo: Iris Voz $VERSION está instalado.$(printf '\033[0m')

Falta enlazarlo con Iris (docs/runbooks/servidor-ia.md §3):
  1. Panel de Iris → Administración → Integraciones → Servidor de IA → «Enlazar servidor de IA»,
     con la dirección https://<este servidor>:${PORT:-8443}. El panel muestra un código de un solo uso.
  2. Aquí:  sudo iris-voz pair   y escribe ese código (no se ve al teclear).
  3. En el panel: «Conectar», compara la huella con la que se imprimió arriba y, si coincide, «Activar».
EOF
}

# Solo corre si se ejecuta (también con `curl | bash`); las pruebas lo cargan con «.».
if ! (return 0 2> /dev/null); then
    main "$@"
fi
