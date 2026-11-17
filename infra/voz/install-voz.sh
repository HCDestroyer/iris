#!/usr/bin/env bash
# Instalador y gestor de Iris Voz: el servidor de IA dedicado de Iris (docs/runbooks/servidor-ia.md).
#
# Verifica este script ANTES de ejecutarlo (cosign 2.4 o más nuevo, de una fuente tuya):
#
#   cosign verify-blob --new-bundle-format --bundle install-voz.sh.sigstore.json \
#     --certificate-oidc-issuer https://token.actions.githubusercontent.com \
#     --certificate-identity https://github.com/HCDestroyer/iris/.github/workflows/voz-release.yml@refs/tags/voz-vX.Y.Z \
#     install-voz.sh && sudo bash install-voz.sh
#
# Nunca construye nada. Baja el paquete de la versión, verifica su firma de Sigstore SIN RED (bundle
# y raíz de confianza embebida en este script; la prueba del registro de transparencia es
# obligatoria) y quién lo firmó (el workflow voz-release de este repositorio en una etiqueta
# voz-vX.Y.Z), y falla cerrado. Después escribe <carpeta>/.env, baja las imágenes (fijadas por
# digest), arranca el servidor y muestra la huella del certificado para compararla en el panel de
# Iris. Tras instalar, este mismo script queda como `iris-voz`.
#
# Uso: install-voz.sh [orden] [opciones]
#
# Órdenes:
#   install             instala o reconfigura (por defecto)
#   upgrade             pasa a una versión más nueva (--version X.Y.Z o la última); nunca baja
#   pair                abre la ventana de enlace; pide el código de un solo uso del panel de Iris
#                       (no se ve al escribirlo; el código nunca se acepta como argumento)
#   unpair              olvida a Iris: no se acepta nada hasta un enlace nuevo
#   status              versión, contenedores, huella, IP de Iris observada y modelos permitidos
#   renew-cert          certificado nuevo, que se anuncia a Iris por el canal vigente
#                       (--no-channel: el anterior se perdió; desenlaza y hay que enlazar con código)
#   ip restrict|release      aceptar el puerto de control solo desde la IP de Iris vista al enlazar
#                            (apagado de fábrica)
#   model pull NOMBRE        baja un modelo (contenedor de un solo uso; solo llega al registro de modelos)
#   model remove NOMBRE      borra un modelo
#   model allow NOMBRE       deja que Iris use un modelo instalado   (model revoke NOMBRE: lo contrario)
#   model list               modelos instalados y permitidos
#   logs [servicio]          sigue los registros (puerta, llm, medidor)
#   stop | start             para o arranca todos los contenedores
#   compose ARGS             ejecuta docker compose sobre la instalación
#   version                  muestra la versión instalada
#   help                     esta ayuda
#
# Opciones de install:
#   --version X.Y.Z     versión a instalar (por defecto, la última voz-v*)
#   --dir RUTA          carpeta de instalación (por defecto /opt/iris-voz)
#   --port N            puerto de control (por defecto 8443)
#   --listen DIRECCION  dirección en la que escucha el puerto de control (por defecto 0.0.0.0)
#   --no-gpu            sin GPU (solo pruebas: en CPU es demasiado lento para uso real)
#   --no-models         no bajar los modelos por defecto
#   --allow-swap                no detenerse si hay intercambio a disco sin cifrar
#   --allow-unsynced-clock      no detenerse si el reloj no está sincronizado por NTP
#   -y, --yes           no preguntar nunca
#
# Entorno (pruebas y servidores sin internet): IRIS_VOZ_DIR, IRIS_VOZ_REPO (HCDestroyer/iris),
# IRIS_VOZ_SOURCE (carpeta local con iris-voz-<v>.tar.gz, su .sigstore.json y un archivo `latest`,
# en lugar de GitHub; la firma se verifica igual), IRIS_VOZ_COSIGN (ruta de un binario de cosign que
# ya está en el servidor; su sha256 debe ser el fijado), IRIS_VOZ_SKIP_PULL=1 (imágenes ya cargadas).
set -Eeuo pipefail

REPO="${IRIS_VOZ_REPO:-HCDestroyer/iris}"
SOURCE="${IRIS_VOZ_SOURCE:-}"
DIR="${IRIS_VOZ_DIR:-/opt/iris-voz}"
COMPOSE_MIN=2.24
KEEP_RELEASES=3
CURL=(curl -fsSL --proto '=https' --tlsv1.2 --retry 3)

# Quién debe haber firmado el paquete: el workflow de release de este repositorio, en una etiqueta
# protegida voz-vX.Y.Z, nunca en una rama. No es configurable.
OIDC_ISSUER=https://token.actions.githubusercontent.com
signer_identity() { printf 'https://github.com/HCDestroyer/iris/.github/workflows/voz-release.yml@refs/tags/voz-v%s' "$1"; }

