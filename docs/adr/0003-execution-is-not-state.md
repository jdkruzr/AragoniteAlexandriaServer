# ADR 0003: Schedulers execute jobs but do not own them

Status: accepted

AWS Batch, Kubernetes workers, and the Compose process are replaceable job
execution adapters. PostgreSQL owns job identity, leases, attempts, retry
availability, and completion state. Duplicate cloud events are harmless and a
missed cloud event is repaired by reconciliation.
