# Note minutes

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
the handwriting. Nothing here writes a Talk Insights file or touches
Couchbase or the device.

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

`minutes.PromptVersion` (`minutes-v1`) is part of `input_hash`. The system prompt
explains the two inputs: the notes are the author's emphasis, the recordings the
fuller record. It asks for strict JSON in this shape:

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

## Not yet

- Alexandria Client notebooks have no audio.
- The recognition jobs list doesn't show minutes; progress is on the minutes
  page.
- Recordings aren't mapped to pages, and speakers aren't separated.
