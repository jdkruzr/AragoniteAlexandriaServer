# NeoReader handwriting on books

NeoReader keeps the ink you write on a book beside the book, not inside it. Its
"embed and export" refuses any file under the sync folder
(`NeoPdfReaderPlugin.supportNoteEmbeded` → `!KReaderPathCommonUtils.isLocalKSyncBookPath`),
which covers every book delivered through library sync, uploads included. The server
draws that ink onto a copy of the stored PDF instead: `GET /boox/books/annotated.pdf?book=<uniqueId>`
(`internal/boox/reader_notes.go`, `annotated_pdf.go`).

Verified 2026-10-31 against a live book (6-page Chromium PDF, 784 strokes on 5 pages).

## Files

Under `<uid>/reader/<book>/` in native sync (`.ksync/document/<book>/` and `.ksync/point/<book>/` on the tablet):

| Key | Contents |
|---|---|
| `book/<book>.pdf` | The PDF the ink was drawn on |
| `shape/<pageKey>#<docRev>#<ms>.zip` | One entry: a `ShapeInfoProtoList` in **NeoReader's** numbering (`ReaderShapeDocProto`), not the Notes one |
| `point/<pageKey>#<revisionId>#points` | Notes' V1 point format; one file per drawing session |

The book's modeType 7 reading record carries `readerNotePageNameMap`, a JSON string
`{"data":{"<pageKey>":["<subPage>", …]}}`. The first sub-page is the ink on the page itself; later
ones are side notes. Archives whose page key isn't listed are stale and skipped.

## Shape fields used

`booxpage.ReaderShapes` decodes by field number: 1 uniqueId, 3/4 created/updated (ms),
5 subPageUniqueId, 9 colour (ARGB as a sign-extended varint), 12 thickness (float, page units),
15 shapeType, 25 revisionId (names the point file), 27 status (absent = live), 33/31 page position
JSON `{"pageReferenceId":"pdf-<obj>-<gen>"}`, 34 page key. Newest `updatedAt` per uniqueId wins.

## Pages and coordinates

- A page is named by its PDF object reference, `pdf-<objNum>-<gen>` (fallback `pdf-invalid-<index>`).
  The page key is the Java `String.hashCode` of `{"pageReferenceId":"<that>"}`. Both depend on the PDF's
  object numbers, so ink only maps onto the exact stored bytes; a re-saved PDF leaves strokes unplaced.
- Points are PDF units in the page as displayed: crop box, top-left origin, y down, no matrix. The
  overlay is a form XObject whose `/Matrix` flips that into PDF space. `/Rotate` and offset crop boxes
  follow from that mapping but haven't been seen in live data yet.

## Drawing

Pen types (`scribbleTypes`) are drawn as round-capped polylines, split where the pressure width
(the notebook renderer's model) changes. The marker (15) blends with `/BM /Darken`, as the firmware
does, so text under it stays dark. The page's own content is wrapped in `q … Q` and kept; the page
gets its own copy of its resources so pages sharing a dictionary aren't changed. Side notes,
typed text and shapes aren't drawn.

One export runs at a time (`exportSlots`); others wait up to a minute.
