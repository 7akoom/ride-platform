#!/bin/sh
# Runs once, when the Postgres volume is created: one role and one database
# per service (RIDE_DATABASES lists their prefixes: IDENTITY, RIDER...), each
# owned by its own role, so a service only reaches its own data. PostGIS goes
# into the location database (an extension only a superuser may create).
set -eu

for prefix in $RIDE_DATABASES; do
  name="$(printenv "${prefix}_DB_NAME")"
  user="$(printenv "${prefix}_DB_USER")"
  password="$(printenv "${prefix}_DB_PASSWORD")"

  psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
    -v name="$name" -v user="$user" -v password="$password" <<'SQL'
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'user', :'password') \gexec
SELECT format('CREATE DATABASE %I OWNER %I', :'name', :'user') \gexec
SELECT format('REVOKE ALL ON DATABASE %I FROM PUBLIC', :'name') \gexec
SQL

  psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$name" \
    -v user="$user" <<'SQL'
SELECT format('ALTER SCHEMA public OWNER TO %I', :'user') \gexec
SQL

  if [ "$prefix" = LOCATION ]; then
    psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$name" -c 'CREATE EXTENSION IF NOT EXISTS postgis;'
  fi

  echo "ride: database $name for $user"
done
