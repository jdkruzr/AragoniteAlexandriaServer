# Connect an AI Assistant (MCP)

MCP (Model Context Protocol) is a standard way for AI assistants to use
outside tools. Alexandria Server includes an MCP server, so an assistant such
as Claude can search your notes, read pages and minutes, edit Alexandria Client
text boxes, and manage your tasks and calendar, all on your say-so. "What did I
write about the irrigation budget last month?" becomes a question the
assistant can actually answer.

## What You Need First

- Your server's MCP address: **Settings → Tasks & Integrations → API and MCP
  Access** shows it as **MCP address**, for example
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

Either way, the assistant gets the same access: it can search and read your
notebooks and books, edit text boxes, and manage tasks and events.

## Connect Claude on the Web

1. In Claude, add a custom connector (in Claude's settings, under
   Connectors).
2. Give it a name such as "Alexandria" and enter your MCP address,
   `https://alexandria.example.com/mcp`. No client ID or secret is needed.
3. Claude sends you to your server. Sign in if asked, read the **Allow
   Access?** page (it names the app and where you'll be sent back to), and
   select **Allow**.
4. Back in Claude, the connector shows Alexandria's tools. Turn it on in a
   conversation and ask away.

The approval appears in your token list as **Claude (approved here)**. Every
approval made through this page shows under that name, whichever app asked.

## Connect With an API Token

### Create a Token

1. Go to **Settings → Tasks & Integrations**.
2. Under **API and MCP Access**, type what the token is for into **What is
   this token for?** (for example "Claude Code on laptop").
3. Select **Create token**.
4. Copy the token from the **New API Token** page. It starts with
   `alexandria_` and is shown only once. Select **Done**.

An operator can also create one from the command line, from
`deploy/compose`:

```sh
docker compose run --rm --no-deps migrate create-token --label "Claude Code"
```

It prints the token. (Use `podman compose` if that's your setup.)

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
its interface.

### Notes and Search

| Tool | What it does |
|---|---|
| `search_notes` | Searches Alexandria Client pages, recognized BOOX pages (including their BOOX tags), reading text, and Audio Note Minutes with their transcripts. Can use words only or words and meaning, and narrow to one source. Results include BOOX tags and web links. |
| `get_note_pages` | Returns the text of every page in an Alexandria Client notebook, or of one page. |
| `get_note_image` | Returns the rendered image of an Alexandria Client page, so the assistant can look at the handwriting itself. |
| `get_note_minutes` | Returns a BOOX note's Audio Note Minutes: topic, keywords, key points, conclusion, recording details and, unless turned off, the transcripts. |
| `list_text_boxes` | Lists the editable text boxes in an Alexandria Client notebook, with their current text. |
| `edit_text_box` | Replaces the text of an Alexandria Client text box. The change syncs to your devices on their next sync; a newer edit on a device wins. |

The page, image and text-box tools work on Alexandria Client notebooks. BOOX
pages are reached through `search_notes` (which returns their text and a web
link) and `get_note_minutes`; Alexandria never changes BOOX notebooks.

### Tasks

| Tool | What it does |
|---|---|
| `list_tasks` | Lists tasks, optionally by status, due date range, source notebook, category or priority. |
| `get_task` | Fetches one task with all its details. |
| `create_task` | Creates a task; only the title is required. |
| `update_task` | Changes only the fields given. |
| `complete_task` | Marks a task done. |
| `delete_task` | Moves a task to Deleted Tasks. |
| `purge_completed_tasks` | Moves every completed task to Deleted Tasks. |
| `purge_deleted_tasks` | Permanently erases deleted tasks older than a number of days (30 by default). Irreversible. |

### Calendar

| Tool | What it does |
|---|---|
| `list_events` | Lists events in a date range (from now onward by default), in your time zone. |
| `get_event` | Fetches one event. |
| `create_event` | Creates a timed or all-day event, with optional location, notes and link. |
| `update_event` | Changes only the fields given; moving the start keeps the length unless an end is given. |
| `delete_event` | Moves an event to Deleted Events, where you can restore it. |

Tasks and events the assistant creates appear in the web app and reach your
CalDAV apps on their next sync. See [Tasks and Calendar](tasks-and-calendar.md).

## Privacy

Whatever a tool returns goes to the assistant, and from there to its model
provider: search snippets, page text, page images, minutes and transcripts,
task and event details. Alexandria doesn't send anything on its own; it
answers the tool calls the assistant makes. Check your assistant's data
policy, and only connect it to a library you're comfortable sharing with it.

## Revoking Access

1. Go to **Settings → Tasks & Integrations**.
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
| "unauthorized" with a token | The token was mistyped, revoked, or sent without `Bearer ` | Check the header is exactly `Authorization: Bearer alexandria_…`; create a new token if it was revoked. |
| The assistant can't open a BOOX page image | Page and image tools only work on Alexandria Client notebooks | Ask it to use `search_notes` or `get_note_minutes` for BOOX content. |
| Search returns nothing for handwriting you know is there | The page hasn't been recognized | See [Search Your Library](search-and-tags.md). |
| A created task shows the wrong day | Time zone not set | Set it in **Settings → Tasks & Integrations**. |
