# Audio Note Minutes

2026-10-13. On request, Alexandria transcribes a BOOX note's recordings and
writes minutes from the transcripts **together with the note's recognized
handwriting**:
- the topic
- 4–6 keywords
- up to five key points, each marked as from the recording, the notes or both
- a conclusion
- metadata on the notebook and its recordings

The firmware's Talk Insights (`meeting`/`minutes` in the decompiled note app)
sends only the speech transcript to BOOX's cloud, gets back HTML, and never uses
the handwriting. Nothing here writes a Talk Insights file or changes the
source notebook. Minutes reach a device only when you choose **Send to BOOX**
(below).

## Native format (verified 2026-10-13 against a real note)

- **Recording body:** `<uid>/note/<doc>/resource/data/<uuid>.mp3`, wrapped in
  ONY1 (AES-CBC). `audioPlaintext` decrypts both the original layout and a
  published audio repair, which lacks the duplicated IV block.
  - Firmware records 16 kHz mono and encodes 32 kbps MP3 (MPEG-2 Layer III).
  - `.pcm` resources are wrapped as 16 kHz mono 16-bit WAV.
- **Recording metadata:** `resource/pb/<resourceId>#<ts>` is a
  `ResourceDocProto` list. `booxpage.ParseResources` decodes it with protowire;
  unknown fields are skipped. Fields:
  - 1 uniqueId, 2 documentId
  - 3 createdAt (recording start, ms)
  - 4 updatedAt, about the end
  - 5 title (`Recording1`), 6 audioDuration (ms), 7 type (2 = audio)
  - 8 relativePath, 11 revisionId, 12 originPath
- **Page chip:** shape type 23 (`SHAPE_AUDIO`).
  - Its `resource` JSON names the relative path, title and resource ID.
  - Its own `audioDuration` was 0 in the sample, so length comes from the
    resource record.
  - Pages render a neutral speaker marker, without the title, so recognition
    doesn't read it. These pages no longer carry a coverage warning, which had
    blocked recognition.
  - Talk Insights' own meeting shape is type 200 and isn't handled.

## Flow

1. **Review** (`GET /boox/minutes/new?id=`) lists the recordings (title, start,
   length, size) and page coverage.
   - Pages without text can be included; they go to the recognition provider
     first, with the usual charge warning.
   - The owner confirms what is sent and to which models.
2. **Request** (`POST /boox/minutes/new`) is fenced on the notebook's preview
   version and idempotent on its request UUID. In one transaction it:
   - queues the missing pages into one recognition batch
     (`recognition.QueuePagesTx`);
   - upserts one `alexandria_transcription` row per recording;
   - resets the notebook's `alexandria_note_minutes` row.
3. **Worker:** `ProcessPages` gains a fifth turn, `boox.ProcessMinutes`.
   - It transcribes one due recording, re-checking its body hash first.
   - Otherwise it leases due minutes. If any page is still being recognized it
     defers 20 s without counting as progress.
   - Then it summarizes (`minutes.Summarize`) and stores the result.
   - Failures use `ocr.Failure`, with backoff and five attempts, and never
     store provider bodies.
4. **View** (`GET /boox/minutes?id=`) shows the minutes, their metadata, and
   each recording with an audio player (`/boox/minutes/audio`, owner-only,
   decrypted, Range-capable) and a timestamped transcript.
   - Minutes are marked out of date when recordings or recognized text no
     longer match `input_hash`. Nothing regenerates automatically.
   - Cancel and retry are on the page.

Transcripts are cached per recording body and transcription settings identity,
so an update re-transcribes only new or changed recordings.

## Summary contract

The system prompt has two parts (`minutes.System`):

- **Instructions**, editable by the owner in Settings → Recognition → Audio Note
  Minutes → Summary instructions (`SummaryConfig.Instructions`, provider
  settings JSON, no migration). Blank means `minutes.DefaultInstructions`, which
  explains the two inputs (the notes are the author's emphasis, the recordings
  the fuller record) and the style of each field. Saving the default text
  unchanged stores blank, so the installation keeps following the built-in text.
  Capped at 16,000 characters.
