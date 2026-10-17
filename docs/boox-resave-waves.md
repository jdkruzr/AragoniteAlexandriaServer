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
- Triggers considered before the decompile (see Root Cause below for the
  answer):
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

## Root Cause (2026-10-16, Decompiled)

The firmware re-saves everything whenever the Notes app does a **full note-tree
push**. Alexandria's writes don't cause it, and neither does Calendar Memo.
Sources: Palma Notes 45960 and ksync 32173 (the installed versions), decompiled
with JADX, with the methods JADX failed on redone in CFR. They live under
`~/booxreverse/palma2pro/2026-10-16_ksync/`. The Lumi's ksync 32073 and Notes
match.

1. **Full push.** `BuildPushFullTreeArgsAction` pages through every local
   notebook, 20 at a time. It writes them all to one protobuf and hands it to
   ksync with `syncAll=true`.
2. **ksync saves without diffing.** `SaveNoteTreeAction` → `SaveNoteTreeRequest`
   → `NoteTreeReplicator.saveNoteTree` calls `saveList` on every entry.
   Couchbase Lite gives each save a new revision even when the body is
   unchanged, and the push replicator sends them all to the server. That's the
   wave. `updatedAt` changes only where the local row has a newer time.
3. **What starts a full push** (Notes `NoteService`, service action
   `SYNC_ONYX_NOTE` with `syncNoteTree=true`, method `R0`: full push, then a
   full pull):
   - **The cloud/sync button in the Notes library**:
     `LibraryViewModel.onSyncFolderTree` → `syncFolderTreeByUser` →
     `SyncNoteTreeAction`. The retry in the sync-error dialog does the same. The
     launcher's library (`com.onyx`) has the same action.
   - **Any incremental tree push, escalated.** After an incremental push,
     `CheckPushNoteTreeCompleteAction` compares the local count of
     sync-enabled notebooks with the "remote" total ksync returns. That total
     is the bean's `noteTreeCount`, which is only the size of *this* push. ksync
     computes the real Couchbase count (`SaveNoteTreeRequest.totalCount`) and
     never uses it; CFR agrees with JADX here. So an incremental push smaller
     than the library always looks incomplete, and a full push follows.
     Incremental pushes happen on:
     - **account login.** ksync `LoginOnyxAccountAction` broadcasts
       `KSYNC_ACCOUNT_LOGIN_ACTION`. Notes `NoteService.q` (JADX failed on it,
       CFR recovered it) runs `HandleNoteDataWhenLoginAction` →
       `PushNoteTreeAction`.
     - **Notes backup** (`BACKUP_NOTE`, requested by another app; new in 45960).
4. **Exonerated.**
   - Calendar Memo's cloud button (`CalendarBundle.syncMemo`) saves the open
     memo and pushes only the calendar tree (`PushCalendarTreeRequest`, changed
     memos).
   - Alexandria's publishes only add documents. Nothing in the pull path
     (`SavePullNoteTreeRecordAction`) starts a push.
   - Token expiry runs `PrepareSyncAction` without a login.
5. **Matching the waves.**
   - 10-09 06:00–08:00 matches enrollment, which is a login (Lumi device row at
     06:24, Palma at 07:08).
   - 10-10 22:40 came 38 minutes after the Palma's second enrollment (22:02).
   - No new session grant was made on 10-16, so the 20:19 wave fits the Notes
     library sync button. The owner had just saved the test note.
   - The release build's `Debug` logging is off, the Alexandria container logs
     reset on each deploy, and Sync Gateway barely logs. So none of these is
     proven from logs.
   - PowerSync's `NativeBridge.login` (Lumi account restore only) is a login
     too, and would cause a wave.

### What to Do About It

- **Nothing on the device.** Alexandria can't change the firmware, and 0032
  already makes waves harmless for recognition and search.
- A wave still stores about 1,600 new `boox_revision` bodies. That's
  acceptable for now; deduplicating revision-only bodies would be a separate,
  owner-approved storage change.
- Optional confirmation: tap the Notes library cloud button once and count new
  `boox_revision` rows. Expect about one per notebook within a few minutes,
  with no requeued pages.
- Open: whether ksync `switchCluster` (PowerSync connect) logs in internally.
  Its handler wasn't located in 32173.
