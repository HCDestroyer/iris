#!/usr/bin/env bash
# Creates the tag voz-vX.Y.Z of «Iris Voz» from infra/voz/manifiesto.json (voz-etiqueta.yml).
#
#   infra/voz/etiquetar.sh
#
# Runs only on main, in the job of the `voz-etiqueta` environment (deployment limited to main), the
# only place that receives the key of the GitHub App allowed to create voz-v* tags. A tag pushed with
# that App's token fires voz-release.yml (`on: push: tags`), which still waits for the approval of
# the `voz-release` environment before signing and publishing.
#
# Environment:
#   GH_TOKEN             GITHUB_TOKEN (contents: read): reads refs and the manifest of a tag.
#   VOZ_APP_ID           ID (or Client ID) of the GitHub App that creates the tags.
#   VOZ_APP_KEY          its private key (PEM).
#   GITHUB_REPOSITORY, GITHUB_SHA, GITHUB_REF, GITHUB_API_URL (set by GitHub Actions).
#
# Rules (it fails closed):
#   - version must be X.Y.Z and GITHUB_SHA must be on main (GITHUB_REF = refs/heads/main).
#   - If voz-vX.Y.Z exists and its manifest is the same as this one: nothing to do.
#   - If it exists with another manifest: error (the manifest changed without raising version).
#   - If it does not exist, X.Y.Z must be greater than every voz-v* tag.
set -euo pipefail
shopt -s inherit_errexit

aqui="$(cd "$(dirname "$0")" && pwd)"
API="${GITHUB_API_URL:-https://api.github.com}"

die() { echo "::error::$*" >&2; exit 1; }

# Cleanup on exit: revokes the App token (if any) and removes the key, JWT and token files.
TMP=""
REVOCAR=0
limpiar() {
    if [ "$REVOCAR" = 1 ]; then api_con "$TMP/token" -X DELETE "$API/installation/token" > /dev/null 2>&1 || true; fi
    if [ -n "$TMP" ]; then rm -rf "$TMP"; fi
}

b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }

# jwt_app <app id> <private key file>: JWT (RS256, 9 minutes) to authenticate as the GitHub App.
jwt_app() {
    local id="$1" llave="$2" ahora cab carga firma
    [[ "$id" =~ ^[A-Za-z0-9.]+$ ]] || die "VOZ_APP_ID no es un ID de GitHub App"
    ahora="$(date +%s)"
    cab="$(printf '{"alg":"RS256","typ":"JWT"}' | b64url)"
    carga="$(printf '{"iat":%d,"exp":%d,"iss":"%s"}' "$((ahora - 60))" "$((ahora + 540))" "$id" | b64url)"
    firma="$(printf '%s.%s' "$cab" "$carga" | openssl dgst -sha256 -sign "$llave" -binary | b64url)"
    [ -n "$firma" ] || die "no se pudo firmar el JWT con VOZ_APP_KEY"
    printf '%s.%s.%s' "$cab" "$carga" "$firma"
}

# api_con <header file> <curl args...>: GitHub REST with the Authorization header in a file (not argv).
api_con() {
    local cabecera="$1"
    shift
    curl -fsS --proto '=https' --retry 3 -H @"$cabecera" -H 'Accept: application/vnd.github+json' \
        -H 'X-GitHub-Api-Version: 2022-11-28' "$@"
}

# commit_de <ref json>: the commit a tag ref points to (dereferences an annotated tag).
commit_de() {
    local tipo sha
    tipo="$(jq -r .object.type <<< "$1")"
    sha="$(jq -r .object.sha <<< "$1")"
    if [ "$tipo" = tag ]; then sha="$(gh api "repos/$GITHUB_REPOSITORY/git/tags/$sha" --jq .object.sha)"; fi
    printf '%s' "$sha"
}

