#!/usr/bin/env bash
# Plays a driver, so the Rider app can be tried end to end without a Driver app.
# Run from the ride-platform repo root and leave it running:
#   bash scripts/dev/sim-driver.sh
#
# What it does, with the test driver (fe94a3d3...):
#   - goes online and keeps reporting a position
#   - when a trip is waiting, it parks about 100 m from the pickup so dispatch picks it
#   - when dispatch gives it a trip, it drives to the pickup, starts the trip, drives to the
#     destination in small steps (the rider sees the dot move) and completes the trip
# Stop it with Ctrl+C. It needs the local default DISPATCH_OFFER_TTL=0 (direct assignment).
set -uo pipefail
export LC_ALL=C # so awk always prints 36.19 and never 36,19

TOKEN="${INTERNAL_SERVICE_TOKEN:-dev-internal-service-token-change-me}"
DRV="fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
PROTOSET="/tmp/ride.binpb"
LOCATION_ADDR="localhost:50054"
TRIP_ADDR="localhost:50055"
WALLET_ADDR="localhost:50058"

# Where the driver is when nothing is happening (the middle of Erbil).
LAT="36.1911"
LNG="44.0092"

call() { # <address> <method> <json>
  grpcurl -plaintext -protoset "$PROTOSET" -H "authorization: Bearer $TOKEN" -d "$3" "$1" "$2" 2> /dev/null
}

sql() { # <container> <query>
  echo "$2" | docker exec -i "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -t -A' 2> /dev/null
}

report() { # <lat> <lng>: tells the platform where the driver is
  LAT="$1"
  LNG="$2"
  call "$LOCATION_ADDR" ride.location.v1.LocationService/UpdateLocation \
    "{\"entity_type\":\"ENTITY_TYPE_DRIVER\",\"entity_id\":\"$DRV\",\"coordinates\":{\"latitude\":$1,\"longitude\":$2}}" > /dev/null
}

drive() { # <to lat> <to lng> <steps>: straight line, one position report a second
  local to_lat="$1" to_lng="$2" steps="$3" i
  local from_lat="$LAT" from_lng="$LNG"

  for i in $(seq 1 "$steps"); do
    report \
      "$(awk -v a="$from_lat" -v b="$to_lat" -v i="$i" -v n="$steps" 'BEGIN { printf "%.6f", a + (b - a) * i / n }')" \
      "$(awk -v a="$from_lng" -v b="$to_lng" -v i="$i" -v n="$steps" 'BEGIN { printf "%.6f", a + (b - a) * i / n }')"
    sleep 1
  done
}

trip_status() { # <trip id>
  sql ride-trip-postgres "select status from trips where id='$1';"
}

echo "==> preparing the driver"
buf build -o "$PROTOSET" || { echo "buf build failed: run this from the repo root" >&2; exit 1; }

sql ride-driver-postgres "update drivers set status='active', availability_status='available' where id='$DRV';" > /dev/null

if [ "$(sql ride-wallet-postgres "select coalesce(sum(balance),0) < 50000 from wallets where owner_id='$DRV' and owner_type='driver';")" = "t" ]; then
  call "$WALLET_ADDR" ride.wallet.v1.WalletService/TopUp \
    "{\"owner_type\":\"OWNER_TYPE_DRIVER\",\"owner_id\":\"$DRV\",\"amount\":\"100000\",\"idempotency_key\":\"sim-driver-$(date +%s)\",\"description\":\"sim driver deposit\"}" > /dev/null
fi

report "$LAT" "$LNG"
echo "==> driver online near $LAT,$LNG. Request a trip in the Rider app. Ctrl+C to stop."

while true; do
  row="$(sql ride-trip-postgres "select id || '|' || status || '|' || pickup_latitude || '|' || pickup_longitude || '|' || dropoff_latitude || '|' || dropoff_longitude from trips where driver_id='$DRV' and status in ('accepted','in_progress') order by created_at desc limit 1;")"

  if [ -n "$row" ]; then
    IFS='|' read -r trip status p_lat p_lng d_lat d_lng <<< "$row"

    if [ "$status" = "accepted" ]; then
      echo "==> got trip $trip: driving to the pickup"
      drive "$p_lat" "$p_lng" 10

      if [ "$(trip_status "$trip")" = "accepted" ]; then
        sleep 3
        echo "==> starting the trip"
        call "$TRIP_ADDR" ride.trip.v1.TripService/StartTrip "{\"trip_id\":\"$trip\"}" > /dev/null
        status="in_progress"
      else
        echo "==> the trip was cancelled"
        continue
      fi
    fi

    if [ "$status" = "in_progress" ]; then
      echo "==> driving to the destination"
      drive "$d_lat" "$d_lng" 15

      if [ "$(trip_status "$trip")" = "in_progress" ]; then
        echo "==> completing the trip"
        call "$TRIP_ADDR" ride.trip.v1.TripService/CompleteTrip "{\"trip_id\":\"$trip\"}" > /dev/null
        echo "==> done. Waiting for the next trip."
        sleep 3
      fi
    fi

    continue
  fi

  # No trip yet: if one is waiting for a driver, stand near its pickup so dispatch finds us.
  waiting="$(sql ride-trip-postgres "select pickup_latitude || '|' || pickup_longitude from trips where status='requested' and driver_id is null order by created_at desc limit 1;")"

  if [ -n "$waiting" ]; then
    IFS='|' read -r w_lat w_lng <<< "$waiting"
    report "$(awk -v a="$w_lat" 'BEGIN { printf "%.6f", a + 0.0009 }')" "$w_lng"
  else
    report "$LAT" "$LNG"
  fi

  sleep 3
done