- **Response format** (`minutes.ResponseFormat`), always appended and shown
  read-only in Settings, because `Parse` depends on it.

`input_hash` covers `minutes.PromptVersion` (`minutes-v2`) and a hash of the
full system prompt, so changing the instructions marks existing minutes out of
date; nothing regenerates until the owner summarizes again. The format asks for
strict JSON in this shape:

```
{"topic":…, "keywords":[4–6], "key_points":[{"text":…, "from":"recording|notes|both"} ×1–5], "conclusion":…}
```

Text before the first `{`, after the last `}`, and any inline `<think>` block is
ignored. One repair retry restates the format and accepts a near miss leniently
(trims extras, accepts fewer keywords).

Prompts are capped at 400k characters. Notes take at most half; transcripts
share the rest and are cut with a marker.

## Search and MCP

Ready minutes of live notebooks are indexed as `alexandria_note_content` source
`boox-minutes` (kind **Minutes**). The title is notebook plus topic, keywords
carry weight B, and the body holds points, conclusion and transcripts. They're
embedded with everything else, and the result links to the minutes page.

MCP `get_note_minutes` takes a `boox-minutes:<id>` path or notebook ID and
returns the structured minutes, metadata and transcripts
(`include_transcripts=false` omits them). Deleted notebooks return nothing.

## Send to BOOX

The minutes page's **On your BOOX** card creates a new native text note named
`<notebook title>_Minutes` in the source notebook's folder, through the
qualified [text note](boox-text-notes.md) path. The wording is
`minutesNoteTemplate` in `internal/boox/minutes_send.go`: topic and keywords in
bold lead-ins, key points as bullets with their source, the conclusion, and an
"About these minutes" section. The operation payload records `minutesOf`.

Sending again follows "native wins":
- **Untouched** (title and folder unchanged, and every live file under the note
  except `pageModel/` byte-identical to what was published): the new note is
  queued, then a `set_note_status` retirement of the old one, fenced on the
  record revision seen. If a device changes the old note first, the Gateway
  revision check blocks the retirement and both notes stay. Opening a note
  rewrites only its page model, so opening doesn't count as a change.
- **Changed** (edited text, handwriting on the blank page, renamed or moved):
  a new note is added and the old one is left alone.
- **Deleted on a device:** a new note is added.

While a send is still publishing, the card hides the button.

**Automatic sending (2026-10-14):** Settings → Recognition → Audio Note
Minutes → "When minutes are ready" (`SummaryConfig.SendToBOOX`, stored in the
provider settings JSON, no migration) makes the worker run the same send as
soon as minutes become ready. The send's request ID is derived from the minutes
request, so a replay never duplicates it. If the send fails (folder gone,
BOOX not enrolled), the minutes stay ready and the failure shows on the minutes
page; Send to BOOX retries by hand. Turning it on doesn't send minutes that
already exist. The Summarize review page says when automatic sending is on.


- Alexandria Client notebooks have no audio.
- The recognition jobs list doesn't show minutes; progress is on the minutes
  page.
- Recordings aren't mapped to pages, and speakers aren't separated.

## Automatic processing (2026-10-15)

Settings → Recognition → **Automatic Processing** (off by default, saved by
`POST /settings/automatic`, stored as `Config.Automatic` with the time it was
turned on). The worker's minutes turn calls `boox.Service.AutoProcess` first:

- **Recognize new BOOX pages automatically:** a live, unskipped notebook whose
  record or files changed after `Since` and that has been quiet for five minutes
  gets its never-recognized pages queued (`recognition.QueuePagesTx`, one batch).
- **Summarize notes with new recordings automatically:** a notebook with an audio
  file that arrived after `Since`, quiet for ten minutes, gets `RequestMinutes`
  (recognizing missing pages first) when it has no minutes or its minutes don't
  include every current recording. Minutes out of date only from handwriting,
  queued or running minutes, and failed minutes are left for the owner.

Client pages need no option: sync already queues each changed page. The minutes
page's "On Your BOOX" section now says a sent note is an ordinary notebook that
syncs on its own; resending is "Replace It on Your BOOX" and never summarizes again.

