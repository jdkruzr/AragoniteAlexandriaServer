# Install Alexandria Server

Alexandria Server keeps one library — your notebooks, books, annotations and tasks — in sync across your tablets, and serves it to you on the web, to calendar apps over CalDAV, and to AI assistants over MCP. This guide installs it on one Linux machine with Docker Compose (or Podman Compose), covers where your data lives, and explains how to upgrade, back up and restore it. It takes about ten minutes, most of which is watching an image build.

## What You Need First

- **A Linux host** (a small VM is fine) with about 2 GB of RAM to spare and disk space for your books and notes.
- **A container engine with Compose:** Docker Engine with Compose v2, or Podman with `podman-compose` (`pipx install podman-compose`, or your distribution's package). The installer uses whichever it finds; set `COMPOSE` to force one, for example `COMPOSE="podman compose"`.
- **`git`**, plus `curl` or `wget` (the installer uses one of them to wait for the server).
- **A TLS reverse proxy and a hostname** such as `https://alexandria.example.com`. Tablets only talk to Alexandria over HTTPS. The server itself speaks plain HTTP on localhost; your proxy adds HTTPS. See [Put Alexandria Behind HTTPS](https-proxy.md). Just kicking the tires? You can install with `http://localhost` and add the proxy later — but no tablet will connect until you do.

No subscription, payment account or cloud service is required. Handwriting recognition and search by meaning are optional and can use models you host yourself.

## Install

1. Get the code and run the installer:

   ```sh
   git clone https://github.com/jdkruzr/AragoniteAlexandriaServer.git alexandria-server
   cd alexandria-server/deploy/compose
   ./install.sh
   ```

2. Answer its questions:

   | Prompt | What to enter |
   |---|---|
   | `Public address of this server, as users will type it` | The HTTPS address your proxy will answer at, e.g. `https://alexandria.example.com`. It must start with `https://`; `http://localhost…` and `http://127.0.0.1…` are accepted for local trials only. A trailing `/` is removed. |
   | `Administrator username [admin]` | Press Enter for `admin`, or type another name. |
   | `Password for admin (at least 12 characters)` | Asked after the server is up, twice, without echoing. Use 12 characters up to 72 bytes; the installer re-asks if the two entries differ or the password is too short. |

3. Wait. The installer builds the server image (a few minutes the first time), starts everything, prints `Waiting for the server to become ready` followed by dots, and then `ready.`
4. When it finishes it prints the address it is listening on (by default `127.0.0.1:18443`, plain HTTP) and a Caddy example for your proxy. Set up the proxy now: [Put Alexandria Behind HTTPS](https-proxy.md).

### What the Installer Writes

On the first run the installer creates `deploy/compose/.env`, readable only by you. It contains your public address, the administrator name, freshly generated random passwords and keys for the database and file storage, the default ports, and an `ALEXANDRIA_SETTINGS_KEY` that encrypts API keys you save in **Settings**. The administrator password is never written to `.env`; it goes straight into the database.

Commented-out lines at the bottom are optional startup defaults for handwriting recognition and embeddings. You can ignore them: those providers are configured in the web **Settings** page (see [Set Up AI Models](ai-models.md)), which takes over after the first start.

Re-running `./install.sh` later never overwrites an existing `.env`. It only adds `ALEXANDRIA_SETTINGS_KEY` if an older file lacks one.

### What Runs

| Service | Role |
|---|---|
| `alexandria` | The server: device sync, the web app, CalDAV, MCP and background work such as recognition and search indexing. |
| `postgres` | PostgreSQL with pgvector: your library's records and search indexes. |
| `seaweedfs` | S3-compatible file storage for books, attachments and other files. |
| `migrate`, `bootstrap-runtime`, `init-storage` | One-time setup steps. They run on every start, finish, and show as exited with code 0. That's normal. |

Ports published on the host, all on `127.0.0.1` unless you change them:

| Port | Setting | Purpose |
|---|---|---|
| 18443 | `ALEXANDRIA_MAIN_HOST` / `ALEXANDRIA_MAIN_PORT` | The server. Point your proxy here. |
| 18089 | `ALEXANDRIA_SPC_HOST` / `ALEXANDRIA_SPC_PORT` | Not used by any client. Leave it private and unproxied. |
| 15432 | `ALEXANDRIA_POSTGRES_PORT` | The database, for local administration only. |

## Optional Features

The installer doesn't ask about these. You turn them on in `.env` and run `./install.sh` again.

- **BOOX tablets (PowerSync).** Setting `ALEXANDRIA_BOOX_ENABLED=true` adds the extra BOOX sync services. They need more configuration and a real HTTPS address. If you plan to use BOOX, also read [Where Your Data Lives](#where-your-data-lives) first: BOOX installations can only be backed up with host directories. Full steps: [Connect BOOX Tablets](boox.md).
- **Bundled embeddings (Ollama on CPU).** `./setup-ollama.sh` starts a small Ollama service on the private Compose network and downloads `nomic-embed-text:v1.5` (pass another model name as an argument if you like). It doesn't change any settings or start indexing. Add `ALEXANDRIA_OLLAMA_ENABLED=true` to `.env` so later installer and backup runs include it, then save `http://ollama:11434` under **Settings → AI Tools and Recognition → Search by Meaning**, and build the index on **Settings → Search**. CPU and memory limits are `ALEXANDRIA_OLLAMA_CPUS` (default `1.0`) and `ALEXANDRIA_OLLAMA_MEMORY` (default `2g`). Details: [Run Models Locally](local-models.md).

## First Sign-In

Open your public address in a browser. The browser asks for a username and password: use the administrator account. (Alexandria uses your browser's built-in sign-in prompt, so there's no login page and no **Sign out** button; close the browser to forget the session.)

You land on **Your Library**. On a new install the **Alexandria Client** card says **No devices connected yet.** with a **Connect a device** link, and under **Across Your Library**, **Handwriting Recognition** and **Search by Meaning** show **Off** — keyword search works regardless. A **BOOX** card appears only when BOOX is enabled.

![Your Library overview, shown with a demo library](../images/overview.png)

Next steps:

- **Connect a tablet:** in Alexandria on the tablet, open **Settings > Sync > Connect This Device** and enter your public address (the base address, nothing after it) and the administrator account. The tablet then appears on the **Client Devices** page. BOOX tablets connect through PowerSync instead: [Connect BOOX Tablets](boox.md).
- **Change your password:** **Settings**, **Account** tab, **Change Password**. Enrolled devices and API tokens keep working.
- **Turn on recognition and search by meaning:** [Set Up AI Models](ai-models.md). Hosted models usually charge per page or per request.

## Where Your Data Lives

### Named Volumes (the Default)

Out of the box everything lives in Docker/Podman named volumes:

| Volume | Holds |
|---|---|
| `aragonite-alexandria-server-postgres-data` | The database. |
| `aragonite-alexandria-server-object-data` | Stored files. |
| `aragonite-alexandria-server-ollama-models` | Downloaded Ollama models (a cache; not backed up). |

Names can be changed with `ALEXANDRIA_POSTGRES_VOLUME`, `ALEXANDRIA_OBJECT_VOLUME` and `ALEXANDRIA_OLLAMA_VOLUME` — before the first start, not after.

### Host Directories

To keep data in ordinary directories (a separate disk, a mounted share), set `ALEXANDRIA_DATA_DIR` in `.env` to an absolute path. The database then goes in `$ALEXANDRIA_DATA_DIR/postgres` and files in `$ALEXANDRIA_DATA_DIR/objects`; override either with `ALEXANDRIA_POSTGRES_DIR` and `ALEXANDRIA_OBJECT_DIR`. With BOOX enabled you must also set `ALEXANDRIA_COUCHBASE_DIR`.

- **Create the directories yourself** before starting. Compose refuses to start rather than invent a missing path.
- **Set `ALEXANDRIA_BACKUP_DIR` explicitly** too. The installer writes `ALEXANDRIA_BACKUP_DIR=./backups` into `.env`, and that wins over the data-directory default.
- **Switching an existing install from volumes to directories is a migration**, not a setting flip. Changing `.env` alone gives you a new, empty library. Stop the stack, back up, copy the data across, verify it, then change the setting.

### Network Mounts

If the data sits on a network mount, set `ALEXANDRIA_REQUIRED_MOUNT` to its mount point (this is checked only when `ALEXANDRIA_DATA_DIR` is set). The installer and backup scripts then refuse to run if it isn't mounted. Containers restarted by Docker at boot skip those scripts, so also make Docker wait for the mount, for example with a systemd drop-in for the Docker service containing `RequiresMountsFor=/your/mount`.

### Reaching the Server From Another Machine

The server listens on `127.0.0.1:18443`, which is right when the proxy runs on the same host. If your proxy is on another machine, set `ALEXANDRIA_MAIN_HOST` to this host's LAN address (for example `192.0.2.10`) and optionally `ALEXANDRIA_MAIN_PORT`, then re-run `./install.sh`. That port is plain HTTP, so firewall it to the proxy only.

### Running Compose Commands Yourself

Run commands from `deploy/compose`. A bare `docker compose ...` works for a default install. If you use host directories or BOOX, the scripts add extra Compose files for you; to get the same selection by hand, wrap the command:

```sh
sh -c '. ./common.sh; compose ps'
```

## Upgrade

```sh
cd alexandria-server
git pull
cd deploy/compose && ./install.sh
```

The installer keeps `.env`, rebuilds the image, applies any database updates before the new server starts, and recreates every container. Expect a minute or so of downtime. Your data stays put. Take a backup first if you like sleeping well.

## Back Up and Restore

There are two backup scripts. Which one you use depends on whether BOOX is enabled.

| | `backup.sh` | `backup-cold.sh` |
|---|---|---|
| For | Installs without BOOX | Any install using host directories; **required** for BOOX |
| Downtime | None; the server keeps running | Stops every service during the copy, then restarts them |
| Contents | A database dump plus a mirror of all stored files | Raw copies of the database, files and (with BOOX) Couchbase, plus `.env`, the Compose files and the exact server image, with checksums |
| Requires | Nothing special | Root, `ALEXANDRIA_DATA_DIR`, the `docker` command, and enough free space |
| Refuses when | BOOX is enabled | Run as non-root, without host directories, the server container is missing, or a service had to be force-killed |

### Everyday Backups (No BOOX)

```sh
cd deploy/compose
./backup.sh
```

This writes `alexandria.dump` and an `objects/` mirror into `ALEXANDRIA_BACKUP_DIR` (default `deploy/compose/backups`). Each run **replaces** the previous backup, so copy the directory and your `.env` somewhere else afterwards, ideally somewhere that keeps old versions. A nightly cron entry does it:

```
15 3 * * * cd /path/to/alexandria-server/deploy/compose && ./backup.sh >/dev/null && rsync -a backups .env backup-host:alexandria/
```

To restore, put the backup in `ALEXANDRIA_BACKUP_DIR` and run `./restore.sh`. It asks you to type `restore`, then stops the server, restores files and then the database, applies any database updates, and starts again. This **replaces** the current library. Tablets keep their own copies: on their next sync they notice the restore, re-send changes they made after the backup, and download the library again.

To restore onto a new machine: clone the code, copy your old `.env` and the backup into `deploy/compose`, run `./install.sh` (it won't ask for a password because `.env` exists), then run `./restore.sh`.

### Full Cold Backups (BOOX, or Belt and Braces)

```sh
cd deploy/compose
sudo ./backup-cold.sh
```

It stops all services (waiting up to `ALEXANDRIA_BACKUP_STOP_TIMEOUT` seconds, default 60, each), copies everything into a new `cold-<timestamp>` directory under `ALEXANDRIA_BACKUP_DIR`, writes `SHA256SUMS`, and restarts the services even if the copy fails. An unfinished attempt is left as `cold-<timestamp>.partial`; don't rename it into a real backup. The package contains your credentials, so keep it private and copy it off the host.

The script calls `docker` directly and must see your containers as root. Rootful Docker works out of the box; with Podman you need a `docker` command (such as `podman-docker`) and rootful containers.

To check a package, and optionally unpack it into a **new** directory that must not exist yet (as root, or under `podman unshare`):

```sh
python3 restore-cold.py /path/to/cold-20260101T031500Z
sudo python3 restore-cold.py /path/to/cold-20260101T031500Z --extract /srv/alexandria-restore
```

`restore-cold.py` needs Python 3.11.4 or newer. It only verifies and unpacks; it never starts or overwrites anything. Putting the data back into service is manual: stop the stack, place the unpacked directories into empty data directories with their ownership preserved, use the saved `deployment.env` as `.env`, load the saved server image, and start. Use the same database versions the backup came from. Practise this on a spare machine before the day you need it.

### Keep `.env` Safe

`.env` holds the database and storage passwords and `ALEXANDRIA_SETTINGS_KEY`. API keys saved in **Settings** are encrypted with that key, and the key isn't stored anywhere else. Lose it, or replace it, and those saved keys become unreadable; you'd have to re-enter every provider key. Back up `.env` with your data, keep it private, and never edit the key by hand.

## Health Checks and Logs

- `curl -fs http://127.0.0.1:18443/readyz` returns `{"status":"ready"}` once the database and file storage (and, with BOOX, its sync services) answer, and `{"status":"not_ready"}` with HTTP 503 otherwise. `/livez` only says the process is alive.
- `docker compose logs -f alexandria` (or `podman compose logs -f alexandria`) shows the server's logs, one JSON object per line.
- `docker compose ps` lists services. The one-time setup services show as exited (0).

A ready probe means the server is up, not that every tablet has finished syncing. Check the device pages for that.

## Moving From UltraBridge

Alexandria Server starts with an empty library and doesn't need UltraBridge running. See [Migrating from UltraBridge](../ultrabridge-migration.md).

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `The address must start with https://` | You typed a plain `http://` address for something other than localhost. | Enter the HTTPS address your proxy will serve. |
| `The server did not become ready.` | The image build or a service failed, or port 18443 is taken. | Read `docker compose logs alexandria`. If the port is in use, set `ALEXANDRIA_MAIN_PORT` in `.env` and re-run `./install.sh`. |
| The installer stopped before you set a password, or you forgot it | Re-runs skip the password step because `.env` exists. | From `deploy/compose`, in bash: `read -rs PW; printf '%s' "$PW" \| sh -c '. ./common.sh; compose --profile setup run --rm --no-deps -T seed-admin'; unset PW`. This sets the password for `ALEXANDRIA_ADMIN_USERNAME`. |
| The browser keeps asking for a password | Wrong username or password. | Check `ALEXANDRIA_ADMIN_USERNAME` in `.env`; reset the password as above. |
| `Set ALEXANDRIA_OBJECT_ACCESS_KEY (install.sh generates it)` | Compose ran without `.env`, usually from the wrong directory. | Run commands from `deploy/compose`. |
| `ALEXANDRIA_DATA_DIR must be absolute` | A relative path in `.env`. | Use a full path starting with `/`. |
| `Required data mount is missing: …` | The network mount isn't mounted. | Mount it, then retry. |
| Compose complains that a bind-mount path doesn't exist | Data directories weren't created. | Create them, then retry. |
| `Set ALEXANDRIA_COUCHBASE_DIR for native BOOX` | BOOX plus host directories, without a Couchbase directory. | Set and create `ALEXANDRIA_COUCHBASE_DIR`. |
| `Native BOOX requires coordinated backup/recovery…` | `backup.sh` or `restore.sh` on a BOOX install. | Use `sudo ./backup-cold.sh` (needs host directories). |
| `Application container missing; refusing incomplete backup.` | The server isn't running, or root can't see rootless Podman containers. | Start the stack; use rootful containers for cold backups. |
| `Forced/OOM container stop; refusing cold backup.` | A service didn't stop in time or ran out of memory. | Read `stop-status.txt` in the `.partial` directory, raise `ALEXANDRIA_BACKUP_STOP_TIMEOUT`, and retry. |
| `ALEXANDRIA_SETTINGS_KEY must be 64 hexadecimal characters` | The key in `.env` was edited or truncated. | Restore the original value from your backup of `.env`. |
| Saved API keys stopped working after moving hosts | The new `.env` has a different settings key. | Use the old `.env`, or re-enter the keys in **Settings**. |
| Tablets can't connect, but the web page loads locally | No HTTPS proxy, or it's misconfigured. | [Put Alexandria Behind HTTPS](https-proxy.md). |
