-- Alexandria notebook page pipeline: render -> OCR -> page_text_from_server,
-- note search and embeddings. Replaces UltraBridge's lossy in-memory queue
-- (internal/syncbridge) with a durable one.

-- Pages whose render input changed. Written in the same transaction as the
-- sync exchange that changed them (outbox), so a crash never loses one.
-- dirtied_at changes on every re-dirty; a worker deletes its claim only if it
-- is unchanged, so a page edited during OCR is processed again.
CREATE TABLE alexandria_page_dirty (
    page_id     text COLLATE "C" PRIMARY KEY,
    dirtied_at  timestamptz NOT NULL DEFAULT clock_timestamp(),
    attempts    integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_at     timestamptz NOT NULL DEFAULT now(),
    lease_until timestamptz
);
CREATE INDEX alexandria_page_dirty_due ON alexandria_page_dirty(next_at);

-- Recognized text keyed by the exact OCR input, so unchanged pages (including
-- every page after an authoritative restore) are never recognized twice.
CREATE TABLE alexandria_page_ocr (
    page_id       text COLLATE "C" PRIMARY KEY,
    input_hash    text COLLATE "C" NOT NULL CHECK (input_hash ~ '^[0-9a-f]{64}$'),
    text          text NOT NULL,
    model         text NOT NULL DEFAULT '',
    recognized_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX alexandria_embeddings_key ON alexandria_embeddings(note_key, page);
