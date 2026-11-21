#!/usr/bin/env bash
# Tests setup-voz.sh without a GPU, Docker, network or root: every outside command (curl, cosign,
# docker, nvidia-smi, apt-get, systemctl, id, ...) is a fake in a private PATH that only keeps the
# basic tools. The fake curl serves files from a local folder, the fake install-voz.sh only writes
# down its arguments, and each case checks the exit code, the message and what was (not) run.
#
#   infra/voz/prueba-setup.sh
#
# Used by CI (voz.yml, job «archivos»). The real download of cosign and the real signature check
# against a published release are exercised by hand (docs/runbooks/servidor-ia.md §2).
set -euo pipefail

aqui="$(cd "$(dirname "$0")" && pwd)"
setup="$aqui/setup-voz.sh"
raiz="$(mktemp -d)"
trap 'rm -rf "$raiz"' EXIT

falsos="$raiz/falsos" basicos="$raiz/basicos" srv="$raiz/srv" registro="$raiz/registro"
mkdir -p "$falsos" "$basicos" "$srv" "$registro"

# Only these real tools are visible: no real docker, cosign, curl, nvidia-smi or package manager.
for t in bash sh env sed grep awk sort head tail cut tr mktemp rm mkdir cat chmod install sha256sum cmp cp mv \
    dirname basename seq sleep date wc tee gpg findmnt ls; do
    p="$(command -v "$t" || true)"
    [ -n "$p" ] && ln -s "$p" "$basicos/$t"
done

falso() { # falso NAME <<< body: a fake command that runs the body
    { echo '#!/usr/bin/env bash'; cat; } > "$falsos/$1"
    chmod +x "$falsos/$1"
}

falso id << 'EOF'
[ "${1:-}" = -u ] && { echo "${FALSO_UID:-0}"; exit 0; }
echo "uid=${FALSO_UID:-0}"
EOF
falso uname << 'EOF'
[ "${1:-}" = -m ] && { echo "${FALSO_ARCH:-x86_64}"; exit 0; }
echo Linux
EOF
falso timedatectl << 'EOF'
case "$1" in show) echo "${FALSO_NTP:-yes}" ;; *) echo "timedatectl $*" >> "$REG/sistema" ;; esac
EOF
falso swapon << 'EOF'
[ -n "${FALSO_SWAP:-}" ] && echo "$FALSO_SWAP"
exit 0
EOF
falso ss << 'EOF'
[ -n "${FALSO_SS:-}" ] && echo "$FALSO_SS"
exit 0
EOF
for c in systemctl apt-get dnf rpm swapoff nvidia-ctk; do
    falso "$c" <<< "echo \"$c \$*\" >> \"\$REG/sistema\""
done
falso dpkg-query <<< 'exit 1'
falso dpkg <<< 'echo amd64'
# curl: serves $SRV; logs every URL.
falso curl << 'EOF'
out="" url=""
while [ $# -gt 0 ]; do
    case "$1" in
        -o) out="$2"; shift 2 ;;
        -H | --proto | --retry) shift 2 ;;
        https://*) url="$1"; shift ;;
        *) shift ;;
    esac
done
echo "$url" >> "$REG/curl"
case "$url" in
    https://api.github.com/repos/HCDestroyer/iris/releases\?*page=1) f="$SRV/releases.json" ;;
    https://api.github.com/repos/HCDestroyer/iris/releases\?*) echo '[]'; exit 0 ;;
    https://github.com/HCDestroyer/iris/releases/download/*) f="$SRV/releases/${url#https://github.com/HCDestroyer/iris/releases/download/}" ;;
    https://github.com/sigstore/cosign/releases/download/*) f="$SRV/cosign" ;;
    https://download.docker.com/*/gpg | https://nvidia.github.io/*/gpgkey) f="$SRV/llave.asc" ;;
    *) exit 22 ;;
