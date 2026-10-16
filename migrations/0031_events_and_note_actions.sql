-- Calendar events (2026-10-16): a second CalDAV collection beside Tasks,
-- served as VEVENT. Times are instants; an all-day event stores midnight UTC
-- of its first day and of the day after its last (DTEND is exclusive), and
-- tzid is the zone a timed event is shown in. ical_blob keeps what a client
-- sent so properties Alexandria doesn't model (RRULE, VALARM, attendees)
-- survive a round trip. Timestamps in ms match alexandria_tasks so the same
-- sync-collection code serves both.
CREATE TABLE alexandria_events (
 event_id text PRIMARY KEY,
 title text NOT NULL DEFAULT '',
 detail text NOT NULL DEFAULT '',
 location text NOT NULL DEFAULT '',
 url text NOT NULL DEFAULT '',
 start_at timestamptz NOT NULL,
 end_at timestamptz NOT NULL,
 all_day boolean NOT NULL DEFAULT false,
 tzid text NOT NULL DEFAULT '',
 ical_blob text NOT NULL DEFAULT '',
 deleted boolean NOT NULL DEFAULT false,
 created_at bigint NOT NULL,
 updated_at bigint NOT NULL,
 CHECK (end_at >= start_at)
);
CREATE INDEX alexandria_events_updated ON alexandria_events(updated_at);
CREATE INDEX alexandria_events_start ON alexandria_events(start_at) WHERE NOT deleted;

-- Like alexandria_task_purged: a permanently deleted UID is never recreated.
CREATE TABLE alexandria_event_purged (
 event_id text PRIMARY KEY,
 purged_at bigint NOT NULL
);

-- Note actions: ink in an assigned pen color on a BOOX page becomes tasks or
-- events. One row per page and color; ink_hash is the colored strokes, so the
-- row is requeued only when that ink changes.
CREATE TABLE alexandria_note_action (
 source text NOT NULL CHECK (source IN ('boox')),
 notebook_id text COLLATE "C" NOT NULL,
 page_id text COLLATE "C" NOT NULL,
 color text NOT NULL CHECK (color IN ('red','blue')),
 action text NOT NULL CHECK (action IN ('task','event')),
 state text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','processing','ready','failed')),
 lease_token text NOT NULL DEFAULT '',
 lease_until timestamptz,
 next_at timestamptz NOT NULL DEFAULT now(),
 attempts integer NOT NULL DEFAULT 0,
 ink_hash text NOT NULL DEFAULT '',
 input_hash text NOT NULL DEFAULT '',
 ocr_text text NOT NULL DEFAULT '',
 model text NOT NULL DEFAULT '',
 detail text NOT NULL DEFAULT '',
 page_saved_at timestamptz,
 requested_at timestamptz NOT NULL DEFAULT now(),
 finished_at timestamptz,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(source,notebook_id,page_id,color)
);
CREATE INDEX alexandria_note_action_due ON alexandria_note_action(next_at) WHERE state IN ('queued','processing');

-- What each page's ink has produced. A passage (normalized) is acted on once
-- per page and color; later runs skip it, and the item is never changed again
-- by Alexandria. item_id is a task_id or event_id.
CREATE TABLE alexandria_note_action_item (
 source text NOT NULL,
 notebook_id text COLLATE "C" NOT NULL,
 page_id text COLLATE "C" NOT NULL,
 color text NOT NULL,
 fingerprint text NOT NULL,
 kind text NOT NULL CHECK (kind IN ('task','event')),
 item_id text NOT NULL,
 source_text text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(source,notebook_id,page_id,color,fingerprint)
);
CREATE INDEX alexandria_note_action_item_item ON alexandria_note_action_item(kind,item_id);
