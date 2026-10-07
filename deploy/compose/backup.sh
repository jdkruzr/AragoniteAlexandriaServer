#!/bin/sh
# Back up the library while it runs: a database dump, then a mirror of every
# stored object, into ALEXANDRIA_BACKUP_DIR (default ./backups). Copy that
# directory (and .env) off the host afterwards.
#
# Order matters: objects are immutable and only garbage-collected after seven
# days unreferenced, so every object the dump references is in the mirror.
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
mkdir -p "$dir"
chmod 700 "$dir"
umask 077
compose exec -T postgres pg_dump -U alexandria -d alexandria -Fc > "$dir/alexandria.dump.partial"
mv "$dir/alexandria.dump.partial" "$dir/alexandria.dump"
compose --profile backup run --rm --no-deps -T backup-objects
echo "Backup written to $dir ($(date -u +%Y-%m-%dT%H:%MZ))."
