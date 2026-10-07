-- Sync epoch (Rhizome spec/protocol.md "Server rewind"). Every /sync/v1
-- response carries it, and it changes only when the server's history is
-- rewound. A database restored from a backup has forgotten each device's
-- ops after the backup, while the devices have already pruned them as
-- acknowledged. A new epoch makes each device re-send what it authored
-- after the server's acknowledgement point, then re-pull from the start.
-- restore.sh calls alexandria_rewind_sync_epoch() after pg_restore.

CREATE TABLE sync_epoch (
    id    smallint PRIMARY KEY CHECK (id = 0),
    epoch text COLLATE "C" NOT NULL CHECK (epoch <> '')
);
INSERT INTO sync_epoch (id, epoch) VALUES (0, gen_random_uuid()::text);

CREATE FUNCTION alexandria_rewind_sync_epoch() RETURNS text
LANGUAGE sql AS $$
    UPDATE sync_epoch SET epoch = gen_random_uuid()::text WHERE id = 0 RETURNING epoch;
$$;
