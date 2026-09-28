# Alexandria Server handoff

Context split: 2026-09-27 UTC. Repository `/home/jtd/AragoniteAlexandriaServer`,
branch `integration/alexandria-foundation`; implementation HEAD `83e6b0e`.
Worktree was clean before these documentation additions.

Forward checklist: [remaining steps](remaining-steps.md), including standalone
setup, the paused sync port and coordinated release gates.

## What exists and what does not

This is the cloud-native successor to UltraBridge, not feature parity with it.
The foundation includes PostgreSQL/pgvector, S3-compatible objects, leased durable
jobs, probes/metrics, authenticated task/job APIs, explicit schema management,
restricted runtime bootstrap and the public `library` runtime used by Hosting.

VM Compose, Helm and personal-cloud Terraform profiles exist. Standalone VM and
personal cloud require neither Hosting nor subscriptions. Management prepares
schema/credentials/storage; normal runtime checks compatibility instead of running
DDL or creating buckets. Preserve immutable migration history and credential
separation when extending setup/upgrade automation.

**Alexandria/Rhizome enrollment, row sync, assets and whole-library replacement
are not yet ported into this cloud runtime.** Existing client/SQLite UltraBridge
tests are not cloud compatibility proof. The user explicitly paused that
porting/deployment step before requesting this context migration.

## Read next

- [Project map](project-map.md): ownership and legacy sources.
- [ADR 0004](adr/0004-runtime-and-hosting-boundary.md): accepted runtime/Hosting
  boundary and the larger implementation sequence.
- [Architecture](architecture.md), [development](development.md),
  [deployment/identity upgrades](deployment-identity-upgrade.md), and
  [UltraBridge migration safety](ultrabridge-migration.md).
- [Hosting cloud qualification](../../AragoniteAlexandriaHosting/docs/cloud-deployment-qualification.md):
  newer live evidence superseding early “not deployed” wording in historical docs.

## Evidence and deployment distinctions

Hosting's September 27 run verified real public sandbox signup/provisioning,
two isolated library databases, task API isolation, scoped S3 worker jobs and
restart recovery on the reused AWS host. Its deployed build uses the Server
module pin in **Hosting's** `go.mod` (`45eb49115071` at this checkpoint), not an
assumption that this checkout's HEAD was deployed. Later local Server commits
cover standalone setup/manifests/docs; their presence is not a live personal-cloud
acceptance result.

Deployment workstations and Hosting infrastructure details are operator records,
kept in the Hosting operator's private ops directory rather than in this public
repository. Recheck any remote checkout's branch/worktree before use.
Hosting's reused-host stack is separate from this repository's personal-cloud
Terraform profile. Never apply one state as though it owned the other deployment.
No remote change is part of this context split.

## Test entry points

```sh
GOWORK=off go build ./cmd/alexandria-server
GOWORK=off go test -race -count=1 ./...
GOWORK=off go vet ./...
```

The integration gate additionally needs `ALEXANDRIA_TEST_DATABASE_URL`,
`ALEXANDRIA_TEST_S3_ENDPOINT`, and `ALEXANDRIA_REQUIRE_INTEGRATION=1`; follow
`.github/workflows/ci.yml` for disposable PostgreSQL/SeaweedFS fixtures. Never
use a live library as a test DB or claim skipped tests as passing integration.
See the deployment guide before Compose/setup commands; preserve existing volume
identities and keep secrets outside source/command arguments.

## Next action

Start by reviewing this checkpoint with the user; keep cloud sync porting/deployment
paused. Other open work includes browser-first standalone setup/deployment wrappers,
independent VM/personal-cloud qualification and remaining security/upgrade gates.
Do not silently expand context migration into any of those implementations.

Suggested opening prompt: “Read AGENTS.md, docs/current-state.md and the project
map. Summarize the Server foundation and remaining standalone work. Keep the
Alexandria/Rhizome cloud port and deployments paused.”
