# Native BOOX library file operations — 2026-10-26

What the device's Library app writes when a user reorganizes books and shelves.
Server-side book and shelf operations (WebDAV and the Reading page) must write the
same thing. This was traced read-only in decompiled sources: launcher `com.onyx`
56184 (Library UI), `com.onyx.kreader` 38456 (reader sync) and Go 6 II ksync
4.2.2-rel_0924. The launcher's firmware build is not recorded. Paths below are
relative to `com/onyx/android/sdk` in the launcher unless marked otherwise.

## Sync model

- Each sync pushes every local `Library`, `Metadata` and `MetadataCollection` row
  whose `updatedAt` falls inside its push window. ksync saves them as upserts.
- `updatedAt`, copied into the document, is the only dirty marker. Locally deleted
  rows never sync, and no `syncStatus` or `extraAttributes` change is involved.
- Every operation below is a soft change, usually `status=1` plus a new `updatedAt`.
  The only real Couchbase deletes (tombstones) come from permanently deleting a
  book in the recycle bin.
- Identities:
  - A book's `uniqueId` is `Metadata.uuid`.
  - A shelf's `uniqueId` is `Library.idString`, a random UUID with the dashes removed.
  - A membership's `uniqueId` is `MetadataCollection.uuid`, generated the same way.
  - Memberships hold the book's file path locally and the book's `uuid` once pushed
    (kreader `PushLibraryTreeRequest`).

## Operations

| User action | Documents written |
|---|---|
| Rename book (`MetadataRenameRequest`) | Renames the file on disk. The book's `name` and `nativeAbsolutePath` change to the new name and path; `title`, `uniqueId` and memberships do not. |
| Delete book, "remove from library" with or without the file (`SelectedMetadataDeleteFromMultipleLibraryRequest`, `BooksRemoveAction`) | Book gets `status=1`. Only the membership for the shelf being viewed gets `status=1`; deleting from All Books touches none. Annotations and bookmarks are not changed. |
| Permanently delete from the recycle bin (`LibraryRecycleBinDeleteAction`) | Book documents are deleted in Couchbase (ksync `DeleteReaderDocListAction`), and per-book reader data is removed. Not verified for memberships or annotations. |
| Remove a book from a shelf (`LibraryMoveToRequest`) | Membership gets `status=1`. Moving a book to the top level sets `libraryUniqueId=null` and `status=1` on the same membership. |
| Add a book to a shelf from All Books (`MetadataCollection.create`) | New membership with a random 32-hex `uniqueId` and `status=0`. |
| Move a book from shelf A to shelf B (`RemoteDataProvider.moveMetadataCollection`) | The same membership document is updated in place: `libraryUniqueId=B`, `status=0`. It is not a removal plus an addition. |
| Create shelf (`LibraryCreateAction`, `LibraryBuildRequest`) | New shelf with a random `uniqueId`, `name`, and `parentUniqueId` set to the current shelf (null at the top level). Refused if a sibling already has that name. |
| Rename shelf (`LibraryBuildRequest`) | `name` changes. |
| Move shelf (`LibraryMoveToLibraryRequest`) | `parentUniqueId` changes. On a sibling name clash the shelf is renamed `name-N`. Child shelves and memberships are untouched. |
| Delete shelf, the non-recursive mode the UI uses (`LibraryDeleteRequest`) | Shelf gets `status=1`. Its memberships move to the parent shelf, or get `status=1` when the shelf was at the top level. Empty child shelves get `status=1`; non-empty ones move up to the parent and are renamed `name-N` on a clash. Books are not changed. |

## One shelf per book

A book has at most one live membership. On every pull, kreader's
`MergeMetadataCollectionHandler` ("handle conflict, ensure last write win") groups
memberships by book, keeps the one with the newest `updatedAt` and sets every other
one to `status=1`, then pushes those removals (`ensureDocumentCollectionUnique`,
`handleDocCondition`; current kreader and Go 10.3 Lumi builds alike). The Library
UI only ever moves a book; it has no "add to another shelf".

