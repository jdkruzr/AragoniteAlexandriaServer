# Plan: Back Up BOOX Installs on Named Volumes (2026-10-18)

Status: proposed, waiting for the owner's go-ahead.

## The Gap

A BOOX install on Docker's default named volumes has no supported backup:

- `backup.sh` refuses when `ALEXANDRIA_BOOX_ENABLED=true`. A live
  `pg_dump` plus an object mirror can't capture Couchbase at the same moment.
- `backup-cold.sh` requires `ALEXANDRIA_DATA_DIR` (host directories). It tars
  `ALEXANDRIA_POSTGRES_DIR`, `ALEXANDRIA_OBJECT_DIR` and
  `ALEXANDRIA_COUCHBASE_DIR` directly, calls `docker` directly, and requires
  root.

The owner's own server uses host directories, so it isn't affected. A new
install that follows [the BOOX guide](guide/boox.md) with the defaults is.

## Approach

Extend the cold backup rather than add a new script. Its coordination is
already right: stop everything, check nothing was force-killed, copy, restart
on every exit. Only where it reads from is hard-wired.

1. **Copy named volumes through a helper container.** When
   `ALEXANDRIA_DATA_DIR` is unset, archive each volume with a short-lived
   container (`run --rm -v <volume>:/src:ro -v <partial>:/out <image> tar -C
   /src -cpf /out/<name>.tar .`).
   - Use an image already on the host (the pinned postgres image has `tar`),
     so the backup never pulls anything.
   - Volume names: `ALEXANDRIA_POSTGRES_VOLUME`, `ALEXANDRIA_OBJECT_VOLUME`
     (defaults in `compose.yml`), and the Couchbase volume from `boox.yml`.
     Its name is project-prefixed, so resolve the real name from
     `compose config` or the container's mounts, not by guessing.
   - Keep the archive names (`postgres.tar`, `objects.tar`, `couchbase.tar`)
     and `SHA256SUMS` exactly as today. Record the source in a new
     `storage.txt` (`volumes` or `directories`), and bump `backup-format` to
     `2` only if the restore side needs to tell them apart.
2. **Use Compose's engine, not `docker`.** Factor the engine choice out of
   `compose()` in `common.sh` into an `engine()` helper (docker or podman,
   honoring `COMPOSE`). Use it for `inspect`, `image save` and the helper
   containers.
   - Rootless Podman then works with volumes.
   - Root stays required for the host-directory path only, where file
     ownership must be preserved. Document that split.
3. **Restore into volumes.** `restore-cold.py` today only verifies and
   extracts. Add `--into-volumes`:
   - Verify `SHA256SUMS` first, as it does now.
   - Refuse unless the stack is stopped and each target volume is empty or
     absent (never overwrite a live library).
   - Create the volumes, and untar each archive into its volume through a
     helper container.
   - Load `application-image.tar`, and print the remaining steps: put
     `deployment.env` in place as `.env` if this is a new host, restore
     `boox-gateway.json` to `ALEXANDRIA_BOOX_GATEWAY_CONFIG`, then
     `compose up -d`.
   - The host-directory path keeps its current manual procedure, now
     documented in the install guide.
4. **Round-trip test with Podman and synthetic data.**
   - Bring up a throwaway BOOX stack on named volumes under a unique project
     and volume names. Initialize Couchbase as the guide does, and run
     `boox-configure`.
   - Seed synthetic content: a user, a task, an uploaded object, and a
     Couchbase document via the Sync Gateway admin API.
   - Run the backup, then destroy the volumes, then restore with
     `--into-volumes` and start.
   - Check row counts and checksums in PostgreSQL, the object listing,
     Couchbase document count and content, `/readyz` 200, and that the
     synthetic task is served.
   - Negative checks: restoring into a non-empty volume refuses; a corrupted
     archive fails verification.
   - Re-run the existing host-directory path in the same harness so it doesn't
     regress.
   - Tear everything down. Never touch the real database or volumes, per
     AGENTS.md.
5. **Docs.**
   - Update `docs/guide/install.md` (the backup table and the "BOOX needs
     host directories" warnings) and `docs/guide/boox.md` (the Backups note).
   - Change `backup.sh`'s refusal message to point at `backup-cold.sh`.
   - Add a dated entry to `docs/current-state.md`.

## Not in Scope

- Hot (no-downtime) BOOX backups. Couchbase's own backup tool and a
  consistent pairing with PostgreSQL would be a separate design.
- Rotation or retention of cold backups, which stays the operator's job (as
  the guide says).

## Effort

Steps 1 and 2 are small. Step 3 (the restore option) and step 4 (the
round-trip harness) are most of the work. No server code changes, no
migration and no deploy to .90 are needed. This is scripts and docs only.
