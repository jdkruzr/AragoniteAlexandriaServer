# Tasks and Calendar

Alexandria keeps one task list and one calendar for your library. You can add
and edit items in the web app, sync them to your phone or desktop with any
CalDAV app, have an AI assistant manage them, and (with
[Note Actions](note-actions.md)) create them by writing in color on a BOOX
page. Everything is the same list everywhere: tick a task done on your phone
and it's done in the web app too.

![The Tasks page](../images/tasks.png)

## What You Need First

- A running Alexandria Server you can sign in to.
- To sync with apps: an address your phone or computer can reach. Away from
  home that usually means HTTPS through a reverse proxy; see
  [Put Alexandria Behind HTTPS](https-proxy.md).
- Your time zone set (below), so due dates and event times land on the right
  day.

## Set Your Time Zone

1. Go to **Settings → Tasks & Integrations**.
2. Pick your zone from **Time zone**. **Server default** uses the server's own
   zone. If your browser reports a different zone, a hint offers **Use it**.
3. Select **Save**.

The time zone is used for task due dates and event times written or typed
without a time zone, including those from note actions. The web Calendar shows
times in this zone.

## Tasks

Select **Tasks** in the top bar. It's also the home page.

### Adding and Editing

1. Type into **New task**, optionally pick a **Due date**, and select **Add
   task**.
2. To change a task, select **Edit**. You can change its **Title**, **Due
   date** and **Notes**, then **Save**.
3. Select **Done** on a task to mark it complete. Completed tasks move to the
   **Done** list.

Due dates in the web app are dates only. A CalDAV app can set a time of day;
see **Due dates** below.

### Sorting

Use **Sort by** (**Due date**, **Created**, **Updated** or **Title**) and the
order menu (**Oldest or A–Z first** / **Newest or Z–A first**), then **Sort**.

### Bulk Actions

Tick the boxes beside tasks, then choose under **Bulk Actions**:

| Button | What it does |
|---|---|
| **Mark done** | Completes the ticked tasks. |
| **Reopen** | Puts completed tasks back in **To Do**. |
| **Delete** | Moves the ticked tasks to Deleted Tasks. |

**Clear Completed Tasks**, under the **Done** list, moves every completed task
to Deleted Tasks in one go.

### Deleted Tasks

Deleting is a two-step affair, on purpose.

1. **Delete** moves a task to **Deleted tasks** (the button at the top of the
   page). It disappears from your apps but can be brought back.
2. In Deleted Tasks, tick tasks and select **Restore** to bring them back, or
   **Delete permanently**. Alexandria lists what will go and asks you to
   confirm.

A permanently deleted task is erased for good: your devices are told it's
gone, and a device that still has an old copy can't bring it back.

### Where a Task Came From

- **Alexandria Client** badge: made from an Alexandria Client page; the link
  opens that page.
- **From note** badge: made by [Note Actions](note-actions.md) from a BOOX
  page; the link opens the page.

## Calendar

Select **Calendar** in the top bar. **Upcoming** starts with the next 30 days,
anchored to today; **Show next 30 days** extends that list. **Past 30 Days**
stays below the main sections, and **Deleted events** has its own page.

Use **Day**, **Week**, or **Month** to browse the calendar. **Previous**,
**Today**, **Next**, and the date picker move through dates. Month shows up to
three entries per day; **+N more** opens that Day view. Week scrolls sideways
on a small screen. Day and Week separate overlapping appointments into columns.

The defaults are **Upcoming first**, **Month**, and **Sunday** as the first day
of the week. Change the section order above the sections, or choose Monday in
**Week starts on** and select **Go**. This browser remembers those preferences;
a different browser has its own. Returning to Calendar starts at today's date.
Links and forms work with JavaScript disabled.

![Calendar Month view with synthetic events](../calendar/20261018/desktop-month.png)

### Adding an Event

1. Select **Add event**, or select a date in the calendar for an all-day event.
   Selecting an hourly slot prefills a one-hour appointment.
2. Enter a title and adjust the dates, times, location, link or notes.
3. Select **Save**. Save and Cancel return to the same calendar view.

Leave the time empty for an all-day event. Times are in the time zone from
Settings, which the page reminds you of. When daylight saving time repeats a
local hour, the UTC offset fields distinguish its two occurrences. A local time
that does not exist produces a validation message and keeps your form values.

### Editing an Event

Select **Edit** to change the **Title**, **Date**, **Start time (optional)**,
**End date (optional)**, **End time (optional)**, **Location**, **Link** and
**Notes**. An end date makes a multi-day event. Events from note actions show a
**From note** link back to their page.

### Deleting and Restoring

**Delete** moves an event to **Deleted events**. There you can **Restore** it,
or tick **Permanently** and select **Delete permanently** to erase it.

Imported recurring events and their exceptions appear in every calendar view
and both event lists. Selecting one opens read-only occurrence details: edit
or delete the series in your CalDAV app. Web edits cannot flatten the series.
Repeat rules, reminders, attendees and original calendar blobs are preserved.

If a series is malformed, unsupported, or too large to expand safely, a notice
names it and marks the results incomplete. Use your CalDAV app to inspect it;
an incomplete result does not mean the series has no events.

## Connect a CalDAV App

Alexandria serves two CalDAV collections under one account:

