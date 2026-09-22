# Alexandria identity and deployment upgrade

There are three supported target arrangements: standalone VM, standalone personal
cloud, and commercial hosting. The first two do not need Alexandria Hosting,
Stripe, subscriptions, or a tenant directory. Hosted rollout is not complete yet.

The command is now `alexandria-server`; configuration uses `ALEXANDRIA_*`.
`LOOM_*` variables fail with an explicit rename diagnostic. Existing API token
hashes remain valid; only new tokens carry the Alexandria prefix.

## Existing foundation database

1. Stop all old gateways/workers and take a consistent database/storage backup.
2. Update configuration, retaining the actual database/user/bucket coordinates.
3. Run `alexandria-server adopt-legacy-schema`. This accepts only the known
   original three-migration foundation and compares its schema against a
   transactional reference schema; it is not an UltraBridge import command.
4. Run `alexandria-server migrate` with management credentials.
5. Use the new binary only. No automatic rollback or old-binary restart.

The rename migration preserves rows and keeps the old ledger as a legacy fence.
Historical migration SQL is intentionally immutable. Old object assets require
explicit prefix migration before exposing them through the new scoped runtime;
the database rename alone does not move blob bytes.

## Compose identity

Fresh installs use Alexandria database/user/volume names. For existing volumes,
set `ALEXANDRIA_POSTGRES_VOLUME` and `ALEXANDRIA_OBJECT_VOLUME` to their exact
existing names and retain the initialized PostgreSQL database/user credentials.
Changing POSTGRES_DB on an existing volume does not rename its database. Review
the generated configuration before starting; do not create an empty replacement
volume accidentally. Object storage is no longer exposed as a host port.

## AWS / Kubernetes

The AWS module remains a personal-cloud deployment, not a subscription service.
Terraform moved blocks preserve logical resource addresses. Existing physical
resource names, database coordinates, container names, and target-group names
must also be preserved through configuration before applying an upgrade; reject
any unexpected replacement in the plan. Never blindly apply the renamed defaults
to an existing stack. The hosting stack will be maintained separately.

Run schema migrations as an explicit management task before starting compatible
runtime images. Runtime startup now checks schema compatibility rather than
performing DDL, and checks object storage rather than creating buckets. Use
`init-storage` only during privileged initial setup. Helm installations likewise
need an explicit migration job, not a runtime startup migration.

## Current checkpoint

The runtime API and identity migration have local PostgreSQL tests. Deployment
hardening, automatic first-run setup, hosted provisioning/onboarding, hosted
upgrade campaigns, and live cloud verification are still under implementation.
Do not treat this checkpoint as a release candidate.
