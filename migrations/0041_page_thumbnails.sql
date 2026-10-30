-- Small page images for the Library preview, kept so a notebook's first pages
-- are drawn once per version rather than on every visit. One row per page:
-- a newer version replaces it. Disposable: anything here can be drawn again.
CREATE TABLE alexandria_page_thumb (
  source text NOT NULL CHECK (source IN ('boox','client')),
  notebook_id text NOT NULL,
  page integer NOT NULL CHECK (page > 0),
  version text NOT NULL,
  png bytea NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  accessed_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (source, notebook_id, page)
);
CREATE INDEX alexandria_page_thumb_accessed ON alexandria_page_thumb(accessed_at);
