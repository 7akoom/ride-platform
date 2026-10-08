#!/usr/bin/env bash
# Turns on monitoring: this instance's traces, metrics and service logs go to
# Grafana Cloud through the Alloy agent. Run on the server, from the repo root
# (no sudo), with the three values of the Grafana Cloud "OpenTelemetry (OTLP)"
# connection at hand:
#   bash scripts/deploy/enable-monitoring.sh
#   bash scripts/deploy/deploy.sh
# It asks for the endpoint, the instance ID and the token (typed, not shown),
# checks them against Grafana Cloud, and writes instance/monitoring.env (only
# the agent reads it). Run it again to change them.
set -Eeuo pipefail

die() { echo "FAIL: $*" >&2; exit 1; }
[ -f instance/instance.env ] || die "run this from the repo root of a deployed instance"

read -r -p "OTLP endpoint (https://otlp-gateway-....grafana.net/otlp): " endpoint
endpoint="${endpoint%/}"
[[ "$endpoint" =~ ^https://[a-z0-9.-]+\.grafana\.net/otlp$ ]] || die "that does not look like a Grafana Cloud OTLP endpoint (https://otlp-gateway-<region>.grafana.net/otlp)"
read -r -p "Instance ID (a number): " instance_id
[[ "$instance_id" =~ ^[0-9]+$ ]] || die "the instance ID is a number"
read -r -s -p "Token (not shown): " token
echo
[ "${#token}" -ge 20 ] || die "that token is too short"
case "$token" in *[[:space:]]*|*\"*|*\'*|*\$*) die "the token has a character it should not have; copy it again" ;; esac

echo "==> checking them with Grafana Cloud"
code="$(curl -s -o /dev/null -w '%{http_code}' -u "$instance_id:$token" -H 'Content-Type: application/json' \
  -d '{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"enable-monitoring"}}]},"scopeLogs":[{"logRecords":[{"body":{"stringValue":"monitoring enabled"}}]}]}]}' \
  "$endpoint/v1/logs" || true)"
case "$code" in
  200|204) echo "  ok    accepted" ;;
  401|403) die "Grafana Cloud refused them ($code): check the instance ID and that the token may write logs, metrics and traces" ;;
  *) die "Grafana Cloud answered $code (expected 200)" ;;
esac

umask 077
{
  echo "# Grafana Cloud, read only by the Alloy agent (scripts/deploy/enable-monitoring.sh)."
  echo "GRAFANA_CLOUD_OTLP_ENDPOINT=$endpoint"
  echo "GRAFANA_CLOUD_INSTANCE_ID=$instance_id"
  echo "GRAFANA_CLOUD_API_TOKEN=$token"
} > instance/monitoring.env
chmod 600 instance/monitoring.env

if grep -q '^OTEL_EXPORTER_OTLP_ENDPOINT=' instance/instance.env; then
  sed -i 's#^OTEL_EXPORTER_OTLP_ENDPOINT=.*#OTEL_EXPORTER_OTLP_ENDPOINT=http://alloy:4317#' instance/instance.env
else
  printf '\n# Traces go to the Alloy agent (monitoring).\nOTEL_EXPORTER_OTLP_ENDPOINT=http://alloy:4317\n' >> instance/instance.env
fi

echo "wrote instance/monitoring.env; the services will send traces to the agent."
echo "next: bash scripts/deploy/deploy.sh"
