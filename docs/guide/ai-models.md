# Set Up AI Models

Alexandria uses AI models you choose for four jobs: a **vision model** that reads handwriting, a **transcription** service and a **summary model** for [Audio Note Minutes](minutes.md), and an **embedding model** for search by meaning. They're all set on one page, **Settings → AI Tools and Recognition**. Each one is just an HTTP endpoint, so it can be a hosted service, a model on your own GPU box, or a mix. Once a model is set up, automatic processing sends new BOOX material to it (you can turn that off); everything else waits until you queue it.

![Settings → AI Tools and Recognition](../images/settings-recognition.png)

## What You Need First

- A running server ([Install Alexandria Server](install.md)) and the administrator account.
- A **settings encryption key**. `install.sh` generates `ALEXANDRIA_SETTINGS_KEY` in `deploy/compose/.env` for you. Without it, the page says *The deployment needs a settings encryption key before provider credentials can be saved.*
- For each model you want, its base URL, model name, and API key if it needs one. To run models yourself, see [Run Models on Your Own Hardware](local-models.md).

You can set up any subset. Recognition alone is the most common start; search by meaning and minutes are extras.

## Vision Model (Handwriting Recognition)

The first card, **Provider**, holds the **Vision Model** settings.

1. Tick **Enable recognition**.
2. Choose the **API format**:
   - **Anthropic Messages**: Anthropic's API.
   - **OpenAI Chat Completions**: OpenAI, and any OpenAI-compatible server, such as vLLM or Ollama.
3. Enter the **Base URL**, without `/v1` or a trailing slash: for example `https://api.anthropic.com`. Alexandria adds `/v1/messages` or `/v1/chat/completions` itself. Local `http://` addresses work.
4. Enter the **Model** name as the provider spells it. It must be a model that accepts images.
5. Enter the **API key**, if the provider needs one.
6. Choose **Test without saving**, then **Save**.

Saving doesn't queue anything. Existing pages are recognized only when you ask, from a notebook, **Recognition** jobs, or **Recognize a library…**, or automatically for notebooks that change from now on (see below). See [Recognize Handwriting](recognition.md).

### API Keys

Keys are never shown again after saving. The field says *Saved — leave blank to keep* when a key exists. The **Key action** menu decides what happens on save:

| Key action | Effect |
|---|---|
| **Use entered key, otherwise keep saved key** | The default. A key you typed replaces the saved one; a blank field keeps it. |
| **Replace with entered key** | Requires a key in the field. |
| **Clear saved key** | Removes the saved key. |

If you change the **Base URL** while a key is saved, Alexandria asks you to re-enter or clear the key, so it never sends an old key to a new server. After a connection test, re-enter a replacement key before saving; tests don't keep it.

### More Options

- **Anthropic workspace ID (optional)**: only needed for an Anthropic key that can reach more than one workspace. It looks like `wrkspc_…`; find it in the Claude Console under Settings → Workspaces. It's ignored for the OpenAI format.
- **Recognition prompt**: leave it blank to use the built-in prompt, which is shown under the box. A custom prompt applies to new requests. It doesn't reprocess pages by itself.
- **vLLM only: disable thinking (OpenAI format)**: for reasoning models served by vLLM. It asks the model to skip its thinking step, which is faster and cheaper for plain transcription. Leave it off for other providers. It needs the OpenAI format.

## Audio Note Minutes

The **Audio Note Minutes** card transcribes a BOOX note's recordings and summarizes them together with its handwriting. Nothing is sent until you summarize a note and confirm, unless automatic summaries are on.

**Transcription**

| Field | Notes |
|---|---|
| **Enable transcription** | Turns it on. |
| **Base URL** | Any OpenAI-compatible `/v1/audio/transcriptions` server: OpenAI, vLLM, or a self-hosted Whisper server. Leave off `/v1`. |
| **Model** | For example `whisper-large-v3-turbo` for the Whisper server in `deploy/whisper`. |
| **Language** | Blank detects it automatically. Otherwise use a short code such as `en`. |
| **API key**, **Key action** | Optional for local servers. Works like the vision model's key. |