esac
[ -f "$f" ] || exit 22
if [ -n "$out" ]; then cp "$f" "$out"; else cat "$f"; fi
EOF
# cosign: FALSO_COSIGN_VERSION (version), FALSO_FIRMA=ok|mal (verify-blob).
falso cosign.real << 'EOF'
case "$1" in
    version) echo "GitVersion:    v${FALSO_COSIGN_VERSION:-2.6.1}" ;;
    verify-blob) echo "cosign $*" >> "$REG/cosign"; [ "${FALSO_FIRMA:-ok}" = ok ] || { echo "Error: invalid signature" >&2; exit 1; } ;;
esac
EOF
falso nvidia-smi.real <<< 'echo "GPU 0: NVIDIA L4 (UUID: GPU-falsa)"'
falso docker.real << 'EOF'
case "$1 ${2:-}" in
    "compose version") echo "${FALSO_COMPOSE:-2.29.1}" ;;
    "info --format") echo '{"nvidia":{"path":"nvidia-container-runtime"},"runc":{"path":"runc"}}' ;;
    "info "*) exit 0 ;;
    "ps "*) [ -n "${FALSO_DOCKER_PS:-}" ] && echo "$FALSO_DOCKER_PS"; exit 0 ;;
    "version --format") echo 27.3.1 ;;
esac
EOF

# Release assets: a fake install-voz.sh that only writes down its arguments.
for v in 0.1.0 0.1.1; do
    mkdir -p "$srv/releases/voz-v$v"
    # shellcheck disable=SC2016 # expanded when the fake installer runs
    printf '#!/usr/bin/env bash\necho "install-voz.sh $*" >> "$REG/instalador"\n' > "$srv/releases/voz-v$v/install-voz.sh"
    echo '{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json"}' > "$srv/releases/voz-v$v/install-voz.sh.sigstore.json"
done
cat > "$srv/releases.json" << 'EOF'
[
  { "tag_name": "v0.34.0", "name": "Iris 0.34.0" },
  { "tag_name": "voz-v0.1.1", "name": "Iris Voz 0.1.1" },
  { "tag_name": "v9.9.9", "name": "Iris 9.9.9" },
  { "tag_name": "voz-v0.1.0", "name": "Iris Voz 0.1.0" },
  { "tag_name": "voz-vx.y.z", "name": "not a version" }
]
EOF
head -c 4096 /dev/zero > "$srv/cosign" # a cosign whose sha256 is not the pinned one
# A real GPG key that is not Docker's.
export GNUPGHOME="$raiz/gnupg"
mkdir -p "$GNUPGHOME" && chmod 700 "$GNUPGHOME"
gpg --batch -q --passphrase '' --quick-gen-key 'Llave falsa <falsa@example.invalid>' ed25519 sign never 2> /dev/null
gpg --batch -q --armor --export > "$srv/llave.asc"
unset GNUPGHOME

ubuntu="$raiz/os-ubuntu" rocky="$raiz/os-rocky" arch="$raiz/os-arch"
printf 'ID=ubuntu\nVERSION_ID="24.04"\nVERSION_CODENAME=noble\nUBUNTU_CODENAME=noble\nPRETTY_NAME="Ubuntu 24.04 LTS"\n' > "$ubuntu"
printf 'ID="rocky"\nID_LIKE="rhel centos fedora"\nVERSION_ID="9.4"\nPRETTY_NAME="Rocky Linux 9.4"\n' > "$rocky"
printf 'ID=arch\nPRETTY_NAME="Arch Linux"\n' > "$arch"
echo n > "$raiz/tty-no"

