# Server-created BOOX text notes

2026-10-13. The server can create a **new** native text notebook
(`internal/boox/text_note.go`). It never edits an existing notebook. Audio Note
Minutes will use this path to publish `<title>_Minutes` beside the source note.
The owner-only test form is `/boox/text-notes/new`, linked from BOOX settings.

**Device status:** unqualified until the paired experiment below is recorded.
Server publication is not the same as device delivery or rendering.

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

## Device experiment (pending)

1. Use the test form to create a synthetic note in a real folder.
2. **Lumi, then Palma:** check that it appears in the right folder with its
   title (the importer may add a numeric suffix), and opens with the text after
   a cold Notes start.
3. Compare each device's next upload against the server's copy.
4. If the note is listed but empty, try the Notes cloud sync button. That's the
   known recovery when a new notebook's files haven't arrived.
5. Then remove one optional file at a time (thumbnail, template) to find the
   minimum set.
