-- Alexandria assets-v1: immutable book and snapshot bytes in 256 KiB chunks.
-- Metadata lives here; chunk bytes live in S3-compatible storage, keyed by the
-- chunk's SHA-256. The chunk row is the sole authority that a chunk was
-- received: an object without a row is an orphan that GC removes.
-- Ported from Rhizome assets.SQLStore (rhizome_asset, rhizome_asset_chunk).

CREATE TABLE rhizome_asset (
    asset_id    text COLLATE "C" PRIMARY KEY CHECK (asset_id ~ '^[0-9a-f]{64}$'),
    byte_length bigint NOT NULL CHECK (byte_length >= 0),
    chunk_bytes integer NOT NULL CHECK (chunk_bytes = 262144),
    state       text NOT NULL CHECK (state IN ('staging', 'verifying', 'ready', 'invalid')),
    generation  bigint NOT NULL DEFAULT 0
);

CREATE TABLE rhizome_asset_chunk (
    asset_id    text COLLATE "C" NOT NULL,
    chunk_index bigint NOT NULL CHECK (chunk_index >= 0),
    sha256      text COLLATE "C" NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    byte_length integer NOT NULL CHECK (byte_length BETWEEN 1 AND 262144),
    PRIMARY KEY (asset_id, chunk_index)
);
CREATE INDEX rhizome_asset_chunk_sha256 ON rhizome_asset_chunk(sha256);

-- Upload intent, touched before every object PUT. GC deletes an object only
-- while holding this row's lock, so a concurrent re-upload of the same bytes
-- waits and then writes the object again instead of losing it.
CREATE TABLE rhizome_asset_object (
    sha256     text COLLATE "C" PRIMARY KEY CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    touched_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX rhizome_asset_object_touched ON rhizome_asset_object(touched_at);