main() {
    : "${GITHUB_REPOSITORY:?}" "${GITHUB_SHA:?}" "${GITHUB_REF:?}" "${GH_TOKEN:?}"
    [ "$GITHUB_REF" = refs/heads/main ] || die "solo se etiqueta desde main (esto es $GITHUB_REF)"
    [[ "$GITHUB_SHA" =~ ^[0-9a-f]{40}$ ]] || die "GITHUB_SHA inválido"

    local manifiesto="$aqui/manifiesto.json" version etiqueta estado refs existente mayor
    version="$(jq -r .version "$manifiesto")"
    [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "version inválida en manifiesto.json: $version"
    etiqueta="voz-v$version"

    # The commit must be on main (identical to it or behind it), never on another branch.
    estado="$(gh api "repos/$GITHUB_REPOSITORY/compare/main...$GITHUB_SHA" --jq .status)" ||
        die "no se pudo comparar $GITHUB_SHA con main"
    case "$estado" in
        identical | behind) ;;
        *) die "el commit $GITHUB_SHA no está en main ($estado)" ;;
    esac

    refs="$(gh api --paginate "repos/$GITHUB_REPOSITORY/git/matching-refs/tags/voz-v" --jq '.[]' | jq -s .)" ||
        die "no se pudieron leer las etiquetas voz-v*"
    existente="$(jq -c --arg r "refs/tags/$etiqueta" 'map(select(.ref == $r)) | .[0] // empty' <<< "$refs")"

    if [ -n "$existente" ]; then
        local sha_etiqueta en_etiqueta
        sha_etiqueta="$(commit_de "$existente")"
        [[ "$sha_etiqueta" =~ ^[0-9a-f]{40}$ ]] || die "no se pudo leer el commit de $etiqueta"
        en_etiqueta="$(gh api "repos/$GITHUB_REPOSITORY/contents/infra/voz/manifiesto.json?ref=$sha_etiqueta" --jq .content | base64 -d)" ||
            die "no se pudo leer el manifiesto de $etiqueta"
        if [ "$(jq -S . <<< "$en_etiqueta")" = "$(jq -S . "$manifiesto")" ]; then
            echo "$etiqueta ya existe ($sha_etiqueta) con este mismo manifiesto: nada que hacer"
            echo "La etiqueta $etiqueta ya existía; no se creó otra." >> "${GITHUB_STEP_SUMMARY:-/dev/null}"
            return 0
        fi
        die "$etiqueta ya existe en $sha_etiqueta con otro manifiesto: sube version en infra/voz/manifiesto.json"
    fi

    mayor="$(jq -r '.[].ref | ltrimstr("refs/tags/voz-v") | select(test("^[0-9]+\\.[0-9]+\\.[0-9]+$"))' <<< "$refs" | sort -V | tail -n1)"
    if [ -n "$mayor" ] && [ "$(printf '%s\n%s\n' "$mayor" "$version" | sort -V | tail -n1)" != "$version" ]; then
        die "la versión $version no es mayor que la última publicada ($mayor)"
    fi

    : "${VOZ_APP_ID:?falta el secreto VOZ_APP_ID del entorno voz-etiqueta (docs/runbooks/servidor-ia.md §8)}"
    : "${VOZ_APP_KEY:?falta el secreto VOZ_APP_KEY del entorno voz-etiqueta (docs/runbooks/servidor-ia.md §8)}"

    local tmp jwt instalacion token cuerpo
    TMP="$(umask 077 && mktemp -d)"
    tmp="$TMP"
    trap limpiar EXIT
    printf '%s\n' "$VOZ_APP_KEY" > "$tmp/llave.pem"
    jwt="$(jwt_app "$VOZ_APP_ID" "$tmp/llave.pem")"
    echo "::add-mask::$jwt"
    printf 'Authorization: Bearer %s\n' "$jwt" > "$tmp/app"

    instalacion="$(api_con "$tmp/app" "$API/repos/$GITHUB_REPOSITORY/installation" | jq -r .id)" ||
        die "la GitHub App de VOZ_APP_ID no está instalada en $GITHUB_REPOSITORY (o la llave no es suya)"
    [[ "$instalacion" =~ ^[0-9]+$ ]] || die "no se pudo leer la instalación de la GitHub App"

    # Token of one repository and one permission, revoked at the end.
    cuerpo="$(jq -nc --arg r "${GITHUB_REPOSITORY#*/}" '{repositories: [$r], permissions: {contents: "write"}}')"
    token="$(api_con "$tmp/app" -X POST -d "$cuerpo" "$API/app/installations/$instalacion/access_tokens" | jq -r .token)" ||
        die "la GitHub App no pudo emitir un token (¿tiene Contents: Read and write?)"
    [ -n "$token" ] && [ "$token" != null ] || die "la GitHub App no devolvió un token"
    echo "::add-mask::$token"
    printf 'Authorization: Bearer %s\n' "$token" > "$tmp/token"
    REVOCAR=1

    cuerpo="$(jq -nc --arg r "refs/tags/$etiqueta" --arg s "$GITHUB_SHA" '{ref: $r, sha: $s}')"
    api_con "$tmp/token" -X POST -d "$cuerpo" "$API/repos/$GITHUB_REPOSITORY/git/refs" > /dev/null ||
        die "no se pudo crear $etiqueta: la regla de etiquetas voz-v* debe permitir a la GitHub App (docs/runbooks/servidor-ia.md §8)"

    echo "creada $etiqueta en $GITHUB_SHA: voz-release.yml construye y espera la aprobación de voz-release"
    {
        echo "Etiqueta **$etiqueta** creada en \`$GITHUB_SHA\`."
        echo
        echo "«Versión de Iris Voz» construye ahora y espera la aprobación del entorno \`voz-release\` para firmar y publicar."
    } >> "${GITHUB_STEP_SUMMARY:-/dev/null}"
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then main "$@"; fi
