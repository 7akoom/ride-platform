#!/usr/bin/env bash
#
# backup-databases.sh
#
# Dumps every Postgres database running in the Ride Platform's Docker Compose
# stack (identity, rider, driver, trip, pricing, wallet, notification,
# analytics — location & dispatch have no DB) to compressed .sql.gz files,
# with simple retention-based rotation.
#
# Auto-discovers containers by name pattern (ride-*-postgres), then reads
# POSTGRES_USER/POSTGRES_DB directly from each container's own environment —
# no need to parse infrastructure/compose/.env by hand, and it stays correct
# even if a service's real creds differ from its .env.example.
#
# Usage:
#   ./backup-databases.sh
#
# Cron (daily at 3am, logs to logs/backup.log):
#   0 3 * * * /full/path/to/scripts/backup-databases.sh >> /full/path/to/logs/backup.log 2>&1

set -uo pipefail

# ---- Config -----------------------------------------------------------

# Where backups are stored. Change to an external/mounted path if you have one.
BACKUP_DIR="${BACKUP_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/backups}"

# How many days of backups to keep locally.
RETENTION_DAYS="${RETENTION_DAYS:-14}"

# Container name pattern to discover. Matches ride-<service>-postgres.
CONTAINER_NAME_FILTER="${CONTAINER_NAME_FILTER:-ride-.*-postgres}"

# Optional: set REMOTE_DEST to something rsync understands (e.g.
# user@host:/path/to/backups or an rclone remote via a wrapper) to also copy
# backups off this machine. Leave empty to skip off-host copying.
REMOTE_DEST="${REMOTE_DEST:-}"

# ---- Setup --------------------------------------------------------------

TIMESTAMP="$(date +%Y%m%d_%H%M%S)"
RUN_DIR="${BACKUP_DIR}/${TIMESTAMP}"
mkdir -p "${RUN_DIR}"

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"
}

FAILURES=0

# ---- Discover containers -------------------------------------------------

mapfile -t CONTAINERS < <(docker ps --format '{{.Names}}' | grep -E "${CONTAINER_NAME_FILTER}" || true)

if [ "${#CONTAINERS[@]}" -eq 0 ]; then
    log "ERROR: no running containers matched pattern '${CONTAINER_NAME_FILTER}'. Is the stack up (docker compose up -d)?"
    exit 1
fi

log "Found ${#CONTAINERS[@]} Postgres container(s): ${CONTAINERS[*]}"

# ---- Dump each database --------------------------------------------------

for CONTAINER in "${CONTAINERS[@]}"; do
    PG_USER="$(docker exec "${CONTAINER}" printenv POSTGRES_USER 2>/dev/null || true)"
    PG_DB="$(docker exec "${CONTAINER}" printenv POSTGRES_DB 2>/dev/null || true)"

    if [ -z "${PG_USER}" ] || [ -z "${PG_DB}" ]; then
        log "ERROR: could not read POSTGRES_USER/POSTGRES_DB from ${CONTAINER}, skipping."
        FAILURES=$((FAILURES + 1))
        continue
    fi

    OUT_FILE="${RUN_DIR}/${PG_DB}.sql.gz"
    log "Dumping ${PG_DB} (container: ${CONTAINER}, user: ${PG_USER})..."

    if docker exec -t "${CONTAINER}" pg_dump -U "${PG_USER}" -d "${PG_DB}" 2>"${RUN_DIR}/${PG_DB}.err.log" \
        | gzip > "${OUT_FILE}"; then

        SIZE=$(stat -c%s "${OUT_FILE}" 2>/dev/null || stat -f%z "${OUT_FILE}" 2>/dev/null || echo 0)
        if [ "${SIZE}" -gt 0 ]; then
            log "OK: ${OUT_FILE} (${SIZE} bytes)"
            rm -f "${RUN_DIR}/${PG_DB}.err.log"
        else
            log "ERROR: ${PG_DB} dump is empty — check ${RUN_DIR}/${PG_DB}.err.log"
            FAILURES=$((FAILURES + 1))
        fi
    else
        log "ERROR: pg_dump failed for ${PG_DB} — see ${RUN_DIR}/${PG_DB}.err.log"
        FAILURES=$((FAILURES + 1))
    fi
done

# ---- Optional off-host copy ----------------------------------------------

if [ -n "${REMOTE_DEST}" ]; then
    log "Copying ${RUN_DIR} to ${REMOTE_DEST}..."
    if ! rsync -az "${RUN_DIR}" "${REMOTE_DEST}"; then
        log "ERROR: rsync to ${REMOTE_DEST} failed (backups still kept locally in ${RUN_DIR})"
        FAILURES=$((FAILURES + 1))
    fi
fi

# ---- Rotation -------------------------------------------------------------

log "Removing local backup runs older than ${RETENTION_DAYS} days..."
find "${BACKUP_DIR}" -maxdepth 1 -mindepth 1 -type d -mtime "+${RETENTION_DAYS}" -exec rm -rf {} \;

# ---- Summary ---------------------------------------------------------------

if [ "${FAILURES}" -eq 0 ]; then
    log "Backup run complete: all ${#CONTAINERS[@]} database(s) OK."
    exit 0
else
    log "Backup run finished with ${FAILURES} failure(s). Check the log above."
    exit 1
fi