**Summary Model**: **Enable summaries**, **API format** (OpenAI Chat Completions or Anthropic Messages), **Base URL**, **Model**, **API key** and **Key action**. **More Options** has the Anthropic workspace ID and the vLLM disable-thinking switch. Any text model works.

**Summary Instructions** is what the model is told about tone, length and emphasis. The box starts with the built-in instructions. Edit them freely, or clear the box and save to go back. Alexandria always adds its own response format (shown under **Response Format (Always Added)**), so minutes always have a topic, 4–6 keywords, up to five key points and a conclusion. Changing the instructions marks existing minutes out of date; nothing is rewritten until you summarize again.

**When Minutes Are Ready**:

- **Send them to BOOX automatically**: each finished summary goes to your tablets as a text note, exactly as if you'd pressed **Send to BOOX**. Minutes that already exist aren't sent when you turn this on.
- **Language of headings in minutes notes**: English, **Match the recordings**, or any language Alexandria is translated into. The minutes themselves are written in the recordings' language; this only sets labels such as "Key points".

Use **Test transcription** (it sends one second of a synthetic tone) and **Test summary** (one synthetic line), then **Save**.

## Search by Meaning

The **Search by Meaning** card holds the embedding endpoint. It needs an **Ollama-compatible endpoint**, meaning Ollama's `/api/embed` API.

1. Enter the **Base URL**: `http://ollama:11434` for the bundled Ollama service, or your own Ollama server's address.
2. Enter the **Embedding model**. The default is `nomic-embed-text:v1.5`.
3. Choose **Test without saving**, then **Save**.

Alexandria sends no API key to the embedding endpoint, so protect it with your network rather than a key (see [Run Models on Your Own Hardware](local-models.md)).

Saving only sets the endpoint. To turn the feature on, go to **Settings → Search**:

| Status | Meaning |
|---|---|
| **Provider** | Whether the saved service answers and has the model: **Ready**, **Not configured**, **Unavailable** or **Model missing**. It's checked without sending any notes. |
| **Semantic Search** | **On** or **Off**. |
| **Index** | **Not built**, **Building**, **Updating**, **Ready**, **Needs attention**, or paused. |

- **Enable and build index** turns it on and sends your existing searchable text to the embedding model. It's available only when the provider is **Ready**.
- While it builds, the page shows progress, pace and time left, and refreshes every 15 seconds. Keyword search works the whole time.
- **Turn off** pauses new work and keeps the index. **Turn on and resume** picks up where it left off.
- If you change the endpoint or model, the existing index no longer matches. Choose **Build replacement index**. The old index keeps serving searches until the new one finishes.
- **Retry failed index work** appears if anything failed.

Search by meaning only indexes text that already exists. It never starts handwriting recognition, so BOOX pages without recognized text aren't included. **Coverage by Source** shows what's indexed. See [Search and Tags](search-and-tags.md).

## Automatic Processing

Automatic processing is **on by default** once its models are set up. Untick a box and choose **Save automatic processing** to turn that half off; then BOOX pages are recognized only when you ask.

- **Recognize new BOOX pages automatically** queues pages that have never been recognized, in notebooks that change after it starts, once a notebook has been quiet for five minutes.
- **Summarize notes with new recordings automatically** starts Audio Note Minutes for a note whose recording arrived after it started, once the note has gone without changes for **Wait before summarizing** (ten minutes by default, 0 to 120). It recognizes the note's unrecognized pages first.

It never sweeps up your existing library; use **Recognize a library…** for that. Pages are sent to your provider, so charges may apply. Alexandria Client pages don't need this: they're recognized as they sync.

## Note Actions

