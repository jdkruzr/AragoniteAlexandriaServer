#!/bin/sh
# Back up the library while it runs: a database dump, then a mirror of every
# stored object, into ALEXANDRIA_BACKUP_DIR (default ./backups). Copy that
# directory (and .env) off the host afterwards.
#
# Order matters: objects are immutable and only garbage-collected after seven
# days unreferenced, so every object the dump references is in the mirror.
set -eu
cd "$(dirname "$0")"
. ./common.sh
if [ "${ALEXANDRIA_BOOX_ENABLED:-false}" = true ]; then
    echo "Native BOOX requires coordinated backup/recovery; use backup-cold.sh and the storage recovery documentation." >&2
    exit 1
fi
dir=$(sed -n 's/^ALEXANDRIA_BACKUP_DIR=//p' .env 2>/dev/null)
dir=${dir:-./backups}
mkdir -p "$dir"
chmod 700 "$dir"
umask 077
compose exec -T postgres pg_dump -U alexandria -d alexandria -Fc > "$dir/alexandria.dump.partial"
mv "$dir/alexandria.dump.partial" "$dir/alexandria.dump"
compose --profile backup run --rm --no-deps -T backup-objects
echo "Backup written to $dir ($(date -u +%Y-%m-%dT%H:%MZ))."
