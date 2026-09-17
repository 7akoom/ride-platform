#!/usr/bin/env bash
#
# Starts all 10 ride-platform backend services locally via `go run`, each
# loading its own services/<name>/.env. Logs go to logs/<service>.log;
# Ctrl+C stops every service cleanly.
#
# Assumes the infrastructure containers (Postgres, Valkey, NATS, OSRM)
# are already up — see infrastructure/compose (`docker compose up -d`).
#
# Usage:
#   ./scripts/run-all.sh              # start all 9 services
#   ./scripts/run-all.sh rider driver # start only the named services
#
# Stop: Ctrl+C in this terminal.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOG_DIR="$REPO_ROOT/logs"
mkdir -p "$LOG_DIR"

ALL_SERVICES=(
	identity-service
	rider-service
	driver-service
	location-service
	trip-service
	wallet-service
	pricing-service
	dispatch-service
	notification-service
	analytics
)

if [ "$#" -gt 0 ]; then
	SERVICES=()
	for name in "$@"; do
		SERVICES+=("${name%-service}-service")
	done
else
	SERVICES=("${ALL_SERVICES[@]}")
fi

# Union of every env var name any service's .env.example sets. Unset
# these in each service's subshell before sourcing its own .env, so a
# variable left exported in this terminal by an earlier manual
# `set -a && source .env` (e.g. testing another service by hand) can't
# leak in and silently override this service's own default — as
# happened when a leftover METRICS_ADDRESS from dispatch-service's
# port leaked into identity-service, which has no METRICS_ADDRESS line
# of its own to override it back.
ALL_ENV_KEYS="$(
	grep -hoE '^[A-Za-z_][A-Za-z0-9_]*=' "$REPO_ROOT"/services/*/.env.example 2>/dev/null |
		sed 's/=$//' |
		sort -u
)"

PIDS=()

cleanup() {
	echo ""
	echo "Stopping ${#PIDS[@]} service(s)..."
	for pid in "${PIDS[@]}"; do
		kill "$pid" 2>/dev/null || true
	done
	wait 2>/dev/null
	echo "All services stopped."
}
trap cleanup INT TERM

for svc in "${SERVICES[@]}"; do
	svc_dir="$REPO_ROOT/services/$svc"

	if [ ! -d "$svc_dir" ]; then
		echo "Skipping $svc: no such service directory" >&2
		continue
	fi

	if [ ! -f "$svc_dir/.env" ]; then
		echo "Skipping $svc: services/$svc/.env not found (copy .env.example first)" >&2
		continue
	fi

	echo "Starting $svc (log: logs/$svc.log)..."

	(
		cd "$svc_dir" || exit 1
		for key in $ALL_ENV_KEYS; do
			unset "$key"
		done
		set -a
		# shellcheck disable=SC1091
		source .env
		set +a
		exec go run "./cmd/$svc"
	) >"$LOG_DIR/$svc.log" 2>&1 &

	PIDS+=("$!")
	sleep 1
done

if [ "${#PIDS[@]}" -eq 0 ]; then
	echo "No services started." >&2
	exit 1
fi

echo ""
echo "${#PIDS[@]} service(s) running. Logs: $LOG_DIR/<service>.log"
echo "Press Ctrl+C to stop everything."
echo ""

wait
