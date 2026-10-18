# Alexandria Server handoff

2026-10-18: **Quality implementation and test deployment complete** on
`refactor/server-quality` (runtime commit `552b9b2`; commits remain local).
All 12 batches from the [implementation log](quality-implementation-20261018.md)
are complete: shared task lifecycle, scheduler traversal, bounded CalDAV XML,
backup configuration, job claim fencing, native query bounds/page counts,
admission/auth reuse, batched search enrichment, atomic provider settings,
transport/publication helpers, safe diagnostics/shared fixtures and cached
request-local template rendering. A timing-sensitive test fixture was repaired.

The final fresh required Go 1.25 PostgreSQL/S3 race gate passes all 49 tested
packages; build, vet, packaging checks and the Hosting compatibility compile pass.
[Validation evidence](quality/20261018/validation.txt) is retained in the repo.
The existing instance now runs `quality-c5ef63223d31`: 34 live smoke checks pass,
readiness 200, zero restarts and no startup-window warnings/errors. Native
observation continues, schema/provider settings are unchanged, and synthetic
task content is purged. The prior image/source/config and a metadata snapshot
are retained for rollback; see the implementation log for scope and procedure.

2026-10-18: **Repository quality survey complete** on `audit/code-quality-20261018`,
based on `b0f6607`. The [survey](code-quality-survey-20261018.md) ranks ten
findings, records smaller DRY opportunities and sequences fixes with regression
criteria. Reproductions confirm REST task resurrection, automatic-minutes
starvation, CalDAV filter drift, backup path disagreement, stale job completion
and native page-sort drift. The [evidence bundle](surveys/20261018/validation.txt)
records a fresh required PostgreSQL/S3 race gate (47 packages), builds/vet and
packaging checks, plus isolated reproductions and local performance measurements.
Helm lint was unavailable. This branch adds survey artifacts only; runtime fixes
and deployment remain future work.

2026-10-18: **Commit attribution cleaned up at the owner's request.** Removed
21 AI co-author trailers and pushed the rewritten `main`; every historical
commit retains its exact file tree, and Hosting's `45eb491` pin is unchanged.
The [hash lookup](history-rewrite-20261018.md) maps the previous hashes to
the rewritten history for existing deployment records. The README now states
the owner's commitment to keeping all Aragonite projects open source.

2026-10-17 (late night): **User guides** in [docs/guide](guide/README.md) (13 guides, checked against the code; the README and the old engineering notes point to them). The guide's Couchbase `cluster-init`/`bucket-create` commands were run against a throwaway `community-7.6.2` container. UI copy fixes deployed as `5075d19-copy1` (app-only, base `a2b10a66c633`: 9/9 staged, readiness 200, anonymous 401, no restarts; the only drift was README.md and install.sh, which app-only deploys don't sync). Findings, 2026-10-18 (`1b80fcd-auto1`, app-only, base `5075d19`: 14/14 staged, readiness 200, anonymous 401, no restarts):
- **Fixed:** `/metrics` needs the owner's login (Basic or API token). Live: anonymous 401.
- **Fixed:** automatic processing is on by default wherever its models are set up, stored as opt-outs. Its start time is the `automatic_since` setting, set the first time there's something to run. Live, the owner chose to have it on: the clock started at 2026-10-18 08:13:33Z, and nothing was swept (0 pages queued, 0 minutes running).
- **Fixed:** `.env.example` and the installer's `.env` list the BOOX and host-directory settings.
- **Open:** a BOOX install on named volumes has no supported backup. `backup.sh` refuses BOOX, and `backup-cold.sh` needs `ALEXANDRIA_DATA_DIR`. Plan, awaiting go-ahead: [boox-volume-backup-plan-20261018.md](boox-volume-backup-plan-20261018.md).

