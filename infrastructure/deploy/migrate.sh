#!/bin/sh
# goose up on every service database (RIDE_DATABASES: IDENTITY, RIDER...).
# The migrations come from /services/<service>/migrations (mounted from the
# checkout). Stops at the first failure.
set -eu

for prefix in $RIDE_DATABASES; do
  lower="$(echo "$prefix" | tr 'A-Z' 'a-z')"
  dir="/services/${lower}-service/migrations"
  [ -d "$dir" ] || dir="/services/${lower}/migrations"
  [ -d "$dir" ] || { echo "migrate: no migrations for $prefix" >&2; exit 1; }

  name="$(printenv "${prefix}_DB_NAME")"
  user="$(printenv "${prefix}_DB_USER")"
  password="$(printenv "${prefix}_DB_PASSWORD")"

  echo "== $lower"
  goose -dir "$dir" postgres "postgres://${user}:${password}@${DB_HOST}:5432/${name}?sslmode=disable" up
done
