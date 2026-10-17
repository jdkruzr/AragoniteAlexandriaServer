# Search Your Library

Search looks through everything Alexandria can read in your library at once:
handwriting that has been recognized, Alexandria Client pages, BOOX reading
highlights and notes, Audio Note Minutes, and notebook titles. Out of the box it
matches the words you type. Connect an embedding model and it can also find
pages by meaning, so a search for "watering schedule" turns up the page where
you wrote "irrigate the beds every other morning". BOOX tags you add on the
tablet help too: a tagged page turns up for its tag even when the handwriting
never says the word.

![Search results with BOOX tags under each page](../images/search.png)

## What You Need First

- Some content: a connected Alexandria Client device or a BOOX tablet running
  PowerSync (see [Connect BOOX Tablets](boox.md)).
- For handwriting: pages that have been through recognition. A page that was
  never recognized can only be found by its notebook title. See
  [Recognize Handwriting](recognition.md).
- Optional, for search by meaning: an embedding endpoint, set up in
  [Set Up AI Models](ai-models.md) (or [Run Models on Your Own Hardware](local-models.md)).

## What Gets Searched

| Content | Found by | Notes |
|---|---|---|
| Alexandria Client page text | Words, and meaning when it's on | The text Alexandria has for each page, including recognized handwriting. |
| Alexandria Client book annotations | Exact words | Listed under **Alexandria Client Book Annotations**. |
| BOOX handwriting | Words, and meaning when it's on | Only pages that have been recognized. |
| BOOX tags | Words | Only on pages that have recognized text (see below). |
| BOOX reading highlights and notes | Words, and meaning when it's on | From NeoReader. |
| Audio Note Minutes | Words, and meaning when it's on | The minutes and their recording transcripts. See [Audio Note Minutes](minutes.md). |
| BOOX notebook and book titles | Title text | Listed under **BOOX Notebook Titles**; matches part of a title, ignoring case. |

Nothing is searchable on a BOOX page until it has recognized text. If a search
misses a page you know is there, check its recognition state first.

## Searching

1. Select **Search** in the top bar (or use the search box on the **Library**
   page).
2. Type up to 256 characters into **Search across your library**.
3. Optional: pick a source from the menu: **All sources**, **Alexandria Client**
   or **BOOX** (BOOX appears when the BOOX source is enabled).
4. Optional, when search by meaning is on: choose **Words and meaning** (the
   default) or **Exact words only**.
5. Select **Search**.

Results come in up to three groups:

- **Pages and Reading**: pages, reading records and minutes, best match first,
  up to 50. Each result shows its source and kind, a link to the page (for
  example "Garden plans, page 3"), a short snippet, and any BOOX tags on the
  page. Select a tag to see every page that carries it.
- **BOOX Notebook Titles**: notebooks and books whose title contains your text.
- **Alexandria Client Book Annotations**: highlights and notes in Client books.
  This group isn't shown when you narrow the search to BOOX.

### Words vs. Meaning

| Mode | What it does |
|---|---|
| **Exact words only** | Matches the words you typed, as whole words. "garden" doesn't find "gardens", so try variations. |
| **Words and meaning** | Does the word match, then adds pages whose meaning is close even if the wording differs. Pages that match your words still rank higher. |

The mode menu only appears once search by meaning is on. Notebook titles and
Client book annotations always use exact words. If the embedding service is
unreachable when you search, you quietly get word results instead of an error.

## Turning On Search by Meaning

Search by meaning needs an index: every searchable page's text is sent to the
embedding model once, and the results are stored on your server.

1. Set the embedding endpoint and model under **Settings → AI Tools and
   Recognition**, in the **Search by Meaning** section. The details are in
   [Set Up AI Models](ai-models.md).
2. Go to **Settings → Search**. The **Status** panel shows three separate
   things:
   - **Provider**: whether the saved embedding service answers.
   - **Semantic Search**: whether you turned the feature on.
   - **Index**: **Not built**, **Building**, **Updating**, **Ready**, or
     **Needs attention**.
3. Select **Test connection** if you want to check the endpoint first.
4. Select **Enable and build index**.