fallos=0
# caso NAME EXPECTED_RC MESSAGE [VAR=value ...] -- [setup args]: runs setup-voz.sh in a clean state.
caso() {
    local nombre="$1" rc_esperado="$2" mensaje="$3" rc=0 vars=()
    shift 3
    while [ $# -gt 0 ] && [ "$1" != -- ]; do vars+=("$1"); shift; done
    [ "${1:-}" = -- ] && shift
    rm -rf "$registro" && mkdir -p "$registro"
    rm -f "$falsos/cosign" "$falsos/docker" "$falsos/nvidia-smi"
    cp "$falsos/cosign.real" "$falsos/cosign"
    cp "$falsos/docker.real" "$falsos/docker"
    cp "$falsos/nvidia-smi.real" "$falsos/nvidia-smi"
    local v tuberia=false
    for v in "${vars[@]}"; do
        case "$v" in
            SIN=*) rm -f "$falsos/${v#SIN=}" ;;
            TUBERIA=1) tuberia=true ;;
        esac
    done
    local entorno=(env -i HOME="$raiz" PATH="$falsos:$basicos" REG="$registro" SRV="$srv" TMPDIR="$raiz"
        IRIS_VOZ_OS_RELEASE="$ubuntu" IRIS_VOZ_TTY="$raiz/tty-no" "${vars[@]/#SIN=*/_=}")
    if $tuberia; then # like `curl -fsSL .../setup-voz.sh | sudo bash -s -- ARGS`
        "${entorno[@]}" bash -s -- "$@" < "$setup" > "$registro/salida" 2>&1 || rc=$?
    else
        "${entorno[@]}" bash "$setup" "$@" > "$registro/salida" 2>&1 < /dev/null || rc=$?
    fi
    if { [ "$rc_esperado" = 0 ] && [ "$rc" != 0 ]; } || { [ "$rc_esperado" != 0 ] && [ "$rc" = 0 ]; }; then
        echo "FALLA: $nombre (salida $rc, se esperaba $rc_esperado)"; sed 's/^/    /' "$registro/salida"; fallos=$((fallos + 1)); return 0
    fi
    if [ -n "$mensaje" ] && ! grep -qF -- "$mensaje" "$registro/salida"; then
        echo "FALLA: $nombre (falta el mensaje «$mensaje»)"; sed 's/^/    /' "$registro/salida"; fallos=$((fallos + 1)); return 0
    fi
    echo "ok: $nombre"
}
# afirmar DESCRIPTION COMMAND...: checks the state left by the last case.
afirmar() {
    local d="$1"
    shift
    if "$@"; then return 0; fi
    echo "FALLA: $d"; sed 's/^/    /' "$registro/salida"; fallos=$((fallos + 1))
}
no_instalo() { [ ! -s "$registro/instalador" ]; }
instalo() { grep -qxF "install-voz.sh $1" "$registro/instalador"; }
sin_paquetes() { ! grep -qE '^(apt-get|dnf) .*install' "$registro/sistema" 2> /dev/null; }
sin_docker() { ! grep -q 'docker-ce' "$registro/sistema" 2> /dev/null; }

caso "todo bien: la última voz-v*, firma verificada con su identidad" 0 "Listo: Iris Voz 0.1.1 está instalado"
afirmar "llama a install-voz.sh con la última versión" instalo "install --version 0.1.1 --yes"
afirmar "verifica la firma con la identidad de voz-v0.1.1" \
    grep -qF -- "--certificate-identity https://github.com/HCDestroyer/iris/.github/workflows/voz-release.yml@refs/tags/voz-v0.1.1" "$registro/cosign"
afirmar "verifica con el emisor de GitHub Actions" \
    grep -qF -- "--certificate-oidc-issuer https://token.actions.githubusercontent.com" "$registro/cosign"
afirmar "baja de la release voz-v0.1.1" grep -qxF "https://github.com/HCDestroyer/iris/releases/download/voz-v0.1.1/install-voz.sh" "$registro/curl"
afirmar "no instala paquetes si ya está todo" sin_paquetes

caso "otra vez (idempotente), con el puerto ya usado por Iris Voz" 0 "ya lo usa Iris Voz" \
    'FALSO_SS=LISTEN 0 4096 0.0.0.0:8443 0.0.0.0:*' 'FALSO_DOCKER_PS=0.0.0.0:8443->8443/tcp' --
afirmar "llama otra vez al instalador" instalo "install --version 0.1.1 --yes"

caso "--version y opciones se pasan al instalador" 0 "versión pedida: 0.1.0" -- \
    --version v0.1.0 --port 9443 --dir /srv/voz --no-models --allow-swap --allow-unsynced-clock
afirmar "pasa la versión y las opciones" \
    instalo "install --version 0.1.0 --yes --dir /srv/voz --port 9443 --no-models --allow-swap --allow-unsynced-clock"