# cosign, fijado por sha256 (binarios oficiales).
COSIGN_VERSION=v2.6.1
cosign_sha256() {
    case "$1" in
        amd64) echo 064954c5d8c7e3b28188eee5b1727b31c411550bc5fefd41aa672d3c761d103a ;;
        arm64) echo 56a16480bdd56ec789abaa65924402f6b92c0041f06885995853c05567b76f34 ;;
    esac
}
COSIGN_URL="https://github.com/sigstore/cosign/releases/download/$COSIGN_VERSION"

log() { printf '\033[1m==>\033[0m %s\n' "$*" >&2; }
warn() { printf '\033[33maviso:\033[0m %s\n' "$*" >&2; }
die() { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }
usage() { sed -n '2,/^set -E/{/^#/p}' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit "${1:-0}"; }

version_ge() { [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" = "$2" ]; }
is_semver() { [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; }
arch() {
    case "$(uname -m)" in
        x86_64 | amd64) echo amd64 ;;
        aarch64 | arm64) echo arm64 ;;
        *) die "arquitectura no soportada: $(uname -m)" ;;
    esac
}

# La raíz de confianza de Sigstore (claves públicas de Fulcio, Rekor, el registro de certificados y
# la autoridad de sellado de tiempo) viaja dentro de este script: verificar no necesita red. Si
# Sigstore rota sus claves y esta copia ya no valida un paquete más nuevo, la verificación falla
# cerrado: se instala con un instalador más nuevo. Idéntica a infra/voz/sigstore/trusted_root.json
# (lo comprueba infra/voz/verificar.sh).
write_trusted_root() {
    cat > "$1" << 'IRIS_VOZ_TRUSTED_ROOT'
{
  "mediaType": "application/vnd.dev.sigstore.trustedroot+json;version=0.1",
  "tlogs": [
    {
      "baseUrl": "https://rekor.sigstore.dev",
      "hashAlgorithm": "SHA2_256",
      "publicKey": {
        "rawBytes": "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE2G2Y+2tabdTV5BcGiBIx0a9fAFwrkBbmLSGtks4L3qX6yYY0zufBnhC8Ur/iy55GhWP/9A/bY2LhC30M9+RYtw==",
        "keyDetails": "PKIX_ECDSA_P256_SHA_256",
        "validFor": {
          "start": "2021-01-12T11:53:27Z"
        }
      },
      "logId": {
        "keyId": "wNI9atQGlz+VWfO6LRygH4QUfY/8W4RFwiT5i5WRgB0="
      }
    },
    {
      "baseUrl": "https://log2025-1.rekor.sigstore.dev",
      "hashAlgorithm": "SHA2_256",
      "publicKey": {
        "rawBytes": "MCowBQYDK2VwAyEAt8rlp1knGwjfbcXAYPYAkn0XiLz1x8O4t0YkEhie244=",
        "keyDetails": "PKIX_ED25519",
        "validFor": {
          "start": "2025-09-23T00:00:00Z"
        }
      },
      "logId": {
        "keyId": "zxGZFVvd0FEmjR8WrFwMdcAJ9vtaY/QXf44Y1wUeP6A="
      }
    }
  ],
  "certificateAuthorities": [
    {
      "subject": {
        "organization": "sigstore.dev",
        "commonName": "sigstore"
      },
      "uri": "https://fulcio.sigstore.dev",
      "certChain": {
        "certificates": [
          {
            "rawBytes": "MIIB+DCCAX6gAwIBAgITNVkDZoCiofPDsy7dfm6geLbuhzAKBggqhkjOPQQDAzAqMRUwEwYDVQQKEwxzaWdzdG9yZS5kZXYxETAPBgNVBAMTCHNpZ3N0b3JlMB4XDTIxMDMwNzAzMjAyOVoXDTMxMDIyMzAzMjAyOVowKjEVMBMGA1UEChMMc2lnc3RvcmUuZGV2MREwDwYDVQQDEwhzaWdzdG9yZTB2MBAGByqGSM49AgEGBSuBBAAiA2IABLSyA7Ii5k+pNO8ZEWY0ylemWDowOkNa3kL+GZE5Z5GWehL9/A9bRNA3RbrsZ5i0JcastaRL7Sp5fp/jD5dxqc/UdTVnlvS16an+2Yfswe/QuLolRUCrcOE2+2iA5+tzd6NmMGQwDgYDVR0PAQH/BAQDAgEGMBIGA1UdEwEB/wQIMAYBAf8CAQEwHQYDVR0OBBYEFMjFHQBBmiQpMlEk6w2uSu1KBtPsMB8GA1UdIwQYMBaAFMjFHQBBmiQpMlEk6w2uSu1KBtPsMAoGCCqGSM49BAMDA2gAMGUCMH8liWJfMui6vXXBhjDgY4MwslmN/TJxVe/83WrFomwmNf056y1X48F9c4m3a3ozXAIxAKjRay5/aj/jsKKGIkmQatjI8uupHr/+CxFvaJWmpYqNkLDGRU+9orzh5hI2RrcuaQ=="
          }
        ]
      },
      "validFor": {
        "start": "2021-03-07T03:20:29Z",
        "end": "2022-12-31T23:59:59.999Z"
      }
    },
    {
      "subject": {
        "organization": "sigstore.dev",
        "commonName": "sigstore"
      },
      "uri": "https://fulcio.sigstore.dev",
      "certChain": {
        "certificates": [
          {
            "rawBytes": "MIICGjCCAaGgAwIBAgIUALnViVfnU0brJasmRkHrn/UnfaQwCgYIKoZIzj0EAwMwKjEVMBMGA1UEChMMc2lnc3RvcmUuZGV2MREwDwYDVQQDEwhzaWdzdG9yZTAeFw0yMjA0MTMyMDA2MTVaFw0zMTEwMDUxMzU2NThaMDcxFTATBgNVBAoTDHNpZ3N0b3JlLmRldjEeMBwGA1UEAxMVc2lnc3RvcmUtaW50ZXJtZWRpYXRlMHYwEAYHKoZIzj0CAQYFK4EEACIDYgAE8RVS/ysH+NOvuDZyPIZtilgUF9NlarYpAd9HP1vBBH1U5CV77LSS7s0ZiH4nE7Hv7ptS6LvvR/STk798LVgMzLlJ4HeIfF3tHSaexLcYpSASr1kS0N/RgBJz/9jWCiXno3sweTAOBgNVHQ8BAf8EBAMCAQYwEwYDVR0lBAwwCgYIKwYBBQUHAwMwEgYDVR0TAQH/BAgwBgEB/wIBADAdBgNVHQ4EFgQU39Ppz1YkEZb5qNjpKFWixi4YZD8wHwYDVR0jBBgwFoAUWMAeX5FFpWapesyQoZMi0CrFxfowCgYIKoZIzj0EAwMDZwAwZAIwPCsQK4DYiZYDPIaDi5HFKnfxXx6ASSVmERfsynYBiX2X6SJRnZU84/9DZdnFvvxmAjBOt6QpBlc4J/0DxvkTCqpclvziL6BCCPnjdlIB3Pu3BxsPmygUY7Ii2zbdCdliiow="
          },
          {
            "rawBytes": "MIIB9zCCAXygAwIBAgIUALZNAPFdxHPwjeDloDwyYChAO/4wCgYIKoZIzj0EAwMwKjEVMBMGA1UEChMMc2lnc3RvcmUuZGV2MREwDwYDVQQDEwhzaWdzdG9yZTAeFw0yMTEwMDcxMzU2NTlaFw0zMTEwMDUxMzU2NThaMCoxFTATBgNVBAoTDHNpZ3N0b3JlLmRldjERMA8GA1UEAxMIc2lnc3RvcmUwdjAQBgcqhkjOPQIBBgUrgQQAIgNiAAT7XeFT4rb3PQGwS4IajtLk3/OlnpgangaBclYpsYBr5i+4ynB07ceb3LP0OIOZdxexX69c5iVuyJRQ+Hz05yi+UF3uBWAlHpiS5sh0+H2GHE7SXrk1EC5m1Tr19L9gg92jYzBhMA4GA1UdDwEB/wQEAwIBBjAPBgNVHRMBAf8EBTADAQH/MB0GA1UdDgQWBBRYwB5fkUWlZql6zJChkyLQKsXF+jAfBgNVHSMEGDAWgBRYwB5fkUWlZql6zJChkyLQKsXF+jAKBggqhkjOPQQDAwNpADBmAjEAj1nHeXZp+13NWBNa+EDsDP8G1WWg1tCMWP/WHPqpaVo0jhsweNFZgSs0eE7wYI4qAjEA2WB9ot98sIkoF3vZYdd3/VtWB5b9TNMea7Ix/stJ5TfcLLeABLE4BNJOsQ4vnBHJ"
          }
        ]
      },
      "validFor": {
        "start": "2022-04-13T20:06:15Z"
      }
    }
  ],
  "ctlogs": [
    {
      "baseUrl": "https://ctfe.sigstore.dev/test",
      "hashAlgorithm": "SHA2_256",
      "publicKey": {
        "rawBytes": "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEbfwR+RJudXscgRBRpKX1XFDy3PyudDxz/SfnRi1fT8ekpfBd2O1uoz7jr3Z8nKzxA69EUQ+eFCFI3zeubPWU7w==",
        "keyDetails": "PKIX_ECDSA_P256_SHA_256",
        "validFor": {
          "start": "2021-03-14T00:00:00Z",
          "end": "2022-10-31T23:59:59.999Z"
        }
      },
      "logId": {
        "keyId": "CGCS8ChS/2hF0dFrJ4ScRWcYrBY9wzjSbea8IgY2b3I="
      }
    },
    {
      "baseUrl": "https://ctfe.sigstore.dev/2022",
      "hashAlgorithm": "SHA2_256",
      "publicKey": {
        "rawBytes": "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEiPSlFi0CmFTfEjCUqF9HuCEcYXNKAaYalIJmBZ8yyezPjTqhxrKBpMnaocVtLJBI1eM3uXnQzQGAJdJ4gs9Fyw==",
        "keyDetails": "PKIX_ECDSA_P256_SHA_256",
        "validFor": {
          "start": "2022-10-20T00:00:00Z"
        }
      },
      "logId": {
        "keyId": "3T0wasbHETJjGR4cmWc3AqJKXrjePK3/h4pygC8p7o4="
      }
    }
  ],
  "timestampAuthorities": [
    {
      "subject": {
        "organization": "sigstore.dev",
        "commonName": "sigstore-tsa-selfsigned"
      },
      "uri": "https://timestamp.sigstore.dev/api/v1/timestamp",
      "certChain": {
        "certificates": [
          {
            "rawBytes": "MIICEDCCAZagAwIBAgIUOhNULwyQYe68wUMvy4qOiyojiwwwCgYIKoZIzj0EAwMwOTEVMBMGA1UEChMMc2lnc3RvcmUuZGV2MSAwHgYDVQQDExdzaWdzdG9yZS10c2Etc2VsZnNpZ25lZDAeFw0yNTA0MDgwNjU5NDNaFw0zNTA0MDYwNjU5NDNaMC4xFTATBgNVBAoTDHNpZ3N0b3JlLmRldjEVMBMGA1UEAxMMc2lnc3RvcmUtdHNhMHYwEAYHKoZIzj0CAQYFK4EEACIDYgAE4ra2Z8hKNig2T9kFjCAToGG30jky+WQv3BzL+mKvh1SKNR/UwuwsfNCg4sryoYAd8E6isovVA3M4aoNdm9QDi50Z8nTEyvqgfDPtTIwXItfiW/AFf1V7uwkbkAoj0xxco2owaDAOBgNVHQ8BAf8EBAMCB4AwHQYDVR0OBBYEFIn9eUOHz9BlRsMCRscsc1t9tOsDMB8GA1UdIwQYMBaAFJjsAe9/u1H/1JUeb4qImFMHic6/MBYGA1UdJQEB/wQMMAoGCCsGAQUFBwMIMAoGCCqGSM49BAMDA2gAMGUCMDtpsV/6KaO0qyF/UMsX2aSUXKQFdoGTptQGc0ftq1csulHPGG6dsmyMNd3JB+G3EQIxAOajvBcjpJmKb4Nv+2Taoj8Uc5+b6ih6FXCCKraSqupe07zqswMcXJTe1cExvHvvlw=="
          },
          {
            "rawBytes": "MIIB9zCCAXygAwIBAgIUV7f0GLDOoEzIh8LXSW80OJiUp14wCgYIKoZIzj0EAwMwOTEVMBMGA1UEChMMc2lnc3RvcmUuZGV2MSAwHgYDVQQDExdzaWdzdG9yZS10c2Etc2VsZnNpZ25lZDAeFw0yNTA0MDgwNjU5NDNaFw0zNTA0MDYwNjU5NDNaMDkxFTATBgNVBAoTDHNpZ3N0b3JlLmRldjEgMB4GA1UEAxMXc2lnc3RvcmUtdHNhLXNlbGZzaWduZWQwdjAQBgcqhkjOPQIBBgUrgQQAIgNiAAQUQNtfRT/ou3YATa6wB/kKTe70cfJwyRIBovMnt8RcJph/COE82uyS6FmppLLL1VBPGcPfpQPYJNXzWwi8icwhKQ6W/Qe2h3oebBb2FHpwNJDqo+TMaC/tdfkv/ElJB72jRTBDMA4GA1UdDwEB/wQEAwIBBjASBgNVHRMBAf8ECDAGAQH/AgEAMB0GA1UdDgQWBBSY7AHvf7tR/9SVHm+KiJhTB4nOvzAKBggqhkjOPQQDAwNpADBmAjEAwGEGrfGZR1cen1R8/DTVMI943LssZmJRtDp/i7SfGHmGRP6gRbuj9vOK3b67Z0QQAjEAuT2H673LQEaHTcyQSZrkp4mX7WwkmF+sVbkYY5mXN+RMH13KUEHHOqASaemYWK/E"
          }
        ]
      },
      "validFor": {
        "start": "2025-07-04T00:00:00Z"
      }
    }
  ]
}
IRIS_VOZ_TRUSTED_ROOT
}

