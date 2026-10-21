-- Import coordination is separate from boox_operation: older workers can
-- continue ordinary uploads while safely ignoring paused import coordination.
CREATE TABLE boox_book_import (
 id uuid PRIMARY KEY,
 request_id uuid NOT NULL UNIQUE,
 native_uid text NOT NULL,
 generation text NOT NULL,
 destination text NOT NULL DEFAULT '',
 destination_revision text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE boox_book_import_item (
 import_id uuid NOT NULL REFERENCES boox_book_import(id),
 path text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('file','folder')),
 bytes bigint NOT NULL DEFAULT 0,
 native_id text NOT NULL DEFAULT '',
 parent_id text NOT NULL DEFAULT '',
 expected_revision text NOT NULL DEFAULT '',
 state text NOT NULL DEFAULT 'waiting',
 error_code text NOT NULL DEFAULT '',
 sha256 text NOT NULL DEFAULT '',
 operation_id uuid REFERENCES boox_operation(id),
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(import_id,path)
);
CREATE INDEX boox_book_import_pending ON boox_book_import_item(next_attempt_at) WHERE kind='file' AND state='queued';
CREATE INDEX boox_book_import_folder ON boox_book_import_item(native_id) WHERE kind='folder';
