#!/usr/bin/env bash
# End-to-end test of the support desk's help centre, canned replies, ratings,
# first-response times and automatic resolve, through the gateway. Run from the
# ride-platform repo root:
#   bash scripts/e2e/test-support-help.sh
#
# Needs the local identity signing key and support-service with migration 00003
# (its worker running, SUPPORT_WORKER_INTERVAL at most 15s).
#
# What it proves:
#   1. riders see the help sections and published articles for them, search
#      them in Arabic, read one with its "contact us" category, and vote once
#   2. the owner writes a draft (hidden from riders) and publishes it; an agent
#      cannot write content
#   3. canned replies: agents list them, the ticket's category first; only the
#      owner changes them
#   4. a ticket carries its first-response time; a ticket left waiting for the
#      rider past 72h is resolved by the worker and the rider is told; the
#      rider rates it once
#   5. the stats count it
#
# Everything it creates is removed again.
set -Eeuo pipefail
trap 'echo "FAIL: the script stopped unexpectedly at line $LINENO" >&2' ERR

BASE="${GATEWAY_URL:-http://localhost:8080}"
OWNER_ROLE="5e7a0000-0000-4000-8000-000000000001"
AGENT_ROLE="5e7a0000-0000-4000-8000-000000000003"
RUN="$(date +%s)"
uuid() { python3 -c 'import uuid; print(uuid.uuid4())'; }
RIDER_IDENTITY="$(uuid)"
OTHER_IDENTITY="$(uuid)"
OWNER_IDENTITY="$(uuid)"
AGENT_IDENTITY="$(uuid)"
OWNER_STAFF_ID="$(uuid)"
AGENT_STAFF_ID="$(uuid)"
UNKNOWN="$(uuid)"
ARTICLE="e2e-help-$RUN"
MACRO="e2e_macro_$RUN"

FAILURES=0
WORK="$(mktemp -d)"
BODY_FILE="$WORK/body.json"
RIDER_ID=""

sql() { # <container> <query>
  docker exec "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"' _ "$2"
}

cleanup() {
  rm -rf "$WORK"
  sql ride-support-postgres "
    create temp table gone as select id from support_tickets where requester_identity_id = '$RIDER_IDENTITY';
    delete from outbox_events where aggregate_id in (select id from gone);
    delete from support_actions where ticket_id in (select id from gone);
    delete from support_attachments where ticket_id in (select id from gone);
    delete from support_messages where ticket_id in (select id from gone);
    delete from support_tickets where id in (select id from gone);
    delete from support_help_votes where identity_id in ('$RIDER_IDENTITY', '$OTHER_IDENTITY');
    delete from support_help_articles where key = '$ARTICLE';
    delete from support_macros where key = '$MACRO';" > /dev/null 2>&1 || true
  sql ride-staff-postgres "delete from staff_members where id in ('$OWNER_STAFF_ID', '$AGENT_STAFF_ID');" > /dev/null 2>&1 || true
  sql ride-notification-postgres "delete from notifications where recipient_id = '${RIDER_ID:-$UNKNOWN}';" > /dev/null 2>&1 || true
  sql ride-rider-postgres "delete from riders where identity_id = '$RIDER_IDENTITY';" > /dev/null 2>&1 || true
}
trap cleanup EXIT

mint() { go run scripts/tools/devtoken/main.go -sub "$1"; }

body_field() { # <python expression over d>
  python3 -c "import json,sys; d=json.load(open(sys.argv[1])); print($1)" "$BODY_FILE"
}