# ensure_cosign CARPETA → ruta de un cosign cuyo sha256 es el fijado.
ensure_cosign() {
    local dest="$1/cosign" want a
    a="$(arch)"
    want="$(cosign_sha256 "$a")"
    if [ -x "$dest" ] && [ "$(sha256sum "$dest" | cut -d' ' -f1)" = "$want" ]; then
        printf '%s' "$dest"
        return 0
    fi
    mkdir -p "$1"
    if [ -n "${IRIS_VOZ_COSIGN:-}" ]; then
        install -m 0755 "$IRIS_VOZ_COSIGN" "$dest.tmp"
    else
        log "Bajando cosign $COSIGN_VERSION"
        "${CURL[@]}" -o "$dest.tmp" "$COSIGN_URL/cosign-linux-$a" < /dev/null || die "no se pudo bajar cosign"
        chmod 0755 "$dest.tmp"
    fi
    if [ "$(sha256sum "$dest.tmp" | cut -d' ' -f1)" != "$want" ]; then
        rm -f "$dest.tmp"
        die "cosign no coincide con su sha256 fijado ($want)"
    fi
    mv -f "$dest.tmp" "$dest"
    printf '%s' "$dest"
}

# verify_signature COSIGN ARCHIVO BUNDLE IDENTIDAD: sin red, con la raíz de confianza embebida. La
# prueba de inclusión en el registro de transparencia que trae el bundle se comprueba siempre.
verify_signature() {
    local cosign="$1" file="$2" bundle="$3" identity="$4" root
    [ -s "$file" ] || die "falta el archivo a verificar: $file"
    [ -s "$bundle" ] || die "falta el bundle de la firma: $bundle (nunca se instala un paquete sin firmar)"
    root="$(mktemp)"
    write_trusted_root "$root"
    if ! "$cosign" verify-blob --new-bundle-format --offline --trusted-root "$root" --bundle "$bundle" \
        --certificate-oidc-issuer "$OIDC_ISSUER" --certificate-identity "$identity" "$file" > /dev/null 2>&1 < /dev/null; then
        rm -f "$root"
        return 1
    fi
    rm -f "$root"
}

