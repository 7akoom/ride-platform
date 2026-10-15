#!/usr/bin/env bash
# End-to-end test of a person checking their own wallet PIN (the app asks for
# the current PIN first when changing it), through the gateway. Run from the
# ride-platform repo root:
#   bash scripts/e2e/test-wallet-pin-verify.sh
#
# Needs the local identity signing key (tokens are minted with
# scripts/tools/devtoken). It puts one person straight into the identity
# database and removes them again. No code is sent anywhere.
#
# What it proves:
#   1. without a token it is refused; with no PIN yet it says so
#   2. the right PIN is ok; a wrong one says how many tries are left
#   3. a wrong one here counts like any other: five lock the PIN (even the
#      right one then), and the status shows the lock
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
PERSON="$(uuid)"
SESSION="$(uuid)"
SUFFIX="$(printf '%08d' $(( $(date +%s) % 100000000 )))"
PHONE="+964799${SUFFIX:1}"

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -rf "$WORK"
  sql ride-identity-postgres "delete from identities where id = '$PERSON';" > /dev/null 2>&1 || true
}
trap cleanup EXIT

body_field() { # <python expression over d>
  python3 -c "import json,sys; d=json.load(open(sys.argv[1])); print($1)" "$BODY_FILE"
}

http() { # <method> <path> <token> [json body]
  local args=(-sS -o "$BODY_FILE" -w '%{http_code}' -X "$1" "$BASE$2")
  [ -n "$3" ] && args+=(-H "Authorization: Bearer $3")
  [ -n "${4:-}" ] && args+=(-H 'Content-Type: application/json' -d "$4")
  curl "${args[@]}"
}

expect() { # <label> <expected status> <method> <path> <token> [json body]
  local actual
  actual="$(http "$3" "$4" "$5" "${6:-}")"
  if [ "$actual" = "$2" ]; then
    printf '  ok    %s -> %s\n' "$1" "$actual"
  else
    printf '  FAIL  %s -> expected %s, got %s: %s\n' "$1" "$2" "$actual" "$(head -c 300 "$BODY_FILE")"
    FAILURES=$((FAILURES + 1))
  fi
}

check() { # <label> <expected> <actual>
  if [ "$2" = "$3" ]; then
    printf '  ok    %s -> %s\n' "$1" "$3"
  else
    printf '  FAIL  %s -> expected %s, got %s\n' "$1" "$2" "$3"
    FAILURES=$((FAILURES + 1))
  fi
}

verify() { # <pin>
  expect "verify $1" 200 POST /v1/me/wallet-pin:verify "$TOKEN" "{\"pin\":\"$1\"}"
}

sql ride-identity-postgres "
  insert into identities (id) values ('$PERSON');
  insert into identity_identifiers (identity_id, identifier_type, normalized_value, verified_at)
    values ('$PERSON', 'phone', '$PHONE', now());
  insert into auth_sessions (id, identity_id, expires_at) values ('$SESSION', '$PERSON', now() + interval '1 day');" > /dev/null
TOKEN="$(go run scripts/tools/devtoken/main.go -sub "$PERSON" -sid "$SESSION")"

echo "==> [1/3] who may ask"
expect "without a token" 401 POST /v1/me/wallet-pin:verify "" '{"pin":"2580"}'
verify 2580
check "no PIN yet" PIN_CHECK_NOT_SET "$(body_field 'd["result"]')"
expect "set 2580" 200 PUT /v1/me/wallet-pin "$TOKEN" '{"newPin":"2580"}'

echo "==> [2/3] right and wrong"
verify 2580
check "the right one" PIN_CHECK_OK "$(body_field 'd["result"]')"
verify 1357
check "a wrong one, with the tries left" "PIN_CHECK_WRONG 4" "$(body_field 'd["result"] + " " + str(d["attemptsLeft"])')"
verify 2580
check "the right one clears the count" "PIN_CHECK_OK 5" "$(body_field 'd["result"] + " " + str(d["attemptsLeft"])')"

echo "==> [3/3] it counts like any other"
for _ in 1 2 3 4; do http POST /v1/me/wallet-pin:verify "$TOKEN" '{"pin":"1357"}' > /dev/null; done
verify 1357
check "the fifth wrong one locks it" PIN_CHECK_LOCKED "$(body_field 'd["result"]')"
verify 2580
check "the right one is locked out too" PIN_CHECK_LOCKED "$(body_field 'd["result"]')"
expect "the status" 200 GET /v1/me/wallet-pin "$TOKEN"
check "shows the lock" True "$(body_field 'bool(d.get("lockedUntil"))')"

echo
if [ "$FAILURES" -gt 0 ]; then
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi

echo "PASS: a person checks their own PIN; wrong ones count toward the same lock"
