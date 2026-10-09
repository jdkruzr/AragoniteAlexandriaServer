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
mkdir -p "$ALEXANDRIA_BACKUP_DIR"
stamp=$(date -u +%Y%m%dT%H%M%SZ)
partial=$ALEXANDRIA_BACKUP_DIR/cold-$stamp.partial
final=$ALEXANDRIA_BACKUP_DIR/cold-$stamp
mkdir "$partial"
# Restart on all exits, including failed copies. No volume deletion.
trap 'compose up -d >/dev/null' EXIT
compose stop
# Ensure no live process writes during the physical copy.
if [ -n "$(compose ps --status running -q)" ]; then
    echo 'Some containers are still running; refusing physical backup.' >&2; exit 1
fi
tar -C "$ALEXANDRIA_POSTGRES_DIR" -cpf "$partial/postgres.tar" .
tar -C "$ALEXANDRIA_OBJECT_DIR" -cpf "$partial/objects.tar" .
cp .env "$partial/deployment.env"
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