# manifest_get ARCHIVO CLAVE → valor de texto de una clave de primer nivel de manifiesto.json.
manifest_get() { sed -n "s/^ *\"$2\": *\"\([^\"]*\)\".*/\1/p" "$1" | head -n1; }
# manifest_models ARCHIVO → «nombre digest» por cada modelo por defecto.
manifest_models() { sed -n 's/.*"nombre": *"\([^"]*\)", *"digest": *"\([^"]*\)".*/\1 \2/p' "$1"; }

env_get() {
    local line
    [ -f "$DIR/.env" ] || return 0
    line="$(grep -E "^$1=" "$DIR/.env" | tail -n 1 || true)"
    printf '%s' "${line#*=}"
}
env_set() {
    local tmp
    tmp="$(mktemp "$DIR/.env.XXXXXX")"
    chmod 600 "$tmp"
    if [ -f "$DIR/.env" ]; then grep -vE "^$1=" "$DIR/.env" > "$tmp" || true; fi
    printf '%s=%s\n' "$1" "$2" >> "$tmp"
    mv -f "$tmp" "$DIR/.env"
}

installed_version() { if [ -f "$DIR/current/VERSION" ]; then cat "$DIR/current/VERSION"; fi; }

dc() {
    local files=(-f "$DIR/current/docker-compose.yml")
    if [ "$(env_get IRIS_VOZ_GPU)" != 0 ]; then files+=(-f "$DIR/current/docker-compose.gpu.yml"); fi
    files+=(-f "$DIR/current/docker-compose.descarga.yml")
    # Solo pruebas: un compose más (el Ollama falso de CI).
    if [ -n "${IRIS_VOZ_COMPOSE_EXTRA:-}" ]; then files+=(-f "$IRIS_VOZ_COMPOSE_EXTRA"); fi
    docker compose --project-name iris-voz --env-file "$DIR/.env" "${files[@]}" "$@"
}
puerta() { dc exec -T puerta /usr/local/bin/voz-puerta "$@"; }

