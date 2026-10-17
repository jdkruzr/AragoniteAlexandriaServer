# Joining a Third BOOX Device (Planned 2026-10-18)

The owner plans to join another BOOX tablet to Alexandria as a chaos test. Most
of its library matches what Alexandria already has, but it has drifted, and it
has stayed signed in to the Onyx cloud the whole time Alexandria was being
built. This page lists what to expect, what to capture, and how to read the
result.

## What to Expect

- **A whole-library push.** Joining signs the device in, and every sign-in
  escalates to a full note-tree push ([boox-resave-waves.md](boox-resave-waves.md)).
  Expect one `NOTE_TREE` push session proposing roughly one change per
  notebook on that device, a full pull back, and per-notebook data checks.
  Migration 0032 keeps recognition through revision-only re-saves.
- **Conflicts are decided by revision ID, not by date.** When the device's copy
  of a notebook record and the server's copy have diverged from a common
  ancestor, Couchbase picks the winner by revision ID: the higher generation
  first, then the revision hash. `updatedAt` plays no part (PowerSync
  qualification, 2026-10-07). This is sharp here:
  - Every re-save wave added one generation to every notebook record on the
    server, and there have been five or six. So where the records conflict,
    the server's copy will usually win, even when the device's edit is newer.
  - The join's own full push adds one generation to each of the device's
    records, which narrows the gap but rarely closes it.
  - The losing revision isn't deleted. It remains a conflict branch that no
    device shows.
- **Page ink is separate.** Strokes live in per-page files (`shape/…`), stored by
  path. A newer upload of the same path replaces the live file, so a notebook
  can end up with the server's record (title, page list) and the device's ink,
  or the reverse.
- **Onyx cloud.** The device's sync target switches from the Onyx cloud to
  Alexandria during joining (PowerSync). Anything it synced only to Onyx stays
  there.

## Before Joining

1. Back up the notebooks you know have drifted on that device, locally (BOOX
   local backup or an export), so a losing edit can be recovered by hand.
2. Take a database dump on .90, as in the deploy routine, under
   `deploy/compose/private/device-join-20261018/`. Couchbase holds Sync
   Gateway's own data; its volume stays as it is.
3. Take a baseline (read-only):

   ```sql
   SELECT count(*) AS revisions FROM boox_revision;
   SELECT count(*) AS notebooks FROM boox_projection WHERE domain='notebook';
   SELECT state, count(*) FROM boox_page_index GROUP BY 1;
   SELECT max(id) AS last_blob_version FROM boox_blob_version;
   ```

4. Start the watchers:
   - Sync Gateway:
     `sudo docker exec aragonite-alexandria-server-sync-gateway-1 tail -n0 -F /var/log/sync_gateway/sg_info.log`,
     filtered for `_blipsync|subChanges|proposeChanges|connection closed`.
     Each push session shows as `proposeChanges #Changes: N` batches.
   - The new device's logcat, filtered for `KS-->`. Look for `build full tree
     arg`, `push tree: … dataCount=`, `SavePullNoteTreeAction`, and
     `NoteDocDataReplicator … pull`.

## After Joining

- **Volume.** New `boox_revision` rows since the baseline should be about one
  per notebook on the device, plus real edits. The page queue should show no
  requeue wave.
- **Conflicts.** Alexandria fetches every leaf revision the changes feed
  reports (`style=all_docs`, `journal.go`) into `boox_revision`, each with its
  `_revisions` ancestry (`revs=true`). `boox_projection` holds the winner, and
  its own body has no ancestry, so join the winner's revision row. A revision
  that is neither the winner nor one of its ancestors is a losing branch.
  **Baseline (2026-10-17): 0 losing branches.** All 4,013 winners carry
  ancestry, and the Palma and Lumi have never conflicted.

  ```sql
  SELECT r.document_id, p.body->>'title' AS title,
         r.revision AS lost, (r.body->>'updatedAt')::bigint AS lost_updated,
         p.revision AS won, (w.body->>'updatedAt')::bigint AS won_updated
  FROM boox_revision r JOIN boox_projection p USING (document_id)
  JOIN boox_revision w ON w.document_id=p.document_id AND w.revision=p.revision
  WHERE r.revision <> p.revision AND NOT r.deleted
    AND NOT (w.body->'_revisions'->'ids') ? split_part(r.revision,'-',2)
  ORDER BY (r.body->>'updatedAt')::bigint - (w.body->>'updatedAt')::bigint DESC;
  ```

  Rows at the top, where `lost_updated` is later than `won_updated`, are newer
  edits that lost. Recover them from the device backup. Ancestry is
  truncated after many generations, so a very old revision can show up as a
  false positive; check the generations.
- **Ink.** For drifted notebooks, compare the page files' `boox_blob_version`
  rows since the baseline (`device_id` tells which device sent them) with what
  the notebook view shows.
