# Nginx Proxy Manager: qualified Alexandria settings

Initial deployment, 2026-10-09. Proxy host `hydrae` (`192.168.9.30`), Nginx Proxy Manager container `sysop-app-1`, proxy-host record 22. Application VM `alexandria-srv` (`192.168.9.90`). These identifiers describe this installation, not product defaults.

## Proxy Host settings

| Setting | Value |
| --- | --- |
| Domain | `alexandria.federation.engineering` |
| Scheme | `http` |
| Forward hostname/IP | `192.168.9.90` |
| Forward port | `8080` |
| Websockets Support | Enabled |
| Block Common Exploits | Enabled in this installation |
| SSL certificate | Valid Let's Encrypt certificate for this hostname |
| Force SSL | Enabled |
| Advanced configuration | See below |

```nginx
proxy_read_timeout 20m;
proxy_send_timeout 20m;
proxy_buffering off;
proxy_request_buffering off;
```

The installed global `client_max_body_size` is `2000m`, sufficient for Alexandria's 64 MiB native asset request limit. Do not require a 2 GB limit on other installations; configure at least `64m` for this adapter. The global connection timeout is 90 seconds; read/send timeouts were also 90 seconds before the per-host override.

WebSocket support generates HTTP/1.1 upstream requests with `Upgrade` and `Connection` forwarded. Proxy Manager's included `proxy.conf` preserves `$host`, forwards scheme/protocol, client IP and port, and proxies the full original request URI. This matters for native BLIP and signed asset paths. Mount Alexandria at the origin root, without an additional URL prefix. PostgreSQL, Couchbase administration, Sync Gateway admin/public ports and SPC are not routed through this host.

## Validation and correction

The initial saved proxy record had `ssl_forced: 0` and no Advanced configuration. HTTP initially returned 502 rather than redirecting. After saving `ssl_forced: 1` and the Advanced settings through Proxy Manager's model and configuration generator, Nginx configuration validation succeeded and the proxy reloaded. A protected pre-change record is saved at `/data/alexandria-proxy-22-before-20261009.json` inside the proxy container. Changes were made to saved configuration, rather than editing generated `22.conf` alone.

Confirmed externally, with normal certificate verification:

- `http://alexandria.federation.engineering/readyz` redirects with HTTP 301 to HTTPS.
- `https://alexandria.federation.engineering/readyz` returns HTTP 200 and `{"status":"ready"}` after application startup.
- Owner-authenticated BOOX setup/devices pages respond successfully.
- Synthetic enrollment returns the same HTTPS origin and `wss://alexandria.federation.engineering/boox-neocloud`.
- Synthetic native sign-in and real Gateway session provisioning succeed through the public origin.
- Authenticated BLIP WebSocket upgrade returns HTTP 101 through TLS, Nginx, Alexandria and the real Gateway after a coordinated restart; the pre-restart session remains usable.
- Revoking the synthetic device causes its prior session to return HTTP 401.
- A synthetic note published to the real Gateway is observed by the production worker and retained in the native revision journal; a subsequent status-0 removal is retained as a second revision. No real-tablet content transfer is claimed.

An initial 502 while the new upstream is not running is expected; certificate success alone is not application readiness. Readiness alone is also not full native synchronization qualification. Real-tablet qualification and long-idle stream qualification are separate.

## Installation holes discovered

The fresh Gateway 3.2 database configuration endpoint returns 403 when the database does not yet exist. Management now authenticates to `/_all_dbs` and confirms absence before creating it; an existing inaccessible database or denied list remains an error. Regression controls cover all three cases.

Storage was previously Docker-volume-only. Configurable bind mounts and independent store paths now support live PostgreSQL/Couchbase on ext4 with assets/backups on CephFS. Compose and Docker startup require the network mount. See [storage and backup configuration](deployment-storage.md).

The standalone server metrics response wrapper also hid ResponseController capabilities, causing a public BLIP upgrade to return 503 despite successful direct Gateway upgrades. The wrapper now exposes `Unwrap`, preserving hijacking and streaming; a regression test drives a WebSocket upgrade through the actual runtime gateway middleware. Gateway startup is health-gated before Alexandria starts; application `/readyz` otherwise checks PostgreSQL and does not prove the full native dependency chain.

Local deployment evidence: `AragonitePowerSync/artifacts/server-deployment-20261009/` (qualification proof, final proxy configuration, storage mapping, required integration test log and checksums). The server keeps protected qualification state under `deploy/compose/private/`. Owner username is `admin`; its generated initial password is stored privately at `/opt/aragonite-alexandria-server/deploy/compose/private/owner-password` on the VM, readable through the authorized SSH account. Never publish that file or include it in documentation examples.
