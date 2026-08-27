# Architecture

## Runtime shape

```text
main hostname ─┐
               ├─ gateway role ── PostgreSQL + pgvector
SPC hostname ──┘       │                   │
                       │ wake              │ durable leases/search/state
                       v                   │
              cloud/Kubernetes launcher ──┘
                       │
                       v
                  worker role ───────── S3-compatible objects
```

The same image runs every role. In Compose, `all` combines gateway and polling
worker behavior. Kubernetes separates long-running gateways and workers. AWS
economy mode runs one gateway and submits individual worker executions to AWS
Batch so processing capacity reaches zero when idle.

## Consistency rules

1. Request handlers commit metadata and a durable job before waking a worker.
2. Cloud scheduling is at-least-once and may fail independently.
3. A worker must acquire the named PostgreSQL lease before doing work.
4. User-visible outputs use stable idempotency keys or content-addressed keys.
5. A crashed worker loses its lease; maintenance makes the job available again.
6. Reconciliation launches committed work that missed its first wake-up.

## Storage

Binary content uses SHA-256-derived immutable keys. PostgreSQL stores names,
directories, source-specific metadata, references, and delayed-deletion times.
Moves and renames are therefore transactions rather than object copies.

The first schema intentionally uses an unconstrained `vector` column and stores
the model and dimensions beside it. An indexed production embedding model will
use a partial expression index for its selected dimension; changing models is a
controlled reindex rather than silently mixing incompatible vectors.

## The WebSocket containment vessel

Ratta and reMarkable clients use sockets to ask a device to pull. Loom does not
put authoritative changes only in a socket frame. Connection registries stay
in gateway memory, HA gateways fan hints through a replaceable event adapter,
and reconnecting clients always recover from PostgreSQL/object storage.

ForestNote's Rhizome protocol remains request/response and offline-first. Loom
will not add a WebSocket merely because an empty socket looks lonely.
