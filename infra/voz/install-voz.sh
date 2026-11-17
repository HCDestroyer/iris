#!/usr/bin/env bash
# Iris Voz installer and manager: the dedicated AI server of Iris (docs/runbooks/servidor-ia.md).
#
# Verify this script BEFORE running it (cosign 2.4 or newer, from your own trusted source):
#
#   cosign verify-blob --new-bundle-format --bundle install-voz.sh.sigstore.json \
#     --certificate-oidc-issuer https://token.actions.githubusercontent.com \
#     --certificate-identity https://github.com/HCDestroyer/iris/.github/workflows/voz-release.yml@refs/tags/voz-vX.Y.Z \
#     install-voz.sh && sudo bash install-voz.sh
#
# It never builds anything. It downloads the release package, verifies its Sigstore signature
# OFFLINE (bundle + the Sigstore trusted root embedded in this script; the transparency log proof
# is mandatory) and the signer identity (the voz-release workflow of this repository on a voz-vX.Y.Z
# tag), and fails closed. Then it writes <dir>/.env, pulls the images (pinned by digest), starts the
# server and prints the certificate fingerprint to compare in the Iris panel. After the install the
# same script is available as `iris-voz`.
#
# Usage: install-voz.sh [command] [options]
#
# Commands:
#   install             install or reconfigure (default)
#   upgrade             move to a newer version (--version X.Y.Z or latest); never downgrades
#   pair                open the pairing window; asks for the one-time code of the Iris panel
#                       (hidden input; the code is never accepted as an argument)
#   unpair              forget Iris: nothing is accepted until a new pairing
#   status              version, containers, fingerprint, observed Iris IP, allowed models
#   renew-cert          new server certificate, announced to Iris over the current channel
#                       (--no-channel: the old one was lost; unpairs, pair again with a code)
#   ip restrict|release only accept the control port from the Iris IP seen at pairing (off by default)
#   model pull NAME     download a model (one-shot container; only reaches the model registry)
#   model remove NAME   delete a model
#   model allow NAME    let Iris use an installed model       (model disallow NAME: the opposite)
#   model list          installed and allowed models
#   logs [service]      follow the logs (puerta, llm, medidor)
#   stop | start        stop or start every container
#   compose ARGS        run docker compose on the installation
#   version             print the installed version
#
# Install options:
#   --version X.Y.Z     version to install (default: latest voz-v* release)
#   --dir PATH          install folder (default /opt/iris-voz)
#   --port N            control port (default 8443)
#   --listen ADDR       address the control port binds to (default 0.0.0.0)
#   --no-gpu            run without a GPU (tests only: CPU inference is too slow for real use)
#   --no-models         do not download the default models
#   --allow-swap        do not stop when unencrypted swap is on
#   --allow-unsynced-clock  do not stop when the clock is not NTP-synchronised
#   -y, --yes           never ask
#
# Environment (tests and offline servers): IRIS_VOZ_DIR, IRIS_VOZ_REPO (HCDestroyer/iris),
# IRIS_VOZ_SOURCE (local folder with iris-voz-<v>.tar.gz, its .sigstore.json and a `latest` file,
# instead of GitHub; the signature is verified all the same), IRIS_VOZ_COSIGN (path to a cosign
# binary already on the server; its sha256 must match the pinned one), IRIS_VOZ_SKIP_PULL=1 (images
# already loaded).
set -Eeuo pipefail

REPO="${IRIS_VOZ_REPO:-HCDestroyer/iris}"
SOURCE="${IRIS_VOZ_SOURCE:-}"
DIR="${IRIS_VOZ_DIR:-/opt/iris-voz}"
COMPOSE_MIN=2.24
KEEP_RELEASES=3
CURL=(curl -fsSL --proto '=https' --tlsv1.2 --retry 3)

# Who must have signed the package: the release workflow of this repository, on a protected
# voz-vX.Y.Z tag, never a branch. Not configurable.
OIDC_ISSUER=https://token.actions.githubusercontent.com
signer_identity() { printf 'https://github.com/HCDestroyer/iris/.github/workflows/voz-release.yml@refs/tags/voz-v%s' "$1"; }

