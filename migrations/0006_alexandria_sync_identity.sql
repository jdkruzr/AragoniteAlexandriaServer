-- Alexandria shared-library sync: the server's own authoring site, device
-- identities and the library generation fence. Ported from UltraBridge
-- (internal/syncstore sync_site, internal/syncidentity, internal/syncgeneration).
-- Text keys use the C collation so ordering matches SQLite's byte order.

CREATE TABLE sync_site (
    id          smallint PRIMARY KEY CHECK (id = 1),
    site_id     text COLLATE "C" NOT NULL,
    last_op_seq bigint NOT NULL DEFAULT 0,
    last_hlc    bigint NOT NULL DEFAULT 0
);

-- Enrollment stores a HASH of the device's bearer key, never the key itself.
-- Revoked bindings are kept: a site or key is never silently reused.
CREATE TABLE sync_device_identity (
    site_id    text COLLATE "C" PRIMARY KEY,
    token_hash text COLLATE "C" NOT NULL UNIQUE CHECK (token_hash ~ '^[0-9a-f]{64}$'),
    created_at bigint NOT NULL,
    revoked    boolean NOT NULL DEFAULT false
);

-- Sites retired by an authoritative restore can never enroll again.
CREATE TABLE sync_retired_replica (
    site_id text COLLATE "C" PRIMARY KEY
);

CREATE TABLE sync_library_generation (
    id         smallint PRIMARY KEY CHECK (id = 1),
    generation text NOT NULL CHECK (generation ~ '^[0-9a-f]{64}$')
);

CREATE TABLE sync_device_generation (
    site_id    text COLLATE "C" PRIMARY KEY,
    generation text NOT NULL CHECK (generation ~ '^[0-9a-f]{64}$')
);

CREATE TABLE sync_library_replacement (
    request_id          text COLLATE "C" PRIMARY KEY,
    expected_generation text NOT NULL,
    snapshot_hash       text NOT NULL,
    publisher           text COLLATE "C" NOT NULL,
    generation          text NOT NULL UNIQUE
);
