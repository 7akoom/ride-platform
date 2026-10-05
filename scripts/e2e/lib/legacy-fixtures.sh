#!/usr/bin/env bash
# Makes sure the fixed people the older e2e scripts are written around exist,
# so they also run on a fresh database (the old seed is gone since the
# rebuild). Safe to run any number of times. Run from the repo root:
#   bash scripts/e2e/lib/legacy-fixtures.sh
#
# What it ensures:
#   - drivers A (fe94a3d3-...) and B (4a1d17de-...): active, an approved and
#     active economy car, every required document approved (placeholders, no
#     files), offline; their names, cars and documents are put back if a test
#     or an account deletion changed them
#   - rider d586ce00-...: active
#   - Erbil (36.19, 44.01) is served: a throw-away "E2E Erbil" city and zone
#     are made only when no active zone covers it
# Development databases only: it refuses when driver-service says production.
set -Eeuo pipefail

DRIVER_A="fe94a3d3-f10d-4c3e-853d-3d1302a5feb5"
DRIVER_B="4a1d17de-9dfe-4fae-add5-d3ea6e2549e9"
RIDER="d586ce00-5c1c-46f1-81b5-ed7e0977d075"
# Identities nobody else uses: tokens for the drivers are minted for these.
IDENTITY_A="e2e0f1c5-0000-4000-8000-0000000000da"
IDENTITY_B="e2e0f1c5-0000-4000-8000-0000000000db"
IDENTITY_RIDER="e2e0f1c5-0000-4000-8000-0000000000c1"
OWNER_ROLE="5e7a0000-0000-4000-8000-000000000001"
BASE="${GATEWAY_URL:-http://localhost:8080}"

if grep -qs '^ENVIRONMENT=production' services/driver-service/.env; then
  echo "legacy-fixtures: refusing, services/driver-service/.env says ENVIRONMENT=production" >&2
  exit 1
fi

sql() { # <container> <query>
  docker exec -i "$1" sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 -q -t -A -1' <<< "$2"
}

driver() { # <id> <identity> <letter>
  local id="$1" identity="$2" letter="$3"
  local plate="FIX-DRV-$letter" car
  car="$(python3 -c "import uuid; print(uuid.uuid5(uuid.NAMESPACE_URL, 'e2e-fixture-car-$id'))")"

  sql ride-driver-postgres "
    INSERT INTO drivers (id, identity_id, display_name, status, availability_status,
                         vehicle_make, vehicle_model, vehicle_color, vehicle_plate_number, vehicle_class)
    VALUES ('$id', '$identity', 'E2E Driver $letter', 'active', 'offline',
            'Toyota', 'Corolla', 'White', '$plate', 'economy')
    ON CONFLICT (id) DO UPDATE SET
        status = 'active', rejection_reason = '', display_name = 'E2E Driver $letter',
        vehicle_make = 'Toyota', vehicle_model = 'Corolla', vehicle_color = 'White',
        vehicle_plate_number = '$plate', vehicle_class = 'economy',
        availability_status = CASE WHEN drivers.availability_status = 'busy' THEN 'offline' ELSE drivers.availability_status END;

    UPDATE vehicles SET active = false WHERE driver_id = '$id' AND id <> '$car' AND active;

    INSERT INTO vehicles (id, driver_id, make, model, color, plate_number, year, vehicle_class, status, active, reviewed_at)
    VALUES ('$car', '$id', 'Toyota', 'Corolla', 'White', '$plate', 2020, 'economy', 'approved', true, now())
    ON CONFLICT (id) DO UPDATE SET
        status = 'approved', active = true, retired_at = NULL, rejection_reason = '', plate_number = '$plate', vehicle_class = 'economy';

    UPDATE drivers SET vehicle_id = '$car', vehicle_year = 2020 WHERE id = '$id';

    INSERT INTO driver_documents (id, driver_id, type_code, media_id, document_number, expires_on, status, reviewed_at, vehicle_id)
    SELECT gen_random_uuid(), '$id', t.code, gen_random_uuid(),
           CASE WHEN t.requires_number THEN 'FIX-' || left(md5('$id' || t.code), 12) ELSE '' END,
           CASE WHEN t.requires_expiry THEN current_date + 365 END,
           'approved', now(),
           CASE WHEN t.scope = 'vehicle' THEN '$car'::uuid END
    FROM driver_document_types t
    WHERE t.active AND t.required
      AND NOT EXISTS (
          SELECT 1 FROM driver_documents d
          WHERE d.driver_id = '$id' AND d.type_code = t.code AND d.status = 'approved'
            AND (d.expires_on IS NULL OR d.expires_on > current_date)
            AND d.vehicle_id IS NOT DISTINCT FROM CASE WHEN t.scope = 'vehicle' THEN '$car'::uuid END)
    ON CONFLICT DO NOTHING;" > /dev/null
}

driver "$DRIVER_A" "$IDENTITY_A" A
driver "$DRIVER_B" "$IDENTITY_B" B

sql ride-rider-postgres "
  INSERT INTO riders (id, identity_id, display_name, status)
  VALUES ('$RIDER', '$IDENTITY_RIDER', 'E2E Rider', 'active')
  ON CONFLICT (id) DO UPDATE SET status = 'active', display_name = 'E2E Rider';" > /dev/null

# Erbil, where the older scripts ask for trips.
served="$(sql ride-location-postgres "
  SELECT count(*) FROM zones z JOIN cities c ON c.id = z.city_id
  WHERE z.active AND c.active
    AND ST_Covers(z.boundary::geometry, ST_SetSRID(ST_MakePoint(44.01, 36.19), 4326))
    AND ST_Covers(z.boundary::geometry, ST_SetSRID(ST_MakePoint(44.02, 36.20), 4326));")"

if [ "$served" = 0 ]; then
  owner_identity="$(python3 -c 'import uuid; print(uuid.uuid4())')"
  owner_staff="$(python3 -c 'import uuid; print(uuid.uuid4())')"
  trap 'sql ride-staff-postgres "delete from staff_members where id = '"'"'$owner_staff'"'"';" > /dev/null 2>&1 || true' EXIT
  sql ride-staff-postgres "
    INSERT INTO staff_members (id, identity_id, email, display_name, status, invited_at, activated_at)
    VALUES ('$owner_staff', '$owner_identity', 'e2e-fixtures-$owner_staff@ride.test', 'E2E Fixtures', 'active', now(), now());
    INSERT INTO staff_member_roles (staff_id, role_id) VALUES ('$owner_staff', '$OWNER_ROLE');" > /dev/null
  owner="$(go run scripts/tools/devtoken/main.go -sub "$owner_identity")"

  admin() { # <path> <json> -> the answer's body
    curl -sS -f -X POST "$BASE$1" -H "Authorization: Bearer $owner" -H 'Content-Type: application/json' -d "$2"
  }

  city="$(admin /v1/admin/cities '{"name":"E2E Erbil","timeZone":"Asia/Baghdad","center":{"latitude":36.19,"longitude":44.01}}' \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["city"]["id"])')"
  admin /v1/admin/zones "{\"cityId\":\"$city\",\"name\":\"E2E Erbil\",\"boundary\":[{\"latitude\":36.0,\"longitude\":43.8},{\"latitude\":36.0,\"longitude\":44.2},{\"latitude\":36.4,\"longitude\":44.2},{\"latitude\":36.4,\"longitude\":43.8}]}" > /dev/null
  echo "legacy-fixtures: Erbil was not served; made the city and zone \"E2E Erbil\""
fi
