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
            and (.value.user == "65532:65532")))
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
echo "paquete de Iris Voz: ok"
