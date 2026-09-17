#!/usr/bin/env bash
#
# Builds a Docker image for every backend service using the single
# shared infrastructure/docker/Dockerfile, and prints a pass/fail
# summary at the end.
#
# Usage:
#   ./scripts/docker-build-all.sh                # build all 9
#   ./scripts/docker-build-all.sh rider driver    # build a subset
#
# Images are tagged ride-platform/<service>:dev.

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DOCKERFILE="$REPO_ROOT/infrastructure/docker/Dockerfile"

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

FAILED=()

for svc in "${SERVICES[@]}"; do
	echo ""
	echo "=== Building $svc ==="

	if docker build \
		--build-arg SERVICE_NAME="$svc" \
		-f "$DOCKERFILE" \
		-t "ride-platform/$svc:dev" \
		"$REPO_ROOT"; then
		echo "OK: $svc"
	else
		echo "FAILED: $svc"
		FAILED+=("$svc")
	fi
done

echo ""
echo "=================================="

if [ "${#FAILED[@]}" -eq 0 ]; then
	echo "All ${#SERVICES[@]} images built successfully."
	exit 0
fi

echo "${#FAILED[@]} of ${#SERVICES[@]} builds failed:"
for svc in "${FAILED[@]}"; do
	echo "  - $svc"
done
exit 1
