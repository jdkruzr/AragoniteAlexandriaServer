# BOOX notebook audio recovery

## User workflow

1. In Alexandria, open the affected BOOX notebook. Expand **Source and history** and choose **Recover audio**.
2. Select **Preview audio repair** for one recording. Alexandria validates the retained wrapper and creates a derived version; downloads have not changed yet.
3. Choose **Publish corrected audio**. Refresh the repair status until it says **published**. A stale notebook revision, generation or live resource prevents publication.
4. On each affected BOOX device, stop recording and close the notebook. In PowerSync 0.1.5 or later, choose **Recover notebook audio**, select the recording and confirm **Recover**. Keep Notes closed until it finishes.
5. Reopen the notebook and check playback. Server publication is not an acknowledgement of device application or audible playback.

Healthy local copies are skipped. Missing copies are left for native Notes to download. A local file that differs from the exact known decoding defect is preserved for inspection. This version supports local recovery of MP3s up to 16 MiB on the two qualified API-35 firmware versions. Server-side preview also understands PCM wrappers, but PCM device recovery is deliberately excluded pending native transport qualification.

## Preservation and authorization

The original upload and derived server body are immutable retained versions. Preview and publication reuse the native-revision/generation/live-version-fenced operation journal; there is no automatic rewrite of incoming uploads. An explicit repair changes the live binding only after confirmation.

The device endpoint requires an active companion credential in the current library generation. It offers only explicitly published repairs whose derived version is still live, belonging to an active notebook of this owner. The original wrapper is hash-checked and decrypted; the reconstructed corrected wrapper must match the published derived hash. Unknown, removed, replaced or revoked requests fail closed. Listings paginate in batches of 50.

PowerSync checks its active enrolled server, native account, firmware and local notebook ownership. It verifies the downloaded original against its SHA-256 and compares the existing local file with both the original hash and the independently predicted defective-decoder hash. It keeps a synced private backup, stages outside native resource directories, rechecks the local file and atomically replaces it. It verifies the result after replacement. Filenames, sizes, paths and symlinks are constrained; no broad native restore or deletion is used.

The private backup is in PowerSync's internal `audio-recovery-backups` directory and survives in-place upgrades. Clearing app storage or uninstalling removes that private backup. This release does not have a backup-export UI. The original server upload remains retained separately. Close/stop-recording is a user prerequisite: this workflow does not claim an exclusive lock against a live native recorder.

## Qualification — 2026-10-11

Fresh required PostgreSQL/S3 race tests pass for BOOX and the library runtime; vet passes. Integration checks cover owner-only preview/execution, same-origin protection, unpublished/changed/deleted/foreign/revoked recovery rejection, exact plaintext and predicted defect hashes, retained originals and consistent OSS metadata. PowerSync release unit tests/lint/build pass; file tests cover exact replacement, retained backup, healthy and changed-file refusal, malformed/foreign identities and symlink rejection.

The signed 0.1.5 app was upgraded in place on Palma 2 Pro and Go 10.3 II Lumi with the existing signing certificate. Each disposable test MP3 was first verified against its original, then replaced by the previously captured exact defective native decode. The app's ordinary user flow recovered all 67,824 original bytes on both devices, SHA-256 `b6ef7bda3e43a54c6945a2c87264a8d84ebd9edc2a34ecbac04ee0f9a24ec2ea`. Palma also explicitly skipped the healthy copy. No personal recordings or notebook metadata were changed. Both note-library fingerprints remain unchanged; endpoint, account and sync flags remain active. Playback was not listened to as part of this check.

Server runtime and restore-helper source hashes match the deployment. Public owner recovery page returns 200, anonymous access 401 and readiness 200. The original API remains compatible. Private evidence: PowerSync `artifacts/followup-20261011/`, especially `paired-app-recovery-proof.json`, `synthetic-audio-recovery-before.json`, `deployed-source-proof.json`, UI captures and build/test logs.