2026-10-17 (late night): **History rewritten** to remove a real person's name, library-derived terms and the owner's host details ([history-rewrite-20261017.md](history-rewrite-20261017.md)). Most commits have new hashes (Hosting's `45eb491` pin is unchanged); that page maps old to new. The README was rewritten for users, with screenshots from a made-up demo library (`scripts/docs-screenshots.sh`).

2026-10-17 (night): **Loose-ends batch** (`2a30efe-batch1`, app-only: drift 0, 21/21 staged, readiness 200, anonymous 401, no restarts). The owner's picks from the remaining-work list:
- **Jobs page:** new Note Actions and Audio Note Minutes sections (counts by state, running or failed rows, links to the page or minutes).
- **Status lines translate:**
  - Counted sentences use plurals ("Made 2 tasks." → "Se crearon 2 tareas.").
  - Provider HTTP errors and the minutes and typed-text notices are catalogued.
  - Multi-sentence details translate sentence by sentence.
  - Spanish strings are drafts.
- **Recordings know their pages** through their chips ([note-minutes.md](note-minutes.md#recordings-and-pages-2026-10-17)): "On page N" on minutes, players on the page view, and pages in the summary prompt (`minutes-v3`, so existing minutes show as out of date, as the owner accepted).
- **Typed-text images are drawn** ([boox-text-notes.md](boox-text-notes.md#images-2026-10-17)). The one real image, a plain WebP, decodes at its native 553 × 420.
- **Intro rewritten:** the AI Tools and Recognition intro now covers the whole page.
- **Scratched by the owner:** the Buy milk fix (it was a test), Calendar Memo CalDAV (a third-party CalDAV app instead), the second client and task edit/delete (DAVx5 and Vivaldi already work), and audio in the Client viewer (the Android Client can't record).
- **Accepted:** re-save waves stay as they are (harmless).
- **Gate:** fresh full race gate (47 packages), vet and gofmt pass. Previews were checked at desktop and phone width.
- **Tomorrow:** the owner joins a third BOOX device: [boox-device-join-20261018.md](boox-device-join-20261018.md). Conflicts are decided by revision ID, and the server's records carry inflated generations from re-save waves. Today's baseline has 0 losing branches across 4,013 winners.

2026-10-17: **Branches merged; work is on `main`.**
- Server: `main` was fast-forwarded to `alexandria-sync-port` (169 commits, `e988e20`). `alexandria-sync-port` and the already-contained `integration/alexandria-foundation` were deleted. AGENTS.md now says to work on `main`.
- rhizome: `main` was fast-forwarded to `integration/forestread` (8 commits; `1a5461a` is the commit this Server pins), and the branch was deleted.
- PowerSync: 6 commits from 10-15 that had never been pushed are now on GitHub.
- AragoniteAlexandria: two stale local branches were deleted (nothing on them was missing from `main`).
- Unchanged: Hosting (it has only `integration/alexandria-foundation` and pins an older Server commit) and PowerSearch's uncommitted page-count audit.

2026-10-17: **Settings tab renamed "AI Tools and Recognition"** (owner's name; `fb723fc-aitools1`, app-only: drift 0, 14/14 staged, readiness 200, anonymous 401, no restarts).
- Renamed: the page title, heading and tab label, plus every "Settings → Recognition" pointer in pages, worker messages and help. Spanish was carried over as "Herramientas de IA y reconocimiento".
- Unchanged: the URL (`/settings/recognition`), the top-level Recognition (jobs) link and the Handwriting Recognition feature cards.
- Fresh full race gate (47 packages) passes. Previews were checked at desktop and phone width.

2026-10-17: **Embedding settings moved to Settings → Recognition** (`405d385-embedtab1`, app-only: drift 0, 7/7 staged, readiness 200, anonymous 401, no restarts).
- The endpoint and model card is now **Search by Meaning** at `/settings/recognition#embedding`, beside the other AI models. Settings → Search keeps the index status, actions and coverage, with a pointer to it.
- `back()` now puts the notice before any `#fragment`; saves that returned to `#note-actions` used to lose their notice.
- Fresh full race gate (47 packages), vet and gofmt pass. Spanish strings were added (drafts).
- The owner saved the gpu-host embedding URL; each index keeps the endpoint it was built with. The replacement index built on gpu-host at 02:58 UTC: 1,633 records in about 70 s, against about 12 a minute on the VM CPU. The VM-built index is retired. The VM's Ollama container was then removed (`ALEXANDRIA_OLLAMA_ENABLED=false`; model volume kept; `.env` backup in `private/retire-vm-ollama-20261017/`).

2026-10-17: **Models moved onto gpu-host's GPUs** ([deployment-local-models.md](deployment-local-models.md#gpu-layout-2026-10-17)), the plan agreed on 2026-10-14.
- vLLM now uses 0.83 GPU memory and a 131,072-token context, down from 0.92 and 262,144.
- Whisper runs on GPU0 (CUDA, `int8_float16`). A 20 s clip takes 0.6 s.
- A new Ollama 0.40.3 runs `nomic-embed-text:v1.5` on GPU1 at `http://192.0.2.20:11434`. It is open on the LAN by the owner's choice. Its embeddings match the VM's to cosine 1.000000.
- Units and backups are on gpu-host, and the repo has `deploy/ollama-embed/` and an updated `deploy/whisper/`.
- **Pending:** the owner switches Settings → Search to the gpu-host URL, which starts a replacement index build. After that the VM's bundled Ollama container can be retired.

2026-10-17: **Slow page queue after a re-save wave, fixed** (`cbec953-autorange1`, app-only: drift 0, 1/1 staged, readiness 200, anonymous 401, no restarts; evidence `deploy/compose/private/auto-range-20261017/`). After the sync-button test, recognition moved one page every ~109 s with the model idle. Automatic recognition's notebook check scanned every page file (`starts_with`) once per notebook, and the wave made every notebook a candidate. It now uses an index range on the C-collated key (0.55 s instead of 44 s on production, same rows). The 57 queued pages cleared in two minutes. Fresh full race gate (47 packages), vet and gofmt pass. Details in [boox-resave-waves.md](boox-resave-waves.md).

2026-10-16 (after midnight): **Re-save waves explained** ([boox-resave-waves.md](boox-resave-waves.md#root-cause-2026-10-16-decompiled)). Palma Notes 45960 and ksync 32173 were decompiled (JADX, plus CFR where JADX failed) under `~/booxreverse/palma2pro/2026-10-16_ksync/`. A wave is a full note-tree push. Notes sends every local notebook and ksync saves each one without diffing, so Couchbase Lite gives each a new revision. Full pushes come from the Notes library's cloud/sync button, and from any incremental push (account login, Notes backup). An incremental push always escalates, because the completeness check compares the local notebook count with the size of that push. Calendar Memo and Alexandria's writes are cleared. Confirmed live at 2026-10-17 01:12 UTC: one tap of the Palma's Notes library cloud button pushed 1,630 changes in 3 s. 1,552 were revision-only, and recognition requeued nothing (0032 works). Nothing to change on the device. No code change, no deploy.

2026-10-16 (late night): **Re-save waves fixed, library restored, first live note action** ([boox-resave-waves.md](boox-resave-waves.md)). At 20:19 UTC a device re-saved every notebook with only `_rev`/`_revisions`/`updatedAt` changed. The page trigger cleared and requeued ~1,525 pages (BOOX search fell to 103 rows) and re-recognized them at about one page every 2 minutes; four earlier waves did the same. Migration 0032 (`aafaec3-resave1`, dump first) ignores revision-only record updates and page files rebound to identical bytes, and the page queue is now newest first. A repair (owner-approved; SQL kept in `private/resave-waves-20261016/`) restored 1,350 pages from their stored `ocr_text` with no model calls (search back to 1,463 rows). The grid template's canvas-sized `clipPath` is now accepted (`f52d62c-svgclip1`). Note actions then worked live: blue "20261030 at 3PM: Buy milk" became an event and red "Hello" was judged not a to-do (see boox-note-actions.md). Why the firmware re-saves everything was explained afterwards (entry above).

2026-10-16 (night): **BOOX Note Actions and a CalDAV calendar** ([boox-note-actions.md](boox-note-actions.md), migration 0031). Red BOOX ink becomes tasks and sky blue ink becomes calendar events. The colored ink is recognized on its own and turned into one item per passage by the summary model, with a link back to the page. Each passage is acted on once and never edited afterwards. It is on by default wherever automatic recognition, OCR and a summary model are configured (the owner's rule), and applies to ink written from then on; a per-page **Find actions** button reads older ink. New: `alexandria_events`, served as a second CalDAV collection (VEVENT, two-way, time-range queries, sync-collection); a `/calendar` page; MCP event tools; and a time zone setting under Tasks & Integrations, which is **not set yet**, so the default is UTC. Fresh full race suite (46 packages), vet, gofmt and Compose tests pass. Deployed `9999484-actions1` from the working tree (uncommitted): drift 0, 40/40 staged, dump, migrate, grants, readiness 200, anonymous 401, no restarts. Evidence is under `deploy/compose/private/note-actions-20261016/`. Live tests on the Palma and Calendar Memo are pending.

2026-10-16 (late): **BOOX Calendar Memo, explored only** ([boox-calendar.md](boox-calendar.md)). A Reminder made on the Palma never reached the server because Reminders are local-only by design (the app's own settings text says so; app-private `ReminderModel`, no sync code). Only handwritten Memos sync (`{uid}-CALENDAR_TREE`, 34 docs already stored as opaque and not shown); Events sync only with Google, Outlook or CalDAV. Nothing built.

2026-10-16 (evening): **BOOX tags** ([boox-tags.md](boox-tags.md), migration 0030). The device's lasso tags (`TagModel`, one row per tag per page) were already reaching the server as NOTE_TREE `modeType` 1 documents and being ignored; 97 had arrived, 73 live (67 on pages, 6 tag-manager tags). Now: view `boox_tag`; a recognized page's tags fill its search `keywords` (weight B; no re-embedding); search results and MCP `search_notes` list page tags; `/boox/tags` (list and per-tag pages; BOOX → Tags); the notebook view shows tags; Audio Note Minutes tells the summary model the notebook's tags and up to 50 library tags so keywords reuse their spelling, counts the notebook's own tags in the input hash (untagged notes hash as before), and shows keywords that are tags as linked filled chips. Read-only: nothing is written to devices; creating tags from keywords is a possible phase 2 needing device qualification. Fresh required PostgreSQL/S3 full race suite (45 packages), vet, gofmt and Compose tests pass; previews pass desktop/mobile/dark. Deployed from the working tree on `383b25b` (uncommitted at deploy time): `383b25b-tags1` with the migration procedure (19/19 staged, drift 0, dump, migrate, grants; readiness 200, anonymous 401, no restarts) copy fix `383b25b-tags2`, and `383b25b-tags3` (app-only, image `sha256:3f2b00eec4f1450e6ff4a1e01b02014aabc4240dca3ed150602ac94d40e13a6b`): **Tags** is now a BOOX tab between Reading and Devices (the More-menu entry is gone) and the tag table fits phones. Evidence and `postgres-before.dump` under `deploy/compose/private/boox-tags-20261016/`, rollback tag `before-boox-tags-20261016`. Live: 73 tags, 31 tagged search rows, search content unchanged against the dump (1,445 pages, 158 reading, 1 minutes), no embedding work queued, no errors. "Vega" finds 3 pages only through tags; the "mulch"-tagged page ranks 13th of 49 (a tag counts about like two mentions). Existing tag documents keep `supported=false` in diagnostics until they next change (display only). **Live test (owner, 19:46–19:56 UTC):** tags NAS, HA100 and RAID on "20261015 Another Audio Test" arrived about a minute after the note was closed (the firmware pushes tags with the note tree on save). Tagging rewrites the tagged shapes, so each save re-recognized the page (one recognition call per tagged save). Re-summarized minutes kept "RAID" and chose the more specific "Greenlime HA100" and "Seed Savers" (the review channel) over the HA100/NAS tags; only exact (case-insensitive) matches are linked, which the owner judged right as is. "Greenlime" is a recognition misread of Greenline. After the owner added a Greenline tag (20:17), the next run wrote "Greenline HA100" in the topic and conclusion although the page text still says Greenlime, and its keywords became HA100, NAS, RAID, Greenline, smart home (4 of 5 exact tags): tags work as a spelling list for the minutes.

2026-10-13: The shared-crystal/Greek-library icon is wired into the embedded web
header, browser favicon and README. Reusable master and prompt are retained in
`assets/`; `python3 assets/export-icons.py` regenerates the web sizes. Fresh web
template tests and direct handler checks pass for the rendered page and both PNG
responses. Deployed with the UI reorganization below (`9fd0fed-ui1`).

2026-10-14 (afternoon): **Recognition holdouts — 6 of the last 8 pages fixed, 2 blocked on purpose.** Deployed `b1cc753-holdouts1` … `-holdouts10` (rollback tag `before-holdouts-20261014`, evidence under `deploy/compose/private/holdouts-20261014/`), each with matching staged hashes, readiness 200, anonymous 401, no restarts; fresh required PostgreSQL/S3 runs for `booxpage`, `boox` and `alexandria/...` pass. Firmware facts come from `~/booxreverse`; tablets were read only, except that "20260518 AI Outcomes Working Group" was opened on the Lumi at the owner's request.
- **Zero-filled shape archives**: the firmware writes session zips in place (zip4j, no temp-rename, no fsync, errors swallowed), so a crash can leave a correctly sized file of zeros. Two such zips (2026-05-18 13:56) were identical on Lumi and server. The tablet draws from its private per-notebook DB, never rereads a recorded zip, and ksync compares names only, so nothing repairs them. The renderer now skips zeroed zips and **recovers their strokes from point files**: the firmware matches points to shapes by shape ID across all of a page's point files, so a stroke whose ID no surviving shape (live or tombstone) carries is drawn with the last pen used before the first zeroed session, with a notice. Only runs when a zeroed zip exists; erased ink stays erased (tombstones carry the ID). Verified against the Lumi screenshot.
- **Misplaced data descriptor**: a 2024-09-30 firmware wrote three stray bytes before an entry's data descriptor; Go's `archive/zip` then reports a checksum error. Entries are now accepted when the inflated data matches the central directory's size and CRC (damage still fails). Fixed Research Technology Architecture p4 and Research IT Touchpoint p2.
- **Universal `radius`** is never read by the firmware's renderers (`Oval` is a plain `addOval`); no longer refused.
- **WaveLine** now follows `WaveLine.loadRenderPath`: period `wavyLength+2w`, control height `wavyPeak+w/2`, two quads per period, cut by arc length, rotated about the start; `wavyOffset` unused. Every earlier wave render had the wrong shape.
- **Shape limit** counts distinct shapes (whole-page rewrites repeat every record; 500k record ceiling). "20250207(1)" (a 15,772-stroke, 2.14M-point test drawing) stays blocked by the 500k point limit, by the owner's choice.
- **Pages with no files on the server** (e.g. the empty "20250829 Presidio Session Notes", empty on both tablets) stay blocked with "No files for this page have reached the server; it may be empty on the device, or still syncing." An attempt to treat them as blank (`f9bf0ca-holdouts8`) was deployed by mistake with two failing tests and reverted within minutes (`fc2322b`): empty and not-yet-synced pages are indistinguishable from the server, so blank would be a false negative. No pages were processed under it.
- Later the same afternoon (`-wording1` … `-busy1`): Jobs gained **Retry these N jobs** for a failed/blocked filter (whole list, refuses a stale count); "Include superseded attempts" now reads "Show earlier attempts at the same page"; work that stops retrying keeps its last problem ("Stopped after 5 attempts. Last problem: …") instead of blaming the provider. Preview errors now **block at once with their reason** unless files are missing (`booxpage.ErrUnavailable`), the preview slots are busy, or the worker's 20 s limit expires, which retry. `boox-coverage` reports the worker's own render (`WorkerError`, `WorkerMillis`): Notebook-10-32_35 took 28 s because its grid template is 1,064 SVG `<rect>`s through rasterx (13.8 s locally); rectangles now take the ruled-line fast path (31 ms, 35 of 2.6M pixels differ slightly), and the page renders in 0.5 s.
- **Recognition skip (migration 0027, `2ba4a74-skip1`)**: new `skipped` page state on `boox_page_index`, `alexandria_page_dirty` and `alexandria_recognition_job`, plus `alexandria_recognition_skip` (notebooks). Triggers keep skips through device changes, Client sync edits/"reprocess" and new pages of skipped notebooks; `queueTx` refuses skipped notebooks (`ErrNotebookSkipped`), library runs mark them excluded. UI: row Skip/Un-skip, bulk "Skip them", notebook switch on Choose pages…, "Skipped by you" in run tables. See docs/provider-settings.md. Deployed with the full migration procedure (evidence and `postgres-before.dump` under `deploy/compose/private/recognition-skip-20261014/`, rollback tag `before-recognition-skip-20261014`); ledger shows 0027, runtime role has DML on the new table, constraints and insert triggers verified, readiness 200, anonymous 401, no restarts, no errors logged. The piped remote script ended after `migrate` because `compose run` read stdin, so grants and start were run by hand (~2 min down); the script now closes stdin. Fresh full suite (45 packages), `go vet` and compose unittests pass.
- After the retries: 1,622 pages ready; blocked only "20250207(1)" (point limit) and Presidio (no files on the server).
- Evening: markers (type 15) now darken-blend under the writing like the firmware's `MarkerScribbleShape` (`KEY_UNDER_CONTENT`, default true); they had hidden text such as "This is a highlighted thing." `boox-coverage -notebook ID -page ID` renders any one page. Search-by-meaning settings show a build's progress, pace and time left (a first build used to read "0 records indexed"). Library run totals count only each page's current attempt.
- **Migration 0028 (`b4a3140-rerun1`)**: re-queueing (library runs, "Recognize this page"), transient failures and blocked outcomes no longer clear a page's text; `boox_search_page_text` includes any page with text, whatever its queue state. Text is still cleared by a change on the device. Before this, a library re-run would have taken every re-queued page out of keyword and semantic search and re-embedded all of them. Evidence and `postgres-before.dump` under `deploy/compose/private/search-keeps-text-20261014/`, rollback tag `before-search-keeps-text-20261014`; after deploy search content was unchanged (1,464 pages, 158 reading records) and the embedding build continued (553 done, 0 failed). Helper scripts now live in `~/.cache/alexandria-ops` (gate runner and both deploy scripts).
- Worker (`-loops1`): embedding runs in its own loop beside recognition, and worker loops skip the 5 s poll wait while work remains. Library-run preparation went from ~4.4 to hundreds of notebooks a minute (1,038 prepared in under 10 minutes).
- Direction (owner, 2026-10-14): model services sit behind HTTP endpoints, never on the app VM's CPU. After the re-run: on gpu-host lower vLLM to ~0.83 GPU memory and max-model-len 131072 (KV was 10.26 GiB/GPU = 304,549 tokens), move Whisper to GPU0 and serve nomic-embed-text from GPU1, then point Search at it (replacement index). Later: identify AWS options for embedding and transcription.
- 2026-10-15 (`-live1`, `-titles1`): progress pages update in place (`static/live.js`, see docs/web-ui.md "Live progress"). The BOOX re-run 69388f86 finished with all 1,622 pages ready. Run labels: only failed, blocked, paused or unavailable work is "Needs attention" (cancelled, owner-skipped and earlier-job pages are decided); catalogs return `recognition.ErrNotebookDeleted` and runs list such notebooks as excluded "Deleted on the device."; pre-run skips count as "Skipped by you". One-off data correction: run ec923e9c's "Oh hey there" row (deleted on the device, status 0) set from unavailable to excluded by SQL, since a confirmed run can't re-check catalogs. Headings, table headers, page and help-tip titles are now Title Case (rule in docs/web-ui.md).
- 2026-10-15: **first live Audio Note Minutes run, end to end** on "20261015 Another Audio Test" (BOOX): 6.5 min recording (389,304 ms) transcribed by Whisper large-v3-turbo on gpu-host CPU into 66 segments (en); its page recognized by Qwen; minutes by qwen3.8-27b with 6 keywords, 5 key points and a conclusion; 2 min 11 s from request to ready, 0 retries; minutes searchable; one BOOX operation published since. Owner: "some impressive functionality."
- Also 2026-10-15: imported-PDF pages without ink are content, not blank (`-pdfblank1`); a re-run comparison against the 22:44 backup found 166 changed pages (111 gained text, mostly from highlighters); the 4 remaining "losses" were model filler on empty pages or a misread under a highlighter. "20241231 Dad's Letter Back to Me" was restored from cache with no model calls.
- `-auto1`: Settings → Recognition → **Automatic Processing** (off by default): recognize new BOOX pages (quiet 5 min) and summarize notes with new recordings (quiet 10 min), only for notebooks changed after it was turned on; details in docs/note-minutes.md. The minutes page's "On Your BOOX" now says a sent note syncs on its own and resending is "Replace It on Your BOOX" (it never summarizes again).
- **2026-10-16 (`0004748-tasks1`, migration 0029; `634bebf-typed1`):** Tasks is the home page (overview at `/library`), with bulk Mark done/Reopen/Delete, Restore and confirmed **Delete permanently** (UID ledger `alexandria_task_purged`; CalDAV PUTs of deleted or purged UIDs get 412, so no device can bring them back), Edit, Created/Updated dates and sorting. BOOX text notebooks now show their typed text in the notebook view (display only; recognition stays ink-only). Details: [tasks-and-text-notes-plan-20261016.md](tasks-and-text-notes-plan-20261016.md#built-2026-10-16). CalDAV verified both ways with the owner's client; UltraBridge names removed from CalDAV.
- **Internationalization** (2026-10-15): web UI translatable and deployed with a Spanish draft (893 messages, needs native review), language picker in Settings → Account, and a catalog test that fails when any language falls behind English. PowerSync done (0.2.0). Step 5 done: minutes-note headings follow a setting (English, match the recordings, or a language); the Template Hub notice follows the tablet's language. Step 6 done: numbers and relative times follow the language; dates stay ISO-style. Spanish native review pending (owner recruiting testers); split-sentence fragments fixed; remaining: Weblate. Plan and gaps: [i18n-plan-20261015.md](i18n-plan-20261015.md); how to translate: [translating.md](translating.md).
- **BOOX cloud services after the PowerSync cluster switch** (2026-10-15): **fixed for firmware updates on both tablets.** Profile split (firmware/log/shop hosts on stock Onyx), PowerSync 0.1.8 in-place **Update server endpoints** (applied and verified on Palma and Lumi; reboot after), scriptless Template Hub notice, unknown-request logging. Feedback stays unsupported by owner decision (Onyx web app or a temporary PowerSync return instead); own Template Hub page proposed. Details: [boox-cloud-services-20261015.md](boox-cloud-services-20261015.md).
- Later: gpu-host GPU change for Whisper and embeddings (see the direction above).

2026-10-14: **Renderer gaps closed for every blocked page** (found with `boox-coverage`; shape meanings from the decompiled firmware in `~/booxreverse`). Deployed in steps `70b3366-render1` … `6aea3f5-render7` (rollback tag `before-renderer-gaps-20261014`, evidence and private renders under `deploy/compose/private/renderer-gaps-20261014/`), each with matching staged hashes, readiness 200, anonymous 401, no restarts; fresh required PostgreSQL/S3 full race suite and vet pass. After the last step, all 19 blocked or failed pages render with no blocking warning (the PowerSync fixture's only notice is the text-notebook one the worker ignores).
- **Type 24 = SHAPE_REGULAR_POLYGON** (27 = hexagon): built from the pen-down and pen-up points in the point file as `PolygonShapeUtils` does: centre at the down x and mid-height, the up point on the circumcircle, first vertex straight up. The side count isn't persisted, so 24 is always the default pentagon. Verified visually on Ink Performance Test.
- **Vertical text** (`orientation` 1): text without CJK runs sideways (90° clockwise) down right-to-left columns and shrinks to fit the stored box (the stored size can't fit it); CJK characters stack upright. A notice records the approximation. Horizontal text boxes now grow downward to fit wrapped lines instead of clipping (this fixed "Tenant WiFi" losing its second line).
- **Draft notebooks** (`activeScene` 4 = DRAFT_SCENE, a shared global page model): the preview grows to fit ink beyond the stored page size, with a notice, instead of blocking.
- **Type 210 = SHAPE_BOOKMARK**: a navigation marker, not drawn; its label is kept as page text.
- **PDF templates** are stored at `note/templateRes/<id>.pdf` although the virtual page names `resource/data/<id>.pdf`; the background loader falls back to that (Preflight II).
- **Meeting notebooks** (`activeScene` 2, Talk Insights): the audio/transcript panel isn't shown; that is now a notice, not a block, since the handwriting pages are ordinary (20260930 Team Sync). `6aea3f5-render7` also makes `boox-coverage` include failed pages; Notebook-10-32_35 p1 failed after five attempts during the deploys and now renders cleanly (transient).
- **Zero-filled templates**: 11 template files across 5 notebooks (SciComp Stand-Up, Deepak Sync, Scientific Computing Team Sync, 20250927 To-Do, Amazing Fall Cocktail) are 498 zero bytes, byte-identical on Lumi and Palma (read-only ADB hash), so the corruption predates Alexandria. Pages draw without the background, with a notice. Any other unreadable template still blocks and now names the decode problem.

2026-10-14: **Why pages are blocked.** Blocked recognition pages now store the actual coverage warnings ("Not sent for recognition because the preview is incomplete: …") instead of a generic message. The renderer attributes each warning to the shape that raised it (`booxpage.Page.Issues`, with a structure-only `ShapeSummary`: proto field names, JSON key names, sizes, resource extension; never text or ink). `alexandria-server boox-coverage` prints that report for every blocked page as JSON, read-only, using the runtime database and object store. Run it inside the app container: `docker exec aragonite-alexandria-server-alexandria-1 alexandria-server boox-coverage [-render DIR]`. The container root is read-only and `/tmp` is a tmpfs `docker cp` can't see, so stream renders out with `tar cf - .`. Fresh required PostgreSQL/S3 full race suite and vet pass. Deployed in three app-only steps during the library run (`bc0c684-coverage1`; a fix to scope the subcommand's object store with `blob.ForLibrary`, c4d743a, deployed under the mistyped tag `8a-coverage2`; then drawing-stage warnings, `cccf69f-coverage3`, image `sha256:10319135a9472319e4e8daf8c9ccd221aaa658356da4e2918134bd39bf176cbb`), each with matching staged hashes, readiness 200, anonymous 401 and no restarts. Rollback tag `before-blocked-coverage-20261014`; report and evidence under `deploy/compose/private/blocked-coverage-20261014/`. **First report (11 blocked pages):** PowerSync fixture p1, only the text-notebook notice (now ignored; retry completes it). Native shape type 24 (fields boundingRect/createArgs/lineStyle/matrixValues, no points or text, roughly 400–830 px boxes) on Weights and Biases Sync p1 and Ink Performance Test(1) p1. Type 16 text with unsupported styling on Screwing Around on Bsky p3. Infinite-1 (`activeScene` 4, "draft" layout not qualified) p1–4, plus type 210 on p1 (extra keys allAudioData, originRectF, textContent, textSize). Drawing stage: Preflight II p1 "page PDF has not arrived or failed verification"; SciComp Stand-Up p1–2 "Template metadata is unreadable".

2026-10-14: **Blank pages and text notebooks in recognition.** A page with no shapes is now finished as ready with no text and is never sent to the model ("No handwriting on this page"). Text notebooks (`activeScene` 1) no longer block their handwriting page on the notebook-level rich-text notice (`booxpage.RichTextNotebookWarning`); other coverage warnings still block. Their typed text is read from the editor resources (`booxpage.RichText`, which understands the docVersion 2 editor JSON and falls back to HTML) and stored as the text of the notebook's first handwriting page, so it is searchable and embedded without a model call; ink on that page is recognized as usual with the typed text added. Edits re-queue the page through the existing invalidation trigger, so typed text stays current. The notebook viewer's page text for rich-text shapes uses the same extractor (it previously showed raw editor JSON). No migration. Fresh required PostgreSQL/S3 full race suite and vet pass; new tests cover typed text reaching search without recognition, blank pages, and the extractor. Search shows these hits as "Recognized page". The first library run (`ec923e9c…`, 1,624 pages, started 07:39 UTC at about 8 pages a minute on gpu-host at 200 W) had one blocked page (a PowerSync fixture text notebook), which "Retry failed and blocked pages" now completes. Running several recognition requests at once (gpu-host idles between pages) is deliberately deferred to a future session. Deployed `c00ede7-textpages1` (image `sha256:d72d41f85b4496834232fb39ac056c5aa3990611b9fa066dfde1c6537859010d`): 4 staged hashes match, readiness 200, anonymous 401, no restarts, no error logs. Rollback tag `before-blank-and-text-pages-20261014`.

2026-10-14: **Library recognition page, friendlier.** The review now opens with a summary table and a single button that names what it does ("Send 1,624 pages to <model>", updated live as notebooks are excluded); pressing it is the confirmation, replacing the separate required checkbox (the provider-charge warning stays beside the button). The notebook list folds into "Exclude notebooks (N)" with a filter box. The progress view is tables: done/pace/time left (pace over the last ten minutes, projected finish), page states and notebook counts; the per-notebook table folds away with a filter. Fresh required PostgreSQL/S3 full race suite and vet pass; the run test covers pace and ETA. Previews pass desktop/mobile/dark. Deployed `7288dce-libraryui1` (image `sha256:00cf05a45dd8dbb62ce55dbf9bd4514c21164e5f2134d8ec1ccb21a6dbbe4566`) while the first full BOOX library run was recognizing: VM matched the previous commit, 6 staged hashes match, readiness 200, anonymous 401, no restarts, no error logs; pages kept finishing across the restart and the interrupted page returned to the queue after its lease. Rollback tag `before-library-run-ui-20261014`; evidence under `deploy/compose/private/library-run-ui-20261014/`.

2026-10-14: **Editable summary instructions** for Audio Note Minutes. Settings → Recognition → Audio Note Minutes → Summary instructions shows the built-in instructions in a textarea; the owner can rewrite them, and clearing the box restores the default. The JSON response format the parser relies on is fixed, always appended, and shown read-only under "Response format (always added)". Stored as `SummaryConfig.Instructions` (provider settings JSON, no migration; saving the default text unchanged stores blank). `minutes-v2`: the input hash now covers the full system prompt, so changing instructions marks minutes out of date without regenerating anything. Fresh required PostgreSQL/S3 full race suite, vet and Compose tests pass; tests cover custom instructions reaching the model, the appended format, staleness on change, CRLF normalization, default-as-blank and the size cap. Settings previews pass desktop/mobile/dark. Deployed `842ece4-instructions1` (image `sha256:76be0b88df5e4c323103d01dd5bc9a2d2cf3ed3a95adad2297dc299aaed88963`): 10 staged hashes match (VM matched the previous commit first), readiness 200, anonymous 401, no restarts, no error logs. Provider settings at revision 9 (owner-configured, both providers enabled) using the default instructions. Rollback tag `before-minutes-instructions-20261014`; evidence under `deploy/compose/private/minutes-instructions-20261014/`.

2026-10-14: **Automatic Send to BOOX** for Audio Note Minutes. Settings → Recognition → Audio Note Minutes → "When minutes are ready → Send them to BOOX automatically" (`SummaryConfig.SendToBOOX` in the provider settings JSON; no migration). When on, the minutes worker runs the same send as the button, with the same replacement rules, as soon as minutes become ready. The send is keyed on the minutes request ID, so replays never duplicate it. A failed send leaves the minutes ready and shows the reason on the minutes page. Existing minutes aren't sent when the option is turned on, and the Summarize page mentions it when it's on. Fresh required PostgreSQL/S3 full race suite and vet pass; the end-to-end minutes test now covers automatic sending and replay, and the provider store test covers persistence across the recognition form. Deployed `f8328e4-autosend1` (image `sha256:3af919ec38ff65d74c4d485c8836687fb9e801bf078a1904528e1303f1270360`): 9 staged hashes match, readiness 200, anonymous 401, no restarts, no error logs; provider settings still revision 8, option off. Rollback tag `before-minutes-autosend-20261014`; evidence under `deploy/compose/private/minutes-autosend-20261014/`.

2026-10-13: **Send Audio Note Minutes to BOOX** ([details](note-minutes.md#send-to-boox)). The minutes page's "On your BOOX" card creates `<notebook title>_Minutes` as a native text note in the source notebook's folder, using the text-note path qualified on Lumi and Palma earlier today. The note's wording is a template (bold Topic/Keywords lead-ins, key-point bullets with their source, conclusion, metadata). Resending replaces the earlier note only while it's untouched: same title and folder, and every live file except the page model byte-identical. The retirement is a `set_note_status` queued after the new note and fenced on the record revision. Edited, inked, renamed, moved or deleted notes are kept, and a new note is added. The test form also accepts `* `/`• ` bullets and `**Label:**` bold lead-ins. Fresh required PostgreSQL/S3 full race suite and vet pass; resend-policy integration test covers pending, untouched after open, ink, text edit, deletion, ordering and a stale-revision fence. Send-card previews pass desktop/mobile/dark. Deployed `436fbdf-minutessend1` (image `sha256:52c1ae8c816f9d8917f8154cea902fc5e5afc78afb48b5d7f28a0f2ff8c65dbe`): 7 staged hashes match, readiness 200, anonymous 401 on send and minutes, no restarts, no error logs. Rollback tag `before-minutes-send-20261013`; evidence under `deploy/compose/private/minutes-send-20261013/`. Not yet exercised live: there are no minutes until the transcription/summary providers are configured. Bullet lists and bold have still not been seen on a device; the first real send is that check.

2026-10-13: **Server-created BOOX text notebooks** ([text notes](boox-text-notes.md)). `internal/boox/text_note.go` builds a new native text notebook (nine files, decoded from a real empty Lumi text note) and publishes it through the durable journal as a `create_text_note` operation: bodies stored, keys bound, one idempotent operation, then record, commit and message. It never edits an existing notebook; a missing or deleted folder is rejected. Owner-only test form at `/boox/text-notes/new`, linked from BOOX settings. Fresh required PostgreSQL/S3 suite and vet pass, including an injected commit failure and resume. Deployed `d1fd3d3-textnotes1` (image `sha256:044236e98e5691721e2f5451a71514056ccf55d61ffea8ed565ce61014286a24`): 13 staged hashes match, readiness 200, anonymous 401 on the form, no restarts. Rollback tag `before-text-notes-20261013`; evidence under `deploy/compose/private/text-notes-20261013/`. **Device result:** the owner's first test note published in 1.8 s; Lumi and Palma both listed it with the right title and parent, held 8/9 files byte-identical to the server (neither stores the blank thumbnail), and rendered headings and paragraphs after a cold Notes start. No sync button needed. Opening a note makes each device write a new page model and bump the record version, so Audio Note Minutes resends must compare text content, not versions. Bullet lists are not yet seen on a device.

2026-10-13: **Note minutes** for BOOX notes are implemented and deployed with migration 0026 ([note minutes](note-minutes.md)). Owner-requested: every recording in a note is transcribed, then a summary model writes minutes from the transcripts *and* the recognized handwriting: topic, 4–6 keywords, ≤5 key points attributed to recording/notes/both, conclusion, and notebook/recording metadata (start time and length come from the native `resource/pb` record). Unlike firmware Talk Insights, nothing is written to Couchbase or devices. Review page lists recordings and pages without text (optionally queued for recognition first, with the usual charge warning) and requires confirmation; results are cached per recording, flagged stale when inputs change, searchable (kind Minutes, embeddings included) and available over MCP `get_note_minutes`. Audio shapes (type 23) now render as a speaker marker without a coverage warning, which had blocked recognition of pages with recordings. Separate encrypted transcription/summary credentials (purpose-bound AAD) are saved from Settings → Recognition → Note minutes. Local providers on gpu-host: new CPU Whisper service (`deploy/whisper`, 0.31× real time on a 3-minute BOOX recording; vLLM untouched) and the existing Qwen3.8-27B, which produced valid minutes from the real "20261013 Audio Test" transcript in 5 s ([local models](deployment-local-models.md)). Fresh required PostgreSQL/S3 full race suite and vet pass on the final code; synthetic desktop/mobile/dark previews pass. Deployed `be6c670-minutes1` (image `sha256:44394f1e61d8d927d59269798e047e030d006324dcea22122e6228a236463aba`) after app stop, database dump, migration and grant refresh: 28 staged hashes match, readiness 200, new routes anonymous 401, no restarts, zero minutes/transcriptions queued. Providers are not yet configured on the live server, so the live end-to-end run on the Audio Test note is pending. Rollback tag `before-note-minutes-20261013`; evidence under `deploy/compose/private/note-minutes-20261013/`. Not yet: recordings aren't mapped to pages, Client notebooks have no audio, minutes aren't listed in /jobs.

**Reading this file:** newest entries come first. Older foundation-era
pause/not-yet-ported statements below are historical; the October 6 resumption in
`AGENTS.md` and newer dated implementation checkpoints supersede them for the
resumed standalone work.

2026-10-13: Alexandria Client notebook viewer now matches BOOX: one page at a time with previous/next, a page-number jump, fit/actual zoom, an "All pages" grid and a side panel with page text and "Recognize this page…". The panel pre-fills the recognition form via new `from`/`to` parameters. Pages stay addressed by stable page ID, so existing search/task/MCP links (`?page=<id>`) still work; `n=` jumps by number. Client devices moved to `/files/forestnote/devices`; GET `/settings/devices` redirects (301) and the old POST actions remain aliases. Fresh required PostgreSQL/S3 full race suite, vet and synthetic previews pass. Deployed `11600a3-ui2` (image `sha256:203ffd5d16d60010f95a18b684972dacc2539120fa63f8361b66228648727d49`); 15 staged hashes match; readiness 200; anonymous 401; no restarts. The redirect and Client pages were checked live. The viewer itself is verified only by synthetic previews and tests, because this library has no Client notebooks. Rollback tag `before-client-viewer-20261013`; evidence under `deploy/compose/private/client-viewer-20261013/`.

2026-10-13: Web UI reorganized for people, deployed as `9fd0fed-ui1` (image `sha256:cab08fb4c056c869e4a2de29129dba012794a99161ee6c16b874f21ac0e6debc`). Global nav is now Library · Search · Tasks · Recognition · Settings + Help. The Library overview shows per-source counts, last sync/upload and recognition/semantic state. Settings is split into Account, Search, Recognition and Tasks & integrations tabs; all four keep the owner-only account gate. The combined provider form carries the other tab's saved values, verified by a round-trip test. Long explanatory text moved into `?` help popovers (native `popover`, no script) backed by a registry in `internal/alexandria/web/help.go` and a `/help` guide. Cost and deletion warnings stay inline at their confirm controls. BOOX Activity became Diagnostics (linked from BOOX Settings, paginated, filterable); BOOX devices show last upload received. Added a favicon set, a 32 px header mark, a version footer/meta tag (`ALEXANDRIA_VERSION` build arg) and an offline preview/screenshot workflow. Fresh required PostgreSQL/S3 full race suite, vet and the seven Compose tests pass. Synthetic desktop/mobile/dark previews of 27 pages pass the overflow and popover checks. Application-only deploy, no migration; 54 staged hashes match; readiness 200, anonymous 401. Live pages checked with real counts. Rollback image tag `before-ui-redesign-20261013`; private evidence under `deploy/compose/private/ui-redesign-20261013/`. See [web UI conventions](web-ui.md).

Open follow-up, 2026-10-13: independent native page-ID enumeration identified an eight-page discrepancy in the pre-cleanup OCR review: its 1,079 notebooks reconstruct 1,677 pages, while those same notebooks list 1,669 pages on Lumi. Two notebooks account for the difference (server 2/device 1 and server 15/device 8). Reconcile virtual-page membership before treating the server catalog as an exact device page census. The separate 10,000–12,000-page estimate came from PowerSearch counting file-based index records as pages; a read-only device audit found 11,822 point files belonging to 1,684 live pages across all local accounts. The audit script and detailed results live in the PowerSearch repository. No catalog reconciliation fix is included in this consolidation.

2026-10-13: First owner-run numbered-notebook cleanup verified end-to-end: all 40 operations published plus one separate device deletion; all 41 are deleted in Gateway and both native device providers. All 1,038 remaining server notebooks match on Lumi and Palma (1,615 native-listed pages). No unexpected status changes, conflict leaves, remaining current page text or derived search rows for the deleted notebooks; observer caught up at sequence 14010. All 40 commits/MESSAGEs verified, including one original MESSAGE recovered from Gateway history after native republication dropped its operation marker. Read-only verification; private evidence under `deploy/compose/private/notebook-cleanup-20261013/post-user-run/`. See [owner-run verification](boox-native-setup.md#first-owner-run-cleanup-verification--2026-10-13).

2026-10-13: BOOX numbered-notebook cleanup is implemented and deployed at `/boox/cleanup`, linked from Notebooks and BOOX settings. Exact `Notebook-[0-9]+` names across folders are reviewed with selection, dates, native listed-page counts and explicit cross-device deletion confirmation. Up to 200 entries per submission use the existing durable `set_note_status` publisher; atomic projection revision checks, authoritative Gateway revision checks and idempotency protect stale selections/concurrent submissions. Progress, recent batches and queued cancellation are available. History/assets remain retained; this is logical deletion, not storage reclamation. Fresh required PostgreSQL/S3 full race suite and vet pass; final template adjustment passes targeted race tests and nine desktop/mobile/dark browser checks. Synthetic Gateway coverage includes native metadata preservation, commit/MESSAGE, lost-ACK replay, stale blocking and search removal after observation. Application-only deployment required no migration. Public read-only verification found 40 matches, owner-only access and confirmation controls pass; provider/native/job/operation fingerprints are unchanged and all six staged source hashes match. No real deletion was queued. Image `sha256:47a10b5399c7c3b047c10e3a04dcd0c664f3d756515bf10dad877f284896da84`. Private rollback database/source/image, logs, browser/test evidence and verification are under `deploy/compose/private/notebook-cleanup-20261013/` on the host. See [cleanup behavior](boox-native-setup.md#numbered-notebook-cleanup).

2026-10-13: Alexandria Client and BOOX now share the library-wide recognition workflow. Jobs offers **Recognize a library…** with a source selector; both notebook browsers offer **Recognize this library…**. Review, exclusions, explicit provider confirmation, progress and pause/resume/retry/cancel use the same templates and controls. Source is persisted per run; URL/form changes cannot retarget it. Client-only deployments work without BOOX. Empty reviews omit queue controls; Client text can sync back to devices, while BOOX text stays server-side. Additive migration 0025 preserves existing reviews as BOOX. Fresh required PostgreSQL/S3 full race suite, vet and 24 synthetic desktop/mobile/dark browser checks pass, including source isolation with identical IDs, chunked dispatch, changed catalogs and migration preservation. Application-stop migration/grant refresh/restart completed; public authenticated pages and anonymous rejection pass. The existing BOOX review remains 1,079 notebooks / 1,677 pages. A live Client catalog-only review correctly found zero notebooks and no available pages. All 14 staged source hashes match; provider/native/job fingerprints are unchanged, with zero OCR jobs. Image `sha256:80fbb580eb314b7ae011a73fc74e9c63568631028947890b61bfc7dbfc3ac467`. Private database/source/image rollback, test/browser evidence, review ID and verification are under `deploy/compose/private/client-library-20261013/` on the host. See [shared library recognition](provider-settings.md#shared-client-and-boox-library-recognition-2026-10-13). No real pages were submitted for recognition.

2026-10-13: BOOX library-wide recognition is implemented and deployed with additive migration 0024. Jobs and BOOX Notebooks link to a durable, asynchronous catalog review; notebook exclusions and an explicit provider confirmation precede any OCR. Dispatch commits at most 100 pages with its cursor, revalidates reviewed page identities, retains active/paused jobs, and survives interruption. Library progress groups notebook batches with pause/resume/retry/cancel controls and explicit unavailable/skipped counts. Superseded attempts are hidden from global current-work counts by default but remain accessible as history. Shared BOOX/Client JPEG preparation caps images at 1,000,000 bytes and 4096 pixels on the long side, and records dimensions/bytes on jobs; compression policy participates in cache identity. Fresh required PostgreSQL/S3 full race suite, vet, synthetic provider payload checks for both API formats, and desktop/mobile/dark browser checks pass. Public Jobs/review verification passes; all 21 staged source hashes match and provider/native/job fingerprints are unchanged. A real catalog-only review of 1,079 notebooks is advancing; no page OCR was queued. Image `sha256:cd305cbb564fe6c57292a365057a21a718f4d9960113f669aff413299a13514e`. Database/source/image rollback, logs, review ID and verification are under `deploy/compose/private/ocr-library-20261013/` on the host. The migration initially waited on long-lived native admission locks; stopping only the application released them and the migration/grant refresh/restart completed. See [library recognition and image budget](provider-settings.md#boox-library-wide-recognition-2026-10-13).

2026-10-13: Settings now displays the default recognition prompt both as the empty-field placeholder and in persistent, accessible help text explaining custom overrides. Both come from the recognition pipeline's `notes.DefaultPrompt` constant; saving a blank field still selects the default. Fresh required PostgreSQL/S3 full race suite passes. Application-only deployment and authenticated public Settings verification pass; provider settings, credentials, revision and recognition job count are unchanged. Previous image/source and deployment verification are retained privately under `deploy/compose/private/prompt-help-20261013/` on the host.

2026-10-12: BOOX semantic search steps 1–5 implemented and deployed with migration 0023. Current recognized notebook text and native reading title/quote/note text feed the shared derived corpus and embedding queue; stable source/page identities, source revisions, parent visibility, immediate vector deletion, leased completion fencing and post-query candidate revalidation protect edits/deletions/restores. Web/API/MCP search supports all/client/BOOX scope with one query embedding and keyword fallback; client-only image tools are not advertised for BOOX. Reading substring matches remain compatible; page links follow stable IDs after reordering. Settings adds per-source available/embedded/pending/failed counts and listed native pages without current text. Migration backfills existing text only; no OCR is requested. Fresh required PostgreSQL/S3 full race suite and vet pass; the final substring compatibility follow-up passes fresh affected race suites. Synthetic desktop/mobile/dark previews pass; CPU Nomic ranks a relevant synthetic passage above an unrelated one (0.6697 vs 0.3048, 768 dimensions). Public verification shows 158 reading text records, zero recognized pages and 1,669 listed native pages without current searchable text; this is a native-metadata coverage count, not a claim of a complete page manifest. Provider/credential/native hashes are unchanged; semantic search stays Off with zero generations/work and zero recognition jobs. Other backing services were not restarted. Deployed image: `sha256:5d5596b04629c405590023e2c4e58be1e7649d7a13033968c31c9fbe97f46337`. Rollback database/source/image, migration/build logs and verification are retained privately under `deploy/compose/private/boox-semantic-20261013/` on the host. See [shared semantic search](architecture.md#shared-semantic-search-2026-10-13).

Test-fixture note for the BOOX semantic change: three legacy tests use the supplied base database directly. The reusable local test container's `postgres` database retains an earlier draft migration-0023 checksum; do not rewrite that ledger. The passing full gate used a fresh disposable base database via `/tmp/alexandria-fresh-gate.py`, which creates/drops only its own test database and supplies required PG/S3 settings. Reuse this approach for fresh gates.

2026-10-12: Settings now has a semantic-search panel separating saved-provider readiness, feature on/off and index progress. It offers atomic **Enable and build index**, pause/resume retaining vectors, first/replacement builds, failure retry, saved connection testing and collapsed advanced URL/model fields. Readiness uses bounded model-list metadata only, never inference. New route has owner-only authentication and CSRF protection; semantic mutations preserve OCR settings and encrypted credentials. BOOX semantic coverage remains explicitly unsupported. Full fresh required PostgreSQL/S3 race suite and vet pass; final copy-only follow-up passes the focused panel race test. Synthetic browser previews pass desktop/mobile/dark. Deployed and publicly verified: Ready / Off / Not built, unchanged provider-state fingerprint, zero embedding generations/work, other service start times unchanged. Image `sha256:5eb4ac4f9123e93dd57fd0f3170a57a21361c5f8dc39b18b2cd7e4001e5e2f9b`; private rollback image/source, build log, verification and hashes are under `deploy/compose/private/semantic-panel-20261013/` on the deployment host (UTC date). See [provider settings](provider-settings.md#semantic-search-panel-2026-10-12).

2026-10-12: Optional CPU Ollama service implemented and deployed (0.40.3, `nomic-embed-text:v1.5`, model digest prefix `0a109f422b47`). Private Compose endpoint `http://ollama:11434` is saved through authenticated Settings; semantic search remains disabled, with zero embedding generations/work and unchanged OCR configuration. Limits: one CPU, 2 GiB, one loaded model, no GPU devices/host ports, cloud disabled. Application-network synthetic requests returned 768 finite dimensions in 1.644 seconds cold and 0.570–0.596 seconds warm; Ollama reports 100% CPU. Model survives container restart; post-interruption recheck passes (0.876 seconds, healthy service, no OOM/restarts, public Settings/readiness 200). Setup helper starts only Ollama and pulls a model; deployment flag includes it in install/backup lifecycle, while defaults stay opt-in. Seven Compose lifecycle/backup tests, shell syntax, enabled/disabled profile validation and fresh required embedding/provider/library PostgreSQL/S3 race tests pass. Model volume is a disposable cache excluded from backups; re-pull after a new-host restore. BOOX semantic indexing remains future work. [Operator instructions](deployment-compose.md#optional-cpu-embedding-service).

2026-10-12: BOOX search lifecycle repair implemented: shared native visibility predicates correct the reversed NeoReader status filter and require live, same-account parent books for highlights/bookmarks in global web search and Reading, including direct links and pagination. Missing parents wait; history stays separate. Added synthetic HTTP/observer lifecycle and notebook/page/resource invalidation tests, including late OCR and restoration. Full fresh required PostgreSQL/S3 `go test -race -count=1 ./...`, vet and whitespace checks pass. Deployed application-only update; all three changed runtime source hashes match. Public Reading inventory exactly matches 158 active records (130 books, 21 highlights, 7 bookmarks); six authenticated public search/direct-link cases cover active/deleted records for all three kinds, and public readiness returns 200. All 28 active children have live parents. The previous search predicate incorrectly admitted 31 soft-deleted reading records. No migration, reindex, device changes or paid recognition run was needed. Prior image/source retained privately for rollback; existing architecture/diagram work preserved. [Contract](architecture.md#search-visibility-and-source-lifecycle).

2026-10-11: Added a separate [AWS architecture variant](architecture-aws.md) with source and native BOOX diagrams, software headers, labeled arrows and AWS deployment/VPC boundaries. Existing Terraform foundation is distinguished from target Fargate worker/Sync Gateway and EC2/EBS Couchbase deployment work. Missing wiring and qualification are pinned as **To be implemented**, including continuous recognition/native sync rather than assuming the existing Batch job runner supplies them. General diagrams are preserved; no infrastructure was changed or applied.

2026-10-11: Architecture nodes now have bold software headers above their roles, naming Alexandria Server, Rhizome, device firmware/apps and backing services. Logical repositories name their storage stack; planned implementations remain explicit. Object storage stays configurable, with SeaweedFS identified only as the bundled Compose default. The exporter preserves header weight in native SVG text.

2026-10-11: Architecture boundaries now distinguish the Alexandria Server deployment from the Alexandria application. The BOOX detail encloses application APIs/workers and private Couchbase/Sync Gateway/PostgreSQL/object-storage services in the deployment boundary, leaving device clients outside. The overview is explicitly a logical source view; deployment boundaries do not prescribe one host or storage provider. Existing labeled arrows, source colors and minimum 1024-pixel PNG short side are preserved.

2026-10-11: Both architecture diagrams now label every connection by exchanged data or operation and use a consistent source palette, with a documented color key. Planned status remains explicit in text and dashed lines. The Onyx detail uses ELK routing to avoid connections crossing intermediate boxes. Portable exports validate arrow labels as well as node labels.

2026-10-11: Architecture SVG labels now use native text rather than embedded HTML, with normalized line spacing and an explicit white background. Both diagrams were rendered and visually checked with Inkscape; librsvg generates the accompanying PNG previews. The repeatable exporter checks every node for a text label and rejects HTML labels. See [diagram export instructions](diagrams/README.md).

2026-10-11: [Architecture guide and diagrams](architecture.md) describe source interfaces and logical data repositories for first-time readers, define component names and storage responsibilities, and separate software requirements from deployment choices. The overview shows logical repositories; the BOOX detail shows backing services. Installation-specific filesystem/VM details remain in operational documentation. BOOX Couchbase authority and the separate Alexandria Client/Rhizome path are explicit; reMarkable and Supernote remain planned stubs. This supersedes the foundation-only architecture sketch; runtime behavior is unchanged.

2026-10-11: Explicit [audio recovery](boox-audio-recovery.md) is implemented and deployed. Owner notebook UI previews/publishes the retained correction; PowerSync 0.1.5 uses authenticated current-generation recovery and exact damaged-file matching to repair local MP3 caches with private backups. Paired disposable native MP3s recover all original bytes through the ordinary app UI; libraries and endpoint settings are preserved. Fresh required affected race tests, vet and signed Android release checks pass. Personal audio is untouched; PCM device transport and audible playback remain unqualified.

2026-10-11: Pre-commit qualification passes: full fresh required PostgreSQL/S3 race suite and vet; four cold-backup/restore tests. Offline restore now rejects unexpected archives and detects quoted BOOX enablement without executing the saved environment. This helper hardening does not change live services. Generated Python caches are ignored.

2026-10-11: Qualification round closed with documented limits. Two-hour observer completed: 121 samples; two readiness failures coincide with planned cold backups; no device-observation errors or unexplained catalog transitions; batteries 24.4–27.1°C. Both tablets remain on production and pass explicit native Library sync after the final restart. All temporary faults, timers, tunnel and restored stacks are stopped. Latest clean-stop backup restores all 51,982 native immutable objects exactly. Required tests pass; all 15 changed Go runtime files match deployment. See the PowerSync qualification campaign for evidence and deferred cases.

2026-10-11: Final clean-stop backup `cold-20261011T104536Z` and separate fresh restore pass: 38,999 body + 12,983 revision objects hash exactly, owner/anonymous access boundaries pass, latest app image ready after proper dependency ordering. SeaweedFS now gets 60 seconds to stop; cold backup records exit/OOM states and refuses forced-stop publication. Both changes are deployed and tested. Tablets retain production configuration; all 1,081 active sync-enabled Lumi catalog entries match Palma. The bounded observer completed at 11:12 UTC. Full evidence and limitations are in the qualification campaign.

2026-10-11: Fresh physical restore and live Palma endpoint rehearsal pass: 38,999 body objects verified; unchanged 1,524 notebook/135 book catalogs; native EPUB and repaired MP3 redownload exact from clone. Original production endpoint, marker and flags restored; tunnel stopped. Deployed dependency-aware readiness (PG/S3/configured BOOX database and bounded document query); required fresh race tests and public probes pass. Bounded two-hour observation is complete. See the qualification campaign for limits and private evidence.

2026-10-11: Native EPUB logical deletion/Recycle Bin restoration, cross-pagination bookmark, exhausted GET/cover retry and exhausted ink PUT/Library sync retry now pass. R renders on Palma; 20 paired resource hashes match. Reopen/close alone did not retry failed Reader uploads. A native Back-dismissed sync prompt stranded close; same-document reactivation preserved ink. Temporary proxy faults are removed with exact config restoration verified. See the PowerSync qualification campaign.

2026-10-11: Deployed explicit BOOX reading-data refresh UI with native revision fences, idempotent publication and separate server/device status. Corrected web highlight/bookmark lifecycle filters (Reader status 0 active, 1 removed). Required PG/S3 race tests and public form checks pass. Native codec probes on both devices validate removal of ONY1 duplicate IV; explicit audio repair endpoint is deployed. Synthetic MP3 native redownload recovers exact original bytes after controlled cache eviction; existing MP3 caches do not refresh on publication alone. Personal audio untouched. See the qualification campaign for evidence and limits.

2026-10-11: Qualification resumed: existing reader recovery API successfully refreshes cached-but-unapplied offline ink on both BOOX peers. Current-revision replay preserves resource bindings and reading position, advances native backend ink freshness, and triggers normal Sync prompts. Visible combined ink persists after reopen; 16 exact paired resource checks pass. Explicit assisted recovery is qualified, automatic convergence is not. [Campaign evidence](../../AragonitePowerSync/docs/qualification-campaign-20261010.md#resume-and-server-driven-stale-ink-recovery--2026-10-11). No runtime change/deployment in this recovery.

2026-10-10: [Qualification campaign](../../AragonitePowerSync/docs/qualification-campaign-20261010.md) now includes an isolated cold restore: all 38,969 BOOX body objects and 9,591 revision objects pass exact hashes; database and Gateway inventories survive restart. Backup now retains the application image; offline verification/extraction tool and synthetic safety tests added. Paired server-originated PDF download/rendering and native ink in both directions pass: exact shape/pressure bytes, L and P visibly retained, original PDF unchanged. Competing production fixture rename previews correctly block stale execution; retained-history title restoration is verified. Interrupted notebook assets recover through native Notes sync. A native ONY1 duplicate-IV decryption defect is reproduced on all six restored Palma audio files; no audio workaround deployed. Uploaded-PDF highlight deletion and server-history restoration are visibly verified on both devices. A real worker-subprocess kill/recovery integration test passes with a disposable DB/S3 and synthetic recognizer. Offline native reading positions converge; retained alternative restores visibly on both devices. Exact re-upload of a known removed book now reports that it does not restore deletion (tested and deployed). Additional real-storage failure tests reject phantom publication and corrupted bytes. Campaign paused at owner request: offline ink bytes reach both peers but visible rendering differs; readers closed and Wi-Fi enabled. Remaining campaign gates stay open.

2026-10-10: Local account password changes added to global Settings: current-password verification, confirmation, minimum 12 characters / maximum 72 UTF-8 bytes, same-origin protection and compare-and-swap updates. Old Basic credentials fail immediately; existing device/API tokens remain valid. Externally authenticated deployments hide and reject this workflow. Required fresh PG/S3 race tests cover successful rotation, rejected inputs, old/new credentials, unchanged token authentication and external-auth exclusion; vet passes. Deployed to the existing VM; public Settings renders the form, owner login succeeds, wrong-current-password submission is rejected without changing credentials, and readiness returns HTTP 200. Operator reset performed separately at the owner’s request; no secret recorded here.

2026-10-10: [Factory-reset Palma qualification](../../AragonitePowerSync/docs/fresh-palma-restore-20261010.md) in progress. Fresh signed-out enrollment and native connect pass after PowerSync 0.1.4 handles absent native system settings. All 1,080 Lumi active sync-enabled catalog entries match on Palma; one additional synthetic fixture is locally disabled on Lumi. Source EPUB redownload and native opening pass without cache seeding; transient no-cloud-file status resolves when initial Reader scanning reaches the book. Notebook asset catch-up and final integrity/rendering checks remain in progress.

2026-10-10: [Server-originated BOOX book upload](boox-book-upload.md) implemented and deployed: owner EPUB/PDF upload (64 MiB), native sampled fingerprints, immutable retained bytes, duplicate/collision guards and durable Reader metadata/commit/MESSAGE publication. Full required PG/S3 race suite passes; public synthetic EPUB publication and duplicate replay pass. Reading now shows live status-0 native books. Home-network paired EPUB qualification now passes: a fresh server upload appears in both native libraries, downloads on opening, and renders in NeoReader on Lumi and Palma with exact original bytes. Required sync-enabled/path metadata fixes are deployed. Native fileSyncStatus remains 2 even after rendering; do not treat it as download acknowledgement. PDF and annotation association remain unqualified.

2026-10-09: Native folder-view correction: device/source inspection confirmed reading-associated and rich-text notes were incorrectly mixed into the handwritten root. Folder browsing now defaults to handwritten notes, with explicit note-type selection and inclusive All notebooks; missing-parent records no longer appear as root children. Kind labels and compact column wrapping corrected. Deployed; fresh BOOX/runtime PG/S3 race tests and public desktop/mobile/dark checks pass. Handwritten root now matches the four native folders; inclusive view retains all eight entries. Source metadata unchanged. [Evidence and semantics](source-browser-and-boox-reader.md#native-folder-categories-2026-10-09).

2026-10-09: Recognition job management implemented: global owner-only Jobs view, source/status filtering, retained history, notebook/page-range batches, optional live updates, and pause/resume/cancel/retry controls. OCR runs separately from sync maintenance; permanent provider failures stop retrying and cancelled client/native results cannot publish. Fresh required PG/S3 full race suite and vet pass. Migration 0022 is deployed with refreshed runtime grants and a private pre-upgrade backup. Public desktop/mobile/dark Jobs and batch-review checks pass; no live recognition jobs were queued. [Contracts and evidence](recognition-jobs.md).

2026-10-09: Anthropic organization-scoped key diagnosis and fix deployed: optional workspace ID in Settings/DB/request snapshots, actionable workspace-header errors, and text-block selection that skips model thinking. Fresh required PG/S3 race tests, vet and live public settings/synthetic-error checks pass. Live Haiku 5.5 synthetic image recognition passed with the user-supplied workspace, saved through owner Settings with the existing key retained. No notebook pages submitted or queued by qualification. [Details](provider-settings.md#anthropic-workspace-authentication-and-response-blocks-2026-10-09).

2026-10-09: Connection-test usability fix deployed: a typed API key now takes precedence over the default keep-key action. OCR tests show sanitized HTTP status, authentication/path/quota hints and network/timeout distinctions. Raw upstream bodies are excluded from OCR errors. Fresh affected tests and public settings/health/reference checks pass. The original unsaved user attempt cannot be recovered; endpoint/model/error details requested. [Details](provider-settings.md#connection-test-follow-up-2026-10-09).

2026-10-09: Database-backed OCR/embedding Settings are deployed with migration 0021: encrypted API keys, owner-only same-origin forms, explicit test/save/reprocess actions, version-checked writes and per-job configuration snapshots. Semantic-index generations preserve the prior active index through rebuild/failure and exclude stale vectors. Legacy provider environment values seed once; the external encryption key and explicit deployment lock remain bootstrap configuration. Full required PG/S3 race suite, vet and public desktop/mobile/dark save/redaction/stale-form checks pass. Both providers remain off, synthetic credentials cleared, no recognition/index jobs queued. [Settings, backup and deployment contract](provider-settings.md).

2026-10-09: OCR compatibility fix: standard OpenAI Chat Completions requests omit `chat_template_kwargs`; vLLM suppression of thinking is explicitly enabled by `ALEXANDRIA_OCR_VLLM_DISABLE_THINKING=true` with format `openai`. Anthropic requests stay extension-free. Strict-request and configuration regression tests cover all three cases. Deployed to the personal VM; fresh required PostgreSQL/S3 BOOX/runtime race tests, vet and public authenticated smoke checks pass. The reference PNG remains exact and OCR is still unconfigured. No migration. [Configuration and upgrade behavior](deployment-compose.md#optional-features).

2026-10-09: Native BOOX decoding/rendering expansion and durable server-only OCR indexing are deployed with migration 0020. Full virtual-page catalogs fix the native 500-name metadata cap; native page models supply canvas dimensions beyond it. PDF backgrounds, common stationery, text/references and broader geometry render. Required PG/S3 race tests and public desktop/mobile checks pass, including pages 501/1791 and an unchanged qualified ink-reference hash. Recognition queues only explicit requests, invalidates stale text and feeds keyword search; no live jobs were created. An OCR provider is still needed for real handwriting qualification. [Implementation, audit, deployment evidence and remaining limits](boox-decoding-and-recognition.md).

2026-10-09: Follow-up listing/reader corrections add creation/modification columns and newest-first changeable sorting across source content lists, explicit counts/pagination, and an all-notebooks BOOX landing view. Older BOOX wrapped transform JSON now renders instead of refusing populated pages. [Evidence and date semantics](source-browser-and-boox-reader.md#listing-and-older-firmware-correction-2026-10-09).

2026-10-09: Source-oriented navigation and the first read-only native BOOX browser/preview are deployed on the personal VM. [Contracts, coverage and validation](source-browser-and-boox-reader.md) record the new global/source separation, snapshot rendering and explicit limitations. The server-only 57-shape reference preview matches the independent archive rendering byte-for-byte. Native pressure remains approximate; coverage and unsupported content are explicit.

2026-10-09: Production BOOX channel/session repair is deployed on the personal VM. Scoped subscriptions now update Gateway grants; stable frontend handles renew private backend sessions. Account validity and session inactivity use explicit 180-day policy, replacing accidental one-hour lab defaults. Migrations 0017/0018 preserve expired/revoked boundaries. Fresh required PG/S3/real-Gateway tests and full race suite pass. Both native accounts refreshed; seven missing Palma notebooks imported and an ink-bearing page rendered. Three sync-enabled deletions converged through bounded recovery; the fourth remains intentionally sync-disabled on Lumi. Template recovery exposed a separate live-publication timestamp defect, now fixed/deployed with migration 0019. Original template redownload and repeat-open rendering pass. [Detailed evidence and remaining limits](../../AragonitePowerSync/docs/production-sync-repair-20261009.md).

2026-10-09: BOOX enrollment codes shortened to eight cryptographically random, unambiguous alphanumerics. Redemption accepts lowercase/outer whitespace; existing long codes remain valid until normal expiry. Ten-minute lifetime, hashed storage and transactional single use are unchanged; allocation retries collisions. Fresh required PostgreSQL/S3 BOOX race tests cover lowercase redemption, replay, expiry and legacy compatibility. Change targets the current personal VM; no app update required.

2026-10-09: Identity-preserving BOOX enrollment is live on the personal VM. Migration 0016 transactionally binds the first original native UID; independent device credentials, ownership/channel checks and signed in-place PowerSync 0.1.2 are qualified. Lumi automatically uploaded 33,608 native resources (1,056,234,304 logical bytes); independent reads verify all 31,430 distinct S3 bodies. Paused native-account return and original-library guards pass. Settings storage now parses correctly and reports current logical usage; the 1 TiB allowance is fixed compatibility policy, not measured/enforced capacity. Cold backup image inventory handles removed one-shot image IDs. [Detailed coverage, exclusions and evidence](../../AragonitePowerSync/docs/existing-library-enrollment.md). Lumi remains connected; Palma unchanged. New-device/physical restore and exhaustive native reference closure remain unqualified.

2026-10-09: BOOX browser pages now render through Alexandria’s shared site layout and `/static/app.css`: library listing, enrollment and device management share navigation/active state, responsive typography, light/dark theme, tables, buttons and badges. CSP permits same-origin styles while retaining no-store enrollment handling and existing form-origin checks. Deployed to the new VM; authenticated public checks return 200 for all three pages and the shared stylesheet.

2026-10-09: First VM deployment is live at `https://alexandria.example.com`, configurable HTTP upstream `192.0.2.10:8080`. PostgreSQL/Couchbase use ext4; objects/backups use CephFS with mount-aware Docker startup. Public owner pages, synthetic enrollment/native sign-in/real Gateway session, authenticated BLIP HTTP 101 after cold restart, and revocation HTTP 401 pass. Real Gateway synthetic creation/removal produces two retained worker revisions. Coordinated physical backup checksum validation passes. Fresh setup found and fixed Gateway's absent-database 403 and the standalone metrics wrapper hiding WebSocket hijacking. [Proxy settings/evidence](deployment-nginx-proxy-manager.md), [storage/backup](deployment-storage.md). Real tablets remain restored to original configurations; first live-tablet deployment acceptance and physical restore qualification remain open.

2026-10-08: Native BOOX runtime implementation exists: migration 0015, device enrollment/account facade, restricted real Gateway proxy, signed retained assets, observed journals/projections, bounded authoring/recovery and browser setup. [Setup/capability matrix](boox-native-setup.md) and [product results](../../AragonitePowerSync/docs/product-implementation-results.md) distinguish tested foundation from remaining capabilities. Final signed product setup/offline return passes on both devices, including paired enrollment, independent native identities/Gateway sessions and original-library guards. No production rollout.

2026-10-08: Native BOOX integration implementation authorized. Canonical [design](boox-native-integration-design.md) records server/app contracts, bounded authoring, explicit recovery and retained observed history. Device restoration and lab teardown precede product work; no production rollout implied.


2026-10-08: [Dirty native Reader editor](../../AragonitePowerSync/docs/native-reader-dirty-editor-status.md) qualifies incoming T disable while Palma pen mode and pending D remain open. Correctly nested ink freshness plus mode-7/commit/MESSAGE triggers autonomous import and push of two D strokes; subsequent native close does not resurrect T in the bounded window. Fresh later restoration restores exact T geometry/info revisions; both devices finish with 23 shapes/15 exact pressures, D semantics and all library guards pass. No SDK pull or broadened push permission. D was flushed before close, so continuously unsaved-buffer and other editor modes remain separate.


2026-10-08: [Offline Reader alternatives archive](../../AragonitePowerSync/docs/native-reader-alternative-archive.md) qualifies full-body status retention for one owned book: 24 operations/five opposing-status shape IDs, original protobuf/ZIP bodies and all exact pressure dependencies retained. Independent missing/tampered pressure controls reject incomplete packets. Server snapshots/listing unchanged; no device actions or winner selection. Production retention/UI/restore and broader resource closure remain open.


2026-10-08: [Reader trigger factors](../../AragonitePowerSync/docs/native-reader-trigger-factors.md) qualify cached-resource rediscovery on both devices: no trigger, MESSAGE alone, unchanged-body commit alone and unchanged-body mode-7 alone yield no refill in 20 seconds; commit revision plus MESSAGE refills exactly with one target GET per peer. Every reset passes. Native 21-shape/14-pressure baselines, library guards, mode-7/commit bodies and existing asset documents stay exact. This is resource recovery, not first-time ink application.


2026-10-08: [BOOX sleep/display-doze delivery](../../AragonitePowerSync/docs/native-reader-sleep-delivery.md) qualifies pause and unaided catch-up: two source PUTs succeed, zero peer asset GETs during 120 seconds with twelve Dozing/DOZE samples. Wake resumes GETs after 2.6 seconds while keyguard is still showing; native import completes after about 8.1 seconds within the unlock sampling interval. No SDK pull/reopen/host publication. Both devices have 21 shapes/14 exact pressures; prior ink and library guards pass. Deep idle and exact wake-versus-unlock import attribution remain open.


2026-10-08 in progress: [BOOX sleep/display-doze delivery](../../AragonitePowerSync/docs/native-reader-sleep-delivery.md): T publishes with two HTTP 200 PUTs, but receiver makes zero asset GETs during 120 seconds; all twelve samples remain Dozing/DOZE. Fresh sessions and exact 19-shape/13-pressure preflight pass. Sleep-only evidence sealed. Receiver wake requested; user unlock and unaided catch-up verification pending. No full/deep-sleep claim.


2026-10-08: [Stopped Reader delivery](../../AragonitePowerSync/docs/native-reader-stopped-delivery.md) passes: receiver Reader remains absent at all twelve Home-window PID samples while ksync downloads S. Ordinary cold launch imports/renders S unaided in about 4.76 seconds. Both devices have 19 shapes/13 exact pressures; prior ink and libraries pass. No SDK pull, host publication or manual Sync. Screen-off and stopped-ksync remain distinct.


2026-10-08: [Home/background Reader delivery](../../AragonitePowerSync/docs/native-reader-background-delivery.md) passes: ksync downloads R while 10.3 II remains Home; Reader explicitly skips import because the document is closed. Ordinary PDF open imports/renders R unaided in about 1.35 seconds. Both devices have 18 shapes/12 exact pressures; prior per-device ink and library guards pass. No SDK pull or host publication. Stopped Reader and screen-off cases remain separate.


2026-10-08: [Actual Reader upload exhaustion](../../AragonitePowerSync/docs/native-reader-upload-retry-exhaustion.md): four native objects receive nine HTTP 503 PUTs each (36 total), then no successful upload after fault removal or bounded UI/requeue/timestamp/targeted job controls. Exact native bodies remain cached. Separate host-assisted resource publication restores storage; notification downloads resources but Reader skips ink on its timestamp gate. Guarded full native pull recovers Y/Z; both have 17 shape IDs/11 exact pressures, prior per-device ink and library guards pass. Autonomous exhausted-upload recovery and server-only freshness-gate repair remain open.


2026-10-08: [Reader expiry/publication hole](../../AragonitePowerSync/docs/native-reader-expiry-publication-hole.md): Y remains native-local on Palma (16 shapes/10 pressures); peer stays at 13/9. Initial expired-session 401, then verified fresh-session scoped start and native UI Sync still yield zero OSS requests in bounded windows. Prior ink and unrelated-library guards pass. Upload 503 harness passes tests but actual failed-upload retry is unqualified; fresh-edit control and retained-publication recovery remain open.


2026-10-08: [Sustained Reader disconnect](../../AragonitePowerSync/docs/native-reader-sustained-disconnect.md) passes: roughly three-minute Gateway outage, CBL 502 backoff through 128 seconds, native publication and foreground peer import recover unaided about 80 seconds after readiness. Thirteen shapes/nine exact pressure files, prior ink, replay and guards pass. No OSS upload occurred while disconnected; failed-upload retry remains separate. [Retry layers](../../AragonitePowerSync/docs/native-client-retry-policy.md) documented.


2026-10-08: [Brief Gateway interruption](../../AragonitePowerSync/docs/native-reader-server-interruption.md) passes autonomous native publication/application after source close while Gateway was down. Receiver imports about 26 seconds after readiness; ten shapes/eight exact pressure files, prior ink, replay and preservation pass. Actual close preceded restart by five seconds, so sustained failed-upload retry remains open. Evidence sealed; Gateway restored.


2026-10-08: [Clean first-close Reader delivery](../../AragonitePowerSync/docs/native-reader-clean-close-delivery.md) passes: native Palma Back/save publishes V and foreground 10.3 II imports/renders about 1.25 seconds later, before helper observation. Nine shapes/seven exact pressure files, prior ink, replay and preservation guards pass. No manual Sync, reopen, host publication or renewal in the observation window. Expiry, background delivery and interrupted publication remain open.


2026-10-08: [Reader config cache and delivery](../../AragonitePowerSync/docs/native-reader-config-cache-delivery.md): first Palma close detects changed U but silently stops at a stale global eligibility gate despite persisted STRING true. Journaled native config notifications plus source reopen/close publish retained U; foreground 10.3 II autonomously downloads/imports/renders before any helper. Eight shapes/four active, six exact pressure files, prior ink, replay and library guards pass. Clean first-close repetition remains next; cache readiness is an enrollment requirement.


2026-10-08: [Fresh per-book history replay](../../AragonitePowerSync/docs/native-reader-fresh-history.md): canonical seven-record snapshots converge to three active shapes with exact pressure; full historical feeds yield four or six active shapes. A controlled older L deletion arriving after a correct canonical baseline disables L on both devices, surviving full/replay with unchanged mode-7 values. Existing reader rows, original ink fixture and notebook guards pass (Palma confirmed user addition excluded only for guard comparison). Optional alternatives must be archived outside the live native listing. Hardware reboot passes both devices; unattended delivery and a truly fresh installation remain open.


2026-10-08: [Reverse-order status conflict](../../AragonitePowerSync/docs/native-reader-reverse-order-status-conflict.md) confirms delayed divergence: 10.3 II connects before Palma, all three native deltas are stored/cached, and both initially retain restoration. The 10.3 II has not imported the competing deletion; its initial full callback is result 0. A separately sealed unchanged-body mode-7/commit/MESSAGE trigger activates that deletion and recreates the split, surviving full/replay. A fresh journaled restoration revision then converges both through ordinary import; all seven IDs/five pressure files and preservation guards pass. Sessions were renewed offline with candidates exact; owned-book Sync Switch controls are recorded. Evidence sealed; devices Home, trace stopped/logging restored, proxy/facade live. Cold native-process/full-pull replay also preserves all seven shape records and five pressure files exactly on both devices; device reboot and fresh-device replay remain open.


2026-10-08: [Native status reconciliation](../../AragonitePowerSync/docs/native-reader-status-reconciliation.md) repairs the same-ID split with a journaled fresh restoration information revision, retaining all alternatives. Unchanged mode-7 ordinary pulls skip a downloaded revision; full pull applies it. Two clean pairs then qualify ordinary activation, including an identical-body new mode-7 revision plus reader-commit revision/MESSAGE: no commitId or timestamp value change required. Both devices converge to the fresh active-L revision; seven IDs, five pressure files, replay and preservation guards pass. Session-interrupted controls are recorded separately. Lab endpoints remain live; production repair policy and opposite reconnect order remain open.

2026-10-08: [Concurrent same-ID status divergence](../../AragonitePowerSync/docs/native-reader-concurrent-status-divergence.md) is confirmed: CBL mode-7 converges to Lumi restoration, but native L is active on Palma and disabled on Lumi after ordinary/full/replay imports. Seven IDs and five pressure files remain exact; preservation guards show no further drift. Unseen information revisions replace same-ID shapes; processed revisions are skipped. Native-compatible configuration resolution alone cannot guarantee ink convergence. Opposite-order fresh candidates and explicit repair policy remain open; trial sealed, devices at Home, proxy/facade live.



2026-10-08: [Native status restoration](../../AragonitePowerSync/docs/native-reader-status-restoration.md) passes erase/Undo after peer deletion: blue L is disabled on Palma, then restored under the same shape ID and unchanged pressure revision. Seven records return to three active/four disabled; all five pressure files remain exact. Source remained open and used native UI Sync; no SDK source push, host commit refresh, or cache injection. Ordinary replay adds nothing; guards show no further unrelated drift. Automatic first-attempt application and concurrent status ordering remain unqualified.




2026-10-08: [Reader commit/MESSAGE recovery](../../AragonitePowerSync/docs/native-reader-commit-trigger-recovery.md) isolates asset rediscovery: zero timestamps work; an identical-body new commit revision followed by native-shaped MESSAGE refills the exact ZIP on both devices. MESSAGE-only results vary; lower syncAt still dispatches on both. Closely overlapping doorbells also recover. Original second-trigger loss is narrowed but not reproduced. Ink, pressure, mode-7 and unrelated-library checks pass without further drift.


2026-10-08: [Known-fragment deletion](../../AragonitePowerSync/docs/native-reader-known-fragment-deletion.md) removes both surviving P fragments on both devices: seven records, three active/four disabled, five exact retained pressure files. Initial delivery stalled despite matching CBL configuration. A separately journaled commit updatedAt refresh plus scoped start recovered native ZIP download; full import applied deletion and replay added nothing. Both notebook guards pass; no further unrelated reader drift. Automatic delivery remains unqualified.




2026-10-08: [Competing erasures](../../AragonitePowerSync/docs/native-reader-competing-erasures.md) leave original P disabled but retain two partial-erase fragments on both devices, despite whole-erasure configuration winning. Ordinary pulls give matching geometry/status and five exact pressure files; full replay adds no ink but leaves disabled-P information-revision IDs divergent. Both notebook guards pass; Lumi reader guard has one earlier unrelated fileSyncStatus 0-to-1 drift, fully journaled without rebaselining. Automatic application remains unqualified.


2026-10-07 staged: competing erasures await user on both devices: erase P loop only on Palma (keep stem), erase whole P on Lumi; leave L/squiggle and Back to save each. Both show the merged disposable PDF; five baseline records and four pressure files match. Initial evidence is sealed in artifacts/gateway-lab/qualification/reader-native-erase-conflict. Replication proxy 18766 is intentionally stopped; asset/account facade stays available. Capture both independent candidates before reconnect/retry.


2026-10-07: [Concurrent native ink](../../AragonitePowerSync/docs/native-reader-concurrent-ink.md) preserves both independently written P/L strokes despite one mode-7 configuration winning. Ordinary incremental pulls merge both letters; five matching shape records and four exact pressure files, visible rendering and replay/personal-library guards pass. No host refresh or native source push retry; full recovery unnecessary. Automatic first-attempt application remains unqualified. Same-shape partial/full erase is next.


2026-10-07 staged: real concurrent ink experiment awaits user edits on both devices: P on Palma, L on Lumi, then Back to save each. Both open PowerSync-reader-clean-v2.pdf; exact ink/pressure and original-library guards pass. Initial baseline is sealed in artifacts/gateway-lab/qualification/reader-native-ink-conflict. Replication proxy 18766 is intentionally stopped, asset/account facade 18765 remains available. Capture passive local/server candidates before reconnect or retry.


2026-10-07: [Reader grants and clean conflicts](../../AragonitePowerSync/docs/native-reader-grants-and-clean-conflicts.md) pass real session replacement without manual reader grants, two clean reconnect orders, and native document width application on both devices. Twelve policy/account tests and Android build/lint pass. Native configuration can change with callback newData=false. Ink/pressure and original-library guards remain exact. Real concurrent ink edits are next; lab endpoints and the explicit reader-session-grants marker remain staged.


2026-10-07: [Native mode-7 divergence](../../AragonitePowerSync/docs/native-reader-config-divergence.md) verified offline sibling saves and eventual width-3 convergence on both devices/server, preserving exact ink exports. Initial reconnect was contaminated by a lost reader grant; journaled grant recovery yielded a native resolution revision. Fix reader grants across session renewal, then repeat opposite reconnect order and test UI application.


2026-10-07: [Partial erasure](~/AragonitePowerSync/docs/native-reader-partial-erasure.md) is closed: native upload/download succeeded; automatic import stalled. Scoped full recovery produced two matching active fragments plus the disabled original, with exact 7072/7952-byte pressure files. Replay adds nothing and original-library guards pass. No CBL tombstone or asset deletion was observed. Configuration divergence remains next; lab endpoints remain staged.


2026-10-07 handoff: fresh ink closure is [documented](../../AragonitePowerSync/docs/native-reader-fresh-ink-cache-timing.md) and checksummed.
Both readers now show the same one-shape fixture for a real partial-erasure test;
user stylus operation is pending. Erasure baseline is separate and must be captured
passively before recovery controls. Lab endpoints remain staged.

2026-10-07: [Fresh native ink and cache timing](../../AragonitePowerSync/docs/native-reader-fresh-ink-cache-timing.md)
qualifies native configuration/commit/MESSAGE publication and exact asset download
without host refresh. The old mode-7 conflict did not recur. Automatic application
saw no cached shape file before download task completion, then treated the update
as handled; later incremental retry remained empty. Scoped one-shot full-data recovery
imports one exact shape and 7952 pressure bytes, restores its flag, and replays without
new data. Original library guards and Android build/lint pass. Automatic end-to-end
application, deliberate mode-7 divergence, and erasure remain separate open cases.


2026-10-07: [Fresh reader lineage](../../AragonitePowerSync/docs/native-reader-fresh-lineage.md) is staged with
one eligible metadata row per device, zero shapes, and matching native configuration
identity/timestamp. New book/configuration publication reached fresh server revision
1 without host-authored documents. Original library guards and Android build/lint
pass. Fixture-only autoSync is enabled and both readers are open; a user pen stroke
is pending. The old accepted mode-7 history is linear, with no rejected branch;
public LiteCore error paths explain why 10409 alone cannot identify the cause.
Automatic ink delivery and native divergent-configuration recovery remain open.


2026-10-07: [Eight-shape reader recovery](../../AragonitePowerSync/docs/native-reader-eight-shape-recovery.md)
passes exact eight-shape/four-pressure-file closure and incremental replay; original
library guards pass. The passive trial failed before publication because Palma had
two fixture metadata rows. Controlled native retry uploaded assets and dispatched
MESSAGE, but mode-7 revision conflicts left the receiver configuration stale.
Assets downloaded after ordered controls; application only advanced after an explicit
fixture-only source-configuration server copy. Automatic delivery and native mode-7
conflict recovery remain open. No full pull or production resolver change was used.


2026-10-07: [Fresh reader ink and listing repair](../../AragonitePowerSync/docs/native-reader-message-fresh-ink.md)
passes controlled seven-shape closure and exact three-point-file equality with
incremental application and idempotent replay; no full pull was needed. Native
plain-ID reader commit publication is proven after correcting its admission rule.
Fresh dynamic assets were uploaded but omitted from OSS listing; that harness
fault is fixed and covered by a signed-listing test. An expired receiver session
confounded one MESSAGE-only phase. Nineteen policy/OSS tests and Android build/lint
pass. Original reader/notebook rows remain exact. Devices stay staged for a clean
fresh-edit repeat; fully automatic first-attempt delivery remains open.


2026-10-07: [Reader MESSAGE identity and wakeup](../../AragonitePowerSync/docs/native-reader-message-identity.md)
passes on both builds: prefixed MESSAGE IDs are pulled but do not dispatch;
plain UUID IDs dispatch READER_LIBRARY, and refreshing the same ID wakes it again.
This corrects server-authored MESSAGE controls, not reader data document IDs.
Six-shape native baselines and original reader/notebook preservation pass.
Both devices are staged reader-only for a fresh native pen edit; lab services run.
Fully automatic ink delivery remains pending.


2026-10-07: [Native reader ink write-back](../../AragonitePowerSync/docs/native-reader-ink-writeback.md)
passes an addition round trip: user-written “Hi.” on Palma imports five new shapes
on Lumi, displays there, and matches all six shape identities/bounds plus both
pressure files exactly. PDF bytes stay unchanged. A controlled type-4 trigger,
per-book eligibility and one-shot full-pull recovery were used; repeat pull adds
no data. Fully automatic first-attempt trigger delivery and ink erasure remain open.


2026-10-07: [Native reader shape ingestion](../../AragonitePowerSync/docs/native-reader-shape-ingestion.md)
passes on Palma: admitting reader commitType 4 and issuing scoped Binder start
fetches the exact shape ZIP; NeoReader imports one shape, with matching identity,
bounds and all 689 pressure points. Repeat application reports no new data.
The delivery trigger was a server-authored control; native source trigger emission
and peer editing/write-back remain open. Palma is staged on the fixture for the
requested real edit; Lumi remains on its original settings.


2026-10-07: [Native PDF ink follow-up](../../AragonitePowerSync/docs/native-reader-ink-export.md) proves real
stylus export, visible annotated-PDF transfer and exact native pressure-point
receipt on Palma. Mode-4/mode-7 and the separate shape ZIP reach our server.
Peer shape export remains empty: editable ink application is an open gate.
Session renewal resets harness reader grants; explicit fixture identity enrollment
is distinct from automatic matching. All original reader rows remain exact;
one new disabled blank Palma row is retained and documented.


2026-10-07: [PDF/reader-ink checkpoint](../../AragonitePowerSync/docs/native-reader-pdf-ink.md)
stages the exact authored four-page geometry PDF on both devices; ordinary-app
metadata reads and original-library preservation pass. The manifest-selected ink
provider rejects queries on both builds. Real stylus control is pending; PDF/ink
replication and coordinate application remain unqualified.

2026-10-07: [Native TLS rejection qualification](../../AragonitePowerSync/docs/native-tls-negative.md)
now passes on both firmware builds: HTTPS account login and independently WSS
NOTE_TREE reject a matching-IP self-signed certificate. HTTP account controls
pass. Missing trust-anchor rejection is qualified; hostname mismatch remains open.

2026-10-07: [Editor switching and freshness qualification](../../AragonitePowerSync/docs/native-editor-switch.md)
passes visible ordinary-app handoff and exact edited-text peer application in both
directions. Android blocks background same/different opens on both builds.
Both local stale recoveries refuse before native body writes; a stale server
commit PUT gets 409 and preserves the current commit. These are bounded guards,
not an editor lock, complete queue drain or unattended recovery qualification.


2026-10-07: [Ordinary-app editor quit qualification](../../AragonitePowerSync/docs/native-editor-close.md)
proves package-targeted QUIT_NOTE delivery on both devices, but neither rich-text
editor closes: Notes reports no event subscribers. This exported broadcast is
not a qualified flush/recovery barrier. Cold/save/publication checks are separate.
A native toolbar Back positive control closes Lumi and propagates exact edited
text to Palma; both cold active bodies match. This is an ADB UI control, not an
ordinary-app flush. The delayed probe builds/lints and upgrades in place with
matching signers.


2026-10-07: [Coordinated pair recovery and open-editor control](../../AragonitePowerSync/docs/native-pair-recovery-coordination.md)
now pass sequential recovery, simultaneous sync/cold reopens and native-edit
propagation. An open edited Lumi buffer subsequently publishes older text after
a newer server recovery exists; both cold devices select that edit. Native-winner policy
stands. Production editor/pending-work coordination remains open; narrower older
pair failures below are historical observations, not the current coordinated result.


2026-10-07: [Palma exact receipt and isolated recovery](../../AragonitePowerSync/docs/native-palma-commit-receipt.md)
now pass parent-history proofs, four exact asset GETs and cold native text
application twice. Both builds support isolated server-created text; reliable
pair convergence and pending-work coordination remain open.

2026-10-07: [Server-created BOOX text recovery](../../AragonitePowerSync/docs/native-server-richtext-recovery.md)
works through fresh graph download/cold rendering on Lumi; pair convergence remains
open because Palma stays older after renewed transport. Local probe/proxy experiments
only; no production Server implementation or deployment.

2026-10-07: [Explicit BOOX text recovery](../../AragonitePowerSync/docs/native-richtext-recovery.md)
now passes via an ordinary helper app and stock native publication to both devices,
including cold reopens, stale-request refusal and hydrated receipt replay. This is
a text-only local prototype; unattended/server-only recovery remains unqualified.

2026-10-07 policy decision: follow native ksync/CBL winners for live BOOX sync;
retain the option for later server-side history/recovery/suggestions without
silently changing device state. Archive coverage must identify what was actually
observed; complete losing-branch recovery is not promised or a v1 prerequisite.
See [recorded policy](native-boox-sync-source.md#conflict-policy-decision-native-compatibility-with-optional-server-history).

2026-10-06: the user identified native BOOX Couchbase synchronization as a planned
notebook and reading source. [Recorded integration scope](native-boox-sync-source.md)
links PowerSync device/protocol evidence and qualification gates. This is local
planning/lab work; the historical cloud deployment sequence remains separately scoped.
PowerSync now has scoped native EPUB metadata/bookmark and bidirectional progress
application evidence, with receiver confirmation, plus reverse native bookmarks.
An offline A/B/A test selects between surviving UUID aliases by creation time;
Native EPUB highlights and annotation/bookmark logical deletion now apply across
devices; repeated highlight publication/cold reopen does not resurrect it.
Native signed EPUB upload/download now passes with exact bytes and no receiver
local copy. Fresh cloud-book import on Lumi now retains the cloud UUID and
restores progress plus a rendered bookmark through explicit ordinary-app native
requests. Automatic import triggers, other receiver firmware, general remap/
deduplication, other reading assets and anchor translation remain gates.

Matched PowerSync scope comparison corrects the earlier fresh-book delivery
inference: Lumi's supposed book-scope receipt contained cached library models.
Palma and Lumi behave identically under the same sequence; READER_LIBRARY plus
native restart delivers fresh revisions, while book-scoped changes retain old
local models. See [scope receipts](../../AragonitePowerSync/docs/native-reading-scope-comparison.md).

Context split: 2026-09-27 UTC. Repository `~/AragoniteAlexandriaServer`,
branch `integration/alexandria-foundation`; implementation HEAD `83e6b0e`.
Worktree was clean before these documentation additions.

Forward checklist: [remaining steps](remaining-steps.md), including standalone
setup, the paused sync port and coordinated release gates.

## What exists and what does not

This is the cloud-native successor to UltraBridge, not feature parity with it.
The foundation includes PostgreSQL/pgvector, S3-compatible objects, leased durable
jobs, probes/metrics, authenticated task/job APIs, explicit schema management,
restricted runtime bootstrap and the public `library` runtime used by Hosting.

VM Compose, Helm and personal-cloud Terraform profiles exist. Standalone VM and
personal cloud require neither Hosting nor subscriptions. Management prepares
schema/credentials/storage; normal runtime checks compatibility instead of running
DDL or creating buckets. Preserve immutable migration history and credential
separation when extending setup/upgrade automation.

**Alexandria/Rhizome enrollment, row sync, assets and whole-library replacement
are not yet ported into this cloud runtime.** Existing client/SQLite UltraBridge
tests are not cloud compatibility proof. The user explicitly paused that
porting/deployment step before requesting this context migration.

## Read next

- [Project map](project-map.md): ownership and legacy sources.
- [ADR 0004](adr/0004-runtime-and-hosting-boundary.md): accepted runtime/Hosting
  boundary and the larger implementation sequence.
- [Architecture](architecture.md), [development](development.md),
  [deployment/identity upgrades](deployment-identity-upgrade.md), and
  [UltraBridge migration safety](ultrabridge-migration.md).
- [Hosting cloud qualification](../../AragoniteAlexandriaHosting/docs/cloud-deployment-qualification.md):
  newer live evidence superseding early “not deployed” wording in historical docs.

## Evidence and deployment distinctions

Hosting's September 27 run verified real public sandbox signup/provisioning,
two isolated library databases, task API isolation, scoped S3 worker jobs and
restart recovery on the reused AWS host. Its deployed build uses the Server
module pin in **Hosting's** `go.mod` (`45eb49115071` at this checkpoint), not an
assumption that this checkout's HEAD was deployed. Later local Server commits
cover standalone setup/manifests/docs; their presence is not a live personal-cloud
acceptance result.

Deployment workstations and Hosting infrastructure details are operator records,
kept in the Hosting operator's private ops directory rather than in this public
repository. Recheck any remote checkout's branch/worktree before use.
Hosting's reused-host stack is separate from this repository's personal-cloud
Terraform profile. Never apply one state as though it owned the other deployment.
No remote change is part of this context split.

## Test entry points

```sh
GOWORK=off go build ./cmd/alexandria-server
GOWORK=off go test -race -count=1 ./...
GOWORK=off go vet ./...
```

The integration gate additionally needs `ALEXANDRIA_TEST_DATABASE_URL`,
`ALEXANDRIA_TEST_S3_ENDPOINT`, and `ALEXANDRIA_REQUIRE_INTEGRATION=1`; follow
`.github/workflows/ci.yml` for disposable PostgreSQL/SeaweedFS fixtures. Never
use a live library as a test DB or claim skipped tests as passing integration.
See the deployment guide before Compose/setup commands; preserve existing volume
identities and keep secrets outside source/command arguments.

## Next action

Start by reviewing this checkpoint with the user; keep cloud sync porting/deployment
paused. Other open work includes browser-first standalone setup/deployment wrappers,
independent VM/personal-cloud qualification and remaining security/upgrade gates.
Do not silently expand context migration into any of those implementations.

Suggested opening prompt: “Read AGENTS.md, docs/current-state.md and the project
map. Summarize the Server foundation and remaining standalone work. Keep the
Alexandria/Rhizome cloud port and deployments paused.”

2026-10-06: PowerSync [paired offline application qualification](../../AragonitePowerSync/docs/native-reading-workflow.md)
shows exact new library userdata receipt, cached bookmark/highlight provider
application while the replication proxy is disconnected, and duplicate-free
reconnect on both devices. Lumi initially returned RESULT_NONE and required a
retry; keep receipt and application acknowledgements separate. This run does
not establish general conflicts, new progress changes, or rendered highlights.
Mode 7 is reader-note configuration rather than progress.

2026-10-07: [PowerSync offline reading conflicts](../../AragonitePowerSync/docs/native-reading-conflicts.md)
show that ordinary native logical deletions can lose to an older enabled edit,
while an older deletion can also beat a newer edit. Four sibling-branch winners
match revision-ID ordering, consistent with CBL default tie-breaking rather than
application updatedAt. Do not use a divergent newest-updatedAt PostgreSQL policy
or promise delete-wins. Native application/provider/UI receipts remain separate;
losing-intent preservation needs a mechanism beyond accepted-server changes feeds.

2026-10-07: [PowerSync native HTTPS/WSS](../../AragonitePowerSync/docs/native-tls-tunnel.md)
now qualifies paired login, replication and exact signed text-notebook transfer
with visible receiver application. Both devices restored; temporary tunnel and
lab stopped. Invalid certificate/hostname controls remain open. This BOOX
integration is single-owner; per-device credentials/revocation remain required.

2026-10-07: [PowerSync pending-work restart](../../AragonitePowerSync/docs/native-server-restart.md)
verifies exact stored assets/session survival, but the clean retry window and
explicit recovery do not preserve the interrupted text as active editor content.
Archive retention is separate from live application; complete recovery remains
a gate. Devices restored, original-library guards passed, lab stopped.
