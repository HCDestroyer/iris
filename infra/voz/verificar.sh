#!/usr/bin/env bash
# Checks the Iris Voz package files without images: the Sigstore trusted root embedded in
# install-voz.sh is the committed one, the manifest is well formed, every compose combination is
# valid, the hardening of the compose holds and package-voz.sh builds what install-voz.sh expects.
# Used by `make lint-infra` and CI (voz.yml).
set -euo pipefail

aqui="$(cd "$(dirname "$0")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# shellcheck source=infra/voz/install-voz.sh
. "$aqui/install-voz.sh"

write_trusted_root "$tmp/raiz.json"
cmp -s "$tmp/raiz.json" "$aqui/sigstore/trusted_root.json" ||
    { echo "the trusted root embedded in install-voz.sh differs from sigstore/trusted_root.json" >&2; exit 1; }
jq -e '.mediaType | startswith("application/vnd.dev.sigstore.trustedroot")' "$tmp/raiz.json" > /dev/null
echo "raíz de confianza de Sigstore: ok"

version="$(manifest_get "$aqui/manifiesto.json" version)"
jq -e --arg v "$version" '.version == $v and .protocolo == "v1" and (.irisMin | test("^[0-9]+\\.[0-9]+\\.[0-9]+$"))
    and (.irisMax | test("^[0-9]+\\.[0-9]+\\.[0-9]+$")) and (.modelos | length > 0)
    and all(.modelos[]; (.digest | test("^sha256:[0-9a-f]{64}$")) and (.nombre | test("^[a-zA-Z0-9._:/-]+$")))' \
    "$aqui/manifiesto.json" > /dev/null || { echo "manifiesto.json is not well formed" >&2; exit 1; }
[ "$(manifest_models "$aqui/manifiesto.json" | wc -l)" = "$(jq '.modelos | length' "$aqui/manifiesto.json")" ] ||
    { echo "install-voz.sh cannot read the models of manifiesto.json (one per line: nombre, digest)" >&2; exit 1; }
echo "manifiesto $version: ok"

# The base images of the Dockerfile and its frontend are pinned by digest.
if grep -E '^(FROM |# syntax=)' "$aqui/Dockerfile" | grep -qv '@sha256:[0-9a-f]\{64\}'; then
    echo "infra/voz/Dockerfile: an image or the frontend is not pinned by digest" >&2
    exit 1
fi
# voz-release.yml signs with the same cosign the installer pins.
flujo="$aqui/../../.github/workflows/voz-release.yml"
if ! grep -qF "COSIGN_VERSION: $COSIGN_VERSION" "$flujo" || ! grep -qF "COSIGN_SHA256: $(cosign_sha256 amd64)" "$flujo"; then
    echo "voz-release.yml does not pin the same cosign as install-voz.sh" >&2
    exit 1
fi
# Nothing is signed or published without the approval of the voz-release environment.
for trabajo in firmar publicar; do
    sed -n "/^  $trabajo:/,/^  [a-z]*:\$/p" "$flujo" | grep -q '^    environment: voz-release$' ||
        { echo "voz-release.yml: the job $trabajo must run in the voz-release environment" >&2; exit 1; }
done
# The App key that creates voz-v* tags is only read in the voz-etiqueta environment (main only).
etiqueta="$aqui/../../.github/workflows/voz-etiqueta.yml"
grep -q '^    environment: voz-etiqueta$' "$etiqueta" ||
    { echo "voz-etiqueta.yml: the job must run in the voz-etiqueta environment" >&2; exit 1; }
if grep -n 'secrets\.VOZ_APP' "$aqui/../../.github/workflows/"*.yml | grep -v '/voz-etiqueta.yml:' | grep -q .; then
    echo "only voz-etiqueta.yml may read the VOZ_APP_* secrets" >&2
    exit 1
fi
# No other workflow may run in that environment or read every secret at once (comments aside;
# `voz-etiqueta.yml` as a file name, e.g. in the paths of voz.yml, is fine).
for w in "$aqui/../../.github/workflows/"*.yml; do
    [ "$(basename "$w")" = voz-etiqueta.yml ] && continue
    if grep -vE '^\s*#' "$w" | grep -qE 'voz-etiqueta([^.]|$)|toJSON\(secrets\)|secrets\['; then
        echo "$(basename "$w"): only voz-etiqueta.yml may use the voz-etiqueta environment or read secrets in bulk" >&2
        exit 1
    fi
done
# Every action of the voz workflows is pinned by commit SHA.
if grep -hE '^\s*-?\s*uses:' "$aqui/../../.github/workflows/voz.yml" "$flujo" "$etiqueta" | grep -qvE '@[0-9a-f]{40}( |$)'; then
    echo "a GitHub action of the voz workflows is not pinned by SHA" >&2
    exit 1
fi
echo "imágenes, cosign y acciones fijadas: ok"

