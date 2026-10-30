# Using the Web App

The web app is where you browse your library, search it, manage tasks and
events, run handwriting recognition and change settings. It runs in any modern
browser, desktop or phone, at your server's address. This guide is a tour of
the screens so you know where things live; the feature guides go deeper.

![The Library overview](../images/overview.png)

## What You Need First

- A running server (see [Install Alexandria Server](install.md)) and its
  address, for example `https://alexandria.example.com` or
  `http://192.0.2.10:8080` on your home network.
- The username and password set during installation.

## Signing In

Open the server's address. Your browser asks for a username and password;
enter your Alexandria account. The browser remembers them for the session, so
you won't be asked on every page.

To change your password, go to **Settings → Account → Change Password**. Use
at least 12 characters (up to 72 bytes). Your browser may ask you to sign in
again; enrolled devices and API tokens keep working.

## Finding Your Way Around

### Top Bar

From left to right:

| Link | Goes to |
|---|---|
| **Today** | Your tasks and the next seven days of events (also the home page). See [Tasks and Calendar](tasks-and-calendar.md). |
| **Library** | Your notebooks, books and tags, one source at a time. |
| **Recognition** | Handwriting recognition jobs, with a count when pages need attention. See [Recognize Handwriting](recognition.md). |
| **Settings** | Account, search, AI models and integrations. |

The search box searches notes, books and highlights (see [Search Your Library](search-and-tags.md)). The moon and sun switch between the dark and light themes, and **?** opens the built-in guide.

## The Library

**Library** opens on the source you looked at last. Each source keeps its own notebooks and devices because each syncs in its own way; search, tasks and recognition work across all of them. When BOOX is set up, the switch beside the tabs moves between **BOOX** and **Alexandria Client**:

| Source | Tabs |
|---|---|
| BOOX | **Notebooks**, **Books**, **Tags** |
| Alexandria Client | **Notebooks**, **Books** |

The **More** menu holds each source's devices, settings and maintenance pages, **Recognize this library…**, the **Library overview** (counts per source, last sync, and whether recognition and search by meaning are on) and **+ Add source**.

## Browsing Notebooks

Notebooks are listed on the left, newest first; sort by name, pages, or created or modified date, and flip the order. **All notebooks** lists every notebook with its folder; **Folders** goes one folder at a time. Dates are in your time zone (**Settings → Tasks & Integrations**). BOOX lists also have a title search and a **Type** filter (handwritten, text, reading and other note types).

Click a notebook to preview it on the right: its first eight pages, its folder and how many pages have recognized text. Double-click it, or choose **Open**, to read it.

- **BOOX** notebooks can be renamed, moved, sent to the tablets' Trash, restored or removed permanently from the preview. See [Connect BOOX Tablets](boox.md#the-notebook-preview).
- **Alexandria Client** notebooks offer **Open**, **Download PDF** and **Delete…**. Deleting removes the notebook from every Client device on its next sync. There's no trash, and a later edit on a device brings it back.

### Reading a Notebook

Open a notebook to see one page at a time:

- **← Previous** and **Next →** move between pages; type a number into
  **Page** and select **Go** to jump.
- **Actual size** shows the page at full resolution; **Fit page** fits it to
  the window.
- Alexandria Client notebooks also have **All pages**, a grid of every page
  with a **Text** or **No text yet** badge, and **Download PDF**.

Beside the page, a side panel shows cards for that page. Which ones appear
depends on the source and what the page has:

| Card | Shows |
|---|---|
| **Page Text** (Client) | The page's recognized text and the model that produced it, with **Recognize this page…** or **Recognize again…**. |
| **Handwriting Recognition** (BOOX) | The page's recognition state and text, and **Recognize this page**. |
| **Note Actions** (BOOX) | Colored ink found on the page and the tasks or events made from it, with **Find actions**. See [Note Actions](note-actions.md). |
| **BOOX Tags** | Tags on this page and elsewhere in the notebook. |
| **Recordings on This Page** | Audio recorded on the page, playable in the browser. |
| **Audio Note Minutes** | The note's minutes, or **Summarize this note…**. See [Audio Note Minutes](minutes.md). |

