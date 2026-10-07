-- Alexandria reader mirrors: the materialized winner of each reader row, plus
-- the change journal consumers (search, projections) follow. Ported from
-- UltraBridge (internal/readerstore Install) under Apache-2.0.
--
-- The table section is generated from internal/alexandria/contract.Registry()
-- by reader.SchemaSQL(); reader.TestMigrationMatchesRegistry pins it.

-- BEGIN GENERATED (reader.SchemaSQL)
CREATE TABLE fn_reader_book (
    id text COLLATE "C" NOT NULL PRIMARY KEY,
    asset_id text COLLATE "C" NOT NULL,
    byte_length bigint NOT NULL,
    media_type text COLLATE "C" NOT NULL,
    metadata_json text COLLATE "C" NOT NULL,
    lww_op_ts bigint NOT NULL,
    lww_op_seq bigint NOT NULL,
    lww_site_id text COLLATE "C" NOT NULL
);

CREATE TABLE fn_reader_book_title (
    id text COLLATE "C" NOT NULL PRIMARY KEY,
    title text COLLATE "C" NOT NULL,
    lww_op_ts bigint NOT NULL,
    lww_op_seq bigint NOT NULL,
    lww_site_id text COLLATE "C" NOT NULL
);

CREATE TABLE fn_reader_book_lifecycle (
    id text COLLATE "C" NOT NULL PRIMARY KEY,
    deleted bigint NOT NULL,
    changed_at bigint NOT NULL,
    lww_op_ts bigint NOT NULL,
    lww_op_seq bigint NOT NULL,
    lww_site_id text COLLATE "C" NOT NULL
);

CREATE TABLE fn_reader_annotation (
    id text COLLATE "C" NOT NULL PRIMARY KEY,
    book_id text COLLATE "C" NOT NULL,
    initial_anchor_json text COLLATE "C" NOT NULL,
    canvas_width bigint NOT NULL,
    initial_height bigint NOT NULL,
    creator_session_id text COLLATE "C" NOT NULL,
    lww_op_ts bigint NOT NULL,
    lww_op_seq bigint NOT NULL,
    lww_site_id text COLLATE "C" NOT NULL
);

CREATE TABLE fn_reader_edit_session (
    id text COLLATE "C" NOT NULL PRIMARY KEY,
    annotation_id text COLLATE "C" NOT NULL,
    kind text COLLATE "C" NOT NULL,
    owner_site text COLLATE "C",
    state text COLLATE "C" NOT NULL,
    lww_op_ts bigint NOT NULL,
    lww_op_seq bigint NOT NULL,
    lww_site_id text COLLATE "C" NOT NULL
);

CREATE TABLE fn_reader_stroke (
    id text COLLATE "C" NOT NULL PRIMARY KEY,
    annotation_id text COLLATE "C" NOT NULL,
    session_id text COLLATE "C" NOT NULL,
    paint_order bigint NOT NULL,
    paint_site text COLLATE "C" NOT NULL,
    color bigint NOT NULL,
    pen_width_min bigint NOT NULL,
    pen_width_max bigint NOT NULL,
    brush_kind text COLLATE "C" NOT NULL,
    brush_version bigint NOT NULL,
    brush_seed bigint NOT NULL,
    points bytea NOT NULL,
    point_dynamics bytea,
    lww_op_ts bigint NOT NULL,
    lww_op_seq bigint NOT NULL,
    lww_site_id text COLLATE "C" NOT NULL
);

CREATE TABLE fn_reader_erase_claim (
    id text COLLATE "C" NOT NULL PRIMARY KEY,
    session_id text COLLATE "C" NOT NULL,
    stroke_id text COLLATE "C" NOT NULL,
    active bigint NOT NULL,
    lww_op_ts bigint NOT NULL,
    lww_op_seq bigint NOT NULL,
    lww_site_id text COLLATE "C" NOT NULL
);

CREATE TABLE fn_reader_annotation_value (
    id text COLLATE "C" NOT NULL PRIMARY KEY,
    session_id text COLLATE "C" NOT NULL,
    property text COLLATE "C" NOT NULL,
    value_json text COLLATE "C" NOT NULL,
    lww_op_ts bigint NOT NULL,
    lww_op_seq bigint NOT NULL,
    lww_site_id text COLLATE "C" NOT NULL
);

