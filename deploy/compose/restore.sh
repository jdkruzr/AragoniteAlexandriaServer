#!/bin/sh
# Restore the library from ALEXANDRIA_BACKUP_DIR (default ./backups), made by
# backup.sh. This REPLACES the current library. Devices keep their own copies
# and re-sync afterwards.
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
# Objects first: the restored database must never reference a missing one.
compose --profile restore run --rm --no-deps -T restore-objects
compose exec -T postgres pg_restore -U alexandria -d alexandria --clean --if-exists --no-owner --exit-on-error < "$dir/alexandria.dump"
# An older backup may predate the current schema.
compose run --rm --no-deps -T migrate
compose run --rm --no-deps -T bootstrap-runtime
# The restored history is older than what devices have seen. A new sync epoch
# makes each device re-send what it wrote after the backup, then re-download.
compose exec -T postgres psql -U alexandria -d alexandria -qAt -c 'SELECT alexandria_rewind_sync_epoch()' >/dev/null
compose start alexandria
echo "Restored. Devices sync again on their own and re-send changes made after the backup."
