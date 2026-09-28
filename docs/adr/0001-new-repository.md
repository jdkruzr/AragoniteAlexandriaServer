# ADR 0001: Build Loom in a new repository

Status: accepted

> Note (2026-09-27): Loom was this project's working name; it is now Aragonite
> Alexandria Server. The decision below is kept as written.

UltraBridge carries a live personal-information workflow. Aragonite Loom will
therefore be implemented in a sibling repository, selectively porting proven
behavior rather than refactoring the live system in place.

Loom has no runtime, module, database-write, or deployment dependency on
UltraBridge. Migration tooling reads offline snapshots only. This costs more
initial engineering time but prevents unfinished cloud-native work from
becoming an availability risk for the existing installation.