Handwriting in an assigned BOOX pen color becomes tasks or calendar events. **Find tasks and events in colored ink** is on by default. Each color (**Red ink**, **Sky blue ink**) **Makes tasks**, **Makes calendar events**, or does **Nothing**. **Task Instructions** and **Event Instructions** are editable, with the built-in text shown and the response format always added. Choose **Save note actions**.

Note actions run only when a recognition provider, a summary model and **Recognize new BOOX pages automatically** are all set up; the card tells you what's missing. Details are in [Note Actions](note-actions.md).

## Reprocess Recognized Pages

**Reprocess Recognized Pages** re-runs pages that were already recognized or requested, using the saved settings, for example after you change models or the prompt. It doesn't add new pages. Tick **I want these pages reprocessed, including any provider charges.** and choose **Reprocess previously requested pages**. Unchanged pages reuse cached results, but a new model or prompt means paying again.

## Where Settings and Keys Live

- Settings are stored in the database and take effect immediately, with no restart. Jobs already running finish with the settings they started with.
- API keys are encrypted in the database with `ALEXANDRIA_SETTINGS_KEY` from `.env`. The key itself is never stored in the database. **Keep `.env` with your backups** and protect it like a password. If it's lost or changed, saved keys can't be decrypted and you'll have to enter them again. Never replace a present value.
- The `ALEXANDRIA_OCR_*` and `ALEXANDRIA_EMBED_*` variables in `.env` only seed these settings the first time the server starts. After that, the page is in charge and changing the variables does nothing.
- `ALEXANDRIA_PROVIDER_SETTINGS_LOCKED=true` locks the page. It shows *Provider settings are locked by the deployment.*, and nothing can be saved.

## Choosing Models

- **Hosted models** (Anthropic, OpenAI and others) are the easiest start and are usually the most accurate on messy handwriting. They **charge per request**. A whole-library recognition run is one request per page, so check your provider's pricing first. Connection tests cost a fraction of a cent.
- **Local models** cost electricity instead, and your notes never leave your network. Vision recognition and summaries want a GPU. Transcription and embeddings are lighter and can run on a CPU, just slowly. See [Run Models on Your Own Hardware](local-models.md).
- Mixing is fine. Every card has its own endpoint and key.
- Each page goes to the vision model as a JPEG of at most 1 MB and 4096 pixels on its long side.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| The forms are greyed out | No settings encryption key, or settings are locked. | Run `install.sh` (it adds a missing key without touching others), or remove `ALEXANDRIA_PROVIDER_SETTINGS_LOCKED`. |
| *Use an HTTP(S) base URL without credentials, query parameters or fragments.* | The URL has a username, `?…` or `#…`. | Enter just the scheme, host, port and optional path. Put the key in **API key**. |
| A test fails with a 404 | The base URL includes `/v1`, or the model name is wrong. | Remove `/v1` and check the model's exact name. |
| A test fails with 401 or 403 | Wrong or missing key. | Re-enter the key with **Replace with entered key**. |
| *Re-enter or clear the API key when changing the … endpoint.* | You changed a base URL while a key was saved. | Type the key for the new endpoint, or choose **Clear saved key**. |
| An Anthropic test mentions the workspace | Your key can reach several workspaces. | Fill in **Anthropic workspace ID**. |
| *Settings changed in another session. Reload before saving.* | The settings were saved from another tab. | Reload and redo your change. |
| Recognition returns empty text or rambles | A reasoning model is spending its answer on thinking. | With vLLM, tick **vLLM only: disable thinking**, or use a non-reasoning vision model. |
| **Provider** on Settings → Search says **Model missing** | The embedding model isn't installed on that Ollama. | Pull it there (for example `ollama pull nomic-embed-text:v1.5`). |
| *Provider credential cannot be decrypted.* | `ALEXANDRIA_SETTINGS_KEY` changed since the key was saved. | Restore the original `.env` value, or re-enter the keys. |
| Changing settings didn't recognize anything | Saving never queues work. | Queue pages, run **Recognize a library…**, or check that automatic processing is ticked. |
