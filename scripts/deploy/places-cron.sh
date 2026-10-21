#!/usr/bin/env bash
# Runs the place import (scripts/deploy/import-places.sh) by itself every month,
# so place search keeps Overture's new and closed places. Run on the server, from
# the repo root, as the user that runs the instance (it needs docker):
#   bash scripts/deploy/places-cron.sh            # install, or show it is installed
#   bash scripts/deploy/places-cron.sh --remove   # stop the monthly import
#   bash scripts/deploy/places-cron.sh --status   # when it last ran, and how
#
# It runs at 03:17 on the 2nd of every month (the server's time), when trips are
# few; Overture publishes a release around the middle of each month, so the 2nd
# always finds the previous month's. One run at a time (flock). The last run's
# output is kept in instance/logs/places-import.log.
set -Euo pipefail

die() { echo "FAIL: $*" >&2; exit 1; }

[ -f instance/instance.env ] && [ -f scripts/deploy/import-places.sh ] ||
  die "run this from the repo root of a deployed instance"
command -v crontab > /dev/null || die "crontab is not installed (apt install cron)"

REPO="$(pwd)"
MARK="# ride-platform monthly place import ($REPO)"
LOG="$REPO/instance/logs/places-import.log"
LINE="17 3 2 * * cd '$REPO' && mkdir -p instance/logs && flock -n /tmp/ride-places-import.lock bash scripts/deploy/import-places.sh > '$LOG' 2>&1"

current() { crontab -l 2> /dev/null || true; }

EXISTING="$(current)"

case "${1:-}" in
  --remove)
    # The mark and the line after it.
    printf '%s\n' "$EXISTING" | awk -v mark="$MARK" '$0 == mark { skip = 1; next } skip { skip = 0; next } { print }' | crontab -
    echo "==> the monthly place import is off"
    ;;
  --status)
    if current | grep -qF "$MARK"; then
      echo "==> installed: $(current | grep -A1 -F "$MARK" | tail -1 | cut -d' ' -f1-5) (minute hour day month weekday)"
    else
      echo "==> not installed"
    fi
    if [ -f "$LOG" ]; then
      echo "==> the last run ($(date -r "$LOG" '+%F %T')):"
      tail -5 "$LOG"
    else
      echo "==> it has not run yet"
    fi
    ;;
  "")
    if current | grep -qF "$MARK"; then
      echo "==> already installed; nothing changed"
    else
      { [ -n "$EXISTING" ] && printf '%s\n' "$EXISTING"; echo "$MARK"; echo "$LINE"; } | crontab -
      echo "==> installed: the place import runs at 03:17 on the 2nd of every month"
    fi
    echo "    status: bash scripts/deploy/places-cron.sh --status"
    ;;
  *) die "unknown option $1 (use --remove or --status)" ;;
esac
