#!/usr/bin/env bash
# The self-hosted Nominatim (address and place search), in one place.
#   bash scripts/tools/nominatim.sh start    start it (the first start imports Iraq: an hour or more)
#   bash scripts/tools/nominatim.sh status   is it importing, is it ready
#   bash scripts/tools/nominatim.sh logs     follow the import
#   bash scripts/tools/nominatim.sh check    look up places in Erbil, in Arabic, Kurdish and English,
#                                            to judge how good the OpenStreetMap data is there
#   bash scripts/tools/nominatim.sh stop     stop it (the data stays)
set -Eeuo pipefail

URL="${NOMINATIM_URL:-http://127.0.0.1:8088}"

compose() { (cd infrastructure/compose && docker compose --profile maps "$@"); }

search() { # <query> [extra parameters]
  curl -fsS -G "$URL/search" --data-urlencode "q=$1" -d "format=jsonv2&limit=3&countrycodes=iq&accept-language=ar,ku,en" ${2:+-d "$2"} \
    | python3 -c '
import json, sys
results = json.load(sys.stdin)
if not results:
    print("    (nothing found)")
for r in results:
    print("    %-14s %-40s %s, %s" % (r.get("category", "?") + "/" + r.get("type", "?"), r.get("display_name", "?")[:40], r["lat"][:8], r["lon"][:8]))
'
}

reverse() { # <lat> <lon>
  curl -fsS -G "$URL/reverse" -d "lat=$1&lon=$2&format=jsonv2&zoom=18&accept-language=ar,ku,en" \
    | python3 -c '
import json, sys
r = json.load(sys.stdin)
print("    %s" % r.get("display_name", r.get("error", "(nothing)")))
'
}

case "${1:-}" in
  start)
    compose up -d nominatim
    echo "started. The first start downloads and imports the Iraq extract; follow it with:"
    echo "  bash scripts/tools/nominatim.sh logs"
    ;;

  status)
    docker ps --filter name=ride-nominatim --format '{{.Names}}  {{.Status}}'
    if curl -fsS --max-time 5 "$URL/status?format=json" > /dev/null 2>&1; then
      echo "ready: $(curl -fsS "$URL/status?format=json")"
    else
      echo "not ready (importing, or not started). Last log lines:"
      docker logs --tail 5 ride-nominatim 2>&1 | sed 's/^/    /'
    fi
    ;;

  logs)
    docker logs -f --tail 20 ride-nominatim
    ;;

  check)
    curl -fsS --max-time 5 "$URL/status" > /dev/null || { echo "Nominatim is not ready yet: bash scripts/tools/nominatim.sh status" >&2; exit 1; }

    echo "== by name, in Arabic, Kurdish and English =="
    for query in "أربيل" "هەولێر" "Erbil" "قلعة أربيل" "مطار أربيل الدولي" "عنكاوا" "Family Mall" "Majidi Mall" "شارع 60" "بغداد" "السليمانية" "الموصل"; do
      echo "  $query"
      search "$query"
    done

    echo "== what is at these points (Erbil centre, the airport road, Ankawa) =="
    reverse 36.1911 44.0092
    reverse 36.2367 43.9631
    reverse 36.2286 43.9750
    ;;

  stop)
    compose stop nominatim
    ;;

  *)
    sed -n '2,9p' "$0" >&2
    exit 2
    ;;
esac
