# ADR 0004: Alexandria runtime and separate hosting platform

Status: accepted (2026-09-21)

Aragonite Alexandria Server is independently deployable on a local VM or in a
personal cloud account. Neither deployment requires subscriptions, a hosting
directory, Stripe, or the commercial Hosting application.

AragoniteAlexandriaHosting is a separate repository and binary. It composes
Server's public, library-scoped Go runtime; it never copies domain logic or
imports internal packages. Server does not depend on Hosting.

Hosting owns invitations, customer accounts, Stripe Checkout/Portal, tenant
routing, provisioning, shared scheduling, and upgrade campaigns. Server owns
library storage, authentication interfaces, jobs, migrations, admission, and
eventually sync/search/MCP. Hosting supplies bound authentication and admission
policies without teaching Server about invoices.

Each hosted customer is one author with one library in a separate PostgreSQL
database and restricted runtime role on shared infrastructure. Objects use
immutable customer-ID prefixes; no cross-customer deduplication. Gateways and
workers are shared rather than permanently allocated per customer.

Hosting onboarding is invitation-only, followed by customer-selected subdomain,
Stripe subscription checkout and resumable provisioning. A failed renewal has
seven days' grace; then content is read-only, with downloads/export retained.
Cancellation retains service through the paid-through date. There is no automatic
data deletion. Billing cannot clear maintenance, provisioning failure, or an
administrative suspension.

All three deployment arrangements are first-class: local VM, personal cloud,
commercial hosting. E2EE/confidential computing and managed inference billing
are out of scope. Normal TLS, protected credentials, isolation, and redacted
operational logging remain required.

## Implementation sequence and acceptance

1. Rename current product identities to Alexandria, retaining migration history
   and truthful legacy-format/license attribution.
2. Expose Server's library runtime and pinned-connection, checksummed migrations.
3. Build Hosting directory/provisioning/routing with cross-tenant tests.
4. Implement invitations, customer onboarding and Stripe test-mode lifecycle.
5. Qualify per-library upgrades and all three deployment arrangements.
6. Return to the larger Alexandria integration plan: port enrollment, row sync,
   assets and whole-library replacement, then Tab -> AWS -> Go qualification.

CI must execute PostgreSQL/object-store integration tests rather than silently
skip them. Migration failures leave only the affected library paused; old
incompatible binaries must not serve upgraded schemas. Customer signup, upgrades
and duplicate Stripe events must be resumable/idempotent.

This milestone is not complete until hosted TLS onboarding, Stripe test-mode,
isolation, crash/retry upgrades and both standalone deployments are verified.
Production UltraBridge and its existing AWS experiment are not automatically
replaced by this work.
