# Put Alexandria Behind HTTPS

Alexandria Server speaks plain HTTP, and by default only on `127.0.0.1:18443`. It does no TLS of its own. Your tablets, PowerSync, calendar apps and Claude all insist on HTTPS, so something in front has to provide it: a reverse proxy such as Caddy, nginx or Nginx Proxy Manager. This guide lists exactly what that proxy must do, gives working examples for all three, and explains how to check the result.

## What You Need First

- Alexandria installed and answering locally: `curl -fs http://127.0.0.1:18443/readyz` prints `{"status":"ready"}`. See [Install Alexandria Server](install.md).
- A hostname that points at your proxy, such as `alexandria.example.com`.
- A certificate your devices trust. Let's Encrypt, which Caddy and Nginx Proxy Manager handle for you, is the easy route. PowerSync has no "ignore certificate errors" switch, so self-signed certificates won't do.
- If the proxy runs on a **different machine**, make Alexandria listen where the proxy can reach it: set `ALEXANDRIA_MAIN_HOST` (for example `192.0.2.10`) and optionally `ALEXANDRIA_MAIN_PORT` in `deploy/compose/.env`, run `./install.sh` again, and firewall that port to the proxy only.

## Proxy Requirements

This is the list. Every example below implements all of it.

| Requirement | Setting | Why |
|---|---|---|
| **Serve Alexandria at the root of its own hostname** | `https://alexandria.example.com/`, not `https://example.com/alexandria/` | Devices and BOOX setup expect an origin with no path. |
| **Request bodies up to at least 129 MiB.** Use 200 MB. | Caddy `max_size 200MB`; nginx `client_max_body_size 200m` | Books can be up to 128 MiB, and uploading one from the web page adds about 1 MiB of form overhead. BOOX tablets upload files of up to 64 MiB. Alexandria Client sync requests are smaller (up to 16 MiB). |
| **Long timeouts: 20 minutes** | Read and send/write timeouts of `20m` (`1200s`) | Restoring a library from a tablet can take minutes, and BOOX keeps a live connection open. |
| **WebSockets** | Forward `Upgrade` and `Connection` headers over HTTP/1.1 | Needed for BOOX live sync, which uses `/boox-neocloud/`. Alexandria Client, the web app, CalDAV and MCP don't need it, but turning it on for the whole host is harmless. |
| **No redirects on HTTPS requests** | No `www` rewrites, trailing-slash fixes, path rewrites, or single-sign-on/forward-auth login pages in front | Tablets refuse redirects during sync. Alexandria checks passwords and tokens itself; a login portal in front breaks devices, CalDAV and MCP. Redirecting plain `http://` to `https://` is fine. |
| **No buffering** | nginx `proxy_buffering off` and `proxy_request_buffering off`; Caddy `flush_interval -1` | Long uploads and streamed responses should flow straight through. |
| **Pass `Host` and `X-Forwarded-Proto`** | Caddy and Nginx Proxy Manager do this automatically | Keeps generated links and sign-in flows on your HTTPS address. |

About that body size: in Caddy, `MB` means millions of bytes, so `128MB` would be slightly *less* than the 128 MiB books Alexandria accepts. Use `200MB`, as the installer's printed Caddy example does. (Older notes that say 32 MB, 64 MB or 100 MB are too small.)

Two optional extras:

- **Hide `/metrics`.** The server publishes Prometheus metrics at `/metrics` without a password. It holds only request counts and timings, but there's no reason to show it to the internet. The examples return 404 for it.
- **Don't proxy anything else.** Port 18089, the database, and the BOOX Couchbase and Sync Gateway services must not be published through the proxy.

## Set the Public Address

`ALEXANDRIA_PUBLIC_URL` in `deploy/compose/.env` is the address people type, and the installer sets it from your first answer. It's used for links in task and assistant results, Claude's sign-in flow, BOOX device setup, and a check that forms really came from your own web page.

It must match what the browser shows exactly: `https://`, the hostname, a port only if it isn't 443, no path, and no trailing slash. For example, `ALEXANDRIA_PUBLIC_URL=https://alexandria.example.com`.

To change it, edit `.env` and run `./install.sh` again. Tablets keep the address they were set up with, so reconnect them afterwards.

## Caddy

Caddy gets and renews a certificate automatically and forwards WebSockets without extra settings.

```
alexandria.example.com {
    @metrics path /metrics
    respond @metrics 404

    request_body {
        max_size 200MB
    }

    reverse_proxy 127.0.0.1:18443 {
        flush_interval -1
        transport http {
            read_timeout 20m
            write_timeout 20m
        }
    }
}
```

Replace `127.0.0.1:18443` with the address from `ALEXANDRIA_MAIN_HOST`/`ALEXANDRIA_MAIN_PORT` if Caddy runs elsewhere. Reload with `caddy reload` (or restart the Caddy service).

## nginx

Put the `map` block in the `http` context (for example in `/etc/nginx/conf.d/`), and the `server` block wherever your sites live. Certificate lines depend on how you obtain certificates.