Observed on the Note Mini C (4.3) on 2026-10-27: after the server gave a stored book
a second membership, the Mini C removed the original within 8 s. Another enrolled
tablet then re-wrote that removal with a newer `updatedAt`, and on the next pull it
won over the new membership too, leaving the book on no shelf. The server therefore
never creates a second live membership: WebDAV COPY is refused, uploads of known
bytes into another shelf are refused, and book imports block such a file.

## Adopting cloud-account books

Tablets create their own records for books in a cloud account, such as a Nextcloud
WebDAV account (`fetchSource` = provider + 10, so 19 for WebDAV), when they download
a file or rebuild the library:
- **Each tablet creates its own record**, with a random uuid (`SaveMetadataForNetDiskFileAction`).
- **The records sync between tablets** through Couchbase.
- **The account finds its records again by path only** (`DataManagerHelper.insertOrUpdateMetadata`), and revives a removed record it finds at that path.
- **None of the observed records has a `uniqueCloudId`.**

Highlights and bookmarks link to a book only by its uuid (`documentId`); progress,
reading status and reader settings (`extraAttributes`) live in the record itself.

An upload whose fingerprint matches only cloud records therefore takes them over
instead of adding a second book. The rules:
- **Candidates must agree:** every candidate must have the same size and the same cloud file (`cloudId`, or the NetDisk path). Any native match is still refused, because a tablet's dedupe deletes an untouched local copy and its file when a synced copy with the same fingerprint arrives (`MetadataDeduplicationHandler`).
- **A new book:** the upload is published as a **new** stored book, inheriting the title and authors, and the `progress`, `readingStatus`, `lastAccess` and `extraAttributes` of the most recently read candidate that has reading data.
- **Highlights and bookmarks:** every candidate's live ones are re-pointed to the new uuid, each with a reader commit.
- **Old records:** every live candidate is retired (`status=1`). The new book joins the shelf it was uploaded into.
- **The original file stays in the cloud service.**

**Converting a record in place does not work** (tried and withdrawn on 2026-10-27). The cloud record was turned into a stored one with `fetchSource` 0 and a `.ksync` path, keeping its uuid. The Note Mini C kept its local record, never created a `READER_BOOK` (type 20) download record for it (`SaveReaderBookRecordRequest` runs only for books new to the tablet), matched it as "book file missing" and set `fileSyncStatus=3` (`NO_CLOUD_FILE`). The Library hides that state (`SQLOperatorUtils`: `fileSyncStatus != 3`), so the books disappeared despite intact data. Neither a rename round trip nor the Library's full sync changed it. Records left in that state are kept out of the file view, and the next upload of the same book replaces them like a cloud record, releasing their stored binding.

**On the Note Mini C** (deployed `adopt-b-6354a54738be`, three Nextcloud books copied
through pcmanfm onto their `eBooks/<Author>` shelves):

| Book | Candidates | Mini C result |
|---|---|---|
| *A Forbidden Alchemy* | one live record with 3 highlights | New book at 1/326; the 3 highlights appeared after accepting the reader's sync prompt; the download matched the upload's SHA-256 |
| *The Bone Shard Daughter* | data only on a removed record (667/668), plus an in-place-converted record | New book at 667/668; the converted record stayed retired and left the file view |
| *The Bone Shard Emperor* | live record at 204/657 with 2 bookmarks | New book opened at 205/661 on chapter 17; both bookmarks arrived after accepting the sync prompt |

**Opening the file from the cloud account brings the old record back.** Tapping
*The Bone Shard Daughter* in the Mini C's Cloud Storage → `jtd` view downloaded it and
revived the removed record at that NetDisk path (`status` 1 → 0, progress kept, rejoined
the `jtd` cloud shelf), and the tablet pushed that as a new revision. The stored book was
untouched, so the tablet then listed the book twice: the stored copy on its author shelf,
with the highlights, and the cloud copy on the `jtd` shelf, whose progress now moves on its
own. The server does not re-retire such a record: the reader has just opened it, and the
account would revive it again on its next visit. Removing the cloud copy on the tablet, or
simply reading from the stored copy, is enough.