BOOX previews are drawn on the server from the ink files received so far.
They're read-only: nothing you do while browsing changes your tablets.

### Alexandria Client Books and Devices

- **Books** lists books the Client has imported, with author, format,
  annotation count and a **Download** link. Open a book to read its
  annotations.
- **Devices** lists Client devices with **Last Sync**, **Waiting** (changes
  the device hasn't pulled yet) and **State**. Under **Manage** you can rename
  a device, **Clear Sync Position** (safe for a device still in use), or
  **Revoke Device** (it stops syncing; its notes stay in the library). To add
  a device, open Settings → Sync in the Alexandria Client app and sign in with
  this server's address and your account.

BOOX tablets are covered in [Connect BOOX Tablets](boox.md).

## Getting Help in the App

- **?** buttons beside headings open a short explanation. Each has a **Learn
  more** link into the guide.
- **Help** (top right, and in the footer) opens the full in-app guide, with
  sections from **Getting Started** to **Operator Notes**.
- The footer shows the running version, handy when reporting a problem.

## Settings

**Settings** has four tabs:

| Tab | What's there |
|---|---|
| **Account** | **Change Password**, **Language**, and **About This Server** (the address and version). |
| **Search** | **Search by Meaning**: status, index building and coverage. See [Search Your Library](search-and-tags.md). |
| **AI Tools and Recognition** | Recognition provider, Audio Note Minutes models, the embedding model, **Automatic Processing**, **Note Actions** and reprocessing. See [Set Up AI Models](ai-models.md). |
| **Tasks & Integrations** | CalDAV addresses, collection names, time zone, due-date handling, and API tokens and MCP access. See [Tasks and Calendar](tasks-and-calendar.md) and [Connect an AI Assistant](assistants.md). |

Each source also has its own **Settings** tab: the BOOX one covers the device
connection, library maintenance and diagnostics.

## Choosing a Language

1. Go to **Settings → Account**.
2. Under **Language**, pick one from **Show Alexandria in**, or **Your
   browser's language** to follow your browser.
3. Select **Save language**.

The choice is saved in this browser only, so set it on each browser you use.
Text that Alexandria writes onto your tablets, such as headings in minutes
notes, is set separately under **Settings → AI Tools and Recognition**. Want
Alexandria in your language? See [Translate Alexandria](translating.md).

## Pages That Update Themselves

Some pages refresh their figures in place while work is running, so you can
leave them open:

- **Recognition** (every few seconds while jobs are queued or running).
- A whole-library recognition run in progress.
- **Settings → Search** while the index builds (every 15 seconds).

Without JavaScript, these pages reload themselves instead.

## On a Phone

The layout adapts to small screens: the sources sidebar becomes a row at the
top, the top bar scrolls sideways, and the notebook side panel moves below the
page. Wide tables scroll horizontally. The app follows your system's light or
dark setting.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| The browser keeps asking for a password | Wrong username or password | Use the account set at installation. To reset the password, from `deploy/compose` run `printf '%s' 'new-password-here' \| docker compose --profile setup run --rm --no-deps -T seed-admin` (at least 12 characters). |
| No **BOOX** in the sidebar | The BOOX source isn't enabled on this server | See [Connect BOOX Tablets](boox.md). |
| "Nothing here yet. Notebooks appear after a device syncs." | No Client device has synced | Connect one from **Devices** in the Alexandria Client source. |
| A BOOX page says a preview isn't available | Some of its files haven't arrived yet | Give the tablet time to sync; a missing preview doesn't mean the page is blank. |
| The language didn't change on another computer | Language is saved per browser | Set it in **Settings → Account** there too. |
| Counts on the Recognition page look frozen | JavaScript is blocked and the page is waiting to reload | Reload the page, or allow scripts for the site. |
