# Alexandria project ownership and session map

Canonical cross-project map, created 2026-09-27 UTC. Keep detailed state in each
repository's `docs/current-state.md`; this file owns only the boundaries and shared
decisions. Relative sibling links assume the existing adjacent checkouts. If a
checkout is elsewhere, locate it rather than creating a duplicate repository.

| Repository / local directory | Owns | Does not own |
|---|---|---|
| [AragoniteAlexandria](../../AragoniteAlexandria/docs/current-state.md) | Android reader/writer, UI, local library, recognition, device lifecycle | Hosting billing or cloud provisioning |
| [AragoniteAlexandriaServer](current-state.md) | Portable library runtime, PostgreSQL/blob storage, jobs, migrations, standalone deployment; future cloud sync/search/MCP ports | Customer signup/subscriptions |
| [AragoniteAlexandriaHosting](../../AragoniteAlexandriaHosting/docs/current-state.md) | Invitations, accounts, billing, tenant routing/provisioning, shared cloud scheduling | Copies of Server domain logic or Android UI |
| [rhizome](../../rhizome/docs/current-state.md) | Generic Kotlin/Go sync, HLC/LWW, registry, atomic receipt and asset transfer contracts | Reader semantics, billing, per-library multi-user collaboration |

## Dependency direction

- Hosting imports Server's **public library runtime**, pinned in `go.mod`; Server
  must still work alone on a local VM or in a personal cloud account.
- Android source-substitutes the sibling Rhizome Kotlin build and checks its exact
  clean Git revision. Rhizome commit changes, including docs, require an explicit
  client pin update; never disable that guard or reset dirty work to satisfy it.
- Reader records, ink/anchors and correction semantics belong to the client and
  its server-side domain adapter. Rhizome transports the registered data without
  acquiring reader-specific interpretation.
- Cloud gateways/workers may share compute, but each hosted customer gets a
  separate library database/runtime role and UUID-scoped objects. Each library
  still represents **one human author across devices**.

## Legacy sources are not the deployed cloud runtime

`~/ForestNote` is the original Android repository and home of historical plans.
Active client work moved to `~/AragoniteAlexandria`; do not continue implementing
Alexandria in ForestNote just because the old conversation started there.

`~/ultrabridge` contains the qualified SQLite-side reader/sync/admission/restore
adapter and its test fixture (`assetlab`). It is a reference/port source, not the
same program as the cloud-native Server. At this split it also contains uncommitted
reader-contract/search changes; inspect and preserve them when resuming porting.
Existing UltraBridge behavior does not prove cloud Server feature parity.

`~/vw_ink_sdk_unofficial` is a sibling Android build dependency. Treat it as its
own project; do not casually vendor or rewrite its code inside the client.

## Explicit shared decisions

- VM, personal cloud without subscriptions, and commercial hosting are all
  first-class deployment choices. Commercial tenancy lives outside Server.
- E2EE was explicitly dropped; TLS, secret handling and tenant isolation remain
  requirements. Do not describe server-visible content as operator-inaccessible.
- Ordinary device join merges. Deliberate whole-library Restore replaces after
  explicit confirmation and uses the shared generation/publication/adoption fence.
- DRY applies across reader/writer and client/server infrastructure: keep one
  contract, shared queues/styling and domain implementations behind adapters.
- No live Stripe charges are authorized. Hosting sandbox success is not launch
  approval or Android sync qualification.
- **Paused by the user at this split: Alexandria/Rhizome cloud porting/deployment.**
  Context cleanup is authorized; it does not resume the deployment plan.

## Starting and handing off work

Open a new Codex session with the owning repository as its working directory.
Read that repository's `AGENTS.md` and `docs/current-state.md`, inspect current Git
state, and follow only the relevant linked plans. For a cross-project change,
identify all touched owners and dependency pins before editing. Do not launch
other agents or deploy services merely because multiple repositories are listed.

After a checkpoint, update the owning handoff with evidence, remaining gates and
the next action. Preserve distinctions between source, local tests, deployed
artifact and real-device behavior. Never copy secrets, private fixture credentials
or the entire historical transcript into these documents. This map and the
handoffs preserve decisions, not a promise of automatic shared chat history.
