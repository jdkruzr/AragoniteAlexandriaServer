# Connect an AI Assistant (MCP)

MCP (Model Context Protocol) is a standard way for AI assistants to use
outside tools. Alexandria Server includes an MCP server, so an assistant such
as Claude can search your notes, read and look at pages from your BOOX and the
Alexandria Client, read your books' highlights and your minutes, edit
Alexandria Client text boxes, and manage your tasks and calendar, all on your
say-so. "What did I
write about the irrigation budget last month?" becomes a question the
assistant can actually answer.

## What You Need First

- Your server's MCP address: **Settings → API & MCP** shows it as **MCP
  address**, for example
  `https://alexandria.example.com/mcp`.
- An assistant that supports remote MCP servers over HTTP.
- For Claude on the web (and the Claude apps): your server must be reachable
  from the internet over HTTPS, because Claude connects from Anthropic's
  servers, not from your computer. See
  [Put Alexandria Behind HTTPS](https-proxy.md). An assistant running on your
  own machine, such as Claude Code, only needs to reach the server from there.

## Two Ways to Sign In

| Method | Use it for | How it works |
|---|---|---|
| Approve in the browser (OAuth) | Claude on the web and other clients that support MCP sign-in | The assistant opens an **Allow Access?** page on your server; you sign in and select **Allow**. |
| API token | Claude Code, scripts, and clients without MCP sign-in | You create a token and paste it into the client as a bearer token. |

Either way you choose how much the assistant may do:

| Access | The assistant can |
|---|---|
| **Read only** | Search and read your notebooks, books, minutes, tasks and calendar. Tools that change anything aren't offered to it at all, and the token can't change anything through CalDAV or the REST API either. |
| **Read and change** | Also create, change, complete and delete tasks and events, edit Alexandria Client text boxes, queue recognition and Audio Note Minutes, and add new text notes to your BOOX. |

MCP takes tokens only. Your account password works for the web app and
CalDAV, but not at the MCP address, so it never ends up in an assistant's
settings.

## Connect Claude on the Web

1. In Claude, add a custom connector (in Claude's settings, under
   Connectors).
2. Give it a name such as "Alexandria" and enter your MCP address,
   `https://alexandria.example.com/mcp`. No client ID or secret is needed.
3. Claude sends you to your server. Sign in if asked, then read the **Allow
   Access?** page. It names the app and where you'll be sent back to. Choose
   **Read only** or **Read and change** (an app that asks for read access
   only arrives with **Read only** already chosen), and select **Allow**.
4. Back in Claude, the connector shows Alexandria's tools. Turn it on in a
   conversation and ask away.

The approval appears in your token list under the app's name, for example
**Claude (approved here)**, with its access. Approvals made before November
2026 show as **Claude (approved here)** whichever app asked.

## Connect With an API Token

### Create a Token

