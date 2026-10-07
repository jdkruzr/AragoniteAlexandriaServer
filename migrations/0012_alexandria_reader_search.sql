-- Alexandria annotation search: documents projected from reader rows (title,
-- highlighted quote, current recognition or correction) and the durable jobs
-- that keep them current. Ported from UltraBridge (internal/readersearch).

CREATE TABLE reader_search_jobs (
    seq        bigint PRIMARY KEY,
    table_name text COLLATE "C" NOT NULL,
    pk         text COLLATE "C" NOT NULL,
    after_id   text COLLATE "C" NOT NULL DEFAULT '',
    done       boolean NOT NULL DEFAULT false,
    retry_at   bigint NOT NULL DEFAULT 0,
    error_code text NOT NULL DEFAULT ''
);
CREATE INDEX reader_search_pending ON reader_search_jobs(done, retry_at, seq);

CREATE TABLE reader_search_documents (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    annotation_id   text COLLATE "C" NOT NULL UNIQUE,
    book_id         text COLLATE "C" NOT NULL,
    title           text NOT NULL,
    quote           text NOT NULL,
    recognized_text text NOT NULL,
    anchor          text NOT NULL,
    input_hash      text NOT NULL,
    alternatives    text NOT NULL,
    revision        bigint NOT NULL,
    search          tsvector GENERATED ALWAYS AS (
        to_tsvector('simple', title) || to_tsvector('simple', quote) || to_tsvector('simple', recognized_text)
    ) STORED
);
CREATE INDEX reader_search_documents_search ON reader_search_documents USING gin(search);
CREATE INDEX reader_search_documents_book ON reader_search_documents(book_id);

-- Staleness checks look up later journal entries by key.
CREATE INDEX reader_search_changes_key ON reader_store_changes(table_name, pk, seq);
