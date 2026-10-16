# BOOX Re-Save Waves

2026-10-16. BOOX devices sometimes re-save every notebook record with nothing
changed. Until migration 0032 each such wave threw away the recognized text of
the whole library.

## What Happens

- Within a few minutes, almost every NOTE_TREE notebook document gets a new
  revision. Comparing the 2026-10-16 wave with each document's previous
  revision: on about 1,500 of the ~1,630 notebooks only `_rev` and
  `_revisions` changed; on 114, `updatedAt` changed too; on 4,
  `notePageInfo` changed. Hardly any page files came with it (16 blob
  versions).
- `_rev` is Couchbase's revision ID (generation-digest, a new one on every
  save); `_revisions` is the ancestry list replication uses to find common
  ancestors and conflicts. Neither is notebook content.
- Waves seen in `boox_revision` (UTC; more than 200 revisions in an hour):
  2026-10-09 06:00–08:00 (first sync after enrollment), 10-10 22:40,
  10-11 09:33 and 10-16 20:19–20:23. 10-13 19:31 also re-saved at least some
  notebooks.
- Possible triggers, **unconfirmed**:
  - The 10-16 wave began about 90 s after Alexandria published a text note
    (`create_text_note`, 20:17:44), and the 10-11 wave began about 2 min after
    a `repair_asset`. But 20-odd `set_note_status` operations on 10-13 caused
    no full wave, and nothing was published before 10-10 22:40.
  - The owner pressed Calendar Memo's cloud button around 20:19 on 10-16. It
    calls `syncMemo()` (docs/boox-calendar.md).
  - It wasn't an app update (all Palma Onyx apps were installed 10-10) or a
    reboot (the Palma booted at 16:51 UTC). The Palma's logcat begins at
    15:18:59 CDT, so the start of the wave was already gone.

## Effect Before 0032

- The trigger `boox_invalidate_page_index` treated any projection update as a
  change. Each wave cleared every page's `text`, requeued it, and took it out
  of search: 103 BOOX search rows were left out of about 1,500.
- Re-recognition made real model calls, because renderer fixes since the last
  pass changed the page images. It ran at about one page every two minutes.
- The queue was oldest first, so new pages (the owner's note-action test)
  waited behind the whole backlog.

## Fix (2026-10-16)

- **Migration 0032.** A projection update that changes only `_rev`,
  `_revisions` or `updatedAt` leaves recognition alone, and so does a page
  file rebound to identical bytes. Page content arrives as page files, which
  still invalidate.
- **Queue order.** The page index takes the newest due page first.
- **One-off repair.** Pages invalidated in the 20:19 wave whose notebook
  changed only in those fields, with no new page files since 20:15, got their
  previous result back from `ocr_text` and were set to ready (1,353 pages in
  954 notebooks; no model calls). Text notebooks' first pages and pages with
  no stored result were left to re-recognize. A dump was taken first (see the
  deploy evidence below).

## Still to Do

Find out why the firmware re-saves everything. That needs the Palma's ksync
v32173 decompiled; the earlier ADB pull stalled at 37 of 72 MB. Look at
`NoteTreeReplicator` and `CalendarTreeReplicator` (full-push and reset paths)
and at how a pulled server-written notebook (`create_text_note`) is handled.
If Alexandria's own writes trigger it, change how they are written.
