#!/bin/sh
# Back up the library: a database dump, then a mirror of every stored object,
# into ALEXANDRIA_BACKUP_DIR (default ./backups). Copy that directory off the
# host afterwards. The server keeps running.
set -eu
cd "$(dirname "$0")"
compose() {
	if [ -n "${COMPOSE:-}" ]; then $COMPOSE "$@"
	elif docker compose version >/dev/null 2>&1; then docker compose "$@"
	elif podman compose version >/dev/null 2>&1; then podman compose "$@"
	else docker-compose "$@"
	fi
}
dir=$(sed -n 's/^ALEXANDRIA_BACKUP_DIR=//p' .env 2>/dev/null)
mkdir -p "${dir:-./backups}"
chmod 700 "${dir:-./backups}"
# Order matters: the dump first, so every object it references is mirrored.
compose --profile backup run --rm backup-database
compose --profile backup run --rm backup-objects
echo "Backup written to ${dir:-./backups} ($(date -u +%Y-%m-%dT%H:%MZ))."