need_root() { [ "$(id -u)" = 0 ] || die "hay que ejecutarlo como root (sudo)"; }
need_install() { [ -f "$DIR/current/VERSION" ] || die "Iris Voz no está instalado en $DIR"; }

check_docker() {
    command -v docker > /dev/null || die "docker no está instalado (https://docs.docker.com/engine/install/)"
    local v
    v="$(docker compose version --short 2> /dev/null || true)"
    [ -n "$v" ] || die "falta el plugin docker compose"
    version_ge "${v#v}" "$COMPOSE_MIN" || die "hace falta docker compose $COMPOSE_MIN o más nuevo (hay $v)"
}

check_gpu() {
    command -v nvidia-smi > /dev/null || die "no está nvidia-smi: instala el controlador de NVIDIA (o usa --no-gpu para una prueba)"
    nvidia-smi --query-gpu=name,memory.total --format=csv,noheader < /dev/null >&2 || die "nvidia-smi no ve ninguna GPU"
    docker info --format '{{json .Runtimes}}' 2> /dev/null | grep -q nvidia ||
        die "docker no tiene el runtime de NVIDIA: instala nvidia-container-toolkit y ejecuta 'nvidia-ctk runtime configure --runtime=docker'"
}

check_clock() {
    command -v timedatectl > /dev/null || { warn "no se puede comprobar el reloj (no hay timedatectl): asegúrate de que NTP está activo"; return 0; }
    [ "$(timedatectl show -p NTPSynchronized --value 2> /dev/null)" = yes ] && return 0
    $ALLOW_CLOCK && { warn "el reloj no está sincronizado por NTP: Iris rechaza peticiones con más de 30 s de desfase"; return 0; }
    die "el reloj no está sincronizado por NTP (las peticiones van firmadas con la hora; más de 30 s de desfase se rechaza). Activa NTP o usa --allow-unsynced-clock"
}

