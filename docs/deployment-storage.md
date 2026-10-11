# Host storage and upstream binding

Set `ALEXANDRIA_MAIN_HOST` and `ALEXANDRIA_MAIN_PORT` in `deploy/compose/.env` to select the HTTP upstream address. Defaults remain `127.0.0.1:18443`; an external TLS proxy needs a reachable LAN address. SPC has independent `ALEXANDRIA_SPC_HOST` / `ALEXANDRIA_SPC_PORT` settings and stays private by default.

Set `ALEXANDRIA_DATA_DIR` to opt into host bind mounts. PostgreSQL defaults to `$ALEXANDRIA_DATA_DIR/postgres`, objects to `$ALEXANDRIA_DATA_DIR/objects`, backups to `$ALEXANDRIA_DATA_DIR/backups`. Override each with `ALEXANDRIA_POSTGRES_DIR`, `ALEXANDRIA_OBJECT_DIR`, `ALEXANDRIA_BACKUP_DIR`. Create directories explicitly before starting; Compose refuses missing host paths. Native BOOX additionally requires `ALEXANDRIA_BOOX_ENABLED=true`, its private Gateway configuration/credentials, and `ALEXANDRIA_COUCHBASE_DIR`.

`install.sh`, `backup.sh`, and `restore.sh` share override selection in `common.sh`. Direct Docker Compose invocations must select the same override files. Existing installations without a data root retain their named volumes. Changing from volumes to paths is a deliberate migration: stop writers, back up, copy and verify existing stores before changing configuration. Merely changing the setting does not migrate data.

For a network mount, set `ALEXANDRIA_REQUIRED_MOUNT` to its mountpoint. Scripts refuse to proceed without that mount. Also order Docker startup after the mount using a systemd Docker service drop-in with `RequiresMountsFor=` and `ExecStartPre=/usr/bin/mountpoint -q ...`; container restart policies otherwise bypass shell checks at boot.

Deployment chosen 2026-10-09 for `alexandria-host`:
- HTTPS origin: `https://alexandria.example.com`.
- HTTP upstream: `192.0.2.10:8080`.
- PostgreSQL: `/var/lib/alexandria/postgres` (ext4).
- Couchbase: `/var/lib/alexandria/couchbase` (ext4).
- Object storage: `/mnt/alexandria/objects` (CephFS).
- Backups: `/mnt/alexandria/backups` (CephFS).

Nginx must forward WebSocket Upgrade/Connection headers, allow at least 64 MiB request bodies, and use long read timeouts for native BLIP sessions. TLS terminates at the existing proxy. Couchbase administration and Sync Gateway must not be published through it.

The existing `backup.sh` saves PostgreSQL and objects only. It is **not a complete native BOOX backup**: Couchbase and private Gateway/configuration secrets must also be captured consistently. Do not use the generic restore script as native disaster recovery. Backups on the same CephFS as assets also need a separate off-host copy.

`sudo ./backup-cold.sh` provides a coordinated full physical snapshot for bind-mounted installations: it stops all writers, captures PostgreSQL, Couchbase, objects, deployment credentials and Gateway configuration, checksums the package, then restarts the stack even if copying fails. It causes downtime. Keep it private and copy it off-host. Physical restoration requires matching database image versions, stopped containers, empty destination directories, original ownership and the saved configuration; it is distinct from `restore.sh`. A 2026-10-10 isolated physical restore boots PostgreSQL, objects, Couchbase and Gateway; all 38,969 retained BOOX objects pass hash/length readback. Device cutover remains a separate gate.

Exact settings and observed checks for the external proxy are recorded in [Nginx Proxy Manager deployment](deployment-nginx-proxy-manager.md).


## Cold backup verification and offline staging

The cold package now includes the exact application image archive and its original image identity, database image metadata, and optional operator-password file. Treat all files as credentials. A retained operator-password file may become stale after a password change through Settings; the restored database hash is authoritative. Never infer that an old backup has the current login.

Run `python3 deploy/compose/restore-cold.py /path/to/cold-package` to verify all checksums and reject incomplete packages or unsafe archive members. Add `--extract /new/empty-staging-path` as root (or under `podman unshare` for rootless containers) to preserve database ownership. The destination must not already exist. This only extracts: it does not start services or overwrite a deployment.