## Server implementation

`internal/boox/book_management.go` queues each change as revision-fenced
`library_create` / `library_update` journal operations. A change to a record that
still has an unpublished change is fenced on the exact revision that change
publishes, so consecutive edits made before the projection catches up chain
safely, and a device edit in between blocks the chain. A marker is not enough:
devices keep unknown fields, `alexandriaOperationId` included, when they rewrite
a record (observed on the Note Mini C on 2026-10-27). Library changes publish a READER_LIBRARY message only, like book
imports; they carry no reader commit. `library_fs.go` and `library_dav.go`
present the result as the WebDAV folder described in the
[BOOX guide](guide/boox.md#manage-books-from-a-file-manager-webdav).

Deliberate differences from the device:

- Trashing a book sets only `status=1`. The device also clears `encryptionType`
  and `digest`, which describe the local file it deletes; the server keeps the file.
- Deleting a shelf removes, rather than duplicates, a membership whose book is
  already on the parent shelf, since devices would remove one of the two anyway.
- Shelf create, rename and move refuse a sibling name clash instead of renaming;
  child shelves that move up during a shelf delete are renamed `name-N`, as on the device.

## Device qualification — Note Mini C, 2026-10-27

Deployed `file-view-dbddd91ae883`. Every step went through a real WebDAV client
(gvfs, the backend pcmanfm uses) against the public server, in the synthetic shelf
*Alexandria WebDAV check 20261026* with two generated books (a 631-byte PDF and a
1,485-byte EPUB). The Mini C's library rows were read after each step through
kreader's `content://com.onyx.content.database.ContentProvider` (`Library`,
`Metadata`, `MetadataCollection`). All 17 server operations published; none blocked.
Changes reached the Mini C 15–120 s after publication.

| Step | Mini C result |
|---|---|
| Create a top-level shelf | Same `uniqueId`, `parentUniqueId` NULL |
| Upload a PDF and an EPUB into it | Both books with their names, titles and sizes; one membership each, with the server's IDs |
| Create a sub-shelf, move the PDF into it | Sub-shelf has the right parent; the membership was updated in place |
| Upload the EPUB's bytes into the sub-shelf (first build) | The device kept one membership per book and removed both: see [One shelf per book](#one-shelf-per-book). Fixed before the remaining steps. |
| Copy a book to another shelf (fixed build) | Refused with 403; gvfs's FUSE layer reports success for a refused `cp`, while gio reports "Forbidden" |
| Rename the EPUB | `name` changed; `title`, storage path and record count unchanged. The Library lists the new name; the book downloaded (SHA-256 matching the upload) and opened in NeoReader. |
| Move it onto the shelf, delete it there | Book `status=1`; restoring by moving it out of `.Trash` gave `status=0` back on that shelf, reusing its earlier membership |
| Delete the non-empty sub-shelf | gvfs refuses to delete a non-empty folder itself, so pcmanfm-style recursive delete trashed the PDF first and then removed the empty shelf |

The device stores a server book under `.ksync/<uid>/reader/<id>/book/<id>.<ext>`, so
a rename never needs a file on the device to move. Renaming a device-originated book,
whose file lives at a user path such as `/Books/…`, was not tested.

While a book was open in NeoReader, these library-only changes to its record made
the reader offer "New document data has been detected. Do you want to sync it?"
Accepting with "Sync font size, spacing, and contrast settings" checked also
applied the synced layout settings.

## Open questions

- Renaming a book that a device imported from its own storage (a user path rather
  than `.ksync`) has not been tested.
- Whether newer launcher builds (for example Go 6 II kcb 57629) behave the same.
  Only their resources were decompiled; the one-shelf merge rule was confirmed in
  both the current kreader and the Go 10.3 Lumi build.
- `updateMetadataCollectionRemovedByDocId` sets `status=1` without bumping
  `updatedAt`, so that change would not sync. Its callers were not traced.
