# Connect BOOX Tablets

Alexandria Server can stand in for the Onyx cloud. A companion app called **PowerSync** points a BOOX tablet's built-in Notes and NeoReader sync at your server, so your notebooks, folders and reading records land in your own library, in their native format. You browse them on the web, search them, recognize their handwriting, and send a few things back: books, text notes and Audio Note Minutes. Your tablets keep working the way they always have; they just sync somewhere you control.

![The BOOX Notebooks list](../images/boox-notebooks.png)

## What You Get

- **Notebooks and folders**, browsable on the web with read-only page previews drawn from the ink the server has received.
- **Reading records** from NeoReader: books, highlights, bookmarks and notes.
- **Tags** you add on the tablet with the lasso's Tag action. They're read-only here, but searching for a tag finds its pages even when the handwriting doesn't say it.
- **Book upload**: send an EPUB or PDF to every tablet's library.
- **Things sent to the tablet**: [Audio Note Minutes](minutes.md) and text notes arrive as ordinary BOOX text notebooks.
- **Handwriting recognition, search and note actions** on BOOX pages. See [Recognize Handwriting](recognition.md), [Search and Tags](search-and-tags.md) and [Note Actions](note-actions.md).

Browsing never changes your tablets. Only a handful of actions write back (deleting numbered notebooks, uploading a book, refreshing reading data, sending a text note or minutes, and audio recovery), and each one tells you so before you confirm.

## What You Need First

- A working server from [Install Alexandria Server](install.md), behind HTTPS ([Put It Behind HTTPS](https-proxy.md)). **BOOX sync requires a real `https://` public address**: PowerSync refuses plain HTTP, and there's no certificate bypass. `ALEXANDRIA_PUBLIC_URL` in `deploy/compose/.env` must be that address, with no path.
- Your reverse proxy must pass WebSocket upgrades and keep long-lived connections open (tablets hold a sync connection), and allow request bodies of at least 65 MiB (books and page files go up to 64 MiB). The Caddy example printed by `install.sh` does all three; see [Put Alexandria Behind HTTPS](https-proxy.md) for nginx and Nginx Proxy Manager.
- The optional BOOX services: Couchbase Server and Sync Gateway, which the `boox.yml` Compose override adds. Setup is below.
- The PowerSync app on each tablet.
- **All your tablets signed in to the same BOOX account.** One library has one owner; the tablets share it.

## Turn On the BOOX Services

Everything happens in `deploy/compose/`. The commands below use the `compose` helper from `common.sh`, which picks Docker or Podman and adds the BOOX files for you once BOOX is enabled. Running each one in `sh -c` keeps your `.env` secrets out of your interactive shell.

1. **Write the Sync Gateway startup file.** Put it somewhere private (for example a directory only root can read), and make the file itself readable by the container. Choose a Couchbase administrator name and a strong password; you'll use them in the next steps.

   ```json
   {
     "bootstrap": {
       "server": "couchbase://couchbase",
       "username": "CHOOSE_A_NAME",
       "password": "CHOOSE_A_STRONG_PASSWORD",
       "use_tls_server": false
     },
     "api": {
       "public_interface": "0.0.0.0:4984",
       "admin_interface": "0.0.0.0:4985",
       "admin_interface_authentication": true
     },
     "logging": { "redaction_level": "full" }
   }
   ```

2. **Add the BOOX settings to `.env`:**

   ```sh
   ALEXANDRIA_BOOX_ENABLED=true
   ALEXANDRIA_BOOX_GATEWAY_CONFIG=/absolute/path/to/boox-gateway.json
   ALEXANDRIA_BOOX_GATEWAY_USERNAME=CHOOSE_A_NAME
   ALEXANDRIA_BOOX_GATEWAY_PASSWORD=CHOOSE_A_STRONG_PASSWORD
   ```

   The Gateway username and password are what Alexandria uses to talk to Sync Gateway's admin interface, which checks them against Couchbase users. Using the same Couchbase administrator as in the startup file is the simplest choice. If you use bind-mounted storage (`ALEXANDRIA_DATA_DIR`), also set `ALEXANDRIA_COUCHBASE_DIR` to an existing, empty directory.