# setup-voz.sh (the one-step setup) pins the same cosign and accepts the same signer as install-voz.sh,
# and voz-release.yml signs and publishes it like install-voz.sh.
fijado() { # fijado SCRIPT → what the script pins (in a subshell: both scripts define the same names)
    (
        # shellcheck disable=SC1090
        . "$1"
        printf '%s\n' "$COSIGN_VERSION" "$(cosign_sha256 amd64)" "$(cosign_sha256 arm64)" "$OIDC_ISSUER" "$(signer_identity 1.2.3)"
    )
}
[ "$(fijado "$aqui/setup-voz.sh")" = "$(fijado "$aqui/install-voz.sh")" ] ||
    { echo "setup-voz.sh does not pin the same cosign or signer identity as install-voz.sh" >&2; exit 1; }
grep -qE '^ *\(cd dist && sha256sum .* setup-voz\.sh ' "$flujo" ||
    { echo "voz-release.yml does not sign setup-voz.sh (missing from the sums of construir)" >&2; exit 1; }
for a in setup-voz.sh setup-voz.sh.sigstore.json setup-voz.sh.sha256; do
    grep -qF "\"dist/$a#" "$flujo" || { echo "voz-release.yml does not publish $a" >&2; exit 1; }
done
echo "setup-voz.sh: ok"

if docker compose version > /dev/null 2>&1; then
    printf 'IRIS_VOZ_VERSION=%s\n' "$version" > "$tmp/.env"
    base=(--env-file "$tmp/.env" -f "$aqui/docker-compose.yml")
    docker compose "${base[@]}" -f "$aqui/docker-compose.descarga.yml" --profile descarga config --quiet
    docker compose "${base[@]}" -f "$aqui/docker-compose.gpu.yml" -f "$aqui/docker-compose.descarga.yml" --profile descarga config --quiet
    docker compose "${base[@]}" -f "$aqui/docker-compose.descarga.yml" -f "$aqui/docker-compose.prueba.yml" config --quiet
    todo="$(docker compose "${base[@]}" -f "$aqui/docker-compose.gpu.yml" -f "$aqui/docker-compose.descarga.yml" --profile descarga config --format json)"
    # Hardening that must not regress (voz.md §4.10 and §6.3).
    jq -e '
        (.services | to_entries | all(.value.read_only == true and (.value.cap_drop == ["ALL"])
            and (.value.security_opt | index("no-new-privileges:true") != null) and (.value.privileged != true)
            and (.value.user == "65532:65532") and ((.value.pids_limit // 0) > 0) and ((.value.mem_limit // "0" | tonumber) > 0)))
        # the download proxy only lets exact hosts through (no suffixes, no wildcards)
        and (.services["proxy-descarga"].environment.IRIS_VOZ_DESCARGA_HOSTS | split(",") | all(test("^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$")))
        # the only published port is the control port, on the puerta
        and ([.services | to_entries[] | select(.value.ports != null) | .key] == ["puerta"])
        # Ollama: internal network only; nothing but the download proxy reaches the outside
        and (.services.llm.networks | keys == ["interna"]) and (.networks.interna.internal == true)
        and (.services.descarga.networks | keys == ["descarga"]) and (.networks.descarga.internal == true)
        and (.services.medidor.network_mode == "none") and (.services.preparar.network_mode == "none")
        # models: read-only everywhere except the one-shot download containers
        and ([.services | to_entries[] | select(any(.value.volumes[]?; .source == "modelos" and .read_only != true)) | .key] | sort == ["descarga", "preparar"])
        # the state volume (credentials) is only in the puerta
        and ([.services | to_entries[] | select(any(.value.volumes[]?; .source == "estado")) | .key] == ["puerta"])
        # GPU: only llm and medidor
        and ([.services | to_entries[] | select(.value.deploy.resources.reservations.devices != null) | .key] | sort == ["llm", "medidor"])
    ' <<< "$todo" > /dev/null || { echo "the compose of Iris Voz lost part of its hardening" >&2; exit 1; }
    echo "compose de Iris Voz: ok"
else
    echo "verificar: omitido docker compose config (docker compose no instalado)"
fi

"$aqui/package-voz.sh" "$version" "$tmp/dist" > /dev/null
tar -tzf "$tmp/dist/iris-voz-$version.tar.gz" > "$tmp/lista"
for f in docker-compose.yml docker-compose.gpu.yml docker-compose.descarga.yml install-voz.sh manifiesto.json README.md VERSION; do
    grep -qxF "iris-voz-$version/$f" "$tmp/lista" || { echo "the package lacks $f" >&2; exit 1; }
done
if grep -q prueba "$tmp/lista"; then echo "the package must not ship test files" >&2; exit 1; fi
for f in install-voz.sh install-voz.sh.sha256 setup-voz.sh setup-voz.sh.sha256 manifiesto.json; do
    [ -s "$tmp/dist/$f" ] || { echo "package-voz.sh did not leave $f next to the package" >&2; exit 1; }
done
(cd "$tmp/dist" && sha256sum -c --quiet setup-voz.sh.sha256) || { echo "setup-voz.sh.sha256 does not match" >&2; exit 1; }
echo "paquete de Iris Voz: ok"