While it builds, the page updates itself every 15 seconds with progress, pace
and an estimated finish time. Word search keeps working the whole time, and
search by meaning starts once the first build finishes.

> **Cost:** building the index sends your existing searchable text to the
> embedding provider. A local Ollama server costs nothing but time; a hosted
> embedding API may charge per token. After that, only new or changed text is
> sent.

### Index Buttons

| Button | What it does |
|---|---|
| **Enable and build index** | Turns the feature on and builds the first index. |
| **Build index** / **Build replacement index** | Builds a new index, for example after changing the embedding model. The old one stays in use until the new one is ready. |
| **Turn off** | Pauses embedding work and keeps the index. Word search is unaffected. |
| **Turn on and resume** | Turns it back on and continues with the index you already have. |
| **Retry failed index work** | Shown when some records failed; queues them again. |
| **Test connection** | Checks the saved embedding endpoint. |

**Coverage by Source** shows, per source, how much text is available, how much
is embedded, and how much is pending or failed. BOOX pages without recognized
text are never indexed; the page says how many there are and links to
recognizing them. Search by meaning never starts handwriting recognition on its
own.

## BOOX Tags

BOOX Notes lets you tag a piece of a page. Alexandria picks those tags up and
uses them in search, on notebook pages, and in [Audio Note Minutes](minutes.md).

### Adding a Tag on the Tablet

1. In a BOOX notebook, select ink with the lasso.
2. Choose **Tag** from the lasso's actions. The tablet suggests text from its
   own handwriting recognition; edit it if you like and confirm.
3. Close the note. Tags reach Alexandria when the note is saved on the
   device, and PowerSync syncs it.

Tags you create in the tablet's tag manager without putting them on a page
also arrive; they show as "Not on any page".

### Tags in the Web App

- **BOOX → Tags** lists every tag with how many pages and notebooks carry it.
  Select a tag to see the pages that have it (matching ignores case, like the
  tag search on the tablet). **Search for it** runs a library search for the
  tag's text.
- A BOOX notebook page shows a **BOOX Tags** card: the tags **On this page**,
  and the notebook's other tags under **Elsewhere in this notebook**.
- Search results show each page's tags as links.

### How Tags Affect Search

- A tag works like a strong keyword: searching for it finds the tagged page
  even if the handwriting never mentions it. It counts for about as much as
  saying the word a couple of times, so it doesn't push a page above others
  that discuss the topic at length.
- A tag only helps once the page has recognized text. An unrecognized tagged
  page still appears on the tag's own page under **BOOX → Tags**, but not in
  search results.
- Adding or removing a tag doesn't re-send the page to the embedding model.

### Read-Only

Alexandria only reads tags. It never creates, renames or removes tags on your
tablets. To change a tag, do it on the device; the change arrives with the
next sync.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| A BOOX page you know is there doesn't show up | The page hasn't been recognized; only its notebook title is searchable | Open the notebook, check the **Handwriting Recognition** card, and select **Recognize this page** or recognize the library. See [Recognize Handwriting](recognition.md). |
| A tagged page doesn't appear when searching for its tag | The page has no recognized text yet | Recognize the page. It already appears under **BOOX → Tags**. |
| A new tag hasn't arrived | The note is still open on the tablet, or hasn't synced | Close the note on the device and let PowerSync sync. |
| No **Words and meaning** menu | Search by meaning is off | Turn it on in **Settings → Search**. |
| Index stuck on **Not built**, buttons greyed out | The provider isn't ready | Check the endpoint in **Settings → AI Tools and Recognition**, then **Test connection** in **Settings → Search**. |
| "The existing index was built with different provider settings" | You changed the embedding model or endpoint | Select **Build replacement index**. The old index keeps working meanwhile. |
| **Index** shows **Needs attention** | Some records failed to embed | Select **Retry failed index work**; if failures persist, check the embedding server's logs. |
| Searching "gardens" misses "garden" | Word matching is exact | Try the other form, or turn on search by meaning. |
| "Invalid search query or source." | The query is longer than 256 characters, or the BOOX source isn't enabled | Shorten the query; pick **All sources**. |
