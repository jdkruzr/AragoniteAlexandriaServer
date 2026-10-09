# Host storage and upstream binding

Set `ALEXANDRIA_MAIN_HOST` and `ALEXANDRIA_MAIN_PORT` in `deploy/compose/.env` to select the HTTP upstream address. Defaults remain `127.0.0.1:18443`; an external TLS proxy needs a reachable LAN address. SPC has independent `ALEXANDRIA_SPC_HOST` / `ALEXANDRIA_SPC_PORT` settings and stays private by default.

Set `ALEXANDRIA_DATA_DIR` to opt into host bind mounts. PostgreSQL defaults to `$ALEXANDRIA_DATA_DIR/postgres`, objects to `$ALEXANDRIA_DATA_DIR/objects`, backups to `$ALEXANDRIA_DATA_DIR/backups`. Override each with `ALEXANDRIA_POSTGRES_DIR`, `ALEXANDRIA_OBJECT_DIR`, `ALEXANDRIA_BACKUP_DIR`. Create directories explicitly before starting; Compose refuses missing host paths. Native BOOX additionally requires `ALEXANDRIA_BOOX_ENABLED=true`, its private Gateway configuration/credentials, and `ALEXANDRIA_COUCHBASE_DIR`.

`install.sh`, `backup.sh`, and `restore.sh` share override selection in `common.sh`. Direct Docker Compose invocations must select the same override files. Existing installations without a data root retain their named volumes. Changing from volumes to paths is a deliberate migration: stop writers, back up, copy and verify existing stores before changing configuration. Merely changing the setting does not migrate data.

For a network mount, set `ALEXANDRIA_REQUIRED_MOUNT` to its mountpoint. Scripts refuse to proceed without that mount. Also order Docker startup after the mount using a systemd Docker service drop-in with `RequiresMountsFor=` and `ExecStartPre=/usr/bin/mountpoint -q ...`; container restart policies otherwise bypass shell checks at boot.

Deployment chosen 2026-10-09 for `alexandria-srv`:
- HTTPS origin: `https://alexandria.federation.engineering`.
- HTTP upstream: `192.168.9.90:8080`.
- PostgreSQL: `/var/lib/alexandria/postgres` (ext4).
- Couchbase: `/var/lib/alexandria/couchbase` (ext4).
- Object storage: `/mnt/alexandria/objects` (CephFS).
- Backups: `/mnt/alexandria/backups` (CephFS).

Nginx must forward WebSocket Upgrade/Connection headers, allow at least 64 MiB request bodies, and use long read timeouts for native BLIP sessions. TLS terminates at the existing proxy. Couchbase administration and Sync Gateway must not be published through it.

The existing `backup.sh` saves PostgreSQL and objects only. It is **not a complete native BOOX backup**: Couchbase and private Gateway/configuration secrets must also be captured consistently. Do not use the generic restore script as native disaster recovery. Backups on the same CephFS as assets also need a separate off-host copy.

`sudo ./backup-cold.sh` provides a coordinated full physical snapshot for bind-mounted installations: it stops all writers, captures PostgreSQL, Couchbase, objects, deployment credentials and Gateway configuration, checksums the package, then restarts the stack even if copying fails. It causes downtime. Keep it private and copy it off-host. Physical restoration requires matching database image versions, stopped containers, empty destination directories, original ownership and the saved configuration; it is distinct from `restore.sh`. Automated native restore qualification remains pending.

Exact settings and observed checks for the external proxy are recorded in [Nginx Proxy Manager deployment](deployment-nginx-proxy-manager.md).