3. **Start Couchbase on its own:**

   ```sh
   sh -c '. ./common.sh; compose up -d couchbase'
   ```

4. **Set up the Couchbase cluster and its bucket.** Alexandria expects a bucket named `neocloud` (change it with `ALEXANDRIA_BOOX_DATABASE`). On a single host, it should have no replicas. With Couchbase's command-line tool:

   ```sh
   sh -c '. ./common.sh; compose exec couchbase couchbase-cli cluster-init -c 127.0.0.1 \
     --cluster-username CHOOSE_A_NAME --cluster-password CHOOSE_A_STRONG_PASSWORD \
     --services data --cluster-ramsize 1024'
   sh -c '. ./common.sh; compose exec couchbase couchbase-cli bucket-create -c 127.0.0.1 \
     -u CHOOSE_A_NAME -p CHOOSE_A_STRONG_PASSWORD \
     --bucket neocloud --bucket-type couchbase --bucket-ramsize 512 --bucket-replica 0'
   ```

   Those commands put the password in your shell history. If you'd rather avoid that, use the Couchbase web console instead: it listens only on the server itself at `127.0.0.1:18091` (`ALEXANDRIA_BOOX_CB_ADMIN_PORT`), so reach it through an SSH tunnel.

5. **Start everything**, including Sync Gateway. The server waits until the Gateway is healthy.

   ```sh
   sh -c '. ./common.sh; compose up -d'
   ```

6. **Let Alexandria configure its Gateway database.** This is a one-time step for each library. It creates the database, turns off guest access, and installs this library's ownership checks. Normal startup never does it for you.

   ```sh
   sh -c '. ./common.sh; compose run --rm alexandria boox-configure'
   sh -c '. ./common.sh; compose restart alexandria'
   ```

   It's safe to run again on the same library. It refuses a Gateway database that belongs to a different library.

Sign in to the web app. The sidebar's **Sources** list now shows **BOOX** *via PowerSync*. If it doesn't, see Troubleshooting.

Neither Couchbase nor Sync Gateway gets a public port. Tablets only ever talk to Alexandria, which checks each device and forwards to the Gateway.

**Backups:** with BOOX turned on, `backup.sh` and `restore.sh` refuse to run, because Couchbase has to be backed up together with everything else. Use `backup-cold.sh`, which needs bind-mounted storage (`ALEXANDRIA_DATA_DIR` and `ALEXANDRIA_COUCHBASE_DIR`). It also saves the Gateway startup file. See [Install Alexandria Server](install.md).

## Connect Your First Tablet

1. On the tablet, sign in to the BOOX account whose library you want on the server. The first tablet decides which account this library belongs to.
2. Install **PowerSync** on the tablet.
3. In the web app, open **BOOX → Devices** and choose **Connect a device**. The **Connect a BOOX Device** page shows your server's address.
4. Choose **Create enrollment code**. The code is eight letters and digits, works **once**, and expires after **ten minutes**. Lowercase is fine. If it expires, choose **Create another code**.
5. In PowerSync, enter the server address exactly as the page shows it, type the code, and follow PowerSync's prompts to connect.
6. In PowerSync, turn on notebook and reading sync to send content to the server.

Your existing BOOX library stays on the tablet. The first sync uploads it, so a large library takes a while: notebook records arrive first, then page files. Until a page's files arrive, its preview may be missing. That doesn't mean the page is blank.

## Add More Tablets

Repeat the same steps on each tablet, with a fresh code each time. Every tablet must be signed in to the same BOOX account as the first one, or be signed out. A tablet signed in to a *different* account is refused, and its code stays unused, so you can fix the sign-in and try again.

A tablet that has drifted from the others (for example one that kept syncing to the Onyx cloud) can bring conflicting copies of the same notebook. Alexandria follows the winner that the tablets' own sync picks, and that isn't always the most recent edit. The server keeps every version it has seen, but back up anything precious on that tablet first (a BOOX local backup or an export).

## Revoke a Tablet or Go Back to Onyx

These are two different things:

- **Revoke** (BOOX → Devices → **Manage** → **Revoke device**) cuts the tablet off from this server almost immediately. Its notes stay in your library. The tablet isn't switched back to Onyx; it just can't sync anymore. To let it back in, connect it again with a new code.
- **Return to Onyx**: use **Restore** in PowerSync on the tablet. That switches its sync back to the Onyx cloud and doesn't need this server. Revoke it here afterward if you're done with it.

## Everyday Use

### The BOOX Tabs

| Tab | What it's for |
|---|---|
| **Notebooks** | Every notebook and folder. Switch between **All notebooks** and **Folders**, filter by **Type** (handwritten, text, reading or other notes), sort, and find by title. The **More** menu has **Clean up numbered notebooks…**, **Deleted notebooks** and **Connect a device**. **Recognize this library…** starts a whole-library recognition review. |
| **Reading** | Books, highlights, bookmarks and notes from NeoReader, plus **Upload a book…**. Page numbers are shown exactly as the tablet recorded them. |
| **Tags** | Every lasso tag, with how many pages and notebooks use it. Open a tag to list its pages, or choose **Search for it**. |
| **Devices** | Connected tablets: **Authorized** or **Revoked**, and **Last Upload Received**. *Authorized* means the tablet may connect, not that everything has finished uploading. |
| **Settings** | Your connection address, library maintenance links, and diagnostics. |

A page count shown as *500+* is a limit of the tablet's metadata. Open the notebook to see every page.

### The Notebook Viewer

![A BOOX notebook page in the viewer](../images/boox-notebook.png)

Step through pages with **← Previous** and **Next →**, or type a page number and choose **Go**. Toggle between **Fit page** and **Actual size**. The preview is drawn on the server, so pen textures are approximate. Text notebooks also show their typed **Page Text**.

The side panel shows, when they apply:

- **Handwriting Recognition**: the page's recognition state and text, **Recognize this page**, **Choose pages…** and a link to **Jobs**. Recognized BOOX text stays on the server and never changes your notebooks.
- **Note Actions**: tasks and events found in colored ink, with **Find actions**. See [Note Actions](note-actions.md).
- **BOOX Tags** on this page and elsewhere in the notebook.
- **Recordings on This Page**, playable in the browser.
- **Audio Note Minutes**: **Summarize this note…** or **Open minutes**. See [Audio Note Minutes](minutes.md).
- **Source and History**: **Recover audio…** (repairs recordings hit by a known BOOX audio defect) and **Observed native versions**.

Deleted notebooks stay viewable under **Deleted notebooks**. Viewing one doesn't restore it.

### Upload a Book

1. Open **BOOX → Reading** and choose **Upload a book…**.
2. Pick an EPUB or PDF of up to 64 MiB and choose **Upload book**.
3. The page shows **Server publication:** and its state. Choose **Refresh status** to update it.

Tablets with reading sync on list the book in their library and download it when you open it there. The server can't see that download, so the page says *Device download: not verified*. Uploading the same file again returns the same upload, even with a different file name. Uploads never replace or rename an existing book.

If a book's ink or annotations look stale on a tablet, open the book's **Reading Details** and choose **Refresh received reading data**, then **Request refresh**. Then open the book on each tablet and accept Sync if prompted.

### Clean Up Numbered Notebooks

BOOX names new notebooks *Notebook-1*, *Notebook-2* and so on, and they pile up. **Notebooks → More → Clean up numbered notebooks…** lists notebooks named exactly `Notebook-` plus digits, from every folder. A name with anything extra is left alone.

1. Review the list. It matches **names only** and never looks at the pages, so open anything you're unsure about. Everything starts checked; uncheck what you want to keep.
2. Tick **Delete the selected notebooks from this library and its synced BOOX devices.**
3. Choose **Delete selected notebooks**.

Opening the review deletes nothing. A review shows at most 200 matches; run another one when those finish. Progress survives closing the browser.

| State | Meaning |
|---|---|
| **Published** | The deletion reached the sync server. Each tablet applies it on its next sync. |
| Pending | Still queued. Retries automatically, and the page refreshes itself. **Cancel not yet published** stops these. |
| **Blocked** | The notebook changed after your review, so it was skipped. |
| **Superseded** | Someone edited it on a tablet after publication. |