# cosign, pinned by sha256 (official release binaries).
COSIGN_VERSION=v2.6.1
cosign_sha256() {
    case "$1" in
        amd64) echo 064954c5d8c7e3b28188eee5b1727b31c411550bc5fefd41aa672d3c761d103a ;;
        arm64) echo 56a16480bdd56ec789abaa65924402f6b92c0041f06885995853c05567b76f34 ;;
    esac
}
COSIGN_URL="https://github.com/sigstore/cosign/releases/download/$COSIGN_VERSION"

log() { printf '\033[1m==>\033[0m %s\n' "$*" >&2; }
warn() { printf '\033[33mwarning:\033[0m %s\n' "$*" >&2; }
die() { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }
usage() { sed -n '2,/^set -E/{/^#/p}' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit "${1:-0}"; }

version_ge() { [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" = "$2" ]; }
is_semver() { [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; }
arch() {
    case "$(uname -m)" in
        x86_64 | amd64) echo amd64 ;;
        aarch64 | arm64) echo arm64 ;;
        *) die "unsupported architecture: $(uname -m)" ;;
    esac
}

# The Sigstore trusted root (public keys of Fulcio, Rekor, CT log and timestamp authority) travels
# inside this script, so verification needs no network. If Sigstore rotates its keys and this copy
# no longer validates a newer package, verification fails closed: install with a newer installer.
# Kept identical to infra/voz/sigstore/trusted_root.json (infra/voz/verificar.sh checks it).
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

# ensure_cosign DIR → path of a cosign binary whose sha256 is the pinned one.
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
        log "Downloading cosign $COSIGN_VERSION"
        "${CURL[@]}" -o "$dest.tmp" "$COSIGN_URL/cosign-linux-$a" < /dev/null || die "could not download cosign"
        chmod 0755 "$dest.tmp"
    fi
    if [ "$(sha256sum "$dest.tmp" | cut -d' ' -f1)" != "$want" ]; then
        rm -f "$dest.tmp"
        die "cosign does not match its pinned sha256 ($want)"
    fi
    mv -f "$dest.tmp" "$dest"
    printf '%s' "$dest"
}

# verify_signature COSIGN FILE BUNDLE IDENTITY: offline, with the embedded trusted root. The
# transparency log inclusion proof inside the bundle is always checked (never ignored).
verify_signature() {
    local cosign="$1" file="$2" bundle="$3" identity="$4" root
    [ -s "$file" ] || die "missing file to verify: $file"
    [ -s "$bundle" ] || die "missing signature bundle: $bundle (unsigned packages are never installed)"
    root="$(mktemp)"
    write_trusted_root "$root"
    if ! "$cosign" verify-blob --new-bundle-format --offline --trusted-root "$root" --bundle "$bundle" \
        --certificate-oidc-issuer "$OIDC_ISSUER" --certificate-identity "$identity" "$file" > /dev/null 2>&1 < /dev/null; then
        rm -f "$root"
        return 1
    fi
    rm -f "$root"
}

# manifest_get FILE KEY → string value of a top-level key of manifiesto.json.
manifest_get() { sed -n "s/^ *\"$2\": *\"\([^\"]*\)\".*/\1/p" "$1" | head -n1; }
# manifest_models FILE → "name digest" per default model.
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
    # Tests only: an extra compose file (the fake Ollama of CI).
    if [ -n "${IRIS_VOZ_COMPOSE_EXTRA:-}" ]; then files+=(-f "$IRIS_VOZ_COMPOSE_EXTRA"); fi
    docker compose --project-name iris-voz --env-file "$DIR/.env" "${files[@]}" "$@"
}
puerta() { dc exec -T puerta /usr/local/bin/voz-puerta "$@"; }

need_root() { [ "$(id -u)" = 0 ] || die "run as root (sudo)"; }
need_install() { [ -f "$DIR/current/VERSION" ] || die "Iris Voz is not installed in $DIR"; }

