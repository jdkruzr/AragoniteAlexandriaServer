# ADR 0002: PostgreSQL and S3-compatible storage are portability boundaries

Status: accepted

Production metadata, search, vectors, and durable jobs use PostgreSQL with
`pgvector`. Binary content uses a small S3-compatible blob interface. Local
filesystem and SQLite implementations may exist for migration or unit tests,
but are not supported production modes.

AWS uses Aurora PostgreSQL and S3. On-prem installations may supply existing
services or use the CloudNativePG and SeaweedFS reference stack.
