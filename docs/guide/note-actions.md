# Turn Ink Into Tasks and Events

Note actions let you pick up a different pen instead of opening an app. Write
on a BOOX page in **red** and Alexandria turns it into a task; write in **sky
blue** and it becomes a calendar event. These are the red and sky blue from the
BOOX pen color palette. You can swap the colors or turn either one off. When
the page is recognized, Alexandria reads that color's ink on its own, asks your
summary model to make one item per passage, and adds the results to
[Tasks and Calendar](tasks-and-calendar.md), each with a link back to the page
it came from. Your notebook is never changed.

![A BOOX page with red and sky blue ink, and the Note Actions card listing what it made](../images/boox-notebook.png)

## What You Need First

Note actions run whenever all of these are set up, with no separate switch to
find:

1. **A vision model** with **Enable recognition** ticked, under
   **Settings → AI Tools and Recognition → Provider**.
2. **A summary model** with **Enable summaries** ticked, in the **Audio Note
   Minutes** section of the same page. See [Set Up AI Models](ai-models.md).
3. **Automatic recognition**: **Recognize new BOOX pages automatically** under
   **Automatic Processing**. See [Recognize Handwriting](recognition.md#automatic-recognition).

**Costs:** each page with colored ink makes one more request to your vision
model (for the colored ink alone) and one to your summary model. Pages without
colored ink cost nothing extra.

## Turning It On, Off or Remapping Colors

Open **Settings → AI Tools and Recognition → Note Actions**.

- The card says whether note actions are running: either "On. Ink written
  since *date* is read when its page is recognized." or what's missing, such
  as "Note actions need automatic recognition."
- **Find tasks and events in colored ink** is the main switch. It's on by
  default.
- **Red ink** and **Sky blue ink** each choose **Makes tasks**, **Makes calendar
  events** or **Nothing**. Want both colors to make tasks? Go ahead.
- Choose **Save note actions**.

## What Ink Counts

- **Pen strokes only**, in exactly the palette's red or sky blue. Highlighters,
  text boxes and images don't count, and neither do other shades that merely
  look reddish.
- **Only ink written after note actions were turned on** is read
  automatically, so turning the feature on doesn't conjure a hundred tasks out
  of last year's notebooks. Turning it off and back on starts that clock again.
- A page is read when it's **recognized**, which with automatic recognition
  happens once the notebook has been quiet for five minutes. Skipped, paused
  and blocked pages produce nothing; their recognition card says why.
- **Older ink:** open the page and choose **Find actions** on its **Note
  Actions** card. It reads that page's colored ink now, even if it was written
  before note actions started. Passages already acted on are skipped, so
  pressing it twice is harmless.

Black ink is left alone. So the red "TODO" heading you scribbled for emphasis?
The default instructions tell the model to skip headings and stray words, but
"Buy milk" in red is a task.

## Writing Items So They're Read Correctly

Each separate passage (a line, a bullet, or a few lines that belong together)
becomes **at most one item**. Up to 20 items can come from one page.

**Tasks**
- The title is a short phrase in your own words; any extra lines in the passage
  go into its details.
- A due date is set only when the passage states or implies one: "Friday", "by
  the 22nd", "tomorrow".

**Events**
- An event needs a **date** the model can work out: "Oct 28", "next Tuesday",
  "the 22nd". A passage without a usable date is skipped, and the page's detail
  says so ("1 without a recognizable date skipped.").
- A **time is optional**. With no time, the event lasts **all day**. With a
  time, it lasts **one hour** unless you write an end time ("3–4:30pm").
- A place goes into the event's location.

**Relative dates** ("tomorrow", "next Tuesday") are counted from when you wrote
the ink, not from when the server got around to reading it.

**Time zone.** Times you write are read in the time zone set under
**Settings → Tasks & Integrations → Time zone**. Until you set it, the server's
default is used, which on many installs is UTC, so "3pm" would land at 3pm UTC.
Set the time zone *before* writing your first timed event.

## Where the Results Appear

| Place | What you see |
| --- | --- |
| **Tasks** | The new task, with a **From note** badge linking to the page |
| **Calendar** | The new event under **Upcoming**, with a **From note** badge |
| The page's **Note Actions** card | Each color's status and result ("Made 2 tasks."), **Recognized ink** (what was read from that color), and **Made from this page**, linking to each item |
| **Recognition** (the jobs page) | A **Note Actions** section listing pages with work running or failed |

![The Tasks list](../images/tasks.png)

![The Calendar page](../images/calendar.png)

Items also sync to CalDAV apps like anything else in Tasks and Calendar.

### Items Are Yours Once Made

Each passage is acted on **once**. Changing or erasing the ink later never
edits or deletes what was made. Got the wrong time? Fix the event on the
Calendar page (or in your calendar app). Deleted a task? It stays deleted; the
page lists it as *deleted* under **Made from this page**. New passages added to
the same page later make new items.

## Editing the Instructions

The **Task Instructions** and **Event Instructions** boxes hold what the
summary model is told about turning a passage into an item. The built-in
instructions are shown in the box, so you can edit them rather than start from
scratch. Alexandria always adds the response format it needs (shown under
**Response Format (Always Added)**), so you can't break the parsing by
rewording. Clear a box and save to go back to the built-in instructions.

Examples of useful edits: "Treat lines starting with a person's name as
something to ask that person", or "Ignore anything inside a box".

## Troubleshooting

| Symptom | Cause | Fix |
| --- | --- | --- |
| The page has no **Note Actions** card, or it says note actions need something | A prerequisite is missing | Check the message in **Settings → AI Tools and Recognition → Note Actions** and set up what it names. |
| Nothing was made from fresh ink | The page hasn't been recognized yet, or the ink isn't exactly the palette color | Wait five quiet minutes, check the page's recognition card, and check the pen color. "No ink in this color on the page." means the color didn't match. |
| Nothing was made from older ink | Ink written before note actions started isn't read automatically | Use **Find actions** on that page. |
| "Made 0 tasks." | The model decided the passage wasn't something to do | Open **Recognized ink** to check what was read; reword, or adjust the instructions. |
| "The colored ink had no readable text." | The vision model couldn't read the colored ink | Write a little larger, or check the recognition model. |
| An event was skipped | No recognizable date | Write a date the model can work out, on a new line, and choose **Find actions**. |
| An event is at the wrong time | The time zone wasn't set, or was wrong, when it was read | Fix the event in Calendar; set **Time zone** under Settings → Tasks & Integrations for the future. |
| "The summary model's answer didn't follow the note action format." | The summary model returned something unusable | It retries by itself; if it persists, try a more capable summary model. |
