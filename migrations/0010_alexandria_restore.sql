-- Alexandria authoritative restore: the published baseline peers adopt, and
-- each old device's one-time adoption of a fresh identity. Ported from
-- UltraBridge (internal/libraryrestore Install) under Apache-2.0.

CREATE TABLE sync_restore_baseline (
    id         smallint PRIMARY KEY CHECK (id = 1),
    generation text COLLATE "C" NOT NULL,
    snapshot   text COLLATE "C" NOT NULL,
    publisher  text COLLATE "C" NOT NULL,
    high_water bigint NOT NULL,
    cursor     bigint NOT NULL
);

CREATE TABLE sync_restore_adoption (
    old_site   text COLLATE "C" NOT NULL,
    generation text COLLATE "C" NOT NULL,
    new_site   text COLLATE "C" NOT NULL UNIQUE,
    token_hash text COLLATE "C" NOT NULL UNIQUE,
    snapshot   text COLLATE "C" NOT NULL,
    PRIMARY KEY (old_site, generation)
);
