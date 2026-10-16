# BOOX Tags

2026-10-16. How BOOX tags are stored on the device, how they reach Alexandria,
and what Alexandria does with them. Alexandria only reads tags; it never
creates, edits or removes them.

## On the Device

Firmware facts are from the decompiled Notes app in `~/booxreverse`.

- One entity, `TagModel` (DBFlow `ShapeDatabase`, table `TagModel`): `uniqueId`,
  `documentId` (the notebook), `pageUniqueId`, `userId`, `createdAt`,
  `updatedAt` (epoch ms), `content` (the tag text), `status`, `revisionId`.
- `status`: 0 enabled, 1 force-removed (deleted in the tag manager, which
  disables every row with that text), 2 removed.
- One row is one tag on one page. Using the same text elsewhere adds another row
  with the same `content`. Notebooks have no tag field; a notebook's tags are
  the rows with its `documentId`.
- **Lasso → Tag** (`CreateTagAction`) suggests text from handwriting
  recognition, creates the row with the page of the first selected shape, and
  adds the tag's ID to every selected shape's `tagIdList` (shape proto field 19,
  a JSON array string; SQLite column `NewShapeModel.tagId`).
- **Tag manager** tags (Settings) have no notebook or page ("common tags").
- Each notebook also keeps an append-only `<doc>/tag/pb/<revisionId>` log
  (doc PB type 4, `TagList`).
- **Sync**: ksync writes each tag as its own Couchbase document in
  `{uid}-NOTE_TREE`, document ID = tag `uniqueId`, marked by `modeType` 1 and
  no `type` (notebooks and folders have `type`).
- Firmware meeting minutes keep their keywords inside the encrypted
  `minutes-*.rec` file, not as tags. KReader's `Tag`/`TagAnnotation` tables are a
  separate, apparently device-only, system.

## In Alexandria

- Tag documents arrive through the ordinary NOTE_TREE journal and are kept in
  `boox_projection` (`domain='notebook'`, `native_type='1/<nil>'`, now
  `supported`).
- Migration 0030 adds the view `boox_tag`: live tags only (status 0, non-blank
  text), with `tag_id`, `notebook_id`, `page_id`, `content`, `created_at`.
- **Search**: a recognized page's tags fill `alexandria_note_content.keywords`
  (weight B, between the title and the text), kept current by a trigger on tag
  changes. One tag counts like about two mentions in the text, so a tag finds a
  page whose handwriting never says it, but doesn't push it above pages that
  discuss the term at length (live: the page tagged "mulch" ranks 13th of 49).
  Tags are not part of the page text, so tagging never re-embeds a page. A tag only makes a page findable once the page has recognized text.
  Search results (web and MCP `search_notes`) list each page's tags.
- **Web**: BOOX → **Tags** (`/boox/tags`) lists every tag with its page and notebook counts
  (common and orphaned tags show "Not on any page"); `/boox/tags?tag=…` lists
  the live pages that carry it (matched without regard to case, like the device's
  tag search). The notebook view shows the page's tags and the notebook's
  others.
- **Audio Note Minutes**: the summary prompt lists the notebook's tags and up
  to 50 other tags, asking the model to reuse a tag's spelling for a keyword
  that means the same. The notebook's own tags are part of the input hash, so
  adding or removing one marks its minutes out of date (notes without tags hash
  as before). Other tags are only a hint and don't. On the minutes page,
  keywords that are tags are filled chips linking to the tag; others are
  outlined.
- Tags reach the server when the note is saved (closed) on the device. Tagging
  rewrites the tagged shapes, so the page is re-recognized after each tagged save.
- Keyword matching is exact (case-insensitive) on purpose: in the first live run
  the model kept "Seed Savers" (a review channel) beside the tag NAS, which the
  owner judged correct. Tags also act as a spelling list for names: with a
  Greenline tag, the next run corrected the recognized "Greenlime" throughout and
  reused four of the note's tags as keywords.
- Shape `tagIdList` is still not kept (it would let the viewer highlight the
  tagged ink).

## Not Yet

Creating tags from minutes keywords would mean publishing `modeType` 1
NOTE_TREE documents (and maybe the `tag/pb` log and shape `tagIdList` edits).
That needs qualification on Lumi and Palma first, and Alexandria never edits an
existing notebook's pages.
