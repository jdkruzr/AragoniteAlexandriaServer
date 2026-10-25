# Server-created BOOX text notes

> **Looking for how to use this?** See the guide [Audio Note Minutes](guide/minutes.md). This page is an engineering note for people working on Alexandria.

2026-10-13. The server can create a **new** native text notebook
(`internal/boox/text_note.go`). It never edits an existing notebook; resending minutes may retire its own earlier, untouched copy. Audio Note
Minutes uses this path for **Send to BOOX** ([details](note-minutes.md#send-to-boox)).
The owner-only test form is `/boox/text-notes/new`, linked from BOOX settings.

**Device status (2026-10-13):** qualified on Lumi (Go 10.3) and Palma 2 Pro
for headings and paragraphs; see the experiment below. Bullet lists and bold
lead-ins have not yet been seen on a device.

## Reference file set

The layout below was decoded from a live, empty text notebook created on the
Go 10.3. All six live text notebooks in production have the same file set.
Keys are under `<uid>/note/<doc>/`.

| Key | Contents |
| --- | --- |
| `resource/data/<res>.html` | Editor JSON (not HTML), `docVersion` 2: `doc` → `heading`, `paragraph`, `bulletList`/`listItem`; `bold` marks |
| `resource/pb/<resRev>#<ts>` | `ResourceDocProto` list: 1 id, 2 doc, 3/4 times, 7 type 3, 8 `/<res>.html`, 11 resRev, 12 device path |
| `shape/<rich>#<shapeRev>#<ts>.zip` | One deflated entry named like the key, holding the shape (type 29). Field 14 references the resource; 16 is the point revision, 18 the shape revision |
| `point/<rich>#<pointRev>#points` | 160 bytes: version, page, point revision, one empty block and an index entry for the shape |
| `virtual/doc/pb/<doc>` | Field 1 is its **own** 32-hex ID, not the notebook ID |
| `virtual/page/pb/<uuidv7>` | The handwriting page: rectangles, `geo_layout`, template path |
| `pageModel/pb/<uuidv7>` | Both pages, each with one layer |
| `template/json/<scribble>.template_json` | Blank layout template |
| `../<doc>.png` | Thumbnail. The device renders one; since 2026-10-25 the server writes none (eight files) |

The notebook record has `activeScene` 1, `status` 1, and `parentUniqueId` for
the folder. It lists the blank handwriting page in `pageNameList` and
`notePageInfo`, and the text page in `richTextPageNameList`.

Compared with that device notebook, the generator's output is:
- byte-identical for the point file;
- structurally identical, apart from timestamps, for every protobuf file;
- the same set of record fields.

## Publication

`QueueTextNote` does three things, all fenced on the library generation and
the native owner:
1. Stores the bodies.
2. Binds every key in `boox_blob_live`.
3. Queues one idempotent `create_text_note` operation, keyed by the caller's
   request ID.

`PublishOne` then re-checks the bindings, writes the record if it's absent,
then writes the commit and the type-6 NOTE_TREE message, exactly as other note
operations do. A missing or deleted folder is rejected.

## Device experiment, 2026-10-13

The owner submitted "Oh hey there" (notebook root) from the test form at 22:29
CDT. Operation `f6a7c30f…` reached `published` in 1.8 s.

- **Delivery:** both tablets' native provider listed `00e22405-…` with the
  title, root parent, `activeScene` 1, `status` 1, `type` 1. No sync button was
  needed. Eight of the nine files are byte-identical (sha256) to the server's
  on both devices. Neither device stores the server's blank thumbnail as a file;
  they show an empty tile until they draw their own.
- **Rendering:** after force-stopping the Notes editor process, the note opened
  on Lumi and on Palma with the title as heading 1, a paragraph, a second
  heading and three paragraphs. The owner typed `* ` bullets, which the form then
  read as paragraphs, so lists are still unverified. The form now accepts `- `,
  `* ` and `• `.
- **Opening is a write.** Each device saved a fresh `pageModel/pb/<uuidv7>`
  when the note was opened (Lumi 22:55:56, Palma 22:56:34). Each is identical to
  the server's apart from timestamps. The record went from version 1 (server)
  to 2 (Lumi) to 3 (Palma) without any edits. So "untouched on the device" can't
  mean "record version unchanged". For Audio Note Minutes resends, compare the
  live `resource/data/<res>.html` body with the one the server published.
- **Minimum file set:** not pursued. The full nine-file set works on both
  devices without the sync button, and removing files would only add risk.
  2026-10-25: the thumbnail was dropped after all (see below); the other eight
  files are unchanged.

## Device saves and the current text (2026-10-25)

Opening a note and moving the cursor is enough for a device to save it, and a
save writes more than the page model ("20261023 Singh Lab_Minutes", opened on a
Tab 8 and then a Go 10.3):

- a **new** `resource/data/<uuid>.html`, identical apart from `currentPos`;
- a `resource/pb/<list>#<ts>` **delta**: the previous editor file restated with
  field 13 `status` = 1 (`ResourceStatus.REMOVED`; 0 is `ENABLED`) and the new
  one added. A later save doesn't repeat resources it didn't touch;
- a new info revision of the same type-29 text shape (field 18), pointing at the
  new file, and a device thumbnail.

The old editor files stay in storage. The current set is what the Notes app
rebuilds: replay **every** list, newest record per resource ID wins, drop
removed ones (`booxpage.CurrentResources`; `currentRichText` in
`internal/boox/richtext_current.go`). Alexandria used to treat every live editor
file as current, so this note showed its text three times. Superseded files are
ignored everywhere derived (notebook view, search text, inventory, resend check)
and never deleted. Notes whose lists don't describe typed text keep every editor
file; an ambiguous history shows the newest save alone, with a warning.

**Resend check.** A cursor-only save is not an edit. `minutesNoteUntouched`
compares what the owner can see: the editor `content` subtree (text and
formatting; `currentPos`, `editor` and scroll state are ignored), the page lists,
the template and `virtual/` files, and the shapes (only the one text shape may
exist; any other shape or new point file is ink or an image). Anything unclear
counts as an edit.

**Thumbnail.** Devices never stored the server's thumbnail (above), so the
server no longer writes one; a sent note shows an empty tile until it is first
opened on a device, which then draws its own.

## Search

2026-10-14: typed text in any BOOX text notebook (device-made or server-made)
becomes searchable when its handwriting page is recognized, for example by a
library run. `booxpage.RichText` reads the editor JSON; the text is stored on
the first handwriting page without a model call. See
[current state](current-state.md).

## Images (2026-10-17; display changed 2026-10-25)

- **Where the file is.** An editor image node (`onyxImg`) has `src` (a device
  path, or a `data:` URI when pasted) and `dataKey` (its cloud name). The file
  is `resource/data/<base name>` in the same note, which is what the device
  itself rebuilds (`ParseRichTextImageAction`). This was checked against the
  one real image (2026-10-17): `federation.webp`, a plain (not ONY1) WebP
  (`VP8X`), 553 × 420, matching the node's `width`/`height`.
- **Display (2026-10-25).** Typed text is no longer drawn into an image. The
  notebook view shows it as HTML generated by `booxpage.RichHTML` from the
  parsed blocks (fixed tags, escaped text, generated sizes and colours; the
  resource itself is never passed through). An image is served by
  `/boox/resource` only if a current document refers to it (by `dataKey`, then
  `src` base name) and it sniffs as PNG, JPEG, GIF or WebP. Pasted `data:`
  images show a placeholder: the page's CSP does not allow `data:` images.
- Earlier (2026-10-17 to 2026-10-25) `ComposeRichText` drew typed text and its
  images into the page preview with a substitute font.
- Page notices on the notebook view are now translated (`td`).