Before isolated startup, remap every host bind path, container/project/network name and published port. Avoid the original explicitly named volumes. Use the saved application image and pinned matching database versions; disable egress and start with application role `gateway` so cloned jobs do not contact providers. Restore the original Couchbase node hostname as a network alias (`cb.local` in this deployment). Staged secrets start mode 0600: deliberately grant the Gateway container user access to its configuration alone (`root:1000`, 0640 for the tested image). Keep all other credentials private. Native BOOX requires an HTTPS public origin even for an isolated rehearsal; a reserved `.invalid` hostname is suitable for backend checks over loopback, but device testing needs actual reachable TLS.

The qualification copy survived service restart with identical digests for BOOX identity/devices/grants, blobs, revision history, cursors, projections, operation receipts, provider settings and owner credentials. The Gateway document index is checked independently of PostgreSQL. See [campaign evidence](../../AragonitePowerSync/docs/qualification-campaign-20261010.md) for open gates and exact counts. Do not promote an isolated clone to production or connect devices merely because its health check passes.

2026-10-11 qualification update: the current VM application includes explicit BOOX reader refresh, corrected highlight/bookmark status filters and a bounded owner audio-repair-preview API. No storage layout or schema migration changed. Audio repair retains original and derived immutable versions with provenance; it is explicit, and changing the server object does not replace existing native MP3 caches. Full required PG/S3 BOOX race tests and native synthetic MP3 redownload pass. See PowerSync `docs/qualification-campaign-20261010.md` for exact scope and evidence.


## Restore cutover and readiness qualification (2026-10-11)

A fresh complete package at `/mnt/alexandria/backups/cold-20261011T100650Z` was checksum-verified and extracted into a separate ext4 directory. All four database/Gateway/object-service image identities match production; all 38,999 retained body objects (1,211,973,010 bytes) read back exactly. An operator-assisted Palma endpoint rehearsal preserves account/cluster identity and catalogs, downloads the exact synthetic EPUB and repaired MP3 from the restored service, and returns the device to its exact original production endpoint/marker and sync flags. This qualifies native restore delivery, not a productized one-click failover or cloned recognition-worker execution. Empty client/WebDAV domains do not establish nonempty restoration coverage.

The clone also exposed a Couchbase view-feed startup failure (`DCP_STREAM_REQ` rejected on a non-DCP connection). A full clone Couchbase restart did not clear it; with Gateway stopped, restarting only the clone CouchDB child yielded successful DCP_OPEN handshakes, working views and Gateway startup. No index deletion or record rewrite was needed. This is diagnostic evidence, not a proven general repair algorithm. Never apply that workaround automatically to production.

Standalone `/readyz` now requires migrated PostgreSQL, a reachable S3 bucket, and—when configured—the named native Gateway database Online plus a successful bounded document query. Checks share a two-second deadline and expose only generic readiness status. `/livez` and `/health` remain process probes. A healthy probe is still not proof of tablet receipt, completed local resource download or rendered ink. Preserve rollback controls and check those independently during device cutover.


Cold backup shutdown now allows `ALEXANDRIA_BACKUP_STOP_TIMEOUT` seconds, default 60. SeaweedFS's normal stop exceeded Docker's ten-second default in the qualification deployment; its Compose grace period is now 60 seconds. Each full package records stopped-container exit/OOM states in `stop-status.txt`. A forced kill (137) or OOM prevents package publication, leaves private `.partial` evidence and still attempts to restart services. Inspect the cause and retry with a suitable grace period; do not rename a partial package into a completed backup. Increasing the timeout does not repair an already unhealthy process.


The subsequent clean-stop package `cold-20261011T104536Z` includes the deployed readiness image. A separate fresh extraction passes 51,982 object hashes (38,999 source/resource bodies plus 12,983 revision bodies) and retained-owner/anonymous authentication checks. Gateway starts without the earlier child-process workaround in this run. Start the application after database and S3 health; production Compose declares that ordering, whereas a minimal rehearsal must supply it explicitly. The copies use gateway-only role and no device endpoint; running restored OCR work or other nonempty source domains is not covered.
