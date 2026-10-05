#!/usr/bin/env bash
# Runs every e2e script built on the legacy fixtures and prints a table.
# Run from the repo root:
#   bash scripts/e2e/lib/run-legacy.sh                 all of them
#   bash scripts/e2e/lib/run-legacy.sh test-trip-authz  only these
# Each one's output is kept in /tmp/e2e-legacy/<name>.log. Exit 1 if any fails.
set -uo pipefail

LOGS=/tmp/e2e-legacy
mkdir -p "$LOGS"

if [ "$#" -gt 0 ]; then
  SCRIPTS=()
  for name in "$@"; do SCRIPTS+=("scripts/e2e/${name%.sh}.sh"); done
else
  mapfile -t SCRIPTS < <(grep -l 'lib/legacy-fixtures.sh' scripts/e2e/*.sh | sort)
fi

# Run by hand only: they send a real SMS (SOS_TEST_PHONE) or need the SOS
# webhook sink container wired into trip-service's .env.
MANUAL="test-sos-alert test-sos-webhook"

bash scripts/e2e/lib/legacy-fixtures.sh || { echo "the fixtures could not be made" >&2; exit 1; }

failed=0
FAILED=()
printf '%-32s %-6s %s\n' SCRIPT RESULT SECONDS
for script in "${SCRIPTS[@]}"; do
  name="$(basename "$script" .sh)"
  if [ "$#" -eq 0 ] && [[ " $MANUAL " == *" $name "* ]]; then
    printf '%-32s %-6s %s\n' "$name" SKIP "run by hand (see the script's header)"
    continue
  fi
  start=$(date +%s)
  if timeout 420 bash "$script" > "$LOGS/$name.log" 2>&1; then
    result=PASS
  else
    result=FAIL
    failed=$((failed + 1))
    FAILED+=("$name")
  fi
  printf '%-32s %-6s %s\n' "$name" "$result" "$(( $(date +%s) - start ))"
done

echo
if [ "$failed" -gt 0 ]; then
  echo "$failed failed. Their output (the end of each) is in $LOGS/failed-tails.txt"
  for name in "${FAILED[@]}"; do echo "===== $name"; tail -40 "$LOGS/$name.log"; done > "$LOGS/failed-tails.txt"
  exit 1
fi
echo "all passed"
