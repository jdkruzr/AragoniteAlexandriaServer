CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS loom_settings (
    key text PRIMARY KEY,
    value jsonb NOT NULL,
    secret boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS loom_sources (
    id uuid PRIMARY KEY,
    source_type text NOT NULL,
    name text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    config jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS loom_objects (
    object_key text PRIMARY KEY,
    sha256 text NOT NULL,
    size_bytes bigint NOT NULL CHECK (size_bytes >= 0),
    media_type text NOT NULL DEFAULT 'application/octet-stream',
    ref_count bigint NOT NULL DEFAULT 0 CHECK (ref_count >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    delete_after timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS loom_objects_sha256_idx ON loom_objects(sha256);
CREATE INDEX IF NOT EXISTS loom_objects_gc_idx ON loom_objects(delete_after) WHERE delete_after IS NOT NULL;

CREATE TABLE IF NOT EXISTS loom_note_content (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    note_key text NOT NULL,
    page integer NOT NULL CHECK (page >= 0),
    title_text text NOT NULL DEFAULT '',
    body_text text NOT NULL DEFAULT '',
    keywords text NOT NULL DEFAULT '',
    source text NOT NULL,
    model text NOT NULL DEFAULT '',
    indexed_at timestamptz NOT NULL DEFAULT now(),
    search_document tsvector GENERATED ALWAYS AS (
        setweight(to_tsvector('simple'::regconfig, coalesce(title_text, '')), 'A') ||
        setweight(to_tsvector('simple'::regconfig, coalesce(keywords, '')), 'B') ||
        setweight(to_tsvector('simple'::regconfig, coalesce(body_text, '')), 'C')
    ) STORED,
    UNIQUE(note_key, page)
);
CREATE INDEX IF NOT EXISTS loom_note_content_search_idx ON loom_note_content USING gin(search_document);

CREATE TABLE IF NOT EXISTS loom_embeddings (
    note_key text NOT NULL,
    page integer NOT NULL CHECK (page >= 0),
    chunk integer NOT NULL DEFAULT 0 CHECK (chunk >= 0),
    model text NOT NULL,
    dimensions integer NOT NULL CHECK (dimensions > 0),
    embedding vector NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(note_key, page, chunk)
);

CREATE TYPE loom_job_status AS ENUM ('pending', 'leased', 'done', 'failed');

CREATE TABLE IF NOT EXISTS loom_jobs (
    id uuid PRIMARY KEY,
    job_type text NOT NULL,
    payload jsonb NOT NULL,
    status loom_job_status NOT NULL DEFAULT 'pending',
    idempotency_key text NOT NULL UNIQUE,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts integer NOT NULL DEFAULT 5 CHECK (max_attempts > 0),
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_owner text,
    lease_until timestamptz,
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    finished_at timestamptz
);
CREATE INDEX IF NOT EXISTS loom_jobs_claim_idx ON loom_jobs(status, available_at, created_at);
CREATE INDEX IF NOT EXISTS loom_jobs_expired_lease_idx ON loom_jobs(lease_until) WHERE status = 'leased';

CREATE TABLE IF NOT EXISTS loom_outbox (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    topic text NOT NULL,
    event_key text NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz,
    UNIQUE(topic, event_key)
);
CREATE INDEX IF NOT EXISTS loom_outbox_unpublished_idx ON loom_outbox(id) WHERE published_at IS NULL;

CREATE TABLE IF NOT EXISTS loom_import_runs (
    id uuid PRIMARY KEY,
    source_fingerprint text NOT NULL UNIQUE,
    status text NOT NULL,
    manifest jsonb NOT NULL,
    started_at timestamptz NOT NULL DEFAULT now(),
    finished_at timestamptz,
    last_error text NOT NULL DEFAULT ''
);