The server keeps history, so cleanup doesn't free storage.

### Text Notes

[Audio Note Minutes](minutes.md) can be sent to your tablets as a text note named after the notebook with `_Minutes` on the end. To check that server-created notes reach your tablets, **BOOX → Settings → Create a test text note** makes one in the folder you choose. Start a line with `# ` for a heading, or `- ` or `* ` for a bullet; `**Label:** text` starts a line in bold. Nothing existing is changed. Delete the test note on a tablet when you're done.

### The Notes App's Sync Button

With AutoSync on, the tablets sync on their own as you open and close notebooks. You don't need the cloud sync button in the Notes library. Tapping it makes the tablet re-send **every** notebook record, changed or not, retry earlier failures, and pull the whole tree back to check it. That's harmless here (the server ignores re-saves that change nothing, so recognition and search aren't touched), but it causes a couple of minutes of busywork.

It's worth a tap when a notebook seems stuck, when another tablet's new notebooks haven't shown up, or when a server-created text note appears but is empty.

## Reference: BOOX Settings

| Card | What's there |
|---|---|
| **Connection** | The address tablets use, **Connect a device** and **Manage devices**. |
| **Library Maintenance** | **Clean up numbered notebooks…**, **Deleted notebooks**, **Recognize this library…** |
| **Diagnostics** | **Open diagnostics** (raw records with native IDs, revisions and history, filterable by **Record type**), **Create a test text note**, **How BOOX sync works**. |

Diagnostics are for investigating sync problems. You don't need them day to day.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| No **BOOX** source in the sidebar | The BOOX services aren't enabled, or the server started without the Gateway settings. | Check `ALEXANDRIA_BOOX_ENABLED=true` and the Gateway variables in `.env`, then `compose up -d`. |
| `compose up` stops with *Set the private Gateway startup file* (or another *Set …* message) | A required BOOX variable is missing from `.env`. | Add the variable it names. |
| The server never becomes ready after enabling BOOX | Sync Gateway isn't healthy, usually because Couchbase isn't initialized or the startup file is wrong or unreadable. | Check `compose logs sync-gateway`. Finish step 4 and check the file's credentials and permissions. |
| `boox-configure` says *BOOX requires an HTTPS public origin* | `ALEXANDRIA_PUBLIC_URL` isn't an `https://` address, or it has a path. | Fix it and run the command again. |
| `boox-configure` says the Gateway database *is not owned by this library* | That database was configured for another library. | Use a dedicated database: set a different `ALEXANDRIA_BOOX_DATABASE` (and create a bucket with that name). Don't reuse another library's. |
| PowerSync rejects the code | It was already used, or it's more than ten minutes old. | Choose **Create another code**. |
| PowerSync is refused even with a fresh code | The tablet is signed in to a different BOOX account from the library's. | Sign it in to the same account, or sign it out, and try again. The code stays valid until it expires. |
| PowerSync can't reach the server | The address isn't HTTPS with a trusted certificate, the proxy blocks WebSockets, or the tablet's network can't reach the server (some hotspots offer IPv6 routes that go nowhere). | Open the server address in the tablet's browser. Check your proxy's WebSocket and timeout settings. Try another network. |
| A page preview is missing | Its page files haven't arrived yet, or part of the page isn't supported by the renderer. | Wait for sync to finish. If it stays missing, tap the Notes sync button on the tablet that wrote it. |
| Another tablet's new notebooks don't show up on this one | The tablet hasn't pulled the latest tree. | Tap the Notes sync button. |
| An uploaded book doesn't appear on a tablet | Reading sync is off in PowerSync, or the tablet hasn't synced. | Turn on reading sync, and open the NeoReader library while online. |
| A test or minutes text note is listed but empty | The tablet has the record but not the content yet. | Tap the Notes sync button. |
| A revoked tablet still shows old content | Revoking stops syncing; it doesn't erase the tablet. | That's expected. Use **Restore** in PowerSync to return it to Onyx. |