```nginx
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 80;
    server_name alexandria.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl;
    server_name alexandria.example.com;
    ssl_certificate     /etc/ssl/alexandria/fullchain.pem;
    ssl_certificate_key /etc/ssl/alexandria/privkey.pem;

    client_max_body_size 200m;

    location = /metrics { return 404; }

    location / {
        proxy_pass http://127.0.0.1:18443;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
        proxy_buffering off;
        proxy_request_buffering off;
        proxy_read_timeout 1200s;
        proxy_send_timeout 1200s;
    }
}
```

Check and reload: `nginx -t && nginx -s reload`.

## Nginx Proxy Manager

1. Open **Hosts > Proxy Hosts** and click **Add Proxy Host**.
2. On the **Details** tab:
   - **Domain Names:** `alexandria.example.com`
   - **Scheme:** `http`
   - **Forward Hostname / IP:** the Alexandria host, e.g. `192.0.2.10` (with `ALEXANDRIA_MAIN_HOST` set to that address)
   - **Forward Port:** `18443`, or your `ALEXANDRIA_MAIN_PORT`
   - **Cache Assets:** off
   - **Websockets Support:** on
   - **Block Common Exploits:** your choice
3. On the **SSL** tab, pick or request a certificate for the hostname, and turn on **Force SSL**. (That only redirects plain HTTP, which is allowed.)
4. On the **Advanced** tab, paste into **Custom Nginx Configuration**:

   ```nginx
   client_max_body_size 200m;
   proxy_read_timeout 20m;
   proxy_send_timeout 20m;
   proxy_buffering off;
   proxy_request_buffering off;
   location = /metrics { return 404; }
   ```

5. Click **Save**. Nginx Proxy Manager already passes `Host` and `X-Forwarded-Proto` and keeps the full request path.

Make changes through the Proxy Manager screens rather than editing its generated files; it rewrites those.

## Verify

1. From any machine:

   ```sh
   curl -fsS https://alexandria.example.com/readyz
   ```

   You should see `{"status":"ready"}`, with no certificate warning. A certificate that works is not the same as a running server; this checks both.
2. Open `https://alexandria.example.com` in a browser and sign in. Make a small change, such as adding a task, to confirm forms are accepted.
3. Connect a tablet: Alexandria under **Settings > Sync > Connect This Device**, or BOOX through PowerSync ([Connect BOOX Tablets](boox.md)). For BOOX, the proxy's access log should show status `101` for `/boox-neocloud/_blipsync`; that's a successful WebSocket upgrade.
4. If you use BOOX, upload a large book from the BOOX pages to confirm the body limit.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `413 Request Entity Too Large` (an HTML page from the proxy) on uploads or sync | The proxy's body limit is too low; nginx defaults to 1 MB. | Set `client_max_body_size 200m` or Caddy `max_size 200MB`. |
| `Choose a file up to 128 MiB.` | That's Alexandria, not the proxy: the file really is too big. | Use a smaller file. |
| `502 Bad Gateway` | Alexandria isn't running or is still starting, or the proxy can't reach it. A common culprit: Alexandria listens on `127.0.0.1` while the proxy is on another machine. | Check `curl http://127.0.0.1:18443/readyz` on the Alexandria host and `docker compose ps`. Set `ALEXANDRIA_MAIN_HOST` to a reachable address and re-run `./install.sh`. |
| `{"status":"not_ready"}` with HTTP 503 | The proxy works; a service behind Alexandria (database, storage, or BOOX sync services) isn't answering yet. | Wait a minute, then read `docker compose logs alexandria`. |
| `504 Gateway Time-out`, or big uploads or tablet restores fail after about a minute | Default proxy timeouts, usually 60 seconds. | Raise read and send timeouts to 20 minutes. |
| BOOX notes sync once, then stop; live sync never connects | WebSocket upgrade isn't forwarded. | Turn on **Websockets Support** in Nginx Proxy Manager, or add the `Upgrade`/`Connection` headers and `proxy_http_version 1.1` in nginx. |
| `native replication unavailable` | Alexandria is reachable, but its BOOX sync service isn't. | Not a proxy problem; see [Connect BOOX Tablets](boox.md). |
| Browser says "too many redirects" | Something in the proxy chain sends HTTPS back to HTTPS. Alexandria never redirects to HTTPS itself. Typical causes: the upstream **Scheme** set to `https`, or a CDN connecting to your proxy over plain HTTP while the proxy forces HTTPS. | Use `http` to Alexandria, and HTTPS end-to-end between CDN and proxy. |
| Tablets fail to connect while the browser works | A redirect or a login portal sits in front (single sign-on, forward auth, `www` rewrite), or the certificate isn't trusted by the tablet. | Remove the redirect or portal for this hostname; use a publicly trusted certificate. |
| `This form must be submitted from the Alexandria web page.`, or `origin required` on BOOX pages | `ALEXANDRIA_PUBLIC_URL` doesn't match the address in the browser (different hostname, port, `http` vs `https`, or a path). | Fix it in `.env` and re-run `./install.sh`. |
| Claude or another MCP client can't finish connecting | Wrong public address, or a login portal in front of `/mcp` or `/.well-known/`. | Fix `ALEXANDRIA_PUBLIC_URL`; let those paths through untouched. See [Connect an AI Assistant](assistants.md). |