1. Go to **Settings → API & MCP**.
2. Type what the token is for into **New token** (for example "Claude Code
   on laptop").
3. Tick **Read only** if the assistant should only look.
4. Select **Create token**.
5. Copy the token from the panel that appears. It starts with `alexandria_`
   and is shown only once.

An operator can also create one from the command line, from
`deploy/compose`:

```sh
docker compose run --rm --no-deps migrate create-token --label "Claude Code"
```

It prints a token with full access. (Use `podman compose` if that's your
setup.)

### Claude Code

```sh
claude mcp add --transport http alexandria https://alexandria.example.com/mcp \
  --header "Authorization: Bearer alexandria_your_token_here"
```

Claude Code can also sign in through the browser instead: add the server
without the header, then use its `/mcp` command to authenticate.

### Other Clients

Configure a streamable HTTP MCP server with the MCP address and the header
`Authorization: Bearer <token>`. Treat the token like a password: anyone who
has it can read your library.

## The Tools

The assistant decides which tools to call; you'll usually see each call in
its interface. A few conventions make them work together:

- Notebooks and books are named `boox:<id>` or `client:<id>`, and pages by
  number, starting at 1. Search results and lists give these, so the
  assistant can open what it found.
- Lists come 20 at a time (up to 100 when asked), with a cursor for more.
- Times are in your time zone (**Settings → Calendar & tasks**).
- Every result includes its web address, so the assistant can point you to
  the same thing in the web app.
- Clients are told which tools only read and which change something, so
  they can ask before a change.

### Reading (every token)

| Tool | What it does |
|---|---|
| `search` | Searches everything, as the web **Search** page does: notebook titles, recognized and typed page text from both sources, BOOX tags, Audio Note Minutes and transcripts, books, highlights and annotations, and tasks. Counts by type; narrows to a type, a source, words only, a sort, or dates. |
| `list_notebooks` | Lists BOOX or Alexandria Client notebooks: all of them, or one folder; by title, by kind (BOOX), or the tablet's Trash (BOOX). |
| `get_notebook` | Describes a notebook: each page's recognition state and tags, which pages note actions made tasks or events from, recordings and minutes, and changes waiting for the tablet. |
| `read_pages` | Reads the recognized text of all pages, one page or a range (up to 50 pages at a time); a BOOX text note returns its typed text. Alexandria Client pages also list their text boxes. |
| `get_page_image` | Returns a picture of a page, from either source, so the assistant can read the handwriting itself or see a drawing. |
| `list_tags`, `get_tag` | BOOX tags with how often they're used, and the pages carrying one. |
| `list_books`, `get_book` | Books from either source, by shelf, reading status, title or author (BOOX); one book with its highlights and bookmarks, or Client annotations with text recognized from handwriting. A stored BOOX PDF with handwriting links to the annotated download. |
| `list_minutes`, `get_minutes` | BOOX notebooks with Audio Note Minutes, and the minutes themselves: topic, key points, conclusion, whether they're out of date, and the transcripts unless turned off. |
| `recognition_status` | How recognition is going: notebooks in progress (pages done of total) and pages that failed, were blocked or were skipped, with the reason. |
| `list_tasks`, `get_task` | Tasks, open ones by default, by due date, category, priority or the notebook they were made from. Each says which notebook page it came from. |
| `list_events`, `get_event` | What's on the calendar between two dates, as the web calendar shows it: your events (repeating ones expanded), subscribed calendars, and open tasks due then. |

### Changing (tokens and approvals with **Read and change**)

| Tool | What it does |
|---|---|
| `create_task`, `update_task` | Adds a task, or changes the fields given. `update_task` can also complete or reopen it. |
| `complete_task` | Marks a task done. |
| `delete_task` | Moves a task to Deleted Tasks, where you can restore it. |
| `clear_completed_tasks` | Moves every completed task to Deleted Tasks, like **Clear completed** on Today. |
| `create_event`, `update_event` | Adds an event, or changes the fields given (moving the start keeps the length). Repeating events and subscribed calendars can't be changed here. |
| `delete_event` | Moves an event to Deleted Events, where you can restore it. |
| `edit_text_box` | Replaces the text of an Alexandria Client text box. The change syncs to your devices on their next sync; a newer edit on a device wins. |
| `recognize` | Queues a notebook's pages (all, one or a range) for handwriting recognition, as **Recognize** does on the web. Pages whose content hasn't changed reuse their earlier result without a model call unless the assistant asks for them afresh. Notebooks over the page limit or set to be skipped are refused, with where to change that. |
| `request_minutes` | Asks for a BOOX notebook's Audio Note Minutes, or fresh ones: recordings are transcribed, pages without text recognized (unless told not to), then the minutes written. |
| `cancel_minutes` | Stops a minutes request that is waiting or running. |
| `create_boox_note` | Adds a new text note to your BOOX, in a folder or at the top level. Lines starting `# ` become headings and `- ` bullets. It reaches the tablets on their next sync. |

Recognition and minutes send page pictures, recordings and text to the
providers set up in **Settings → AI models**, exactly as when you start them
on the web. Nothing permanently erases anything, and the only change a tool
makes on your BOOX is adding a new note; it never alters or removes an
existing notebook.
Tasks and events the assistant creates appear in the web app and reach your
CalDAV apps on their next sync. See [Tasks and Calendar](tasks-and-calendar.md).

## Staying Safe

An assistant reads whatever its tools return, and some of that is written by
other people: events in calendars you subscribe to, the text of your books,
and what people said in recordings. Someone could hide an instruction in that
text ("ignore your task and…"), hoping an assistant follows it. This is called
prompt injection, and no server can rule it out entirely, because it's the
assistant that reads. Alexandria keeps the risk small:

- **Text written by others is labelled.** Results mark it as written by a
  calendar's publisher, a book's author or the people recorded, and tell the
  assistant to treat it as information, never as instructions.
- **Subscribed calendars' descriptions stay out unless asked for.** "What's on
  this week" gets times, titles and places; descriptions come only when the
  assistant asks for details.
- **Every change is recorded.** **Settings → API & MCP → Recent changes by
  assistants** lists what each app changed and when, for 90 days.
- **Costly or device-facing actions are capped.** `recognize` takes at most 50
  pages per call; `request_minutes` and `create_boox_note` run at most 10 times
  a day each. Bigger jobs stay on the web.
- **Nothing is erased for good,** and on your BOOX an assistant can only add a
  note, never change or remove one.

What helps most is on your side: give an assistant you don't watch closely
**Read only** access, and leave your assistant's "ask before changing"
confirmations on rather than allowing changes automatically.

## Privacy

Whatever a tool returns goes to the assistant, and from there to its model
provider: search snippets, page text, page images, minutes and transcripts,
task and event details. Alexandria doesn't send anything on its own; it
answers the tool calls the assistant makes. Check your assistant's data
policy, and only connect it to a library you're comfortable sharing with it.

## Revoking Access

1. Go to **Settings → API & MCP**.
2. In the token table, find the token (**Last Used** helps) or **Claude
   (approved here)**.
3. Select **Revoke**.

The client loses access immediately. To use it again, create a new token or
approve it again. Changing your account password does not revoke tokens.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| Claude on the web can't connect or times out | The server isn't reachable from the internet over HTTPS | Put it behind a public HTTPS proxy; see [Put Alexandria Behind HTTPS](https-proxy.md). |
| The approval page sends you back to the wrong address, or sign-in fails partway | Alexandria is working out its own address wrong, usually behind a proxy that doesn't send `X-Forwarded-Proto` | Set `ALEXANDRIA_PUBLIC_URL=https://alexandria.example.com` in `deploy/compose/.env` and restart the `alexandria` service. The **MCP address** in Settings should then show the HTTPS address. |
| "unauthorized" with a token | The token was mistyped, revoked, or sent without `Bearer ` | Check the header is exactly `Authorization: Bearer alexandria_…`; create a new token if it was revoked. Clients that sign in through the browser are asked to approve again. |
| "unauthorized" with a username and password | MCP takes tokens only | Create a token, or let the client sign in through the browser. |
| The assistant says it has no tool to add a task | The token or approval is **Read only** | Approve again choosing **Read and change**, or create a token without **Read only**. |
| "This token can only read." from CalDAV or the API | The token is **Read only** | Use a token with full access for that app. |
| The assistant says a page picture is busy | Page pictures are drawn one at a time, after the web app's own | Ask again in a moment. |
| The assistant's tool list looks out of date | The client remembers tools from an earlier version | Reconnect it, or refresh its tools. |
| Search returns nothing for handwriting you know is there | The page hasn't been recognized | See [Search Your Library](search-and-tags.md). |
| A created task shows the wrong day | Time zone not set | Set it in **Settings → Calendar & tasks**. |
