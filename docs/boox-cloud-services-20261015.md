# BOOX cloud services after the PowerSync cluster switch (2026-10-15)

Handoff for the next session. Status: **investigated, not yet built.** The owner
approved the first two steps of the plan below on 2026-10-15.

## Problem

PowerSync retargets a BOOX tablet's sync to Alexandria by switching the device's
ksync "cluster". The owner reports that this also breaks other Onyx cloud
features: **Feedback** (system Settings), the **Template Hub** (Notes app), and
possibly **firmware (OTA) updates**. The goal is to restore them without losing
the Alexandria sync endpoint.

## Owner decisions

- Losing firmware updates is far worse than Alexandria holding the owner's Onyx
  credentials to forward a few requests to Onyx. A pass-through through
  Alexandria is an **accepted design** (the installation belongs to the user).
  Credentials, if needed, are stored encrypted like provider keys, never logged,
  and the UI says plainly which Onyx services go through Alexandria.
- Prefer a per-service approach where it avoids credentials. Fall back to the
  pass-through where it doesn't.

## How the switch works (confirmed in source)

- PowerSync (`../AragonitePowerSync`):
  - `client/src/main/java/dev/aragonite/powersync/client/NativeBridge.java` writes
    the system config key `ONYX_CLUSTER_SYSTEM_KEY` through the
    `onyx_device_service` binder (transaction 4) and reads it back to verify.
    It then calls ksync `ISyncService` `switchCluster` (80) and
    `updateClusters` (81) and broadcasts `SERVER_INFO_CHANGED_ACTION`.
  - Supported devices: Palma2_Pro_C and Go103_2Lumi, API 35, ksync 32173 or 32079.
  - `client/.../EndpointProfile.java` (`validate()`) **requires all nine host
    fields to be same-origin with Alexandria**: `onyxHostBaseUrl`,
    `onyxCloudDataHostBaseUrl`, `onyxLogHostBaseUrl`, `onyxContentHostBaseUrl`,
    `onyxSend2BooxHostBaseUrl`, `shopBookUrl`, `onyxMaterialHostBaseUrl`,
    `onyxCouchbaseWSSHost`, `onyxMessageReplicator`. OSS endpoints are set in
    `ossConfig` too. **This is the root cause**: every Onyx service moves with sync.
  - `docs/ksync-retargeting-spike.md` and `docs/message-channel.md` hold the
    background. Switching logs the account out.
- Firmware (decompiled in `~/booxreverse/decompiled`): one cluster record
  (`com.onyx.kime/.../data/cluster/model/ClusterInfo.java`, `getCloudConf(int mode)`,
  lines 101–123). `data/CloudManager.java` picks the mode per feature:

| Mode | CloudManager call | Host field | Path base |
|---|---|---|---|
| 0 (default) | — | `onyxHostBaseUrl` | `api/v1/` (account, `users/syncToken`, login, market apps) |
| 1 | `useDataCloudConf` | `onyxCloudDataHostBaseUrl` | |
| 2 | `useContentCloudConf` | `onyxContentHostBaseUrl` | |
| 3 | `otaCloudConf` | **unknown** (missing from the decompiled method) | |
| 4 | `sendFeedBackCloudManager` | `onyxSend2BooxHostBaseUrl` | |
| 5 | `useSendToBooxConf` | `onyxSend2BooxHostBaseUrl` | |

  Note sync uses `onyxCouchbaseWSSHost`; the MESSAGE channel uses
  `onyxMessageReplicator`. The stock US cluster (`ClusterHost.java`) sets
  `onyxHostBaseUrl`, `onyxSend2BooxHostBaseUrl`, `onyxContentHostBaseUrl` and
  `shopBookUrl` to `https://push.boox.com`.

## Per feature

- **Template Hub (confirmed):** `com.onyx.android.note/.../note/action/background/LoadCloudBKGroundAction.java:44`
  uses mode 5. `request/template/LoadCloudBKGroundRequest.java:45` calls
  `GET api/1/writeTemplates?model=&lang=&apiVersion=&category=&usage=`
  (`OnyxTemplateService.java:10`) on the send2boox host, with **no Authorization
  header**. Files come from absolute `file.url` values in the response
  (`DownloadTemplateAction`), which are presumably Onyx CDN URLs. Today the list
  request hits Alexandria, which has no such route.
