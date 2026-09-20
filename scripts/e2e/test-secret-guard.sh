#!/usr/bin/env bash
# Checks the secret guard on every service that carries an internal service
# token, by building each binary and starting it with a scrubbed environment.
# Run from the ride-platform repo root:
#   bash scripts/e2e/test-secret-guard.sh
#
# It needs no database, NATS or containers: the guard runs first thing at
# startup. What it proves, per service:
#   1. ENVIRONMENT=production with the placeholder token  -> refuses to start
#   2. ENVIRONMENT=production with a short token          -> refuses to start
#   3. ENVIRONMENT=production with a 64 character secret  -> gets past the guard
#      (it then stops at the first missing dependency, which is fine here)
#   4. no ENVIRONMENT (development) with the placeholder  -> gets past the guard
#      (local development keeps working exactly as before)
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

GUARD_MESSAGE="refusing to start with an unsafe configuration"
STRONG_TOKEN="$(printf 'f%.0s' $(seq 1 64))"
BIN_DIR="$(mktemp -d)"
trap 'rm -rf "$BIN_DIR"' EXIT

FAILURES=0
CHECKS=0

SERVICES=()
for cfg in services/*/internal/config/config.go; do
  [ -f "$cfg" ] || continue

  if grep -q 'InternalServiceToken' "$cfg"; then
    SERVICES+=("$(dirname "$(dirname "$(dirname "$cfg")")")")
  fi
done

[ "${#SERVICES[@]}" -gt 0 ] || { echo "FAIL: no service with an InternalServiceToken found" >&2; exit 1; }

# run_service <binary> [VAR=value ...] -> prints "<exit code>|<combined output>"
run_service() {
  local binary="$1"
  shift

  local output code

  # A refusal is a non-zero exit; testing it in an "if" keeps set -e and the
  # ERR trap out of the way.
  if output="$(env -i PATH="$PATH" HOME="$HOME" "$@" timeout 8 "$binary" 2>&1)"; then
    code=0
  else
    code=$?
  fi

  printf '%s|%s' "$code" "$output"
}

check() { # <label> <expect: refuse|pass> <result from run_service>
  local label="$1" expect="$2" result="$3"
  local code="${result%%|*}" output="${result#*|}"
  local refused=no

  grep -q "$GUARD_MESSAGE" <<<"$output" && refused=yes
  CHECKS=$((CHECKS + 1))

  if [ "$expect" = refuse ]; then
    if [ "$refused" = yes ] && [ "$code" = 1 ]; then
      printf '  ok    %s -> refused (exit 1)\n' "$label"
    else
      printf '  FAIL  %s -> expected a refusal, got exit=%s\n' "$label" "$code"
      FAILURES=$((FAILURES + 1))
    fi
  else
    if [ "$refused" = no ]; then
      printf '  ok    %s -> got past the guard\n' "$label"
    else
      printf '  FAIL  %s -> the guard refused a configuration it should accept\n' "$label"
      FAILURES=$((FAILURES + 1))
    fi
  fi
}

echo "==> building ${#SERVICES[@]} services"
BINARIES=()
for svc in "${SERVICES[@]}"; do
  mains=("$svc"/cmd/*/)
  name="$(basename "$svc")"
  (cd "$svc" && go build -o "$BIN_DIR/$name" "./cmd/$(basename "${mains[0]}")")
  BINARIES+=("$BIN_DIR/$name")
done

for binary in "${BINARIES[@]}"; do
  echo "==> $(basename "$binary")"
  check "production with the placeholder token"  refuse "$(run_service "$binary" ENVIRONMENT=production)"
  check "production with a short token"          refuse "$(run_service "$binary" ENVIRONMENT=production INTERNAL_SERVICE_TOKEN=short)"
  check "production with a 64 character secret"  pass   "$(run_service "$binary" ENVIRONMENT=production INTERNAL_SERVICE_TOKEN="$STRONG_TOKEN")"
  check "development with the placeholder token" pass   "$(run_service "$binary")"
done

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: all ${#SERVICES[@]} services refuse an unsafe internal token outside development ($CHECKS checks)"
else
  echo "FAIL: $FAILURES of $CHECKS check(s) failed" >&2
  exit 1
fi