check_swap() {
    [ -z "$(swapon --noheadings 2> /dev/null || true)" ] && return 0
    $ALLOW_SWAP && { warn "hay intercambio a disco: lo que el modelo tiene en memoria podría llegar al disco"; return 0; }
    die "hay intercambio a disco (lo que el modelo tiene en memoria podría llegar al disco sin cifrar). Apágalo (swapoff -a) o usa --allow-swap si está cifrado"
}

latest_version() {
    local v
    if [ -n "$SOURCE" ]; then
        v="$(cat "$SOURCE/latest" 2> /dev/null || true)"
    else
        v="$("${CURL[@]}" "https://api.github.com/repos/$REPO/releases?per_page=100" < /dev/null |
            sed -n 's/.*"tag_name": *"voz-v\([0-9][0-9.]*\)".*/\1/p' | sort -V | tail -n1 || true)"
    fi
    is_semver "$v" || die "no se encontró la última versión de Iris Voz (usa --version X.Y.Z)"
    printf '%s' "$v"
}

fetch() { # fetch VERSION NOMBRE DESTINO
    if [ -n "$SOURCE" ]; then
        cp "$SOURCE/$2" "$3" 2> /dev/null || die "falta $2 en $SOURCE"
    else
        "${CURL[@]}" -o "$3" "https://github.com/$REPO/releases/download/voz-v$1/$2" < /dev/null || die "no se pudo bajar $2"
    fi
}

# Baja, verifica y desempaqueta una versión en $DIR/releases/<versión>. Nada se desempaqueta antes
# de verificar la firma y quién firmó.
fetch_release() {
    local v="$1" cosign tmp pkg dest
    dest="$DIR/releases/$v"
    [ -f "$dest/VERSION" ] && return 0
    cosign="$(ensure_cosign "$DIR/bin")"
    tmp="$(mktemp -d)"
    pkg="iris-voz-$v.tar.gz"
    log "Bajando Iris Voz $v"
    fetch "$v" "$pkg" "$tmp/$pkg"
    fetch "$v" "$pkg.sigstore.json" "$tmp/$pkg.sigstore.json"
    log "Verificando la firma (sin red) y quién firmó"
    if ! verify_signature "$cosign" "$tmp/$pkg" "$tmp/$pkg.sigstore.json" "$(signer_identity "$v")"; then
        rm -rf "$tmp"
        die "la firma del paquete NO es válida para $(signer_identity "$v"). No se instaló nada"
    fi
    tar -xzf "$tmp/$pkg" -C "$tmp"
    # El manifiesto firmado debe decir la misma versión que el paquete (y que la etiqueta de quien firmó).
    if [ "$(cat "$tmp/iris-voz-$v/VERSION" 2> /dev/null)" != "$v" ] || [ "$(manifest_get "$tmp/iris-voz-$v/manifiesto.json" version)" != "$v" ]; then
        rm -rf "$tmp"
        die "el paquete dice otra versión que $v. No se instaló nada"
    fi
    mkdir -p "$DIR/releases"
    rm -rf "$dest"
    mv "$tmp/iris-voz-$v" "$dest"
    rm -rf "$tmp"
}

activate_release() {
    local v="$1"
    ln -sfn "releases/$v" "$DIR/current"
    env_set IRIS_VOZ_VERSION "$v"
    install -m 0755 "$DIR/current/install-voz.sh" /usr/local/bin/iris-voz 2> /dev/null || warn "no se pudo instalar /usr/local/bin/iris-voz"
    # Solo se guardan las últimas versiones.
    find "$DIR/releases" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort -V | head -n -"$KEEP_RELEASES" |
        while read -r old; do [ "$old" = "$v" ] || rm -rf "$DIR/releases/${old:?}"; done
}

prepare_volumes() {
    # El volumen de modelos es del usuario sin privilegios con el que corre cada contenedor.
    # (Docker copia el dueño de /modelos de la imagen la primera vez que se monta el volumen vacío.)
    dc --profile descarga run --rm --no-deps preparar < /dev/null > /dev/null
}

