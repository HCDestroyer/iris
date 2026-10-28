#!/usr/bin/env bash
# Trae `main` a una rama de agente en un worktree temporal, resuelve lo mecánico y verifica.
#
#   scripts/integrar.sh agente/i3            # mezcla y verifica, no sube nada
#   scripts/integrar.sh agente/i3 --push     # además sube la rama si todo pasa
#
# Resuelve solo conflictos mecánicos:
#   - .claude/integration/*.md                         → unión de ambos lados
#   - pnpm-lock.yaml, packages/contratos/*, go.sum     → se regeneran (nunca a mano)
# Cualquier otro conflicto detiene el script y deja el worktree para resolverlo a mano.
#
# Verifica con make lint, make typecheck y make test, y que los contratos no cambien al
# regenerarlos. Base de pruebas: IRIS_DB (por defecto iris_test_integracion; debe empezar por
# iris_test_) en PGHOST/PGPORT/PGUSER/PGPASSWORD (por defecto localhost:5432, iris/iris). Se borra y
# se crea de nuevo, y las pruebas la reciben como IRIS_DB_URL. PORT por defecto 3590.
set -euo pipefail

rama="${1:?uso: scripts/integrar.sh <rama> [--push]}"
subir="${2:-}"
raiz="$(git rev-parse --show-toplevel)"

export IRIS_DB="${IRIS_DB:-iris_test_integracion}" PORT="${PORT:-3590}"
case "$IRIS_DB" in
    iris_test_*) ;;
    *) echo "IRIS_DB debe empezar por iris_test_ (se borra y se recrea): $IRIS_DB" >&2; exit 1 ;;
esac
export PGHOST="${PGHOST:-localhost}" PGPORT="${PGPORT:-5432}" PGUSER="${PGUSER:-iris}" PGPASSWORD="${PGPASSWORD:-iris}"
export IRIS_DB_URL="postgres://$PGUSER:$PGPASSWORD@$PGHOST:$PGPORT/$IRIS_DB?sslmode=disable"

# Refspecs explícitos: un clon de una sola rama no trae origin/<rama> con un fetch a secas.
git -C "$raiz" fetch -q origin "+refs/heads/main:refs/remotes/origin/main" "+refs/heads/$rama:refs/remotes/origin/$rama"
git -C "$raiz" rev-parse -q --verify "origin/$rama" >/dev/null || { echo "No existe origin/$rama" >&2; exit 1; }
dir="$(mktemp -d "${TMPDIR:-/tmp}/integrar-XXXXXX")"
git -C "$raiz" worktree add -q --detach "$dir" "origin/$rama"
cd "$dir"
echo "== $rama ($(git rev-parse --short HEAD)) ← origin/main ($(git rev-parse --short origin/main))"

if git merge-base --is-ancestor origin/main HEAD; then
    echo "La rama ya contiene main."
elif ! git merge -q --no-edit origin/main -m "merge: main en $rama" >/dev/null 2>&1; then
    for f in $(git diff --name-only --diff-filter=U); do
        case "$f" in
            .claude/integration/*.md)
                base="$(mktemp)"; nuestro="$(mktemp)"; suyo="$(mktemp)"
                git show ":1:$f" >"$base" 2>/dev/null || : >"$base"
                git show ":2:$f" >"$nuestro"; git show ":3:$f" >"$suyo"
                git merge-file --union "$nuestro" "$base" "$suyo" || true
                cp "$nuestro" "$f"; git add "$f"
                rm -f "$base" "$nuestro" "$suyo" ;;
            pnpm-lock.yaml | packages/contratos/* | apps/api/go.sum)
                # Cualquier lado sirve: se regenera abajo
                git checkout --ours "$f"; git add "$f" ;;
            *) echo "Conflicto que requiere decisión: $f" ;;
        esac
    done
    if [ -n "$(git diff --name-only --diff-filter=U)" ]; then
        echo "Quedan conflictos reales. Worktree en $dir para resolverlos."
        exit 1
    fi
    git commit -q --no-edit
fi

# Base de pruebas recién creada: una base usada por otra rama puede tener migraciones que esta
# rama no conoce.
if command -v createdb >/dev/null 2>&1; then
    dropdb --if-exists "$IRIS_DB" && createdb "$IRIS_DB" && echo "Base de pruebas $IRIS_DB recreada."
else
    echo "Aviso: sin dropdb/createdb; se usa $IRIS_DB tal como esté." >&2
fi

# Regenerar lo generado: lockfile, go.sum y contratos (openapi.json solo si existe apps/api)
pnpm install >/dev/null
if [ -f apps/api/go.mod ]; then
    (cd apps/api && go mod tidy)
fi
make --no-print-directory contratos >/dev/null
if [ -n "$(git status --porcelain -- packages/contratos pnpm-lock.yaml apps/api/go.mod apps/api/go.sum)" ]; then
    git add packages/contratos pnpm-lock.yaml
    if [ -f apps/api/go.mod ]; then git add apps/api/go.mod; fi
    if [ -f apps/api/go.sum ]; then git add apps/api/go.sum; fi
    git commit -q -m "chore(contratos): regenerar tras traer main"
    echo "Contratos, lockfile y go.sum regenerados."
fi

fallo=0
for c in lint typecheck test; do
    if make --no-print-directory "$c" >"$dir/.integrar-$c.log" 2>&1; then
        echo "  $c: ok"
    else
        echo "  $c: FALLA (ver $dir/.integrar-$c.log)"; fallo=1
    fi
done
# Lo omitido (p. ej. Go sin apps/api) queda dicho en el resumen
grep -h "omitido" "$dir"/.integrar-*.log 2>/dev/null | sed 's/^/  /' || true

if [ "$fallo" = 1 ]; then
    echo "No se sube nada. Worktree en $dir"
    exit 1
fi
if [ "$subir" = "--push" ]; then
    git push -q origin "HEAD:$rama"
    echo "Subida $rama → $(git rev-parse --short HEAD)"
fi
cd "$raiz"
git worktree remove --force "$dir"
echo "Listo."
