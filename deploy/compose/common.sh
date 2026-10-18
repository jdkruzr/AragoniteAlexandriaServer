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
# Resolve paths once; scripts and Compose consume the same exported value.
export ALEXANDRIA_BACKUP_DIR=${ALEXANDRIA_BACKUP_DIR:-./backups}

select_compose() {
    [ -z "${_alexandria_compose_command:-}" ] || return 0
    if [ -n "${COMPOSE:-}" ]; then
        _alexandria_compose_command=$COMPOSE
    elif docker compose version >/dev/null 2>&1; then
        _alexandria_compose_command='docker compose'
    elif podman compose version >/dev/null 2>&1; then
        _alexandria_compose_command='podman compose'
    else
        _alexandria_compose_command=docker-compose
    fi
    _alexandria_container_engine=${CONTAINER_ENGINE:-}
    if [ -z "$_alexandria_container_engine" ]; then
        # COMPOSE is a trusted, whitespace-separated command, as before.
        set -- $_alexandria_compose_command
        case "${1##*/}" in
            docker|docker-compose) _alexandria_container_engine=docker ;;
            podman|podman-compose) _alexandria_container_engine=podman ;;
        esac
    fi
}
engine() {
    select_compose
    case "$_alexandria_container_engine" in
        docker|podman) "$_alexandria_container_engine" "$@" ;;
        *) echo 'Set CONTAINER_ENGINE=docker or podman for a custom COMPOSE wrapper.' >&2; return 1 ;;
    esac
}

compose() {
    if [ "${ALEXANDRIA_OLLAMA_ENABLED:-false}" = true ]; then
        set -- --profile embeddings "$@"
    fi
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
    select_compose
    $_alexandria_compose_command "$@"
}
