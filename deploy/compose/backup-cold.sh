#!/bin/sh
# Coordinated physical backup of this bind-mounted single-host installation.
# Requires root and enough space. Restore only with the same database versions.
set -eu
cd "$(dirname "$0")"
. ./common.sh
[ "$(id -u)" = 0 ] || { echo 'Run as root to preserve database ownership.' >&2; exit 1; }
: "${ALEXANDRIA_DATA_DIR:?Cold backup requires bind-mounted storage}"
: "${ALEXANDRIA_POSTGRES_DIR:?}"
: "${ALEXANDRIA_OBJECT_DIR:?}"
if [ "${ALEXANDRIA_BOOX_ENABLED:-false}" = true ]; then
    : "${ALEXANDRIA_COUCHBASE_DIR:?}"
    : "${ALEXANDRIA_BOOX_GATEWAY_CONFIG:?}"
fi
umask 077
stop_timeout=${ALEXANDRIA_BACKUP_STOP_TIMEOUT:-60}
case "$stop_timeout" in
    ''|*[!0-9]*|0) echo 'ALEXANDRIA_BACKUP_STOP_TIMEOUT must be positive seconds.' >&2; exit 1 ;;
esac
mkdir -p "$ALEXANDRIA_BACKUP_DIR"
stamp=$(date -u +%Y%m%dT%H%M%SZ)
partial=$ALEXANDRIA_BACKUP_DIR/cold-$stamp.partial
final=$ALEXANDRIA_BACKUP_DIR/cold-$stamp
mkdir "$partial"
# Keep the exact locally built application image; a mutable tag is not a backup.
app_container=$(compose ps -q alexandria)
[ -n "$app_container" ] || { echo "Application container missing; refusing incomplete backup." >&2; exit 1; }
app_image=$(docker inspect --format '{{.Image}}' "$app_container")
docker image save "$app_image" -o "$partial/application-image.tar"
printf '%s\n' "$app_image" > "$partial/application-image-id"
# Restart on all exits, including failed copies. No volume deletion.
trap 'compose up -d >/dev/null' EXIT
running_containers=$(compose ps --status running -q)
compose stop --timeout "$stop_timeout"
# Ensure no live process writes during the physical copy.
if [ -n "$(compose ps --status running -q)" ]; then
    echo 'Some containers are still running; refusing physical backup.' >&2; exit 1
fi
# Docker stop may succeed after SIGKILL. Preserve evidence and refuse to publish
# that as a clean cold package; the restart trap still restores service.
if [ -n "$running_containers" ]; then
    # Container IDs are Docker-generated whitespace-separated identifiers.
    docker inspect --format '{{.Name}} {{.State.ExitCode}} {{.State.OOMKilled}}' $running_containers > "$partial/stop-status.txt"
    if awk '$2 == 137 || $3 == "true" { bad=1 } END { exit !bad }' "$partial/stop-status.txt"; then
        echo 'Forced/OOM container stop; refusing cold backup. Inspect stop-status.txt and retry with a longer shutdown timeout.' >&2
        exit 1
    fi
fi
tar -C "$ALEXANDRIA_POSTGRES_DIR" -cpf "$partial/postgres.tar" .
tar -C "$ALEXANDRIA_OBJECT_DIR" -cpf "$partial/objects.tar" .
cp .env "$partial/deployment.env"
# The encryption key is contained in .env; retain the optional operator login too.
if [ -f private/owner-password ]; then cp private/owner-password "$partial/owner-password"; fi
printf '%s\n' 1 > "$partial/backup-format"
cp compose.yml data-dir.yml "$partial/"
if [ "${ALEXANDRIA_BOOX_ENABLED:-false}" = true ]; then
    tar -C "$ALEXANDRIA_COUCHBASE_DIR" -cpf "$partial/couchbase.tar" .
    cp "$ALEXANDRIA_BOOX_GATEWAY_CONFIG" "$partial/boox-gateway.json"
    cp boox.yml boox-data-dir.yml "$partial/"
fi
# Inspect container metadata: old one-shot containers can reference an image
# removed after a rebuild, which makes `compose images` fail despite valid data.
compose ps -a -q | xargs -r docker inspect --format '{{.Name}} {{.Config.Image}} {{.Image}}' > "$partial/images.txt"
(cd "$partial" && find . -maxdepth 1 -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS)
mv "$partial" "$final"
echo "Cold backup written to $final. Contains credentials; keep private."