start() {
    if [ "${IRIS_VOZ_SKIP_PULL:-0}" != 1 ]; then
        log "Bajando las imágenes"
        dc --profile descarga pull --quiet < /dev/null
    fi
    prepare_volumes
    log "Arrancando"
    dc up -d --remove-orphans < /dev/null
    for _ in $(seq 1 60); do
        if puerta health > /dev/null 2>&1 < /dev/null; then return 0; fi
        sleep 2
    done
    die "el servidor no llegó a estar sano (mira: iris-voz logs puerta)"
}

model_name() {
    [[ "$1" =~ ^[a-zA-Z0-9._:/-]{1,128}$ ]] || die "nombre de modelo inválido: $1"
    case "$1" in *:*) printf '%s' "$1" ;; *) printf '%s:latest' "$1" ;; esac
}

model_pull() { # model_pull NOMBRE [DIGEST]
    local name want="${2:-}" got
    name="$(model_name "$1")"
    log "Bajando el modelo $name (solo se alcanza el registro de modelos)"
    dc --profile descarga run --rm descarga pull "$name" < /dev/null || { dc --profile descarga rm -sf proxy-descarga > /dev/null 2>&1 || true; die "no se pudo bajar $name"; }
    dc --profile descarga rm -sf proxy-descarga > /dev/null 2>&1 || true
    if [ -n "$want" ]; then
        got="$(puerta model digest "$name" < /dev/null || true)"
        if [ "$got" != "$want" ]; then
            dc --profile descarga run --rm --no-deps descarga rm "$name" < /dev/null > /dev/null 2>&1 || true
            die "el modelo $name no coincide con el digest fijado en el manifiesto (llegó '${got:-ninguno}', se esperaba $want). Se borró"
        fi
        log "Digest del modelo verificado: $want"
    fi
}

show_fingerprint() {
    local fp
    fp="$(puerta fingerprint < /dev/null)"
    cat >&2 << EOF

Iris Voz $(installed_version) está en marcha en el puerto $(env_get IRIS_VOZ_PUERTO).

  Huella del certificado de este servidor:
    $fp

Ahora, en el panel de Iris (Administración → Integraciones → Servidor de IA):
  1. «Enlazar servidor de IA», con la dirección de este servidor (https://<este servidor>:$(env_get IRIS_VOZ_PUERTO)).
  2. Aquí: sudo iris-voz pair     (escribe el código de un solo uso que muestra el panel; no se ve al teclear)
  3. En el panel: «Conectar», compara la huella de arriba con la que muestra el panel y «Activar».
EOF
}

cmd_install() {
    need_root
    check_docker
    $NO_GPU || check_gpu
    check_clock
    check_swap
    mkdir -p "$DIR"
    chmod 700 "$DIR"
    local v="$VERSION" cur
    [ -n "$v" ] || v="$(latest_version)"
    is_semver "$v" || die "versión inválida: $v"
    cur="$(installed_version)"
    if [ -n "$cur" ] && [ "$cur" != "$v" ] && version_ge "$cur" "$v"; then
        die "está instalada la versión $cur: no se baja a la $v"
    fi
    fetch_release "$v"
    [ -n "$(env_get IRIS_VOZ_PUERTO)" ] || env_set IRIS_VOZ_PUERTO "${PORT:-8443}"
    [ -z "$PORT" ] || env_set IRIS_VOZ_PUERTO "$PORT"
    [ -z "$LISTEN" ] || env_set IRIS_VOZ_ESCUCHA "$LISTEN"
    if $NO_GPU; then env_set IRIS_VOZ_GPU 0; elif [ -z "$(env_get IRIS_VOZ_GPU)" ]; then env_set IRIS_VOZ_GPU 1; fi
    activate_release "$v"
    start
    if ! $NO_MODELS; then
        local name digest
        while read -r name digest; do
            [ -n "$name" ] || continue
            model_pull "$name" "$digest"
            puerta model allow "$name" < /dev/null >&2
        done < <(manifest_models "$DIR/current/manifiesto.json")
    fi
    show_fingerprint
}

cmd_upgrade() {
    need_root
    need_install
    check_docker
    local v="$VERSION" cur
    cur="$(installed_version)"
    [ -n "$v" ] || v="$(latest_version)"
    is_semver "$v" || die "versión inválida: $v"
    [ "$cur" != "$v" ] || { log "Iris Voz $v ya está instalado"; return 0; }
    version_ge "$v" "$cur" || die "está instalada la versión $cur: no se baja a la $v"
    fetch_release "$v"
    activate_release "$v"
    start
    log "Iris Voz actualizado de $cur a $v"
}

cmd_pair() {
    need_root
    need_install
    local code
    if { : < /dev/tty; } 2> /dev/null && [ -t 0 ]; then
        printf 'Código de un solo uso del panel de Iris (no se ve al escribirlo): ' > /dev/tty
        IFS= read -rs code < /dev/tty || true
        printf '\n' > /dev/tty
    else
        IFS= read -r code || true # por la entrada (automatización); nunca como argumento
    fi
    [ -n "$code" ] || die "no se escribió ningún código"
    printf '%s\n' "$code" | puerta pair
    unset code
}

cmd_status() {
    need_install
    echo "versión: $(installed_version)"
    dc ps
    puerta status < /dev/null
    echo "modelos instalados:"
    dc exec -T llm ollama list < /dev/null 2> /dev/null || true
}

cmd_model() {
    need_root
    need_install
    local sub="${1:-}" name="${2:-}"
    case "$sub" in
        pull) [ -n "$name" ] || die "uso: model pull NOMBRE"; model_pull "$name" ;;
        remove)
            [ -n "$name" ] || die "uso: model remove NOMBRE"
            name="$(model_name "$name")"
            puerta model revoke "$name" < /dev/null
            dc --profile descarga run --rm --no-deps descarga rm "$name" < /dev/null
            ;;
        allow) [ -n "$name" ] || die "uso: model allow NOMBRE"; puerta model allow "$(model_name "$name")" < /dev/null ;;
        revoke) [ -n "$name" ] || die "uso: model revoke NOMBRE"; puerta model revoke "$(model_name "$name")" < /dev/null ;;
        list)
            echo "permitidos:"
            puerta model list < /dev/null
            echo "instalados:"
            dc exec -T llm ollama list < /dev/null 2> /dev/null || true
            ;;
        *) die "uso: model pull|remove|allow|revoke NOMBRE | model list" ;;
    esac
}

