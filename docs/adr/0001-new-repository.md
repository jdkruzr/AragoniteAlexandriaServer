# ADR 0001: Build Loom in a new repository

Status: accepted

UltraBridge carries a live personal-information workflow. Aragonite Loom will
therefore be implemented in a sibling repository, selectively porting proven
behavior rather than refactoring the live system in place.

Loom has no runtime, module, database-write, or deployment dependency on
UltraBridge. Migration tooling reads offline snapshots only. This costs more
initial engineering time but prevents unfinished cloud-native work from
becoming an availability risk for the existing installation.
