#!/bin/sh
# Restore the library from ALEXANDRIA_BACKUP_DIR (default ./backups), made by
# backup.sh. This REPLACES the current library's database. Devices keep their
# own copies and re-sync afterwards.
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
dir=${dir:-./backups}
if [ ! -f "$dir/alexandria.dump" ]; then
	echo "No backup found at $dir/alexandria.dump." >&2
	exit 1
fi
printf 'This replaces the current library with the backup in %s. Type "restore" to continue: ' "$dir" >&2
read -r answer
[ "$answer" = "restore" ] || { echo "Cancelled." >&2; exit 1; }
compose stop alexandria
compose --profile restore run --rm restore-objects
compose --profile restore run --rm restore-database
# Starting again re-applies migrations and the runtime role's grants.
compose up -d
echo "Restored. Devices sync again on their own."