CREATE TABLE fn_reader_annotation_lifecycle (
    id text COLLATE "C" NOT NULL PRIMARY KEY,
    deleted bigint NOT NULL,
    changed_at bigint NOT NULL,
    lww_op_ts bigint NOT NULL,
    lww_op_seq bigint NOT NULL,
    lww_site_id text COLLATE "C" NOT NULL
);

CREATE TABLE fn_reader_position (
    id text COLLATE "C" NOT NULL PRIMARY KEY,
    book_id text COLLATE "C" NOT NULL,
    site_id text COLLATE "C" NOT NULL,
    locator_json text COLLATE "C" NOT NULL,
    lww_op_ts bigint NOT NULL,
    lww_op_seq bigint NOT NULL,
    lww_site_id text COLLATE "C" NOT NULL
);

CREATE TABLE fn_reader_recognition (
    id text COLLATE "C" NOT NULL PRIMARY KEY,
    annotation_id text COLLATE "C" NOT NULL,
    producer_id text COLLATE "C" NOT NULL,
    input_hash text COLLATE "C" NOT NULL,
    engine text COLLATE "C" NOT NULL,
    model text COLLATE "C",
    language text COLLATE "C",
    status text COLLATE "C" NOT NULL,
    text text COLLATE "C" NOT NULL,
    lww_op_ts bigint NOT NULL,
    lww_op_seq bigint NOT NULL,
    lww_site_id text COLLATE "C" NOT NULL
);

CREATE TABLE fn_content_anchor (
    id text COLLATE "C" NOT NULL PRIMARY KEY,
    selector_json text COLLATE "C" NOT NULL,
    label text COLLATE "C",
    lww_op_ts bigint NOT NULL,
    lww_op_seq bigint NOT NULL,
    lww_site_id text COLLATE "C" NOT NULL
);

CREATE TABLE fn_content_reference (
    id text COLLATE "C" NOT NULL PRIMARY KEY,
    source_anchor_id text COLLATE "C" NOT NULL,
    target_anchor_id text COLLATE "C" NOT NULL,
    label text COLLATE "C",
    lww_op_ts bigint NOT NULL,
    lww_op_seq bigint NOT NULL,
    lww_site_id text COLLATE "C" NOT NULL
);

CREATE TABLE fn_content_anchor_lifecycle (
    id text COLLATE "C" NOT NULL PRIMARY KEY,
    deleted bigint NOT NULL,
    changed_at bigint NOT NULL,
    lww_op_ts bigint NOT NULL,
    lww_op_seq bigint NOT NULL,
    lww_site_id text COLLATE "C" NOT NULL
);

CREATE TABLE fn_content_reference_lifecycle (
    id text COLLATE "C" NOT NULL PRIMARY KEY,
    deleted bigint NOT NULL,
    changed_at bigint NOT NULL,
    lww_op_ts bigint NOT NULL,
    lww_op_seq bigint NOT NULL,
    lww_site_id text COLLATE "C" NOT NULL
);

CREATE INDEX reader_store_annotations_book ON fn_reader_annotation(book_id, id);
CREATE INDEX reader_store_sessions_annotation ON fn_reader_edit_session(annotation_id, id);
CREATE INDEX reader_store_strokes_annotation ON fn_reader_stroke(annotation_id, id);
CREATE INDEX reader_store_claims_session ON fn_reader_erase_claim(session_id, id);
CREATE INDEX reader_store_claims_stroke ON fn_reader_erase_claim(stroke_id, id);
CREATE INDEX reader_store_values_session ON fn_reader_annotation_value(session_id, id);
CREATE INDEX reader_store_recognition_annotation ON fn_reader_recognition(annotation_id, id);
-- END GENERATED

-- Durable invalidations in commit order (rows are written under the reader
-- drain lock). A consumer reads current state, never the row "at" a seq.
CREATE TABLE reader_store_changes (
    seq        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    table_name text COLLATE "C" NOT NULL,
    pk         text COLLATE "C" NOT NULL
);

CREATE TABLE reader_store_change_cursor (
    id  smallint PRIMARY KEY CHECK (id = 1),
    seq bigint NOT NULL
);
INSERT INTO reader_store_change_cursor(id, seq) VALUES (1, 0);
