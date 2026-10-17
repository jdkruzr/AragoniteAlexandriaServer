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
   - 10-16 20:19: Sync Gateway's own info log
     (`/var/log/sync_gateway/sg_info.log` inside the container, kept about 6
     days) shows the device pulling our 20:17:44 `create_text_note` at
     20:17:47 with **0** changes to push. Then, at 20:19:14, one `NOTE_TREE`
     push session proposed 1,627 changes in 3 s (200-change batches). No new
     session grant was made that day, so this was not a login.
   - PowerSync's `NativeBridge.login` (Lumi account restore only) is a login
     too, and would cause a wave.

### Live Confirmation (2026-10-17 01:12 UTC, Palma)

The owner tapped the Notes library cloud button once. We watched the Sync
Gateway info log and the Palma's logcat. Onyx's `KS-->` lines *do* reach
logcat; the earlier waves had only rotated out of the buffer.

- **Device** (times in CDT):
  - 20:12:26 `PushNoteTreeAction: build full tree arg`
  - 20:12:33 `push tree: … dataCount=1534, completed=true`
  - ksync `SaveNoteTreeAction` took 3.7 s
  - ksync then pushed in batches (`NoteTreeReplicator: push doc count`)
  - 20:12:37–20:13:51: the full pull, with per-notebook data checks
    (`NoteDocDataReplicator … pull`, 743 notebooks) and the tree pulled back
    (`SavePullNoteTreeAction`, 1,043 sync-enabled notebooks)
- **Gateway:** one push session at 01:12:34 UTC proposed **1,630** changes
  (8 × 200 + 29) and closed at 01:12:37. The update sequence went from 15,816
  to 17,459.
- **Alexandria:**
  - It ingested the 1,630 revisions by 01:15:49, reading the changes feed 64
    at a time.
  - 1,552 changed only `_rev`/`_revisions`, 74 also changed `updatedAt`, and 4
    changed other fields. One new page file arrived.
  - Recognition touched 5 pages, all ready again within a minute. The queue
    kept draining (89 → 87). With 0032, nothing was cleared or requeued.
- Evidence (gitignored):
  `deploy/compose/private/resave-waves-20261016/button-test-20261017/`.

### What the Library Sync Button Is For

From the Notes 45960 strings and code (`NoteListFragment.onSyncNoteFolderTreeEvent`):

- **Officially, it's manual sync for when AutoSync is off.** The strings say:
  - AutoSync: *"Automatically sync data when Wi-Fi is connected"*, with
    separate options to sync when opening and when exiting a notebook.
  - Turning AutoSync off: *"After closing it, you need to manually tap the
    Sync icon to sync."*
- **Gates.**
  - It needs Wi-Fi; otherwise a "turn on Wi-Fi" dialog appears.
  - It needs a signed-in account.
  - It needs the master switch "Sync all notebooks to ONYX Cloud"
    (`KEY_ENABLE_SYNC_NOTE_DATA`). With that off, it only shows a toast, *"ONYX
    cloud sync has been turned off…"*. Manual sync isn't supported then.
  - A first-time tips dialog and a migrate-notes dialog may appear first.
- **What a tap does.** The code doesn't check AutoSync; every tap runs the
  same sequence (all seen in the 01:12 logcat):
  1. pushes every notebook record, changed or not (the wave);
  2. retries notebooks that failed to sync earlier (`setSyncFailDoc(true)`);
  3. uploads a snapshot of the notebook tree to cloud storage
     (`SaveNoteTreePBToCloudAction`, `forceSave: true`), which was the one new
     page file of the test;
  4. pulls the whole tree back and checks each notebook's page data against
     the server (743 notebooks in about 80 s);
  5. runs third-party cloud exports and old-notebook migration where they
     apply (neither did here: "has not bind third party account").
- **AutoSync, by contrast, is incremental.** In the Sync Gateway log, ordinary
  saves show up as small sessions (1–6 changes) around opening or closing a
  notebook. This comes from the log; the code that reads the AutoSync setting
  wasn't traced.
- **In practice:**
  - With AutoSync on, the button is a "resync everything / repair" button.
  - It's useful when a notebook seems stuck, or when another tablet's new
    notebooks haven't appeared.
  - It isn't needed in normal use.
  - Each tap costs about 1,600 re-saved records (harmless since 0032) and a
    couple of minutes of churn.

### Aftershock: a Slow Queue (Fixed 2026-10-17)

After the button test, the page queue ran at one page every ~109 s, even for
blank pages that need no model call. The inference host was idle. The cause
was automatic recognition's notebook check (`quietNotebooks`,
`internal/boox/auto.go`):
- It considers notebooks whose record changed since automatic recognition was
  turned on. A wave makes that the whole library (1,043 notebooks).
- For each notebook it found the newest page file with `starts_with`, which
  scans all ~41,000 live page files: about 45 s per pass.
- It runs twice per worker cycle (recognition, then minutes), so the cycle
  took ~100 s.

The fix (`cbec953-autorange1`, app-only, evidence
`deploy/compose/private/auto-range-20261017/`) looks a notebook's files up as
the C-collated key range `[prefix/, prefix0)` instead, which is an index range
scan. On production it returns the same rows with an identical checksum, in
0.55 s instead of 44 s. A regression test shows that sibling notebook IDs
(`note0`, `note1`) don't count as the notebook's files; it fails if the bound
is widened. After the deploy the 57 queued pages cleared in about two minutes.

### What to Do About It

- **Nothing on the device.** Alexandria can't change the firmware, and 0032
  already makes waves harmless for recognition and search.
- A wave still stores about 1,600 new `boox_revision` bodies. That's
  acceptable for now; deduplicating revision-only bodies would be a separate,
  owner-approved storage change.
- Open: whether ksync `switchCluster` (PowerSync connect) logs in internally.
  Its handler wasn't located in 32173.