check_docker() {
    command -v docker > /dev/null || die "docker is not installed (https://docs.docker.com/engine/install/)"
    local v
    v="$(docker compose version --short 2> /dev/null || true)"
    [ -n "$v" ] || die "the docker compose plugin is not installed"
    version_ge "${v#v}" "$COMPOSE_MIN" || die "docker compose $COMPOSE_MIN or newer is required (found $v)"
}

check_gpu() {
    command -v nvidia-smi > /dev/null || die "nvidia-smi not found: install the NVIDIA driver (or use --no-gpu for a test)"
    nvidia-smi --query-gpu=name,memory.total --format=csv,noheader < /dev/null >&2 || die "nvidia-smi cannot see a GPU"
    docker info --format '{{json .Runtimes}}' 2> /dev/null | grep -q nvidia ||
        die "docker has no NVIDIA runtime: install nvidia-container-toolkit and run 'nvidia-ctk runtime configure --runtime=docker'"
}

check_clock() {
    command -v timedatectl > /dev/null || { warn "cannot check the clock (no timedatectl): make sure NTP is on"; return 0; }
    [ "$(timedatectl show -p NTPSynchronized --value 2> /dev/null)" = yes ] && return 0
    $ALLOW_CLOCK && { warn "the clock is not NTP-synchronised: Iris rejects requests more than 30 s off"; return 0; }
    die "the clock is not NTP-synchronised (requests are signed with a timestamp; more than 30 s off is rejected). Enable NTP or pass --allow-unsynced-clock"
}

check_swap() {
    [ -z "$(swapon --noheadings 2> /dev/null || true)" ] && return 0
    $ALLOW_SWAP && { warn "swap is on: prompts in memory could reach the disk"; return 0; }
    die "swap is on (prompts in memory could reach the disk unencrypted). Turn it off (swapoff -a) or pass --allow-swap if it is encrypted"
}

latest_version() {
    local v
    if [ -n "$SOURCE" ]; then
        v="$(cat "$SOURCE/latest" 2> /dev/null || true)"
    else
        v="$("${CURL[@]}" "https://api.github.com/repos/$REPO/releases?per_page=100" < /dev/null |
            sed -n 's/.*"tag_name": *"voz-v\([0-9][0-9.]*\)".*/\1/p' | sort -V | tail -n1 || true)"
    fi
    is_semver "$v" || die "could not find the latest Iris Voz release (use --version X.Y.Z)"
    printf '%s' "$v"
}

fetch() { # fetch VERSION NAME DEST
    if [ -n "$SOURCE" ]; then
        cp "$SOURCE/$2" "$3" 2> /dev/null || die "missing $2 in $SOURCE"
    else
        "${CURL[@]}" -o "$3" "https://github.com/$REPO/releases/download/voz-v$1/$2" < /dev/null || die "could not download $2"
    fi
}

# Downloads, verifies and unpacks a version into $DIR/releases/<version>. Nothing is unpacked before
# the signature and the signer identity are verified.
fetch_release() {
    local v="$1" cosign tmp pkg dest
    dest="$DIR/releases/$v"
    [ -f "$dest/VERSION" ] && return 0
    cosign="$(ensure_cosign "$DIR/bin")"
    tmp="$(mktemp -d)"
    pkg="iris-voz-$v.tar.gz"
    log "Downloading Iris Voz $v"
    fetch "$v" "$pkg" "$tmp/$pkg"
    fetch "$v" "$pkg.sigstore.json" "$tmp/$pkg.sigstore.json"
    log "Verifying the signature (offline) and the signer"
    if ! verify_signature "$cosign" "$tmp/$pkg" "$tmp/$pkg.sigstore.json" "$(signer_identity "$v")"; then
        rm -rf "$tmp"
        die "the package signature is NOT valid for $(signer_identity "$v"). Nothing was installed"
    fi
    tar -xzf "$tmp/$pkg" -C "$tmp"
    # The signed manifest must say the same version as the package (and as the tag of the signer).
    if [ "$(cat "$tmp/iris-voz-$v/VERSION" 2> /dev/null)" != "$v" ] || [ "$(manifest_get "$tmp/iris-voz-$v/manifiesto.json" version)" != "$v" ]; then
        rm -rf "$tmp"
        die "the package says another version than $v. Nothing was installed"
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
    install -m 0755 "$DIR/current/install-voz.sh" /usr/local/bin/iris-voz 2> /dev/null || warn "could not install /usr/local/bin/iris-voz"
    # Keep the last releases only.
    find "$DIR/releases" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort -V | head -n -"$KEEP_RELEASES" |
        while read -r old; do [ "$old" = "$v" ] || rm -rf "$DIR/releases/${old:?}"; done
}

