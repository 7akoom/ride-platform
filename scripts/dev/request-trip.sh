#!/usr/bin/env bash
# Requests a trip for the test rider, starting right next to a driver, so the Driver app can
# be tried without a Rider app. Run from the ride-platform repo root:
#   bash scripts/dev/request-trip.sh "<driver name>" [cash|wallet]
# The driver must be online in the Driver app (the platform must know where they are).
# The trip starts at the driver's position and goes about 1.5 km north-east. It is handed to
# the driver directly, or offered to them if the platform is set to offers.
set -uo pipefail
export LC_ALL=C.UTF-8

TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
PROTOSET="/tmp/ride.binpb"
RIDER_ADDR="localhost:50052"
LOCATION_ADDR="localhost:50054"
TRIP_ADDR="localhost:50055"
RIDER_IDENTITY="a0000000-0000-4000-8000-0000000000a1"

NAME="${1:-}"
PAYMENT="${2:-cash}"

if [ -z "$NAME" ]; then
  echo "usage: bash scripts/dev/request-trip.sh \"<driver name>\" [cash|wallet]" >&2
  exit 1
fi

case "$PAYMENT" in
  cash | wallet) ;;
  *) echo "the payment must be cash or wallet" >&2; exit 1 ;;
esac

grpc() { # <address> <method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $TOKEN" -d "$3" "$1" "$2"
}

sql() { # <container> <query>
  echo "$2" | docker exec -i "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -t -A'
}

buf build -o "$PROTOSET" || { echo "buf build failed: run this from the repo root" >&2; exit 1; }

NAME_SQL="${NAME//\'/\'\'}"
DRIVER="$(sql ride-driver-postgres "select id from drivers where display_name = '$NAME_SQL' order by created_at desc limit 1;")"
[ -n "$DRIVER" ] || { echo "no driver is called \"$NAME\"" >&2; exit 1; }

POSITION="$(grpc "$LOCATION_ADDR" ride.location.v1.LocationService/GetLocation \
  "{\"entity_type\":\"ENTITY_TYPE_DRIVER\",\"entity_id\":\"$DRIVER\"}" 2> /dev/null \
  | python3 -c 'import json,sys; c=json.load(sys.stdin)["coordinates"]; print(c["latitude"], c["longitude"])' 2> /dev/null || true)"

if [ -z "$POSITION" ]; then
  echo "the platform does not know where \"$NAME\" is: press \"ابدأ العمل\" in the Driver app first" >&2
  exit 1
fi

read -r LAT LNG <<< "$POSITION"
DROP_LAT="$(awk -v a="$LAT" 'BEGIN { printf "%.6f", a + 0.010 }')"
DROP_LNG="$(awk -v a="$LNG" 'BEGIN { printf "%.6f", a + 0.010 }')"

RIDER="$(grpc "$RIDER_ADDR" ride.rider.v1.RiderService/GetRiderByIdentity "{\"identity_id\":\"$RIDER_IDENTITY\"}" 2> /dev/null \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["rider"]["id"])' 2> /dev/null || true)"

if [ -z "$RIDER" ]; then
  grpc "$RIDER_ADDR" ride.rider.v1.RiderService/CreateRider \
    "{\"identity_id\":\"$RIDER_IDENTITY\",\"display_name\":\"Test Rider\"}" > /dev/null || { echo "could not create the test rider" >&2; exit 1; }
  RIDER="$(grpc "$RIDER_ADDR" ride.rider.v1.RiderService/GetRiderByIdentity "{\"identity_id\":\"$RIDER_IDENTITY\"}" \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["rider"]["id"])')"
fi

# a trip the test rider left unfinished would block a new one
for id in $(sql ride-trip-postgres "select id from trips where rider_id = '$RIDER' and status in ('requested','accepted','in_progress');"); do
  grpc "$TRIP_ADDR" ride.trip.v1.TripService/CancelTrip "{\"trip_id\":\"$id\",\"reason\":\"test cleanup\"}" > /dev/null 2>&1 || true
done

echo "requesting a $PAYMENT trip from $LAT,$LNG to $DROP_LAT,$DROP_LNG"

if ! OUT="$(grpc "$TRIP_ADDR" ride.trip.v1.TripService/RequestTrip \
  "{\"rider_id\":\"$RIDER\",\"pickup\":{\"latitude\":$LAT,\"longitude\":$LNG},\"dropoff\":{\"latitude\":$DROP_LAT,\"longitude\":$DROP_LNG},\"vehicle_class\":\"economy\",\"payment_method\":\"$PAYMENT\"}" 2>&1)"; then
  echo "the platform refused the trip:" >&2
  echo "$OUT" >&2
  exit 1
fi

echo "trip requested: $(sql ride-trip-postgres "select id from trips where rider_id = '$RIDER' order by created_at desc limit 1;")"
echo "Look at the Driver app: within a few seconds it shows the trip."