- **Feedback (path confirmed, Settings UI inferred):**
  `com.onyx.android.ksync/.../ksync/message/action/SubmitFeedbackAction.java`
  uses mode 4. The steps:
  1. `GET api/v1/encrypt/key` (`GetLogEncryptionKeyRequest`, `OnyxLogService`),
     anonymous, on the send2boox host.
  2. Log zip upload to OSS (`CompressAndUploadZipToOssRequest`). Credentials not
     traced.
  3. The report is a **MESSAGE document (msgType 0)** replicated over the message
     channel, i.e. into Alexandria's Couchbase, not Onyx's. PowerSync's lab
     policy currently denies MESSAGE writes.

  Entry points: ksync binder `submitFeedback` (`KSyncService.java:818`) and
  `KSyncCommonReceiver.java:99`. The Settings app isn't in the decompile set.
- **Firmware updates (unresolved):** there are two code paths.
  - `data/manager/OTAManager.java:131` uses `otaCloudConf()` (mode 3,
    cluster-derived, host unknown).
  - `firmware/request/FirmwareUpdateRequest.java:17` is **hardcoded** to
    `https://data.boox.com/api/` and doesn't follow the cluster.

  `OnyxOTAService.java` defines `firmware/update`, `app/list`,
  `apks/batchQuery` and beta-group calls. Which path runs needs a live check.
- **TLS:** no pinning was found in these paths (`data/utils/HttpUtils.java`).
  Trust-all classes appear only in `WebDavProvider`. ksync permits cleartext. The
  Notes app's cleartext policy is unchecked.
  `../AragonitePowerSync/docs/native-tls-tunnel.md` hasn't been reviewed.

## Options considered

1. **Field-level split in PowerSync:** stock hosts for non-sync fields. The
   firmware accepts it, but it breaks `EndpointProfile` same-origin on purpose.
   Anything session-bound (Feedback) then reaches Onyx with an Alexandria
   session and is likely rejected.
2. **Pass-through in Alexandria (preferred):** forward allow-listed non-sync
   paths to Onyx. Templates and the feedback log key are anonymous, so no
   credentials are needed. OTA depends on the live check.
3. **Feedback delivery:** relaying MESSAGE msgType 0 to Onyx would need the
   owner's Onyx Couchbase session and a PowerSync policy change. Alternative:
   accept that Feedback is unavailable while synced to Alexandria, and point to
   Onyx web or email support.

## Plan (approved 2026-10-15: steps 1–2)

1. **Log unknown requests** on Alexandria's BOOX-facing gateway listeners
   (`:8443` main and `:8089` spc): method and path only. No headers, tokens,
   cookies, bodies or query strings. Rate-limited or deduplicated. Then the
   owner taps Settings → Firmware Update → Check on one tablet. This shows
   whether OTA (or anything else) is hitting Alexandria. Today the app doesn't
   log requests at all, and logs reset on each deploy, so a 72 h search found
   nothing either way.
2. **Template Hub pass-through:** an anonymous, allow-listed forward of
   `GET /api/1/writeTemplates` (exact path, GET only, query passed through, no
   cookies or auth headers forwarded either way) to `https://push.boox.com`.
   Check the response's `file.url` hosts. Test with a synthetic upstream. Verify
   on one tablet's Template Hub (a read; no notes opened).
3. **OTA pass-through** if step 1 shows update checks reach Alexandria. Identify
   the mode-3 host from traffic first.
4. **Feedback:** the log-key pass-through is easy. Report delivery needs an owner
   decision (credential relay versus "unavailable while synced").

## Constraints that still apply

Read-only on tablets unless the owner acts; never reset or uninstall; opening a
note is a write. Don't print credentials. Use synthetic fixtures. The fresh
PG/S3 gate before deploys. Server never imports Hosting.