main() {
    CMD=install
    case "${1:-}" in
        install | upgrade | pair | unpair | status | renew-cert | ip | model | logs | stop | start | compose | version) CMD="$1"; shift ;;
        help | -h | --help) usage 0 ;;
        '' | -*) ;;
        *) die "orden desconocida: $1 (mira: iris-voz help)" ;;
    esac
    VERSION="" PORT="" LISTEN="" NO_GPU=false NO_MODELS=false ALLOW_SWAP=false ALLOW_CLOCK=false
    REST=()
    case "$CMD" in
        compose | logs | model | ip | renew-cert) REST=("$@"); set -- ;;
    esac
    while [ $# -gt 0 ]; do
        case "$1" in
            --version) [ $# -ge 2 ] || die "--version necesita un valor"; VERSION="${2#v}"; shift 2 ;;
            --dir) [ $# -ge 2 ] || die "--dir necesita un valor"; DIR="$2"; shift 2 ;;
            --port) [ $# -ge 2 ] || die "--port necesita un valor"; PORT="$2"; shift 2 ;;
            --listen) [ $# -ge 2 ] || die "--listen necesita un valor"; LISTEN="$2"; shift 2 ;;
            --no-gpu) NO_GPU=true; shift ;;
            --no-models) NO_MODELS=true; shift ;;
            --allow-swap) ALLOW_SWAP=true; shift ;;
            --allow-unsynced-clock) ALLOW_CLOCK=true; shift ;;
            -y | --yes) shift ;;
            -h | --help) usage 0 ;;
            *) die "opción desconocida: $1 (mira: iris-voz help)" ;;
        esac
    done
    case "$DIR" in /*) ;; *) DIR="$(pwd)/$DIR" ;; esac
    DIR="${DIR%/}"
    [ -n "$DIR" ] || die "--dir no puede ser /"
    [ -z "$PORT" ] || [[ "$PORT" =~ ^[0-9]{1,5}$ ]] || die "puerto inválido: $PORT"

    case "$CMD" in
        install) cmd_install ;;
        upgrade) cmd_upgrade ;;
        pair) cmd_pair ;;
        unpair) need_root; need_install; puerta unpair < /dev/null ;;
        status) cmd_status ;;
        renew-cert)
            need_root
            need_install
            if [ "${REST[0]:-}" = --no-channel ]; then puerta renew-cert --no-channel < /dev/null; else puerta renew-cert < /dev/null; fi
            ;;
        ip)
            need_root
            need_install
            case "${REST[0]:-}" in
                restrict) puerta ip restrict < /dev/null ;;
                release) puerta ip release < /dev/null ;;
                *) die "uso: ip restrict|release" ;;
            esac
            ;;
        model) cmd_model "${REST[@]}" ;;
        logs) need_install; dc logs -f --tail 100 "${REST[@]}" ;;
        stop) need_root; need_install; dc --profile descarga down ;;
        start) need_root; need_install; IRIS_VOZ_SKIP_PULL=1 start ;;
        compose) need_install; dc "${REST[@]}" ;;
        version) need_install; installed_version ;;
    esac
}

# Solo corre si se ejecuta; las pruebas lo cargan con «.» para llamar a sus funciones.
if ! (return 0 2> /dev/null); then
    main "$@"
fi