http() { # <method> <path> <token> [json body]
  local method="$1" path="$2" token="$3" body="${4:-}"
  local args=(-sS -o "$BODY_FILE" -w '%{http_code}' -X "$method" "$BASE$path")

  [ -n "$token" ] && args+=(-H "Authorization: Bearer $token")
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' -d "$body")

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

eventually() { # <label> <expected> <seconds> <command...>
  local label="$1" expected="$2" seconds="$3" actual=""
  shift 3

  for _ in $(seq 1 $((seconds * 2))); do
    actual="$("$@" 2>/dev/null || true)"
    [ "$actual" = "$expected" ] && break
    sleep 0.5
  done

  check "$label" "$expected" "$actual"
}

add_staff() { # <staff id> <identity> <role>
  sql ride-staff-postgres "
    insert into staff_members (id, identity_id, email, display_name, status, invited_at, activated_at)
    values ('$1', '$2', 'e2e-help-$1@ride.test', 'E2E Help', 'active', now(), now());
    insert into staff_member_roles (staff_id, role_id) values ('$1', '$3');" > /dev/null
}

article() { # <published true|false>
  echo "{\"article\":{\"key\":\"$ARTICLE\",\"sectionKey\":\"rides\",\"audience\":\"CATEGORY_AUDIENCE_RIDER\",\"titleEn\":\"E2E article $RUN\",\"titleAr\":\"مقالة اختبار $RUN\",\"titleKu\":\"وتاری تاقیکردنەوە $RUN\",\"bodyEn\":\"Body\",\"bodyAr\":\"نص\",\"bodyKu\":\"دەق\",\"contactCategoryKey\":\"other\",\"sortOrder\":999,\"published\":$1}}"
}

ticket_status() { sql ride-support-postgres "select status from support_tickets where id = '$1'"; }
notified() { sql ride-notification-postgres "select count(*) from notifications where recipient_id = '$1' and event_key = '$2'"; }

[ "$(sql ride-support-postgres "select to_regclass('public.support_help_articles') is not null" 2>/dev/null)" = t ] \
  || { echo "ABORT: the help centre tables do not exist: apply support-service migration 00003 (goose up)" >&2; exit 2; }

RIDER="$(mint "$RIDER_IDENTITY")"
OTHER="$(mint "$OTHER_IDENTITY")"
OWNER="$(mint "$OWNER_IDENTITY")"
AGENT="$(mint "$AGENT_IDENTITY")"

echo "==> [0/5] a rider, an owner and an agent"
add_staff "$OWNER_STAFF_ID" "$OWNER_IDENTITY" "$OWNER_ROLE"
add_staff "$AGENT_STAFF_ID" "$AGENT_IDENTITY" "$AGENT_ROLE"
expect "a rider" 200 POST /v1/riders "$RIDER" "{\"identityId\":\"$RIDER_IDENTITY\",\"displayName\":\"E2E Help Rider\"}"
RIDER_ID="$(body_field 'd["rider"]["id"]')"

echo "==> [1/5] the help centre for a rider"
expect "sections" 200 GET "/v1/support/help/sections?audience=AUDIENCE_RIDER" "$RIDER"
check "rides, no driving" "True False" "$(body_field '" ".join(str(any(s["key"] == k for s in d["sections"])) for k in ("rides", "driving"))')"
expect "articles" 200 GET "/v1/support/help/articles?audience=AUDIENCE_RIDER" "$RIDER"
check "lost item, no driver article, no bodies" "True False True" \
  "$(body_field '" ".join((str(any(a["key"] == "lost-item" for a in d["articles"])), str(any(a["key"] == "documents-expiring" for a in d["articles"])), str(all(not a.get("bodyEn") for a in d["articles"]))))')"
expect "a search in Arabic" 200 GET "/v1/support/help/articles?audience=AUDIENCE_RIDER&query=$(python3 -c 'import urllib.parse; print(urllib.parse.quote("المحفظة"))')" "$RIDER"
check "finds the wallet article" True "$(body_field 'any(a["key"] == "top-up-wallet" for a in d["articles"])')"
expect "a one-letter search" 400 GET "/v1/support/help/articles?audience=AUDIENCE_RIDER&query=a" "$RIDER"
expect "an article" 200 GET /v1/support/help/articles/lost-item "$RIDER"
check "with its body and contact category" "True lost_item" "$(body_field '" ".join((str(bool(d["article"]["bodyAr"])), d["article"]["contactCategoryKey"]))')"
BEFORE="$(body_field 'd["article"].get("helpfulCount", 0)')"
expect "not helpful" 200 POST /v1/support/help/articles/lost-item:rate "$RIDER" '{"helpful":false}'
expect "changed to helpful" 200 POST /v1/support/help/articles/lost-item:rate "$RIDER" '{"helpful":true}'
check "one helpful vote more, the rider's" "$((BEFORE + 1)) True True" "$(body_field '" ".join((str(d["article"].get("helpfulCount", 0)), str(d["article"].get("voted", False)), str(d["article"].get("votedHelpful", False))))')"
expect "an article that does not exist" 404 GET /v1/support/help/articles/nope-nope "$RIDER"
expect "without a token" 401 GET "/v1/support/help/sections?audience=AUDIENCE_RIDER" ""

echo "==> [2/5] writing content"
expect "an agent writes an article" 403 PUT "/v1/admin/support/help/articles/$ARTICLE" "$AGENT" "$(article true)"
expect "the owner writes a draft" 200 PUT "/v1/admin/support/help/articles/$ARTICLE" "$OWNER" "$(article false)"
expect "the rider reads the draft" 404 GET "/v1/support/help/articles/$ARTICLE" "$RIDER"
expect "the owner publishes it" 200 PUT "/v1/admin/support/help/articles/$ARTICLE" "$OWNER" "$(article true)"
expect "the rider reads it" 200 GET "/v1/support/help/articles/$ARTICLE" "$OTHER"
expect "an article in a section that does not exist" 400 PUT "/v1/admin/support/help/articles/$ARTICLE-x" "$OWNER" "$(article true | sed 's/"sectionKey":"rides"/"sectionKey":"nope"/; s/"key":"'"$ARTICLE"'"/"key":"'"$ARTICLE"'-x"/')"
expect "the owner lists every article" 200 GET /v1/admin/support/help/articles "$OWNER"
check "with bodies" True "$(body_field "any(a['key'] == '$ARTICLE' and a['bodyEn'] == 'Body' for a in d['articles'])")"

echo "==> [3/5] canned replies"
expect "an agent lists them for a lost item" 200 GET "/v1/admin/support/macros?category_key=lost_item" "$AGENT"
check "the lost-item one first" lost_item_driver "$(body_field 'd["macros"][0]["key"]')"
expect "an agent adds one" 403 PUT "/v1/admin/support/macros/$MACRO" "$AGENT" "{\"macro\":{\"key\":\"$MACRO\",\"title\":\"E2E\",\"bodyEn\":\"Hi\",\"bodyAr\":\"مرحباً\",\"bodyKu\":\"سڵاو\",\"active\":true}}"
expect "the owner adds one" 200 PUT "/v1/admin/support/macros/$MACRO" "$OWNER" "{\"macro\":{\"key\":\"$MACRO\",\"title\":\"E2E\",\"bodyEn\":\"Hi\",\"bodyAr\":\"مرحباً\",\"bodyKu\":\"سڵاو\",\"active\":false}}"
expect "the agent's list" 200 GET /v1/admin/support/macros "$AGENT"
check "has no inactive one" False "$(body_field "any(m['key'] == '$MACRO' for m in d['macros'])")"
expect "the owner's full list" 200 GET /v1/admin/support/macros:all "$OWNER"
check "has it" True "$(body_field "any(m['key'] == '$MACRO' for m in d['macros'])")"

echo "==> [4/5] first-response time, automatic resolve, rating"
expect "a ticket" 200 POST /v1/support/tickets "$RIDER" '{"audience":"AUDIENCE_RIDER","categoryKey":"other","body":"how do I change my name?"}'
TICKET="$(body_field 'd["ticket"]["id"]')"
check "due 4 hours after it opened" 14400 "$(sql ride-support-postgres "select extract(epoch from first_response_due_at - created_at)::int from support_tickets where id = '$TICKET'")"
check "not late" False "$(body_field 'd["ticket"].get("firstResponseLate", False)')"
expect "rating an open ticket" 400 POST "/v1/support/tickets/$TICKET:rate" "$RIDER" '{"rating":5}'
expect "the agent answers" 200 POST "/v1/admin/support/tickets/$TICKET/messages" "$AGENT" '{"body":"Send us your new name, please."}'
check "waiting for the rider" waiting_user "$(ticket_status "$TICKET")"
sql ride-support-postgres "update support_tickets set status_changed_at = now() - interval '73 hours' where id = '$TICKET';" > /dev/null
eventually "resolved by the worker" resolved 40 ticket_status "$TICKET"
eventually "the rider is told" 1 20 notified "$RIDER_ID" support.ticket_resolved
expect "a rating of 6" 400 POST "/v1/support/tickets/$TICKET:rate" "$RIDER" '{"rating":6}'
expect "the rider rates it 4" 200 POST "/v1/support/tickets/$TICKET:rate" "$RIDER" '{"rating":4,"comment":"fast, thanks"}'
check "rated" "4 fast, thanks" "$(body_field '" ".join((str(d["ticket"]["rating"]), d["ticket"]["ratingComment"]))')"
expect "rating it again" 400 POST "/v1/support/tickets/$TICKET:rate" "$RIDER" '{"rating":1}'
expect "the staff view" 200 GET "/v1/admin/support/tickets/$TICKET" "$AGENT"
check "shows the rating" 4 "$(body_field 'd["ticket"]["ticket"]["rating"]')"

echo "==> [5/5] stats"
expect "the stats, agent" 200 GET /v1/admin/support/stats "$AGENT"
check "count the rating and the answer" True "$(body_field 'd.get("ratings", 0) >= 1 and d.get("answered", 0) >= 1 and len(d["openByPriority"]) == 4')"
expect "a period the wrong way round" 400 GET "/v1/admin/support/stats?from=2026-10-02T00:00:00Z&to=2026-10-01T00:00:00Z" "$AGENT"
expect "the stats, a rider" 403 GET /v1/admin/support/stats "$RIDER"

echo
if [ "$FAILURES" -eq 0 ]; then
  echo "PASS: help centre, canned replies, first-response times, automatic resolve and ratings work end to end"
else
  echo "FAIL: $FAILURES check(s) failed" >&2
  exit 1
fi