prepare_volumes() {
    # The models volume belongs to the unprivileged user every container runs as.
    # (Docker copies the owner of /modelos in the image the first time the empty volume is mounted.)
    dc --profile descarga run --rm --no-deps preparar < /dev/null > /dev/null
}

start() {
    if [ "${IRIS_VOZ_SKIP_PULL:-0}" != 1 ]; then
        log "Pulling the images"
        dc --profile descarga pull --quiet < /dev/null
    fi
    prepare_volumes
    log "Starting"
    dc up -d --remove-orphans < /dev/null
    for _ in $(seq 1 60); do
        if puerta salud > /dev/null 2>&1 < /dev/null; then return 0; fi
        sleep 2
    done
    die "the server did not become healthy (see: iris-voz logs puerta)"
}

model_name() {
    [[ "$1" =~ ^[a-zA-Z0-9._:/-]{1,128}$ ]] || die "invalid model name: $1"
    case "$1" in *:*) printf '%s' "$1" ;; *) printf '%s:latest' "$1" ;; esac
}

model_pull() { # model_pull NAME [DIGEST]
    local name want="${2:-}" got
    name="$(model_name "$1")"
    log "Downloading the model $name (only the model registry is reachable)"
    dc --profile descarga run --rm descarga pull "$name" < /dev/null || { dc --profile descarga rm -sf proxy-descarga > /dev/null 2>&1 || true; die "could not download $name"; }
    dc --profile descarga rm -sf proxy-descarga > /dev/null 2>&1 || true
    if [ -n "$want" ]; then
        got="$(puerta modelo digest "$name" < /dev/null || true)"
        if [ "$got" != "$want" ]; then
            dc --profile descarga run --rm --no-deps descarga rm "$name" < /dev/null > /dev/null 2>&1 || true
            die "the model $name does not match the digest pinned in the manifest (got '${got:-none}', want $want). It was removed"
        fi
        log "Model digest verified: $want"
    fi
}

show_fingerprint() {
    local fp
    fp="$(puerta huella < /dev/null)"
    cat >&2 << EOF

Iris Voz $(installed_version) is running on port $(env_get IRIS_VOZ_PUERTO).

  Certificate fingerprint of this server:
    $fp

Next, in the Iris panel (Administration → Integrations → AI server):
  1. "Link AI server", with this server's address (https://<this server>:$(env_get IRIS_VOZ_PUERTO)).
  2. Here: sudo iris-voz pair       (type the one-time code the panel shows; it is not echoed)
  3. In the panel: "Connect", compare the fingerprint above with the one the panel shows, "Activate".
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
    is_semver "$v" || die "invalid version: $v"
    cur="$(installed_version)"
    if [ -n "$cur" ] && [ "$cur" != "$v" ] && version_ge "$cur" "$v"; then
        die "version $cur is installed: refusing to downgrade to $v"
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
            puerta modelo permitir "$name" < /dev/null >&2
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
    is_semver "$v" || die "invalid version: $v"
    [ "$cur" != "$v" ] || { log "Iris Voz $v is already installed"; return 0; }
    version_ge "$v" "$cur" || die "version $cur is installed: refusing to downgrade to $v"
    fetch_release "$v"
    activate_release "$v"
    start
    log "Iris Voz upgraded from $cur to $v"
}

cmd_pair() {
    need_root
    need_install
    local code
    if { : < /dev/tty; } 2> /dev/null && [ -t 0 ]; then
        printf 'One-time code from the Iris panel (not shown while you type): ' > /dev/tty
        IFS= read -rs code < /dev/tty || true
        printf '\n' > /dev/tty
    else
        IFS= read -r code || true # piped (automation); never an argument
    fi
    [ -n "$code" ] || die "no code given"
    printf '%s\n' "$code" | puerta enlazar
    unset code
}

