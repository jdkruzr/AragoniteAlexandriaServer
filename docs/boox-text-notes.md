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
| `../<doc>.png` | Thumbnail. The device renders one; the server writes a blank one |

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

## Search

2026-10-14: typed text in any BOOX text notebook (device-made or server-made)
becomes searchable when its handwriting page is recognized, for example by a
library run. `booxpage.RichText` reads the editor JSON; the text is stored on
the first handwriting page without a model call. See
[current state](current-state.md).

## Images (2026-10-17)

- **Where the file is.** An editor image node (`onyxImg`) has `src` (a device
  path, or a `data:` URI when pasted) and `dataKey` (its cloud name). The file
  is `resource/data/<base name>` in the same note, which is what the device
  itself rebuilds (`ParseRichTextImageAction`). This was checked against the
  one real image (2026-10-17): `federation.webp`, a plain (not ONY1) WebP
  (`VP8X`), 553 × 420, matching the node's `width`/`height`.
- **Drawing.**
  - `ComposeRichText` (`internal/booxpage/richdoc.go`) resolves `dataKey`,
    then `src`, or decodes a `data:` URI.
  - It decodes PNG, JPEG or WebP (`x/image/webp`), with the 20-million-pixel
    guard, and draws the image across the text width, keeping its
    proportions, at most 24 lines tall.
  - An image that hasn't arrived or can't be read keeps the grey placeholder,
    plus the notice "Some images in typed text haven't arrived or can't be
    read; placeholders mark them."
- Page notices on the notebook view are now translated (`td`).
