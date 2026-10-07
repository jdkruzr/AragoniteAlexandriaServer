# Alexandria Server project instructions

A little less formality and a little more humor are welcome. Be precise about evidence.

- Read `docs/current-state.md` at the beginning of a fresh task; use `docs/project-map.md` for cross-repository ownership and dependencies.
- This is the standalone cloud-native Server, not legacy UltraBridge and not the commercial Hosting control plane. Local VM and personal-cloud deployment are first-class and must not require Stripe or subscriptions.
- 2026-10-06: the user resumed the Alexandria/Rhizome port (S2), targeting a self-hosted Docker Compose deployment first. Work happens on branch `alexandria-sync-port`, phase by phase; see `docs/remaining-steps.md` S2. Other paused/historical plan items still need an explicit request.
- Preserve the public `library` runtime boundary: Hosting imports it; Server never imports Hosting. Reuse domain logic rather than copying it into adapters.
- PostgreSQL/pgvector is production metadata; S3-compatible storage is production blob storage. Local cloud-task disk is disposable scratch, not durable state.
- A library has one author and multiple devices. Hosted customers are isolated libraries, not collaborating users of one library.
- Use explicit, checksummed migrations and separate management/runtime credentials. Never rewrite historical migration SQL or let normal startup perform privileged DDL.
- Follow `docs/development.md` for porting/license rules. Legacy UltraBridge is a behavior/reference source, not an imported server dependency.
- Use synthetic integration fixtures. Preserve dirty work, real databases and volumes; inspect plans before changing infrastructure. Do not print credentials or raw provider responses.
- Fresh required PostgreSQL/object-store tests are the integration gate; skipped/cached output is not deployment proof. Consult CI for fixture configuration.
- Keep handoffs dated and link evidence rather than repeating an entire plan here. This file is intentionally tracked despite the inherited ignore rule.
