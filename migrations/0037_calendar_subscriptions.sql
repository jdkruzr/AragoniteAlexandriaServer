-- Public ICS feeds are independent of writable local/CalDAV event identities.
CREATE TABLE alexandria_calendar_subscription (
 id text PRIMARY KEY,
 name text NOT NULL CHECK(length(name) BETWEEN 1 AND 120),
 color text NOT NULL CHECK(color IN ('blue','green','purple','orange','gray')),
 feed_url text NOT NULL,
 enabled boolean NOT NULL DEFAULT true,
 revision bigint NOT NULL DEFAULT 1,
 next_at timestamptz NOT NULL DEFAULT now(),
 lease_until timestamptz,
 lease_token text NOT NULL DEFAULT '',
 last_success timestamptz,
 last_attempt timestamptz,
 error_code text NOT NULL DEFAULT '',
 failures integer NOT NULL DEFAULT 0,
 etag text NOT NULL DEFAULT '',
 modified text NOT NULL DEFAULT '',
 original_feed text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX alexandria_calendar_subscription_due ON alexandria_calendar_subscription(next_at) WHERE enabled;
CREATE TABLE alexandria_calendar_import (
 subscription_id text NOT NULL REFERENCES alexandria_calendar_subscription(id) ON DELETE CASCADE,
 uid text NOT NULL,
 series_key text NOT NULL,
 ical_blob text NOT NULL,
 PRIMARY KEY(subscription_id,uid),
 UNIQUE(subscription_id,series_key)
);
