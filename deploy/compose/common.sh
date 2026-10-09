#!/bin/sh
# Shared Compose selection. .env is trusted deployment configuration.
if [ -f .env ]; then
    set -a
    . ./.env
    set +a
fi
if [ -n "${ALEXANDRIA_DATA_DIR:-}" ]; then
    case "$ALEXANDRIA_DATA_DIR" in /*) ;; *) echo "ALEXANDRIA_DATA_DIR must be absolute" >&2; exit 1;; esac
    export ALEXANDRIA_POSTGRES_DIR=${ALEXANDRIA_POSTGRES_DIR:-$ALEXANDRIA_DATA_DIR/postgres}
    export ALEXANDRIA_OBJECT_DIR=${ALEXANDRIA_OBJECT_DIR:-$ALEXANDRIA_DATA_DIR/objects}
    export ALEXANDRIA_BACKUP_DIR=${ALEXANDRIA_BACKUP_DIR:-$ALEXANDRIA_DATA_DIR/backups}
    if [ -n "${ALEXANDRIA_REQUIRED_MOUNT:-}" ]; then
        mountpoint -q "$ALEXANDRIA_REQUIRED_MOUNT" || { echo "Required data mount is missing: $ALEXANDRIA_REQUIRED_MOUNT" >&2; exit 1; }
    fi
fi
compose() {
    if [ "${ALEXANDRIA_BOOX_ENABLED:-false}" = true ]; then
        if [ -n "${ALEXANDRIA_DATA_DIR:-}" ]; then
            : "${ALEXANDRIA_COUCHBASE_DIR:?Set ALEXANDRIA_COUCHBASE_DIR for native BOOX}"
            set -- --profile boox -f compose.yml -f boox.yml -f data-dir.yml -f boox-data-dir.yml "$@"
        else
            set -- --profile boox -f compose.yml -f boox.yml "$@"
        fi
    elif [ -n "${ALEXANDRIA_DATA_DIR:-}" ]; then
        set -- -f compose.yml -f data-dir.yml "$@"
    else
        set -- -f compose.yml "$@"
    fi
    if [ -n "${COMPOSE:-}" ]; then $COMPOSE "$@"
    elif docker compose version >/dev/null 2>&1; then docker compose "$@"
    elif podman compose version >/dev/null 2>&1; then podman compose "$@"
    else docker-compose "$@"
    fi
}