cmd_status() {
    need_install
    echo "version: $(installed_version)"
    dc ps
    puerta estado < /dev/null
    echo "installed models:"
    dc exec -T llm ollama list < /dev/null 2> /dev/null || true
}

cmd_model() {
    need_root
    need_install
    local sub="${1:-}" name="${2:-}"
    case "$sub" in
        pull) [ -n "$name" ] || die "usage: model pull NAME"; model_pull "$name" ;;
        remove)
            [ -n "$name" ] || die "usage: model remove NAME"
            name="$(model_name "$name")"
            puerta modelo retirar "$name" < /dev/null
            dc --profile descarga run --rm --no-deps descarga rm "$name" < /dev/null
            ;;
        allow) [ -n "$name" ] || die "usage: model allow NAME"; puerta modelo permitir "$(model_name "$name")" < /dev/null ;;
        disallow) [ -n "$name" ] || die "usage: model disallow NAME"; puerta modelo retirar "$(model_name "$name")" < /dev/null ;;
        list)
            echo "allowed:"
            puerta modelo lista < /dev/null
            echo "installed:"
            dc exec -T llm ollama list < /dev/null 2> /dev/null || true
            ;;
        *) die "usage: model pull|remove|allow|disallow NAME | model list" ;;
    esac
}

main() {
    CMD=install
    case "${1:-}" in
        install | upgrade | pair | unpair | status | renew-cert | ip | model | logs | stop | start | compose | version) CMD="$1"; shift ;;
        help | -h | --help) usage 0 ;;
        '' | -*) ;;
        *) die "unknown command: $1 (see --help)" ;;
    esac
    VERSION="" PORT="" LISTEN="" NO_GPU=false NO_MODELS=false ALLOW_SWAP=false ALLOW_CLOCK=false
    REST=()
    case "$CMD" in
        compose | logs | model | ip | renew-cert) REST=("$@"); set -- ;;
    esac
    while [ $# -gt 0 ]; do
        case "$1" in
            --version) [ $# -ge 2 ] || die "--version needs a value"; VERSION="${2#v}"; shift 2 ;;
            --dir) [ $# -ge 2 ] || die "--dir needs a value"; DIR="$2"; shift 2 ;;
            --port) [ $# -ge 2 ] || die "--port needs a value"; PORT="$2"; shift 2 ;;
            --listen) [ $# -ge 2 ] || die "--listen needs a value"; LISTEN="$2"; shift 2 ;;
            --no-gpu) NO_GPU=true; shift ;;
            --no-models) NO_MODELS=true; shift ;;
            --allow-swap) ALLOW_SWAP=true; shift ;;
            --allow-unsynced-clock) ALLOW_CLOCK=true; shift ;;
            -y | --yes) shift ;;
            -h | --help) usage 0 ;;
            *) die "unknown option: $1 (see --help)" ;;
        esac
    done
    case "$DIR" in /*) ;; *) DIR="$(pwd)/$DIR" ;; esac
    DIR="${DIR%/}"
    [ -n "$DIR" ] || die "--dir cannot be /"
    [ -z "$PORT" ] || [[ "$PORT" =~ ^[0-9]{1,5}$ ]] || die "invalid port: $PORT"

    case "$CMD" in
        install) cmd_install ;;
        upgrade) cmd_upgrade ;;
        pair) cmd_pair ;;
        unpair) need_root; need_install; puerta desenlazar < /dev/null ;;
        status) cmd_status ;;
        renew-cert)
            need_root
            need_install
            if [ "${REST[0]:-}" = --no-channel ]; then puerta renovar-certificado --sin-canal < /dev/null; else puerta renovar-certificado < /dev/null; fi
            ;;
        ip)
            need_root
            need_install
            case "${REST[0]:-}" in
                restrict) puerta ip restringir < /dev/null ;;
                release) puerta ip liberar < /dev/null ;;
                *) die "usage: ip restrict|release" ;;
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

# Runs only when executed; tests source this file to call its functions.
if ! (return 0 2> /dev/null); then
    main "$@"
fi
