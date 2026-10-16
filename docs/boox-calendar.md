# BOOX Calendar Memo

2026-10-16. What BOOX's Calendar Memo app keeps, what of it syncs, and what
Alexandria receives. Exploration only; nothing was built.

## The Short Version

The owner added a Reminder named "Test Reminder" on the Palma. The app said it
had synced to the Onyx Cloud, but it never reached Alexandria, and it never
will: **Reminders stay on the device.** The app's own settings text (string
`calendar_settings_tips`, present in the Palma's com.onyx.mail v32454) says:

> • Events: Synced only with linked third-party accounts (e.g. Outlook).
> • Reminders: Stored locally on this device only.
> • Memo: Synced via Onyx Cloud only.

| Item | Where it lives | How it syncs |
| --- | --- | --- |
| Reminder | `CalendarDatabase.ReminderModel` (app-private) | Never |
| Event | `CalendarDatabase.CalendarEventModel` | Google, Outlook or CalDAV account only |
| Memo | `ShapeDatabase.NoteModel` with `associateDate` | ksync → `{uid}-CALENDAR_TREE`, strokes to `{uid}/calendar/{docId}/` |

## The App

- Package `com.onyx.mail` (KMail). Calendar Memo is `app_name_calendar`. On the
  Palma it is v32454, activity `.calendar.ui.CalendarActivity`. The screen has
  Events, Reminder, Today's Notes and Today's Read tabs.
- The Chinese characters next to "Today" (e.g. 初七) are the lunar date
  (2026-10-16 is day 7 of the ninth lunar month).

## Reminders

- DBFlow database `CalendarDatabase` (version 3), table `ReminderModel`:
  `id, title, startTime, isEnableDate, isEnableTime, reminderTime (-1 = none),
  rule (repeat), isHasAlarm, soundPath, status (0 open, 1 complete), isDelete,
  originalId, originalInstanceTime` (exceptions to a repeating reminder).
  There is no user or account column.
- Alarms are scheduled with AlarmManager. No code sends a `ReminderModel`
  anywhere; the cloud-calendar converters use only `EventReminderModel` (an
  event's alarm).
- Checked on the Palma: "Test Reminder" isn't in any
  `/sdcard/.ksync/couch/*.cblite2` database or on shared storage. It's only in
  app-private storage, which ADB can't read without root.
- Same database: `CalendarEventModel`, `CalendarModel`, `CalendarSyncInfoModel`,
  `EventReminderModel`, `EventSyncInfoModel`, `ProviderAccountModel`,
  `ResourceLinkModel`.

## Memos

- A memo is an ordinary notebook (`NoteModel`) linked to a date by
  `associateDate` and `association` {associationId, associationType}.
- ksync `CalendarTreeReplicator` (extends `NoteTreeReplicator`) replicates them
  as `{uid}-CALENDAR_TREE`. Pen data goes to object storage under
  `{uid}/calendar/{docId}/point/…` and `…/shape/<page>.zip`.
- They push only when the account is logged in **and** the system Notes sync
  switch is on (`key_enable_sync_note_data`, the parent replicator's filter).
  Auto-push also needs "Automatically sync to ONYX Cloud" (`auto_cloud_sync`,
  default off); otherwise only the cloud button pushes.
- Alexandria already holds 34 CALENDAR_TREE documents (25 memo notebooks, newest
  2026-09-30, and 9 commits) in `boox_projection` as `domain='opaque'`,
  unsupported. Nothing displays, recognizes or searches them yet. The storage
  report already counts `calendar` blobs as "Calendar resources".

## Events

- Events sync only with linked Google, Outlook or CalDAV accounts. kmail ≥32216
  imports CalDAV one way. Since 2026-10-16 (migration 0031) Alexandria also serves
  an events calendar over CalDAV ([boox-note-actions.md](boox-note-actions.md)),
  so Calendar Memo can import it; not yet tried on a device.

## Also Seen

- Devices request the `APP_DATA`, `CALENDAR_TREE` and `CONFIG` channels. No
  `APP_DATA` or `CONFIG` documents have reached the journal.

## Evidence

Decompiled sources in `~/booxreverse` (mail = `go103lumi/decompiled/com.onyx.mail.42rel_0410`,
v32180; ksync = `go103lumi/decompiled/com.onyx.android.ksync`, v32073):

- mail `resources/res/values/strings.xml:108` (app name), `:272` (sync tips)
- mail `android/sdk/calendar/db/CalendarDatabase.java`, `…/db/table/ReminderModel_Table.java`,
  `…/calendar/data/ReminderStatus.java`, `mail/calendar/reminder/action/AddReminderAction.java`
- mail `mail/calendar/meno/converter/NoteModelConverter.java`, `…/meno/action/doc/MigrateMemoDataAction.java`
- ksync `sdk/data/sync/KSyncConstant.java:18,42`, `ksync/calendar/couch/CalendarTreeReplicator.java`,
  `NoteTreeReplicator.java` (push/pull filters), `OnyxSystemConfig.java`
- mail `AppSettings.java` (`auto_cloud_sync`), `PushCalendarTreeAction.java` (login filter)

**Rechecked in the Palma's own build (v32454, decompiled 2026-10-16 to
`~/booxreverse/palma2pro/2026-10-16_mail/decompiled/kmail-32454`).** The
calendar's cloud button (`ReaderCalendarFragment`, private method `P()`) runs
`syncMemo()` and `SyncAllCalendarDataAction` (linked-account events) and nothing
else. The `com.onyx.mail.calendar.reminder` package (add, complete, delete,
query, search and alarm scheduling) has no network or ksync code. Outside it,
`ReminderModel` is used only by the reminder screens, its DBFlow table and
`ReminderProvider`; the Google, Outlook and CalDAV converters use only
`EventReminderModel` (an event's alarm). Any "synced" indicator after adding a
reminder reflects memos and events, not the reminder.

Remaining gap: the Palma's ksync v32173 sources aren't decompiled (pulling it
over ADB stalled), so memo sync rules come from v32073.
`~/booxreverse/BOOX_RESEARCH.md` lists CalendarReplicatorManager as "calendar
events"; it replicates memos.

## Possible Next Steps (Each Needs an Explicit Request)

- Make memos first-class: list by date in BOOX, render, recognize, search.
- ~~Add VEVENT to Alexandria's CalDAV~~ (done 2026-10-16; device import untested).
- Reminders could only come off a device through a helper on it. Even
  PowerSync can't read another app's private database, so this probably isn't
  worth it.
