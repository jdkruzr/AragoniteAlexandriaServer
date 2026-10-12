<p align="center">
  <img src="assets/aragonite-alexandria-server-mark.png" width="240" alt="Aragonite Alexandria Server woven-page mark">
</p>

# Aragonite Alexandria Server

Aragonite Alexandria Server synchronizes notes and reading data from supported
applications and e-ink devices, with a web interface for browsing, search, tasks
and handwriting recognition. It supports self-hosted and managed deployments.
Alexandria Client and native BOOX applications connect through separate sync
interfaces while sharing server features.

> **Status: active development.** Alexandria Client sync, native BOOX sync,
> source browsing, recognition jobs and standalone deployment are implemented.
> reMarkable and Supernote connectors remain planned; full UltraBridge parity
> is not claimed. See the [source and data-store architecture](docs/architecture.md)
> for implemented boundaries and planned interfaces.

## Architectural Contract

- Alexandria-managed metadata and search use PostgreSQL plus `pgvector`.
  Native BOOX replication additionally uses Couchbase Server and Sync Gateway.
- S3-compatible object storage is the only production blob contract.
- Pods and cloud tasks use local disk only for disposable scratch space.
- Jobs are durable, leased, idempotent records. AWS Batch and Kubernetes are
  execution mechanisms, never the source of truth.
- Notification sockets are wake-up hints; native BOOX CBL/BLIP sockets carry
  replication data. Durable state remains in the owning source repository.
- AWS-specific code stays in infrastructure and adapter packages.

See [Architecture](docs/architecture.md), [development guide](docs/development.md),
and [UltraBridge migration safety](docs/ultrabridge-migration.md).

## Local Stack

```bash
cp deploy/compose/.env.example deploy/compose/.env
# Set separate random hexadecimal database and runtime passwords in that file.
# Do not use the example placeholders for a real installation.
docker compose --env-file deploy/compose/.env \
  -f deploy/compose/compose.yml up --build
```

Then open `http://localhost:18443/health`. Alexandria Server intentionally uses host ports
`18443` and `18089` so a live UltraBridge instance can retain `8443/8089`. The
SPC listener deliberately returns `501` until its protocol port is ready.

## Commands

```bash
go build ./cmd/alexandria-server

# Run the configured role (gateway, worker, maintenance, or all).
ALEXANDRIA_DATABASE_URL='postgres://...' ALEXANDRIA_ROLE=all ./alexandria-server serve

# Apply embedded PostgreSQL migrations.
ALEXANDRIA_DATABASE_URL='postgres://...' ./alexandria-server migrate

# Create/update the single local administrator without exposing a password in
# process arguments, then mint a revocable API token (shown exactly once).
ALEXANDRIA_DATABASE_URL='postgres://...' ./alexandria-server seed-user \
  --username admin --password-file /run/secrets/alexandria_admin_password
ALEXANDRIA_DATABASE_URL='postgres://...' ./alexandria-server create-token --label laptop

# Produce a content-free inventory of UltraBridge snapshots.
./alexandria-server legacy-preflight \
  --notes-db /snapshot/ultrabridge.db \
  --task-db /snapshot/ultrabridge-tasks.db \
  --root /snapshot/supernote \
  --output /secure/location/manifest.json
```

The preflight manifest contains database/table names, row counts, and aggregate
file sizes. It does not extract settings values, filenames, notes, or tasks.

The first authenticated application surface is `/api/v1/tasks`, with list,
create, read, update, and soft-delete operations. It accepts HTTP Basic auth or
the hashed bearer tokens created above. This is an intentionally narrow first
vertical slice, not an assertion of UltraBridge feature parity.

## Deployment Profiles

- `deploy/compose`: small on-prem stack with PostgreSQL/pgvector and SeaweedFS.
- `deploy/helm/aragonite-alexandria-server`: Kubernetes deployment against external
  PostgreSQL and S3-compatible endpoints.
- `deploy/terraform/aws`: AWS economy or HA profile using ECS/Fargate, Aurora
  Serverless v2, S3, and AWS Batch/Fargate.

The AWS module expects an existing VPC, subnets, DNS management, and ACM
certificate. It does not create a surprise network architecture on your behalf.
The [AWS architecture diagrams](docs/architecture-aws.md) map the complete target
design to AWS services and mark remaining deployment work as **To be implemented**.
First-run management tasks and credential cutover are described in
[deployment setup](docs/deployment-identity-upgrade.md). Neither standalone
deployment requires the separate Hosting repository or subscriptions.

Brand usage and the reproducible generation brief for the project mark are in
[the brand note](docs/brand.md).

## License

Apache License 2.0. Selectively ported UltraBridge code must retain its original
copyright/license notices and be identified in `NOTICE`.
