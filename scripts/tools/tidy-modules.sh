#!/usr/bin/env bash
# Tidies and builds every Go module of the repo the way Docker and CI build them:
# each module ALONE, without go.work, with gen/go replaced by the local path.
#
#   bash scripts/tools/tidy-modules.sh           fix go.mod / go.sum where they are stale
#   bash scripts/tools/tidy-modules.sh --check   change nothing; fail if any is stale
#
# Why it exists: with go.work every module resolves the dependencies of gen/go
# through the workspace, so `go build` works locally even when a service's own
# go.mod / go.sum lack an entry that its generated code now needs. The Docker
# build (GOWORK=off) and CI then fail with "missing go.sum entry". Regenerating
# protos (a new grpc-gateway file, a new import) is exactly what triggers it.
#
# The replace of gen/go is added only for the run and removed again, unless the
# module already carries one.
set -Eeuo pipefail

MODE="tidy"
case "${1:-}" in
  "") ;;
  --check) MODE="check" ;;
  *) echo "usage: $0 [--check]" >&2; exit 2 ;;
esac

GEN="github.com/7akoom/ride-platform/gen/go"
GEN_PATH="../../gen/go"

if [ ! -f go.work ] || [ ! -d gen/go ]; then
  echo "ABORT: run this from the ride-platform repo root." >&2
  exit 1
fi

MODULES=()
for dir in services/* infrastructure/gateway; do
  [ -f "$dir/go.mod" ] && MODULES+=("$dir")
done

has_gen_replace() { # is there already a replace for gen/go in this go.mod?
  GOWORK=off go mod edit -json | python3 -c '
import json, sys
mod = json.load(sys.stdin)
sys.exit(0 if any(r["Old"]["Path"] == sys.argv[1] for r in mod.get("Replace") or []) else 1)
' "$GEN"
}

STALE=()
FAILED=()

for dir in "${MODULES[@]}"; do
  BACKUP="$(mktemp -d)"
  cp "$dir/go.mod" "$BACKUP/go.mod"
  if [ -f "$dir/go.sum" ]; then cp "$dir/go.sum" "$BACKUP/go.sum"; else : > "$BACKUP/go.sum.absent"; fi

  restore() {
    cp "$BACKUP/go.mod" "$dir/go.mod"
    if [ -f "$BACKUP/go.sum" ]; then cp "$BACKUP/go.sum" "$dir/go.sum"; else rm -f "$dir/go.sum"; fi
  }

  HAD_REPLACE=no
  if (cd "$dir" && has_gen_replace); then HAD_REPLACE=yes; fi

  if (
    cd "$dir"

    if [ "$HAD_REPLACE" = no ]; then
      GOWORK=off go mod edit -replace "$GEN=$GEN_PATH"
    fi

    GOWORK=off go mod tidy
    GOWORK=off go build ./...
  ) > "$BACKUP/output.log" 2>&1; then
    # Drop the temporary replace again, unless the module already had one.
    if [ "$HAD_REPLACE" = no ]; then
      (cd "$dir" && GOWORK=off go mod edit -dropreplace "$GEN")
    fi

    if cmp -s "$BACKUP/go.mod" "$dir/go.mod" && { [ ! -f "$BACKUP/go.sum" ] && [ ! -f "$dir/go.sum" ] || cmp -s "$BACKUP/go.sum" "$dir/go.sum"; }; then
      printf '  ok       %s\n' "$dir"
    else
      STALE+=("$dir")

      if [ "$MODE" = check ]; then
        printf '  STALE    %s (go.mod / go.sum need `go mod tidy`)\n' "$dir"
        restore
      else
        printf '  updated  %s\n' "$dir"
      fi
    fi
  else
    FAILED+=("$dir")
    printf '  FAILED   %s\n' "$dir"
    sed 's/^/           /' "$BACKUP/output.log" | tail -15
    restore
  fi

  rm -rf "$BACKUP"
done

echo

if [ "${#FAILED[@]}" -gt 0 ]; then
  echo "FAIL: ${#FAILED[@]} module(s) do not build standalone; their go.mod and go.sum were left untouched." >&2
  exit 1
fi

if [ "$MODE" = check ] && [ "${#STALE[@]}" -gt 0 ]; then
  echo "FAIL: ${#STALE[@]} module(s) have a stale go.mod / go.sum. Run: bash scripts/tools/tidy-modules.sh" >&2
  exit 1
fi

if [ "${#STALE[@]}" -gt 0 ]; then
  echo "Updated ${#STALE[@]} module(s). Stage only their go.mod and go.sum:"
  for dir in "${STALE[@]}"; do echo "  git add $dir/go.mod $dir/go.sum"; done
else
  echo "All ${#MODULES[@]} modules are tidy and build standalone."
fi