afirmar "verifica con la identidad de voz-v0.1.0" grep -qF "refs/tags/voz-v0.1.0 " "$registro/cosign"

caso "Rocky Linux con todo instalado" 0 "Rocky Linux 9.4" "IRIS_VOZ_OS_RELEASE=$rocky" --
afirmar "llama al instalador en Rocky" instalo "install --version 0.1.1 --yes"

caso "por tubería (curl | sudo bash -s -- --version 0.1.0)" 0 "Listo: Iris Voz 0.1.0 está instalado" TUBERIA=1 -- --version 0.1.0
afirmar "por tubería llama al instalador" instalo "install --version 0.1.0 --yes"

caso "versión inválida" 1 "versión inválida" -- --version 1.2
afirmar "no baja ni ejecuta nada con una versión inválida" no_instalo
afirmar "no consulta la red con una versión inválida" test ! -s "$registro/curl"
caso "versión que no existe" 1 "voz-v7.7.7" -- --version 7.7.7
afirmar "no ejecuta nada si la versión no existe" no_instalo

caso "sin root" 1 "como administrador" FALSO_UID=1000 --
afirmar "sin root no hace nada" no_instalo
caso "distribución no soportada" 1 "Solo se admite Ubuntu, Debian, RHEL" "IRIS_VOZ_OS_RELEASE=$arch" --
caso "arquitectura no soportada" 1 "no está soportada" FALSO_ARCH=riscv64 --
caso "puerto ocupado por otro programa" 1 "ya lo usa otro programa" 'FALSO_SS=LISTEN 0 4096 0.0.0.0:8443 0.0.0.0:*' --
afirmar "con el puerto ocupado no ejecuta nada" no_instalo
caso "reloj sin sincronizar y dice que no" 1 "sin el reloj sincronizado" FALSO_NTP=no --
caso "intercambio a disco y dice que no" 1 "con intercambio a disco no se instala" 'FALSO_SWAP=/swap.img file 2G 0B -2' --
afirmar "no apaga el intercambio si dice que no" test ! -s "$registro/sistema"

caso "sin GPU: explica cómo instalar el controlador" 1 "sudo ubuntu-drivers install" SIN=nvidia-smi --
afirmar "sin GPU no instala nada" sin_paquetes
afirmar "sin GPU no ejecuta el instalador" no_instalo

caso "sin Docker y el usuario dice que no" 1 "sin Docker no se puede instalar" SIN=docker --
afirmar "si dice que no, no instala paquetes" sin_paquetes
afirmar "si dice que no, no ejecuta el instalador" no_instalo
caso "sin Docker y sin terminal para preguntar" 1 "Vuelve a correrlo con --yes" SIN=docker "IRIS_VOZ_TTY=$raiz/no-existe" --
caso "sin Docker, --yes y una llave GPG que no es la oficial" 1 "no es la oficial" SIN=docker -- --yes
afirmar "con la llave equivocada no instala Docker" sin_docker

caso "cosign viejo y el descargado no coincide con su sha256" 1 "NO coincide con su sha256 fijado" FALSO_COSIGN_VERSION=2.2.0 --
afirmar "con cosign alterado no ejecuta el instalador" no_instalo
afirmar "baja cosign de la release oficial fijada" \
    grep -qxF "https://github.com/sigstore/cosign/releases/download/v2.6.1/cosign-linux-amd64" "$registro/curl"
caso "sin cosign y el descargado no coincide con su sha256" 1 "NO coincide con su sha256 fijado" SIN=cosign --
afirmar "sin cosign válido no ejecuta el instalador" no_instalo

caso "firma inválida" 1 "NO es válida" FALSO_FIRMA=mal --
afirmar "con firma inválida NO ejecuta el instalador" no_instalo

caso "opción desconocida" 1 "opción desconocida" -- --borrar-todo
caso "ayuda" 0 "Instalación de un paso del Servidor de IA" -- --help

[ "$fallos" = 0 ] || { echo "$fallos caso(s) fallaron" >&2; exit 1; }
echo "setup-voz.sh: todas las pruebas pasaron"
