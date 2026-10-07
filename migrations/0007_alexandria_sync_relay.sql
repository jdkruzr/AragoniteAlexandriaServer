-- Alexandria shared-library relay: the global op log, per-device cursors, the
-- ForestNote writer mirrors and the reader inbox. Ported from UltraBridge
-- (internal/syncstore Migrate, internal/readerstore Install) under Apache-2.0.
--
-- sync_seq is the single-writer lock: every mutating sync transaction takes
-- its row FOR UPDATE and allocates seq from it (never a PostgreSQL sequence),
-- so commit order equals seq order and a pulling device never skips a gap.
-- Payloads are text, never jsonb: relay must return the exact bytes stored.

CREATE TABLE sync_seq (
    id       smallint PRIMARY KEY CHECK (id = 1),
    last_seq bigint NOT NULL
);
INSERT INTO sync_seq(id, last_seq) VALUES (1, 0);

CREATE TABLE sync_ops (
    seq        bigint PRIMARY KEY,
    site_id    text COLLATE "C" NOT NULL,
    op_seq     bigint NOT NULL,
    table_name text COLLATE "C" NOT NULL,
    pk         text COLLATE "C" NOT NULL,
    wall_ts    bigint NOT NULL,
    payload    text NOT NULL,
    applied_at bigint NOT NULL,
    UNIQUE (site_id, op_seq)
);

CREATE TABLE sync_cursors (
    site_id        text COLLATE "C" PRIMARY KEY,
    last_pull_seq  bigint NOT NULL DEFAULT 0,
    acked_op_seq   bigint NOT NULL DEFAULT 0,
    updated_at     bigint NOT NULL,
    device_name    text NOT NULL DEFAULT '',
    operator_label text NOT NULL DEFAULT ''
);

-- Writer mirrors. No foreign keys: the wire is row-level LWW, so apply order
-- is irrelevant. Every row carries the (op_ts, op_seq, site_id) provenance.
CREATE TABLE fn_folder (
    id               text COLLATE "C" PRIMARY KEY,
    name             text,
    sort_order       bigint,
    created_at       bigint,
    deleted_at       bigint,
    parent_folder_id text COLLATE "C",
    lww_wall_ts      bigint NOT NULL,
    lww_op_seq       bigint NOT NULL,
    lww_site_id      text COLLATE "C" NOT NULL
);
CREATE INDEX idx_fn_folder_parent ON fn_folder(parent_folder_id);

CREATE TABLE fn_notebook (
    id               text COLLATE "C" PRIMARY KEY,
    name             text,
    sort_order       bigint,
    created_at       bigint,
    deleted_at       bigint,
    folder_id        text COLLATE "C",
    aspect_long_axis bigint,
    page_width       bigint,
    page_height      bigint,
    lww_wall_ts      bigint NOT NULL,
    lww_op_seq       bigint NOT NULL,
    lww_site_id      text COLLATE "C" NOT NULL
);
CREATE INDEX idx_fn_notebook_folder ON fn_notebook(folder_id);

CREATE TABLE fn_page (
    id                text COLLATE "C" PRIMARY KEY,
    notebook_id       text COLLATE "C",
    sort_order        bigint,
    created_at        bigint,
    deleted_at        bigint,
    template          text,
    template_pitch_mm bigint,
    lww_wall_ts       bigint NOT NULL,
    lww_op_seq        bigint NOT NULL,
    lww_site_id       text COLLATE "C" NOT NULL
);
CREATE INDEX idx_fn_page_nb ON fn_page(notebook_id);

CREATE TABLE fn_stroke (
    id             text COLLATE "C" PRIMARY KEY,
    page_id        text COLLATE "C",
    color          bigint,
    pen_width_min  bigint,
    pen_width_max  bigint,
    points         bytea,
    brush_kind     text NOT NULL DEFAULT 'fountain',
    brush_version  bigint NOT NULL DEFAULT 1,
    brush_seed     bigint NOT NULL DEFAULT 0,
    point_dynamics bytea,
    z              bigint,
    created_at     bigint,
    deleted_at     bigint,
    lww_wall_ts    bigint NOT NULL,
    lww_op_seq     bigint NOT NULL,
    lww_site_id    text COLLATE "C" NOT NULL
);
CREATE INDEX idx_fn_stroke_pg ON fn_stroke(page_id, z);

CREATE TABLE fn_text_box (
    id           text COLLATE "C" PRIMARY KEY,
    page_id      text COLLATE "C",
    x            bigint,
    y            bigint,
    width        bigint,
    height       bigint,
    text         text,
    font_name    text,
    font_size    bigint,
    color        bigint,
    weight       bigint,
    border_width bigint,
    z            bigint,
    created_at   bigint,
    deleted_at   bigint,
    lww_wall_ts  bigint NOT NULL,
    lww_op_seq   bigint NOT NULL,
    lww_site_id  text COLLATE "C" NOT NULL
);
CREATE INDEX idx_fn_text_box_page ON fn_text_box(page_id, z);

CREATE TABLE fn_page_text_from_server (
    id          text COLLATE "C" PRIMARY KEY,
    text        text,
    ocr_at      bigint,
    model       text,
    created_at  bigint,
    deleted_at  bigint,
    lww_wall_ts bigint NOT NULL,
    lww_op_seq  bigint NOT NULL,
    lww_site_id text COLLATE "C" NOT NULL
);

CREATE TABLE fn_page_text_from_client (
    id          text COLLATE "C" PRIMARY KEY,
    text        text,
    ocr_at      bigint,
    model       text,
    created_at  bigint,
    deleted_at  bigint,
    lww_wall_ts bigint NOT NULL,
    lww_op_seq  bigint NOT NULL,
    lww_site_id text COLLATE "C" NOT NULL
);

-- Reader inbox: every received reader row, in arrival order, before the
-- dependency gate materializes it. payload is bytea because a quarantined row
-- keeps its original (possibly invalid UTF-8) bytes for diagnostics; valid
-- rows are canonical JSON. Rows are inserted only under the sync_seq lock, so
-- seq order is commit order.
CREATE TABLE reader_store_incoming (
    seq     bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    site_id text COLLATE "C" NOT NULL,
    op_seq  bigint NOT NULL,
    payload bytea NOT NULL,
    state   text NOT NULL CHECK (state IN ('pending', 'applied', 'quarantined')),
    reason  text NOT NULL,
    CONSTRAINT reader_store_incoming_identity UNIQUE (site_id, op_seq)
);
CREATE INDEX reader_store_incoming_pending ON reader_store_incoming(state, seq);