| Collection | Holds | Address |
|---|---|---|
| Task list | Tasks (VTODO) | `https://alexandria.example.com/caldav/user/calendars/tasks/` |
| Calendar | Events (VEVENT) | `https://alexandria.example.com/caldav/user/calendars/events/` |

Open **Connect a CalDAV app** near the heading on Tasks or Calendar, or in
**Settings → Tasks & Integrations**. It shows your server's discovery address
and the relevant collection address, with Copy buttons. If copying is blocked,
the address is selected so you can copy it manually. The address fields also
work without JavaScript.

Apps that support discovery can start from the server address alone:
`https://alexandria.example.com/` (Alexandria answers `/.well-known/caldav`).

**Credentials:** your Alexandria username and password, the same ones you use
to sign in to the web app. API tokens don't work as a CalDAV password.

Alexandria supports incremental sync, so apps fetch only what changed.

### Names Apps Show

In **Settings → Tasks & Integrations**, **Task list name** and **Calendar
name** set what your apps display for the two collections.

### Example Setups

The menu names below belong to each app and may differ between versions.

**DAVx5 (Android):**

1. Add an account with **Login with URL and user name**.
2. Base URL: `https://alexandria.example.com/`; enter your username and
   password.
3. Turn on both collections. Use a tasks app DAVx5 supports (such as
   jtx Board, Tasks.org or OpenTasks) for the task list.

**Apple Calendar and Reminders (macOS, iOS):**

1. Add a CalDAV account: in the system's account settings choose
   **Other**, then a CalDAV account (on macOS, also from Calendar's
   Settings → Accounts).
2. Server: `alexandria.example.com`, plus your username and password.
3. The task list appears in Reminders and the calendar in Calendar.

**Thunderbird:**

1. Create a new calendar **On the Network**.
2. Enter your username and the location
   `https://alexandria.example.com/caldav/user/calendars/events/`, or the
   server address to let it find both collections.
3. Thunderbird shows the task list under Tasks.

BOOX Calendar Memo can also use the calendar with the same address and account.

### Due Dates

Some apps handle due times badly (a task due "today" ends up due yesterday at
11 p.m.). **Due dates** in **Settings → Tasks & Integrations** fixes that:

| Setting | Effect |
|---|---|
| **Keep the time of day** | A due date stays as the app wrote it: date-only stays date-only, a time stays a time. |
| **Date only** | Drops the time of day from due dates, both ways. |

## What Syncs

| Item | Synced fields |
|---|---|
| Tasks | Title, status (done or not), due date, notes, and deletions. Other fields an app sets are kept and sent back to apps. |
| Events | Title, start and end, all-day, location, link, notes, and deletions. Repeat rules, reminders and attendees are kept as the app wrote them. |

Changes go both ways: the web app, CalDAV apps, note actions and an
[AI assistant](assistants.md) all edit the same items. Apps see changes on
their next sync.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| The app says the login failed | Wrong username or password, or you used an API token | Use the account you sign in to the web app with. |
| The app can't find any calendars | Discovery isn't reaching Alexandria, often because a reverse proxy doesn't pass `/.well-known/caldav` | Enter the full collection address from **Settings → Tasks & Integrations** instead, or fix the proxy (see [Put Alexandria Behind HTTPS](https-proxy.md)). |
| Tasks show up in a calendar app but not events (or the other way round) | Only one collection was added | Add both addresses, or turn on both collections in the app. |
| Due dates are a day off | Time zone not set, or the app mishandles due times | Set **Time zone**; if it persists, choose **Date only** under **Due dates**. |
| A deleted task is missing from Deleted Tasks | It was deleted permanently | Permanent deletion can't be undone. |
| Events show at the wrong hour | Time zone not set | Set it in **Settings → Tasks & Integrations**. |

## Subscribe to a public calendar

Choose **Subscribe to calendar** on Calendar, or open **Settings → Tasks &
Integrations → Calendar subscriptions**. Enter a name, choose a color, and paste
an HTTPS iCalendar (ICS) feed address. In Google Calendar, use **Public address
in iCal format**; a normal calendar webpage/share URL is not the feed.

Subscribed events appear in Day, Week, Month, Upcoming and Past, with the source
calendar's name and color. **Visible calendars** lets you hide or show each source
and the local calendar for this browser. Event details are read-only; make changes
at the source. Subscriptions do not add events to Alexandria's writable CalDAV
collection or its event API.

Alexandria refreshes enabled subscriptions every 30 minutes. **Refresh now**
requests an earlier refresh; repeated requests are limited to one attempt per
30 seconds. Settings shows the last successful refresh and any error. A failed
refresh keeps the last good snapshot. An unsupported time zone or recurrence rule
produces an incomplete-results notice instead of silently hiding the problem.
Uncheck **Enable calendar and automatic refresh** to pause a source. **Unsubscribe**
removes its cached events after confirmation and leaves local events untouched.

Public feeds need no Google sign-in. There is a limit of 20 subscriptions, 4 MiB
per downloaded feed, and 2,000 event series per source. The Server worker must be
running for refreshes; normal page loads read the cached snapshot.

Example checked on 2026-10-19: the [TCDP Community Calendar page](https://www.traviscountydemocrats.org/tcdp-calendar)
embeds a public Google Calendar. Its direct [public ICS feed](https://calendar.google.com/calendar/ical/info%40travisdems.org/public/basic.ics)
can be pasted into the subscription form. The webpage URL itself cannot.
