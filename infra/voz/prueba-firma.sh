#!/usr/bin/env bash
# Tests the signature check of install-voz.sh with a REAL keyless signature and NO network.
#
#   infra/voz/prueba-firma.sh <identity> [sign]
#
# With `sign` (CI, voz.yml, a job with `id-token: write`): builds a test package and signs it with
# cosign keyless, so the signer is the workflow that runs this (<identity>, e.g.
# https://github.com/OWNER/REPO/.github/workflows/voz.yml@refs/pull/N/merge). Without it, verifies
# the files already in $IRIS_VOZ_FIRMA_DIR (voz-release.yml: the real release package).
#
# Then it calls verify_signature of install-voz.sh, the same function the installer uses, inside a
# network namespace with no interfaces (`unshare -n`), with the trusted root embedded in the script
# and the pinned cosign: the right identity must verify, and another identity, a tampered file, a
# bundle of another file and a missing bundle must all fail.
set -euo pipefail

identidad="${1:?usage: prueba-firma.sh <identity> [sign]}"
aqui="$(cd "$(dirname "$0")" && pwd)"
d="${IRIS_VOZ_FIRMA_DIR:-$(mktemp -d)}"

# shellcheck source=infra/voz/install-voz.sh
. "$aqui/install-voz.sh"

cosign="$(ensure_cosign "$d/bin")"
"$cosign" version 2>&1 | grep -F "GitVersion" >&2 || true

if [ "${2:-}" = sign ]; then
    version="$(manifest_get "$aqui/manifiesto.json" version)"
    "$aqui/package-voz.sh" "$version" "$d" > /dev/null
    for f in "iris-voz-$version.tar.gz" install-voz.sh setup-voz.sh manifiesto.json; do
        "$cosign" sign-blob --yes --new-bundle-format --bundle "$d/$f.sigstore.json" "$d/$f" > /dev/null
    done
fi

paquete="$(find "$d" -maxdepth 1 -name 'iris-voz-*.tar.gz' | head -n1)"
[ -n "$paquete" ] || die "no package in $d"

# Everything below runs without network: verification must not need it.
sin_red() { unshare -n -- "$@"; }
if ! sin_red true 2> /dev/null; then die "cannot create a network namespace (run as root)"; fi
if sin_red curl -fsS -m 3 https://rekor.sigstore.dev > /dev/null 2>&1; then die "the test namespace has network"; fi
export -f verify_signature write_trusted_root die
export OIDC_ISSUER
verificar() { sin_red bash -c 'verify_signature "$@"' _ "$cosign" "$@"; }

for f in "$paquete" "$d/install-voz.sh" "$d/setup-voz.sh" "$d/manifiesto.json"; do
    verificar "$f" "$f.sigstore.json" "$identidad" || die "a valid signature did NOT verify offline: $f"
done
log "valid signatures verify offline with the embedded trusted root"

fallar() { # fallar <descripción> <archivo> <bundle> <identidad>
    if (verificar "$2" "$3" "$4") 2> /dev/null; then die "verification passed and must fail: $1"; fi
    log "fails closed: $1"
}
fallar "another signer (a branch instead of the tag)" "$paquete" "$paquete.sigstore.json" \
    "https://github.com/HCDestroyer/iris/.github/workflows/voz-release.yml@refs/heads/main"
fallar "another workflow of the same repository" "$paquete" "$paquete.sigstore.json" "${identidad/voz/otro}"
cp "$paquete" "$d/alterado.tar.gz"
printf 'x' >> "$d/alterado.tar.gz"
fallar "a tampered package" "$d/alterado.tar.gz" "$paquete.sigstore.json" "$identidad"
fallar "the bundle of another file" "$paquete" "$d/install-voz.sh.sigstore.json" "$identidad"
fallar "setup-voz.sh with the bundle of install-voz.sh" "$d/setup-voz.sh" "$d/install-voz.sh.sigstore.json" "$identidad"
fallar "no bundle" "$paquete" "$d/no-existe.sigstore.json" "$identidad"
# A bundle without its transparency log entry must not verify: the inclusion proof is mandatory.
jq 'del(.verificationMaterial.tlogEntries)' "$paquete.sigstore.json" > "$d/sin-tlog.sigstore.json"
fallar "a bundle without the transparency log entry" "$paquete" "$d/sin-tlog.sigstore.json" "$identidad"
log "ok"
