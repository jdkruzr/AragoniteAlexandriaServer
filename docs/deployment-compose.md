# Running Alexandria Server on Your Own Machine

Alexandria Server keeps one Alexandria library in sync across your tablets.
It serves the library on the web, keeps your tasks in sync with calendar apps,
and gives Claude and other MCP clients access to your notes. This guide runs
it on a single Linux host with Docker Compose (or Podman Compose) behind the
TLS reverse proxy you already use.

## What runs

| Service | Role |
|---|---|
| `alexandria` | The server: device sync, web UI, CalDAV, MCP, and the background worker (materialization, search indexing, handwriting recognition, cleanup). Listens on `127.0.0.1:18443`, plain HTTP. |
| `postgres` | PostgreSQL 17 with pgvector. Notebooks, books' metadata, annotations, tasks, search indexes. |
| `seaweedfs` | S3-compatible storage for book files, restore snapshots and task attachments. |
| `migrate`, `bootstrap-runtime`, `init-storage` | One-shot setup steps. They run on every start and do nothing when already done. |

All data lives in two named volumes, `aragonite-alexandria-server-postgres-data`
and `aragonite-alexandria-server-object-data`.

## Install

You need a container engine with Compose, about 2 GB of RAM, and disk space
for your books. The Compose file is the supported deployment; it is tested
with rootless Podman and `podman-compose`, and works with Docker Compose v2
unchanged. `install.sh` uses whichever it finds (or set `COMPOSE`).

To add Compose to Podman: `pipx install podman-compose` (or install your
distribution's `podman-compose` package).

```sh
git clone <this repository> alexandria-server
cd alexandria-server/deploy/compose
./install.sh
```

The installer asks for the public address (for example
`https://alexandria.example.com`) and an administrator name and password. It
writes `deploy/compose/.env` with random database and storage secrets, builds
and starts everything, and prints a reverse-proxy example. Keep `.env`
private, and back it up with your data: it holds the storage keys.

## Reverse proxy

The server speaks plain HTTP on localhost. Your proxy provides HTTPS, which
the tablets require. Requirements:

- **No redirects** on `/sync/`: the tablets refuse them.
- **Large bodies:** at least 32 MB (`/sync/v1` exchanges up to 16 MB).
- **Long requests:** a restore publish can take minutes, so allow 20 minutes.
- **No response buffering**, and pass `Host` and `X-Forwarded-Proto` through.

Caddy (the installer prints this with your values):

```
alexandria.example.com {
    reverse_proxy 127.0.0.1:18443 {
        flush_interval -1
        transport http {
            read_timeout 20m
            write_timeout 20m
        }
    }
    request_body {
        max_size 32MB
    }
}
```

nginx:

```
server {
    server_name alexandria.example.com;
    listen 443 ssl;   # plus your certificate settings
    client_max_body_size 32m;
    location / {
        proxy_pass http://127.0.0.1:18443;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;
        proxy_request_buffering off;
        proxy_read_timeout 1200s;
        proxy_send_timeout 1200s;
    }
}
```

## Connect your devices and apps

- **Tablets:** in Alexandria, open Settings → Sync → Connect This Device.
  Enter the public address as the HTTPS Server URL (the base address, not
  `/sync/v1`) and the administrator account. Each tablet appears under
  Devices in the web UI, where you can rename or revoke it.
- **Web:** open the public address and sign in with the same account.
- **Tasks in other apps:** add a CalDAV account with the address
  `https://alexandria.example.com/caldav/user/calendars/tasks/` and the same
  credentials. Alexandria's own to-do export uses this address too.
- **Claude on the web:** add a custom connector with the address
  `https://alexandria.example.com/mcp`. Claude sends you to a page here to
  approve access. Approved connections are listed under Settings → API and
  MCP Access, where you can revoke them.
- **Other MCP clients:** create a token under Settings → API and MCP Access,
  and send it as `Authorization: Bearer <token>`.

## Optional features

Add these to `.env`, then run `./install.sh` again to apply them.

- Configure **handwriting recognition** and **search by meaning** in the web
  **Settings** page. Changes are stored in PostgreSQL and apply without restarting.
  Recognition supports Anthropic Messages and OpenAI Chat Completions; embeddings
  currently use Ollama. Tests send synthetic content, not your notes.
- The installer creates `ALEXANDRIA_SETTINGS_KEY` in `.env` to protect stored API
  keys. Keep it with your backups. Existing OCR/embedding environment variables
  seed the database once; after that the Settings page is authoritative.
- Reprocessing recognition and building a replacement semantic index are explicit
  actions with their own cost implications. Saving a provider does not schedule
  the entire library. See [provider settings](provider-settings.md) for encryption,
  deployment locks, index replacement and supported source coverage.

## Upgrade

```sh
git pull
cd deploy/compose && ./install.sh
```

Running the installer again keeps `.env`, rebuilds the image, applies
database migrations before the new server starts, and leaves your data in
place.

## Back up and restore

```sh
cd deploy/compose
./backup.sh      # database dump, then a mirror of all stored files, into ./backups
```

The server keeps running during a backup. Copy `deploy/compose/backups` and
`deploy/compose/.env` somewhere else. A nightly cron entry is enough:

```
15 3 * * * cd /path/to/alexandria-server/deploy/compose && ./backup.sh >/dev/null && rsync -a backups/ .env backup-host:alexandria/
```

To restore, put the backup in `deploy/compose/backups` and run
`./restore.sh`. It asks for confirmation, then stops the server, restores
files and database, and starts again. Tablets reconnect on their own.

Tablets keep everything they synced after the backup was taken. On their next
sync they notice the server was restored, send back the changes they made, and
download the library again, so nothing a connected tablet holds is lost. Each
tablet re-sends only its own changes, so a tablet that is gone for good cannot
restore its post-backup work. Tablets need an Alexandria version from October
2026 or later for this.

If a tablet holds a better copy of your library than the server, restore
that copy on the tablet from an Alexandria backup. When it reconnects,
choose Replace Synced Library. That copy then becomes the library on every
device, and the other tablets offer to adopt it.

## Health and logs

- `curl -fs http://127.0.0.1:18443/readyz` succeeds when the database is ready.
- `docker compose logs -f alexandria` (or `podman compose`) shows server logs.
  They are JSON and never contain note text or credentials.
- `docker compose ps` shows the services; the one-shot ones show as exited (0).

## Moving from UltraBridge

Alexandria Server starts with an empty library. Enroll each tablet as above.
The first tablet uploads its library, and others merge with it. Keep
UltraBridge running until every tablet syncs here, then retire it.
